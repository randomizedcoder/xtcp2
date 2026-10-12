package goip

import (
	"bytes"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// TestLookupRouteWriteVerb is do_iproute's verb chain up to the list verbs:
// which spellings reach iproute_modify, with which message type and flags,
// and which fall through to the show path.
//
// go test ./internal/goip/ -run TestLookupRouteWriteVerb
func TestLookupRouteWriteVerb(t *testing.T) {
	type verb struct {
		msgType uint16
		flags   uint16
	}
	tests := []struct {
		description string
		arg         string
		expected    *verb // nil: not a write verb
	}{
		{description: "positive: add", arg: "add",
			expected: &verb{unix.RTM_NEWROUTE, unix.NLM_F_CREATE | unix.NLM_F_EXCL}},
		{description: "positive: change", arg: "change", expected: &verb{unix.RTM_NEWROUTE, unix.NLM_F_REPLACE}},
		{description: "positive: chg is change, by strcmp", arg: "chg", expected: &verb{unix.RTM_NEWROUTE, unix.NLM_F_REPLACE}},
		{description: "positive: replace", arg: "replace",
			expected: &verb{unix.RTM_NEWROUTE, unix.NLM_F_CREATE | unix.NLM_F_REPLACE}},
		{description: "positive: prepend", arg: "prepend", expected: &verb{unix.RTM_NEWROUTE, unix.NLM_F_CREATE}},
		{description: "positive: append", arg: "append",
			expected: &verb{unix.RTM_NEWROUTE, unix.NLM_F_CREATE | unix.NLM_F_APPEND}},
		{description: "positive: test", arg: "test", expected: &verb{unix.RTM_NEWROUTE, unix.NLM_F_EXCL}},
		{description: "positive: delete", arg: "delete", expected: &verb{unix.RTM_DELROUTE, 0}},
		{description: "corner: a is add", arg: "a", expected: &verb{unix.RTM_NEWROUTE, unix.NLM_F_CREATE | unix.NLM_F_EXCL}},
		{description: "corner: c is change", arg: "c", expected: &verb{unix.RTM_NEWROUTE, unix.NLM_F_REPLACE}},
		{description: "corner: d is delete", arg: "d", expected: &verb{unix.RTM_DELROUTE, 0}},
		{description: "corner: del is delete", arg: "del", expected: &verb{unix.RTM_DELROUTE, 0}},
		{description: "corner: p is prepend, not a later verb", arg: "p", expected: &verb{unix.RTM_NEWROUTE, unix.NLM_F_CREATE}},
		{description: "corner: r is replace, not restore", arg: "r",
			expected: &verb{unix.RTM_NEWROUTE, unix.NLM_F_CREATE | unix.NLM_F_REPLACE}},
		{description: "corner: s stays a show", arg: "s"},
		{description: "corner: l stays a list", arg: "l"},
		{description: "negative: chgx is not chg, which does not abbreviate or extend", arg: "chgx"},
		{description: "negative: get is not a write verb", arg: "get"},
		{description: "negative: flush is not a write verb", arg: "flush"},
		{description: "boundary: the full word plus one is no verb", arg: "adds"},
		{description: "boundary: an empty verb matches nothing", arg: ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v := lookupRouteWriteVerb(tc.arg)
			switch {
			case tc.expected == nil && v != nil:
				t.Fatalf("got verb %q, want none", v.name)
			case tc.expected == nil:
			case v == nil:
				t.Fatalf("got no verb, want %+v", *tc.expected)
			case v.msgType != tc.expected.msgType || v.flags != tc.expected.flags:
				t.Errorf("got type %d flags %#x, want %d %#x", v.msgType, v.flags, tc.expected.msgType, tc.expected.flags)
			}
		})
	}
}

// routeWriteExpected is a row's outcome: the RouteWrite and the names
// resolved, or an error class and a fragment of iproute2's message.
type routeWriteExpected struct {
	write    xtcpnl.RouteWrite
	resolved []string
	err      error
	errMsg   string
}

// The attributes the rows use, as iproute_modify writes them.
func rtaAddr(atype uint16, s string) xtcpnl.RouteAttr {
	a := netip.MustParseAddr(s)
	return xtcpnl.RouteAttrBytes(atype, a.AsSlice())
}

func rtaDst(s string) xtcpnl.RouteAttr { return rtaAddr(unix.RTA_DST, s) }
func rtaOif(idx uint32) xtcpnl.RouteAttr {
	return xtcpnl.RouteAttrU32(unix.RTA_OIF, idx)
}

// newRouteWrite is an IPv4 RTM_NEWROUTE with iproute_modify's defaults for
// a dev route: table main, proto boot, scope link, unicast.
func newRouteWrite(flags uint16, dstLen uint8, attrs ...xtcpnl.RouteAttr) xtcpnl.RouteWrite {
	return xtcpnl.RouteWrite{
		MsgType: unix.RTM_NEWROUTE, Flags: flags | unix.NLM_F_ACK,
		Family: unix.AF_INET, DstLen: dstLen,
		Table: unix.RT_TABLE_MAIN, Protocol: unix.RTPROT_BOOT,
		Scope: unix.RT_SCOPE_LINK, Type: unix.RTN_UNICAST,
		Attrs: attrs,
	}
}

