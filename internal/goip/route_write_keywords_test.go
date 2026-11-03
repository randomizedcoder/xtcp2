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
