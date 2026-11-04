package goip

import (
	"bytes"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// srv6Seg1 is the forms' S1, 2001:db8:9::1.
const srv6Seg1 = "2001:db8:9::1"

// srv6SRH is xtcpnl.SRH, fatal on an error.
func srv6SRH(t *testing.T, nsegs int, segs ...string) []byte {
	t.Helper()
	as := make([][16]byte, 0, len(segs))
	for _, s := range segs {
		a, err := routeGetAddr(s, unix.AF_INET6)
		if err != nil {
			t.Fatal(err)
		}
		as = append(as, [16]byte(a.data))
	}
	b, err := xtcpnl.SRH(nsegs, as, 0)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestRouteWriteSrv6 is LX2-T02 for a route and a hop: each type's two
// attributes where the keyword is, a seg6local device looked up before
// dev, the route bound, and the quirks that change what the caller reads
// next. The encap bytes are the encoder's, which TestPutSeg6Encap and
// TestPutLwtEncapOther check against hand-built goldens; this checks where
// they go.
//
// go test ./internal/goip/ -run TestRouteWriteSrv6
func TestRouteWriteSrv6(t *testing.T) {
	const (
		create = unix.NLM_F_CREATE
		excl   = unix.NLM_F_EXCL
		p6     = "2001:db8:2::/64"
		p24    = "198.51.100.0/24"
	)
	enc := func(e xtcpnl.LwtEncap) []xtcpnl.RouteAttr {
		as, err := xtcpnl.EncodeLwtEncap(e, 1020)
		if err != nil {
			t.Fatal(err)
		}
		return as
	}
	cat := func(parts ...[]xtcpnl.RouteAttr) []xtcpnl.RouteAttr {
		var out []xtcpnl.RouteAttr
		for _, p := range parts {
			out = append(out, p...)
		}
		return out
	}
	one := func(a ...xtcpnl.RouteAttr) []xtcpnl.RouteAttr { return a }
	seg6 := func(nsegs int, segs ...string) []xtcpnl.RouteAttr {
		return enc(xtcpnl.LwtEncap{Seg6: &xtcpnl.Seg6Encap{Mode: xtcpnl.Seg6ModeEncap, SRH: srv6SRH(t, nsegs, segs...)}})
	}
	segsOf := func(n int) string {
		s := make([]string, n)
		for i := range s {
			s[i] = srv6Seg1
		}
		return strings.Join(s, ",")
	}
	// v6 is an IPv6 route to dst: universe scope, attrs after RTA_DST, and
	// the hops.
	v6 := func(dst string, resolved []string, attrs []xtcpnl.RouteAttr, hops ...xtcpnl.RouteNexthop) routeWriteExpected {
		w := newRouteWrite(create|excl, 64, cat(one(rtaDst(dst)), attrs)...)
		w.Family, w.Scope, w.Nexthops = unix.AF_INET6, unix.RT_SCOPE_UNIVERSE, hops
		return routeWriteExpected{write: w, resolved: resolved}
	}
	v4 := func(universe bool, resolved []string, attrs []xtcpnl.RouteAttr, hops ...xtcpnl.RouteNexthop) routeWriteExpected {
		w := newRouteWrite(create|excl, 24, cat(one(rtaDst("198.51.100.0")), attrs)...)
		if universe {
			w = scopeUniverse(w)
		}
		w.Nexthops = hops
		return routeWriteExpected{write: w, resolved: resolved}
	}
	gw := func(s string) xtcpnl.RouteAttr { return rtaAddr(unix.RTA_GATEWAY, s) }
	hop := func(ifindex int32, attrs ...xtcpnl.RouteAttr) xtcpnl.RouteNexthop {
		return xtcpnl.RouteNexthop{Ifindex: ifindex, Attrs: attrs}
	}
	d0, d0d0 := []string{"d0"}, []string{"d0", "d0"}
	invalid := func(msg string) routeWriteExpected {
		return routeWriteExpected{err: ErrInvalidArgument, errMsg: msg}
	}
	runRouteWriteRows(t, []routeWriteRow{
		{description: "positive: a route's seg6, before RTA_OIF", verb: "add", family: unix.AF_INET6,
			args:     p6 + " encap seg6 mode encap segs " + srv6Seg1 + " dev d0",
			expected: v6("2001:db8:2::", d0, cat(seg6(1, srv6Seg1), one(rtaOif(2))))},
		{description: "positive: seg6 on an IPv4 route", verb: "add",
			args:     p24 + " encap seg6 mode encap segs " + srv6Seg1 + " dev d0",
			expected: v4(false, d0, cat(seg6(1, srv6Seg1), one(rtaOif(2))))},
		{description: "positive: seg6local's oif looked up in the loop, before dev", verb: "add", family: unix.AF_INET6,
			args: "2001:db8:5::/64 encap seg6local action End.X nh6 2001:db8:1::1 oif d0 dev d0",
			expected: v6("2001:db8:5::", d0d0, cat(enc(xtcpnl.LwtEncap{Seg6Local: &xtcpnl.Seg6LocalEncap{Items: []xtcpnl.Seg6LocalItem{
				{Kind: xtcpnl.Seg6LocalAction, U32: 2}, {Kind: xtcpnl.Seg6LocalNH6, Addr: rtaAddr(0, "2001:db8:1::1").Payload},
				{Kind: xtcpnl.Seg6LocalOIF, U32: 2}}}}), one(rtaOif(2))))},
		{description: "positive: an ila route", verb: "add", family: unix.AF_INET6,
			args: "2001:db8:0:1::/64 encap ila 2001:0:0:2 ident-type luid dev d0",
			expected: v6("2001:db8:0:1::", d0, cat(enc(xtcpnl.LwtEncap{ILA: &xtcpnl.IlaEncap{
				Locator: [8]byte{0x20, 0x01, 0, 0, 0, 0, 0, 2}, Items: []xtcpnl.IlaItem{{Kind: xtcpnl.ILAAttrIdentType, U8: 1}}}}), one(rtaOif(2))))},
		{description: "positive: an xfrm route, dev before encap", verb: "add", family: unix.AF_INET6,
			args: p6 + " dev d0 encap xfrm if_id 1 link_dev d0",
			expected: v6("2001:db8:2::", d0d0, cat(enc(xtcpnl.LwtEncap{Xfrm: &xtcpnl.XfrmEncap{Items: []xtcpnl.XfrmItem{
				{Kind: xtcpnl.LwtXfrmIfID, U32: 1}, {Kind: xtcpnl.LwtXfrmLink, U32: 2}}}}), one(rtaOif(2))))},
		{description: "positive: a hop's seg6, after its gateway", verb: "add", family: unix.AF_INET6,
			args: p6 + " nexthop via 2001:db8:1::1 dev d0 encap seg6 mode encap segs " + srv6Seg1 +
				" nexthop via 2001:db8:1::2 dev d0",
			expected: v6("2001:db8:2::", d0d0, nil,
				hop(2, cat(one(gw("2001:db8:1::1")), seg6(1, srv6Seg1))...), hop(2, gw("2001:db8:1::2")))},
		{description: "positive: a hop's seg6 on IPv4", verb: "add",
			args: p24 + " nexthop via 192.0.2.1 dev d0 encap seg6 mode encap segs " + srv6Seg1 +
				" nexthop via 192.0.2.2 dev d0",
			expected: v4(true, d0d0, nil, hop(2, cat(one(gw("192.0.2.1")), seg6(1, srv6Seg1))...), hop(2, gw("192.0.2.2")))},
		{description: "negative: bpf on a route", verb: "add", family: unix.AF_INET6, args: p6 + " encap bpf in obj.o dev d0",
			expected: routeWriteExpected{err: ErrNotImplemented, errMsg: "encap bpf"}},
		{description: "boundary: a seg6 SRH of 59 segments fills the route bound", verb: "add", family: unix.AF_INET6,
			args:     p6 + " encap seg6 mode encap segs " + segsOf(59) + " dev d0",
			expected: v6("2001:db8:2::", d0, cat(seg6(59, strings.Split(segsOf(59), ",")...), one(rtaOif(2))))},
		{description: "boundary: one segment more is over it", verb: "add", family: unix.AF_INET6,
			args:     p6 + " encap seg6 mode encap segs " + segsOf(60) + " dev d0",
			expected: invalid("addraw_l ERROR: message exceeded bound of 1024")},
		{description: "corner: xfrm on a route swallows dev", verb: "add", family: unix.AF_INET6,
			args:     p6 + " encap xfrm if_id 1 dev d0",
			expected: v6("2001:db8:2::", nil, enc(xtcpnl.LwtEncap{Xfrm: &xtcpnl.XfrmEncap{Items: []xtcpnl.XfrmItem{{Kind: xtcpnl.LwtXfrmIfID, U32: 1}}}}))},
		{description: "corner: xfrm in a hop swallows the hop's dev", verb: "add",
			args: p24 + " nexthop via 192.0.2.1 encap xfrm if_id 1 dev d0",
			expected: v4(true, nil, nil, hop(0, cat(one(gw("192.0.2.1")),
				enc(xtcpnl.LwtEncap{Xfrm: &xtcpnl.XfrmEncap{Items: []xtcpnl.XfrmItem{{Kind: xtcpnl.LwtXfrmIfID, U32: 1}}}}))...))},
		{description: "corner: ioam6's trace last, its last value read again as the destination", verb: "add", family: unix.AF_INET6,
			args:     p6 + " dev d0 encap ioam6 trace prealloc type 0x800000 ns 1 size 12",
			expected: invalid(`either "to" is duplicate, or "12" is garbage`)},
		{description: "corner: seg6local's srh segs S last, S read again as the destination", verb: "add", family: unix.AF_INET6,
			args:     "2001:db8:5::/64 dev d0 encap seg6local action End.B6 srh segs " + srv6Seg1,
			expected: invalid(`either "to" is duplicate, or "` + srv6Seg1 + `" is garbage`)},
	})
}

// TestNexthopWriteSrv6 is LX2-T02 for a nexthop object: NHA_ENCAP and
// NHA_ENCAP_TYPE where the keyword is, and seg6local's oif looked up as
// it is read. The resolver knows d0 as 3.
//
// go test ./internal/goip/ -run TestNexthopWriteSrv6
func TestNexthopWriteSrv6(t *testing.T) {
	enc := func(e xtcpnl.LwtEncap) []byte {
		ab := xtcpnl.NewAttrBuilder(make([]byte, 1024))
		if err := xtcpnl.PutLwtEncap(&ab, xtcpnl.NhaEncap, xtcpnl.NhaEncapType, e); err != nil {
			t.Fatal(err)
		}
		return bytes.Clone(ab.Bytes())
	}
	cat := func(a ...[]byte) []byte { return bytes.Join(a, nil) }
	v6 := nhmOf(unix.AF_INET6, 0, 0)
	oif3 := fraOf(xtcpnl.NhaOIF, 3, 0, 0, 0)
	seg6 := enc(xtcpnl.LwtEncap{Seg6: &xtcpnl.Seg6Encap{Mode: xtcpnl.Seg6ModeEncap, SRH: srv6SRH(t, 1, srv6Seg1)}})
	end := func(items ...xtcpnl.Seg6LocalItem) []byte {
		return enc(xtcpnl.LwtEncap{Seg6Local: &xtcpnl.Seg6LocalEncap{Items: append([]xtcpnl.Seg6LocalItem{
			{Kind: xtcpnl.Seg6LocalAction, U32: 1}}, items...)}})
	}
	runNexthopKeywordRows(t, []nexthopKeywordRow{
		{description: "positive: a nexthop's seg6", family: unix.AF_INET6, args: "id 71 encap seg6 mode encap segs " + srv6Seg1 + " dev d0",
			nhm: v6, attrs: cat(fraOf(xtcpnl.NhaID, 71, 0, 0, 0), seg6, oif3), resolved: []string{"d0"}},
		{description: "positive: a nexthop's seg6local", family: unix.AF_INET6, args: "id 72 encap seg6local action End dev d0",
			nhm: v6, attrs: cat(fraOf(xtcpnl.NhaID, 72, 0, 0, 0), end(), oif3), resolved: []string{"d0"}},
		{description: "positive: seg6local's oif looked up before the nexthop's dev", family: unix.AF_INET6,
			args: "encap seg6local action End oif d0 dev d0",
			nhm:  v6, attrs: cat(end(xtcpnl.Seg6LocalItem{Kind: xtcpnl.Seg6LocalOIF, U32: 3}), oif3), resolved: []string{"d0", "d0"}},
		{description: "negative: bpf on a nexthop", family: unix.AF_INET6, args: "encap bpf in obj.o dev d0",
			err: ErrNotImplemented, errMsg: "encap bpf"},
		{description: "corner: xfrm swallows the nexthop's dev", family: unix.AF_INET6, args: "id 73 encap xfrm if_id 1 dev d0",
			nhm: v6, attrs: cat(fraOf(xtcpnl.NhaID, 73, 0, 0, 0),
				enc(xtcpnl.LwtEncap{Xfrm: &xtcpnl.XfrmEncap{Items: []xtcpnl.XfrmItem{{Kind: xtcpnl.LwtXfrmIfID, U32: 1}}}}))},
	})
}