func routeWith(w xtcpnl.RouteWrite, f func(*xtcpnl.RouteWrite)) xtcpnl.RouteWrite {
	f(&w)
	return w
}

// TestParseRouteWriteArgs is argv after the verb, through iproute_modify,
// into the RouteWrite the encoder takes: route-write-path.md table 2 and the
// rows its corrections add. d0 is ifindex 2 and boom a resolver failure;
// any other name is a miss.
//
// go test ./internal/goip/ -run TestParseRouteWriteArgs
func TestParseRouteWriteArgs(t *testing.T) {
	const (
		create  = unix.NLM_F_CREATE
		excl    = unix.NLM_F_EXCL
		replace = unix.NLM_F_REPLACE
		p24     = "198.51.100.0/24"
	)
	replaceDev := func(extra string) string { return p24 + " dev d0 " + extra }
	// replaced is `replace P dev d0 …` with the metrics and lock given.
	replaced := func(lock uint32, ms ...xtcpnl.RouteMetric) routeWriteExpected {
		w := newRouteWrite(create|replace, 24, rtaDst("198.51.100.0"), rtaOif(2))
		w.Metrics, w.MetricLock = ms, lock
		return routeWriteExpected{write: w, resolved: []string{"d0"}}
	}
	metric := func(rtax uint16, v uint32) xtcpnl.RouteMetric { return xtcpnl.RouteMetric{Type: rtax, U32: v} }
	invalid := func(msg string, resolved ...string) routeWriteExpected {
		return routeWriteExpected{err: ErrInvalidArgument, errMsg: msg, resolved: resolved}
	}
	notImpl := func(msg string) routeWriteExpected {
		return routeWriteExpected{err: ErrNotImplemented, errMsg: msg}
	}
	devRoute := func(flags uint16) routeWriteExpected {
		return routeWriteExpected{write: newRouteWrite(flags, 24, rtaDst("198.51.100.0"), rtaOif(2)), resolved: []string{"d0"}}
	}
	delV4 := xtcpnl.RouteWrite{
		MsgType: unix.RTM_DELROUTE, Flags: unix.NLM_F_ACK, Family: unix.AF_INET, DstLen: 24,
		Table: unix.RT_TABLE_MAIN, Scope: unix.RT_SCOPE_NOWHERE,
		Attrs: []xtcpnl.RouteAttr{rtaDst("198.51.100.0"), rtaOif(2)},
	}
	universe := func(w xtcpnl.RouteWrite) xtcpnl.RouteWrite {
		return routeWith(w, func(w *xtcpnl.RouteWrite) { w.Scope = unix.RT_SCOPE_UNIVERSE })
	}
	typed := func(typ uint8) xtcpnl.RouteWrite {
		return universe(routeWith(newRouteWrite(create|excl, 24, rtaDst("198.51.100.0")),
			func(w *xtcpnl.RouteWrite) { w.Type = typ }))
	}
	via6 := append([]byte{unix.AF_INET6, 0}, netip.MustParseAddr("2001:db8::2").AsSlice()...)

	tests := []struct {
		description string
		verb        string
		family      uint8
		args        string
		expected    routeWriteExpected
	}{
		// The verbs.
		{description: "positive: replace", verb: "replace", args: p24 + " dev d0", expected: devRoute(create | replace)},
		{description: "positive: add", verb: "add", args: p24 + " dev d0", expected: devRoute(create | excl)},
		{description: "positive: change", verb: "change", args: p24 + " dev d0", expected: devRoute(replace)},
		{description: "positive: chg", verb: "chg", args: p24 + " dev d0", expected: devRoute(replace)},
		{description: "positive: prepend", verb: "prepend", args: p24 + " dev d0", expected: devRoute(create)},
		{description: "positive: append", verb: "append", args: p24 + " dev d0",
			expected: devRoute(create | unix.NLM_F_APPEND)},
		{description: "positive: test", verb: "test", args: p24 + " dev d0", expected: devRoute(excl)},
		{description: "positive: delete has proto 0, type 0, scope nowhere", verb: "delete", args: p24 + " dev d0",
			expected: routeWriteExpected{write: delV4, resolved: []string{"d0"}}},
		{description: "positive: a dev-less delete sends no lookup", verb: "delete", args: p24,
			expected: routeWriteExpected{write: routeWith(delV4, func(w *xtcpnl.RouteWrite) { w.Attrs = w.Attrs[:1] })}},
		{description: "corner: an optional to before the prefix", verb: "replace", args: "to " + p24 + " dev d0",
			expected: devRoute(create | replace)},
		{description: "corner: oif is dev", verb: "replace", args: p24 + " oif d0", expected: devRoute(create | replace)},
		{description: "corner: dev before the prefix, RTA_OIF still after it", verb: "replace", args: "dev d0 " + p24,
			expected: devRoute(create | replace)},

		// The worked example and the metrics.
		{description: "positive: the NFS argv", verb: "replace",
			args: "192.0.2.50/32 dev d0 congctl lock cubic rto_min 3ms initcwnd 32 initrwnd 32 quickack 1",
			expected: routeWriteExpected{write: routeWith(newRouteWrite(create|replace, 32, rtaDst("192.0.2.50"), rtaOif(2)),
				func(w *xtcpnl.RouteWrite) {
					w.Metrics = []xtcpnl.RouteMetric{{Type: unix.RTAX_CC_ALGO, Str: "cubic"},
						metric(unix.RTAX_RTO_MIN, 3), metric(unix.RTAX_INITCWND, 32),
						metric(unix.RTAX_INITRWND, 32), metric(unix.RTAX_QUICKACK, 1)}
					w.MetricLock = 0x12000
				}), resolved: []string{"d0"}}},
		{description: "positive: rtt and rttvar with a unit are scaled by 8 and 4", verb: "replace",
			args:     replaceDev("rtt 3ms rttvar 10ms"),
			expected: replaced(0, metric(unix.RTAX_RTT, 24), metric(unix.RTAX_RTTVAR, 40))},
		{description: "boundary: raw rtt and rttvar are not scaled", verb: "replace", args: replaceDev("rtt 24 rttvar 40"),
			expected: replaced(0, metric(unix.RTAX_RTT, 24), metric(unix.RTAX_RTTVAR, 40))},
		{description: "boundary: rto_min always locks and is never scaled", verb: "replace", args: replaceDev("rto_min 200"),
			expected: replaced(1<<unix.RTAX_RTO_MIN, metric(unix.RTAX_RTO_MIN, 200))},
		{description: "boundary: a fraction rounds up", verb: "replace", args: replaceDev("rto_min 2.1ms"),
			expected: replaced(1<<unix.RTAX_RTO_MIN, metric(unix.RTAX_RTO_MIN, 3))},
		{description: "boundary: seconds are 1000 ms", verb: "replace", args: replaceDev("rto_min 1s"),
			expected: replaced(1<<unix.RTAX_RTO_MIN, metric(unix.RTAX_RTO_MIN, 1000))},
		{description: "corner: the unit is case-insensitive", verb: "replace", args: replaceDev("rto_min 1.5SECS"),
			expected: replaced(1<<unix.RTAX_RTO_MIN, metric(unix.RTAX_RTO_MIN, 1500))},
		{description: "corner: a scaled rtt with a fraction rounds before it is scaled", verb: "replace",
			args: replaceDev("rtt 0.5ms"), expected: replaced(0, metric(unix.RTAX_RTT, 8))},
		{description: "positive: lock comes before the value", verb: "replace", args: replaceDev("mtu lock 1400"),
			expected: replaced(1<<unix.RTAX_MTU, metric(unix.RTAX_MTU, 1400))},
		{description: "positive: every u32 metric, in argv order", verb: "replace",
			args: replaceDev("advmss 1360 window 65535 ssthresh 100 cwnd 10 reordering 5 hoplimit 64 initcwnd 10 initrwnd 20 fastopen_no_cookie 1"),
			expected: replaced(0, metric(unix.RTAX_ADVMSS, 1360), metric(unix.RTAX_WINDOW, 65535),
				metric(unix.RTAX_SSTHRESH, 100), metric(unix.RTAX_CWND, 10), metric(unix.RTAX_REORDERING, 5),
				metric(unix.RTAX_HOPLIMIT, 64), metric(unix.RTAX_INITCWND, 10), metric(unix.RTAX_INITRWND, 20),
				metric(unix.RTAX_FASTOPEN_NO_COOKIE, 1))},
		{description: "positive: features ORs ecn and tcp_usec_ts", verb: "replace", args: replaceDev("features ecn tcp_usec_ts"),
			expected: replaced(0, metric(unix.RTAX_FEATURES, 0x11))},
		{description: "corner: features stops at the next keyword", verb: "replace", args: replaceDev("features ecn initcwnd 10"),
			expected: replaced(0, metric(unix.RTAX_FEATURES, 1), metric(unix.RTAX_INITCWND, 10))},
		{description: "corner: a repeated metric is kept twice, in order", verb: "replace", args: replaceDev("initcwnd 10 initcwnd 20"),
			expected: replaced(0, metric(unix.RTAX_INITCWND, 10), metric(unix.RTAX_INITCWND, 20))},
		{description: "boundary: hoplimit 255 is accepted", verb: "replace", args: replaceDev("hoplimit 255"),
			expected: replaced(0, metric(unix.RTAX_HOPLIMIT, 255))},
		{description: "boundary: quickack 0 is still written", verb: "replace", args: replaceDev("quickack 0"),
			expected: replaced(0, metric(unix.RTAX_QUICKACK, 0))},
		{description: "corner: s is ssthresh, since src and scope are strcmp", verb: "replace", args: replaceDev("s 100"),
			expected: replaced(0, metric(unix.RTAX_SSTHRESH, 100))},
		{description: "corner: rt is rttvar, since rtt is strcmp", verb: "replace", args: replaceDev("rt 10ms"),
			expected: replaced(0, metric(unix.RTAX_RTTVAR, 40))},
		{description: "corner: a metric value in hex, get_unsigned base 0", verb: "replace", args: replaceDev("initcwnd 0x20"),
			expected: replaced(0, metric(unix.RTAX_INITCWND, 32))},

		// Gateways, types, tables, scopes.
		{description: "positive: via keeps universe scope and sends no lookup", verb: "add", args: p24 + " via 192.0.2.254",
			expected: routeWriteExpected{write: universe(newRouteWrite(create|excl, 24,
				rtaDst("198.51.100.0"), rtaAddr(unix.RTA_GATEWAY, "192.0.2.254")))}},
		{description: "positive: a cross-family via is RTA_VIA", verb: "add", args: p24 + " via inet6 2001:db8::2 dev d0",
			expected: routeWriteExpected{write: universe(newRouteWrite(create|excl, 24, rtaDst("198.51.100.0"),
				xtcpnl.RouteAttrBytes(unix.RTA_VIA, via6), rtaOif(2))), resolved: []string{"d0"}}},
		{description: "corner: via inet with the route's family is RTA_GATEWAY", verb: "add", args: p24 + " via inet 192.0.2.254",
			expected: routeWriteExpected{write: universe(newRouteWrite(create|excl, 24,
				rtaDst("198.51.100.0"), rtaAddr(unix.RTA_GATEWAY, "192.0.2.254")))}},
		{description: "positive: blackhole keeps universe scope", verb: "add", args: "blackhole " + p24,
			expected: routeWriteExpected{write: typed(unix.RTN_BLACKHOLE)}},
		{description: "positive: unreachable keeps universe scope", verb: "add", args: "unreachable " + p24,
			expected: routeWriteExpected{write: typed(unix.RTN_UNREACHABLE)}},
		{description: "corner: u is unreachable, ahead of unicast", verb: "add", args: "u " + p24,
			expected: routeWriteExpected{write: typed(unix.RTN_UNREACHABLE)}},
		{description: "corner: a type needs a non-digit first, so to 0x6 is the prefix 6.0.0.0", verb: "add",
			args: "to 0x6 " + p24, expected: invalid(`either "to" is duplicate, or "198.51.100.0/24" is garbage`)},
		{description: "corner: a numeric type after a sign, which strtoul takes", verb: "add", args: "+6 " + p24,
			expected: routeWriteExpected{write: typed(unix.RTN_BLACKHOLE)}},
		{description: "positive: local goes to the local table with host scope", verb: "add", args: "local 192.0.2.77 dev d0",
			expected: routeWriteExpected{write: routeWith(newRouteWrite(create|excl, 32, rtaDst("192.0.2.77"), rtaOif(2)),
				func(w *xtcpnl.RouteWrite) {
					w.Type, w.Table, w.Scope = unix.RTN_LOCAL, unix.RT_TABLE_LOCAL, unix.RT_SCOPE_HOST
				}), resolved: []string{"d0"}}},
		{description: "positive: broadcast goes to the local table with link scope", verb: "add", args: "broadcast 192.0.2.255 dev d0",
			expected: routeWriteExpected{write: routeWith(newRouteWrite(create|excl, 32, rtaDst("192.0.2.255"), rtaOif(2)),
				func(w *xtcpnl.RouteWrite) { w.Type, w.Table = unix.RTN_BROADCAST, unix.RT_TABLE_LOCAL }), resolved: []string{"d0"}}},
		{description: "corner: an explicit table keeps a local route out of the local table", verb: "add",
			args: "local 192.0.2.77 dev d0 table 100",
			expected: routeWriteExpected{write: routeWith(newRouteWrite(create|excl, 32, rtaDst("192.0.2.77"), rtaOif(2)),
				func(w *xtcpnl.RouteWrite) { w.Type, w.Table, w.Scope = unix.RTN_LOCAL, 100, unix.RT_SCOPE_HOST }),
				resolved: []string{"d0"}}},
		{description: "positive: src, proto, scope and metric in argv order", verb: "add",
			args: p24 + " dev d0 src 192.0.2.1 proto static scope link metric 50",
			expected: routeWriteExpected{write: routeWith(newRouteWrite(create|excl, 24, rtaDst("198.51.100.0"),
				rtaAddr(unix.RTA_PREFSRC, "192.0.2.1"), xtcpnl.RouteAttrU32(unix.RTA_PRIORITY, 50), rtaOif(2)),
				func(w *xtcpnl.RouteWrite) { w.Protocol = unix.RTPROT_STATIC }), resolved: []string{"d0"}}},
		{description: "corner: an explicit scope global is kept on a dev route", verb: "add", args: p24 + " dev d0 scope global",
			expected: routeWriteExpected{write: universe(newRouteWrite(create|excl, 24, rtaDst("198.51.100.0"), rtaOif(2))),
				resolved: []string{"d0"}}},
		{description: "corner: numeric proto and scope, base 0", verb: "add", args: p24 + " dev d0 proto 0x10 scope 200",
			expected: routeWriteExpected{write: routeWith(newRouteWrite(create|excl, 24, rtaDst("198.51.100.0"), rtaOif(2)),
				func(w *xtcpnl.RouteWrite) { w.Protocol, w.Scope = 16, 200 }), resolved: []string{"d0"}}},
		{description: "positive: onlink sets RTNH_F_ONLINK", verb: "add", args: p24 + " via 203.0.113.1 dev d0 onlink",
			expected: routeWriteExpected{write: universe(routeWith(newRouteWrite(create|excl, 24, rtaDst("198.51.100.0"),
				rtaAddr(unix.RTA_GATEWAY, "203.0.113.1"), rtaOif(2)),
				func(w *xtcpnl.RouteWrite) { w.RtmFlags = unix.RTNH_F_ONLINK })), resolved: []string{"d0"}}},
		{description: "positive: from is RTA_SRC and rtm_src_len", verb: "add", args: p24 + " from 192.0.2.0/24 dev d0",
			expected: routeWriteExpected{write: routeWith(newRouteWrite(create|excl, 24, rtaDst("198.51.100.0"),
				rtaAddr(unix.RTA_SRC, "192.0.2.0"), rtaOif(2)), func(w *xtcpnl.RouteWrite) { w.SrcLen = 24 }),
				resolved: []string{"d0"}}},
		{description: "boundary: table 255 fits rtm_table", verb: "add", args: p24 + " dev d0 table 255",
			expected: routeWriteExpected{write: routeWith(newRouteWrite(create|excl, 24, rtaDst("198.51.100.0"), rtaOif(2)),
				func(w *xtcpnl.RouteWrite) { w.Table = 255 }), resolved: []string{"d0"}}},
		{description: "boundary: table 256 spills to RTA_TABLE at its argv place", verb: "add", args: p24 + " table 256 dev d0",
			expected: routeWriteExpected{write: routeWith(newRouteWrite(create|excl, 24, rtaDst("198.51.100.0"),
				xtcpnl.RouteAttrU32(unix.RTA_TABLE, 256), rtaOif(2)), func(w *xtcpnl.RouteWrite) { w.Table = 0 }),
				resolved: []string{"d0"}}},
		{description: "corner: t is table, and table names are read", verb: "add", args: p24 + " dev d0 t default",
			expected: routeWriteExpected{write: routeWith(newRouteWrite(create|excl, 24, rtaDst("198.51.100.0"), rtaOif(2)),
				func(w *xtcpnl.RouteWrite) { w.Table = unix.RT_TABLE_DEFAULT }), resolved: []string{"d0"}}},

		// Destinations and families.
		{description: "boundary: default with no family has no RTA_DST", verb: "replace", args: "default via 192.0.2.254",
			expected: routeWriteExpected{write: universe(newRouteWrite(create|replace, 0, rtaAddr(unix.RTA_GATEWAY, "192.0.2.254")))}},
		{description: "boundary: default under -4 is RTA_DST 0.0.0.0", verb: "replace", family: unix.AF_INET,
			args: "default via 192.0.2.254", expected: routeWriteExpected{write: universe(newRouteWrite(create|replace, 0,
				rtaDst("0.0.0.0"), rtaAddr(unix.RTA_GATEWAY, "192.0.2.254")))}},
		{description: "corner: all under -4 still has no RTA_DST", verb: "replace", family: unix.AF_INET,
			args: "all via 192.0.2.254", expected: routeWriteExpected{write: universe(newRouteWrite(create|replace, 0,
				rtaAddr(unix.RTA_GATEWAY, "192.0.2.254")))}},
		{description: "corner: a dotted netmask", verb: "replace", args: "198.51.100.0/255.255.255.0 dev d0",
			expected: devRoute(create | replace)},
		{description: "corner: a bare address is a /32", verb: "add", args: "198.51.100.7 dev d0",
			expected: routeWriteExpected{write: newRouteWrite(create|excl, 32, rtaDst("198.51.100.7"), rtaOif(2)),
				resolved: []string{"d0"}}},
		{description: "corner: a leading digit is never a type, so 6 is 6.0.0.0", verb: "add", args: "6 dev d0",
			expected: routeWriteExpected{write: newRouteWrite(create|excl, 32, rtaDst("6.0.0.0"), rtaOif(2)),
				resolved: []string{"d0"}}},
		{description: "positive: -6 replace via", verb: "replace", family: unix.AF_INET6, args: "2001:db8:1::/64 via 2001:db8::2 dev d0",
			expected: routeWriteExpected{write: universe(routeWith(newRouteWrite(create|replace, 64, rtaDst("2001:db8:1::"),
				rtaAddr(unix.RTA_GATEWAY, "2001:db8::2"), rtaOif(2)), func(w *xtcpnl.RouteWrite) { w.Family = unix.AF_INET6 })),
				resolved: []string{"d0"}}},
		{description: "positive: a v6 dev route is universe scope, not link", verb: "add", args: "2001:db8:1::/64 dev d0",
			expected: routeWriteExpected{write: universe(routeWith(newRouteWrite(create|excl, 64, rtaDst("2001:db8:1::"), rtaOif(2)),
				func(w *xtcpnl.RouteWrite) { w.Family = unix.AF_INET6 })), resolved: []string{"d0"}}},
		{description: "boundary: a v6 delete is universe scope", verb: "delete", family: unix.AF_INET6, args: "2001:db8:1::/64 dev d0",
			expected: routeWriteExpected{write: xtcpnl.RouteWrite{MsgType: unix.RTM_DELROUTE, Flags: unix.NLM_F_ACK,
				Family: unix.AF_INET6, DstLen: 64, Table: unix.RT_TABLE_MAIN, Scope: unix.RT_SCOPE_UNIVERSE,
				Attrs: []xtcpnl.RouteAttr{rtaDst("2001:db8:1::"), rtaOif(2)}}, resolved: []string{"d0"}}},
		{description: "corner: a delete of a blackhole keeps nowhere scope and the type", verb: "delete", args: "blackhole " + p24,
			expected: routeWriteExpected{write: routeWith(delV4, func(w *xtcpnl.RouteWrite) {
				w.Type, w.Attrs = unix.RTN_BLACKHOLE, w.Attrs[:1]
			})}},

		// Argument errors: none of them costs a lookup.
		{description: "negative: microseconds are not a unit", verb: "replace", args: replaceDev("rto_min 500us"),
			expected: invalid(`"500us" is wrong: "rto_min" value is invalid`)},
		{description: "negative: lock after the value is a second destination", verb: "replace", args: replaceDev("congctl cubic lock"),
			expected: invalid(`either "to" is duplicate, or "lock" is garbage`)},
		{description: "negative: rto_min takes no lock token", verb: "replace", args: replaceDev("rto_min lock 3ms"),
			expected: invalid(`"lock" is wrong: "rto_min" value is invalid`)},
		{description: "negative: quickack 2", verb: "replace", args: replaceDev("quickack 2"),
			expected: invalid(`"quickack" value should be 0 or 1`)},
		{description: "negative: quickack is not a number", verb: "replace", args: replaceDev("quickack on"),
			expected: invalid(`"quickack" value is invalid`)},
		{description: "negative: fastopen_no_cookie 2", verb: "replace", args: replaceDev("fastopen_no_cookie 2"),
			expected: invalid(`"fastopen_no_cookie" value should be 0 or 1`)},
		{description: "negative: an unknown feature", verb: "replace", args: replaceDev("features bogus"),
			expected: invalid(`"bogus" is wrong: "features" value not valid`)},
		{description: "corner: features at the end of argv quotes (null), as glibc prints NULL", verb: "replace",
			args: replaceDev("features"), expected: invalid(`"(null)" is wrong: "features" value not valid`)},
		{description: "negative: hoplimit 256", verb: "replace", args: replaceDev("hoplimit 256"),
			expected: invalid(`"hoplimit" value is invalid`)},
		{description: "negative: advmss says mss", verb: "replace", args: replaceDev("advmss x"),
			expected: invalid(`"mss" value is invalid`)},
		{description: "negative: a metric past UINT_MAX", verb: "replace", args: replaceDev("initcwnd 4294967296"),
			expected: invalid(`"initcwnd" value is invalid`)},
		{description: "negative: a second via", verb: "add", args: p24 + " via 192.0.2.1 via 192.0.2.2",
			expected: invalid("use nexthop syntax to specify multiple via")},
		{description: "corner: a second via is refused before its value is read", verb: "add", args: p24 + " via 192.0.2.1 via",
			expected: invalid("use nexthop syntax to specify multiple via")},
		{description: "negative: the metric keyword's value", verb: "add", args: p24 + " metric x",
			expected: invalid(`"metric" value is invalid`)},
		{description: "negative: an unknown scope", verb: "add", args: p24 + " scope bogus", expected: invalid(`invalid "scope" value`)},
		{description: "negative: proto 256", verb: "add", args: p24 + " proto 256", expected: invalid(`"protocol" value is invalid`)},
		{description: "negative: a table name with no built-in entry", verb: "add", args: p24 + " table mgmt",
			expected: invalid(`"table" value is invalid`)},
		{description: "boundary: a table past 32 bits", verb: "add", args: p24 + " table 4294967296",
			expected: invalid(`"table" value is invalid`)},
		{description: "negative: two destinations", verb: "add", args: p24 + " 203.0.113.0/24",
			expected: invalid(`either "to" is duplicate, or "203.0.113.0/24" is garbage`)},
		{description: "negative: no destination", verb: "add", args: "dev d0", expected: invalid("no destination")},
		{description: "negative: an empty argv", verb: "add", args: "", expected: invalid("no destination")},
		{description: "boundary: a /33", verb: "add", args: "198.51.100.0/33",
			expected: invalid(`any valid prefix is expected rather than "198.51.100.0/33"`)},
		{description: "negative: a v4 prefix under -6", verb: "add", family: unix.AF_INET6, args: p24,
			expected: invalid(`inet6 prefix is expected`)},
		{description: "negative: a slash after default", verb: "add", args: "default/0", expected: invalid(`prefix is expected`)},
		{description: "negative: a v6 src on a v4 route", verb: "add", args: p24 + " src 2001:db8::1",
			expected: invalid(`inet address is expected rather than "2001:db8::1"`)},
		{description: "negative: via bridge never parses", verb: "add", args: p24 + " via bridge 192.0.2.1",
			expected: invalid(`bridge address is expected`)},
		{description: "negative: -0 refuses a prefix", verb: "add", family: unix.AF_PACKET, args: p24,
			expected: invalid(`may be inet prefix, but it is not allowed in this context`)},
		{description: "corner: a metric missing its value", verb: "replace", args: replaceDev("initcwnd"),
			expected: invalid("Command line is not complete")},
		{description: "corner: to with nothing after it", verb: "add", args: "to", expected: invalid("Command line is not complete")},
		{description: "corner: a type with nothing after it", verb: "add", args: "blackhole",
			expected: invalid("Command line is not complete")},
		{description: "negative: an argv error after dev sends no lookup", verb: "replace", args: replaceDev("quickack 2"),
			expected: invalid(`"quickack" value should be 0 or 1`)},
		{description: "negative: an unknown dev", verb: "add", args: p24 + " dev nosuch",
			expected: invalid(`Cannot find device "nosuch"`, "nosuch")},
		{description: "negative: a resolver failure is returned as it is", verb: "add", args: p24 + " dev boom",
			expected: routeWriteExpected{err: errFakeResolve, errMsg: "fake", resolved: []string{"boom"}}},

		// Keywords goip does not implement.
		{description: "negative: nexthop", verb: "add", args: p24 + " nexthop via 192.0.2.1", expected: notImpl(`"nexthop"`)},
		{description: "negative: encap", verb: "add", args: p24 + " encap mpls 100 dev d0", expected: notImpl(`"encap"`)},
		{description: "negative: vrf, which costs a lookup of its own in ip", verb: "add", args: p24 + " vrf red",
			expected: notImpl(`"vrf"`)},
		{description: "negative: tos", verb: "add", args: p24 + " tos 0x10", expected: notImpl(`"tos"`)},
		{description: "negative: realms", verb: "add", args: p24 + " realms 1", expected: notImpl(`"realms"`)},
		{description: "negative: nhid", verb: "add", args: p24 + " nhid 1", expected: notImpl(`"nhid"`)},
		{description: "negative: via link", verb: "add", args: p24 + " via link 02:00:00:00:00:01", expected: notImpl("link")},
		{description: "negative: help", verb: "add", args: "help", expected: notImpl("help")},
		{description: "corner: h is help, since hoplimit is strcmp", verb: "add", args: "h", expected: notImpl("help")},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			verb := lookupRouteWriteVerb(tc.verb)
			if verb == nil {
				t.Fatalf("no verb %q", tc.verb)
			}
			links := newFakeLinks()
			got, err := parseRouteWriteArgs(verb, tc.family, strings.Fields(tc.args), links.resolve)
			if !equalStrings(links.calls, tc.expected.resolved) {
				t.Errorf("resolved %q, want %q", links.calls, tc.expected.resolved)
			}
			if tc.expected.err != nil {
				if !errors.Is(err, tc.expected.err) || !strings.Contains(err.Error(), tc.expected.errMsg) {
					t.Fatalf("err = %v, want %v containing %q", err, tc.expected.err, tc.expected.errMsg)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			if !equalRouteWrite(got, tc.expected.write) {
				t.Errorf("got  %+v\nwant %+v", got, tc.expected.write)
			}
		})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// equalRouteWrite compares two RouteWrites by the request they encode, so a
// nil and an empty attribute list are the same, as they are on the wire.
func equalRouteWrite(a, b xtcpnl.RouteWrite) bool {
	ab, aerr := xtcpnl.BuildRouteWriteRequest(a, 1)
	bb, berr := xtcpnl.BuildRouteWriteRequest(b, 1)
	return aerr == nil && berr == nil && bytes.Equal(ab, bb)
}

// TestGetTimeRtt is get_time_rtt on its own: the units, the rounding, and
// raw, which is what decides whether rtt and rttvar are scaled.
//
// go test ./internal/goip/ -run TestGetTimeRtt
func TestGetTimeRtt(t *testing.T) {
	type result struct {
		val uint32
		raw bool
		ok  bool
	}
	tests := []struct {
		description string
		arg         string
		expected    result
	}{
		{description: "positive: a bare number is raw", arg: "24", expected: result{24, true, true}},
		{description: "positive: ms", arg: "3ms", expected: result{3, false, true}},
		{description: "positive: msec and msecs", arg: "3msecs", expected: result{3, false, true}},
		{description: "positive: s is 1000", arg: "2s", expected: result{2000, false, true}},
		{description: "positive: sec and secs", arg: "2sec", expected: result{2000, false, true}},
		{description: "boundary: a fraction rounds up", arg: "2.1", expected: result{3, true, true}},
		{description: "boundary: an exact fraction does not", arg: "2.0ms", expected: result{2, false, true}},
		{description: "boundary: 0 is accepted", arg: "0", expected: result{0, true, true}},
		{description: "boundary: UINT_MAX", arg: "4294967295", expected: result{4294967295, true, true}},
		{description: "boundary: past UINT_MAX is refused", arg: "4294967296", expected: result{}},
		{description: "corner: hex, strtoul base 0", arg: "0x10", expected: result{16, true, true}},
		{description: "corner: octal, strtoul base 0", arg: "010", expected: result{8, true, true}},
		{description: "corner: a leading-dot fraction", arg: ".5ms", expected: result{1, false, true}},
		{description: "corner: an exponent", arg: "1.5e1", expected: result{15, true, true}},
		{description: "corner: the unit is case-insensitive", arg: "1MS", expected: result{1, false, true}},
		{description: "negative: us is not a unit", arg: "500us", expected: result{}},
		{description: "negative: a negative fraction", arg: "-1.5", expected: result{}},
		{description: "negative: no digits", arg: "ms", expected: result{}},
		{description: "negative: a lone dot", arg: ".", expected: result{}},
		{description: "negative: empty", arg: "", expected: result{}},
		{description: "negative: a space before the unit", arg: "3 ms", expected: result{}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v, raw, ok := getTimeRtt(tc.arg)
			got := result{v, raw, ok}
			if !ok {
				got = result{}
			}
			if got != tc.expected {
				t.Errorf("getTimeRtt(%q) = %+v, want %+v", tc.arg, got, tc.expected)
			}
		})
	}
}

// TestRunRouteWrite is the CLI end to end: the GOIP_WRITE_OUT hook, the
// refusal without it, and the exit codes. The rows are dev-less, so no
// lookup is sent and the replay source is never asked for anything.
//
// go test ./internal/goip/ -run TestRunRouteWrite
func TestRunRouteWrite(t *testing.T) {
	want, err := xtcpnl.BuildRouteWriteRequest(xtcpnl.RouteWrite{
		MsgType: unix.RTM_NEWROUTE, Flags: unix.NLM_F_CREATE | unix.NLM_F_EXCL | unix.NLM_F_ACK,
		Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN, Protocol: unix.RTPROT_BOOT,
		Type: unix.RTN_BLACKHOLE, Attrs: []xtcpnl.RouteAttr{rtaDst("198.51.100.0")},
	}, 1)
	if err != nil {
		t.Fatal(err)
	}
	type hook uint8
	const (
		hookUnset hook = iota
		hookEmpty
		hookSet
	)
	type result struct {
		code      int
		stderr    string // a fragment; "" for empty
		writeFile []byte // nil when no file is expected
	}
	tests := []struct {
		description string
		args        []string
		hook        hook
		expected    result
	}{
		{description: "positive: the request goes to GOIP_WRITE_OUT and nothing is printed",
			args: []string{"route", "add", "blackhole", "198.51.100.0/24"}, hook: hookSet,
			expected: result{code: ExitOK, writeFile: want}},
		{description: "corner: r a bl is route add blackhole",
			args: []string{"r", "a", "bl", "198.51.100.0/24"}, hook: hookSet,
			expected: result{code: ExitOK, writeFile: want}},
		{description: "negative: without GOIP_WRITE_OUT the write is refused",
			args:     []string{"route", "add", "blackhole", "198.51.100.0/24"},
			expected: result{code: ExitUsage, stderr: "never applies a write"}},
		{description: "corner: an empty GOIP_WRITE_OUT is no hook",
			args: []string{"route", "add", "blackhole", "198.51.100.0/24"}, hook: hookEmpty,
			expected: result{code: ExitUsage, stderr: "never applies a write"}},
		{description: "negative: an argv error wins over the missing hook",
			args:     []string{"route", "add", "blackhole", "198.51.100.0/24", "quickack", "2"},
			expected: result{code: ExitUsage, stderr: `"quickack" value should be 0 or 1`}},
		{description: "negative: an argv error writes no file",
			args: []string{"route", "add", "blackhole", "198.51.100.0/24", "quickack", "2"}, hook: hookSet,
			expected: result{code: ExitUsage, stderr: `"quickack" value should be 0 or 1`}},
		{description: "boundary: an unimplemented keyword writes no file",
			args: []string{"route", "add", "198.51.100.0/24", "nexthop", "via", "192.0.2.1"}, hook: hookSet,
			expected: result{code: ExitUsage, stderr: "not implemented"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", linkDumpPcap)
			out := filepath.Join(t.TempDir(), "write.bin")
			switch tc.hook {
			case hookSet:
				t.Setenv(writeOutEnvCst, out)
			case hookEmpty:
				t.Setenv(writeOutEnvCst, "")
			case hookUnset:
				t.Setenv(writeOutEnvCst, "")
				if err := os.Unsetenv(writeOutEnvCst); err != nil {
					t.Fatal(err)
				}
			}
			var stdout, stderr bytes.Buffer
			code := Run(tc.args, &stdout, &stderr)
			if code != tc.expected.code {
				t.Errorf("code = %d, want %d; stderr %q", code, tc.expected.code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want nothing", stdout.String())
			}
			if !strings.Contains(stderr.String(), tc.expected.stderr) || (tc.expected.stderr == "") != (stderr.Len() == 0) {
				t.Errorf("stderr = %q, want %q", stderr.String(), tc.expected.stderr)
			}
			got, rerr := os.ReadFile(out)
			switch {
			case tc.expected.writeFile == nil && rerr == nil:
				t.Errorf("wrote %x, want no file", got)
			case tc.expected.writeFile != nil && !bytes.Equal(got, tc.expected.writeFile):
				t.Errorf("wrote %x (%v)\nwant  %x", got, rerr, tc.expected.writeFile)
			}
		})
	}
}
