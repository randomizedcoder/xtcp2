// Package gotipc is a read-only Go clone of iproute2's tipc(8), the
// version the flake pins (7.2.0), built on pkg/xtcpnl's TIPC decoder and
// request builders.
//
// Like godevlink and govdpa, it exists to make xtcpnl's TIPC coverage
// something a diff can check: replaying tipc's nlmon (ws) pcap corpus
// (pkg/xtcpnl/testdata/<kernel>/ws/tipc) must reproduce tipc's .out, .err
// and .rc byte for byte. The output rules are tipc's, down to the JSON
// tipc gets wrong (a key inside an array, one object for every link) and
// the crash it dies of (-j link get broadcast); the comments cite the
// tipc/*.c function each piece follows.
//
// Scope is the read-only commands: bearer list and get, link list, get,
// statistics show and monitor summary/list/get, media list and get,
// nametable show, node list and get, and socket list. Every write (enable,
// disable, add, set, reset, remove, flush) is refused, and so is `bearer
// get media`, which the corpus does not capture. gotipc never writes.
//
// Two things tipc reads come from a TIPC socket, not netlink: the node's
// own address (node get address) and the identity behind a node hash
// (hash2nodestr's SIOCGETNODEID ioctl). A replay takes them from its
// Source (GOTIPC_NODEID, GOTIPC_ADDR).
package gotipc

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// progName is argv[0] as tipc's help text prints it.
const progName = "tipc"

// The words tipc's tables and options share.
const (
	wGet       = "get"
	wList      = "list"
	wSet       = "set"
	wLink      = "link"
	wMedia     = "media"
	wMTU       = "mtu"
	wPriority  = "priority"
	wTolerance = "tolerance"
	wWindow    = "window"
	wDevice    = "device"
	wUDP       = "udp"
	wName      = "name"
)

// errFail is a failure gotipc has already reported to stderr, as tipc does
// before main returns -1.
var errFail = errors.New("gotipc: failed")

// errCrash is tipc dying of SIGSEGV on a read of an attribute the kernel
// did not send (mnl_attr_get_*(NULL)) or a NULL help function: exit 139,
// stdout lost.
var errCrash = errors.New("gotipc: tipc dies of SIGSEGV here")

// errFPE is tipc dying of SIGFPE: a division by a zero the kernel sent.
var errFPE = errors.New("gotipc: tipc dies of SIGFPE here")

// Run is gotipc's main: it returns the exit code. GOTIPC_REPLAY=<file>
// answers requests from a ws pcap capture; GOTIPC_NODEID and GOTIPC_ADDR
// (hex) are the node's identity and address for that replay.
func Run(args []string, stdout, stderr io.Writer) int {
	cfg := config{replay: os.Getenv("GOTIPC_REPLAY"), own: os.Getenv("GOTIPC_NODEID")}
	if a := os.Getenv("GOTIPC_ADDR"); a != "" {
		v, err := strconv.ParseUint(a, 16, 32)
		if err != nil {
			fmt.Fprintf(stderr, "gotipc: GOTIPC_ADDR %q is not a hex u32\n", a)
			return 2
		}
		cfg.addr = uint32(v)
	}
	return run(cfg, args, stdout, stderr)
}

type config struct {
	src    Source // overrides replay when set
	replay string
	own    string
	addr   uint32
}

// runner is tipc's globals: the output, help_flag, the genl socket's family
// and the source.
type runner struct {
	o        *out
	stderr   io.Writer
	src      Source
	help     bool
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
	rest, rc, ok := r.getopt(args)
	if ok {
		rc = r.start(cfg, rest)
	}
	if rc == 139 || rc == 136 {
		return rc // tipc died: its buffered stdout never reached the pipe
	}
	if err := w.Flush(); err != nil && r.o.err == nil {
		r.o.err = err
	}
	if r.o.err != nil && rc == 0 {
		rc = 1
	}
	return rc
}

