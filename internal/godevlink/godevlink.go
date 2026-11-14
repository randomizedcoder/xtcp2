// Package godevlink is a read-only Go clone of iproute2's devlink(8), the
// version the flake pins (7.2.0), built on pkg/xtcpnl's devlink decoders.
//
// Like goip for ip(8) and gordma for rdma(8), it exists to make xtcpnl's
// devlink coverage something a diff can check: replaying devlink's nlmon (ws)
// pcap corpus (pkg/xtcpnl/testdata/<kernel>/ws/devlink) must reproduce
// devlink's .out, .err and .rc byte for byte. The output rules are devlink's,
// down to the trailing spaces and the handle grouping; the comments cite the
// devlink/devlink.c function each piece follows.
//
// Scope is the read-only dump/show subcommands actually captured: dev show,
// port show, sb show, resource show, dev param show, region show, dev info,
// health show, trap show and trap group show. Everything else — the writes,
// monitor, dpipe/eswitch/sb pools, and the subcommands with no capture — is
// refused. godevlink never writes.
package godevlink

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// Version is what -V prints: devlink's, since godevlink clones that release.
const Version = "devlink utility, iproute2-7.2.0"

// DEVLINK_GENL_NAME / VERSION (include/uapi/linux/devlink.h).
const (
	devlinkGenlName    = "devlink"
	devlinkGenlVersion = 1
)

// DEVLINK_CMD_* the show paths send, by the upstream enum
// (include/uapi/linux/devlink.h). The capture kernel renumbers these, so the
// command byte is not matched on replay (see source.go); they are the numbers
// a live devlink of the upstream ABI would send.
const (
	cmdGet               uint8 = 1
	cmdPortGet           uint8 = 5
	cmdSBGet             uint8 = 11
	cmdDpipeTableGet     uint8 = 35
	cmdResourceDump      uint8 = 40
	cmdParamGet          uint8 = 44
	cmdRegionGet         uint8 = 48
	cmdInfoGet           uint8 = 57
	cmdHealthReporterGet uint8 = 58
	cmdTrapGet           uint8 = 67
	cmdTrapGroupGet      uint8 = 71
	ctrlCmdGetFamily     uint8 = 3
)

// DEVLINK_ATTR_* the request builders put (include/uapi/linux/devlink.h).
const (
	attrBusName uint16 = 1
	attrDevName uint16 = 2
)

// errFail is a failure godevlink has already reported to stderr, as devlink
// does before main returns EXIT_FAILURE.
var errFail = errors.New("godevlink: failed")

// Run is godevlink's main: it returns the exit code. GODEVLINK_REPLAY=<file>
// answers requests from a ws pcap capture instead of the kernel.
func Run(args []string, stdout, stderr io.Writer) int {
	return run(config{replay: os.Getenv("GODEVLINK_REPLAY")}, args, stdout, stderr)
}

type config struct {
	src    Source // overrides replay and the kernel when set
	replay string
}

// runner is devlink.c's struct dl, cut to what the read paths need.
type runner struct {
	d        *dl
	stderr   io.Writer
	src      Source
	args     []string
	familyID uint16
	seq      uint32
}

func (r *runner) errf(format string, a ...any) {
	if _, err := fmt.Fprintf(r.stderr, format, a...); err != nil && r.d.err == nil {
		r.d.err = err
	}
}

func (r *runner) nextSeq() uint32 {
	r.seq++
	return r.seq
}

func run(cfg config, args []string, stdout, stderr io.Writer) int {
	w := bufio.NewWriter(stdout)
	r := &runner{d: &dl{w: w}, stderr: stderr, seq: 1}
	rc, done := r.parseOptions(args)
	if !done {
		rc = r.start(cfg)
	}
	if err := w.Flush(); err != nil && r.d.err == nil {
		r.d.err = err
	}
	if r.d.err != nil && rc == 0 {
		rc = 1
	}
	return rc
}

