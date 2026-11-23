// Package gobridge is a read-only Go clone of iproute2's bridge(8), the
// version the flake pins (7.2.0), for its link, vlan, vni, mst, fdb and
// mdb reads, built on pkg/xtcpnl's AF_BRIDGE link, vnifilter, vlan,
// neighbor and mdb decoders.
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
// Scope is the reads: link, vlan (show, -d, -s, -com, tunnelshow, global
// show with -s router ports), vni, mst, fdb (show, get) and mdb (show,
// get), alone or in a -batch (file or stdin, -force), with -netns,
// -family, -4 and -6. A batch keeps bridge's process state between
// lines, its leftover filters included (B28-B32, F2, M3). Every write
// (link set, vlan add/del/set, vlan global set, vni add/del, mst set,
// fdb add/append/replace/del/flush, mdb add/replace/del/flush) and
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

// retCode is a command function returning non-zero to do_cmd: main
// returns it, so alone it is the exit status, as exitCode is; in a batch
// it is "Command failed" and the batch stops, or with -force goes on.
// exitCode (exit()) ends a batch whatever -force says (B30).
type retCode int

func (e retCode) Error() string { return "return " + strconv.Itoa(int(e)) }

// errNotImplemented is a command gobridge does not clone (a write or
// monitor).
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

	// Each bridge/*.c object's filter statics, kept apart as the C keeps
	// them. They live as long as the process: a dev, vid or master word
	// sets one and nothing clears it, so in a batch the next line of the
	// same object inherits it (B28).
	linkDev    uint32 // link.c filter_dev_index
	linkMaster uint32 // link.c filter_master_index
	vlanIndex  uint32 // vlan.c filter_index
	vlanVid    int32  // vlan.c filter_vlan
	vniIndex   uint32 // vni.c filter_index
	mstIndex   uint32 // mst.c filter_index
	fdbIndex   uint32 // fdb.c filter_index
	fdbMaster  uint32 // fdb.c filter_master
	fdbState   uint32 // fdb.c filter_state: ORed across lines (F2)
	fdbVlan    uint32 // fdb.c filter_vlan
	fdbDynamic bool   // fdb.c filter_dynamic: set by show and get (F2)
	mdbIndex   uint32 // mdb.c filter_index
	mdbVlan    int32  // mdb.c filter_vlan
	vlanCurIdx int64  // vlan.c vlan_rtm_cur_ifidx; -1 for none
	llInited   bool   // ll_init_map's initialized: one link dump a process
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
	return run(config{replay: os.Getenv("GOBRIDGE_REPLAY"), stdin: os.Stdin}, args, stdout, stderr)
}

type config struct {
	src    Source // overrides replay when set
	replay string
	stdin  io.Reader // -batch -
}

