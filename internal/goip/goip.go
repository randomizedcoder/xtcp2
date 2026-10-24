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
	// ExitMinusTwo is do_cmd's -(2), for a command that returned 2:
	// mptcp_monitor's listen ending in error (ip/ipmptcp.c).
	ExitMinusTwo = 254
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

// run is Run with stdin a parameter, which the showdump and restore verbs
// read.
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
	c.timestamp, c.timestampShort = o.timestamp, o.timestampShort

	if msg := statsCountMsg(c.showStats); msg != "" && !wideStatsObject(args) {
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
	if msg := outputModeMsg(c, &obj); msg != "" {
		fmt.Fprintf(stderr, "goip: %s\n", msg)
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
// refuses (its exit(-1)), as for mrule's family refusal. A save whose dump failed exits 1, and so does a
// neigh flush whose dump failed. The monitor exits 255 for an argument or a
// file it refuses, 1 for a subscription refused or a file whose walk fails,
// and 2 for a listen that ends in error. tcp_metrics exits 255 for what
// tcp_metrics.c ends with exit(-1), 1 for a dump it refuses or a family
// genl_init_handle cannot resolve, and 2 for a kernel refusal. sr exits 255
// for what ipseg6.c ends with exit(-1), 1 for a dump or a reply it cannot
// finish or parse, and 2 for a refused tunsrc show. ioam exits 255 for
// what ipioam6.c ends with exit(-1), and 1 for a dump it cannot finish or a
// monitor that cannot subscribe or whose listen ends. mptcp exits 255 for
// what ipmptcp.c ends with exit(-1) and for a monitor that cannot
// subscribe, 254 for a monitor whose listen ends in error, 1 for an answer
// it refuses, and 2 for a dump it cannot finish or a kernel refusal. fou
// exits 255 for what ipfou.c ends with exit(-1) and for a dump it cannot
// finish, and 1 for a device it refuses or a show given arguments. ila
// exits 255 for what ipila.c ends with exit(-1) and for a dump it cannot
// finish, and 1 for a show given arguments. l2tp exits 255 for what
// ipl2tp.c ends with exit(-1), and 1 for a dump it cannot finish. macsec
// exits 255 for what ipmacsec.c ends with exit(-1), and 1 for a device
// ll_name_to_index does not find, an SA check_sa_args refuses and a dump
// it cannot finish. xfrm exits 255 for what ip/xfrm*.c ends with exit(-1),
// 1 for an SPI the protocol cannot take, an answer it cannot print and a
// dump it cannot finish, and 2 for a kernel refusal.
func exitCodeOf(err error) int {
	switch {
	case errors.Is(err, ErrFlushIncomplete), errors.Is(err, ErrDumpFile), errors.Is(err, ErrRuleDumpTerminated),
		errors.Is(err, ErrAddrLabelFlushTerminated), errors.Is(err, ErrMruleFamily), errors.Is(err, ErrMonitorAbort),
		errors.Is(err, ErrTCPMetricsUsage), errors.Is(err, ErrSrUsage), errors.Is(err, ErrIoamUsage),
		errors.Is(err, ErrMptcpUsage), errors.Is(err, ErrMptcpSubscribe), errors.Is(err, ErrFouUsage),
		errors.Is(err, ErrFouDumpTerminated), errors.Is(err, ErrIlaUsage), errors.Is(err, ErrIlaDumpTerminated),
		errors.Is(err, ErrL2tpUsage), errors.Is(err, ErrMacsecUsage), errors.Is(err, ErrXfrmUsage):
		return ExitMinusOne
	case errors.Is(err, ErrMptcpListen):
		return ExitMinusTwo
	case errors.Is(err, ErrMptcpDumpTerminated):
		// Ahead of ErrMptcpReply, which a refused answer in a dump wraps.
		return ExitFailure
	case errors.Is(err, ErrMonitorSetup), errors.Is(err, ErrMonitorFile):
		return ExitUsage
	case errors.Is(err, ErrNotImplemented), errors.Is(err, ErrInvalidArgument),
		errors.Is(err, ErrWriteNotApplied), errors.Is(err, ErrSaveTerminated),
		errors.Is(err, ErrDumpTerminated), errors.Is(err, ErrNeighFlushTerminated),
		errors.Is(err, ErrTCPMetricsDumpTerminated), errors.Is(err, ErrGenlResolve),
		errors.Is(err, ErrSrReply), errors.Is(err, ErrSrDumpTerminated),
		errors.Is(err, ErrIoamDumpTerminated), errors.Is(err, ErrIoamMonitor),
		errors.Is(err, ErrMptcpReply), errors.Is(err, ErrFouRefused), errors.Is(err, ErrIlaShowArgs),
		errors.Is(err, ErrL2tpDumpTerminated), errors.Is(err, ErrMacsecRefused),
		errors.Is(err, ErrMacsecDumpTerminated), errors.Is(err, ErrXfrmRefused),
		errors.Is(err, ErrXfrmDumpTerminated):
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
