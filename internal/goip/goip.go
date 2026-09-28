package goip

import (
	"errors"
	"fmt"
	"io"

	"golang.org/x/sys/unix"
)

// Exit codes, matching `ip`: 1 for a usage or argument error, 2 for a runtime
// failure (ip/ip.c returns the errno-derived code, and 2 is what a failed
// netlink operation produces in practice).
const (
	ExitOK      = 0
	ExitUsage   = 1
	ExitFailure = 2
)

const usage = `Usage: goip [ OPTIONS ] OBJECT { COMMAND | help }
where  OBJECT := { link | address | route | neigh }
       OPTIONS := { -4 | -6 | -j[son] }

goip is a read-only subset of ip(8), built as a coverage test for
pkg/xtcpnl. It never creates, deletes or sets anything.
`

// Run is the whole program, minus os.Args and os.Exit.
//
// Taking the writers as arguments and returning an int rather than calling
// os.Exit is what makes the CLI testable end to end: a test can assert on
// stdout, on stderr and on the exit code in one table row. cmd/goip is a
// dozen lines over this.
//
// # Option parsing is iproute2's, not Go's flag package
//
// `ip` accepts options only *before* the object, matches them with the same
// unanchored-prefix rule as objects (so `-j`, `-js` and `-json` are one
// option), and treats the first non-option argument as the object. Go's flag
// package would additionally accept them after the object and would reject
// `-4` as a flag with a value. Since the parity harness drives goip and `ip`
// with the same argv, the parsing has to be the same shape.
func Run(args []string, stdout, stderr io.Writer) int {
	c := &runCtx{
		out:    stdout,
		errOut: stderr,
		family: unix.AF_UNSPEC,
	}

	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		if len(a) == 0 || a[0] != '-' {
			break
		}
		switch {
		case a == "-4":
			c.family = unix.AF_INET
		case a == "-6":
			c.family = unix.AF_INET6
		case matchesPrefix(a, "-json"):
			c.json = true
		case a == "-h", matchesPrefix(a, "-help"):
			fmt.Fprint(stdout, usage)
			return ExitOK
		default:
			fmt.Fprintf(stderr, "Option %q is unknown, try \"goip -help\".\n", a)
			return ExitUsage
		}
	}
	args = args[i:]

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
	c.lltab = NewLLTab()

	if err := obj.run(c, args[1:]); err != nil {
		fmt.Fprintf(stderr, "goip: %v\n", err)
		if errors.Is(err, ErrNotImplemented) {
			return ExitUsage
		}
		return ExitFailure
	}
	return ExitOK
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
