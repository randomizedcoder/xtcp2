// Package gofou is a read-only Go clone of iproute2's `ip fou` (foo-over-UDP)
// show path, the version the flake pins (7.2.0), built on pkg/xtcpnl's fou
// decoder and request builder.
//
// Like goip for ip(8), gordma for rdma(8), godevlink for devlink(8) and
// govdpa for vdpa(8), it exists to make xtcpnl's fou coverage something a
// diff can check: replaying `ip fou`'s nlmon (ws) pcap corpus
// (pkg/xtcpnl/testdata/<kernel>/ws/fou) must reproduce `ip fou`'s .out, .err
// and .rc byte for byte. The output rules are ip/ipfou.c's; the comments
// cite the function each piece follows.
//
// Scope is `fou show` (the dump) and its refusals: `help` (refused before
// the family is resolved), a stray argument ("does not take any arguments")
// and an unknown sub-command. The writes (`fou add`, `fou del`) are refused;
// gofou never writes.
package gofou

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// FOU_GENL_NAME / FOU_GENL_VERSION (include/uapi/linux/fou.h). genl_rth
// resolves the family with genl version 0 (libnetlink's GENL_REQUEST _ver),
// so initFamily passes 0; the fou request itself uses version 1.
const fouGenlName = "fou"

// genlIDCtrl is GENL_ID_CTRL, the controller's fixed message type.
const genlIDCtrl = 0x10

// errFail is a failure gofou has already reported to stderr, as ip does
// before a command returns a non-zero code.
var errFail = errors.New("gofou: failed")

// Run is gofou's main: it returns the exit code. GOFOU_REPLAY=<file> answers
// requests from a ws pcap capture instead of the kernel.
func Run(args []string, stdout, stderr io.Writer) int {
	return run(config{replay: os.Getenv("GOFOU_REPLAY")}, args, stdout, stderr)
}

type config struct {
	src    Source // overrides replay and the kernel when set
	replay string
}

// runner is ip's state cut to what `ip fou`'s read path needs.
type runner struct {
	o        *out
	stderr   io.Writer
	src      Source
	familyID uint16
	seq      uint32
}

func (r *runner) errf(format string, a ...any) {
	if _, err := fmt.Fprintf(r.stderr, format, a...); err != nil && r.o.err == nil {
		r.o.err = err
	}
}

// nextSeq stands in for libnetlink's seq (time-based); replay ignores it.
func (r *runner) nextSeq() uint32 {
	r.seq++
	return r.seq
}

func run(cfg config, args []string, stdout, stderr io.Writer) int {
	w := bufio.NewWriter(stdout)
	r := &runner{o: &out{w: w}, stderr: stderr}
	rc := r.dispatch(cfg, args)
	if err := w.Flush(); err != nil && r.o.err == nil {
		r.o.err = err
	}
	if r.o.err != nil && rc == 0 {
		rc = 1
	}
	return rc
}

// matches is lib/utils.c's: arg must be a non-empty prefix of pattern.
func matches(arg, pattern string) bool {
	return arg != "" && strings.HasPrefix(pattern, arg)
}

// dispatch is ip's top-level: it eats the leading options (ip reads -j/-p
// before the object), then the "fou" object, then do_ipfou.
func (r *runner) dispatch(cfg config, args []string) int {
	for len(args) > 0 && len(args[0]) > 1 && args[0][0] == '-' {
		opt := args[0]
		switch {
		case matches(opt, "-json"):
			r.o.json = true
		case matches(opt, "-pretty"):
			r.o.pretty = true
		default:
			// ip would getopt-reject it; no corpus row has one.
			r.errf("Option \"%s\" is unknown, try \"ip -help\".\n", opt)
			return 255
		}
		args = args[1:]
	}
	if len(args) == 0 || !matches(args[0], "fou") {
		r.errf("Object \"%s\" is unknown, try \"ip help\".\n", first(args))
		return 255
	}
	return r.doIPFou(cfg, args[1:])
}

func first(args []string) string {
	if len(args) == 0 {
		return ""
	}
	return args[0]
}