// start opens the source, runs dl_init (the one CTRL_CMD_GETFAMILY), then the
// command.
func (r *runner) start(cfg config) int {
	switch {
	case cfg.src != nil:
		r.src = cfg.src
	case cfg.replay != "":
		s, err := NewReplaySource(cfg.replay)
		if err != nil {
			r.errf("godevlink: %v\n", err)
			return 2
		}
		r.src = s
	default:
		r.errf("godevlink: no replay capture (set GODEVLINK_REPLAY)\n")
		return 2
	}

	// dl_init: resolve the family once, then open the JSON writer.
	if err := r.initFamily(); err != nil {
		if errors.Is(err, ErrReplay) {
			r.errf("%v\n", err)
			return 2
		}
		r.errf("Failed to connect to devlink Netlink\n")
		return 1
	}
	r.d.newJSONObj()

	err := r.dlCmd()
	r.d.deleteJSONObj()

	switch {
	case err == nil:
		return 0
	case errors.Is(err, ErrReplay):
		r.errf("%v\n", err)
		return 2
	case errors.Is(err, errFail):
		return 1
	default:
		r.errf("%v\n", err)
		return 1
	}
}

// strcmpx is devlink's: arg must be a prefix of pattern (the empty string
// included).
func strcmpx(arg, pattern string) bool {
	return len(arg) <= len(pattern) && pattern[:len(arg)] == arg
}

func (r *runner) argMatch(pattern string) bool {
	return len(r.args) > 0 && strcmpx(r.args[0], pattern)
}

func (r *runner) noArg() bool { return len(r.args) == 0 }

func (r *runner) argInc() {
	if len(r.args) > 0 {
		r.args = r.args[1:]
	}
}

// dlCmd is devlink.c's dl_cmd: the object dispatch.
func (r *runner) dlCmd() error {
	switch {
	case r.argMatch("help") || r.noArg():
		r.help()
		return nil
	case r.argMatch("dev"):
		r.argInc()
		return r.cmdDev()
	case r.argMatch("port"):
		r.argInc()
		return r.cmdPort()
	case r.argMatch("sb"):
		r.argInc()
		return r.cmdSB()
	case r.argMatch("resource"):
		r.argInc()
		return r.cmdResource()
	case r.argMatch("region"):
		r.argInc()
		return r.cmdRegion()
	case r.argMatch("health"):
		r.argInc()
		return r.cmdHealth()
	case r.argMatch("trap"):
		r.argInc()
		return r.cmdTrap()
	}
	r.errf("Object \"%s\" not found\n", r.args[0])
	return errFail
}

func (r *runner) help() {
	r.errf("Usage: devlink [ OPTIONS ] OBJECT { COMMAND | help }\n" +
		"       devlink [ -f[orce] ] -b[atch] filename -N[etns] netnsname\n" +
		"where  OBJECT := { dev | port | lc | sb | monitor | dpipe | resource | region | health | trap }\n" +
		"       OPTIONS := { -V[ersion] | -n[o-nice-names] | -j[son] | -p[retty] | -v[erbose] -s[tatistics] -[he]x }\n")
}

// notImplemented refuses a verb godevlink does not clone.
func (r *runner) notImplemented(what string) error {
	r.errf("godevlink: %s is not implemented\n", what)
	return errFail
}

// --- object dispatch (devlink.c cmd_<object>) ---

func (r *runner) cmdDev() error {
	switch {
	case r.argMatch("help"):
		return nil
	case r.argMatch("show") || r.argMatch("list") || r.noArg():
		r.argInc()
		return r.cmdDevShow()
	case r.argMatch("param"):
		r.argInc()
		return r.cmdDevParam()
	case r.argMatch("info"):
		r.argInc()
		return r.cmdDevInfo()
	}
	return r.notImplemented("dev " + r.firstArg())
}

func (r *runner) cmdDevParam() error {
	if r.argMatch("show") || r.argMatch("list") || r.noArg() {
		r.argInc()
		return r.cmdDevParamShow()
	}
	return r.notImplemented("dev param " + r.firstArg())
}

func (r *runner) cmdPort() error {
	if r.argMatch("show") || r.argMatch("list") || r.noArg() {
		r.argInc()
		return r.cmdPortShow()
	}
	return r.notImplemented("port " + r.firstArg())
}

