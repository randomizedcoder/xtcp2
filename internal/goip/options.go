package goip

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// ErrRefusedOption is an option a read-only caller never serves: one that
// would enter another namespace (-netns), read a file (-batch), force a
// batch on (-force), size the socket (-rcvbuf), drive flush (-loops),
// print the usage (-help) or stamp a monitor's lines (-timestamp,
// -tshort). Run implements -loops, -help and the two stamps; ResolveShow
// refuses all eight.
var ErrRefusedOption = errors.New("option refused on a read-only path")

// The display options' full spellings, which the chain, Resolved.Options and
// runCtx.outputMode share.
const (
	optStatsCst   = "-stats"
	optDetailsCst = "-details"
	optOnelineCst = "-oneline"
	optBriefCst   = "-brief"
	optJSONCst    = "-json"
)

// options is what ip's option loop (ip/ip.c:189-301) leaves behind, as far
// as goip reads it. Run and ResolveShow both get it from parseOptions, so
// the two cannot disagree about what an option means.
type options struct {
	family      uint8
	json        bool
	brief       bool
	oneline     bool
	help        bool
	showStats   int
	showDetails int
	// timestamp and timestampShort count -timestamp and -tshort, which
	// the monitors read.
	timestamp, timestampShort int
	// flushLoops is -loops N when haveFlushLoops is set.
	flushLoops     int64
	haveFlushLoops bool
	// given is each option row matched, in argument order.
	given []*ipOption
}

// ipOption is one test of ip's option chain.
type ipOption struct {
	name string
	// exact marks the options ip compares with strcmp: -4, -6, -0, -M, -B
	// and -echo.
	exact bool
	// arg is the missarg name of the option's argument, or "" when it takes
	// none.
	arg string
	// set records the option; nil for one goip does not implement.
	set func(o *options, arg string)
	// refused marks the options ResolveShow refuses, whether or not Run
	// implements them.
	refused bool
}

// matches is ip's test for this option.
func (p *ipOption) matches(a string) bool {
	if p.exact {
		return a == p.name
	}
	return matchesPrefix(a, p.name)
}

// ipOptions is ip's option chain in ip/ip.c's order (7.1.0, :200-293),
// including the options goip does not implement. As with objects, the order
// is the interface: `-b` is -batch and not -brief because ip tests -batch
// first, and `-f` is -family, not -force. Dropping a row goip does not
// implement would hand its abbreviations to a later option.
//
// matches_color (:280) is approximated by a plain -color row: ip splits a
// `=value` off first, which only changes which error `-c=auto` gets, since
// goip implements neither.
var ipOptions = []ipOption{
	{name: "-loops", arg: "loop count", refused: true, set: func(o *options, a string) {
		// atoi's: `abc` is 0, which is no limit, and a negative count runs no
		// round. Every object accepts it; only `addr flush` reads it.
		o.flushLoops, o.haveFlushLoops = cAtoi(a), true
	}},
	{name: "-family", arg: "family type"},
	{name: "-4", exact: true, set: func(o *options, _ string) { o.family = unix.AF_INET }},
	{name: "-6", exact: true, set: func(o *options, _ string) { o.family = unix.AF_INET6 }},
	// AF_PACKET (ip/ip.c:221-222). This is the one option that reaches code
	// that was already written: `addr show`'s AF_PACKET branch — skip the
	// address dump, list every link with no address lines — exists in
	// obj_addr.go and in Service.AddressSnapshot, and -0 is the only thing
	// `ip` sets preferred_family to AF_PACKET from on a show path. Not to be
	// confused with ipaddr_list_link's AF_PACKET, which `link show` assigns
	// internally (ip/ipaddress.c:2416) and which goip models by ignoring
	// c.family in req.LinkShowDump.
	{name: "-0", exact: true, set: func(o *options, _ string) { o.family = unix.AF_PACKET }},
	{name: "-M", exact: true},
	{name: "-B", exact: true},
	{name: "-human"},
	{name: "-human-readable"},
	{name: "-iec"},
	// `ip` INCREMENTS show_stats (ip/ip.c:232-234) rather than setting it,
	// and `-s -s` selects a second, wider render with extra error-counter
	// lines. goip implements show_stats == 1 only, so it counts too — and
	// then refuses a count above one (statsCountMsg) rather than quietly
	// rendering the narrow form. Accepting `-s -s` and printing the `-s`
	// output would be a stdout divergence the parity harness reports as goip
	// getting `-s` wrong, with nothing to say a flag had been dropped.
	{name: optStatsCst, set: countStats},
	{name: "-statistics", set: countStats},
	// `ip` increments show_details too (ip/ip.c:235-236), and unlike -s no
	// site tests it for a count above one, so `-d -d` is `-d`. No earlier
	// option begins with `-d`, so `-d` alone reaches here, which is the
	// spelling everything in the corpus was captured with.
	{name: optDetailsCst, set: func(o *options, _ string) { o.showDetails++ }},
	{name: "-resolve"},
	{name: optOnelineCst, set: func(o *options, _ string) { o.oneline = true }},
	// -timestamp and -tshort (ip/ip.c:239-245) shape a monitor's lines;
	// no object has a monitor a read-only caller serves, so ResolveShow
	// refuses both. -tshort sets both counts, as ip's does.
	{name: "-timestamp", refused: true, set: func(o *options, _ string) { o.timestamp++ }},
	{name: "-tshort", refused: true, set: func(o *options, _ string) { o.timestamp++; o.timestampShort++ }},
	{name: "-Version"},
	{name: "-force", refused: true},
	{name: "-batch", arg: "batch file", refused: true},
	// `-b` and `-ba…` are -batch above, so the shortest brief is `-br`.
	{name: optBriefCst, set: func(o *options, _ string) { o.brief = true }},
	{name: optJSONCst, set: func(o *options, _ string) { o.json = true }},
	{name: "-pretty"},
	{name: "-rcvbuf", arg: "rcvbuf size", refused: true},
	{name: "-color"},
	{name: "-help", refused: true, set: func(o *options, _ string) { o.help = true }},
	{name: "-netns", arg: "netns name", refused: true},
	{name: "-Numeric"},
	{name: "-all"},
	{name: "-echo", exact: true},
}