// getopt is main's getopt_long(argc, argv, "hjp", ...) loop, GNU style: an
// option may come anywhere before "--", and the words that are not options
// are kept in order. An unknown option is getopt's message and main's
// return 1.
func (r *runner) getopt(args []string) (rest []string, rc int, ok bool) {
	longs := []string{"help", "json", "pretty"}
	set := func(c byte) {
		switch c {
		case 'h':
			r.help = true
		case 'j':
			r.o.json = true
		case 'p':
			r.o.pretty = true
		}
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--":
			return append(rest, args[i+1:]...), 0, true
		case strings.HasPrefix(a, "--"):
			name, _, hasVal := strings.Cut(a[2:], "=")
			var match []string
			for _, l := range longs {
				if l == name {
					match = []string{l}
					break
				}
				if strings.HasPrefix(l, name) {
					match = append(match, l)
				}
			}
			switch {
			case len(match) == 0:
				r.errf("%s: unrecognized option '%s'\n", progName, a)
				return nil, 1, false
			case len(match) > 1:
				r.errf("%s: option '%s' is ambiguous; possibilities: '--%s'\n", progName, a,
					strings.Join(match, "' '--"))
				return nil, 1, false
			case hasVal:
				r.errf("%s: option '--%s' doesn't allow an argument\n", progName, match[0])
				return nil, 1, false
			}
			set(match[0][0])
		case len(a) > 1 && a[0] == '-':
			for j := 1; j < len(a); j++ {
				if !strings.ContainsRune("hjp", rune(a[j])) {
					r.errf("%s: invalid option -- '%c'\n", progName, a[j])
					return nil, 1, false
				}
				set(a[j])
			}
		default:
			rest = append(rest, a)
		}
	}
	return rest, 0, true
}

// start opens the source, resolves the family (main's mnlu_gen_socket_open)
// and runs the command.
func (r *runner) start(cfg config, rest []string) int {
	switch {
	case cfg.src != nil:
		r.src = cfg.src
	case cfg.replay != "":
		s, err := NewReplaySource(cfg.replay)
		if err != nil {
			r.errf("gotipc: %v\n", err)
			return 2
		}
		s.Own, s.Addr = cfg.own, cfg.addr
		r.src = s
	default:
		r.errf("gotipc: no replay capture (set GOTIPC_REPLAY)\n")
		return 2
	}
	if err := r.initFamily(); err != nil {
		if errors.Is(err, ErrReplay) {
			r.errf("%v\n", err)
			return 2
		}
		r.errf("Unable to get TIPC nl family id (module loaded?)\n")
		return 255
	}
	c := &cmdl{argv: append([]string{progName}, rest...), optind: 1}
	top := &cmdEntry{name: progName, help: about}
	err := r.runCmd(top, topCmds(), c)
	switch {
	case err == nil:
		return 0
	case errors.Is(err, ErrReplay):
		r.errf("%v\n", err)
		return 2
	case errors.Is(err, errCrash), errors.Is(err, errNullString):
		return 139
	case errors.Is(err, errFPE):
		return 136
	case errors.Is(err, errFail):
		return 255
	default:
		r.errf("gotipc: %v\n", err)
		return 255
	}
}

// --- tipc/cmdl.c ---

// cmdl is struct cmdl: the words, argv[0] first, and the next to read.
type cmdl struct {
	argv   []string
	optind int
}

// cmdEntry is struct cmd: a word, its function and its help.
type cmdEntry struct {
	name string
	fn   func(r *runner, c *cmdl, cmd *cmdEntry) error
	help func(r *runner, c *cmdl)
}

// findCmd is find_cmd: the one entry str is a prefix of; none when no
// entry or more than one matches.
func findCmd(cmds []cmdEntry, str string) *cmdEntry {
	var match *cmdEntry
	for i := range cmds {
		if !strings.HasPrefix(cmds[i].name, str) {
			continue
		}
		if match != nil {
			return nil
		}
		match = &cmds[i]
	}
	return match
}

// runCmd is run_cmd: with no word left, the caller's help; an unknown or
// ambiguous word is refused (or, under -h, the caller's help).
func (r *runner) runCmd(caller *cmdEntry, cmds []cmdEntry, c *cmdl) error {
	if c.optind >= len(c.argv) {
		if caller.help != nil {
			caller.help(r, c)
		}
		return errFail
	}
	name := c.argv[c.optind]
	c.optind++
	cmd := findCmd(cmds, name)
	if cmd == nil {
		if r.help && caller.help != nil {
			caller.help(r, c)
		} else {
			r.errf("error, invalid command \"%s\"\n", name)
			r.errf("use --help for command help\n")
		}
		return errFail
	}
	return cmd.fn(r, c, cmd)
}

// opt is struct opt: a key, whether it takes a value, and what it got.
type opt struct {
	key    string
	keyval bool
	val    *string
}

// findOpt is find_opt: the one key str is a prefix of.
func findOpt(opts []opt, str string) *opt {
	var match *opt
	for i := range opts {
		if !strings.HasPrefix(opts[i].key, str) {
			continue
		}
		if match != nil {
			return nil
		}
		match = &opts[i]
	}
	return match
}

