// Package gotc is a read-only Go clone of iproute2's tc(8), the version the
// flake pins (7.2.0), for its qdisc, class, filter and chain reads, built on
// pkg/xtcpnl's tcmsg decoders.
//
// Like gobridge, it exists to make xtcpnl's coverage something a diff can
// check: replaying tc's nlmon (ws) pcap corpus must reproduce tc's stdout,
// stderr and exit status byte for byte, what tc gets wrong included
// (docs/netlink/gotc.md, docs/netlink/upstream/iproute2-tc-qdisc-class.md).
// The comments cite the tc/*.c and lib/*.c function each piece follows.
//
// Scope is qdisc and class show, and filter and chain show and get, alone
// or in a -batch (file or stdin, -force), with -netns. actions, monitor and
// exec, and every write, are refused: gotc never writes.
package gotc

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// exitCode is tc leaving with exit(code), or main returning it; -1 is 255.
type exitCode int

func (e exitCode) Error() string { return "exit " + strconv.Itoa(int(e)) }

// retCode is a command function returning non-zero to do_cmd: main returns
// it, so alone it is the exit status, as exitCode is; in a batch it is
// "Command failed" and the batch stops, or with -force goes on. exitCode
// (exit()) ends a batch whatever -force says.
type retCode int

func (e retCode) Error() string { return "return " + strconv.Itoa(int(e)) }

// errNotImplemented is a command gotc does not clone: a write, an object
// outside qdisc, class, filter and chain, or a kind outside the slice.
var errNotImplemented = errors.New("gotc: not implemented")

// The tc version strings -Version prints: the flake's iproute2 and the
// libbpf it links.
const (
	versionCst       = "7.2.0"
	libbpfVersionCst = "1.8.0"
)

// runner is tc's globals: the output, the option counters, the socket's
// sequence, the link cache, the class names and the source.
type runner struct {
	o      *out
	stderr io.Writer
	errOut error
	src    Source
	seq    uint32
	sys    sysFS

	stats     int     // show_stats
	details   int     // show_details
	raw       int     // show_raw
	graph     bool    // show_graph
	iec       int     // use_iec
	numeric   int     // numeric
	useNames  bool    // use_names
	confFile  *string // conf_file, nil for NULL
	batchMode bool    // batch_mode
	brief     int     // brief
	core      tcCore
	names     *namesDB

	cache    llCache
	llInited bool // ll_init_map's initialized: one link dump a process

	qf qdiscFilter  // tc_qdisc.c's statics, kept across batch lines (Q29)
	cf classFilter  // tc_class.c's statics, kept across batch lines (Q30)
	ff filterFilter // tc_filter.c's statics, kept across batch lines (Q29f)

	fProto uint16     // f_proto: the last filter's htons'd protocol, u32 -p's key
	realms rtRealmTab // rt_names.c's realm table
}

func (r *runner) errf(format string, a ...any) {
	if _, err := fmt.Fprintf(r.stderr, format, a...); err != nil && r.errOut == nil {
		r.errOut = err
	}
}

// Run is gotc's main: it returns the exit code. GOTC_REPLAY=<file> answers
// requests from a ws pcap capture; without it gotc asks the kernel.
func Run(args []string, stdout, stderr io.Writer) int {
	return run(config{replay: os.Getenv("GOTC_REPLAY"), stdin: os.Stdin, sys: hostSys()}, args, stdout, stderr)
}

type config struct {
	src    Source // overrides replay when set
	replay string
	stdin  io.Reader // -batch -
	sys    sysFS     // /proc/net/psched and the class names files
}

func run(cfg config, args []string, stdout, stderr io.Writer) int {
	_, rc := runWith(cfg, args, stdout, stderr)
	return rc
}

// runWith is run, keeping the runner for the tests to look at.
func runWith(cfg config, args []string, stdout, stderr io.Writer) (*runner, int) {
	w := bufio.NewWriter(stdout)
	r := &runner{o: newOut(w), stderr: stderr, sys: cfg.sys}
	r.o.under = stdout
	return r, r.exit(r.main(cfg, args), w)
}

