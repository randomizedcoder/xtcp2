//go:build goip_write

package goip

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// TestRouteWriteNhid is iproute_modify's nhid arm (ip/iproute.c:1461):
// get_u32 in base 0 into RTA_NH_ID where it is read, and universe scope
// unless the last id read is 0. GW1-T01 in goip-write-keywords.md.
//
// go test ./internal/goip/ -run TestRouteWriteNhid
func TestRouteWriteNhid(t *testing.T) {
	const (
		create = unix.NLM_F_CREATE
		excl   = unix.NLM_F_EXCL
		p24    = "198.51.100.0/24"
	)
	nhid := func(id uint32) xtcpnl.RouteAttr { return xtcpnl.RouteAttrU32(xtcpnl.RtaNhID, id) }
	// byID is `add 198.51.100.0/24 …` with no dev, at the scope given.
	byID := func(scope uint8, attrs ...xtcpnl.RouteAttr) routeWriteExpected {
		w := newRouteWrite(create|excl, 24, append([]xtcpnl.RouteAttr{rtaDst("198.51.100.0")}, attrs...)...)
		w.Scope = scope
		return routeWriteExpected{write: w}
	}
	invalid := func(msg string) routeWriteExpected {
		return routeWriteExpected{err: ErrInvalidArgument, errMsg: msg}
	}
	runRouteWriteRows(t, []routeWriteRow{
		{description: "positive: a blackhole by id", verb: "add", family: unix.AF_INET,
			args: "blackhole 198.51.100.7/32 table 4294967295 nhid 10",
			expected: routeWriteExpected{write: routeWith(newRouteWrite(create|excl, 32, rtaDst("198.51.100.7"),
				xtcpnl.RouteAttrU32(unix.RTA_TABLE, 4294967295), nhid(10)), func(w *xtcpnl.RouteWrite) {
				w.Type, w.Table, w.Scope = unix.RTN_BLACKHOLE, unix.RT_TABLE_UNSPEC, unix.RT_SCOPE_UNIVERSE
			})}},
		{description: "positive: an IPv4 unicast by id keeps universe scope", verb: "add", args: p24 + " nhid 7",
			expected: byID(unix.RT_SCOPE_UNIVERSE, nhid(7))},
		{description: "positive: an IPv6 unicast by id", verb: "add", family: unix.AF_INET6, args: "2001:db8::/64 nhid 7",
			expected: routeWriteExpected{write: routeWith(newRouteWrite(create|excl, 64, rtaDst("2001:db8::"), nhid(7)),
				func(w *xtcpnl.RouteWrite) { w.Family, w.Scope = unix.AF_INET6, unix.RT_SCOPE_UNIVERSE })}},
		{description: "positive: a hex id", verb: "add", args: p24 + " nhid 0x10",
			expected: byID(unix.RT_SCOPE_UNIVERSE, nhid(16))},
		{description: "positive: the id before the metric, in word order", verb: "add", args: p24 + " nhid 10 metric 5",
			expected: byID(unix.RT_SCOPE_UNIVERSE, nhid(10), xtcpnl.RouteAttrU32(unix.RTA_PRIORITY, 5))},
		{description: "positive: the id after the metric, in word order", verb: "add", args: p24 + " metric 5 nhid 10",
			expected: byID(unix.RT_SCOPE_UNIVERSE, xtcpnl.RouteAttrU32(unix.RTA_PRIORITY, 5), nhid(10))},
		{description: "positive: the id before RTA_OIF, which goes out after the loop", verb: "add",
			args: p24 + " dev d0 nhid 10",
			expected: routeWriteExpected{write: scopeUniverse(newRouteWrite(create|excl, 24, rtaDst("198.51.100.0"),
				nhid(10), rtaOif(2))), resolved: []string{"d0"}}},
		{description: "negative: a word for the id", verb: "add", args: p24 + " nhid x",
			expected: invalid(`Error: argument "x" is wrong: "id" value is invalid`)},
		{description: "negative: a negative id", verb: "add", args: p24 + " nhid -1",
			expected: invalid(`Error: argument "-1" is wrong: "id" value is invalid`)},
		{description: "negative: no value", verb: "add", args: p24 + " nhid",
			expected: invalid("Command line is not complete")},
		{description: "boundary: the largest id", verb: "add", args: p24 + " nhid 4294967295",
			expected: byID(unix.RT_SCOPE_UNIVERSE, nhid(4294967295))},
		{description: "boundary: one past the largest id", verb: "add", args: p24 + " nhid 4294967296",
			expected: invalid(`Error: argument "4294967296" is wrong: "id" value is invalid`)},
		{description: "boundary: id 0 is absent to the scope default", verb: "add", args: p24 + " nhid 0",
			expected: byID(unix.RT_SCOPE_LINK, nhid(0))},
		{description: "corner: two ids send two attributes", verb: "add", args: p24 + " nhid 1 nhid 2",
			expected: byID(unix.RT_SCOPE_UNIVERSE, nhid(1), nhid(2))},
		{description: "corner: the last id decides the scope", verb: "add", args: p24 + " nhid 1 nhid 0",
			expected: byID(unix.RT_SCOPE_LINK, nhid(1), nhid(0))},
		{description: "corner: a delete by id keeps scope nowhere", verb: "del", args: p24 + " nhid 10",
			expected: routeWriteExpected{write: xtcpnl.RouteWrite{
				MsgType: unix.RTM_DELROUTE, Flags: unix.NLM_F_ACK, Family: unix.AF_INET, DstLen: 24,
				Table: unix.RT_TABLE_MAIN, Scope: unix.RT_SCOPE_NOWHERE,
				Attrs: []xtcpnl.RouteAttr{rtaDst("198.51.100.0"), nhid(10)},
			}}},
		{description: "corner: nh is no keyword, so it is a second destination", verb: "add", args: p24 + " nh 10",
			expected: invalid(`Error: either "to" is duplicate, or "nh" is garbage`)},
	})
}