// parseOpts is parse_opts: every word left is a key, a KEYVAL key taking
// the next word as its value (none past the end, argv[argc] being NULL).
func (r *runner) parseOpts(opts []opt, c *cmdl) error {
	for i := c.optind; i < len(c.argv); i++ {
		o := findOpt(opts, c.argv[i])
		if o == nil {
			r.errf("error, invalid option \"%s\"\n", c.argv[i])
			return errFail
		}
		if o.keyval {
			c.optind++
			i++
		}
		o.val = nil
		if i < len(c.argv) {
			v := c.argv[i]
			o.val = &v
		}
		c.optind++
	}
	return nil
}

// getOpt is get_opt: the key's value, by exact key, when it has one.
func getOpt(opts []opt, key string) *string {
	for i := range opts {
		if opts[i].key == key && opts[i].val != nil {
			return opts[i].val
		}
	}
	return nil
}

// cmdHelp is a "(cmd->help)(cmdl)" call: a NULL help is tipc's crash.
func (r *runner) cmdHelp(c *cmdl, cmd *cmdEntry) error {
	if cmd.help == nil {
		return errCrash
	}
	cmd.help(r, c)
	return errFail
}

// notImplemented refuses a command gotipc does not clone.
func notImplemented(r *runner, c *cmdl, cmd *cmdEntry) error {
	r.errf("gotipc: %s %s is not implemented (read-only clone)\n", strings.Join(c.argv[1:c.optind-1], " "), cmd.name)
	return errFail
}

// helpText returns a help function printing text, with each %s replaced
// by argv[0].
func helpText(text string) func(r *runner, c *cmdl) {
	return func(r *runner, c *cmdl) {
		r.errf("%s", strings.ReplaceAll(text, "%s", c.argv[0]))
	}
}

// --- the command tables (tipc/tipc.c and each object's file) ---

func topCmds() []cmdEntry {
	return []cmdEntry{
		{"bearer", func(r *runner, c *cmdl, cmd *cmdEntry) error { return r.runCmd(cmd, bearerCmds(), c) }, bearerHelp},
		{wLink, func(r *runner, c *cmdl, cmd *cmdEntry) error { return r.runCmd(cmd, linkCmds(), c) }, linkHelp},
		{wMedia, func(r *runner, c *cmdl, cmd *cmdEntry) error { return r.runCmd(cmd, mediaCmds(), c) }, mediaHelp},
		{"nametable", func(r *runner, c *cmdl, cmd *cmdEntry) error { return r.runCmd(cmd, nametableCmds(), c) }, nametableHelp},
		{"node", func(r *runner, c *cmdl, cmd *cmdEntry) error { return r.runCmd(cmd, nodeCmds(), c) }, nodeHelp},
		{"peer", func(r *runner, c *cmdl, cmd *cmdEntry) error { return r.runCmd(cmd, peerCmds(), c) }, peerHelp},
		{"socket", func(r *runner, c *cmdl, cmd *cmdEntry) error { return r.runCmd(cmd, socketCmds(), c) }, socketHelp},
	}
}

func bearerCmds() []cmdEntry {
	return []cmdEntry{
		{"add", notImplemented, nil},
		{"disable", notImplemented, nil},
		{"enable", notImplemented, nil},
		{wGet, func(r *runner, c *cmdl, cmd *cmdEntry) error { return r.runCmd(cmd, bearerGetCmds(), c) }, bearerGetHelp},
		{wList, bearerList, nil},
		{wSet, notImplemented, nil},
	}
}

func bearerGetCmds() []cmdEntry {
	return []cmdEntry{
		{wPriority, bearerGetProp, bearerGetHelp},
		{wTolerance, bearerGetProp, bearerGetHelp},
		{wWindow, bearerGetProp, bearerGetHelp},
		{wMTU, bearerGetProp, bearerGetHelp},
		{wMedia, notImplemented, bearerGetHelp},
	}
}

func linkCmds() []cmdEntry {
	return []cmdEntry{
		{wGet, func(r *runner, c *cmdl, cmd *cmdEntry) error { return r.runCmd(cmd, linkGetCmds(), c) }, linkGetHelp},
		{wList, linkList, nil},
		{wSet, notImplemented, nil},
		{"statistics", func(r *runner, c *cmdl, cmd *cmdEntry) error { return r.runCmd(cmd, linkStatCmds(), c) }, linkStatHelp},
		{"monitor", func(r *runner, c *cmdl, cmd *cmdEntry) error { return r.runCmd(cmd, linkMonCmds(), c) }, linkMonHelp},
	}
}