// exit flushes stdout and maps main's result to the exit status.
func (r *runner) exit(err error, w *bufio.Writer) int {
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

// helpWord is the "help" every object takes.
const helpWord = "help"

// matches is lib/utils.c matches(prefix, s) == 0: prefix is a non-empty
// prefix of s.
func matches(prefix, s string) bool { return prefix != "" && strings.HasPrefix(s, prefix) }

// The color options lib/color.c knows: COLOR_OPT_NEVER, _AUTO, _ALWAYS.
const (
	colorNever = iota
	colorAuto
	colorAlways
)

// matchesColor is lib/color.c matches_color: "-color" by prefix, with no
// value, "always", "auto" or "never" after a '='. Any other value is not a
// color option at all, so the option loop goes on to -timestamp.
func matchesColor(arg string, val *int) bool {
	name, v, _ := strings.Cut(arg, "=")
	if !matches(name, "-color") {
		return false
	}
	switch v {
	case "", "always":
		*val = colorAlways
	case "auto":
		*val = colorAuto
	case "never":
		*val = colorNever
	default:
		return false
	}
	return true
}

// defaultColor is default_color_opt: never when NO_COLOR has a value, else
// CONF_COLOR, which the flake's build leaves at never.
func defaultColor() int { return colorNever }

// options are main's flags that outlive the option loop.
type options struct {
	color, force int
	batchFile    string
	batch        bool
	oneline      int
	json         int
	pretty       int
}

// main is tc.c main: the options, then batch(), or usage with no object,
// or tc_core_init, rtnl_open, cls_names_init and do_cmd.
func (r *runner) main(cfg config, args []string) error {
	st := options{color: defaultColor()}
	args, err := r.options(args, &st)
	if err != nil {
		return err
	}
	if st.oneline > 0 {
		r.o.sl = "\\"
	}
	r.o.json, r.o.pretty = st.json > 0, st.pretty > 0
	// check_enable_color: never under -j; always, or auto on a terminal.
	r.o.color = st.json == 0 && (st.color == colorAlways || (st.color == colorAuto && isTerminal(os.Stdout)))

	if st.batch {
		return r.batch(cfg, st.batchFile, st.force > 0)
	}
	if len(args) == 0 {
		r.usage()
		return nil
	}
	r.core = r.sys.coreInit()
	if err := r.open(cfg, exitCode(1)); err != nil {
		return err
	}
	defer r.close()
	if r.useNames {
		if err := r.namesInit(); err != nil {
			return err
		}
	}
	return r.doCmd(args[0], args[1:])
}

// open is rtnl_open: the injected source, the replay, or the kernel. A
// socket that will not open is "Cannot open rtnetlink" and fail: main
// exits 1, batch returns -1.
func (r *runner) open(cfg config, fail error) error {
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
			r.errf("Cannot open rtnetlink\n")
			return fail
		}
		r.src = s
	}
	return nil
}

// close is rtnl_close.
func (r *runner) close() {
	if c, ok := r.src.(io.Closer); ok {
		if err := c.Close(); err != nil && r.errOut == nil {
			r.errOut = err
		}
	}
}

// options is main's option loop, in its order. Each test is matches(argv,
// option), so a prefix takes the first option it begins: -b is -batch, not
// -brief; -c is -cf, not -color (Q24); -n is -netns; and a lone "-" is
// -stats. Unlike ip, tc gives "--" no meaning: --json is unknown. It
// returns the words after the options.
func (r *runner) options(args []string, st *options) ([]string, error) {
	for len(args) > 0 {
		opt := args[0]
		if !strings.HasPrefix(opt, "-") {
			break
		}
		switch {
		case matches(opt, "-stats"), matches(opt, "-statistics"):
			r.stats++
		case matches(opt, "-details"):
			r.details++
		case matches(opt, "-raw"):
			r.raw++
		case matches(opt, "-pretty"):
			st.pretty++
		case matches(opt, "-graph"):
			r.graph = true
		case matches(opt, "-Version"):
			r.o.printf("tc utility, iproute2-" + versionCst + ", libbpf " + libbpfVersionCst + "\n")
			return nil, exitCode(0)
		case matches(opt, "-iec"):
			r.iec++
		case matches(opt, "-help"):
			r.usage()
			return nil, exitCode(0)
		case matches(opt, "-force"):
			st.force++
		case matches(opt, "-batch"):
			args = args[1:]
			if len(args) == 0 {
				r.errf("Error: argument \"%s\" is required\n", "batch file")
				return nil, exitCode(255)
			}
			st.batch, st.batchFile = true, args[0]
		case matches(opt, "-netns"):
			// NEXT_ARG counts argv[0], so with no name it goes on with
			// argv[argc], NULL, which glibc prints "(null)".
			name := "(null)"
			if len(args) > 1 {
				args = args[1:]
				name = args[0]
			}
			if err := r.netnsSwitch(name); err != nil {
				return nil, err
			}
		case matches(opt, "-Numeric"):
			r.numeric++
		case matches(opt, "-names"), matches(opt, "-nm"):
			r.useNames = true
		case matches(opt, "-cf"), matches(opt, "-conf"):
			// NEXT_ARG: with no path, conf_file is argv[argc], NULL.
			r.confFile = nil
			if len(args) > 1 {
				args = args[1:]
				r.confFile = &args[0]
			}
		case matchesColor(opt, &st.color):
		case matches(opt, "-timestamp"), matches(opt, "-tshort"):
			// Only monitor prints a timestamp, and gotc refuses monitor.
		case matches(opt, "-json"):
			st.json++
		case matches(opt, "-oneline"):
			st.oneline++
		case matches(opt, "-brief"):
			r.brief++
		case opt == "-echo":
			// echo_request: only the writes read it.
		default:
			r.errf("Option \"%s\" is unknown, try \"tc -help\".\n", opt)
			return nil, exitCode(255)
		}
		args = args[1:]
	}
	return args, nil
}