func run(cfg config, args []string, stdout, stderr io.Writer) int {
	w := bufio.NewWriter(stdout)
	r := &runner{o: newOut(w), stderr: stderr, vlanCurIdx: -1}
	err := r.main(cfg, args)
	if errors.Is(err, errSegv) {
		return 139 // bridge died: its buffered stdout never reached the pipe
	}
	if errors.Is(err, errAbort) {
		r.errf("bridge: json_writer.c:122: jsonw_destroy: Assertion `self->depth == 0' failed.\n")
		return 134 // SIGABRT, stdout lost as for SIGSEGV
	}
	if ferr := w.Flush(); ferr != nil && r.o.err == nil {
		r.o.err = ferr
	}
	var code exitCode
	var ret retCode
	switch {
	case err == nil:
	case errors.As(err, &code):
		return int(code) & 0xff
	case errors.As(err, &ret):
		return int(ret) & 0xff
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

// helpWord is the "help" every object and option takes.
const helpWord = "help"

// The words several objects' parsers and printers share.
const (
	wordDev       = "dev"
	wordVni       = "vni"
	wordSrcVni    = "src_vni"
	wordPermanent = "permanent"
)

// matches is lib/utils.c matches(prefix, s) == 0: prefix is a non-empty
// prefix of s.
func matches(prefix, s string) bool { return prefix != "" && strings.HasPrefix(s, prefix) }

// options are main's flags that outlive the option loop.
type options struct {
	color, force, batch bool
	batchFile           string
}

// main is bridge.c main: the options, rtnl_open, then do_cmd, or the
// batch.
func (r *runner) main(cfg config, args []string) error {
	var st options
	args, err := r.options(args, &st)
	if err != nil {
		return err
	}
	// check_enable_color: never under -j.
	r.o.color = st.color && !r.o.json

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
	if st.batch {
		// Words after -batch FILE are never looked at (B32).
		return r.batch(st.batchFile, cfg.stdin, st.force)
	}
	if len(args) == 0 {
		return r.usage()
	}
	return r.doCmd(args[0], args[1:])
}

// options is main's option loop, in its order: -f is -family, tested
// before -force, and -c is -color, before -compressvlans. It returns the
// words after the options.
func (r *runner) options(args []string, st *options) ([]string, error) {
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
			return nil, r.usage()
		case matches(opt, "-Version"):
			// "bridge utility, %s": no "iproute2-", unlike ip and tc (B37).
			r.o.printf("bridge utility, 7.2.0\n")
			return nil, exitCode(0)
		case matches(opt, "-stats"), matches(opt, "-statistics"):
			r.stats = true
		case matches(opt, "-details"):
			r.details = true
		case matches(opt, "-oneline"):
			r.o.sl = "\\"
		case matches(opt, "-timestamp"):
			// do_link, do_vlan and do_vni reset it; mst never prints one.
		case matches(opt, "-family"):
			if len(args) < 2 {
				return nil, r.usage()
			}
			args = args[1:]
			if err := r.family(args[0]); err != nil {
				return nil, err
			}
		case opt == "-4", opt == "-6":
		case matches(opt, "-netns"):
			// NEXT_ARG counts argv[0], so with no name it goes on with
			// argv[argc], NULL, which glibc prints "(null)" (B33), and
			// the loop ends past it.
			if len(args) < 2 {
				return nil, r.netnsSwitch("(null)")
			}
			args = args[1:]
			if err := r.netnsSwitch(args[0]); err != nil {
				return nil, err
			}
		case matches(colorName, "-color"):
			// matches_color: no value or "always" is on, "auto" is on
			// for a terminal, "never" off.
			st.color = colorVal == "" || colorVal == "always" || (colorVal == "auto" && isTerminal(os.Stdout))
		case matches(opt, "-compressvlans"):
			r.compress = true
		case matches(opt, "-force"):
			st.force = true
		case matches(opt, "-json"):
			r.o.json = true
		case matches(opt, "-pretty"):
			r.o.pretty = true
		case matches(opt, "-batch"):
			if len(args) < 2 {
				return nil, r.usage()
			}
			args = args[1:]
			st.batch, st.batchFile = true, args[0]
		default:
			r.errf("Option \"%s\" is unknown, try \"bridge help\".\n", opt)
			return nil, exitCode(255)
		}
		args = args[1:]
	}
	return args, nil
}

// family is -family's value: preferred_family, which only fdb's address
// parsing reads.
func (r *runner) family(v string) error {
	switch v {
	case "inet", "inet6":
		return nil
	case helpWord:
		return r.usage()
	}
	r.errf("Error: argument \"%s\" is wrong: %s\n", v, "invalid protocol family")
	return exitCode(255)
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
		{"fdb", r.doFdb},
		{"mdb", r.doMdb},
		{"mst", r.doMst},
		{"vlan", r.doVlan},
		{wordVni, r.doVni},
		{"monitor", r.refuse("monitor")},
		{helpWord, func([]string) error { return r.usage() }},
	} {
		if matches(obj, c.name) {
			return c.fn(args)
		}
	}
	r.errf("Object \"%s\" is unknown, try \"bridge help\".\n", obj)
	return retCode(-1)
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
func (r *runner) showArgs(args []string, vid *int32) (dev string, err error) {
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == wordDev:
			if i+1 >= len(args) {
				return "", r.incomplete()
			}
			i++
			if dev != "" {
				return "", r.duparg(wordDev, args[i])
			}
			dev = args[i]
		case vid != nil && args[i] == "vid":
			if i+1 >= len(args) {
				return "", r.incomplete()
			}
			i++
			if *vid != 0 {
				return "", r.duparg("vid", args[i])
			}
			*vid = atoi(args[i])
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
//
//	filter_index = ll_name_to_index(filter_dev);
//	if (!filter_index)
//		return nodev(filter_dev);
//
// The static is set before the test, so a failed name clears it.
func (r *runner) resolveFilter(dev string, dst *uint32) error {
	if dev == "" {
		return nil
	}
	i, err := r.nameToIndex(dev)
	if err != nil {
		return err
	}
	*dst = i
	if i == 0 {
		return r.nodev(dev)
	}
	return nil
}