// scopeUniverse is w at universe scope.
func scopeUniverse(w xtcpnl.RouteWrite) xtcpnl.RouteWrite {
	w.Scope = unix.RT_SCOPE_UNIVERSE
	return w
}

// TestRouteWritePrefExpires is iproute_modify's pref arm (ip/iproute.c:1504),
// the three names by strcmp, else get_u8 in base 0, into RTA_PREF u8, and its
// expires arm (:1258), get_u32 in base 0 into RTA_EXPIRES. GW1-T02.
//
// go test ./internal/goip/ -run TestRouteWritePrefExpires
func TestRouteWritePrefExpires(t *testing.T) {
	const (
		create = unix.NLM_F_CREATE
		excl   = unix.NLM_F_EXCL
		p64    = "2001:db8::/64 dev d0"
	)
	pref := func(v uint8) xtcpnl.RouteAttr { return xtcpnl.RouteAttrBytes(unix.RTA_PREF, []byte{v}) }
	u32 := xtcpnl.RouteAttrU32
	// v6 is `-6 add 2001:db8::/64 dev d0 …`: the attributes argv gives, then
	// RTA_OIF.
	v6 := func(attrs ...xtcpnl.RouteAttr) routeWriteExpected {
		as := append(append([]xtcpnl.RouteAttr{rtaDst("2001:db8::")}, attrs...), rtaOif(2))
		return routeWriteExpected{write: routeWith(newRouteWrite(create|excl, 64, as...),
			func(w *xtcpnl.RouteWrite) { w.Family, w.Scope = unix.AF_INET6, unix.RT_SCOPE_UNIVERSE }),
			resolved: []string{"d0"}}
	}
	invalid := func(msg string) routeWriteExpected {
		return routeWriteExpected{err: ErrInvalidArgument, errMsg: msg}
	}
	runRouteWriteRows(t, []routeWriteRow{
		{description: "positive: pref low", verb: "add", family: unix.AF_INET6, args: p64 + " pref low", expected: v6(pref(3))},
		{description: "positive: pref medium", verb: "add", family: unix.AF_INET6, args: p64 + " pref medium",
			expected: v6(pref(0))},
		{description: "positive: pref high", verb: "add", family: unix.AF_INET6, args: p64 + " pref high", expected: v6(pref(1))},
		{description: "positive: a numeric pref", verb: "add", family: unix.AF_INET6, args: p64 + " pref 2", expected: v6(pref(2))},
		{description: "positive: a hex pref, base 0", verb: "add", family: unix.AF_INET6, args: p64 + " pref 0x10",
			expected: v6(pref(16))},
		{description: "positive: an expiry", verb: "add", family: unix.AF_INET6, args: p64 + " expires 30",
			expected: v6(u32(unix.RTA_EXPIRES, 30))},
		{description: "positive: an IPv4 expiry is sent, as ip sends it", verb: "add", args: "198.51.100.0/24 dev d0 expires 30",
			expected: routeWriteExpected{write: newRouteWrite(create|excl, 24, rtaDst("198.51.100.0"),
				u32(unix.RTA_EXPIRES, 30), rtaOif(2)), resolved: []string{"d0"}}},
		{description: "positive: pref and expires in word order", verb: "add", family: unix.AF_INET6,
			args: p64 + " expires 5 pref high", expected: v6(u32(unix.RTA_EXPIRES, 5), pref(1))},
		{description: "negative: an unknown name", verb: "add", family: unix.AF_INET6, args: p64 + " pref highest",
			expected: invalid(`Error: argument "highest" is wrong: "pref" value is invalid`)},
		{description: "negative: a pref past a u8", verb: "add", family: unix.AF_INET6, args: p64 + " pref 256",
			expected: invalid(`Error: argument "256" is wrong: "pref" value is invalid`)},
		{description: "negative: a negative expiry", verb: "add", family: unix.AF_INET6, args: p64 + " expires -1",
			expected: invalid(`Error: argument "-1" is wrong: "expires" value is invalid`)},
		{description: "negative: a word for the expiry", verb: "add", family: unix.AF_INET6, args: p64 + " expires x",
			expected: invalid(`Error: argument "x" is wrong: "expires" value is invalid`)},
		{description: "negative: pref with no value", verb: "add", family: unix.AF_INET6, args: p64 + " pref",
			expected: invalid("Command line is not complete")},
		{description: "boundary: the largest pref", verb: "add", family: unix.AF_INET6, args: p64 + " pref 255",
			expected: v6(pref(255))},
		{description: "boundary: the largest expiry", verb: "add", family: unix.AF_INET6, args: p64 + " expires 4294967295",
			expected: v6(u32(unix.RTA_EXPIRES, 4294967295))},
		{description: "boundary: a zero expiry", verb: "add", family: unix.AF_INET6, args: p64 + " expires 0",
			expected: v6(u32(unix.RTA_EXPIRES, 0))},
		{description: "corner: pre is pref", verb: "add", family: unix.AF_INET6, args: p64 + " pre high", expected: v6(pref(1))},
		{description: "corner: preference is the metric, by strcmp", verb: "add", family: unix.AF_INET6,
			args: p64 + " preference 7", expected: v6(u32(unix.RTA_PRIORITY, 7))},
		{description: "corner: pr is priority, earlier in the chain", verb: "add", family: unix.AF_INET6,
			args: p64 + " pr 7", expected: v6(u32(unix.RTA_PRIORITY, 7))},
		{description: "corner: p is priority, which the chain tests before pref", verb: "add", family: unix.AF_INET6,
			args: p64 + " p 7", expected: v6(u32(unix.RTA_PRIORITY, 7))},
		{description: "corner: a prefix past the word is the destination", verb: "add", family: unix.AF_INET6,
			args: p64 + " prefe high", expected: invalid(`Error: either "to" is duplicate, or "prefe" is garbage`)},
		{description: "corner: the names are strcmp", verb: "add", family: unix.AF_INET6, args: p64 + " pref Low",
			expected: invalid(`Error: argument "Low" is wrong: "pref" value is invalid`)},
		{description: "corner: expires is strcmp", verb: "add", family: unix.AF_INET6, args: p64 + " expire 5",
			expected: invalid(`Error: either "to" is duplicate, or "expire" is garbage`)},
	})
}

