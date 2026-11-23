// Package gobridge is a read-only Go clone of iproute2's bridge(8), the
// version the flake pins (7.2.0), for its link, vlan, vni and mst shows,
// built on pkg/xtcpnl's AF_BRIDGE link, vnifilter and vlan decoders.
//
// Like gotipc and govdpa, it exists to make xtcpnl's coverage something a
// diff can check: replaying bridge's nlmon (ws) pcap corpus
// (pkg/xtcpnl/testdata/<kernel>/ws/bridge) must reproduce bridge's .out,
// .err and .rc byte for byte. The output rules are bridge's, down to what
// it gets wrong (docs/netlink/gobridge.md, B1-B27): a port named `master`
// cannot be filtered, unknown words are ignored, `vid` goes through atoi,
// the mst dump stops at the first port without IFLA_AF_SPEC, and the
// headers are printed before the dump. The comments cite the bridge/*.c
// function each piece follows.
//
// Scope is the read-only shows: link, vlan (show, -d, -s, -com,
// tunnelshow, global show), vni and mst. Every write (link set, vlan
// add/del/set, vlan global set, vni add/del, mst set), fdb, mdb and
// monitor are refused. gobridge never writes.
package gobridge

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
)

// exitCode is bridge leaving with exit(code), or main returning it; -1
// is 255.
type exitCode int

func (e exitCode) Error() string { return "exit " + strconv.Itoa(int(e)) }

// errNotImplemented is a command gobridge does not clone (a write, fdb,
// mdb, monitor).
var errNotImplemented = errors.New("gobridge: not implemented")

// runner is bridge's globals: the output, the option counters, the
// socket's sequence, the link cache and the source.
type runner struct {
	o        *out
	stderr   io.Writer
	errOut   error
	src      Source
	seq      uint32
	stats    bool
	details  bool
	compress bool
	cache    llCache

	// The per-object filters (each bridge/*.c has its own statics; one
	// command runs one object).
	filterIndex  uint32
	filterVlan   int32
	filterMaster uint32
	vlanCurIdx   int64 // vlan.c's vlan_rtm_cur_ifidx; -1 for none
}

func (r *runner) errf(format string, a ...any) {
	if _, err := fmt.Fprintf(r.stderr, format, a...); err != nil && r.errOut == nil {
		r.errOut = err
	}
}

// Run is gobridge's main: it returns the exit code. GOBRIDGE_REPLAY=<file>
// answers requests from a ws pcap capture; without it gobridge asks the
// kernel.
func Run(args []string, stdout, stderr io.Writer) int {
	return run(config{replay: os.Getenv("GOBRIDGE_REPLAY")}, args, stdout, stderr)
}

type config struct {
	src    Source // overrides replay when set
	replay string
}

func run(cfg config, args []string, stdout, stderr io.Writer) int {
	w := bufio.NewWriter(stdout)
	r := &runner{o: newOut(w), stderr: stderr, vlanCurIdx: -1}
	err := r.main(cfg, args)
	if errors.Is(err, errSegv) {
		return 139 // bridge died: its buffered stdout never reached the pipe
	}
	if ferr := w.Flush(); ferr != nil && r.o.err == nil {
		r.o.err = ferr
	}
	var code exitCode
	switch {
	case err == nil:
	case errors.As(err, &code):
		return int(code) & 0xff
	case errors.Is(err, ErrReplay):
		r.errf("%v\n", err)
		return 2
	case errors.Is(err, errNotImplemented):
		r.errf("%v\n", err)
		return 255
	default:
		r.errf("%v\n", err)
		return 1
	}
	if r.o.err != nil || r.errOut != nil {
		return 1
	}
	return 0
}

// matches is lib/utils.c matches(prefix, s) == 0: prefix is a non-empty
// prefix of s.
func matches(prefix, s string) bool { return prefix != "" && strings.HasPrefix(s, prefix) }

// main is bridge.c main: the options, rtnl_open, then do_cmd.
func (r *runner) main(cfg config, args []string) error {
	color := false
	for len(args) > 0 {
		opt := args[0]
		if opt == "--" {
			args = args[1:]
			break
		}
		if !strings.HasPrefix(opt, "-") {
			break
		}
		if strings.HasPrefix(opt, "--") {
			opt = opt[1:]
		}
		colorName, colorVal, _ := strings.Cut(opt, "=")
		switch {
		case matches(opt, "-help"):
			return r.usage()
		case matches(opt, "-Version"):
			r.o.printf("bridge utility, iproute2-7.2.0\n")
			return exitCode(0)
		case matches(opt, "-stats"), matches(opt, "-statistics"):
			r.stats = true
		case matches(opt, "-details"):
			r.details = true
		case matches(opt, "-oneline"):
			r.o.sl = "\\"
		case matches(opt, "-timestamp"):
			// do_link, do_vlan and do_vni reset it; mst never prints one.
		case matches(opt, "-family"), matches(opt, "-netns"), matches(opt, "-batch"), opt == "-4", opt == "-6":
			return fmt.Errorf("%w: option %s", errNotImplemented, opt)
		case matches(colorName, "-color"):
			// matches_color: no value or "always" is on, "auto" is on
			// for a terminal, "never" off.
			color = colorVal == "" || colorVal == "always" || (colorVal == "auto" && isTerminal(os.Stdout))
		case matches(opt, "-compressvlans"):
			r.compress = true
		case matches(opt, "-force"):
		case matches(opt, "-json"):
			r.o.json = true
		case matches(opt, "-pretty"):
			r.o.pretty = true
		default:
			r.errf("Option \"%s\" is unknown, try \"bridge help\".\n", opt)
			return exitCode(255)
		}
		args = args[1:]
	}
	// check_enable_color: never under -j.
	r.o.color = color && !r.o.json

	switch {
	case cfg.src != nil:
		r.src = cfg.src
	case cfg.replay != "":
		s, err := NewReplaySource(cfg.replay)
		if err != nil {
			return err
		}
		r.src = s
	default:
		s, err := NewLiveSource()
		if err != nil {
			return err
		}
		defer func() {
			if cerr := s.Close(); cerr != nil && r.errOut == nil {
				r.errOut = cerr
			}
		}()
		r.src = s
	}
	if len(args) == 0 {
		return r.usage()
	}
	return r.doCmd(args[0], args[1:])
}