// usage is ipfou.c's usage(): to stderr, then exit(-1) (255).
func (r *runner) usage() int {
	r.errf("Usage: ip fou add port PORT { ipproto PROTO  | gue }\n" +
		"		   [ local IFADDR ] [ peer IFADDR ]\n" +
		"		   [ peer_port PORT ] [ dev IFNAME ]\n" +
		"       ip fou del port PORT [ local IFADDR ]\n" +
		"		   [ peer IFADDR ] [ peer_port PORT ]\n" +
		"		   [ dev IFNAME ]\n" +
		"       ip fou show\n" +
		"\n" +
		"Where: PROTO { ipproto-name | 1..255 }\n" +
		"       PORT { 1..65535 }\n" +
		"       IFADDR { addr }\n")
	return 255
}

// doIPFou is ipfou.c's do_ipfou: help is refused before the family is
// resolved; add/delete/show after. An unknown sub-command exits 255.
func (r *runner) doIPFou(cfg config, args []string) int {
	if len(args) < 1 {
		return r.usage()
	}
	if matches(args[0], "help") {
		return r.usage()
	}
	if rc, ok := r.open(cfg); !ok {
		return rc
	}
	switch {
	case matches(args[0], "add"), matches(args[0], "delete"):
		// gofou is read-only; the corpus captures no write.
		r.errf("gofou: \"fou %s\" is a write, not cloned\n", args[0])
		return 1
	case matches(args[0], "show"):
		return r.doShow(args[1:])
	}
	r.errf("Command \"%s\" is unknown, try \"ip fou help\".\n", args[0])
	return 255
}

// open sets the source and runs genl_init_handle (the one CTRL_CMD_GETFAMILY
// that resolves the fou family). ok is false and rc set on failure.
func (r *runner) open(cfg config) (int, bool) {
	switch {
	case cfg.src != nil:
		r.src = cfg.src
	case cfg.replay != "":
		s, err := NewReplaySource(cfg.replay)
		if err != nil {
			r.errf("gofou: %v\n", err)
			return 2, false
		}
		r.src = s
	default:
		r.errf("gofou: no replay capture (set GOFOU_REPLAY)\n")
		return 2, false
	}
	if err := r.initFamily(); err != nil {
		if errors.Is(err, ErrReplay) {
			r.errf("%v\n", err)
			return 2, false
		}
		r.errf("Error: Unable to initialize genl handle\n")
		return 1, false
	}
	return 0, true
}

// initFamily is genl_init_handle/genl_resolve_family: a CTRL_CMD_GETFAMILY
// for "fou" (genl version 0, NLM_F_REQUEST only, no ACK), whose NEWFAMILY
// answer carries CTRL_ATTR_FAMILY_ID.
func (r *runner) initFamily() error {
	req, err := xtcpnl.BuildGetFamilyRequestVersion(fouGenlName, 0, r.nextSeq())
	if err != nil {
		return err
	}
	rep, err := r.src.Talk(req)
	if err != nil {
		return err
	}
	for _, m := range rep.Msgs {
		if m.Type != genlIDCtrl {
			continue
		}
		f, perr := xtcpnl.ParseGenericNetlinkFamily(m.Body)
		if perr == nil && f.ID != 0 && f.Name == fouGenlName {
			r.familyID = f.ID
			return nil
		}
	}
	return errFail
}

// doShow is ipfou.c's do_show: a stray argument is refused before the wire;
// otherwise it sends the FOU_CMD_GET dump, wraps the replies in the JSON
// array new_json_obj opens, and prints each fou mapping.
func (r *runner) doShow(args []string) int {
	if len(args) > 0 {
		r.errf("\"ip fou show\" does not take any arguments.\n")
		return 1
	}
	req, err := xtcpnl.BuildGetFOURequest(r.familyID, r.nextSeq())
	if err != nil {
		r.errf("gofou: %v\n", err)
		return 1
	}
	rep, err := r.src.Talk(req)
	if err != nil {
		if errors.Is(err, ErrReplay) {
			r.errf("%v\n", err)
			return 2
		}
		r.errf("Cannot send show request: %v\n", err)
		return 1
	}
	r.o.newJSONObj()
	for _, m := range rep.Msgs {
		switch m.Type {
		case unix.NLMSG_DONE:
			r.o.deleteJSONObj()
			return 0
		case unix.NLMSG_ERROR:
			r.errf("Dump terminated\n")
			r.o.deleteJSONObj()
			return 1
		case r.familyID:
			r.printFouMapping(m.Body)
		}
		// print_fou_mapping returns 0 (skips) for any other type.
	}
	// A dump with no NLMSG_DONE: rtnl_dump_filter would keep reading; here
	// the capture ended, so close what we opened.
	r.o.deleteJSONObj()
	return 0
}