// TestRouteWriteTosRealmsVrf is iproute_modify's tos and dsfield arm
// (ip/iproute.c:1251), rtnl_dsfield_a2n into rtm_tos; its realms arm
// (:1450), get_rt_realms_or_raw into RTA_FLOW; and its vrf arm (:1487),
// ipvrf_get_table in the loop, then table's path into rtm_table or
// RTA_TABLE with table_ok set. The fake VRF lookup's calls are listed as
// "vrf NAME" beside dev's. GW1-T03.
//
// go test ./internal/goip/ -run TestRouteWriteTosRealmsVrf
func TestRouteWriteTosRealmsVrf(t *testing.T) {
	const (
		create = unix.NLM_F_CREATE
		excl   = unix.NLM_F_EXCL
		p24    = "198.51.100.0/24 dev d0"
	)
	// v4 is `add 198.51.100.0/24 dev d0 …` with f applied, the attributes
	// argv gives before RTA_OIF, and the lookups.
	v4 := func(f func(*xtcpnl.RouteWrite), resolved []string, attrs ...xtcpnl.RouteAttr) routeWriteExpected {
		as := append(append([]xtcpnl.RouteAttr{rtaDst("198.51.100.0")}, attrs...), rtaOif(2))
		return routeWriteExpected{write: routeWith(newRouteWrite(create|excl, 24, as...), f), resolved: resolved}
	}
	d0 := []string{"d0"}
	tos := func(v uint8) func(*xtcpnl.RouteWrite) { return func(w *xtcpnl.RouteWrite) { w.Tos = v } }
	table := func(v uint8) func(*xtcpnl.RouteWrite) { return func(w *xtcpnl.RouteWrite) { w.Table = v } }
	flow := func(v uint32) xtcpnl.RouteAttr { return xtcpnl.RouteAttrU32(unix.RTA_FLOW, v) }
	keep := func(*xtcpnl.RouteWrite) {}
	invalid := func(msg string, resolved ...string) routeWriteExpected {
		return routeWriteExpected{err: ErrInvalidArgument, errMsg: msg, resolved: resolved}
	}
	runRouteWriteRows(t, []routeWriteRow{
		{description: "positive: a hex tos", verb: "add", args: p24 + " tos 0x10", expected: v4(tos(16), d0)},
		{description: "positive: dsfield is base 16 without the 0x", verb: "add", args: p24 + " dsfield 10",
			expected: v4(tos(16), d0)},
		{description: "positive: a realm", verb: "add", args: p24 + " realms 10", expected: v4(keep, d0, flow(10))},
		{description: "positive: a FROM/TO realm, FROM in the high half", verb: "add", args: p24 + " realms 1/2",
			expected: v4(keep, d0, flow(0x10002))},
		{description: "positive: a stock realm name", verb: "add", args: p24 + " realms cosmos/5",
			expected: v4(keep, d0, flow(5))},
		{description: "positive: a vrf's table, looked up before dev", verb: "add", args: p24 + " vrf vr0",
			expected: v4(table(10), []string{"vrf vr0", "d0"})},
		{description: "positive: a vrf's large table goes in RTA_TABLE", verb: "add", args: p24 + " vrf vr1",
			expected: v4(table(unix.RT_TABLE_UNSPEC), []string{"vrf vr1", "d0"},
				xtcpnl.RouteAttrU32(unix.RTA_TABLE, 1000))},
		{description: "negative: a tos past a byte", verb: "add", args: p24 + " tos 0x100",
			expected: invalid(`Error: argument "0x100" is wrong: "tos" value is invalid`)},
		{description: "negative: a word for the tos", verb: "add", args: p24 + " tos zz",
			expected: invalid(`Error: argument "zz" is wrong: "tos" value is invalid`)},
		{description: "negative: an unknown realm", verb: "add", args: p24 + " realms nosuch",
			expected: invalid(`Error: argument "nosuch" is wrong: "realm" value is invalid`)},
		{description: "negative: a bad FROM realm", verb: "add", args: p24 + " realms x/2",
			expected: invalid(`Error: argument "x/2" is wrong: "realm" value is invalid`)},
		{description: "negative: not a vrf", verb: "add", args: p24 + " vrf d0",
			expected: invalid(`Error: argument "d0" is wrong: Invalid VRF`, "vrf d0")},
		{description: "negative: the vrf lookup fails", verb: "add", args: p24 + " vrf boom",
			expected: routeWriteExpected{err: errFakeResolve, errMsg: "fake", resolved: []string{"vrf boom"}}},
		{description: "negative: vrf with no value", verb: "add", args: p24 + " vrf",
			expected: invalid("Command line is not complete")},
		{description: "boundary: the largest tos", verb: "add", args: p24 + " tos 0xff", expected: v4(tos(255), d0)},
		{description: "boundary: a zero tos", verb: "add", args: p24 + " tos 0", expected: v4(tos(0), d0)},
		{description: "boundary: a TO past a byte is raw", verb: "add", args: p24 + " realms 256",
			expected: v4(keep, d0, flow(256))},
		{description: "boundary: an empty TO", verb: "add", args: p24 + " realms 3/",
			expected: v4(keep, d0, flow(0x30000))},
		{description: "corner: the default vrf is main", verb: "add", args: p24 + " vrf default",
			expected: v4(table(unix.RT_TABLE_MAIN), []string{"vrf default", "d0"})},
		{description: "corner: v is vrf", verb: "add", args: p24 + " v vr0",
			expected: v4(table(10), []string{"vrf vr0", "d0"})},
		{description: "corner: vrf then table, the last wins", verb: "add", args: p24 + " vrf vr0 table 20",
			expected: v4(table(20), []string{"vrf vr0", "d0"})},
		{description: "corner: a local route in a vrf stays out of the local table", verb: "add",
			args: "local 198.51.100.1 dev d0 vrf vr0",
			expected: routeWriteExpected{write: routeWith(newRouteWrite(create|excl, 32, rtaDst("198.51.100.1"), rtaOif(2)),
				func(w *xtcpnl.RouteWrite) { w.Type, w.Table, w.Scope = unix.RTN_LOCAL, 10, unix.RT_SCOPE_HOST }),
				resolved: []string{"vrf vr0", "d0"}}},
		{description: "corner: ds is dsfield, by its prefix", verb: "add", args: p24 + " ds 10", expected: v4(tos(16), d0)},
	})
}

