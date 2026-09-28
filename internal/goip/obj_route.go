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
	table, err := parseRouteShowArgs(args)
	if err != nil {
		return err
	}
	return routeShow(c, table)
}

// routeTableMainCst is filter.tb's default, set at ip/iproute.c:1835 before
// any argument is read. It is why `ip route show` shows one table and why no
// line of ip_route_main carries a `table` token.
const routeTableMainCst uint32 = unix.RT_TABLE_MAIN

// parseRouteShowArgs walks the selectors after the verb, returning filter.tb.
//
// `table` is the only selector implemented. The others — dev, via, proto,
// scope, type, root, match, exact, src, metric, vrf, cached — are rejected
// rather than ignored: the harness drives `ip` and `goip` with the same argv,
// so a silently dropped filter would have the two tools answering different
// questions while the comparator reported a stdout divergence it could not
// explain.
//
// Repeating `table` is last-wins, because iproute2's loop simply assigns
// filter.tb again (ip/iproute.c:1843-1858). That is worth pinning rather than
// leaving unspecified: `route show table all table main` is one table filter in
// `ip`, not an error and not an intersection.
func parseRouteShowArgs(args []string) (uint32, error) {
	table := routeTableMainCst
	for i := 0; i < len(args); i++ {
		if !matchesPrefix(args[i], "table") {
			return 0, fmt.Errorf("route show %q: %w", args[i], ErrNotImplemented)
		}
		// NEXT_ARG() (include/utils.h), which exits with a usage error when the
		// keyword is the last argument.
		if i+1 >= len(args) {
			return 0, fmt.Errorf("route show table: argument expected: %w", ErrNotImplemented)
		}
		i++
		id, err := routeTableID(args[i])
		if err != nil {
			return 0, err
		}
		table = id
	}
	return table, nil
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

// routeShow runs the dump, resolves the device names it references, and
// renders.
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
func routeShow(c *runCtx, table uint32) error {
	dumpFamily := c.family
	if dumpFamily == unix.AF_UNSPEC && table != unix.RT_TABLE_UNSPEC {
		dumpFamily = unix.AF_INET
	}

	svc := service.New(c.src, c.nextSeq)
	routes, err := svc.Routes(dumpFamily, table)
	if err != nil {
		return err
	}
	routes = routeFilter(routes, c.family, table)
	resolveRouteNames(c, svc, routes)

	f := render.RouteShowFilter{Table: table}
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
// Three rules, in source order because the second one depends on it:
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
func routeFilter(in []model.Route, preferredFamily uint8, table uint32) []model.Route {
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
		out = append(out, *r)
	}
	return out
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
func resolveRouteNames(c *runCtx, svc *service.Service, routes []model.Route) {
	for i := range routes {
		resolveRouteIndex(c, svc, routes[i].Oif)
		resolveRouteIndex(c, svc, routes[i].Iif)
		for j := range routes[i].Multipath {
			resolveRouteIndex(c, svc, uint32(routes[i].Multipath[j].Ifindex))
		}
	}
}

// resolveRouteIndex is ll_index_to_name's cache-miss path. Index 0 is not a
// device — ll_index_to_name answers "*" for it without asking the kernel — and
// a cached index is answered from the cache.
func resolveRouteIndex(c *runCtx, svc *service.Service, idx uint32) {
	if idx == 0 || c.lltab.Cached(int32(idx)) {
		return
	}
	link, err := svc.LinkByIndex(int32(idx))
	if err != nil {
		// See the doc comment above: nothing is cached, and IndexToName's
		// `if%u` fallback renders the token.
		return
	}
	c.lltab.Fill([]xtcpnl.LinkInfo{xtcpnl.LinkInfo(link)})
}
