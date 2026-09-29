package goip

import (
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/randomizedcoder/xtcp2/internal/goip/model"
	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// runRoute handles `ip route ...`. Only the list verbs are implemented.
//
// do_iproute (ip/iproute.c:2418-2462) tests its verbs in a fixed order with
// matches(), and the order decides what an abbreviation means:
//
//	l    -> list,  because no earlier verb starts with 'l'
//	s    -> show,  because the list group precedes "save"
//	sa   -> save,  which is NOT show — "sa" is not a prefix of "show"
//	ls   -> lst,   the third spelling of list, and the only one "ls" matches
//
// goip implements list/show/lst and rejects everything else, including the
// read-only-but-unimplemented get/save/showdump, with ErrNotImplemented. That
// is a deliberate narrowing of iproute2's two outcomes — it says "command
// unknown" for a typo and runs the verb otherwise — into the one the parity
// harness can act on. A bare `ip route` with no verb is a list
// (ip/iproute.c:2420).
//
// Note that a selector without a verb, `ip route table all`, is an error in
// `ip` too: do_iproute matches "table" against no verb and exits. It arrives
// here as ErrNotImplemented rather than as an unknown command, which is the
// same collapsing.
func runRoute(c *runCtx, args []string) error {
	if len(args) > 0 {
		verb := args[0]
		if !matchesPrefix(verb, "list") && !matchesPrefix(verb, "show") && !matchesPrefix(verb, "lst") {
			return fmt.Errorf("route %q: %w", verb, ErrNotImplemented)
		}
		args = args[1:]
	}
	sel, err := parseRouteShowArgs(args)
	if err != nil {
		return err
	}
	return routeShow(c, sel)
}

// routeSelectors is the subset of iproute2's `struct filter` that
// `ip route show` fills from argv and that goip implements.
//
// Dev is the device NAME, not an index, because that is as far as the parser
// can get: resolving it costs a netlink round trip (ll_name_to_index →
// ll_link_get) and iproute2 defers it until after the whole argument loop has
// run (ip/iproute.c:2002-2017). Deferring it here too is what keeps an
// argument error — an unknown selector, a missing value — from being reported
// only after a request has already gone out.
type routeSelectors struct {
	// Table is filter.tb, defaulting to RT_TABLE_MAIN.
	Table uint32

	// Dev is iproute2's local `od`, the pending `dev`/`oif` name.
	Dev string

	// DevSet is `od != NULL`. It is a separate field rather than a
	// Dev != "" test because the two differ on `ip route show dev ""`, which
	// is a real command: `od` is non-NULL and empty, ll_name_to_index fails
	// all three of its lookups, and `ip` exits with `Cannot find device ""`
	// (ip/iproute.c:2008-2010, lib/ll_map.c:354-372). Collapsing the two
	// would turn that into a silent unfiltered dump.
	DevSet bool
}

// routeTableMainCst is filter.tb's default, set at ip/iproute.c:1835 before
// any argument is read. It is why `ip route show` shows one table and why no
// line of ip_route_main carries a `table` token.
const routeTableMainCst uint32 = unix.RT_TABLE_MAIN

// parseRouteShowArgs walks the selectors after the verb.
//
// `table` and the `dev`/`oif` pair are the only selectors implemented. The
// others — via, proto, scope, type, root, match, exact, src, metric, vrf,
// cached, iif, mark — are rejected rather than ignored: the harness drives
// `ip` and `goip` with the same argv, so a silently dropped filter would have
// the two tools answering different questions while the comparator reported a
// stdout divergence it could not explain.
//
// # Two keyword-matching rules in one loop, and the difference is observable
//
// `table` is matched with matches(), so every non-empty prefix of it works —
// `t`, `ta`, `tab` (ip/iproute.c:1844). `dev` and `oif` are matched with
// strcmp (:1911-1913), so they do not abbreviate at all. That is not a
// cosmetic difference: an unrecognized token falls through to the else-arm at
// :2158, which reads it as a destination prefix, so `ip route show d eth0` is
// a malformed ADDRESS and not a device filter. goip reproduces the split —
// matchesPrefix for `table`, == for the other two — and refuses the abbreviated
// forms rather than accepting a spelling `ip` would reject.
//
// # Repetition, and which of the two is last-wins
//
// Both are. iproute2's loop simply assigns filter.tb (:1843-1858) or `od`
// (:1911-1914) again, with no accumulation and no error, so
// `route show table all table main` is one table filter and
// `route show dev a dev b` is one device filter. Worth pinning rather than
// leaving unspecified.
//
// `dev` and `oif` are exact synonyms in the same else-if, so mixing them
// (`dev a oif b`) is also last-wins — they are not two independent filters.
func parseRouteShowArgs(args []string) (routeSelectors, error) {
	sel := routeSelectors{Table: routeTableMainCst}
	for i := 0; i < len(args); i++ {
		switch {
		case matchesPrefix(args[i], "table"):
			// NEXT_ARG() (include/utils.h), which exits with a usage error
			// when the keyword is the last argument.
			if i+1 >= len(args) {
				return routeSelectors{}, fmt.Errorf("route show table: argument expected: %w", ErrNotImplemented)
			}
			i++
			id, err := routeTableID(args[i])
			if err != nil {
				return routeSelectors{}, err
			}
			sel.Table = id
		case args[i] == devKeywordCst, args[i] == "oif":
			kw := args[i]
			if i+1 >= len(args) {
				return routeSelectors{}, fmt.Errorf("route show %s: argument expected: %w", kw, ErrNotImplemented)
			}
			i++
			sel.Dev, sel.DevSet = args[i], true
		default:
			return routeSelectors{}, fmt.Errorf("route show %q: %w", args[i], ErrNotImplemented)
		}
	}
	return sel, nil
}

// routeTableID is rtnl_rttable_a2n (lib/rt_names.c:552-597) plus the three
// spellings iproute2 handles only after it fails (ip/iproute.c:1848-1856).
//
// "all" and "0" both mean no filter and reach the same place by different
// routes: "0" parses as a table id, "all" does not and is caught by the
// fallback. "cache" and "help" are the other two fallbacks; goip has neither,
// so they are refused by name rather than being silently read as a bad id.
//
// The upper bound is RT_TABLE_MAX = 0xFFFFFFFF, so every uint32 is a legal
// table. Only the low byte fits rtm_table, which is exactly why the request
// carries RTA_TABLE; see xtcpnl.BuildDumpRouteRequestTable.
func routeTableID(arg string) (uint32, error) {
	switch arg {
	case "default":
		return unix.RT_TABLE_DEFAULT, nil
	case "main":
		return unix.RT_TABLE_MAIN, nil
	case "local":
		return unix.RT_TABLE_LOCAL, nil
	case "all":
		return unix.RT_TABLE_UNSPEC, nil
	case "cache", "help":
		return 0, fmt.Errorf("route show table %q: %w", arg, ErrNotImplemented)
	}
	// strtoul(arg, &end, 0): base 0, so 0x-prefixed and 0-prefixed forms are
	// accepted as well as decimal.
	id, err := strconv.ParseUint(arg, 0, 32)
	if err != nil {
		// Wrapped, not swallowed: strconv distinguishes ErrSyntax from
		// ErrRange, and "table 4294967296" is a different user mistake from
		// "table wombat".
		return 0, fmt.Errorf("goip: invalid table id %q: %w", arg, err)
	}
	return uint32(id), nil
}

// routeShow resolves the device selector if there is one, runs the dump,
// resolves the device names the replies reference, and renders.
//
// # The family promotion, which is not the same as the -4/-6 option
//
// `dump_family = preferred_family`, and then
// `if (dump_family == AF_UNSPEC && filter.tb) dump_family = AF_INET`
// (ip/iproute.c:2000-2001). So plain `ip route show` asks the kernel for
// AF_INET while `ip route show table all` asks for AF_UNSPEC and gets both
// families. The promotion applies ONLY to the request: the client-side family
// filter in routeFilter still uses preferred_family, which is still AF_UNSPEC,
// so it drops nothing.
//
// The promotion is decided by the TABLE alone, and `dev NAME` does not enter
// it — `ip route show dev goip0` is (AF_INET, 254, idx) and
// `ip route show table all dev goip0` is (AF_UNSPEC, 0, idx). That is a real
// consequence rather than a curiosity: the second form is the only way to see
// an interface's v6 routes from this command without `-6`.
//
// # Where the name resolution goes, and why it is before the dump
//
// iproute2 resolves `od` after the argument loop and before
// rtnl_routedump_req (:2002-2021), so the throwaway ll_link_get is
// transaction one and the dump is transaction two. The order is observable on
// the wire and the parity comparator compares positionally, so it is
// reproduced rather than reordered — even though nothing here would break if
// the dump went first.
//
// One consequence of ll_name_to_index that goip inherits: the resolution get
// fills the index cache, so the name is already known by render time. goip
// does not exploit that — resolveRouteNames skips the OIF index for a
// different reason, below — but a `dev NAME` route whose RTA_IIF happened to
// be the same interface would be answered from the cache in both tools.
func routeShow(c *runCtx, sel routeSelectors) error {
	dumpFamily := c.family
	if dumpFamily == unix.AF_UNSPEC && sel.Table != unix.RT_TABLE_UNSPEC {
		dumpFamily = unix.AF_INET
	}

	svc := service.New(c.src, c.nextSeq)

	var oif uint32
	if sel.DevSet {
		// ll_name_to_index (lib/ll_map.c:354-372). `ip` falls back to
		// if_nametoindex and then to the `if%u` spelling when the get fails;
		// goip has neither fallback and reports the error, because both
		// fallbacks resolve a name without asking the kernel and so would make
		// goip answer where `ip` would have sent a request the capture
		// records.
		resolved, rerr := svc.LinkByName(sel.Dev)
		if rerr != nil {
			return rerr
		}
		c.lltab.Fill([]xtcpnl.LinkInfo{xtcpnl.LinkInfo(resolved)})
		oif = uint32(resolved.Index)
	}

	routes, err := svc.Routes(dumpFamily, sel.Table, oif)
	if err != nil {
		return err
	}
	routes = routeFilter(routes, c.family, sel.Table, oif)
	resolveRouteNames(c, svc, routes, oif != 0)

	f := render.RouteShowFilter{Table: sel.Table, OifMask: oif != 0}
	views := make([]render.RouteView, 0, len(routes))
	for i := range routes {
		views = append(views, render.RouteViewOf(xtcpnl.RouteInfo(routes[i]), c.lltab, f))
	}
	if c.json {
		return json.NewEncoder(c.out).Encode(views)
	}
	for i := range views {
		if _, werr := fmt.Fprint(c.out, views[i].Text()); werr != nil {
			return werr
		}
	}
	return nil
}

// routeFilter is the part of filter_nlmsg (ip/iproute.c:176-366) that a `show`
// with no selectors other than `table` still applies. It is not redundant with
// the request: the kernel answers a table-filtered dump on a best-effort basis
// and `ip` re-checks every reply.
//
// Four rules, in source order because the second one depends on it:
//
//   - preferred_family, the -4/-6 option, drops a reply of the other family.
//     This is the UNPROMOTED family; see routeShow.
//   - ip6_multiple_tables is latched the first time an IPv6 route arrives
//     carrying a table other than main, and it changes how the table filter is
//     applied to every IPv6 route AFTER it. The ordering dependence is real
//     and is reproduced rather than tidied: on a kernel without
//     CONFIG_IPV6_MULTIPLE_TABLES the v6 tables do not exist, so `ip` emulates
//     `table local` and `table main` by route type and refuses every other
//     table outright.
//   - a cloned route is dropped, which is the inverse of `route show cache`.
//     `filter.cloned == !(rtm_flags & RTM_F_CLONED)` reads backwards: with
//     filter.cloned zero it is true exactly when the route IS cloned.
//   - oif, when a `dev`/`oif` selector was given. See routeMatchesOif for the
//     one shape this does NOT drop.
func routeFilter(in []model.Route, preferredFamily uint8, table, oif uint32) []model.Route {
	out := make([]model.Route, 0, len(in))
	var ip6MultipleTables bool
	for i := range in {
		r := &in[i]
		if preferredFamily != unix.AF_UNSPEC && r.Family != preferredFamily {
			continue
		}
		if r.Family == unix.AF_INET6 && r.Table != unix.RT_TABLE_MAIN {
			ip6MultipleTables = true
		}
		if r.Flags&unix.RTM_F_CLONED != 0 {
			continue
		}
		if r.Family == unix.AF_INET6 && !ip6MultipleTables {
			if table != unix.RT_TABLE_UNSPEC {
				switch table {
				case unix.RT_TABLE_LOCAL:
					if r.Type != unix.RTN_LOCAL {
						continue
					}
				case unix.RT_TABLE_MAIN:
					if r.Type == unix.RTN_LOCAL {
						continue
					}
				default:
					continue
				}
			}
		} else if table != unix.RT_TABLE_UNSPEC && table != r.Table {
			continue
		}
		// After the table block, not before it: the oif test sits at
		// ip/iproute.c:331, downstream of the ip6_multiple_tables latch at
		// :191. A route dropped here has already had its chance to set the
		// latch, and moving the test earlier would change how every LATER
		// IPv6 route is filtered.
		if oif != 0 && !routeMatchesOif(r, oif) {
			continue
		}
		out = append(out, *r)
	}
	return out
}

// routeMatchesOif is filter_nlmsg's oif arm (ip/iproute.c:331-341) plus
// filter_multipath (:158-174).
//
// # The shape it does not drop, which is the whole reason this is a function
//
// C's test is an if/else-if over two attributes:
//
//	if (tb[RTA_OIF])            { drop unless it matches }
//	else if (tb[RTA_MULTIPATH]) { drop unless some nexthop matches }
//
// A route with NEITHER — a blackhole, an unreachable, a prohibit, or a
// nexthop-object route carrying only RTA_NH_ID — matches no arm and therefore
// SURVIVES. `ip route show dev goip0` lists every blackhole route on the host
// alongside goip0's, and that is not a bug in iproute2 to be tidied away: the
// kernel applies RTA_OIF on the dump side too, so on a strict socket those
// replies do not arrive in the first place and the client-side arm never sees
// them. Reproducing the fall-through matters because goip's render tests feed
// this function a pcap directly, with no kernel in between.
//
// # Why the xor-and-mask is spelled as equality here
//
// C writes `(oif ^ filter.oif) & filter.oifmask` because oifmask is either 0
// or -1 and the same expression serves both. goip carries the mask as the
// oif != 0 test at the call site, so the surviving comparison is a plain
// equality — which is what the C reduces to when the mask is all-ones, the
// only value `dev NAME` can produce (:2015-2016).
func routeMatchesOif(r *model.Route, oif uint32) bool {
	if r.Oif != 0 {
		return r.Oif == oif
	}
	if len(r.Multipath) > 0 {
		for i := range r.Multipath {
			if uint32(r.Multipath[i].Ifindex) == oif {
				return true
			}
		}
		return false
	}
	return true
}

// resolveRouteNames fills the index cache the way `ip route show` fills it:
// lazily, one RTM_GETLINK single-get per index it has never seen.
//
// # Why this is not an up-front link dump, which would be simpler
//
// Because `ip route show` does not send one. iproute_list_flush_or_save
// (ip/iproute.c:1818) never calls ll_init_map — unlike `ip neigh show`, which
// does — and every name in the output is resolved during printing by
// ll_index_to_name, which on a cache miss calls ll_link_get(NULL, idx)
// (lib/ll_map.c:320,264). A dump instead of a get would keep the transaction
// COUNT plausible while sending nlmsg_flags 0x0301 and ifi_family AF_PACKET
// where `ip` sends 0x0001 and an ifi_index: identical output, and an L2
// request-equality failure. That is the assertion this command exists to
// exercise, so shortcutting it would defeat the point of implementing it.
//
// # Resolving up front rather than mid-render, and why the wire is unchanged
//
// The walk below is in print order — RTA_OIF, then RTA_IIF, then each
// multipath nexthop, for each route in dump order — which is exactly the order
// print_route asks for a name (ip/iproute.c:897, 986, 744). Netlink sees the
// same messages in the same sequence; only the interleaving with stdout
// differs, and stdout is not on the socket. Doing it here keeps the renderer a
// pure function of its inputs, which is what lets render/route_test.go run
// against a pcap with no service at all.
//
// # Two divergences, both documented rather than fixed
//
//   - **One socket, not one per lookup.** ll_link_get opens a fresh
//     rtnl_handle per call (lib/ll_map.c:280), so each of `ip`'s side-gets has
//     its own portid and its own time(NULL) seq seed; goip reuses its socket.
//     nlmon mirrors datagrams, not sockets, and the comparator normalizes
//     nlmsg_pid, so this is invisible on the wire — and goip's reused sequence
//     counter actually segments the transactions more cleanly than `ip`, whose
//     two sockets seeded in the same second collide.
//   - **The IFLA_EXT_MASK version skew.** Against the pinned iproute2 7.1.0
//     the mask is RTEXT_FILTER_VF|RTEXT_FILTER_SKIP_STATS = 0x09, which is
//     req.ExtMaskShow. The 7.2.0 reading reference sends 0x109, because commit
//     de91e928 added RTEXT_FILTER_NAME_ONLY (1 << 8) to both ll_link_get and
//     ll_init_map. A nixpkgs bump therefore breaks this locus on `route show`,
//     `link show dev` and `neigh show` at the same moment.
//
// A failed get is not cached, and that is also iproute2's behavior: when
// ll_link_get returns something other than the index it asked for,
// ll_index_to_name falls through to if_indextoname and then to `if%u` without
// remembering anything (lib/ll_map.c:321-327). So a second route on a
// vanished index sends a second get. Reproducing the retry matters more than
// saving it, because the retry is what a parity capture would show.
//
// # oifMask suppresses the RTA_OIF walk, and it is the render that decides
//
// `dev NAME` makes `ip` print no `dev` token at all: print_route's call is
// guarded by `if (tb[RTA_OIF] && filter.oifmask != -1)` (ip/iproute.c:900),
// because the device is already in the command line. print_rta_ifidx is the
// only thing that would have called ll_index_to_name for that index, so the
// suppressed token suppresses the netlink transaction with it — a RENDER
// decision that is visible on the wire. `route show dev NAME` is therefore
// two transactions where the bare form is one plus one per distinct index,
// and it gets SHORTER as the interface gets busier.
//
// Two things the guard does not reach, and both are deliberate:
//
//   - RTA_IIF has the mirror-image guard at :984 keyed on filter.iifmask, not
//     oifmask, so a `dev NAME` route that also carries RTA_IIF still resolves
//     it. goip has no `iif` selector, so iifmask is always 0 here and the
//     walk always runs.
//   - the multipath nexthops at :743 and :751 call ll_index_to_name with no
//     guard at all. A multipath route surviving an oif filter prints — and
//     resolves — every one of its nexthop devices, including the one named on
//     the command line. The gated topology has exactly one such route, and
//     the committed ip_route_dev sidecar shows the result: `dev goip0` on
//     none of the main lines and on both nexthops of 203.0.113.0/24.
func resolveRouteNames(c *runCtx, svc *service.Service, routes []model.Route, oifMask bool) {
	for i := range routes {
		if !oifMask {
			resolveIndexName(c, svc, int32(routes[i].Oif))
		}
		resolveIndexName(c, svc, int32(routes[i].Iif))
		for j := range routes[i].Multipath {
			resolveIndexName(c, svc, routes[i].Multipath[j].Ifindex)
		}
	}
}

// resolveIndexName is ll_index_to_name's cache-miss path. Index 0 is not a
// device — ll_index_to_name answers "*" for it without asking the kernel — and
// a cached index is answered from the cache.
//
// It lives in this file because `route show` is the command built around it,
// but it is not route-specific: linkShowDev calls it for the IFLA_MASTER and
// IFLA_LINK indexes print_linkinfo resolves the same way.
func resolveIndexName(c *runCtx, svc *service.Service, idx int32) {
	if idx == 0 || c.lltab.Cached(idx) {
		return
	}
	link, err := svc.LinkByIndex(idx)
	if err != nil {
		// See the doc comment above: nothing is cached, and IndexToName's
		// `if%u` fallback renders the token.
		return
	}
	c.lltab.Fill([]xtcpnl.LinkInfo{xtcpnl.LinkInfo(link)})
}
