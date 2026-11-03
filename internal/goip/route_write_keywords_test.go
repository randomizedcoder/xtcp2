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