func countStats(o *options, _ string) { o.showStats++ }

// helpOption is the -help row, which goip's `-h` also selects.
var helpOption = func() *ipOption {
	for i := range ipOptions {
		if ipOptions[i].name == "-help" {
			return &ipOptions[i]
		}
	}
	panic("goip: no -help option")
}()

// optionError is an option parseOptions could not take: row is nil for one
// that matches nothing, and missing is set when its argument is absent.
type optionError struct {
	arg     string
	row     *ipOption
	missing bool
}

func (e *optionError) Error() string {
	switch {
	case e.row == nil:
		return fmt.Sprintf("Option %q is unknown", e.arg)
	case e.missing:
		return fmt.Sprintf("argument %q is required", e.row.arg)
	}
	return fmt.Sprintf("option %s: %v", e.row.name, ErrNotImplemented)
}

// parseOptions is ip's option loop: it consumes the leading options of args
// and returns the rest, which starts at the object. It stops at -help, as
// ip's usage() exits there.
//
// # Option parsing is iproute2's, not Go's flag package
//
// `ip` accepts options only *before* the object, matches them with the same
// unanchored-prefix rule as objects (so `-j`, `-js` and `-json` are one
// option), and treats the first non-option argument as the object. Go's flag
// package would additionally accept them after the object and would reject
// `-4` as a flag with a value. Since the parity harness drives goip and `ip`
// with the same argv, the parsing has to be the same shape.
func parseOptions(args []string) (options, []string, error) {
	var o options
	i := 0
	for ; i < len(args); i++ {
		a := args[i]
		// `--` ends the options and is CONSUMED, so `ip -- link show` runs
		// `link show` (ip/ip.c:192-195). It is tested before the dash test,
		// as there.
		if a == "--" {
			i++
			break
		}
		if a == "" || a[0] != '-' {
			break
		}
		// One leading dash is stripped when the second character is also a
		// dash (ip/ip.c:198-199), so `--json` is `-json`. Exactly one comes
		// off: `---json` becomes `--json`, which matches nothing and is still
		// an error, where a strings.TrimLeft would quietly accept it.
		if len(a) > 1 && a[1] == '-' {
			a = a[1:]
		}
		row := lookupOption(a)
		if row == nil || row.set == nil {
			return o, nil, &optionError{arg: a, row: row}
		}
		var val string
		if row.arg != "" {
			if i+1 >= len(args) {
				return o, nil, &optionError{arg: a, row: row, missing: true}
			}
			i++
			val = args[i]
		}
		row.set(&o, val)
		o.given = append(o.given, row)
		if o.help {
			i++
			break
		}
	}
	return o, args[i:], nil
}

// lookupOption is the first option a matches, in ip's order, or nil.
//
// `-h` is goip's one departure: ip reads it as -human, the first option it
// prefixes, but goip has no -human and has always printed its usage for
// `-h`, which is what anyone typing it wants.
func lookupOption(a string) *ipOption {
	if a == "-h" {
		return helpOption
	}
	for i := range ipOptions {
		if ipOptions[i].matches(a) {
			return &ipOptions[i]
		}
	}
	return nil
}

// statsCountMsg is the refusal for a -s count goip does not render, or ""
// for one it does. Run prints it and Show and ResolveShow return it, so the
// three refuse `-s -s` in the same words.
func statsCountMsg(n int) string {
	if n <= 1 {
		return ""
	}
	return fmt.Sprintf("`-s -s` selects the wider statistics render, which is not implemented "+
		"(show_stats = %d); use a single -s.", n)
}