func (r *runner) cmdSB() error {
	if r.argMatch("show") || r.argMatch("list") || r.noArg() {
		r.argInc()
		return r.cmdSBShow()
	}
	return r.notImplemented("sb " + r.firstArg())
}

func (r *runner) cmdResource() error {
	if r.argMatch("show") || r.noArg() {
		r.argInc()
		return r.cmdResourceShow()
	}
	return r.notImplemented("resource " + r.firstArg())
}

func (r *runner) cmdRegion() error {
	switch {
	case r.noArg():
		return r.cmdRegionShow()
	case r.argMatch("show"):
		r.argInc()
		return r.cmdRegionShow()
	}
	return r.notImplemented("region " + r.firstArg())
}

func (r *runner) cmdHealth() error {
	if r.argMatch("show") || r.argMatch("list") || r.noArg() {
		r.argInc()
		return r.cmdHealthShow()
	}
	return r.notImplemented("health " + r.firstArg())
}

func (r *runner) cmdTrap() error {
	switch {
	case r.argMatch("show") || r.argMatch("list") || r.noArg():
		r.argInc()
		return r.cmdTrapShow()
	case r.argMatch("group"):
		r.argInc()
		return r.cmdTrapGroup()
	}
	return r.notImplemented("trap " + r.firstArg())
}

func (r *runner) cmdTrapGroup() error {
	if r.argMatch("show") || r.argMatch("list") || r.noArg() {
		r.argInc()
		return r.cmdTrapGroupShow()
	}
	return r.notImplemented("trap group " + r.firstArg())
}

func (r *runner) firstArg() string {
	if len(r.args) > 0 {
		return r.args[0]
	}
	return ""
}

// --- the generic netlink layer (devlink.c mnlu_gen_socket_*) ---

// initFamily is dl_init's mnlu_gen_socket_open: the one CTRL_CMD_GETFAMILY
// that resolves "devlink" and caches its id.
func (r *runner) initFamily() error {
	id, err := r.resolveFamily()
	if err != nil {
		return err
	}
	r.familyID = id
	return nil
}

// resolveFamily sends one CTRL_CMD_GETFAMILY and reads the id from the
// CTRL_CMD_NEWFAMILY reply, as mnlu_gen_socket_open does on every socket it
// opens (so `resource show`'s per-dpipe sockets each resolve again).
func (r *runner) resolveFamily() (uint16, error) {
	req := r.buildGetFamily(r.nextSeq())
	rep, err := r.src.Talk(req)
	if err != nil {
		return 0, err
	}
	for _, m := range rep.Msgs {
		if m.Type != xtcpnl.GenlControllerID {
			continue
		}
		f, perr := xtcpnl.ParseGenericNetlinkFamily(m.Body)
		if perr == nil && f.Name == devlinkGenlName {
			return f.ID, nil
		}
	}
	return 0, fmt.Errorf("%w: devlink family not resolved", ErrReplay)
}

// dump sends a DEVLINK_CMD_*_GET dump and returns every reply body of the
// family's type, or the errno its NLMSG_ERROR/NLMSG_DONE carried.
func (r *runner) dump(cmd uint8) ([][]byte, error) {
	req := r.buildCmd(cmd, unix.NLM_F_REQUEST|unix.NLM_F_ACK|unix.NLM_F_DUMP, nil)
	return r.collect(req)
}

// get sends a targeted (non-dump) DEVLINK_CMD_*_GET with the given attrs and
// returns the reply bodies or the errno.
func (r *runner) get(cmd uint8, attrs []byte) ([][]byte, error) {
	req := r.buildCmd(cmd, unix.NLM_F_REQUEST|unix.NLM_F_ACK, attrs)
	return r.collect(req)
}

func (r *runner) collect(req []byte) ([][]byte, error) {
	rep, err := r.src.Talk(req)
	if err != nil {
		return nil, err
	}
	var out [][]byte
	for _, m := range rep.Msgs {
		switch m.Type {
		case unix.NLMSG_DONE, unix.NLMSG_ERROR:
			if e := r.doneErr(m); e != nil {
				return nil, e
			}
		case r.familyID:
			out = append(out, m.Body)
		}
	}
	return out, nil
}