// isTerminal is isatty(fileno(stdout)) for -color=auto.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// doCmd is bridge.c do_cmd: the object by prefix, in cmds[]'s order.
func (r *runner) doCmd(obj string, args []string) error {
	for _, c := range []struct {
		name string
		fn   func([]string) error
	}{
		{"link", r.doLink},
		{"fdb", r.refuse("fdb")},
		{"mdb", r.refuse("mdb")},
		{"mst", r.doMst},
		{"vlan", r.doVlan},
		{"vni", r.doVni},
		{"monitor", r.refuse("monitor")},
		{"help", func([]string) error { return r.usage() }},
	} {
		if matches(obj, c.name) {
			return c.fn(args)
		}
	}
	r.errf("Object \"%s\" is unknown, try \"bridge help\".\n", obj)
	return exitCode(255)
}

func (r *runner) refuse(what string) func([]string) error {
	return func([]string) error { return fmt.Errorf("%w: bridge %s", errNotImplemented, what) }
}

// usage is bridge.c usage.
func (r *runner) usage() error {
	r.errf("%s", "Usage: bridge [ OPTIONS ] OBJECT { COMMAND | help }\n"+
		"       bridge [ -force ] -batch filename\n"+
		"where  OBJECT := { link | fdb | mdb | mst | vlan | vni | monitor }\n"+
		"       OPTIONS := { -V[ersion] | -s[tatistics] | -d[etails] |\n"+
		"                    -o[neline] | -t[imestamp] | -n[etns] name |\n"+
		"                    -com[pressvlans] -c[olor] -p[retty] -j[son] }\n")
	return exitCode(255)
}

// showWords is a sub-command that means show: show, lst or list, by
// prefix (matches) or exactly (strcmp, vni and vlan global).
func showWords(w string, exact bool) bool {
	for _, s := range []string{"show", "lst", "list"} {
		if (exact && w == s) || (!exact && matches(w, s)) {
			return true
		}
	}
	return false
}

// unknownCommand is the objects' "Command ... is unknown" and exit(-1).
func (r *runner) unknownCommand(obj, cmd string) error {
	r.errf("Command \"%s\" is unknown, try \"bridge %s help\".\n", cmd, obj)
	return exitCode(255)
}

// incomplete is lib/utils.c incomplete_command (NEXT_ARG with no next).
func (r *runner) incomplete() error {
	r.errf("Command line is not complete. Try option \"help\"\n")
	return exitCode(255)
}

// duparg is lib/utils.c duparg.
func (r *runner) duparg(key, arg string) error {
	r.errf("Error: duplicate \"%s\": \"%s\" is the second value.\n", key, arg)
	return exitCode(255)
}

// showArgs is the shape of the *_show loops: "dev" and, where vid,
// "vid"; other words are ignored (B2). link.c's master keyword is not
// else-if'd after dev (B1), so link parses its own loop.
func (r *runner) showArgs(args []string, vid bool) (dev string, err error) {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "dev":
			if i+1 >= len(args) {
				return "", r.incomplete()
			}
			i++
			if dev != "" {
				return "", r.duparg("dev", args[i])
			}
			dev = args[i]
		case vid && args[i] == "vid":
			if i+1 >= len(args) {
				return "", r.incomplete()
			}
			i++
			if r.filterVlan != 0 {
				return "", r.duparg("vid", args[i])
			}
			r.filterVlan = atoi(args[i])
		}
	}
	return dev, nil
}

// atoi is C atoi: optional space and sign, digits, the rest ignored; it
// wraps as the int conversion does (B5).
func atoi(s string) int32 {
	s = strings.TrimLeft(s, " \t\n\v\f\r")
	neg := false
	if s != "" && (s[0] == '+' || s[0] == '-') {
		neg = s[0] == '-'
		s = s[1:]
	}
	var v uint32
	for _, c := range []byte(s) {
		if c < '0' || c > '9' {
			break
		}
		v = v*10 + uint32(c-'0')
	}
	if neg {
		v = -v
	}
	return cint.Int(v)
}

// resolveFilter is the shows' ll_name_to_index and nodev.
func (r *runner) resolveFilter(dev string) error {
	if dev == "" {
		return nil
	}
	i, err := r.nameToIndex(dev)
	if err != nil {
		return err
	}
	if i == 0 {
		return r.nodev(dev)
	}
	r.filterIndex = i
	return nil
}
