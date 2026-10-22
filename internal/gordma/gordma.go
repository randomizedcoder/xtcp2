// Package gordma is a read-only Go clone of iproute2's rdma(8), the
// version the flake pins (7.2.0), built on pkg/xtcpnl's nldev decoders.
// rdma's 7.1.0 -> 7.2.0 changes are all in `dev set netns`, which gordma
// refuses, so the read paths are the same in both.
//
// Like goip for ip(8), it exists to make xtcpnl's RDMA coverage something
// a diff can check: replaying rdma's strace corpus
// (pkg/xtcpnl/testdata/<kernel>/rdma) must reproduce rdma's requests byte
// for byte and its .out, .err and .rc sidecars. The output rules are
// rdma's, down to the trailing spaces; the comments cite the rdma/*.c
// function each piece follows.
//
// gordma never writes: every request is built by an xtcpnl builder, and
// xtcpnl admits only the read-only nldev commands. rdma's write verbs, the
// _RAW resource dumps (-r), driver details (-dd), batch files (-b),
// frmr_pools and the per-QP and per-MR statistics are refused with rc 1.
//
// The one value that does not come from the wire is a user resource's
// comm, which rdma reads from /proc/<pid>/comm: GORDMA_PROC names another
// proc root, so a replay can supply the capture's.
package gordma

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// Version is what -V prints: rdma's, since gordma clones that release.
const Version = "rdma utility, iproute2-7.2.0"

// toolName is rdma's basename(argv[0]) in its help texts.
const toolName = "rdma"

// Run is gordma's main: it returns the exit code. GORDMA_REPLAY=<file>
// answers requests from an strace capture instead of the kernel, and
// GORDMA_PROC is the proc root comm is read from.
func Run(args []string, stdout, stderr io.Writer) int {
	return run(config{replay: os.Getenv("GORDMA_REPLAY"), proc: os.Getenv("GORDMA_PROC")}, args, stdout, stderr)
}

type config struct {
	src    Source // overrides replay and the kernel when set
	replay string
	proc   string
}

// devMap is rdma's struct dev_map: one device of the GET dump rd_init
// sends first.
type devMap struct {
	name     string
	idx      uint32
	numPorts uint32
}

// rd is rdma's struct rd, the state every command runs on.
type rd struct {
	args           []string
	p              *printer
	stderr         io.Writer
	src            Source
	procRoot       string
	showDetails    bool
	showRaw        bool
	devs           []devMap
	devIdx         uint32
	portIdx        uint32
	filters        []filterEntry
	suppressErrors bool
}

func (r *rd) errf(format string, a ...any) {
	if _, err := fmt.Fprintf(r.stderr, format, a...); err != nil && r.p.err == nil {
		r.p.err = err
	}
}

// seq is rd_prepare_msg's time(NULL).
func seq() uint32 { return uint32(time.Now().Unix()) } //nolint:gosec // rdma's truncation

func run(cfg config, args []string, stdout, stderr io.Writer) int {
	w := bufio.NewWriter(stdout)
	r := &rd{p: &printer{w: w, sl: "\n"}, stderr: stderr, procRoot: "/proc"}
	if cfg.proc != "" {
		r.procRoot = cfg.proc
	}
	rc, done := r.parseOptions(args)
	if !done {
		rc = r.start(cfg)
	}
	if err := w.Flush(); err != nil && r.p.err == nil {
		r.p.err = err
	}
	if r.p.err != nil && rc == 0 {
		rc = 1
	}
	return rc
}

