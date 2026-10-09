package goip

import (
	"fmt"
	"strconv"

	"github.com/randomizedcoder/xtcp2/internal/goip/model"
	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// nexthopShowVerbs are do_ipnh's listing synonyms (ip/ipnexthop.c:1465-1467),
// matched with matches() exactly as route and neigh are.
var nexthopShowVerbs = []string{"list", "show", "lst"}

// nexthopSelectors is the subset of ipnh_list_flush's filter (ip/ipnexthop.c:1217)
// goip grounds: the wire-filter selectors dev/master/vrf/groups/fdb and the
// client-side protocol filter. `id` is a point GET handled before this.
type nexthopSelectors struct {
	dev, master, vrf          string
	devSet, masterSet, vrfSet bool
	groups, fdb               bool
	proto                     uint8
	protoSet                  bool
}

// runNexthop is do_ipnh restricted to the listing path (ip/ipnexthop.c:1451).
//
// Bare `ip nexthop` is ipnh_list_flush(0, NULL, IPNH_LIST) — the same dump as
// `show` with no filter (:1453-1454) — so an empty verb is the listing, not an
// error. The add/replace/delete/get/flush verbs are write or single-get paths
// goip does not implement and are refused.
//
// `id N` is a point GET (:1241), handled before the dump selectors. The rest of
// the SELECTOR grammar (dev/master/vrf/groups/fdb and the client-side protocol
// filter, :1222-1256) is parsed by parseNexthopShowArgs and grounded by the
// per-selector captures; an unknown selector is refused rather than answered
// with an unfiltered dump.
func runNexthop(c *runCtx, args []string) error {
	if len(args) > 0 {
		verb := args[0]
		if !matchesAny(verb, nexthopShowVerbs) {
			return fmt.Errorf("nexthop %q: %w", verb, ErrNotImplemented)
		}
		args = args[1:]
	}
	if len(args) > 0 && args[0] == "id" {
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
	sel, err := parseNexthopShowArgs(args)
	if err != nil {
		return err
	}
	return nexthopShow(c, sel)
}

// parseNexthopShowArgs is the ipnh_list_flush selector loop (ip/ipnexthop.c:1222),
// each keyword matched with matches() as iproute2 does. dev/master/vrf take a
// name; groups/fdb are flags; protocol takes a NUMBER (get_unsigned at :1248, not
// a proto name, so `protocol static` is an error and `protocol 4` is RTPROT_STATIC).
func parseNexthopShowArgs(args []string) (nexthopSelectors, error) {
	var sel nexthopSelectors
	takeName := func(i *int) (string, error) {
		if *i+1 >= len(args) {
			return "", fmt.Errorf("nexthop show: argument %q is missing its value", args[*i])
		}
		*i++
		return args[*i], nil
	}
	for i := 0; i < len(args); i++ {
		var err error
		switch {
		case matchesPrefix(args[i], "dev"):
			sel.dev, err = takeName(&i)
			sel.devSet = err == nil
		case matchesPrefix(args[i], "groups"):
			sel.groups = true
		case matchesPrefix(args[i], "master"):
			sel.master, err = takeName(&i)
			sel.masterSet = err == nil
		case matchesPrefix(args[i], "vrf"):
			sel.vrf, err = takeName(&i)
			sel.vrfSet = err == nil
		case matchesPrefix(args[i], "protocol"):
			var v string
			if v, err = takeName(&i); err == nil {
				p, perr := strconv.ParseUint(v, 0, 8)
				if perr != nil {
					return sel, fmt.Errorf("nexthop show protocol %q: invalid protocol value", v)
				}
				sel.proto, sel.protoSet = uint8(p), true
			}
		case matchesPrefix(args[i], "fdb"):
			sel.fdb = true
		default:
			return sel, fmt.Errorf("nexthop show %q: %w", args[i], ErrNotImplemented)
		}
		if err != nil {
			return sel, err
		}
	}
	return sel, nil
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

// nexthopShow is ipnh_list_flush's dump-and-print: the RTM_GETNEXTHOP dump, then
// print_nexthop per object. nh_oif is resolved to a name the way
// print_rta_ifidx's ll_index_to_name does — a per-index link single-get on a
// cache miss (ip/iproute.c:443) during the render, so it follows the dump just as
// it does on the wire.
//
// dev/master/vrf append an NHA_OIF/NHA_MASTER wire filter and so first send
// ll_init_map (which also resolves the name and pre-fills the index cache);
// vrf validates the name is a VRF beforehand (name_is_vrf). groups/fdb append a
// flag and take no name. protocol is a client-side filter over the reply, so it
// sends the bare dump and drops non-matching entries after render resolution.
func nexthopShow(c *runCtx, sel nexthopSelectors) error {
	if c.json {
		// `ip nexthop show -json` has no captured sidecar, so there is no
		// ground truth to reproduce; refuse rather than emit an unverified shape.
		return fmt.Errorf("nexthop show -json: %w", ErrNotImplemented)
	}

	svc := service.New(c.src, c.nextSeq)

	var filter xtcpnl.NexthopDumpFilter
	resolveName := sel.devSet || sel.masterSet || sel.vrfSet
	if resolveName {
		if sel.vrfSet {
			// name_is_vrf (ip/iplink_vrf.c:170): a link single-get by name whose
			// IFLA_INFO_KIND must be "vrf", sent before ll_init_map.
			lk, lerr := svc.LinkByName(sel.vrf)
			if lerr != nil {
				return fmt.Errorf("nexthop show vrf %q: %w", sel.vrf, lerr)
			}
			if lk.Kind != "vrf" {
				return fmt.Errorf("nexthop show: %q is not a VRF", sel.vrf)
			}
		}
		// ll_init_map: the link dump, which is also the name resolution.
		links, lerr := svc.NexthopShowLinks()
		if lerr != nil {
			return lerr
		}
		decoded := make([]xtcpnl.LinkInfo, len(links))
		for i := range links {
			decoded[i] = xtcpnl.LinkInfo(links[i])
		}
		c.lltab.Fill(decoded)

		name, isDev := sel.dev, true
		if !sel.devSet {
			isDev, name = false, sel.master
			if !sel.masterSet {
				name = sel.vrf
			}
		}
		idx := c.lltab.NameToIndex(name)
		if idx == 0 {
			return fmt.Errorf("nexthop show: cannot find device %q", name)
		}
		if isDev {
			filter.OIF = uint32(idx)
		} else {
			filter.Master = uint32(idx)
		}
	}
	filter.Groups = sel.groups
	filter.Fdb = sel.fdb

	var nhs []model.Nexthop
	var err error
	if resolveName || sel.groups || sel.fdb {
		nhs, err = svc.NexthopsFiltered(c.family, filter)
	} else {
		nhs, err = svc.Nexthops(c.family)
	}
	if err != nil {
		return err
	}
	for i := range nhs {
		resolveIndexName(c, svc, nhs[i].OIF)
	}
	for i := range nhs {
		// Client-side protocol filter (print_cache_nexthop, ip/ipnexthop.c:821):
		// proto 0 is unspec and disables the filter, matching `if (filter.proto &&
		// ...)`.
		if sel.proto != 0 && nhs[i].Protocol != sel.proto {
			continue
		}
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