// doneErr turns an NLMSG_ERROR/NLMSG_DONE message into devlink's error, as
// _mnlg_socket_recv_run_intr's "devlink answers: %s" does.
func (r *runner) doneErr(m Msg) error {
	var errno int32
	switch m.Type {
	case unix.NLMSG_ERROR:
		if len(m.Body) < 4 {
			return nil
		}
		errno = int32(binary.LittleEndian.Uint32(m.Body[0:4]))
	case unix.NLMSG_DONE:
		if len(m.Body) < 4 {
			return nil
		}
		errno = int32(binary.LittleEndian.Uint32(m.Body[0:4]))
	}
	if errno == 0 {
		return nil
	}
	if errno < 0 {
		errno = -errno
	}
	// A targeted GET goes through libmnl's mnlu_gen_socket_sndrcv, whose
	// failure prints "kernel answers: %s" (lib/mnl_utils.c).
	r.errf("kernel answers: %s\n", strerror(unix.Errno(errno)))
	return errFail
}

// dpipeConsume is resources_dpipe_tables_init: a fresh socket (so a fresh
// CTRL_CMD_GETFAMILY), one DEVLINK_CMD_DPIPE_TABLE_GET for the handle, and the
// reply. netdevsim registers no dpipe table that maps to a resource, so the
// tables are not rendered — resource_dpipe_tables_show prints "none" — but the
// exchange must still happen for the replay to match.
func (r *runner) dpipeConsume(bus, dev string) error {
	if _, err := r.resolveFamily(); err != nil {
		return err
	}
	attrs := append(strAttr(attrBusName, bus), strAttr(attrDevName, dev)...)
	req := r.buildCmd(cmdDpipeTableGet, unix.NLM_F_REQUEST, attrs)
	_, err := r.src.Talk(req)
	return err
}

// --- request builders (mnlu_gen_socket_cmd_prepare + dl_opts_put) ---

// buildGetFamily is mnlu_gen_socket_open's CTRL_CMD_GETFAMILY: the controller
// family, REQUEST|ACK, genl version 1, one CTRL_ATTR_FAMILY_NAME.
func (r *runner) buildGetFamily(seq uint32) []byte {
	return genlReq(xtcpnl.GenlControllerID, unix.NLM_F_REQUEST|unix.NLM_F_ACK,
		ctrlCmdGetFamily, devlinkGenlVersion, seq, strAttr(2, devlinkGenlName))
}

// buildCmd is mnlu_gen_socket_cmd_prepare for the devlink family.
func (r *runner) buildCmd(cmd uint8, flags uint16, attrs []byte) []byte {
	return genlReq(r.familyID, flags, cmd, devlinkGenlVersion, r.nextSeq(), attrs)
}

// genlReq builds a generic-netlink request: nlmsghdr + genlmsghdr + attrs.
func genlReq(famType, flags uint16, cmd, version uint8, seq uint32, attrs []byte) []byte {
	out := make([]byte, netlinkHeaderLen+4+len(attrs))
	binary.LittleEndian.PutUint32(out[0:4], uint32(len(out)))
	binary.LittleEndian.PutUint16(out[4:6], famType)
	binary.LittleEndian.PutUint16(out[6:8], flags)
	binary.LittleEndian.PutUint32(out[8:12], seq)
	// nlmsg_pid at [12:16] is 0.
	out[16] = cmd
	out[17] = version
	// genlmsghdr reserved at [18:20] is 0.
	copy(out[20:], attrs)
	return out
}

// strAttr builds one NUL-terminated string attribute (mnl_attr_put_strz),
// 4-byte aligned with zero padding.
func strAttr(id uint16, s string) []byte {
	payload := len(s) + 1
	total := 4 + payload
	aligned := (total + 3) &^ 3
	b := make([]byte, aligned)
	binary.LittleEndian.PutUint16(b[0:2], uint16(total))
	binary.LittleEndian.PutUint16(b[2:4], id)
	copy(b[4:], s)
	return b
}

// strerror is glibc's strerror for errno: x/sys/unix's strings, which are
// glibc's with the first letter lowered, raised again.
func strerror(errno unix.Errno) string {
	if errno == 0 {
		return "Success"
	}
	s := errno.Error()
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
