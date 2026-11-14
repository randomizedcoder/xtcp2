// Package govdpa is a read-only Go clone of iproute2's vdpa(8), the version
// the flake pins (7.2.0), built on pkg/xtcpnl's vdpa decoder and request
// builder.
//
// Like goip for ip(8), gordma for rdma(8) and godevlink for devlink(8), it
// exists to make xtcpnl's vdpa coverage something a diff can check:
// replaying vdpa's nlmon (ws) pcap corpus (pkg/xtcpnl/testdata/<kernel>/ws/
// vdpa) must reproduce vdpa's .out, .err and .rc byte for byte. The output
// rules are vdpa's, down to the trailing spaces, the "link " JSON key and the
// queue_type vdpa derives from a MAX_VQP the kernel did not send; the
// comments cite the vdpa/vdpa.c function each piece follows.
//
// Scope is the read-only shows: mgmtdev show (the dump), dev show [DEV], dev
// config show [DEV] and dev vstats show DEV [qidx N]. The writes (dev add,
// del, set) and a targeted mgmtdev show are refused. govdpa never writes.
package govdpa

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// Version is what -V prints: vdpa's, since govdpa clones that release.
const Version = "vdpa utility, iproute2-7.2.0"

// VDPA_GENL_NAME / VDPA_GENL_VERSION (include/uapi/linux/vdpa.h).
const (
	vdpaGenlName    = "vdpa"
	vdpaGenlVersion = 1
)

// errFail is a failure govdpa has already reported to stderr, as vdpa does
// before main returns EXIT_FAILURE.
var errFail = errors.New("govdpa: failed")

// Run is govdpa's main: it returns the exit code. GOVDPA_REPLAY=<file>
// answers requests from a ws pcap capture instead of the kernel.
func Run(args []string, stdout, stderr io.Writer) int {
	return run(config{replay: os.Getenv("GOVDPA_REPLAY")}, args, stdout, stderr)
}

type config struct {
	src    Source // overrides replay and the kernel when set
	replay string
}

// runner is vdpa.c's struct vdpa, cut to what the read paths need.
type runner struct {
	o        *out
	stderr   io.Writer
	src      Source
	args     []string
	familyID uint16
	seq      uint32
}

func (r *runner) errf(format string, a ...any) {
	if _, err := fmt.Fprintf(r.stderr, format, a...); err != nil && r.o.err == nil {
		r.o.err = err
	}
}

// nextSeq stands in for mnlu_msg_prepare's time(NULL); replay ignores it.
func (r *runner) nextSeq() uint32 {
	r.seq++
	return r.seq
}

func run(cfg config, args []string, stdout, stderr io.Writer) int {
	w := bufio.NewWriter(stdout)
	r := &runner{o: &out{w: w}, stderr: stderr}
	rc, done := r.parseOptions(args)
	if !done {
		rc = r.start(cfg)
	}
	if err := w.Flush(); err != nil && r.o.err == nil {
		r.o.err = err
	}
	if r.o.err != nil && rc == 0 {
		rc = 1
	}
	return rc
}

