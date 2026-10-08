package goip

import (
	"fmt"
	"strconv"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// nexthopShowVerbs are do_ipnh's listing synonyms (ip/ipnexthop.c:1465-1467),
// matched with matches() exactly as route and neigh are.
var nexthopShowVerbs = []string{"list", "show", "lst"}

// runNexthop is do_ipnh restricted to the listing path (ip/ipnexthop.c:1451).
//
// Bare `ip nexthop` is ipnh_list_flush(0, NULL, IPNH_LIST) — the same dump as
// `show` with no filter (:1453-1454) — so an empty verb is the listing, not an
// error. The add/replace/delete/get/flush verbs are write or single-get paths
// goip does not implement and are refused.
//
// The listing's own selectors (dev, id, groups, master, vrf, protocol, fdb at
// :1222-1256) are refused rather than parsed: no topology the harness builds
// exercises a filtered nexthop dump, so there is no captured request to
// reproduce. The bare dump is the one shape grounded by netlink_route_getnexthop.
func runNexthop(c *runCtx, args []string) error {
	if len(args) > 0 {
		verb := args[0]
		if !matchesAny(verb, nexthopShowVerbs) {
			return fmt.Errorf("nexthop %q: %w", verb, ErrNotImplemented)
		}
		args = args[1:]
	}
	if len(args) > 0 {
		// `id N` is a point GET (ip/ipnexthop.c:1241), not a filtered dump; the
		// other selectors (dev/master/vrf/groups/fdb/protocol) have no captured
		// request to reproduce and are refused.
		if args[0] == "id" {
			if len(args) < 2 {
				return fmt.Errorf("nexthop show id: missing id value: %w", ErrNotImplemented)
			}
			id, perr := strconv.ParseUint(args[1], 0, 32)
			if perr != nil {
				return fmt.Errorf("nexthop show id %q: invalid id: %w", args[1], ErrNotImplemented)
			}
			if len(args) > 2 {
				return fmt.Errorf("nexthop show id %q: %w", args[2], ErrNotImplemented)
			}
			return nexthopShowID(c, uint32(id))
		}
		return fmt.Errorf("nexthop show %q: %w", args[0], ErrNotImplemented)
	}
	return nexthopShow(c)
}

// nexthopShowID is `ip nexthop show id N` (ip/ipnexthop.c:1241 -> __ipnh_get_id):
// a single RTM_GETNEXTHOP carrying NHA_ID, rendered by the same print_nexthop as
// the dump. A group id renders its group line; a resilient group is declined.
func nexthopShowID(c *runCtx, id uint32) error {
	if c.json {
		return fmt.Errorf("nexthop show id -json: %w", ErrNotImplemented)
	}

	svc := service.New(c.src, c.nextSeq)
	nh, err := svc.NexthopByID(c.family, id)
	if err != nil {
		return err
	}
	resolveIndexName(c, svc, nh.OIF)
	line, rerr := render.NexthopText(xtcpnl.NexthopInfo(nh), c.detailed(), c.lltab)
	if rerr != nil {
		return fmt.Errorf("nexthop id %d: %v: %w", nh.ID, rerr, ErrNotImplemented)
	}
	if _, werr := fmt.Fprintln(c.out, line); werr != nil {
		return werr
	}
	return nil
}

// nexthopShow is ipnh_list_flush's dump-and-print for the bare command: the
// RTM_GETNEXTHOP dump, then print_nexthop per object. nh_oif is resolved to a
// name the way print_rta_ifidx's ll_index_to_name does — a per-index link
// single-get on a cache miss (ip/iproute.c:443) — which runs during the render,
// so it follows the dump just as it does on the wire.
func nexthopShow(c *runCtx) error {
	if c.json {
		// `ip nexthop show -json` has no captured sidecar, so there is no
		// ground truth to reproduce; refuse rather than emit an unverified shape.
		return fmt.Errorf("nexthop show -json: %w", ErrNotImplemented)
	}

	svc := service.New(c.src, c.nextSeq)
	nhs, err := svc.Nexthops(c.family)
	if err != nil {
		return err
	}
	for i := range nhs {
		resolveIndexName(c, svc, nhs[i].OIF)
	}
	for i := range nhs {
		line, rerr := render.NexthopText(xtcpnl.NexthopInfo(nhs[i]), c.detailed(), c.lltab)
		if rerr != nil {
			return fmt.Errorf("nexthop id %d: %v: %w", nhs[i].ID, rerr, ErrNotImplemented)
		}
		if _, werr := fmt.Fprintln(c.out, line); werr != nil {
			return werr
		}
	}
	return nil
}
