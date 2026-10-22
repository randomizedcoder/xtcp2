package goip

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"

	"github.com/randomizedcoder/xtcp2/internal/goip/model"
	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// nexthopSelectors is the subset of ipnh_list_flush's filter (ip/ipnexthop.c:1203)
// goip grounds: the wire-filter selectors dev/master/vrf/groups/fdb and the
// client-side protocol filter. `id` is a point GET handled before this.
type nexthopSelectors struct {
	// names holds the dev/master/vrf selectors in argv order. ipnh_list_flush
	// resolves each name as it parses it, so the gets go out in that order,
	// and a later master or vrf replaces an earlier one's filter.master.
	names       []nexthopName
	groups, fdb bool
	proto       uint8
	protoSet    bool
}

// nexthopName is one dev, master or vrf selector and its name.
type nexthopName struct {
	keyword string // "dev", "master" or vrfKeywordCst
	name    string
}

// runNexthop is do_ipnh restricted to the listing path (ip/ipnexthop.c:1437).
//
// Bare `ip nexthop` is ipnh_list_flush(0, NULL, IPNH_LIST) — the same dump as
// `show` with no filter (:1439-1440) — so an empty verb is the listing, not an
// error. The add/replace/delete/get/flush verbs are write or single-get paths
// goip does not implement and are refused.
//
// `id N` is a point GET (:1227), handled before the dump selectors. The rest of
// the SELECTOR grammar (dev/master/vrf/groups/fdb and the client-side protocol
// filter, :1207-1244) is parsed by parseNexthopShowArgs and grounded by the
// per-selector captures; an unknown selector is refused rather than answered
// with an unfiltered dump.
func runNexthop(c *runCtx, args []string) error {
	args, err := showArgs("nexthop", nexthopVerbs, args)
	if err != nil {
		return err
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

// parseNexthopShowArgs is the ipnh_list_flush selector loop (ip/ipnexthop.c:1207),
// each keyword matched with matches() as iproute2 does. dev/master/vrf take a
// name; groups/fdb are flags; protocol takes a NUMBER (get_unsigned at :1234, not
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
	addName := func(i *int, keyword string) error {
		name, err := takeName(i)
		if err == nil {
			sel.names = append(sel.names, nexthopName{keyword: keyword, name: name})
		}
		return err
	}
	for i := 0; i < len(args); i++ {
		var err error
		switch {
		case matchesPrefix(args[i], "dev"):
			err = addName(&i, "dev")
		case matchesPrefix(args[i], "groups"):
			sel.groups = true
		case matchesPrefix(args[i], "master"):
			err = addName(&i, "master")
		case matchesPrefix(args[i], vrfKeywordCst):
			err = addName(&i, vrfKeywordCst)
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

// nexthopShowID is `ip nexthop show id N` (ip/ipnexthop.c:1227 -> __ipnh_get_id):
// a single RTM_GETNEXTHOP carrying NHA_ID, rendered by the same print_nexthop as
// the dump. A group id renders its group line; a resilient group is declined.
func nexthopShowID(c *runCtx, id uint32) error {
	svc := service.New(c.src, c.nextSeq)
	nh, err := svc.NexthopByID(c.family, id)
	if err != nil {
		return err
	}
	resolveIndexName(c, svc, nh.OIF)
	if c.json {
		// ip -j nexthop show id N keeps the dump's array wrapper around the one
		// object (ip_nexthop_id_json), so encode a one-element slice.
		view, verr := render.NexthopViewOf(xtcpnl.NexthopInfo(nh), c.detailed(), c.lltab)
		if verr != nil {
			return fmt.Errorf("nexthop id %d: %v: %w", nh.ID, verr, ErrNotImplemented)
		}
		return json.NewEncoder(c.out).Encode([]render.NexthopView{view})
	}
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
// dev/master/vrf append an NHA_OIF/NHA_MASTER wire filter, so each name is
// resolved first, in argv order, by nexthopNameToIndex. ipnexthop.c has no
// ll_init_map, so there is no link dump. vrf validates the name is a VRF
// beforehand (name_is_vrf). groups/fdb append a flag and take no name.
// protocol is a client-side filter over the reply, so it sends the bare dump
// and drops non-matching entries after render resolution.
func nexthopShow(c *runCtx, sel nexthopSelectors) error {
	svc := service.New(c.src, c.nextSeq)

	var filter xtcpnl.NexthopDumpFilter
	for _, n := range sel.names {
		if n.keyword == vrfKeywordCst {
			// name_is_vrf (ip/iplink_vrf.c:170): a link single-get by name, with
			// no ext mask, whose IFLA_INFO_KIND must be "vrf". It is not
			// LinkByName: that is ll_link_get, whose 7.2.0 mask carries
			// RTEXT_FILTER_NAME_ONLY, and a Linux 7.2+ kernel answers it
			// without IFLA_LINKINFO, so every VRF would read as not one.
			isVRF, lerr := svc.IsVRF(c.family, n.name)
			if lerr != nil {
				return fmt.Errorf("nexthop show vrf %q: %w", n.name, lerr)
			}
			if !isVRF {
				return fmt.Errorf("nexthop show: %q is not a VRF", n.name)
			}
		}
		idx, err := nexthopNameToIndex(c, svc, n.name)
		if err != nil {
			return err
		}
		if n.keyword == "dev" {
			filter.OIF = idx
		} else {
			filter.Master = idx
		}
	}
	filter.Groups = sel.groups
	filter.Fdb = sel.fdb

	var nhs []model.Nexthop
	var err error
	if len(sel.names) > 0 || sel.groups || sel.fdb {
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

	var views []render.NexthopView
	for i := range nhs {
		// Client-side protocol filter (print_cache_nexthop, ip/ipnexthop.c:815):
		// proto 0 is unspec and disables the filter, matching `if (filter.proto &&
		// ...)`.
		if sel.proto != 0 && nhs[i].Protocol != sel.proto {
			continue
		}
		if c.json {
			view, verr := render.NexthopViewOf(xtcpnl.NexthopInfo(nhs[i]), c.detailed(), c.lltab)
			if verr != nil {
				return fmt.Errorf("nexthop id %d: %v: %w", nhs[i].ID, verr, ErrNotImplemented)
			}
			views = append(views, view)
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
	if c.json {
		if views == nil {
			views = []render.NexthopView{}
		}
		return json.NewEncoder(c.out).Encode(views)
	}
	return nil
}

// nexthopNameToIndex is ll_name_to_index (lib/ll_map.c:354-372). A name the
// cache already holds as a primary name sends nothing, as ll_get_by_name
// matches only im->name; any other name sends ll_link_get, whose reply fills
// the cache. goip has neither of `ip`'s fallbacks after a failed get,
// if_nametoindex or the `if%u` spelling, because both answer without asking
// the kernel.
func nexthopNameToIndex(c *runCtx, svc *service.Service, name string) (uint32, error) {
	if idx := c.lltab.NameToIndex(name); idx != 0 && c.lltab.IndexToName(idx) == name {
		return uint32(idx), nil
	}
	resolved, err := svc.LinkByName(name)
	if err != nil {
		var errno unix.Errno
		if errors.As(err, &errno) {
			return 0, noSuchDevice(fmt.Errorf("nexthop show: cannot find device %q", name))
		}
		return 0, fmt.Errorf("nexthop show: %w", err)
	}
	c.lltab.Fill([]xtcpnl.LinkInfo{xtcpnl.LinkInfo(resolved)})
	return uint32(resolved.Index), nil
}