// TestRouteWriteMultipath is iproute_modify's nexthop arm (ip/iproute.c:1459),
// which ends the loop, and parse_nexthops and parse_one_nh (:1101, :1019),
// which read the hops after RTA_OIF: each hop's dev looked up as it is read,
// its weight less one in rtnh_hops, and RTA_MULTIPATH after RTA_METRICS. A
// route with hops keeps universe scope. GW1-T04.
//
// go test ./internal/goip/ -run TestRouteWriteMultipath
func TestRouteWriteMultipath(t *testing.T) {
	const (
		create = unix.NLM_F_CREATE
		excl   = unix.NLM_F_EXCL
		p24    = "198.51.100.0/24"
		gw1    = "192.0.2.1"
	)
	gw := func(s string) xtcpnl.RouteAttr { return rtaAddr(unix.RTA_GATEWAY, s) }
	flow := func(v uint32) xtcpnl.RouteAttr { return xtcpnl.RouteAttrU32(unix.RTA_FLOW, v) }
	hop := func(flags, hops uint8, ifindex int32, attrs ...xtcpnl.RouteAttr) xtcpnl.RouteNexthop {
		return xtcpnl.RouteNexthop{Flags: flags, Hops: hops, Ifindex: ifindex, Attrs: attrs}
	}
	// v4 is `add 198.51.100.0/24 …` at universe scope, with the attributes
	// before RTA_MULTIPATH, the hops, and the lookups.
	v4 := func(resolved []string, attrs []xtcpnl.RouteAttr, hops ...xtcpnl.RouteNexthop) routeWriteExpected {
		w := scopeUniverse(newRouteWrite(create|excl, 24, append([]xtcpnl.RouteAttr{rtaDst("198.51.100.0")}, attrs...)...))
		w.Nexthops = hops
		return routeWriteExpected{write: w, resolved: resolved}
	}
	d0 := []string{"d0"}
	invalid := func(msg string, resolved ...string) routeWriteExpected {
		return routeWriteExpected{err: ErrInvalidArgument, errMsg: msg, resolved: resolved}
	}
	runRouteWriteRows(t, []routeWriteRow{
		{description: "positive: two weighted hops, each dev looked up", verb: "add",
			args:     p24 + " nexthop via 192.0.2.1 dev d0 weight 1 nexthop via 192.0.2.2 dev d0 weight 256",
			expected: v4([]string{"d0", "d0"}, nil, hop(0, 0, 2, gw(gw1)), hop(0, 255, 2, gw("192.0.2.2")))},
		{description: "positive: an IPv4 hop on an IPv6 route is RTA_VIA", verb: "add", family: unix.AF_INET6,
			args: "2001:db8::/64 nexthop via inet 192.0.2.1 dev d0",
			expected: routeWriteExpected{write: routeWith(newRouteWrite(create|excl, 64, rtaDst("2001:db8::")),
				func(w *xtcpnl.RouteWrite) {
					w.Family, w.Scope = unix.AF_INET6, unix.RT_SCOPE_UNIVERSE
					w.Nexthops = []xtcpnl.RouteNexthop{hop(0, 0, 2,
						xtcpnl.RouteAttrBytes(unix.RTA_VIA, []byte{unix.AF_INET, 0, 192, 0, 2, 1}))}
				}), resolved: d0}},
		{description: "positive: onlink and realms in a hop", verb: "add",
			args:     p24 + " nexthop via 192.0.2.1 dev d0 onlink realms 5",
			expected: v4(d0, nil, hop(unix.RTNH_F_ONLINK, 0, 2, gw(gw1), flow(5)))},
		{description: "positive: the route's attributes, then the hops", verb: "add",
			args: p24 + " table 1000 metric 5 mtu 1400 nexthop via 192.0.2.1 dev d0",
			expected: routeWriteExpected{write: routeWith(scopeUniverse(newRouteWrite(create|excl, 24, rtaDst("198.51.100.0"),
				xtcpnl.RouteAttrU32(unix.RTA_TABLE, 1000), xtcpnl.RouteAttrU32(unix.RTA_PRIORITY, 5))),
				func(w *xtcpnl.RouteWrite) {
					w.Table = unix.RT_TABLE_UNSPEC
					w.Metrics = []xtcpnl.RouteMetric{{Type: unix.RTAX_MTU, U32: 1400}}
					w.Nexthops = []xtcpnl.RouteNexthop{hop(0, 0, 2, gw(gw1))}
				}), resolved: d0}},
		{description: "positive: the route's dev is looked up before a hop's", verb: "add", args: p24 + " dev d0 nexthop dev v0",
			expected: v4([]string{"d0", "v0"}, []xtcpnl.RouteAttr{rtaOif(2)}, hop(0, 0, 6))},
		{description: "negative: weight 0", verb: "add", args: p24 + " nexthop dev d0 weight 0",
			expected: invalid(`Error: argument "0" is wrong: "weight" is invalid`, "d0")},
		{description: "negative: weight 257", verb: "add", args: p24 + " nexthop dev d0 weight 257",
			expected: invalid(`Error: argument "257" is wrong: "weight" is invalid`, "d0")},
		{description: "negative: no such hop device", verb: "add", args: p24 + " nexthop via 192.0.2.1 dev nosuch",
			expected: invalid("Cannot find device \"nosuch\"\nError: cannot parse nexthop", "nosuch")},
		{description: "negative: the hop lookup fails", verb: "add", args: p24 + " nexthop dev boom",
			expected: routeWriteExpected{err: errFakeResolve, errMsg: "fake", resolved: []string{"boom"}}},
		{description: "negative: a route keyword after a hop", verb: "add", args: p24 + " nexthop via 192.0.2.1 dev d0 metric 5",
			expected: invalid(`Error: "nexthop" or end of line is expected instead of "metric"`, "d0")},
		{description: "negative: a bare nexthop at the end", verb: "add", args: p24 + " nexthop via 192.0.2.1 nexthop",
			expected: invalid(`Error: unexpected end of line after "nexthop"`)},
		{description: "negative: via with no address", verb: "add", args: p24 + " nexthop via",
			expected: invalid("Command line is not complete")},
		{description: "negative: an IPv6 gateway on an IPv4 route", verb: "add", args: p24 + " nexthop via 2001:db8::1",
			expected: invalid(`Error: inet address is expected rather than "2001:db8::1"`)},
		{description: "positive: as to in a hop skips the to", verb: "add",
			args:     p24 + " nexthop via 192.0.2.1 as to 10.0.0.1 dev d0",
			expected: v4(d0, nil, hop(0, 0, 2, gw(gw1), xtcpnl.RouteAttrBytes(unix.RTA_NEWDST, []byte{10, 0, 0, 1})))},
		{description: "negative: as in a hop reads the route's family", verb: "add",
			args:     p24 + " nexthop via 192.0.2.1 as 2001:db8::1 dev d0",
			expected: invalid(`inet address is expected rather than "2001:db8::1"`)},
		{description: "negative: the route's dev fails before any hop is read", verb: "add",
			args: p24 + " dev nosuch nexthop dev d0 weight 0", expected: invalid(`Cannot find device "nosuch"`, "nosuch")},
		{description: "boundary: a hex weight of 256, base 0", verb: "add", args: p24 + " nexthop via 192.0.2.1 weight 0x100",
			expected: v4(nil, nil, hop(0, 255, 0, gw(gw1)))},
		{description: "boundary: an empty hop is a bare rtnexthop", verb: "add", args: p24 + " nexthop nexthop via 192.0.2.1",
			expected: v4(nil, nil, hop(0, 0, 0), hop(0, 0, 0, gw(gw1)))},
		{description: "corner: a hop keeps universe scope where dev alone is link", verb: "add", args: p24 + " nexthop dev d0",
			expected: v4(d0, nil, hop(0, 0, 2))},
		{description: "corner: a delete with hops keeps scope nowhere", verb: "del", args: p24 + " nexthop via 192.0.2.1 weight 1",
			expected: routeWriteExpected{write: xtcpnl.RouteWrite{
				MsgType: unix.RTM_DELROUTE, Flags: unix.NLM_F_ACK, Family: unix.AF_INET, DstLen: 24,
				Table: unix.RT_TABLE_MAIN, Scope: unix.RT_SCOPE_NOWHERE,
				Attrs: []xtcpnl.RouteAttr{rtaDst("198.51.100.0")}, Nexthops: []xtcpnl.RouteNexthop{hop(0, 0, 0, gw(gw1))},
			}}},
		{description: "corner: realm is realms, by its prefix", verb: "add", args: p24 + " nexthop via 192.0.2.1 realm 5",
			expected: v4(nil, nil, hop(0, 0, 0, gw(gw1), flow(5)))},
		{description: "corner: two vias in a hop are two gateways", verb: "add",
			args:     p24 + " nexthop via 192.0.2.1 via 192.0.2.2",
			expected: v4(nil, nil, hop(0, 0, 0, gw(gw1), gw("192.0.2.2")))},
		{description: "corner: weight is strcmp, so weigh ends the hop", verb: "add", args: p24 + " nexthop via 192.0.2.1 weigh 2",
			expected: invalid(`Error: "nexthop" or end of line is expected instead of "weigh"`)},
		{description: "corner: a hop gives the route its family", verb: "add", args: "default nexthop via 2001:db8::1",
			expected: routeWriteExpected{write: xtcpnl.RouteWrite{
				MsgType: unix.RTM_NEWROUTE, Flags: create | excl | unix.NLM_F_ACK, Family: unix.AF_INET6,
				Table: unix.RT_TABLE_MAIN, Protocol: unix.RTPROT_BOOT, Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_UNICAST,
				Nexthops: []xtcpnl.RouteNexthop{hop(0, 0, 0, gw("2001:db8::1"))},
			}}},
	})
}
