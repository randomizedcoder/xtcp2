package goip

import (
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/randomizedcoder/xtcp2/internal/goip/req"
)

// ErrUnknownObject is `ip`'s "Object ... is unknown" case: the argument
// matches no object at all.
var ErrUnknownObject = errors.New("unknown object")

// ErrNotImplemented is the distinct case of an object goip recognizes but does
// not implement. Keeping it separate from ErrUnknownObject is not politeness:
// the parity harness drives both tools with the same argv, so it needs to tell
// "goip has not got there yet" from "that argument is a typo", and a single
// error would collapse them.
var ErrNotImplemented = errors.New("object recognized but not implemented by goip")

// object is one row of iproute2's cmds[] table (ip/ip.c).
type object struct {
	name string
	// run is nil for a recognized-but-unimplemented object.
	run func(*runCtx, []string) error
	// outputModes is set for the objects that read -brief and -oneline. Run
	// refuses both for every other object.
	outputModes bool
	// verbs is the object's verb chain in iproute2's order (verbs.go); nil
	// for an unimplemented object.
	verbs []verb
	// aliasOf is the row this one spells differently (`neighbour`, `ntbl`,
	// `tcpmetrics`, `tap`), or "" for a canonical row. Lists of objects name
	// each one once by leaving the aliases out.
	aliasOf string
}

// objects is the object table in iproute2's order, which is load-bearing.
//
// # Why the full table is here, including the objects goip will never do
//
// Because matching is by unanchored prefix and first-match-wins, so an
// object's position decides what a short abbreviation means. Deleting the
// unimplemented rows would silently change the meaning of `r`, `n` and `l`:
//
//	r    -> route, because route precedes rule
//	n    -> neigh, because neigh precedes netns, netconf and nexthop
//	net  -> netns, because netns precedes netconf
//	l    -> link, because link precedes l2tp
//
// Drop `rule` from the table and `r` still resolves to route — by luck. Drop
// `route` and `r` becomes `rule`, which is a different command silently
// accepted. The abbreviations are part of the interface the harness drives, so
// the table has to be complete even where the implementations are not.
//
// Order transcribed from ip/ip.c's cmds[].
var objects = []object{
	{name: "address", run: runAddress, outputModes: true, verbs: addressVerbs},
	{name: "addrlabel", run: runAddrLabel, verbs: addrLabelVerbs},
	{name: "maddress"},
	{name: "route", run: runRoute, verbs: routeVerbs},
	{name: "rule", run: runRule, verbs: ruleVerbs},
	{name: "neighbor", run: runNeigh, verbs: neighVerbs},
	{name: "neighbour", run: runNeigh, verbs: neighVerbs, aliasOf: "neighbor"},
	{name: "ntable", run: runNTable, verbs: ntableVerbs},
	{name: "ntbl", run: runNTable, verbs: ntableVerbs, aliasOf: "ntable"},
	{name: "link", run: runLink, verbs: linkVerbs},
	{name: "l2tp"},
	{name: "fou"},
	{name: "ila"},
	{name: "macsec"},
	{name: "tunnel"},
	{name: "tunl6"},
	{name: "tcp_metrics"},
	{name: "tcpmetrics", aliasOf: "tcp_metrics"},
	{name: "token"},
	{name: "tuntap"},
	{name: "tap", aliasOf: "tuntap"},
	{name: "mroute"},
	{name: "mrule"},
	{name: "netns"},
	{name: "netconf", run: runNetconf, verbs: netconfVerbs},
	{name: vrfKeywordCst, run: runVrf, verbs: vrfVerbs},
	{name: "sr"},
	{name: "nexthop", run: runNexthop, verbs: nexthopVerbs},
	{name: "mptcp"},
	{name: "ioam"},
	{name: "stats", run: runStats, verbs: statsVerbs},
	{name: "monitor"},
	{name: "xfrm"},
}

// matchesPrefix reimplements iproute2's matches() (lib/utils.c:909-919) as a
// predicate, and inverts its one awkward convention.
//
//	int matches(const char *cmd, const char *pattern) {
//		int len = strlen(cmd);
//		if (len > strlen(pattern))
//			return -1;
//		return memcmp(pattern, cmd, len);
//	}
//
// Three properties fall out of that and all three are tested:
//
//   - **Unanchored prefix, not abbreviation-of-a-known-length.** `a`, `ad`,
//     `addr` and `address` all match "address".
//   - **A prefix longer than the pattern fails.** `addressx` is -1, not a
//     match, because the length test comes first.
//   - **Case-sensitive.** memcmp, so `ADDR` does not match.
//
// The inversion: `matches("", p)` returns 0 — a *match* — because memcmp of
// zero bytes is 0. So in iproute2 an empty argument matches the first table
// entry. That is reachable only through `ip ""`, and treating it as a match
// would make an empty argv silently mean `ip address`. This returns false for
// it instead, and the negative row in the table says so.
func matchesPrefix(arg, pattern string) bool {
	if arg == "" {
		return false
	}
	if len(arg) > len(pattern) {
		return false
	}
	return pattern[:len(arg)] == arg
}

// devKeywordCst is the `dev NAME` selector keyword, which every object that
// takes one spells identically.
//
// It is one constant rather than four literals because the spelling is
// shared, and it is deliberately NOT a matchesPrefix pattern. iproute2
// compares this keyword with strcmp everywhere it accepts it - route at
// ip/iproute.c:1912, neigh at ip/ipneigh.c:526, address and link at
// ip/ipaddress.c:2241, which both list paths reach - so `dev` does not
// abbreviate and `ip route show d lo` is an error, not a shorter spelling.
// Using this constant with == is therefore the faithful comparison.
const devKeywordCst = "dev"

