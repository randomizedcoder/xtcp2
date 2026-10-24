package goip

import (
	"encoding/json"
	"fmt"

	"github.com/randomizedcoder/xtcp2/internal/goip/model"
	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// routeGetVerbCst is do_iproute's `get`, matched with matches() after the
// list verbs (ip/iproute.c:2447), so `g` is get.
const routeGetVerbCst = "get"

// routeGetState is iproute_get's locals (ip/iproute.c:2043-2065): the request
// it fills as it walks argv, and what it resolves after the walk.
type routeGetState struct {
	g            xtcpnl.RouteGet
	idev, odev   string
	idevSet      bool
	odevSet      bool
	mark         uint32
	fibMatch     bool
	addressFound bool
}

// routeGetArm is one arm of iproute_get's keyword chain.
type routeGetArm int

const (
	routeGetArmAddress routeGetArm = iota
	routeGetArmUnsupported
	routeGetArmFrom
	routeGetArmIif
	routeGetArmMark
	routeGetArmOif
	routeGetArmNotify
	routeGetArmUID
	routeGetArmFibmatch
)

// routeGetKeyword is one test in the chain: an exact strcmp or a matches()
// prefix test.
type routeGetKeyword struct {
	keyword string
	prefix  bool
	arm     routeGetArm
}

// routeGetKeywords is iproute_get's else-if chain (ip/iproute.c:2068-2189), in
// its order, which decides what an abbreviation means: `d` is dsfield and not
// dport, `i` is iif and not ipproto, `f` is from and not fibmatch.
//
// The arms goip does not implement are refused by name rather than read as a
// destination, which is where an unknown word goes: tos and dsfield (a
// rt_dsfield name table), connected (a second get built from the first's
// reply), vrf (a VRF check by name), as (MPLS), and the four flow keys.
var routeGetKeywords = []routeGetKeyword{
	{keyword: tosKeywordCst, arm: routeGetArmUnsupported},
	{keyword: dsfieldKeywordCst, prefix: true, arm: routeGetArmUnsupported},
	{keyword: fromKeywordCst, prefix: true, arm: routeGetArmFrom},
	{keyword: iifKeywordCst, prefix: true, arm: routeGetArmIif},
	{keyword: "mark", prefix: true, arm: routeGetArmMark},
	{keyword: oifKeywordCst, prefix: true, arm: routeGetArmOif},
	{keyword: devKeywordCst, arm: routeGetArmOif},
	{keyword: "notify", prefix: true, arm: routeGetArmNotify},
	{keyword: "connected", prefix: true, arm: routeGetArmUnsupported},
	{keyword: vrfKeywordCst, prefix: true, arm: routeGetArmUnsupported},
	{keyword: "uid", prefix: true, arm: routeGetArmUID},
	{keyword: "fibmatch", prefix: true, arm: routeGetArmFibmatch},
	{keyword: "as", arm: routeGetArmUnsupported},
	{keyword: "sport", prefix: true, arm: routeGetArmUnsupported},
	{keyword: "dport", prefix: true, arm: routeGetArmUnsupported},
	{keyword: "ipproto", prefix: true, arm: routeGetArmUnsupported},
	{keyword: "flowlabel", arm: routeGetArmUnsupported},
}

// lookupRouteGetKeyword is the chain's first match, or nil for the final else,
// the destination.
func lookupRouteGetKeyword(arg string) *routeGetKeyword {
	for i := range routeGetKeywords {
		kw := &routeGetKeywords[i]
		if (kw.prefix && matchesPrefix(arg, kw.keyword)) || arg == kw.keyword {
			return kw
		}
	}
	return nil
}

// parseRouteGetArgs is iproute_get's argument loop and the defaults after it,
// up to the name lookups (ip/iproute.c:2067-2195). family is preferred_family.
//
// Warnings go to warn, as iproute_get's fprintf(stderr) does: a destination
// prefix that is not a host route is sent as one anyway.
func parseRouteGetArgs(family uint8, args []string, warn func(string)) (routeGetState, error) {
	st := routeGetState{g: xtcpnl.RouteGet{Family: family}}
	for i := 0; i < len(args); i++ {
		var err error
		if kw := lookupRouteGetKeyword(args[i]); kw != nil {
			i, err = st.keyword(kw, args, i)
		} else {
			i, err = st.destination(args, i, warn)
		}
		if err != nil {
			return routeGetState{}, err
		}
	}
	if !st.addressFound {
		return routeGetState{}, fmt.Errorf("%w: need at least a destination address", ErrInvalidArgument)
	}
	return st, nil
}

func (st *routeGetState) keyword(kw *routeGetKeyword, args []string, i int) (int, error) {
	switch kw.arm {
	case routeGetArmUnsupported:
		return i, fmt.Errorf("route get %q: %w", kw.keyword, ErrNotImplemented)
	case routeGetArmNotify:
		st.g.RtmFlags |= unix.RTM_F_NOTIFY
		return i, nil
	case routeGetArmFibmatch:
		st.fibMatch = true
		return i, nil
	default:
		// The arms below take a value.
	}
	i, err := nextArg(args, i)
	if err != nil {
		return i, err
	}
	v := args[i]
	switch kw.arm {
	case routeGetArmFrom:
		return i, st.from(v)
	case routeGetArmIif:
		st.idev, st.idevSet = v, true
	case routeGetArmOif:
		st.odev, st.odevSet = v, true
	case routeGetArmMark:
		m, ok := getU32(v, 0)
		if !ok {
			return i, invargErr("invalid mark value", v)
		}
		st.mark = m
	case routeGetArmUID:
		u, ok := getU32(v, 0)
		if !ok {
			return i, invargErr("invalid UID\n", v)
		}
		st.g.Attrs = append(st.g.Attrs, xtcpnl.RouteAttrU32(uint16(unix.RTA_UID), u))
	default:
		// The value-less arms returned above.
	}
	return i, nil
}

// from is the `from` arm (ip/iproute.c:2076-2088): a prefix in the request's
// family so far, which also sets that family if it was unset.
func (st *routeGetState) from(v string) error {
	if matchesPrefix(v, routeHelpKeywordCst) {
		return routeHelpErr()
	}
	p, err := routeGetPrefix(v, st.g.Family)
	if err != nil {
		return err
	}
	st.adoptFamily(p.family)
	if len(p.data) > 0 {
		st.g.Attrs = append(st.g.Attrs, xtcpnl.RouteAttrBytes(uint16(unix.RTA_SRC), p.data))
	}
	st.g.SrcLen = bitlenU8(p.bitlen)
	return nil
}

// destination is the final else (ip/iproute.c:2161-2188): an optional `to`,
// then the prefix, whose length is forced to the host length with a warning.
func (st *routeGetState) destination(args []string, i int, warn func(string)) (int, error) {
	if args[i] == routeToKeywordCst {
		var err error
		if i, err = nextArg(args, i); err != nil {
			return i, err
		}
	}
	v := args[i]
	if matchesPrefix(v, routeHelpKeywordCst) {
		return i, routeHelpErr()
	}
	p, err := routeGetPrefix(v, st.g.Family)
	if err != nil {
		return i, err
	}
	st.adoptFamily(p.family)
	if len(p.data) > 0 {
		st.g.Attrs = append(st.g.Attrs, xtcpnl.RouteAttrBytes(uint16(unix.RTA_DST), p.data))
	}
	host := afBitLen(st.g.Family)
	switch {
	case st.g.Family == unix.AF_INET && p.bitlen != host,
		st.g.Family == unix.AF_INET6 && p.bitlen != host:
		warn(fmt.Sprintf("Warning: /%d as prefix is invalid, only /%d (or none) is supported.\n", p.bitlen, host))
		st.g.DstLen = bitlenU8(host)
	default:
		st.g.DstLen = bitlenU8(p.bitlen)
	}
	st.addressFound = true
	return i, nil
}

func (st *routeGetState) adoptFamily(f uint8) {
	if st.g.Family == unix.AF_UNSPEC {
		st.g.Family = f
	}
}

// finish is iproute_get after its loop (ip/iproute.c:2197-2224): the device
// lookups, iif first, then RTA_MARK, then the family default and the flags
// that depend on it.
func (st *routeGetState) finish(resolve linkResolver) error {
	if st.idevSet {
		idx, err := resolve(st.idev)
		if err != nil {
			return err
		}
		if idx == 0 {
			return noSuchDevice(nodevErr(st.idev))
		}
		st.g.Attrs = append(st.g.Attrs, xtcpnl.RouteAttrU32(uint16(unix.RTA_IIF), uint32(idx)))
	}
	if st.odevSet {
		idx, err := resolve(st.odev)
		if err != nil {
			return err
		}
		if idx == 0 {
			return noSuchDevice(nodevErr(st.odev))
		}
		st.g.Attrs = append(st.g.Attrs, xtcpnl.RouteAttrU32(uint16(unix.RTA_OIF), uint32(idx)))
	}
	if st.mark != 0 {
		st.g.Attrs = append(st.g.Attrs, xtcpnl.RouteAttrU32(uint16(unix.RTA_MARK), st.mark))
	}
	if st.g.Family == unix.AF_UNSPEC {
		st.g.Family = unix.AF_INET
	}
	// Only IPv4 supports RTM_F_LOOKUP_TABLE (ip/iproute.c:2219-2221).
	if st.g.Family == unix.AF_INET {
		st.g.RtmFlags |= unix.RTM_F_LOOKUP_TABLE
	}
	if st.fibMatch {
		st.g.RtmFlags |= unix.RTM_F_FIB_MATCH
	}
	return nil
}

// routeGet is `ip route get` (iproute_get, ip/iproute.c:2043-2294): one
// RTM_GETROUTE single get, and print_route over its one reply.
//
// The traffic is the request's and the names': a `from … iif d1` get first
// resolves d1 by name, and the reply's devices are named afterwards with one
// RTM_GETLINK by index each, d1 already cached by its lookup
// (link-write/rtnl/route_get_from_iif). An error reply, `Network is
// unreachable`, is a failed talk and exit 2, with nothing on stdout.
//
// print_route's filter_nlmsg runs over the reply with iproute_get's reset
// filter (filter.cloned = 2, so the cloned test never drops), which leaves
// one test that can: a reply in a family other than a set preferred_family is
// silently not printed (ip/iproute.c:188-189).
func routeGet(c *runCtx, args []string) error {
	st, err := parseRouteGetArgs(c.family, args, func(s string) {
		fmt.Fprint(c.errOut, s)
	})
	if err != nil {
		return err
	}
	svc := service.New(c.src, c.nextSeq)
	resolve := newLinkNameResolverRemember(svc, func(l xtcpnl.LinkInfo) {
		c.lltab.Fill([]xtcpnl.LinkInfo{l})
	})
	if err := st.finish(resolve); err != nil {
		return err
	}

	r, err := svc.RouteGet(st.g)
	if err != nil {
		return err
	}
	if c.family != unix.AF_UNSPEC && r.Family != c.family {
		return nil
	}
	routes := []model.Route{r}
	resolveRouteNames(c, svc, routes, false)

	f := render.RouteShowFilter{Details: c.detailed(), Stats: c.stats()}
	view := render.RouteViewOf(xtcpnl.RouteInfo(r), c.lltab, f)
	if c.json {
		return json.NewEncoder(c.out).Encode([]render.RouteView{view})
	}
	_, werr := fmt.Fprint(c.out, view.Text())
	return werr
}
