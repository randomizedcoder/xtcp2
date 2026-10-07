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
where  OBJECT := { link | address | route | rule | neigh }
       OPTIONS := { -4 | -6 | -0 | -j[son] | -s[tats] | -d[etails] }

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
		// `--` ends the options and is CONSUMED, so `ip -- link show` runs
		// `link show` (ip/ip.c:192-195). The `i++` is the `argv++` there; the
		// loop's own increment must not also run, hence the explicit break.
		if a == "--" {
			i++
			break
		}
		// One leading dash is stripped when the second character is also a
		// dash (ip/ip.c:198-199), so `--json` is `-json` and `--oneline` is
		// `-oneline`. This is why goip rejected `ip --json link show` while
		// `ip` accepted it: matchesPrefix("--json", "-json") is false on
		// length alone, before any character is compared.
		//
		// Exactly one dash comes off, not all of them. `---json` becomes
		// `--json`, which matches nothing and is still an error — worth a row,
		// because a strings.TrimLeft would quietly accept it.
		if len(a) > 1 && a[1] == '-' {
			a = a[1:]
		}
		switch {
		case a == "-4":
			c.family = unix.AF_INET
		case a == "-6":
			c.family = unix.AF_INET6
		case a == "-0":
			// AF_PACKET (ip/ip.c:221-222), and an exact match rather than a
			// prefix for the same reason -4 and -6 are: `ip` compares these
			// three with strcmp, not matches().
			//
			// This is the one option here that reaches code that was already
			// written. `addr show`'s AF_PACKET branch — skip the address dump,
			// list every link with no address lines — exists in obj_addr.go
			// and in Service.AddressSnapshot, and until this case was added no
			// CLI input could produce it, because -0 was the only thing `ip`
			// sets preferred_family to AF_PACKET from on a show path.
			//
			// Not to be confused with ipaddr_list_link's AF_PACKET, which
			// `link show` assigns internally (ip/ipaddress.c:2416) and which
			// goip models by ignoring c.family in req.LinkShowDump.
			c.family = unix.AF_PACKET
		case matchesPrefix(a, "-json"):
			c.json = true
		case matchesPrefix(a, "-stats"), matchesPrefix(a, "-statistics"):
			// `ip` INCREMENTS show_stats here (ip/ip.c:232-234) rather than
			// setting it, and `-s -s` selects a second, wider render with
			// extra error-counter lines. goip implements show_stats == 1
			// only, so it counts too — and then refuses a count above one
			// rather than quietly rendering the narrow form.
			//
			// Refusing is the deliberate choice, and the alternative is worse
			// in a way specific to this tool: accepting `-s -s` and printing
			// the `-s` output would be a stdout divergence the parity harness
			// reports as goip getting `-s` wrong, with nothing in the report
			// to say a flag had been dropped. An explicit error names the
			// missing feature where it was asked for.
			c.showStats++
		case matchesPrefix(a, "-details"):
			// `ip` increments show_details here too (ip/ip.c:235-236), and
			// unlike -s it is never tested for a count above one: every one of
			// the nine sites that reads it tests `show_details` or
			// `show_details > 0`. So `-d -d` is `-d`, and goip counts for the
			// same reason `ip` does — to have nothing to say about the second
			// one — rather than needing the count.
			//
			// No option earlier in this switch begins with `-d`, so `-d` alone
			// reaches here, which is the spelling everything in the corpus was
			// captured with. That is a property of the ORDER of iproute2's
			// chain (`-loops`, `-family`, `-4`, `-6`, `-0`, `-M`, `-B`,
			// `-human`, `-iec`, `-stats`, `-details`, …) and not of the
			// pattern, since matches() is an unanchored prefix test — see
			// matchesPrefix.
			c.showDetails++
		case a == "-h", matchesPrefix(a, "-help"):
			fmt.Fprint(stdout, usage)
			return ExitOK
		default:
			fmt.Fprintf(stderr, "Option %q is unknown, try \"goip -help\".\n", a)
			return ExitUsage
		}
	}
	args = args[i:]

	if c.showStats > 1 {
		fmt.Fprintf(stderr,
			"goip: `-s -s` selects the wider statistics render, which is not implemented "+
				"(show_stats = %d); use a single -s.\n", c.showStats)
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
