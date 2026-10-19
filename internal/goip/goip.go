package goip

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// Exit codes, matching `ip`: 1 for a usage or argument error, 2 for a runtime
// failure (ip/ip.c returns the errno-derived code, and 2 is what a failed
// netlink operation produces in practice).
const (
	ExitOK      = 0
	ExitUsage   = 1
	ExitFailure = 2
	// ExitMinusOne is `ip`'s exit(-1), from missarg, and do_cmd's -(1) for
	// a command that returned 1: `addr flush` running out of rounds
	// (ip/ip.c:131, ip/ipaddress.c:2012-2014).
	ExitMinusOne = 255
)

const usage = `Usage: goip [ OPTIONS ] OBJECT { COMMAND | help }
where  OBJECT := { link | address | route | rule | neigh }
       OPTIONS := { -l[oops] COUNT | -4 | -6 | -0 | -j[son] | -s[tats] |
                    -d[etails] | -br[ief] | -o[neline] }

goip is a subset of ip(8), built as a coverage test for pkg/xtcpnl.
It never creates, deletes or sets anything: the link, route and address
write verbs, address flush included, build their requests and write them to
the file GOIP_WRITE_OUT names, and refuse to run without it. Only the
lookups and dumps they need are sent.
`

// Run is the whole program, minus os.Args and os.Exit.
//
// Taking the writers as arguments and returning an int rather than calling
// os.Exit is what makes the CLI testable end to end: a test can assert on
// stdout, on stderr and on the exit code in one table row. cmd/goip is a
// dozen lines over this. The options are parsed by parseOptions, which
// ResolveShow shares.
func Run(args []string, stdout, stderr io.Writer) int {
	return run(args, os.Stdin, stdout, stderr)
}

// run is Run with stdin a parameter, which `addr showdump` reads.
func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	c := newRunCtx(stdin, stdout, stderr)

	o, args, err := parseOptions(args)
	if err != nil {
		var oe *optionError
		if !errors.As(err, &oe) {
			fmt.Fprintf(stderr, "goip: %v\n", err)
			return ExitUsage
		}
		if oe.missing {
			return missLoopCount(stderr)
		}
		// Every option goip does not implement, refused ones included, is
		// "unknown" here, as it was before the chain held ip's full list.
		fmt.Fprintf(stderr, "Option %q is unknown, try \"goip -help\".\n", oe.arg)
		return ExitUsage
	}
	if o.help {
		fmt.Fprint(stdout, usage)
		return ExitOK
	}
	c.family = o.family
	c.json, c.brief, c.oneline = o.json, o.brief, o.oneline
	c.showStats, c.showDetails = o.showStats, o.showDetails
	c.flushLoops, c.haveFlushLoops = o.flushLoops, o.haveFlushLoops

	if msg := statsCountMsg(c.showStats); msg != "" {
		fmt.Fprintf(stderr, "goip: %s\n", msg)
		return ExitUsage
	}

	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return ExitUsage
	}

	obj, err := lookupObject(args[0])
	if err != nil {
		switch {
		case errors.Is(err, ErrUnknownObject):
			fmt.Fprintf(stderr, "Object %q is unknown, try \"goip -help\".\n", args[0])
		default:
			fmt.Fprintf(stderr, "goip: %v\n", err)
		}
		return ExitUsage
	}

	// ip ignores -brief and -oneline for an object that has no such render,
	// and renders them for several that goip does not claim to (link, route,
	// neigh). Refusing here, by name, keeps a dropped flag from showing up in
	// the parity report as a full-render divergence. address is the one
	// object that reads them (object.outputModes); see addrShow.
	if opt := c.outputMode(); opt != "" && !obj.outputModes {
		fmt.Fprintf(stderr, "goip: option %s is only implemented for address\n", opt)
		return ExitUsage
	}

	src, closeSrc, err := openSource()
	if err != nil {
		fmt.Fprintf(stderr, "goip: %v\n", err)
		return ExitFailure
	}
	defer func() {
		if cerr := closeSrc(); cerr != nil {
			fmt.Fprintf(stderr, "goip: close: %v\n", cerr)
		}
	}()

	c.src = src

	if err := obj.run(c, args[1:]); err != nil {
		fmt.Fprintf(stderr, "goip: %v\n", err)
		return exitCodeOf(err)
	}
	return ExitOK
}

// exitCodeOf is the status `ip` exits with for an object's error. It exits
// 1 for an argument it refuses before sending (do_cmd negates
// iplink_modify's -1), 2 for a kernel refusal (its -2), and 255 for a flush
// that ran out of rounds (ipaddr_flush's 1, negated) or a dump file showdump
// refuses (its exit(-1)). A save whose dump failed exits 1.
func exitCodeOf(err error) int {
	switch {
	case errors.Is(err, ErrFlushIncomplete), errors.Is(err, ErrDumpFile):
		return ExitMinusOne
	case errors.Is(err, ErrNotImplemented), errors.Is(err, ErrInvalidArgument),
		errors.Is(err, ErrWriteNotApplied), errors.Is(err, ErrSaveTerminated),
		errors.Is(err, ErrDumpTerminated):
		return ExitUsage
	}
	return ExitFailure
}

// openSource picks the live socket or, when GOIP_REPLAY names a pcap, the
// replay source. The returned closer is always safe to call.
//
// The closer returns an error rather than nothing because the only way closing
// a netlink fd fails is EBADF, which means it was already closed — a
// double-close in this package, not a condition of the machine. Discarding it
// would hide the one bug it can report. `ip` never closes its socket at all, so
// there is no iproute2 behavior to match here.
func openSource() (Source, func() error, error) {
	noop := func() error { return nil }
	if path, ok := replayPathFromEnv(); ok {
		s, err := OpenReplayPortid(path, replayPortidFromEnv())
		if err != nil {
			return nil, noop, err
		}
		return s, noop, nil
	}
	s, err := OpenNetlink()
	if err != nil {
		return nil, noop, err
	}
	return s, s.Close, nil
}

// missLoopCount is utils.c's missarg for `-loops` with no count, which exits
// -1, so the status is 255.
func missLoopCount(stderr io.Writer) int {
	fmt.Fprint(stderr, "Error: argument \"loop count\" is required\n")
	return ExitMinusOne
}