// isTerminal is isatty(fileno(stdout)) for -color=auto.
func isTerminal(f *os.File) bool {
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

// doCmd is tc.c do_cmd: the object by prefix, in its order.
func (r *runner) doCmd(obj string, args []string) error {
	for _, c := range []struct {
		name string
		fn   func([]string) error
	}{
		{wordQdisc, r.doQdisc},
		{"class", r.doClass},
		{"filter", r.doFilter},
		{"chain", r.doChain},
		{"actions", r.refuse("actions")},
		{"monitor", r.refuse("monitor")},
		{"exec", r.refuse("exec")},
		{helpWord, func([]string) error { r.usage(); return nil }},
	} {
		if matches(obj, c.name) {
			return c.fn(args)
		}
	}
	r.errf("Object \"%s\" is unknown, try \"tc help\".\n", obj)
	return retCode(-1)
}

func (r *runner) refuse(what string) func([]string) error {
	return func([]string) error { return fmt.Errorf("%w: tc %s", errNotImplemented, what) }
}

// usage is tc.c usage. Its callers return 0.
func (r *runner) usage() {
	r.errf("%s", "Usage:\ttc [ OPTIONS ] OBJECT { COMMAND | help }\n"+
		"\ttc [-force] -batch filename\n"+
		"where  OBJECT := { qdisc | class | filter | chain |\n"+
		"\t\t    action | monitor | exec }\n"+
		"       OPTIONS := { -V[ersion] | -s[tatistics] | -d[etails] | -r[aw] |\n"+
		"\t\t    -o[neline] | -j[son] | -p[retty] | -c[olor]\n"+
		"\t\t    -b[atch] [filename] | -n[etns] name | -N[umeric] |\n"+
		"\t\t     -nm | -nam[es] | { -cf | -conf } path\n"+
		"\t\t     -br[ief] | -echo }\n")
}

// listWords is a sub-command that means show: list, show or lst, by
// prefix, in do_qdisc's and do_class's order.
func listWords(w string) bool {
	return matches(w, "list") || matches(w, "show") || matches(w, "lst")
}

// writeVerb is a sub-command do_qdisc or do_class sends to its *_modify.
func writeVerb(w string, verbs ...string) bool {
	for _, v := range verbs {
		if matches(w, v) {
			return true
		}
	}
	return false
}

// doQdisc is tc_qdisc.c do_qdisc.
func (r *runner) doQdisc(args []string) error {
	if len(args) == 0 {
		return r.qdiscList(nil)
	}
	switch w := args[0]; {
	case writeVerb(w, "add", "change", "replace", "link", "delete"):
		return fmt.Errorf("%w: tc qdisc %s", errNotImplemented, w)
	case listWords(w):
		return r.qdiscList(args[1:])
	case matches(w, helpWord):
		r.qdiscUsage()
		return nil
	}
	r.errf("Command \"%s\" is unknown, try \"tc qdisc help\".\n", args[0])
	return retCode(-1)
}

// doClass is tc_class.c do_class.
func (r *runner) doClass(args []string) error {
	if len(args) == 0 {
		return r.classList(nil)
	}
	switch w := args[0]; {
	case writeVerb(w, "add", "change", "replace", "delete"):
		return fmt.Errorf("%w: tc class %s", errNotImplemented, w)
	case listWords(w):
		return r.classList(args[1:])
	case matches(w, helpWord):
		r.classUsage()
		return nil
	}
	r.errf("Command \"%s\" is unknown, try \"tc class help\".\n", args[0])
	return retCode(-1)
}

// qdiscUsage is tc_qdisc.c usage.
func (r *runner) qdiscUsage() {
	r.errf("%s", "Usage: tc qdisc [ add | del | replace | change | show ] dev STRING\n"+
		"       [ handle QHANDLE ] [ root | ingress | clsact | parent CLASSID ]\n"+
		"       [ estimator INTERVAL TIME_CONSTANT ]\n"+
		"       [ stab [ help | STAB_OPTIONS] ]\n"+
		"       [ ingress_block BLOCK_INDEX ] [ egress_block BLOCK_INDEX ]\n"+
		"       [ [ QDISC_KIND ] [ help | OPTIONS ] ]\n"+
		"\n"+
		"       tc qdisc { show | list } [ dev STRING ] [ QDISC_ID ] [ invisible ]\n"+
		"Where:\n"+
		"QDISC_KIND := { [p|b]fifo | tbf | prio | red | etc. }\n"+
		"OPTIONS := ... try tc qdisc add <desired QDISC_KIND> help\n"+
		"STAB_OPTIONS := ... try tc qdisc add stab help\n"+
		"QDISC_ID := { root | ingress | handle QHANDLE | parent CLASSID }\n")
}

// classUsage is tc_class.c usage.
func (r *runner) classUsage() {
	r.errf("%s", "Usage: tc class [ add | del | change | replace | show ] dev STRING\n"+
		"       [ classid CLASSID ] [ root | parent CLASSID ]\n"+
		"       [ [ QDISC_KIND ] [ help | OPTIONS ] ]\n"+
		"\n"+
		"       tc class show [ dev STRING ] [ root | parent CLASSID ]\n"+
		"Where:\n"+
		"QDISC_KIND := { prio | etc. }\n"+
		"OPTIONS := ... try tc class add <desired QDISC_KIND> help\n")
}