func linkGetCmds() []cmdEntry {
	return []cmdEntry{
		{wPriority, linkGetProp, linkGetHelp},
		{wTolerance, linkGetProp, linkGetHelp},
		{wWindow, linkGetProp, linkGetHelp},
		{"broadcast", linkGetBcast, linkGetBcastHelp},
	}
}

func linkStatCmds() []cmdEntry {
	return []cmdEntry{
		{"reset", notImplemented, nil},
		{"show", linkStatShow, linkStatShowHelp},
	}
}

func linkMonCmds() []cmdEntry {
	return []cmdEntry{
		{wSet, notImplemented, nil},
		{wGet, func(r *runner, c *cmdl, cmd *cmdEntry) error { return r.runCmd(cmd, linkMonGetCmds(), c) }, linkMonGetHelp},
		{wList, linkMonList, linkMonListHelp},
		{"summary", linkMonSummary, nil},
	}
}

func linkMonGetCmds() []cmdEntry {
	return []cmdEntry{{"threshold", linkMonGetProp, nil}}
}

func mediaCmds() []cmdEntry {
	return []cmdEntry{
		{wGet, func(r *runner, c *cmdl, cmd *cmdEntry) error { return r.runCmd(cmd, mediaGetCmds(), c) }, mediaGetHelp},
		{wList, mediaList, nil},
		{wSet, notImplemented, nil},
	}
}

func mediaGetCmds() []cmdEntry {
	return []cmdEntry{
		{wPriority, mediaGetProp, mediaGetHelp},
		{wTolerance, mediaGetProp, mediaGetHelp},
		{wWindow, mediaGetProp, mediaGetHelp},
		{wMTU, mediaGetProp, mediaGetHelp},
	}
}

func nametableCmds() []cmdEntry { return []cmdEntry{{"show", nametableShow, nil}} }

func nodeCmds() []cmdEntry {
	return []cmdEntry{
		{wList, nodeList, nil},
		{wGet, func(r *runner, c *cmdl, cmd *cmdEntry) error { return r.runCmd(cmd, nodeGetCmds(), c) }, nodeGetHelp},
		{wSet, notImplemented, nil},
		{"flush", notImplemented, nil},
	}
}

// nodeGetCmds is cmd_node_get's table: every entry's help is NULL, which
// identity's and netid's -h paths call.
func nodeGetCmds() []cmdEntry {
	return []cmdEntry{
		{"address", nodeGetAddr, nil},
		{"identity", nodeGetNodeID, nil},
		{"netid", nodeGetNetID, nil},
		{"clusterid", nodeGetNetID, nil},
	}
}

func peerCmds() []cmdEntry   { return []cmdEntry{{"remove", notImplemented, nil}} }
func socketCmds() []cmdEntry { return []cmdEntry{{wList, socketList, nil}} }

// --- the help texts, verbatim ---

var about = helpText("Transparent Inter-Process Communication Protocol\n" +
	"Usage: %s [OPTIONS] COMMAND [ARGS] ...\n" +
	"\n" +
	"Options:\n" +
	" -h, --help \t\tPrint help for last given command\n" +
	" -j, --json \t\tJson format printouts\n" +
	" -p, --pretty \t\tpretty print\n" +
	"\n" +
	"Commands:\n" +
	" bearer                - Show or modify bearers\n" +
	" link                  - Show or modify links\n" +
	" media                 - Show or modify media\n" +
	" nametable             - Show nametable\n" +
	" node                  - Show or modify node related parameters\n" +
	" peer                  - Peer related operations\n" +
	" socket                - Show sockets\n")

var bearerHelp = helpText("Usage: %s bearer COMMAND [ARGS] ...\n" +
	"\n" +
	"COMMANDS\n" +
	" add\t\t\t- Add data to existing bearer\n" +
	" enable\t\t- Enable a bearer\n" +
	" disable\t\t- Disable a bearer\n" +
	" set\t\t\t- Set various bearer properties\n" +
	" get\t\t\t- Get various bearer properties\n" +
	" list\t\t\t- List bearers\n")

const bearerOpts = "OPTIONS\n" +
	" priority\t\t- Bearer link priority\n" +
	" tolerance\t\t- Bearer link tolerance\n" +
	" window\t\t- Bearer link window\n" +
	" mtu\t\t\t- Bearer link mtu\n"

const bearerMedia = "\nMEDIA\n" +
	" udp\t\t\t- User Datagram Protocol\n" +
	" ib\t\t\t- Infiniband\n" +
	" eth\t\t\t- Ethernet\n"

var bearerGetHelp = helpText("Usage: %s bearer get [OPTION] media MEDIA ARGS...\n" + bearerOpts + bearerMedia)

