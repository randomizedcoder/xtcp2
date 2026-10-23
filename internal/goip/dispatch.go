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
	// alias marks a second spelling of another row's object (the British
	// spelling of neighbor, ntbl), so lists of objects can name each one once.
	alias bool
	// showVerb is the verb Show runs, where it is not show: netns's show
	// is a directory listing, and list-id its netlink read.
	showVerb string
	// noShow marks an object with no read that ends: monitor prints until
	// it is killed, so Show, and the HTTP paths over it, leave it out.
	noShow bool
}

// The object names other tables spell too: the monitor's object words.
const (
	objRouteCst   = "route"
	objLinkCst    = "link"
	objNexthopCst = "nexthop"
)

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
	{name: addressObjectCst, run: runAddress, outputModes: true},
	{name: "addrlabel", run: runAddrLabel},
	{name: "maddress"},
	{name: objRouteCst, run: runRoute},
	{name: "rule", run: runRule},
	{name: "neighbor", run: runNeigh},
	{name: "neighbour", run: runNeigh, alias: true},
	{name: "ntable", run: runNTable},
	{name: "ntbl", run: runNTable, alias: true},
	{name: objLinkCst, run: runLink},
	{name: "l2tp"},
	{name: "fou"},
	{name: "ila"},
	{name: "macsec"},
	{name: "tunnel"},
	{name: "tunl6"},
	{name: "tcp_metrics", run: runTCPMetrics},
	{name: "tcpmetrics", run: runTCPMetrics, alias: true},
	{name: "token", run: runToken},
	{name: "tuntap"},
	{name: "tap", alias: true},
	{name: "mroute", run: runMroute},
	{name: "mrule", run: runMrule},
	{name: "netns", run: runNetns, showVerb: netnsListIDVerbCst},
	{name: "netconf", run: runNetconf},
	{name: "vrf", run: runVrf},
	{name: "sr", run: runSr, noShow: true},
	{name: objNexthopCst, run: runNexthop},
	{name: "mptcp"},
	{name: "ioam", run: runIoam, noShow: true},
	{name: "stats", run: runStats},
	{name: "monitor", run: runMonitor, noShow: true},
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

// matchesAny is matchesPrefix against a set, for the places where iproute2
// writes a chain of `matches(*argv, …) == 0 ||` over synonyms for one verb.
//
// It exists so a synonym list is data rather than a boolean expression that
// has to be edited in two places. The neigh listing is the case in point:
// ip/ipneigh.c:758-760 accepts show, list AND lst, and goip carried only the
// first two for as long as the test was a hand-written pair of conditions.
func matchesAny(arg string, patterns []string) bool {
	for _, p := range patterns {
		if matchesPrefix(arg, p) {
			return true
		}
	}
	return false
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

// addressObjectCst is the address object's name, which its dump file's
// terminal refusal names too.
const addressObjectCst = "address"

// oifKeywordCst is `oif`, the synonym route show, route get and the route
// writes accept for `dev`.
const oifKeywordCst = "oif"

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
	for _, o := range objects {
		if matchesPrefix(arg, o.name) {
			if o.run == nil {
				return o, fmt.Errorf("%q: %w", o.name, ErrNotImplemented)
			}
			return o, nil
		}
	}
	return object{}, fmt.Errorf("%q: %w", arg, ErrUnknownObject)
}

// runCtx is the state one goip invocation shares between dispatch and the
// object handlers.
type runCtx struct {
	src   Source
	lltab *LLTab
	// in is stdin, which only the showdump and restore verbs read.
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
	// genlIDs is the generic netlink family ids resolved so far, by name:
	// genl_init_handle asks once per family per run. See genlFamily.
	genlIDs map[string]uint16
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
	// timestamp and timestampShort count -timestamp and -tshort, which
	// raises both (ip/ip.c:241-245). Only the monitor's print_headers
	// reads them; every other object ignores them, as `ip`'s does.
	timestamp      int
	timestampShort int
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
		return "-brief"
	case c.oneline:
		return "-oneline"
	}
	return ""
}

// nextSeq returns the sequence number for the next request.
func (c *runCtx) nextSeq() uint32 {
	c.seq++
	return c.seq
}