// start opens the source, runs vdpa_init (the one CTRL_CMD_GETFAMILY and
// new_json_obj_plain), vdpa_cmd, then vdpa_fini's delete_json_obj_plain,
// which runs whether or not the command failed.
func (r *runner) start(cfg config) int {
	switch {
	case cfg.src != nil:
		r.src = cfg.src
	case cfg.replay != "":
		s, err := NewReplaySource(cfg.replay)
		if err != nil {
			r.errf("govdpa: %v\n", err)
			return 2
		}
		r.src = s
	default:
		r.errf("govdpa: no replay capture (set GOVDPA_REPLAY)\n")
		return 2
	}

	if err := r.initFamily(); err != nil {
		if errors.Is(err, ErrReplay) {
			r.errf("%v\n", err)
			return 2
		}
		r.errf("Failed to connect to vdpa Netlink\n")
		return 1
	}
	r.o.newJSONObjPlain()
	err := r.vdpaCmd()
	r.o.deleteJSONObjPlain()

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

// matches is lib/utils.c's: arg must be a non-empty prefix of pattern.
func matches(arg, pattern string) bool {
	return arg != "" && strings.HasPrefix(pattern, arg)
}

func (r *runner) argMatches(pattern string) bool {
	return len(r.args) > 0 && matches(r.args[0], pattern)
}

func (r *runner) argIs(s string) bool { return len(r.args) > 0 && r.args[0] == s }

func (r *runner) argInc() {
	if len(r.args) > 0 {
		r.args = r.args[1:]
	}
}

// help is vdpa.c's help.
func (r *runner) help() {
	r.errf("Usage: vdpa [ OPTIONS ] OBJECT { COMMAND | help }\n" +
		"where  OBJECT := { mgmtdev | dev }\n" +
		"       OPTIONS := { -V[ersion] | -n[o-nice-names] | -j[son] | -p[retty] }\n")
}

// notImplemented refuses a verb govdpa does not clone.
func (r *runner) notImplemented(what string) error {
	r.errf("govdpa: %s is not implemented\n", what)
	return errFail
}

// notFound is the "Command \"%s\" not found" every cmd_* ends with.
func (r *runner) notFound() error {
	r.errf("Command \"%s\" not found\n", r.args[0])
	return errFail
}

// vdpaCmd is vdpa.c's vdpa_cmd: the object dispatch.
func (r *runner) vdpaCmd() error {
	switch {
	case len(r.args) == 0 || r.argMatches("help"):
		r.help()
		return nil
	case r.argMatches("mgmtdev"):
		r.argInc()
		return r.cmdMgmtdev()
	case r.argMatches("dev"):
		r.argInc()
		return r.cmdDev()
	}
	r.errf("Object \"%s\" not found\n", r.args[0])
	return errFail
}

// cmdMgmtdev is cmd_mgmtdev.
func (r *runner) cmdMgmtdev() error {
	switch {
	case len(r.args) == 0 || r.argMatches("help"):
		r.errf("Usage: vdpa mgmtdev show [ DEV ]\n")
		return nil
	case r.argMatches("show") || r.argMatches("list"):
		r.argInc()
		if len(r.args) > 0 {
			// cmd_mgmtdev_show with a handle: a targeted MGMTDEV_GET no
			// capture holds.
			return r.notImplemented("mgmtdev show DEV")
		}
		return r.show("mgmtdev", xtcpnl.VdpaMgmtdevKind, "", nil, r.mgmtdevCB)
	}
	return r.notFound()
}

func (r *runner) devHelp() {
	r.errf("Usage: vdpa dev show [ DEV ]\n" +
		"       vdpa dev add name NAME mgmtdev MANAGEMENTDEV [ device_features DEVICE_FEATURES]\n" +
		"                                                    [ mac MACADDR ] [ mtu MTU ]\n" +
		"                                                    [ max_vqp MAX_VQ_PAIRS ]\n" +
		"       vdpa dev del DEV\n" +
		"Usage: vdpa dev config COMMAND [ OPTIONS ]\n" +
		"Usage: vdpa dev vstats COMMAND\n")
}

// cmdDev is cmd_dev. Note vstats and set are strcmp, the rest matches().
func (r *runner) cmdDev() error {
	switch {
	case len(r.args) == 0:
		return r.devShow(xtcpnl.VdpaDevKind, "dev", r.devCB)
	case r.argMatches("help"):
		r.devHelp()
		return nil
	case r.argMatches("show") || r.argMatches("list"):
		r.argInc()
		return r.devShow(xtcpnl.VdpaDevKind, "dev", r.devCB)
	case r.argMatches("add"), r.argMatches("del"):
		return r.notImplemented("dev " + r.args[0])
	case r.argMatches("config"):
		r.argInc()
		return r.cmdDevConfig()
	case r.argIs("vstats"):
		r.argInc()
		return r.cmdDevVstats()
	case r.argIs("set"):
		return r.notImplemented("dev set")
	}
	return r.notFound()
}

// cmdDevConfig is cmd_dev_config.
func (r *runner) cmdDevConfig() error {
	switch {
	case len(r.args) == 0:
		return r.devShow(xtcpnl.VdpaConfigKind, "config", r.configCB)
	case r.argMatches("help"):
		r.errf("Usage: vdpa dev config show [ DEV ]\n")
		return nil
	case r.argMatches("show"):
		r.argInc()
		return r.devShow(xtcpnl.VdpaConfigKind, "config", r.configCB)
	}
	return r.notFound()
}

func (r *runner) vstatsHelp() {
	r.errf("Usage: vdpa dev vstats show DEV [qidx QUEUE_INDEX]\n")
}

// cmdDevVstats is cmd_dev_vstats: strcmp, not matches().
func (r *runner) cmdDevVstats() error {
	switch {
	case len(r.args) == 0:
		r.vstatsHelp()
		return errFail
	case r.argIs("help"):
		r.vstatsHelp()
		return nil
	case r.argIs("show"):
		r.argInc()
		return r.cmdDevVstatsShow()
	}
	return r.notFound()
}

// cmdDevVstatsShow is cmd_dev_vstats_show: exactly DEV or DEV qidx N, then
// vdpa_argv_parse with the handle required and qidx optional.
func (r *runner) cmdDevVstatsShow() error {
	if len(r.args) != 1 && len(r.args) != 3 {
		r.vstatsHelp()
		return errFail
	}
	dev, qidx, err := r.argvParse(true)
	if err != nil {
		return err
	}
	return r.show("vstats", xtcpnl.VdpaVstatsKind, dev, qidx, r.vstatsCB)
}

// devShow is cmd_dev_show / cmd_dev_config_show: a dump with no arguments,
// otherwise a GET of the handle vdpa_argv_parse takes.
func (r *runner) devShow(kind xtcpnl.VdpaKind, section string, cb dataCB) error {
	dev := ""
	if len(r.args) > 0 {
		var err error
		if dev, _, err = r.argvParse(false); err != nil {
			return err
		}
	}
	return r.show(section, kind, dev, nil, cb)
}

// argvParse is vdpa_argv_parse with VDPA_OPT_VDEV_HANDLE required and, when
// qidxOK, VDPA_OPT_QUEUE_INDEX optional. Its loop runs while NEXT_ARG_OK()
// (argc - 1 > 0), so a lone trailing word after the handle is never looked
// at. The only option either show accepts is qidx, so any other word the
// loop reaches is "Unknown option".
func (r *runner) argvParse(qidxOK bool) (dev string, qidx *uint32, err error) {
	args := r.args
	dev = args[0]
	args = args[1:]
	for len(args) > 1 {
		if !qidxOK || args[0] != "qidx" {
			r.errf("Unknown option \"%s\"\n", args[0])
			return "", nil, errFail
		}
		v, ok := getU32(args[1])
		if !ok {
			// get_u32 fails silently; vdpa_cmd's error is rc 1 alone.
			return "", nil, errFail
		}
		qidx = &v
		args = args[2:]
	}
	if dev == "" {
		// vdpa would send DEV_NAME "" without NLM_F_DUMP; the builder reads
		// an empty name as the dump, and no capture holds it.
		return "", nil, r.notImplemented("an empty DEV")
	}
	return dev, qidx, nil
}

// getU32 is lib/utils.c's get_u32 at base 10 over strtoul: leading space and
// a sign are accepted, a negative value wraps (so only -0 survives the
// 32-bit bound), and the whole word must be digits.
func getU32(s string) (uint32, bool) {
	i := 0
	for i < len(s) && (s[i] == ' ' || (s[i] >= '\t' && s[i] <= '\r')) {
		i++
	}
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	if i == len(s) {
		return 0, false
	}
	var v uint64
	for ; i < len(s); i++ {
		c := s[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		if v > (^uint64(0)-uint64(c-'0'))/10 {
			return 0, false // strtoul's ULONG_MAX/ERANGE
		}
		v = v*10 + uint64(c-'0')
	}
	if neg {
		v = -v
	}
	if v > 0xFFFFFFFF {
		return 0, false
	}
	return uint32(v), true
}

// show is the shape every cmd_*_show shares: prepare the request,
// pr_out_section_start, mnlu_gen_socket_sndrcv with the callback, then
// pr_out_section_end, which runs even when the exchange failed.
func (r *runner) show(section string, kind xtcpnl.VdpaKind, dev string, qidx *uint32, cb dataCB) error {
	req, err := xtcpnl.BuildGetVdpaRequest(r.familyID, kind, dev, qidx, r.nextSeq())
	if err != nil {
		return r.notImplemented(fmt.Sprintf("this request (%v)", err))
	}
	r.o.openJSONObject(nil)
	r.o.openJSONObject(&section)
	err = r.sndrcv(req, cb)
	r.o.closeJSONObject()
	r.o.closeJSONObject()
	return err
}

// --- the generic netlink layer (lib/mnl_utils.c) ---

// dataCB is a libmnl data callback: false is MNL_CB_ERROR.
type dataCB func(m Msg) bool

// initFamily is vdpa_init's mnlu_gen_socket_open: family_get's one
// CTRL_CMD_GETFAMILY (genl version 1, REQUEST|ACK), its reply read by
// get_family_cb. A failure here is reported by vdpa_init alone ("Failed to
// connect"), with no "kernel answers" line, though an extack still prints.
func (r *runner) initFamily() error {
	req, err := xtcpnl.BuildGetFamilyRequestVersion(vdpaGenlName, vdpaGenlVersion, r.nextSeq())
	if err != nil {
		return err
	}
	binary.LittleEndian.PutUint16(req[6:8], unix.NLM_F_REQUEST|unix.NLM_F_ACK)
	rep, err := r.src.Talk(req)
	if err != nil {
		return err
	}
	var id uint16
	if err := r.runReply(rep, func(m Msg) bool {
		f, perr := xtcpnl.ParseGenericNetlinkFamily(m.Body)
		if perr != nil || f.ID == 0 {
			return false // get_family_cb: no CTRL_ATTR_FAMILY_ID
		}
		id = f.ID
		return true
	}); err != nil {
		return err
	}
	r.familyID = id
	return nil
}

// sndrcv is mnlu_gen_socket_sndrcv: send, run the reply through
// mnlu_socket_recv_run, and on failure print "kernel answers: %s".
func (r *runner) sndrcv(req []byte, cb dataCB) error {
	rep, err := r.src.Talk(req)
	if err != nil {
		return err
	}
	err = r.runReply(rep, cb)
	var ce *cbError
	if !errors.As(err, &ce) {
		return err
	}
	r.errf("kernel answers: %s\n", strerror(ce.errno))
	return errFail
}

// cbError is a reply mnl_cb_run2 ended with MNL_CB_ERROR, and the errno
// vdpa's strerror then reads.
type cbError struct{ errno unix.Errno }

func (e *cbError) Error() string { return "govdpa: MNL_CB_ERROR: " + strerror(e.errno) }

// runReply is mnl_cb_run2 over a reply with mnlu_utils' control callbacks:
// data messages go to cb, NLMSG_ERROR and NLMSG_DONE end it (printing an
// extack the way nl_dump_ext_ack does), NOOP and OVERRUN are skipped. It
// returns a *cbError on failure.
func (r *runner) runReply(rep Reply, cb dataCB) error {
	for _, m := range rep.Msgs {
		if m.Flags&unix.NLM_F_DUMP_INTR != 0 {
			return &cbError{unix.EINTR}
		}
		if m.Type >= unix.NLMSG_MIN_TYPE {
			if cb != nil && !cb(m) {
				// A callback's MNL_CB_ERROR leaves errno as it was: 0 here.
				return &cbError{0}
			}
			continue
		}
		switch m.Type {
		case unix.NLMSG_ERROR:
			if errno := r.cbErr(m); errno != 0 {
				return &cbError{errno}
			}
			return nil
		case unix.NLMSG_DONE:
			if errno := r.cbStop(m); errno != 0 {
				return &cbError{errno}
			}
			return nil
		}
	}
	return fmt.Errorf("%w: the reply ends without NLMSG_DONE or NLMSG_ERROR", ErrReplay)
}

// cbErr is mnlu_cb_error: either way the reply ends. A payload shorter
// than struct nlmsgerr, or a zero error (after any warning extack), is
// MNL_CB_STOP; otherwise the errno, whichever sign the kernel gave it.
func (r *runner) cbErr(m Msg) unix.Errno {
	if len(m.Body) < 4+netlinkHeaderLen {
		return 0
	}
	e := int64(int32(binary.LittleEndian.Uint32(m.Body[0:4])))
	if e < 0 {
		e = -e
	}
	r.dumpExtAck(m, e != 0)
	return unix.Errno(e)
}

// cbStop is mnlu_cb_stop: NLMSG_DONE's int is the dump's result; a negative
// one is an error, whose extack (past the int) nl_dump_ext_ack_done prints.
func (r *runner) cbStop(m Msg) unix.Errno {
	if len(m.Body) < 4 {
		return 0
	}
	l := int32(binary.LittleEndian.Uint32(m.Body[0:4]))
	if l >= 0 {
		return 0
	}
	r.extAckAttrs(m.Body[4:], true)
	return unix.Errno(-int64(l))
}

// dumpExtAck is nl_dump_ext_ack with no errfn: only with NLM_F_ACK_TLVS, the
// TLVs past struct nlmsgerr (and past the echoed request unless
// NLM_F_CAPPED).
func (r *runner) dumpExtAck(m Msg, isErr bool) {
	if m.Flags&unix.NLM_F_ACK_TLVS == 0 {
		return
	}
	hlen := 4 + netlinkHeaderLen
	if m.Flags&unix.NLM_F_CAPPED == 0 {
		inner := int(binary.LittleEndian.Uint32(m.Body[4:8]))
		hlen += max(inner-netlinkHeaderLen, 0)
	}
	if hlen > len(m.Body) {
		return
	}
	r.extAckAttrs(m.Body[hlen:], isErr)
}

// NLMSGERR_ATTR_* (include/uapi/linux/netlink.h).
const (
	nlmsgerrAttrMsg      = 1
	nlmsgerrAttrOffs     = 2
	nlmsgerrAttrMissType = 5
	nlmsgerrAttrMax      = 6
)

// extAckAttrs parses extack TLVs as mnl_attr_parse with err_attr_cb does and
// prints what nl_dump_ext_ack prints: the missing-attribute line, then
// "Error: msg" (or "Warning: ") with a "." added when msg lacks one.
func (r *runner) extAckAttrs(b []byte, isErr bool) {
	var msg *string
	var missType *uint32
	for len(b) >= 4 {
		l := int(binary.LittleEndian.Uint16(b[0:2]))
		if l < 4 || l > len(b) {
			break // mnl_attr_ok ends the walk
		}
		typ := binary.LittleEndian.Uint16(b[2:4]) & 0x3fff
		p := b[4:l]
		if typ > nlmsgerrAttrMax {
			r.errf("Invalid extack attribute\n")
			return
		}
		switch typ {
		case nlmsgerrAttrMsg:
			if len(p) == 0 || p[len(p)-1] != 0 {
				r.errf("extack attribute %d failed validation\n", typ)
				return
			}
			s := string(p[:bytes.IndexByte(p, 0)]) // mnl_attr_get_str
			msg = &s
		case nlmsgerrAttrOffs:
			if len(p) != 4 {
				r.errf("extack attribute %d failed validation\n", typ)
				return
			}
		case nlmsgerrAttrMissType:
			if len(p) >= 4 {
				v := binary.LittleEndian.Uint32(p[0:4])
				missType = &v
			}
		}
		b = b[min(len(b), (l+3)&^3):]
	}
	if missType != nil {
		r.errf("Missing required attribute type %d\n", *missType)
	}
	if msg == nil || *msg == "" {
		return
	}
	label := "Warning"
	if isErr {
		label = "Error"
	}
	dot := ""
	if !strings.HasSuffix(*msg, ".") {
		dot = "."
	}
	r.errf("%s: %s%s\n", label, *msg, dot)
}

// strerror is glibc's strerror: x/sys/unix's strings, which are glibc's
// with the first letter lowered, raised again.
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