func bearerGetUDPHelp(r *runner, c *cmdl, media string) {
	r.errf("Usage: %s bearer get [OPTION] media %s name NAME [UDP OPTIONS]\n\n", c.argv[0], media)
	r.errf("UDP OPTIONS\n" +
		" remoteip              - Remote ip address\n" +
		" remoteport            - Remote port\n" +
		" localip               - Local ip address\n" +
		" localport             - Local port\n\n")
	r.errf("%s", bearerOpts)
}

func bearerGetL2Help(r *runner, c *cmdl, media string) {
	r.errf("Usage: %s bearer get OPTION media %s device DEVICE\n", c.argv[0], media)
	r.errf("%s", bearerOpts)
}

var linkHelp = helpText("Usage: %s link COMMAND [ARGS] ...\n" +
	"\n" +
	"COMMANDS\n" +
	" list                  - List links\n" +
	" get                   - Get various link properties\n" +
	" set                   - Set various link properties\n" +
	" statistics            - Show or reset statistics\n" +
	" monitor               - Show or set link supervision\n")

var linkGetHelp = helpText("Usage: %s link get PPROPERTY link LINK\n\n" +
	"PROPERTIES\n" +
	" tolerance             - Get link tolerance\n" +
	" priority              - Get link priority\n" +
	" window                - Get link window\n" +
	" broadcast             - Get link broadcast\n")

var linkGetBcastHelp = helpText("Usage: %s link get PPROPERTY\n\n" +
	"PROPERTIES\n" +
	" broadcast             - Get link broadcast\n")

var linkStatHelp = helpText("Usage: %s link stat COMMAND [ARGS]\n\n" +
	"COMMANDS:\n" +
	" reset                 - Reset link statistics for link\n" +
	" show                  - Get link priority\n")

var linkStatShowHelp = helpText("Usage: %s link stat show [ link { LINK | SUBSTRING | all } ]\n")

var linkMonHelp = helpText("Usage: %s montior COMMAND [ARGS] ...\n\n" +
	"COMMANDS\n" +
	" set\t\t\t- Set monitor properties\n" +
	" get\t\t\t- Get monitor properties\n" +
	" list\t\t\t- List all cluster members\n" +
	" summary\t\t- Show local node monitor summary\n")

var linkMonGetHelp = helpText("Usage: %s monitor get PPROPERTY\n\n" +
	"PROPERTIES\n" +
	" threshold\t- Get monitor activation threshold\n")

var linkMonListHelp = helpText("Usage: %s monitor list [ media MEDIA ARGS...]\n\n" + bearerMedia)

func linkMonListL2Help(r *runner, c *cmdl, media string) {
	r.errf("Usage: %s monitor list media %s device DEVICE [OPTIONS]\n", c.argv[0], media)
}

func linkMonListUDPHelp(r *runner, c *cmdl, _ string) {
	r.errf("Usage: %s monitor list media udp name NAME\n\n", c.argv[0])
}

var mediaHelp = helpText("Usage: %s media COMMAND [ARGS] ...\n" +
	"\n" +
	"Commands:\n" +
	" list                  - List active media types\n" +
	" get                   - Get various media properties\n" +
	" set                   - Set various media properties\n")

var mediaGetHelp = helpText("Usage: %s media get PPROPERTY media MEDIA\n\n" +
	"PROPERTIES\n" +
	" tolerance             - Get media tolerance\n" +
	" priority              - Get media priority\n" +
	" window                - Get media window\n" +
	" mtu                   - Get media mtu\n")

var nametableHelp = helpText("Usage: %s nametable COMMAND\n\n" +
	"COMMANDS\n" +
	" show                  - Show nametable\n")

var nodeHelp = helpText("Usage: %s node COMMAND [ARGS] ...\n\n" +
	"COMMANDS\n" +
	" list                  - List remote nodes\n" +
	" get                   - Get local node parameters\n" +
	" set                   - Set local node parameters\n" +
	" flush                 - Flush local node parameters\n")

var nodeGetHelp = helpText("Usage: %s node get PROPERTY\n\n" +
	"PROPERTIES\n" +
	" identity              - Get node identity\n" +
	" clusterid             - Get local clusterid\n")

var peerHelp = helpText("Usage: %s peer COMMAND [ARGS] ...\n\n" +
	"COMMANDS\n" +
	" remove                - Remove an offline peer node\n")

var socketHelp = helpText("Usage: %s socket COMMAND\n\n" +
	"Commands:\n" +
	" list                  - List sockets (ports)\n")