// The verbs every list-capable object accepts for its show path. Each object's
// table keeps its own upstream order (its matches() chain), so only the words
// are shared, not the tables.
const (
	showVerbCst = "show"
	lstVerbCst  = "lst"
	listVerbCst = "list"
)

// The keywords spelled the same in `ip link`, `ip route` and `ip nexthop`.
const (
	vrfKeywordCst = "vrf"
	mtuKeywordCst = "mtu"
)

// lookupObject resolves an argument to an object, first match in table order.
func lookupObject(arg string) (object, error) {
	i := objectIndex(arg)
	if i < 0 {
		return object{}, fmt.Errorf("%q: %w", arg, ErrUnknownObject)
	}
	o := objects[i]
	if o.run == nil {
		return o, fmt.Errorf("%q: %w", o.name, ErrNotImplemented)
	}
	return o, nil
}

// canonical is the name of the row o aliases, or o's own.
func (o *object) canonical() string {
	if o.aliasOf != "" {
		return o.aliasOf
	}
	return o.name
}

// runCtx is the state one goip invocation shares between dispatch and the
// object handlers.
type runCtx struct {
	src   Source
	lltab *LLTab
	// in is stdin, which only `addr showdump` reads.
	in     io.Reader
	out    io.Writer
	errOut io.Writer
	json   bool
	// family is the -4 / -6 / default selection, as an AF_*. Note that
	// `link show` overrides it to AF_PACKET regardless; see req.LinkShowDump.
	family uint8
	// showStats counts `-s`, mirroring iproute2's show_stats, which is an int
	// and not a bool because `-s -s` selects a wider render than `-s`
	// (ip/ip.c:232-234). goip implements 1 and rejects anything above it, so
	// by the time an object runs this is 0 or 1 — but it is counted rather
	// than set, because the rejection has to be able to tell the difference.
	showStats int
	// showDetails counts `-d`, mirroring iproute2's show_details. Unlike
	// show_stats no site reads it for a value above one, so goip implements
	// every count it accepts and rejects nothing — see the parsing arm in Run,
	// and detailed for the predicate the object handlers actually use.
	showDetails int
	// brief and oneline are -br and -o (ip/ip.c:238, :260). Only address
	// reads them; Run refuses them for every other object.
	brief   bool
	oneline bool
	// seq is the sequence number to put on the next request. iproute2 seeds it
	// from time(NULL) and increments per request; goip starts at 1 and
	// increments, because the parity comparator zeroes nlmsg_seq before
	// comparing and a wall-clock seed would only add noise to a capture.
	seq uint32
	// flushLoops is `ip -loops N`, max_flush_loops, when haveFlushLoops is
	// set; maxFlushLoops says what an unset one means.
	flushLoops     int64
	haveFlushLoops bool
	// now is the wall clock the one timestamp-rendering object (ntable's `-s`
	// config block) reads, so a replay can reproduce a captured date. Set once
	// in Run from GOIP_NOW (a test seam, like GOIP_REPLAY) or time.Now.
	now time.Time
}

// linkExtMask is the IFLA_EXT_MASK an `ip link show` dump carries under this
// invocation's options: 0x09 normally, 0x01 under `-s`.
//
// It lives on runCtx rather than at the one call site because `-s` composes
// with every link-dump form, and because the single byte it chooses is the
// entire request-side difference between two commands the parity harness
// compares separately. See req.ExtMaskShow and req.ExtMaskStats.
func (c *runCtx) linkExtMask() uint32 {
	if c.showStats > 0 {
		return req.ExtMaskStats
	}
	return req.ExtMaskShow
}

// detailed is `show_details` as the object handlers want it: a predicate.
//
// It exists so that the count-versus-bool question is answered in one place.
// Every one of iproute2's nine readers tests `show_details` or `show_details >
// 0` and none tests for two, so a `-d -d` is a `-d` — the opposite of `-s -s`,
// which selects a different render and which goip therefore refuses.
func (c *runCtx) detailed() bool { return c.showDetails > 0 }

// stats is show_stats as a boolean, for the renderers whose only use of it is
// a presence test. Every `if (show_stats)` guard in iproute2 that goip
// reproduces tests for non-zero, not for a specific count; the one place the
// count matters is `-s -s`, which goip refuses at the option loop.
func (c *runCtx) stats() bool { return c.showStats > 0 }

// outputMode names the -brief or -oneline option this invocation set, or
// returns "" when it set neither. brief is named first because ip's renderers
// test brief before oneline.
func (c *runCtx) outputMode() string {
	switch {
	case c.brief:
		return optBriefCst
	case c.oneline:
		return optOnelineCst
	}
	return ""
}

// outputModeMsg is the refusal of -brief or -oneline for an object that
// does not render them, or "". Run prints it and Show returns it.
func outputModeMsg(c *runCtx, obj *object) string {
	if opt := c.outputMode(); opt != "" && !obj.outputModes {
		return "option " + opt + " is only implemented for address"
	}
	return ""
}

// nextSeq returns the sequence number for the next request.
func (c *runCtx) nextSeq() uint32 {
	c.seq++
	return c.seq
}