// start opens the source, runs rd_init and then the command.
func (r *rd) start(cfg config) int {
	switch {
	case cfg.src != nil:
		r.src = cfg.src
	case cfg.replay != "":
		s, err := NewReplaySource(cfg.replay)
		if err != nil {
			r.errf("gordma: %v\n", err)
			return 2
		}
		r.src = s
	default:
		r.src = LiveSource{}
	}
	err := r.init()
	if err == nil {
		err = r.execCmd(cmdHelp, []command{
			{"help", cmdHelp},
			{"dev", cmdDev},
			{"link", cmdLink},
			{"resource", cmdRes},
			{"system", cmdSys},
			{"statistic", cmdStat},
			{"monitor", cmdMon},
		}, "object")
	}
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

// longOption is one of main's long_options.
type longOption struct {
	name   string
	short  byte
	hasArg bool
}

var longOptions = []longOption{
	{"version", 'V', false}, {"help", 'h', false}, {"json", 'j', false},
	{"oneline", 'o', false}, {"pretty", 'p', false}, {"details", 'd', false},
	{"raw", 'r', false}, {"force", 'f', false}, {"batch", 'b', true},
}

// shortOptions is main's getopt_long string, its leading ':' dropped.
const shortOptions = "Vhdropjfb:"

// parseOptions is main's getopt_long loop, with GNU permutation: options
// may follow the object, and "--" ends them. It leaves the operands in
// r.args, and returns done when main would return before rd_init.
func (r *rd) parseOptions(args []string) (rc int, done bool) {
	var operands []string
	details, batch := 0, false
	opt := func(c byte) (int, bool) {
		switch c {
		case 'V':
			r.p.raw(Version + "\n")
			return 0, true
		case 'h':
			r.help()
			return 0, true
		case 'p':
			r.p.pretty = true
		case 'd':
			details++
		case 'r':
			r.showRaw = true
		case 'o':
			r.p.sl = `\`
		case 'j':
			r.p.json = true
		case 'f':
		case 'b':
			batch = true
		case ':':
			r.errf("-b option requires an argument\n")
			return 1, true
		default:
			r.errf("Unknown option.\n")
			r.help()
			return 1, true
		}
		return 0, false
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			operands = append(operands, args[i+1:]...)
			i = len(args)
		case strings.HasPrefix(a, "--"):
			c, takesNext := longOpt(a[2:], i+1 < len(args))
			if takesNext {
				i++
			}
			if rc, done := opt(c); done {
				return rc, true
			}
		case len(a) > 1 && a[0] == '-':
			for j := 1; j < len(a); j++ {
				c := a[j]
				k := strings.IndexByte(shortOptions, c)
				if k < 0 || c == ':' {
					c = '?'
				} else if k+1 < len(shortOptions) && shortOptions[k+1] == ':' {
					// An argument: the rest of the cluster, or the next word.
					if j+1 == len(a) {
						if i+1 == len(args) {
							c = ':'
						} else {
							i++
						}
					}
					j = len(a)
				}
				if rc, done := opt(c); done {
					return rc, true
				}
			}
		default:
			operands = append(operands, a)
		}
	}
	if batch {
		r.errf("gordma: -b (batch) is not implemented\n")
		return 1, true
	}
	r.showDetails = details > 0
	if details > 1 {
		r.errf("gordma: -dd (driver details) is not implemented\n")
		return 1, true
	}
	r.args = operands
	return 0, false
}

// longOpt resolves a long option as getopt_long does: an exact name, or a
// prefix of only one. It returns the option's character, '?' for an
// unknown, ambiguous or wrongly valued one, ':' for a missing argument,
// and whether the argument is the next word.
func longOpt(s string, haveNext bool) (byte, bool) {
	name, _, hasValue := strings.Cut(s, "=")
	var match *longOption
	for i := range longOptions {
		o := &longOptions[i]
		if o.name == name {
			match = o
			break
		}
		if strings.HasPrefix(o.name, name) && name != "" {
			if match != nil {
				return '?', false
			}
			match = o
		}
	}
	switch {
	case match == nil:
		return '?', false
	case !match.hasArg && hasValue:
		return '?', false
	case match.hasArg && !hasValue && !haveNext:
		return ':', false
	}
	return match.short, match.hasArg && !hasValue
}

// help is rdma.c's help.
func (r *rd) help() {
	r.p.raw(fmt.Sprintf("Usage: %[1]s [ OPTIONS ] OBJECT { COMMAND | help }\n"+
		"       %[1]s [ -f[orce] ] -b[atch] filename\n"+
		"where  OBJECT := { dev | link | resource | monitor | system | statistic | help }\n"+
		"       OPTIONS := { -V[ersion] | -d[etails] | -j[son] | -p[retty] | -r[aw]}\n", toolName))
}

func cmdHelp(r *rd) error {
	r.help()
	return nil
}

// usage returns a command that prints lines, each a format of toolName.
func usage(lines ...string) func(*rd) error {
	return func(r *rd) error {
		for _, l := range lines {
			if strings.Contains(l, "%s") {
				l = fmt.Sprintf(l, toolName)
			}
			r.p.raw(l + "\n")
		}
		return nil
	}
}

// notImplemented is every verb rdma has that gordma does not: the writes,
// and the reads xtcpnl has no builder for.
func notImplemented(what string) func(*rd) error {
	return func(r *rd) error {
		r.errf("gordma: %s is not implemented\n", what)
		return errFail
	}
}

// command is struct rd_cmd.
type command struct {
	name string
	fn   func(*rd) error
}

// strcmpx is rdma's: s matches name when s is a prefix of it, the empty
// string included.
func strcmpx(s, name string) bool { return strings.HasPrefix(name, s) }

// execCmd is rd_exec_cmd: def with no argument, else the first command the
// argument is a prefix of. An unknown one is reported and is not an error.
func (r *rd) execCmd(def func(*rd) error, cmds []command, what string) error {
	if len(r.args) == 0 {
		return def(r)
	}
	for _, c := range cmds {
		if strcmpx(r.args[0], c.name) {
			r.args = r.args[1:]
			return c.fn(r)
		}
	}
	r.errf("Unknown %s '%s'.\n", what, r.args[0])
	return nil
}

// init is rd_init: the GET dump that maps names to indexes and port counts.
func (r *rd) init() error {
	return r.talk(xtcpnl.BuildRdmaDevDumpRequest(seq()), func(m Msg) error {
		d, err := xtcpnl.ParseRdmaDev(m.Body)
		if err != nil || !d.HasDevName || !d.HasDevIndex {
			return errCB
		}
		if !d.HasNumPorts {
			r.errf("This tool doesn't support switches yet\n")
			return errCB
		}
		r.devs = append(r.devs, devMap{name: d.DevName, idx: d.DevIndex, numPorts: d.NumPorts})
		return nil
	})
}

// talk is rd_send_msg and rd_recv_msg.
func (r *rd) talk(req []byte, cb func(Msg) error) error {
	rep, err := r.src.Talk(req)
	switch {
	case errors.Is(err, ErrSocket):
		r.errf("Failed to open NETLINK_RDMA socket\n")
		return errFail
	case errors.Is(err, ErrSend):
		r.errf("Failed to send to socket with err %d\n", -1)
		return errFail
	case err != nil:
		return err
	}
	return r.recvMsg(rep, cb)
}

// lookup is dev_map_lookup: the device the argument names, its "/PORT"
// cut first when allowPort.
func (r *rd) lookup(allowPort bool) *devMap {
	if len(r.args) == 0 {
		return nil
	}
	name := r.args[0]
	if allowPort {
		if i := strings.LastIndexByte(name, '/'); i >= 0 {
			name = name[:i]
		}
	}
	for i := range r.devs {
		if r.devs[i].name == name {
			return &r.devs[i]
		}
	}
	return nil
}

// cAtoi is atoi of s's leading digits: strtol's saturation, then int's
// truncation, as uint32.
func cAtoi(s string) uint32 {
	var v uint64
	for i := 0; i < len(s) && s[i] >= '0' && s[i] <= '9'; i++ {
		v = v*10 + uint64(s[i]-'0')
		if v > 1<<63-1 {
			v = 1<<63 - 1
		}
	}
	return uint32(v) //nolint:gosec // atoi's truncation
}

// portFromArg is get_port_from_argv: arg's port, whether to dump every
// port from it, and whether arg is malformed.
func portFromArg(arg string, strict bool) (port uint32, dumpAll, bad bool) {
	dumpAll = !strict
	_, rest, ok := strings.Cut(arg, "/")
	if !ok {
		return 0, dumpAll, strict
	}
	if strings.HasPrefix(rest, "-") {
		if strict {
			return 0, dumpAll, true
		}
		return 0, false, false
	}
	if rest != "" && rest[0] >= '0' && rest[0] <= '9' {
		dumpAll = false
		port = cAtoi(rest)
	}
	if port == 0 && rest != "" {
		return 0, dumpAll, true
	}
	if strict && port == 0 {
		return 0, dumpAll, true
	}
	return port, dumpAll, false
}

// execLink is rd_exec_link: cb per port of every device, from port 1 when
// strict and 0 when not, or of the DEV[/PORT] argument.
func (r *rd) execLink(cb func(*rd) error, strict bool) error {
	r.p.newJSONObj()
	err := r.execLinkPorts(cb, strict)
	r.p.deleteJSONObj()
	return err
}

func (r *rd) execLinkPorts(cb func(*rd) error, strict bool) error {
	if len(r.args) == 0 {
		for _, d := range r.devs {
			r.devIdx = d.idx
			port := uint32(0)
			if strict {
				port = 1
			}
			for ; port < d.numPorts+1; port++ {
				r.portIdx = port
				if err := cb(r); err != nil {
					return err
				}
			}
		}
		return nil
	}
	d := r.lookup(true)
	port, dumpAll, bad := portFromArg(r.args[0], strict)
	if d == nil || port > d.numPorts || (port == 0 && bad) {
		r.errf("Wrong device name\n")
		return errFail
	}
	r.args = r.args[1:]
	r.devIdx = d.idx
	for r.portIdx = port; r.portIdx < d.numPorts+1; r.portIdx++ {
		if err := cb(r); err != nil {
			return err
		}
		if !dumpAll {
			break
		}
	}
	return nil
}

// execDev is rd_exec_dev: cb per device, or for the DEV argument.
func (r *rd) execDev(cb func(*rd) error) error {
	r.p.newJSONObj()
	err := r.execDevs(cb)
	r.p.deleteJSONObj()
	return err
}

func (r *rd) execDevs(cb func(*rd) error) error {
	if len(r.args) == 0 {
		for _, d := range r.devs {
			r.devIdx = d.idx
			if err := cb(r); err != nil {
				return err
			}
		}
		return nil
	}
	d := r.lookup(false)
	if d == nil {
		r.errf("Wrong device name - %s\n", r.args[0])
		return errFail
	}
	r.args = r.args[1:]
	r.devIdx = d.idx
	return cb(r)
}
