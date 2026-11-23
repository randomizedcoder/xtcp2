// Command tnlsnap records the tnlsys snapshot (internal/tnlsys) of one
// `ip` run: the tunnel ioctls it makes, traced under ptrace with their
// exact input and output; the tun properties of every device in
// /sys/class/net; and, when ip would walk /proc itself (-details on
// tuntap, or on a -batch), the /dev/net/tun descriptors there. Any other
// walk would only record other processes' tun descriptors, which vary.
//
//	tnlsnap -- -j tunnel show
//
// runs `ip -j tunnel show`, the `ip` first on PATH. It runs nothing but
// `ip`: it is a root ptrace tool in the capture guest, and its arguments
// come from a rows file.
//
// It prints the snapshot as JSON on stdout; the traced run's own stdout,
// stderr and exit status go into it rather than through. A capture row
// runs it as a readback beside the `ip` it captures, and goip replays the
// row with GOIP_TNLSYS naming the result. It exits 1 when the trace or a
// walk fails, and 2 on bad usage.
package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/tnlsys"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) < 2 || args[0] != "--" {
		fmt.Fprintln(stderr, "usage: tnlsnap -- IP-ARG...")
		return 2
	}
	b, err := snapshotIP(args[1:])
	if err != nil {
		fmt.Fprintf(stderr, "tnlsnap: %v\n", err)
		return 1
	}
	if _, err := stdout.Write(b); err != nil {
		fmt.Fprintf(stderr, "tnlsnap: %v\n", err)
		return 1
	}
	return 0
}

// snapshotIP records `ip ARGS`. The program is fixed, ip as PATH
// resolves it, and the arguments reach it as its argv, never a shell.
func snapshotIP(args []string) ([]byte, error) {
	path, err := exec.LookPath("ip")
	if err != nil {
		return nil, err
	}
	// ip reads its stdin for `-batch -`: a readback's `< b.txt` reaches it.
	cmd := &exec.Cmd{Path: path, Args: append([]string{"ip"}, args...), Stdin: os.Stdin}
	return snapshot(cmd, tnlsys.Live{}, walksProc(args))
}

// walksProc is whether ip, given args, runs show_processes: it sets
// show_details, and its object is tuntap, or a -batch file's lines may name
// it. It mirrors ip.c's option loop (ipOpts, first match wins, a second
// leading '-' dropped) and do_cmd's object table (ipObjects).
func walksProc(args []string) bool {
	details, batch := false, false
	for i := 0; i < len(args); i++ {
		opt := args[i]
		if opt == "--" {
			i++
			return details && (batch || (i < len(args) && tuntapObject(args[i])))
		}
		if !strings.HasPrefix(opt, "-") {
			return details && (batch || tuntapObject(opt))
		}
		if strings.HasPrefix(opt, "--") {
			opt = opt[1:]
		}
		o, ok := ipOpt(opt)
		if !ok {
			return false // ip exits: unknown option
		}
		switch o.name {
		case "-details":
			details = true
		case "-batch":
			batch = true
		}
		if o.value {
			i++
		}
	}
	return details && batch
}

// ipOption is one of ip.c's options.
type ipOption struct {
	name  string
	exact bool // strcmp, not matches
	value bool // takes the next word
}

var ipOpts = []ipOption{
	{"-loops", false, true}, {"-family", false, true},
	{"-4", true, false}, {"-6", true, false}, {"-0", true, false}, {"-M", true, false}, {"-B", true, false},
	{"-human-readable", false, false}, {"-iec", false, false}, {"-stats", false, false},
	{"-statistics", false, false}, {"-details", false, false}, {"-resolve", false, false},
	{"-oneline", false, false}, {"-timestamp", false, false}, {"-tshort", false, false},
	{"-Version", false, false}, {"-force", false, false}, {"-batch", false, true},
	{"-brief", false, false}, {"-json", false, false}, {"-pretty", false, false},
	{"-rcvbuf", false, true}, {"-color", false, false}, {"-help", false, false},
	{"-netns", false, true}, {"-Numeric", false, false}, {"-all", false, false},
	{"-echo", true, false},
}

// ipOpt is the first of ip.c's options that opt selects.
func ipOpt(opt string) (ipOption, bool) {
	for _, o := range ipOpts {
		if o.exact && opt == o.name || !o.exact && matches(opt, o.name) {
			return o, true
		}
		// matches_color: -c, -col, -color, each with an optional =WHEN.
		if o.name == "-color" {
			if w, _, _ := strings.Cut(opt, "="); len(w) >= 2 && matches(w, "-color") {
				return o, true
			}
		}
	}
	return ipOption{}, false
}

// ipObjects is the part of do_cmd's table, in order, that can claim a word
// before tuntap does: a word matching tuntap or tap starts with t, and no
// earlier entry starts with t.
var ipObjects = []string{"tunnel", "tunl", "tuntap", "tap"}

// tuntapObject is whether do_cmd runs do_iptuntap for word.
func tuntapObject(word string) bool {
	for _, c := range ipObjects {
		if matches(word, c) {
			return c == "tuntap" || c == "tap"
		}
	}
	return false
}

// matches is iproute2's matches(): whether prefix begins word. An empty
// prefix matches nothing.
func matches(prefix, word string) bool {
	return prefix != "" && strings.HasPrefix(word, prefix)
}

// snapshot traces cmd and records the props, and the /proc walk if walk.
func snapshot(cmd *exec.Cmd, live tnlsys.Live, walk bool) ([]byte, error) {
	r, recs, err := tnlsys.Trace(cmd)
	if err != nil {
		return nil, err
	}
	s := tnlsys.Snapshot{Ioctls: recs, Run: r}
	devs, err := netDevs(live)
	if err != nil {
		return nil, err
	}
	s.RecordProps(live, devs)
	if walk {
		fds, err := live.TunFDs()
		if err != nil {
			return nil, err
		}
		s.Fds = &fds
	}
	return s.Marshal()
}

// netDevs lists /sys/class/net.
func netDevs(live tnlsys.Live) ([]string, error) {
	root := live.SysRoot
	if root == "" {
		root = "/sys"
	}
	ents, err := os.ReadDir(filepath.Join(root, "class", "net"))
	if err != nil {
		return nil, errors.Join(errors.New("listing devices"), err)
	}
	devs := make([]string, 0, len(ents))
	for _, e := range ents {
		devs = append(devs, e.Name())
	}
	return devs, nil
}
