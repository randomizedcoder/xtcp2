package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The positive rows in this file are transcribed from the route sidecars
// captured in the gated microVM topologies, and every one of them cites the
// golden file and line it came from:
//
//	pkg/xtcpnl/testdata/7_1_4/dumps/ip_route_main       — `ip route show`
//	pkg/xtcpnl/testdata/7_1_4/dumps/ip_route6           — `ip -6 route show`
//	pkg/xtcpnl/testdata/7_1_4/dumps/ip_route_table_all  — `ip route show table all`
//	pkg/xtcpnl/testdata/7_1_4/dumps/mesh/ip_route_main  — the linkdown topology
//	pkg/xtcpnl/testdata/7_1_4/dumps/mesh/ip_route6
//
// The xtcpnl.RouteInfo inputs are not invented to fit those strings: they are
// what ParseNewRoute decodes from the RTM_NEWROUTE replies in the matching
// netlink_route_getroute*.pcap, field for field. A citation here is therefore
// a claim that the pinned `ip` 7.1.0 printed exactly this line for exactly
// these bytes.
//
// Two reading notes, both of which a "tidied up" expectation would break:
//
//   - Every IPv4 line ends in a trailing SPACE before the newline, and every
//     IPv6 line ends flush at `pref medium`. That is print_rt_pref
//     (ip/iproute.c:419-439) being the one token in print_route whose format
//     has no trailing space. Check the goldens with `cat -A`.
//   - A route with next hops ends its own line with a trailing space and NO
//     newline; each `\n\tnexthop ` supplies the break. print_route emits the
//     multipath block last for exactly that reason (ip/iproute.c:1010).
//
// Rows marked corner reason from the C source rather than from observation,
// because the topologies never produced the line.

// routeTabNames is the gated topology's index cache: `lo` is 1 and the dummy
// `goip0` is 3, which is what every RTA_OIF in the v4 and v6 captures names.
var routeTabNames = fakeNames{
	1: {name: "lo"},
	3: {name: "goip0"},
}

// routeMeshTabNames is the mesh topology's, where index 3 is the bridge whose
// carrier is down — the only topology that produces a `linkdown` token.
var routeMeshTabNames = fakeNames{
	1: {name: "lo"},
	3: {name: "br0"},
}

// filterMain is `ip route show`: filter.tb defaults to RT_TABLE_MAIN
// (ip/iproute.c:1835), which SUPPRESSES the `table` token everywhere.
var filterMain = RouteShowFilter{Table: unix.RT_TABLE_MAIN}

// filterAll is `ip route show table all`: filter.tb is 0, which is what lets
// the `table local` token through on the local-table routes.
var filterAll = RouteShowFilter{Table: 0}

// filterMainDetails and filterAllDetails are the same two commands under -d.
// See RouteShowFilter.Details for the four guards that weakens.
var filterMainDetails = RouteShowFilter{Table: unix.RT_TABLE_MAIN, Details: true}
var filterAllDetails = RouteShowFilter{Table: 0, Details: true}

// mx builds the RouteMetrics a decoded RTA_METRICS would have produced.
// Pairs are {RTAX_*, value}; the presence bit is set for each, so a metric
// explicitly set to zero stays distinguishable from an absent one.
func mx(pairs ...[2]uint32) *xtcpnl.RouteMetrics {
	m := &xtcpnl.RouteMetrics{}
	for _, p := range pairs {
		m.Present |= 1 << p[0]
		m.Values[p[0]] = p[1]
	}
	return m
}

// mxLock builds a RouteMetrics carrying an RTAX_LOCK mask on top of pairs.
func mxLock(lock uint32, pairs ...[2]uint32) *xtcpnl.RouteMetrics {
	m := mx(pairs...)
	m.Present |= 1 << unix.RTAX_LOCK
	m.Values[unix.RTAX_LOCK] = lock
	return m
}

func TestRouteViewOfText(t *testing.T) {
	tests := []struct {
		description string
		in          xtcpnl.RouteInfo
		filter      RouteShowFilter
		tab         NameTab // nil means routeTabNames
		want        string
	}{
		// ---------------------------------------------------------------
		// ip_route_main — `ip route show`, filter.tb = RT_TABLE_MAIN
		// ---------------------------------------------------------------
		{
			description: "positive: ip_route_main:1 — the connected prefix, proto kernel scope link with a preferred source",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_LINK, Type: unix.RTN_UNICAST,
				Protocol: unix.RTPROT_KERNEL,
				Dst:      v4(192, 0, 2, 0), PrefSrc: v4(192, 0, 2, 1), Oif: 3,
			},
			filter: filterMain,
			want:   "192.0.2.0/24 dev goip0 proto kernel scope link src 192.0.2.1 \n",
		},
		{
			// rtm_protocol is RTPROT_BOOT and rtm_scope is RT_SCOPE_UNIVERSE,
			// and print_route suppresses BOTH tokens for exactly those values
			// (ip/iproute.c:906-917). A renderer that printed them
			// unconditionally would add `proto boot scope global` to this line.
			description: "positive: ip_route_main:2 — a boot-proto universe-scope route prints neither proto nor scope",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_UNICAST,
				Protocol: unix.RTPROT_BOOT,
				Dst:      v4(198, 18, 0, 0), Gateway: v4(192, 0, 2, 10), Oif: 3,
			},
			filter: filterMain,
			want:   "198.18.0.0/24 via 192.0.2.10 dev goip0 \n",
		},
		{
			description: "positive: ip_route_main:3 — RTA_METRICS renders mtu then advmss, in RTAX_* order",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v4(198, 18, 1, 0), Gateway: v4(192, 0, 2, 10), Oif: 3,
				Metrics: mx([2]uint32{unix.RTAX_MTU, 1400}, [2]uint32{unix.RTAX_ADVMSS, 1300}),
			},
			filter: filterMain,
			want:   "198.18.1.0/24 via 192.0.2.10 dev goip0 mtu 1400 advmss 1300 \n",
		},
		{
			// RFC 5549: an IPv4 route whose next hop is an IPv6 address. The
			// gateway arrives in RTA_VIA, not RTA_GATEWAY, and print_rta_via
			// prints the family name between the keyword and the address.
			description: "positive: ip_route_main:4 — RTA_VIA renders `via inet6 ADDR`, not a bare `via ADDR`",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v4(198, 18, 2, 0), Oif: 3, HasVia: true,
				Via: &xtcpnl.RtVia{Family: unix.AF_INET6, Addr: v6(t, "2001:db8::2")},
			},
			filter: filterMain,
			want:   "198.18.2.0/24 via inet6 2001:db8::2 dev goip0 \n",
		},
		{
			description: "positive: ip_route_main:5 — a scope-link route with no gateway and no preferred source",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_LINK, Type: unix.RTN_UNICAST,
				Protocol: unix.RTPROT_BOOT,
				Dst:      v4(198, 51, 100, 0), Oif: 3,
			},
			filter: filterMain,
			want:   "198.51.100.0/24 dev goip0 scope link \n",
		},
		{
			// The ECMP route. It carries NO top-level RTA_OIF and NO
			// RTA_GATEWAY — both live inside RTA_MULTIPATH — so the route's own
			// line is just the prefix and a trailing space. rtnh_hops 0 and 2
			// render as weight 1 and weight 3.
			description: "positive: ip_route_main:6-8 — an ECMP route renders both next hops with weight 1 and weight 3",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v4(203, 0, 113, 0), HasMultipath: true,
				Multipath: []xtcpnl.RouteNextHop{
					{Hops: 0, Ifindex: 3, Gateway: v4(192, 0, 2, 10)},
					{Hops: 2, Ifindex: 3, Gateway: v4(192, 0, 2, 11)},
				},
			},
			filter: filterMain,
			want: "203.0.113.0/24 " +
				"\n\tnexthop via 192.0.2.10 dev goip0 weight 1 " +
				"\n\tnexthop via 192.0.2.11 dev goip0 weight 3 \n",
		},

		// ---------------------------------------------------------------
		// ip_route6 — `ip -6 route show`
		// ---------------------------------------------------------------
		{
			// The v6 routes are where RTA_PRIORITY presence and RTA_PREF both
			// show up: the kernel sends both on every v6 route, so every line
			// carries `metric N` and ends flush at `pref medium`.
			description: "positive: ip_route6:1 — a v6 connected prefix ends flush at `pref medium`, with no trailing space",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET6, DstLen: 64, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_KERNEL,
				Dst: v6(t, "2001:db8::"), Oif: 3,
				Priority: 256, HasPriority: true,
				Pref: icmpv6RouterPrefMedium, HasPref: true,
			},
			filter: filterMain,
			want:   "2001:db8::/64 dev goip0 proto kernel metric 256 pref medium\n",
		},
		{
			description: "positive: ip_route6:2 — a v6 route via a v6 gateway",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET6, DstLen: 64, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v6(t, "2001:db8:1::"), Gateway: v6(t, "2001:db8::2"), Oif: 3,
				Priority: 1024, HasPriority: true,
				Pref: icmpv6RouterPrefMedium, HasPref: true,
			},
			filter: filterMain,
			want:   "2001:db8:1::/64 via 2001:db8::2 dev goip0 metric 1024 pref medium\n",
		},
		{
			// pref comes BEFORE the multipath block, so a v6 ECMP route's own
			// line ends `pref medium` with no space and the first nexthop's
			// "\n\t" supplies the break.
			description: "positive: ip_route6:3-5 — a v6 ECMP route prints pref before the nexthop block",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET6, DstLen: 64, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v6(t, "2001:db8:2::"), Priority: 1024, HasPriority: true,
				Pref: icmpv6RouterPrefMedium, HasPref: true, HasMultipath: true,
				Multipath: []xtcpnl.RouteNextHop{
					{Hops: 0, Ifindex: 3, Gateway: v6(t, "2001:db8::2")},
					{Hops: 2, Ifindex: 3, Gateway: v6(t, "2001:db8::3")},
				},
			},
			filter: filterMain,
			want: "2001:db8:2::/64 metric 1024 pref medium" +
				"\n\tnexthop via 2001:db8::2 dev goip0 weight 1 " +
				"\n\tnexthop via 2001:db8::3 dev goip0 weight 3 \n",
		},
		{
			description: "positive: ip_route6:6 — the link-local prefix",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET6, DstLen: 64, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_KERNEL,
				Dst: v6(t, "fe80::"), Oif: 3,
				Priority: 256, HasPriority: true,
				Pref: icmpv6RouterPrefMedium, HasPref: true,
			},
			filter: filterMain,
			want:   "fe80::/64 dev goip0 proto kernel metric 256 pref medium\n",
		},

		// ---------------------------------------------------------------
		// ip_route_table_all — `ip route show table all`, filter.tb = 0
		// ---------------------------------------------------------------
		{
			// The same bytes as ip_route_main:1, rendered under filter.tb = 0.
			// It still prints no `table` token, because the gate is
			// `table != RT_TABLE_MAIN` as well as `!filter.tb`
			// (ip/iproute.c:903) — so dropping the filter does not suddenly
			// annotate the main table.
			description: "boundary: ip_route_table_all:1 — a main-table route prints no table token even with no table filter",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_LINK, Type: unix.RTN_UNICAST,
				Protocol: unix.RTPROT_KERNEL,
				Dst:      v4(192, 0, 2, 0), PrefSrc: v4(192, 0, 2, 1), Oif: 3,
			},
			filter: filterAll,
			want:   "192.0.2.0/24 dev goip0 proto kernel scope link src 192.0.2.1 \n",
		},
		{
			description: "positive: ip_route_table_all:9 — a local-table route prints its type prefix and `table local`",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 8, Table: unix.RT_TABLE_LOCAL,
				Scope: unix.RT_SCOPE_HOST, Type: unix.RTN_LOCAL,
				Protocol: unix.RTPROT_KERNEL,
				Dst:      v4(127, 0, 0, 0), PrefSrc: v4(127, 0, 0, 1), Oif: 1,
			},
			filter: filterAll,
			want:   "local 127.0.0.0/8 dev lo table local proto kernel scope host src 127.0.0.1 \n",
		},
		{
			// rtm_dst_len == host_len, so print_route drops the `/32` and
			// prints a bare host address (ip/iproute.c:838-849).
			description: "boundary: ip_route_table_all:10 — a dst_len equal to host_len renders a bare address with no prefix length",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 32, Table: unix.RT_TABLE_LOCAL,
				Scope: unix.RT_SCOPE_HOST, Type: unix.RTN_LOCAL,
				Protocol: unix.RTPROT_KERNEL,
				Dst:      v4(127, 0, 0, 1), PrefSrc: v4(127, 0, 0, 1), Oif: 1,
			},
			filter: filterAll,
			want:   "local 127.0.0.1 dev lo table local proto kernel scope host src 127.0.0.1 \n",
		},
		{
			description: "positive: ip_route_table_all:11 — a broadcast route's type prefix is `broadcast`",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 32, Table: unix.RT_TABLE_LOCAL,
				Scope: unix.RT_SCOPE_LINK, Type: unix.RTN_BROADCAST,
				Protocol: unix.RTPROT_KERNEL,
				Dst:      v4(127, 255, 255, 255), PrefSrc: v4(127, 0, 0, 1), Oif: 1,
			},
			filter: filterAll,
			want:   "broadcast 127.255.255.255 dev lo table local proto kernel scope link src 127.0.0.1 \n",
		},
		{
			// RTA_PRIORITY is PRESENT with the value 0 on every v6 local-table
			// route, which is why the token appears as `metric 0` rather than
			// being suppressed. This is the row that would break if Metric were
			// a plain uint32 keyed on value rather than a pointer keyed on
			// presence.
			description: "boundary: ip_route_table_all:20 — RTA_PRIORITY present with value 0 prints `metric 0`, not nothing",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET6, DstLen: 128, Table: unix.RT_TABLE_LOCAL,
				Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_LOCAL,
				Protocol: unix.RTPROT_KERNEL,
				Dst:      v6(t, "::1"), Oif: 1,
				Priority: 0, HasPriority: true,
				Pref: icmpv6RouterPrefMedium, HasPref: true,
			},
			filter: filterAll,
			want:   "local ::1 dev lo table local proto kernel metric 0 pref medium\n",
		},
		{
			description: "positive: ip_route_table_all:24 — a multicast route's type prefix is `multicast`",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET6, DstLen: 8, Table: unix.RT_TABLE_LOCAL,
				Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_MULTICAST,
				Protocol: unix.RTPROT_KERNEL,
				Dst:      v6(t, "ff00::"), Oif: 3,
				Priority: 256, HasPriority: true,
				Pref: icmpv6RouterPrefMedium, HasPref: true,
			},
			filter: filterAll,
			want:   "multicast ff00::/8 dev goip0 table local proto kernel metric 256 pref medium\n",
		},

		// ---------------------------------------------------------------
		// mesh/ — the only topology with a down carrier
		// ---------------------------------------------------------------
		{
			// rtm_flags = RTNH_F_LINKDOWN (16). print_rt_flags runs after `src`
			// and before RTA_METRICS, so the token lands at the end of the line
			// rather than next to `dev`.
			description: "positive: mesh/ip_route_main:1 — RTNH_F_LINKDOWN prints `linkdown` after src",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_LINK, Type: unix.RTN_UNICAST,
				Protocol: unix.RTPROT_KERNEL, Flags: unix.RTNH_F_LINKDOWN,
				Dst: v4(198, 19, 0, 0), PrefSrc: v4(198, 19, 0, 1), Oif: 3,
			},
			filter: filterMain,
			tab:    routeMeshTabNames,
			want:   "198.19.0.0/24 dev br0 proto kernel scope link src 198.19.0.1 linkdown \n",
		},
		{
			description: "positive: mesh/ip_route_main:2 — linkdown on a route with no preferred source",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_LINK, Type: unix.RTN_UNICAST,
				Protocol: unix.RTPROT_BOOT, Flags: unix.RTNH_F_LINKDOWN,
				Dst: v4(198, 19, 1, 0), Oif: 3,
			},
			filter: filterMain,
			tab:    routeMeshTabNames,
			want:   "198.19.1.0/24 dev br0 scope link linkdown \n",
		},
		{
			// The ordering proof: flags come BEFORE pref, so the v6 line reads
			// `metric 256 linkdown pref medium` and not `... pref medium
			// linkdown`.
			description: "positive: mesh/ip_route6:1 — linkdown is printed before pref on a v6 route",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET6, DstLen: 64, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_KERNEL,
				Flags: unix.RTNH_F_LINKDOWN,
				Dst:   v6(t, "2001:db8:ff::"), Oif: 3,
				Priority: 256, HasPriority: true,
				Pref: icmpv6RouterPrefMedium, HasPref: true,
			},
			filter: filterMain,
			tab:    routeMeshTabNames,
			want:   "2001:db8:ff::/64 dev br0 proto kernel metric 256 linkdown pref medium\n",
		},

		// ---------------------------------------------------------------
		// boundary / negative / corner — reasoned from ip/iproute.c
		// ---------------------------------------------------------------
		{
			// No RTA_DST and rtm_dst_len 0 is the default route
			// (ip/iproute.c:851-852). "0.0.0.0/0" would be a divergence, not a
			// synonym.
			description: "boundary: no RTA_DST with dst_len 0 renders `default`, not 0.0.0.0/0",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 0, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Gateway: v4(192, 0, 2, 10), Oif: 3,
			},
			filter: filterMain,
			want:   "default via 192.0.2.10 dev goip0 \n",
		},
		{
			// No RTA_DST but a non-zero rtm_dst_len. C's format is `"0/%d "`
			// with the space INSIDE the string, and print_color_string then adds
			// its own — so the line really does carry two spaces there
			// (ip/iproute.c:849,853-854). Reproduced rather than tidied.
			description: "corner: no RTA_DST with a non-zero dst_len renders `0/24` followed by two spaces",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT, Oif: 3,
			},
			filter: filterMain,
			want:   "0/24  dev goip0 \n",
		},
		{
			// The source branch's no-attribute format is `"0/%u"` with NO
			// embedded space, unlike the destination's. The two branches differ
			// by one character in the C and they differ by one character here.
			description: "corner: a non-zero src_len with no RTA_SRC renders `from 0/24 ` with a single space",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, SrcLen: 24, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v4(198, 18, 0, 0), Oif: 3,
			},
			filter: filterMain,
			want:   "198.18.0.0/24 from 0/24 dev goip0 \n",
		},
		{
			description: "corner: RTA_SRC renders a `from PREFIX` token after the destination",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, SrcLen: 24, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v4(198, 18, 0, 0), Src: v4(10, 0, 0, 0), Oif: 3,
			},
			filter: filterMain,
			want:   "198.18.0.0/24 from 10.0.0.0/24 dev goip0 \n",
		},
		{
			// RTA_OIF is absent, which for RouteInfo means Oif == 0 — ifindex 0
			// is not a device and the kernel never sends it. The token must be
			// dropped, not rendered `dev *`, and obj_route must not send a
			// single-get for index 0 either.
			description: "boundary: a route with no RTA_OIF prints no dev token at all",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNREACHABLE, Protocol: unix.RTPROT_BOOT,
				Dst: v4(198, 18, 9, 0),
			},
			filter: filterMain,
			want:   "unreachable 198.18.9.0/24 \n",
		},
		{
			// ll_index_to_name's own fallback (lib/ll_map.c:327) when its live
			// single-get resolves nothing. goip must print the same string
			// rather than dropping the token or erroring the whole dump.
			description: "negative: an RTA_OIF the cache cannot resolve renders the if%u fallback",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v4(198, 18, 8, 0), Oif: 99,
			},
			filter: filterMain,
			want:   "198.18.8.0/24 dev if99 \n",
		},
		{
			// rtnl_rtntype_n2a spells RTN_UNSPEC "none", not "unspec"
			// (ip/rtm_map.c:22) — the one RTN_/RTPROT_ pair whose zero values
			// disagree. And 0 != RTN_UNICAST, so the prefix is printed.
			description: "corner: rtm_type RTN_UNSPEC renders the type prefix `none`, not `unspec`",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNSPEC, Protocol: unix.RTPROT_BOOT,
				Dst: v4(198, 18, 7, 0), Oif: 3,
			},
			filter: filterMain,
			want:   "none 198.18.7.0/24 dev goip0 \n",
		},
		{
			// af_bit_len knows nothing about family 99, so host_len is 0,
			// dst_len != host_len and the prefix form is taken; addrString
			// cannot parse the bytes as an address so it hex-dumps them. The
			// assertion is that an unknown family degrades rather than panics.
			description: "corner: an unknown rtm_family hex-dumps the destination instead of panicking",
			in: xtcpnl.RouteInfo{
				Family: 99, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v4(192, 0, 2, 0), Oif: 3,
			},
			filter: filterMain,
			want:   "c0000200/24 dev goip0 \n",
		},
		{
			// get_real_family maps RTNL_FAMILY_IPMR (128) to AF_INET so the
			// address formats, while host_len still comes from rtm_family — so
			// it is 0, dst_len 32 != 0, and the /32 is printed even though 32
			// IS the v4 host length. Both halves of that asymmetry are the
			// C's (ip/iproute.c:794,836).
			description: "corner: a multicast route from the v4 multicast FIB formats as v4 but keeps its prefix length",
			in: xtcpnl.RouteInfo{
				Family: 128, DstLen: 32, Table: unix.RT_TABLE_LOCAL,
				Type: unix.RTN_MULTICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v4(224, 0, 0, 1), Oif: 3,
			},
			filter: filterAll,
			want:   "multicast 224.0.0.1/32 dev goip0 table local \n",
		},
		{
			// rtnl_dsfield_n2a has one built-in entry, `[0] = "0"`, and
			// print_route only emits the token for a non-zero tos — so the
			// entry is unreachable and every tos goip prints is the `0x%02x`
			// fallback. See the file header.
			description: "corner: a non-zero rtm_tos renders as a two-digit hex dsfield",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Tos: 0x10, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v4(198, 18, 6, 0), Oif: 3,
			},
			filter: filterMain,
			want:   "198.18.6.0/24 tos 0x10 dev goip0 \n",
		},
		{
			description: "corner: RTA_NH_ID renders an `nhid` token before the gateway",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v4(198, 18, 5, 0), NhID: 17,
			},
			filter: filterMain,
			want:   "198.18.5.0/24 nhid 17 \n",
		},
		{
			// RTA_IIF is only ever set on a cloned or multicast route, neither
			// of which the gated topologies produce — so this row is
			// constructed. It exists because RouteInfo.Iif was added for this
			// token and an unread field is an untested one.
			description: "corner: RTA_IIF renders an `iif` token after the metrics block",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 32, Table: unix.RT_TABLE_LOCAL,
				Type: unix.RTN_MULTICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v4(224, 0, 0, 1), Iif: 3, Oif: 1,
			},
			filter: filterAll,
			want:   "multicast 224.0.0.1 dev lo table local iif goip0 \n",
		},
		{
			// print_route puts proto and scope inside `if (!(rtm_flags &
			// RTM_F_CLONED))` (ip/iproute.c:905-919), so a cloned route prints
			// neither even when both are non-default. goip's routeFilter drops
			// cloned routes before they get here — as `ip` does — so this row
			// covers the guard for a caller that renders one directly.
			description: "corner: RTM_F_CLONED suppresses both the proto and scope tokens",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 32, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_LINK, Type: unix.RTN_UNICAST,
				Protocol: unix.RTPROT_KERNEL, Flags: unix.RTM_F_CLONED,
				Dst: v4(198, 18, 4, 1), Oif: 3,
			},
			filter: filterMain,
			want:   "198.18.4.1 dev goip0 \n",
		},
		{
			// family_name returns the literal "???" for a family it does not
			// know, and print_rta_via has no branch that suppresses it. addr.go's
			// familyName deliberately returns "" instead, which is why route.go
			// wraps it — this row is what pins that wrapper.
			description: "corner: RTA_VIA with an unknown family renders the literal ??? sentinel",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v4(198, 18, 3, 0), Oif: 3, HasVia: true,
				Via: &xtcpnl.RtVia{Family: 99, Addr: []byte{0xde, 0xad}},
			},
			filter: filterMain,
			want:   "198.18.3.0/24 via ??? dead dev goip0 \n",
		},
		{
			// print_rta_multipath passes the ROUTE's family down
			// (ip/iproute.c:735), and AF_MPLS is the one family whose next hops
			// print no weight at all (:754). Every other family's nexthop
			// carries `weight rtnh_hops + 1`.
			description: "corner: an AF_MPLS route's next hops print no weight token",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_MPLS, DstLen: 20, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Dst: []byte{0x00, 0x00, 0x64, 0x00}, HasMultipath: true,
				Multipath: []xtcpnl.RouteNextHop{{Hops: 0, Ifindex: 3}},
			},
			filter: filterMain,
			want:   "00006400 \n\tnexthop dev goip0 \n",
		},
		{
			// A nexthop's rtnh_flags go through the same print_rt_flags as the
			// route's rtm_flags, so a single dead path is annotated in place
			// rather than collapsing the whole route.
			description: "corner: a nexthop's own rtnh_flags render after its weight",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v4(203, 0, 113, 0), HasMultipath: true,
				Multipath: []xtcpnl.RouteNextHop{
					{Hops: 0, Ifindex: 3, Gateway: v4(192, 0, 2, 10), Flags: unix.RTNH_F_DEAD},
				},
			},
			filter: filterMain,
			want:   "203.0.113.0/24 \n\tnexthop via 192.0.2.10 dev goip0 weight 1 dead \n",
		},
		{
			// A named table that is neither main nor one of the three built-in
			// entries. rtnl_rttable_n2a's fallback is `%u`, and goip reads no
			// /etc/iproute2/rt_tables, so a host that HAS named table 100
			// diverges here. Documented in the file header.
			description: "negative: a table with no built-in name renders as its number, since no config file is read",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: 100,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v4(198, 18, 100, 0), Oif: 3,
			},
			filter: filterAll,
			want:   "198.18.100.0/24 dev goip0 table 100 \n",
		},
		{
			// rt_protos ships `84 ovn`, but the built-in rtnl_rtprot_tab does
			// not, so goip prints the number. Same divergence class as the
			// table row above, and named in the file header.
			description: "negative: a protocol with no built-in name renders as its number",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: 84,
				Dst: v4(198, 18, 84, 0), Oif: 3,
			},
			filter: filterMain,
			want:   "198.18.84.0/24 dev goip0 proto 84 \n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			tab := tc.tab
			if tab == nil {
				tab = routeTabNames
			}
			got := RouteViewOf(tc.in, tab, tc.filter).Text()
			if got != tc.want {
				t.Errorf("Text() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestRouteMetricsViewOf covers print_rta_metrics' four easy-to-lose rules —
// the lock mask, the HOPLIMIT -1 skip, the RTT/RTTVAR divisions and the
// seconds/milliseconds split — plus the three places where the text form and
// the JSON form deliberately disagree.
func TestRouteMetricsViewOf(t *testing.T) {
	tests := []struct {
		description string
		in          *xtcpnl.RouteMetrics
		wantNil     bool
		wantText    string
		wantJSON    string
	}{
		{
			description: "boundary: a route with no RTA_METRICS has no metrics view at all",
			in:          nil,
			wantNil:     true,
		},
		{
			// print_rta_metrics opens its JSON array and object before it looks
			// at anything (ip/iproute.c:627-628), so an RTA_METRICS carrying
			// nothing decodable still emits `"metrics": [ { } ]`.
			description: "boundary: an empty RTA_METRICS renders no text but still emits an empty one-object array",
			in:          mx(),
			wantText:    "",
			wantJSON:    `[{}]`,
		},
		{
			description: "positive: ip_route_main:3 — mtu then advmss, in RTAX_* order and not alphabetically",
			in:          mx([2]uint32{unix.RTAX_MTU, 1400}, [2]uint32{unix.RTAX_ADVMSS, 1300}),
			wantText:    "mtu 1400 advmss 1300 ",
			wantJSON:    `[{"mtu":1400,"advmss":1300}]`,
		},
		{
			// The whole reason MarshalJSON is hand written: encoding/json over a
			// map would sort these keys and put advmss first, which is not what
			// `ip -j route` emits.
			description: "corner: declaring advmss before mtu does not change the emitted order",
			in:          mx([2]uint32{unix.RTAX_ADVMSS, 1300}, [2]uint32{unix.RTAX_MTU, 1400}),
			wantText:    "mtu 1400 advmss 1300 ",
			wantJSON:    `[{"mtu":1400,"advmss":1300}]`,
		},
		{
			description: "positive: a locked metric that is also present prints `lock` between the name and the value",
			in:          mxLock(1<<unix.RTAX_MTU, [2]uint32{unix.RTAX_MTU, 1400}),
			wantText:    "mtu lock 1400 ",
			wantJSON:    `[{"mtu":1400}]`,
		},
		{
			// The lock bit alone is enough to print the entry: the loop's skip
			// is `mxrta[i] == NULL && !(mxlock & (1 << i))`, so a locked-but-
			// absent metric prints with the value 0.
			description: "boundary: a metric that is locked but absent still prints, with the value 0",
			in:          mxLock(1 << unix.RTAX_MTU),
			wantText:    "mtu lock 0 ",
			wantJSON:    `[{"mtu":0}]`,
		},
		{
			// The kernel writes -1 for "use the system default". Printing it
			// would give 4294967295, which is worse than printing nothing, so
			// print_rta_metrics skips the entry entirely.
			description: "boundary: RTAX_HOPLIMIT of -1 is skipped rather than printed as 4294967295",
			in:          mx([2]uint32{unix.RTAX_HOPLIMIT, 0xffffffff}),
			wantText:    "",
			wantJSON:    `[{}]`,
		},
		{
			description: "positive: a real RTAX_HOPLIMIT is printed",
			in:          mx([2]uint32{unix.RTAX_HOPLIMIT, 64}),
			wantText:    "hoplimit 64 ",
			wantJSON:    `[{"hoplimit":64}]`,
		},
		{
			// RTAX_RTT is stored in eighths of a millisecond. 7992/8 = 999,
			// which is one below the seconds threshold, so it takes the `%ums`
			// branch.
			description: "boundary: RTAX_RTT divided by 8 landing at 999 takes the milliseconds form",
			in:          mx([2]uint32{unix.RTAX_RTT, 7992}),
			wantText:    "rtt 999ms ",
			wantJSON:    `[{"rtt":999}]`,
		},
		{
			description: "boundary: RTAX_RTT divided by 8 landing at exactly 1000 takes the seconds form",
			in:          mx([2]uint32{unix.RTAX_RTT, 8000}),
			wantText:    "rtt 1s ",
			wantJSON:    `[{"rtt":1000}]`,
		},
		{
			// C's %g defaults to six significant digits, so 1234.567 prints as
			// 1234.57. Go's plain %g is shortest-round-trip and would print
			// 1234.567; only %.6g matches. This row is the reason for that
			// precision.
			description: "corner: the seconds form keeps C's six significant digits, so 1234.567 prints 1234.57",
			in:          mx([2]uint32{unix.RTAX_RTT, 8 * 1234567}),
			wantText:    "rtt 1234.57s ",
			wantJSON:    `[{"rtt":1234567}]`,
		},
		{
			description: "positive: RTAX_RTTVAR is divided by 4, not by 8",
			in:          mx([2]uint32{unix.RTAX_RTTVAR, 4000}),
			wantText:    "rttvar 1s ",
			wantJSON:    `[{"rttvar":1000}]`,
		},
		{
			description: "boundary: RTAX_RTO_MIN is not divided at all",
			in:          mx([2]uint32{unix.RTAX_RTO_MIN, 200}),
			wantText:    "rto_min 200ms ",
			wantJSON:    `[{"rto_min":200}]`,
		},
		{
			// The text name is "congctl" and the JSON key is "congestion" —
			// two different strings in the same call (ip/iproute.c:686-688),
			// not a transcription slip.
			description: "corner: RTAX_CC_ALGO's text name congctl and JSON key congestion disagree by design",
			in: func() *xtcpnl.RouteMetrics {
				m := mx()
				m.Present |= 1 << unix.RTAX_CC_ALGO
				m.CcAlgo = "bbr"
				return m
			}(),
			wantText: "congctl bbr ",
			wantJSON: `[{"congestion":"bbr"}]`,
		},
		{
			description: "positive: RTAX_FEATURES with the ECN bit prints a bare `ecn` and a null-valued JSON key",
			in:          mx([2]uint32{unix.RTAX_FEATURES, unix.RTAX_FEATURE_ECN}),
			wantText:    "features ecn ",
			wantJSON:    `[{"ecn":null}]`,
		},
		{
			description: "positive: RTAX_FEATURES with the TCP_USEC_TS bit prints tcp_usec_ts",
			in:          mx([2]uint32{unix.RTAX_FEATURES, unix.RTAX_FEATURE_TCP_USEC_TS}),
			wantText:    "features tcp_usec_ts ",
			wantJSON:    `[{"tcp_usec_ts":null}]`,
		},
		{
			// print_rtax_features captures `of` BEFORE it clears the named bits,
			// so the residual it prints is the ORIGINAL value — 0x101, not the
			// 0x100 that is actually left over. Transcribed, not corrected.
			description: "corner: an unrecognized feature bit prints the ORIGINAL value in hex, not the residual",
			in:          mx([2]uint32{unix.RTAX_FEATURES, unix.RTAX_FEATURE_ECN | 0x100}),
			wantText:    "features ecn 0x101 ",
			wantJSON:    `[{"ecn":null,"features":257}]`,
		},
		{
			description: "boundary: RTAX_FEATURES with no bits set prints the name and nothing else",
			in:          mx([2]uint32{unix.RTAX_FEATURES, 0}),
			wantText:    "features ",
			wantJSON:    `[{}]`,
		},
		{
			// Every RTAX_* from 2 to RTAX_MAX at once, which is also the proof
			// that the loop visits them in index order rather than in the order
			// they were declared.
			description: "corner: a metrics block holding every RTAX_* renders them in ascending RTAX_* order",
			in: mx(
				[2]uint32{unix.RTAX_QUICKACK, 1},
				[2]uint32{unix.RTAX_MTU, 1500},
				[2]uint32{unix.RTAX_INITCWND, 10},
				[2]uint32{unix.RTAX_WINDOW, 2},
				[2]uint32{unix.RTAX_SSTHRESH, 3},
				[2]uint32{unix.RTAX_CWND, 4},
				[2]uint32{unix.RTAX_REORDERING, 5},
				[2]uint32{unix.RTAX_INITRWND, 6},
				[2]uint32{unix.RTAX_FASTOPEN_NO_COOKIE, 1},
			),
			wantText: "mtu 1500 window 2 ssthresh 3 cwnd 4 reordering 5 " +
				"initcwnd 10 initrwnd 6 quickack 1 fastopen_no_cookie 1 ",
			wantJSON: `[{"mtu":1500,"window":2,"ssthresh":3,"cwnd":4,"reordering":5,` +
				`"initcwnd":10,"initrwnd":6,"quickack":1,"fastopen_no_cookie":1}]`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := RouteMetricsViewOf(tc.in)
			if tc.wantNil {
				if got != nil {
					t.Fatalf("RouteMetricsViewOf() = %v, want nil", *got)
				}
				return
			}
			if got == nil {
				t.Fatal("RouteMetricsViewOf() = nil, want a view")
			}
			if text := got.Text(); text != tc.wantText {
				t.Errorf("Text() = %q, want %q", text, tc.wantText)
			}
			b, err := json.Marshal(got)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(b) != tc.wantJSON {
				t.Errorf("JSON = %s, want %s", b, tc.wantJSON)
			}
		})
	}
}

// TestRtFlagTokens pins print_rt_flags' token order, which is its source order
// and not the numeric order of the bits.
func TestRtFlagTokens(t *testing.T) {
	tests := []struct {
		description string
		in          uint32
		want        []string
	}{
		{
			// Not nil: print_rt_flags opens and closes its JSON array
			// unconditionally, so every entry of the committed
			// ip_route_main_json carries `"flags": [ ]`. A nil slice would
			// marshal as null and break a consumer iterating it.
			description: "boundary: no flags gives an empty non-nil slice, so the JSON is [] and never null",
			in:          0,
			want:        []string{},
		},
		{
			description: "positive: mesh/ip_route_main:1 — RTNH_F_LINKDOWN alone",
			in:          unix.RTNH_F_LINKDOWN,
			want:        []string{"linkdown"},
		},
		{
			// dead is 1, pervasive is 2 and onlink is 4, but print_rt_flags
			// tests onlink SECOND, so the token order is dead, onlink,
			// pervasive. Sorting by bit value would give the wrong line.
			description: "corner: dead|onlink|pervasive prints in source order, not in ascending bit order",
			in:          unix.RTNH_F_DEAD | unix.RTNH_F_ONLINK | unix.RTNH_F_PERVASIVE,
			want:        []string{"dead", "onlink", "pervasive"},
		},
		{
			// print_rt_flags has no `else` for bits it does not know — unlike
			// print_link_flags, which prints a numeric residual. RTM_F_CLONED
			// is real and set on cache entries, and it prints nothing.
			description: "negative: a bit print_rt_flags has no test for prints nothing at all",
			in:          unix.RTM_F_CLONED,
			want:        []string{},
		},
		{
			description: "boundary: RTM_F_OFFLOAD_FAILED, the highest bit with a name",
			in:          unix.RTM_F_OFFLOAD_FAILED,
			want:        []string{"rt_offload_failed"},
		},
		{
			description: "corner: every named bit at once, in full source order",
			in: unix.RTNH_F_DEAD | unix.RTNH_F_ONLINK | unix.RTNH_F_PERVASIVE |
				unix.RTNH_F_OFFLOAD | unix.RTNH_F_TRAP | unix.RTM_F_NOTIFY |
				unix.RTNH_F_LINKDOWN | unix.RTNH_F_UNRESOLVED |
				unix.RTM_F_OFFLOAD | unix.RTM_F_TRAP | unix.RTM_F_OFFLOAD_FAILED,
			want: []string{
				"dead", "onlink", "pervasive", "offload", "trap", "notify",
				"linkdown", "unresolved", "rt_offload", "rt_trap",
				"rt_offload_failed",
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := RtFlagTokens(tc.in)
			if got == nil {
				t.Fatal("RtFlagTokens() = nil, want a non-nil slice")
			}
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Errorf("RtFlagTokens() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestRoutePref covers print_rt_pref, whose two oddities are both observable:
// a named preference has no trailing space, and an unnamed one drops the
// `pref ` keyword from the text while keeping "pref" as the JSON key.
func TestRoutePref(t *testing.T) {
	tests := []struct {
		description string
		in          uint8
		wantJSON    any
		wantText    string
	}{
		{
			// The value every route in the committed v6 goldens carries, and
			// the reason each of those lines ends flush rather than in a space.
			description: "positive: ip_route6:1 — medium is 0 and renders with no trailing space",
			in:          icmpv6RouterPrefMedium,
			wantJSON:    "medium",
			wantText:    "pref medium",
		},
		{
			description: "positive: high is 1, not the largest value",
			in:          icmpv6RouterPrefHigh,
			wantJSON:    "high",
			wantText:    "pref high",
		},
		{
			// The numbering is a two-bit signed field, so low (0b11) is the
			// LARGEST of the three. Treating the values as a rank would swap
			// high and low.
			description: "boundary: low is 3, the largest of the three named values",
			in:          icmpv6RouterPrefLow,
			wantJSON:    "low",
			wantText:    "pref low",
		},
		{
			// 0b10 is reserved by RFC 4191 and a router that sends it is
			// out of spec. print_rt_pref's default prints the bare number with
			// no keyword — while still using "pref" as the JSON key.
			description: "corner: the reserved value 2 prints a bare number with no `pref ` keyword",
			in:          2,
			wantJSON:    uint8(2),
			wantText:    "2",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			gotJSON, gotText := routePref(tc.in)
			if gotJSON != tc.wantJSON {
				t.Errorf("routePref() json = %v (%T), want %v (%T)",
					gotJSON, gotJSON, tc.wantJSON, tc.wantJSON)
			}
			if gotText != tc.wantText {
				t.Errorf("routePref() text = %q, want %q", gotText, tc.wantText)
			}
		})
	}
}

// TestRouteNameTables pins the built-in n2a tables and their fallbacks. Each
// fallback is a different format string in the C, and harmonizing them would
// be a divergence rather than a cleanup.
func TestRouteNameTables(t *testing.T) {
	tests := []struct {
		description string
		got         string
		want        string
	}{
		{
			description: "positive: RTPROT_KERNEL is `kernel`, the proto on every connected route in the goldens",
			got:         routeProtoName(unix.RTPROT_KERNEL),
			want:        "kernel",
		},
		{
			description: "boundary: RTPROT_UNSPEC is `unspec`, unlike RTN_UNSPEC which is `none`",
			got:         routeProtoName(unix.RTPROT_UNSPEC),
			want:        "unspec",
		},
		{
			description: "negative: rtnl_rtprot_n2a's fallback is unsigned decimal",
			got:         routeProtoName(200),
			want:        "200",
		},
		{
			description: "positive: RTN_UNREACHABLE is `unreachable`",
			got:         routeTypeName(unix.RTN_UNREACHABLE),
			want:        "unreachable",
		},
		{
			description: "boundary: RTN_XRESOLVE is the last entry of rtnl_rtntype_n2a's switch",
			got:         routeTypeName(unix.RTN_XRESOLVE),
			want:        "xresolve",
		},
		{
			description: "negative: a route type past the end of the table falls back to decimal",
			got:         routeTypeName(200),
			want:        "200",
		},
		{
			description: "positive: RT_TABLE_LOCAL is `local`, which ip_route_table_all prints on thirteen lines",
			got:         routeTableName(unix.RT_TABLE_LOCAL),
			want:        "local",
		},
		{
			// rt_tables names 0 "unspec", but the built-in hash does not hold
			// it. print_route never asks — `if (table && ...)` skips the token
			// for 0 — so the difference is unobservable through Text().
			description: "boundary: RT_TABLE_UNSPEC has no built-in name and falls back to 0",
			got:         routeTableName(unix.RT_TABLE_UNSPEC),
			want:        "0",
		},
		{
			description: "corner: the largest table id, RT_TABLE_MAX, renders as its number",
			got:         routeTableName(0xFFFFFFFF),
			want:        "4294967295",
		},
		{
			description: "corner: dsfieldName is always the 0x%02x fallback, since no rt_dsfield file is read",
			got:         dsfieldName(0x0a),
			want:        "0x0a",
		},
		{
			description: "boundary: dsfieldName zero-pads to two digits",
			got:         dsfieldName(0x02),
			want:        "0x02",
		},
		{
			description: "positive: viaFamilyName resolves AF_INET6 to inet6, the family in ip_route_main:4",
			got:         viaFamilyName(unix.AF_INET6),
			want:        "inet6",
		},
		{
			description: "negative: viaFamilyName returns family_name's literal ??? rather than addr.go's empty string",
			got:         viaFamilyName(99),
			want:        "???",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("= %q, want %q", tc.got, tc.want)
			}
		})
	}
}

// TestAfBitLen pins af_bit_len, which decides whether a destination renders as
// a prefix or as a bare host address.
func TestAfBitLen(t *testing.T) {
	tests := []struct {
		description string
		in          uint8
		want        int
	}{
		{description: "positive: AF_INET is 32", in: unix.AF_INET, want: 32},
		{description: "positive: AF_INET6 is 128", in: unix.AF_INET6, want: 128},
		{description: "boundary: AF_MPLS is 20, the label width and not a byte multiple", in: unix.AF_MPLS, want: 20},
		{
			// A family af_bit_len does not know has length 0, which is not an
			// error: it makes dst_len != host_len for every non-zero dst_len,
			// so the prefix form is always taken.
			description: "negative: an unknown family is 0, so every prefix of it renders with a length",
			in:          99, want: 0,
		},
		{description: "boundary: AF_UNSPEC is 0", in: unix.AF_UNSPEC, want: 0},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := afBitLen(tc.in); got != tc.want {
				t.Errorf("afBitLen(%d) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

// TestGetRealFamily pins get_real_family, which exists because a multicast
// route out of the multicast FIB carries a pseudo-family that cannot format an
// address.
func TestGetRealFamily(t *testing.T) {
	tests := []struct {
		description             string
		rtmType, rtmFamily      uint8
		want                    uint8
		explanationIsObservable bool
	}{
		{
			description: "positive: a unicast route keeps its own family untouched",
			rtmType:     unix.RTN_UNICAST, rtmFamily: unix.AF_INET, want: unix.AF_INET,
		},
		{
			// The guard is on rtm_type, so a NON-multicast route carrying the
			// pseudo-family is left alone — which is the C's behavior and not
			// an oversight worth "fixing".
			description: "negative: a non-multicast route carrying RTNL_FAMILY_IPMR is NOT remapped",
			rtmType:     unix.RTN_UNICAST, rtmFamily: 128, want: 128,
		},
		{
			description: "positive: a multicast route from RTNL_FAMILY_IPMR formats as AF_INET",
			rtmType:     unix.RTN_MULTICAST, rtmFamily: 128, want: unix.AF_INET,
		},
		{
			description: "positive: a multicast route from RTNL_FAMILY_IP6MR formats as AF_INET6",
			rtmType:     unix.RTN_MULTICAST, rtmFamily: 129, want: unix.AF_INET6,
		},
		{
			// ip_route_table_all:24 is exactly this: a multicast route dumped
			// from the ordinary v6 FIB, whose family is already AF_INET6.
			description: "boundary: ip_route_table_all:24 — a multicast route already in AF_INET6 passes through",
			rtmType:     unix.RTN_MULTICAST, rtmFamily: unix.AF_INET6, want: unix.AF_INET6,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := getRealFamily(tc.rtmType, tc.rtmFamily); got != tc.want {
				t.Errorf("getRealFamily(%d, %d) = %d, want %d",
					tc.rtmType, tc.rtmFamily, got, tc.want)
			}
		})
	}
}

// TestRouteViewOfTextDetails is `ip -d route show`, which does exactly one
// thing to a route line: it UNSUPPRESSES four tokens whose value is the
// default.
//
// The rows are paired against TestRouteViewOfText above — same RouteInfo,
// different filter — because the point is the delta and not the line. Every
// want here is transcribed from ip_route_main_n or ip_route_table_all_n, and
// the plain form of the same route is quoted beside it.
//
// go test ./internal/goip/render/ -run TestRouteViewOfTextDetails
func TestRouteViewOfTextDetails(t *testing.T) {
	tests := []struct {
		description string
		in          xtcpnl.RouteInfo
		filter      RouteShowFilter
		want        string
	}{
		{
			// ip_route_main:1 "192.0.2.0/24 dev goip0 proto kernel scope link src …"
			// ip_route_main_n:1 adds only `unicast`, because this route's
			// proto and scope are already non-default and were already
			// printed. The narrowest of the four deltas, and the one that
			// says the guards are independent.
			description: "positive: a route already showing proto and scope gains only the type token",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_LINK, Type: unix.RTN_UNICAST,
				Protocol: unix.RTPROT_KERNEL,
				Dst:      v4(192, 0, 2, 0), PrefSrc: v4(192, 0, 2, 1), Oif: 3,
			},
			filter: filterMainDetails,
			want:   "unicast 192.0.2.0/24 dev goip0 proto kernel scope link src 192.0.2.1 \n",
		},
		{
			// ip_route_main:2   "198.18.0.0/24 via 192.0.2.10 dev goip0 "
			// ip_route_main_n:2 "unicast 198.18.0.0/24 via 192.0.2.10 dev goip0 proto boot scope global "
			//
			// Three of the four at once, and the widest delta in the corpus.
			description: "positive: a default-everything route gains type, proto and scope",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_UNICAST,
				Protocol: unix.RTPROT_BOOT,
				Dst:      v4(198, 18, 0, 0), Gateway: v4(192, 0, 2, 10), Oif: 3,
			},
			filter: filterMainDetails,
			want:   "unicast 198.18.0.0/24 via 192.0.2.10 dev goip0 proto boot scope global \n",
		},
		{
			// ip_route_table_all_n:1 — the fourth token, and the one with an
			// extra conjunct. `table` needs `filter.tb == 0` as well
			// (ip/iproute.c:903), so it appears under `table all` and not
			// under a bare `route show`; the row below is the negative half.
			description: "positive: under table all, -d adds the table token the main-table default was hiding",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_LINK, Type: unix.RTN_UNICAST,
				Protocol: unix.RTPROT_KERNEL,
				Dst:      v4(192, 0, 2, 0), PrefSrc: v4(192, 0, 2, 1), Oif: 3,
			},
			filter: filterAllDetails,
			want:   "unicast 192.0.2.0/24 dev goip0 table main proto kernel scope link src 192.0.2.1 \n",
		},
		{
			// ip_route6:1   "2001:db8::/64 dev goip0 proto kernel metric 256 pref medium"
			// ip_route6_n:1 "unicast 2001:db8::/64 dev goip0 proto kernel scope global metric 256 pref medium"
			//
			// The v6 arm, and the token it adds is `scope global` — the NAME
			// RT_SCOPE_UNIVERSE renders under. That is why every IPv6 line in
			// ip_route6_n carries it: the kernel sets rtm_scope to UNIVERSE on
			// v6 routes as a matter of course, so the token is suppressed on
			// all of them without -d and appears on all of them with it.
			//
			// It also lands BEFORE `metric`, which is print_route's order
			// (:916 then :925) and not a place a v4 row could have checked —
			// no IPv4 route in the corpus carries both a restored scope and an
			// RTA_PRIORITY.
			description: "positive: an IPv6 route gains scope global, and it precedes the metric",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET6, DstLen: 64, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_UNICAST,
				Protocol: unix.RTPROT_KERNEL,
				Dst:      v6(t, "2001:db8::"), Oif: 3,
				Priority: 256, HasPriority: true,
				Pref: icmpv6RouterPrefMedium, HasPref: true,
			},
			filter: filterMainDetails,
			want:   "unicast 2001:db8::/64 dev goip0 proto kernel scope global metric 256 pref medium\n",
		},
		{
			// Table 0 is the other conjunct, and it is outside the
			// parenthesis too: `if (table && …)`. A route with no RTA_TABLE
			// and rtm_table 0 prints no table token under -d either.
			description: "boundary: a route in table 0 prints no table token under -d",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: 0,
				Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_UNICAST,
				Protocol: unix.RTPROT_BOOT,
				Dst:      v4(203, 0, 113, 0),
			},
			filter: filterAllDetails,
			want:   "unicast 203.0.113.0/24 proto boot scope global \n",
		},
		{
			// ip_route_table_all_n:9 "local 127.0.0.0/8 dev lo table local proto
			// kernel scope host src 127.0.0.1" — byte-identical to its plain
			// form at ip_route_table_all:9. A non-unicast route in a named
			// table with a non-default proto and scope has all four tokens
			// already, so -d changes nothing. The only rows in the corpus
			// where the two goldens agree line for line.
			description: "negative: a local-table route already prints all four tokens, so -d changes nothing",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 8, Table: unix.RT_TABLE_LOCAL,
				Scope: unix.RT_SCOPE_HOST, Type: unix.RTN_LOCAL,
				Protocol: unix.RTPROT_KERNEL,
				Dst:      v4(127, 0, 0, 0), PrefSrc: v4(127, 0, 0, 1), Oif: 1,
			},
			filter: filterAllDetails,
			want:   "local 127.0.0.0/8 dev lo table local proto kernel scope host src 127.0.0.1 \n",
		},
		{
			// `dev` is the token -d does NOT bring back. Its guard is
			// `filter.oifmask != -1` with no show_details arm at all
			// (ip/iproute.c:900), which is consistent: the other four hide a
			// DEFAULT, while this one hides something the user typed. Nothing
			// in the corpus states this — ip_route_dev is a plain capture —
			// so the row exists because only the source says it.
			description: "negative: -d does not restore the dev token a dev selector suppressed",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_UNICAST,
				Protocol: unix.RTPROT_BOOT,
				Dst:      v4(198, 18, 0, 0), Gateway: v4(192, 0, 2, 10), Oif: 3,
			},
			filter: RouteShowFilter{Table: unix.RT_TABLE_MAIN, OifMask: true, Details: true},
			want:   "unicast 198.18.0.0/24 via 192.0.2.10 proto boot scope global \n",
		},
		{
			// The cloned guard is not weakened either: proto and scope live
			// inside `if (!(rtm_flags & RTM_F_CLONED))` (:905-919), which -d
			// never reaches. The type token is outside it and still appears,
			// so this row is also what shows the two guards are nested rather
			// than parallel.
			description: "corner: a cloned route gains the type token under -d but still no proto or scope",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_UNIVERSE, Type: unix.RTN_UNICAST,
				Protocol: unix.RTPROT_BOOT, Flags: unix.RTM_F_CLONED,
				Dst: v4(198, 18, 0, 0), Oif: 3,
			},
			filter: filterMainDetails,
			want:   "unicast 198.18.0.0/24 dev goip0 \n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := RouteViewOf(tc.in, routeTabNames, tc.filter).Text()
			if got != tc.want {
				t.Errorf("Text() =\n  %q\nwant\n  %q", got, tc.want)
			}
		})
	}
}

// TestRouteViewJSON pins the JSON shape against `ip -j route`, which is a
// separate contract from Text(): a consumer parsing `ip -j` output must not
// have to special-case goip.
//
// The first row is an exact whole-document match, not a key-presence check,
// because the KEY ORDER is part of the contract too — encoding/json follows
// struct declaration order, so a field moved in RouteView silently reorders
// the JSON as well as the text.
func TestRouteViewJSON(t *testing.T) {
	tests := []struct {
		description string
		in          xtcpnl.RouteInfo
		filter      RouteShowFilter
		wantExact   string   // when non-empty, the whole marshaled document
		wantSubstrs []string // otherwise, fragments that must appear
		absentKeys  []string
	}{
		{
			// ip_route_main_json's first entry, verbatim. Reordering any field
			// of RouteView fails this row.
			description: "positive: ip_route_main_json:1 — the full key order of a connected route",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Scope: unix.RT_SCOPE_LINK, Type: unix.RTN_UNICAST,
				Protocol: unix.RTPROT_KERNEL,
				Dst:      v4(192, 0, 2, 0), PrefSrc: v4(192, 0, 2, 1), Oif: 3,
			},
			filter: filterMain,
			wantExact: `{"dst":"192.0.2.0/24","dev":"goip0","protocol":"kernel",` +
				`"scope":"link","prefsrc":"192.0.2.1","flags":[]}`,
		},
		{
			description: "positive: ip_route_main_json:3 — metrics is an ARRAY holding one object, in RTAX_* order",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v4(198, 18, 1, 0), Gateway: v4(192, 0, 2, 10), Oif: 3,
				Metrics: mx([2]uint32{unix.RTAX_MTU, 1400}, [2]uint32{unix.RTAX_ADVMSS, 1300}),
			},
			filter: filterMain,
			wantExact: `{"dst":"198.18.1.0/24","gateway":"192.0.2.10","dev":"goip0",` +
				`"flags":[],"metrics":[{"mtu":1400,"advmss":1300}]}`,
		},
		{
			description: "positive: ip_route_main_json:4 — RTA_VIA is a nested object, the only one print_route opens",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v4(198, 18, 2, 0), Oif: 3, HasVia: true,
				Via: &xtcpnl.RtVia{Family: unix.AF_INET6, Addr: v6(t, "2001:db8::2")},
			},
			filter: filterMain,
			wantExact: `{"dst":"198.18.2.0/24","via":{"family":"inet6","host":"2001:db8::2"},` +
				`"dev":"goip0","flags":[]}`,
		},
		{
			description: "positive: ip_route_main_json:6 — each nexthop carries its own always-present flags array",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v4(203, 0, 113, 0), HasMultipath: true,
				Multipath: []xtcpnl.RouteNextHop{
					{Hops: 0, Ifindex: 3, Gateway: v4(192, 0, 2, 10)},
					{Hops: 2, Ifindex: 3, Gateway: v4(192, 0, 2, 11)},
				},
			},
			filter: filterMain,
			wantExact: `{"dst":"203.0.113.0/24","flags":[],"nexthops":[` +
				`{"gateway":"192.0.2.10","dev":"goip0","weight":1,"flags":[]},` +
				`{"gateway":"192.0.2.11","dev":"goip0","weight":3,"flags":[]}]}`,
		},
		{
			// `flags` carries no omitempty and RtFlagTokens never returns nil,
			// so the key is present and its value is `[]` even on a route with
			// no flags at all. `"flags":null` would break a consumer iterating
			// it, and it is not what `ip -j route` emits.
			description: "boundary: flags is always an array and never null, even with no bits set",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, Type: unix.RTN_UNICAST,
				Protocol: unix.RTPROT_BOOT, Table: unix.RT_TABLE_MAIN,
			},
			filter:      filterMain,
			wantSubstrs: []string{`"flags":[]`},
			absentKeys:  []string{`"flags":null`},
		},
		{
			// `dst` carries no omitempty either, so a default route emits
			// `"dst":"default"` rather than dropping the key.
			description: "boundary: a default route still emits a dst key, with the value `default`",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, Type: unix.RTN_UNICAST,
				Protocol: unix.RTPROT_BOOT, Table: unix.RT_TABLE_MAIN,
				Gateway: v4(192, 0, 2, 10), Oif: 3,
			},
			filter:      filterMain,
			wantSubstrs: []string{`"dst":"default"`},
		},
		{
			// The two-keys-for-one-token case: the RTA_SRC branch uses "from"
			// and the rtm_src_len-only branch uses "src" (ip/iproute.c:872,876)
			// even though both print `from %s `. Reproducing that is the point
			// of a parity renderer.
			description: "corner: a src_len with no RTA_SRC uses the JSON key `src`, not `from`",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, SrcLen: 24,
				Table: unix.RT_TABLE_MAIN, Type: unix.RTN_UNICAST,
				Protocol: unix.RTPROT_BOOT, Dst: v4(198, 18, 0, 0), Oif: 3,
			},
			filter:      filterMain,
			wantSubstrs: []string{`"src":"0/24"`},
			absentKeys:  []string{`"from"`},
		},
		{
			description: "corner: an RTA_SRC uses the JSON key `from`, not `src`",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, SrcLen: 24,
				Table: unix.RT_TABLE_MAIN, Type: unix.RTN_UNICAST,
				Protocol: unix.RTPROT_BOOT,
				Dst:      v4(198, 18, 0, 0), Src: v4(10, 0, 0, 0), Oif: 3,
			},
			filter:      filterMain,
			wantSubstrs: []string{`"from":"10.0.0.0/24"`},
			absentKeys:  []string{`"src"`},
		},
		{
			// `metric` is a pointer precisely so that a present-but-zero
			// RTA_PRIORITY emits the key. omitempty on a uint32 would drop it.
			description: "boundary: RTA_PRIORITY present with value 0 emits `\"metric\":0`",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET6, DstLen: 128, Table: unix.RT_TABLE_LOCAL,
				Type: unix.RTN_LOCAL, Protocol: unix.RTPROT_KERNEL,
				Dst: v6(t, "::1"), Oif: 1, HasPriority: true,
				Pref: icmpv6RouterPrefMedium, HasPref: true,
			},
			filter:      filterAll,
			wantSubstrs: []string{`"metric":0`, `"pref":"medium"`, `"table":"local"`, `"type":"local"`},
		},
		{
			// The omitempty side: a plain main-table route must not carry empty
			// gateway/table/type/metric keys, because `ip -j route` does not
			// emit them and a consumer testing for presence would be misled.
			description: "negative: absent attributes drop their keys entirely rather than emitting empty values",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v4(198, 51, 100, 0), Oif: 3,
			},
			filter: filterMain,
			absentKeys: []string{
				`"gateway"`, `"via"`, `"table"`, `"type"`, `"metric"`,
				`"metrics"`, `"nexthops"`, `"prefsrc"`, `"scope"`,
				`"protocol"`, `"iif"`, `"pref"`, `"nhid"`, `"tos"`,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			b, err := json.Marshal(RouteViewOf(tc.in, routeTabNames, tc.filter))
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			got := string(b)
			if tc.wantExact != "" && got != tc.wantExact {
				t.Errorf("JSON = %s\nwant  %s", got, tc.wantExact)
			}
			for _, s := range tc.wantSubstrs {
				if !strings.Contains(got, s) {
					t.Errorf("JSON %s missing %s", got, s)
				}
			}
			for _, k := range tc.absentKeys {
				if strings.Contains(got, k) {
					t.Errorf("JSON %s unexpectedly contains %s", got, k)
				}
			}
		})
	}
}

// filterMainStats and filterAllStats are `ip -s route show` and
// `ip -s route show table all`.
var filterMainStats = RouteShowFilter{Table: unix.RT_TABLE_MAIN, Stats: true}
var filterAllStats = RouteShowFilter{Table: 0, Stats: true}

// ci builds the RtaCacheinfo a decoded RTA_CACHEINFO would have produced,
// in the kernel's member order.
func ci(clntref, lastuse uint32, expires int32, errv, used, id, ts, tsage uint32) *xtcpnl.RtaCacheinfo {
	return &xtcpnl.RtaCacheinfo{
		Clntref: clntref, Lastuse: lastuse, Expires: expires, Error: errv,
		Used: used, ID: id, Ts: ts, Tsage: tsage,
	}
}

// TestRouteViewOfCacheinfoText drives print_rta_cacheinfo's seven tokens
// (ip/iproute.c:500-532) and the family guard that decides whether it runs at
// all (:970-979).
//
// # Why most of these inputs are constructed
//
// Three of the eight members are written by the kernel only behind `if (dst)`
// (net/core/rtnetlink.c:1036-1041) and a route DUMP passes dst == NULL, so no
// capture of any `route show` can carry a non-zero rta_clntref, rta_lastuse
// or rta_used. rta_error, rta_id, rta_ts and rta_tsage are never non-zero on
// a dump either — the first is `dst ? dst->error : 0`, the second is the
// literal 0 at both call sites, and the last two are never assigned at all.
// Every committed fixture confirms it: 48 attributes, all 32 zero bytes.
//
// rta_expires is the exception and the one row below with a measured
// provenance. rt6_fill_node reads `dst ? dst->expires : rt->expires`
// (net/ipv6/route.c:5931), so an IPv6 route with a finite lifetime carries it
// on the dump path. Reproduced outside the test with
// `ip -6 route add fd99:beef::/64 dev dummy0 expires 600` in a throwaway
// namespace, where `ip` printed `expires 599sec` and goip, before this change,
// printed nothing.
//
// # The point of the -s rows
//
// `-s` gates exactly the three members a dump cannot populate, so `-s route
// show` is a no-op on every command goip implements. The rows below prove the
// gate works in BOTH directions anyway — withheld without the flag, emitted
// with it — because the suppression that matters in production comes from the
// data being zero, and a renderer that hardcoded the tokens away would pass
// every capture-derived test while being wrong the day `route get` lands.
//
// go test ./internal/goip/render/ -run TestRouteViewOfCacheinfoText
func TestRouteViewOfCacheinfoText(t *testing.T) {
	// base is a plain IPv6 connected route; every row below differs from it
	// only in its cacheinfo, so the diff in `want` is the cacheinfo block.
	base := func(c *xtcpnl.RtaCacheinfo) xtcpnl.RouteInfo {
		return xtcpnl.RouteInfo{
			Family: unix.AF_INET6, DstLen: 64, Table: unix.RT_TABLE_MAIN,
			Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
			Dst: v6(t, "fd99::"), Oif: 3, CacheInfo: c,
		}
	}

	tests := []struct {
		description string
		in          xtcpnl.RouteInfo
		filter      RouteShowFilter
		want        string
	}{
		{
			description: "negative: no RTA_CACHEINFO at all renders no tokens, with -s — absent must not become `users 0 used 0 age 0sec`",
			in:          base(nil),
			filter:      filterMainStats,
			want:        "fd99::/64 dev goip0 \n",
		},
		{
			description: "negative: an all-zero RTA_CACHEINFO renders no tokens either — this is what every one of the 48 committed attributes actually is",
			in:          base(ci(0, 0, 0, 0, 0, 0, 0, 0)),
			filter:      filterMainStats,
			want:        "fd99::/64 dev goip0 \n",
		},
		{
			description: "positive: rta_expires renders `expires Nsec` WITHOUT -s — the token is outside the show_stats guard, which is the divergence this change fixes",
			in:          base(ci(0, 0, 59900, 0, 0, 0, 0, 0)),
			filter:      filterMain,
			want:        "fd99::/64 dev goip0 expires 599sec \n",
		},
		{
			description: "corner: the same route under -s is byte-identical — -s adds nothing to a dump, which is why `-s route show` is a no-op",
			in:          base(ci(0, 0, 59900, 0, 0, 0, 0, 0)),
			filter:      filterMainStats,
			want:        "fd99::/64 dev goip0 expires 599sec \n",
		},
		{
			description: "positive: rta_error renders `error N`, also ungated, and follows expires",
			in:          base(ci(0, 0, 100, 7, 0, 0, 0, 0)),
			filter:      filterMain,
			want:        "fd99::/64 dev goip0 expires 1sec error 7 \n",
		},
		{
			description: "positive: under -s the three gated tokens render as `users N used N age Nsec`, in that order (constructed; only `route get` reaches this)",
			in:          base(ci(2, 30000, 0, 0, 5, 0, 0, 0)),
			filter:      filterMainStats,
			want:        "fd99::/64 dev goip0 users 2 used 5 age 300sec \n",
		},
		{
			description: "negative: the same three members WITHOUT -s render nothing — the gate is show_stats, not attribute presence",
			in:          base(ci(2, 30000, 0, 0, 5, 0, 0, 0)),
			filter:      filterMain,
			want:        "fd99::/64 dev goip0 \n",
		},
		{
			description: "negative: rta_clntref == 0 with a non-zero rta_used suppresses `users` alone — the three gated tokens suppress independently, not as a block",
			in:          base(ci(0, 30000, 0, 0, 5, 0, 0, 0)),
			filter:      filterMainStats,
			want:        "fd99::/64 dev goip0 used 5 age 300sec \n",
		},
		{
			description: "negative: rta_used == 0 between two non-zero siblings drops only `used`",
			in:          base(ci(2, 30000, 0, 0, 0, 0, 0, 0)),
			filter:      filterMainStats,
			want:        "fd99::/64 dev goip0 users 2 age 300sec \n",
		},
		{
			description: "boundary: rta_lastuse 149 renders `age 1sec` — USER_HZ division truncates, it does not round to 2",
			in:          base(ci(0, 149, 0, 0, 0, 0, 0, 0)),
			filter:      filterMainStats,
			want:        "fd99::/64 dev goip0 age 1sec \n",
		},
		{
			description: "boundary: rta_lastuse 99 renders `age 0sec` — suppression keys on the RAW member being zero, so a sub-second age still prints",
			in:          base(ci(0, 99, 0, 0, 0, 0, 0, 0)),
			filter:      filterMainStats,
			want:        "fd99::/64 dev goip0 age 0sec \n",
		},
		{
			description: "boundary: rta_expires 99 renders `expires 0sec` for the same reason, ungated",
			in:          base(ci(0, 0, 99, 0, 0, 0, 0, 0)),
			filter:      filterMain,
			want:        "fd99::/64 dev goip0 expires 0sec \n",
		},
		{
			description: "corner: a negative rta_expires renders signed, as `expires -1sec` — %d, not %u, and the reason the member is int32",
			in:          base(ci(0, 0, -100, 0, 0, 0, 0, 0)),
			filter:      filterMain,
			want:        "fd99::/64 dev goip0 expires -1sec \n",
		},
		{
			description: "positive: rta_id renders `ipid 0x%04x`, zero-padded to four digits",
			in:          base(ci(0, 0, 0, 0, 0, 0x2a, 0, 0)),
			filter:      filterMain,
			want:        "fd99::/64 dev goip0 ipid 0x002a \n",
		},
		{
			description: "corner: `ts` carries NO trailing space in its format string, so ts and tsage run together as `ts 0x1tsage 2sec` — upstream's output, reproduced rather than tidied",
			in:          base(ci(0, 0, 0, 0, 0, 0, 1, 2)),
			filter:      filterMain,
			want:        "fd99::/64 dev goip0 ts 0x1tsage 2sec \n",
		},
		{
			description: "corner: rta_ts == 0 with a non-zero rta_tsage still prints BOTH — they share one guard, unlike every other token here",
			in:          base(ci(0, 0, 0, 0, 0, 0, 0, 2)),
			filter:      filterMain,
			want:        "fd99::/64 dev goip0 ts 0x0tsage 2sec \n",
		},
		{
			description: "corner: all seven tokens at once, in print_rta_cacheinfo's emission order under -s",
			in:          base(ci(2, 30000, 100, 7, 5, 0x2a, 1, 2)),
			filter:      filterMainStats,
			want:        "fd99::/64 dev goip0 expires 1sec error 7 users 2 used 5 age 300sec ipid 0x002a ts 0x1tsage 2sec \n",
		},
		{
			description: "positive: the same cacheinfo on an IPv4 route renders identically — print_route calls print_rta_cacheinfo from both family arms",
			in: xtcpnl.RouteInfo{
				Family: unix.AF_INET, DstLen: 24, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v4(198, 18, 0, 0), Oif: 3,
				CacheInfo: ci(0, 0, 100, 0, 0, 0, 0, 0),
			},
			filter: filterMain,
			want:   "198.18.0.0/24 dev goip0 expires 1sec \n",
		},
		{
			description: "negative: RTNL_FAMILY_IPMR (128) drops the whole block — print_route's guard tests the RAW rtm_family, which getRealFamily would have folded onto AF_INET",
			in: xtcpnl.RouteInfo{
				Family: 128, DstLen: 32, Table: unix.RT_TABLE_MAIN,
				Type: unix.RTN_MULTICAST, Protocol: unix.RTPROT_BOOT,
				Dst: v4(224, 0, 0, 1), Oif: 3,
				CacheInfo: ci(0, 0, 100, 7, 0, 0x2a, 0, 0),
			},
			filter: filterAllStats,
			// The `/32` is not incidental. host_len comes from the RAW
			// rtm_family too — afBitLen(128) is 0 — so the prefix prints
			// where an AF_INET route of the same length shows a bare
			// address. That is the same raw-vs-real distinction the
			// cacheinfo guard turns on, visible twice on one line.
			// `table main` is absent because that guard is
			// `!= RT_TABLE_MAIN || Details`, not `filter.tb == 0` alone.
			want: "multicast 224.0.0.1/32 dev goip0 \n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got := RouteViewOf(tt.in, routeTabNames, tt.filter).Text()
			if got != tt.want {
				t.Errorf("got  %q\nwant %q", got, tt.want)
			}
		})
	}
}

// TestRouteViewCacheinfoJSON is the JSON half of
// TestRouteViewOfCacheinfoText: the same gating, plus the two places where
// the JSON form is not simply the text form's number.
//
// The key ORDER is a contract here, not a detail. encoding/json follows
// struct declaration order, so these rows fail if RouteView's cacheinfo
// fields are moved out of print_rta_cacheinfo's emission position between
// `flags` and `metrics`. The first row's expectation was taken from real
// `ip -6 -j route show` output on an expiring route.
//
// go test ./internal/goip/render/ -run TestRouteViewCacheinfoJSON
func TestRouteViewCacheinfoJSON(t *testing.T) {
	base := func(c *xtcpnl.RtaCacheinfo) xtcpnl.RouteInfo {
		return xtcpnl.RouteInfo{
			Family: unix.AF_INET6, DstLen: 64, Table: unix.RT_TABLE_MAIN,
			Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT,
			Dst: v6(t, "fd99::"), Oif: 3, Priority: 1024, HasPriority: true,
			CacheInfo: c,
		}
	}

	tests := []struct {
		description string
		in          xtcpnl.RouteInfo
		filter      RouteShowFilter
		wantExact   string
		absentKeys  []string
	}{
		{
			// Taken from `ip -6 -j route show` on a route added with
			// `expires 600`, which emitted
			// {"dst":"fd99:beef::/64","dev":"dummy0","metric":1024,
			//  "flags":[],"expires":599,"pref":"medium"}
			// — expires between flags and pref, and a NUMBER not a string.
			description: "positive: expires sits between `flags` and `metrics` in key order and marshals as a number, matching real `ip -6 -j route show`",
			in:          base(ci(0, 0, 59900, 0, 0, 0, 0, 0)),
			filter:      filterMain,
			wantExact:   `{"dst":"fd99::/64","dev":"goip0","metric":1024,"flags":[],"expires":599}`,
		},
		{
			description: "negative: an all-zero cacheinfo emits none of the seven keys — what every committed fixture carries",
			in:          base(ci(0, 0, 0, 0, 0, 0, 0, 0)),
			filter:      filterMainStats,
			wantExact:   `{"dst":"fd99::/64","dev":"goip0","metric":1024,"flags":[]}`,
			absentKeys:  []string{"expires", "error", "users", "used", "age", "ipid", "ts", "tsage"},
		},
		{
			description: "positive: under -s the three gated keys appear as users/used/age, after error and before ipid",
			in:          base(ci(2, 30000, 0, 0, 5, 0, 0, 0)),
			filter:      filterMainStats,
			wantExact:   `{"dst":"fd99::/64","dev":"goip0","metric":1024,"flags":[],"users":2,"used":5,"age":300}`,
		},
		{
			description: "negative: without -s the same three keys are absent from JSON as well as from text",
			in:          base(ci(2, 30000, 0, 0, 5, 0, 0, 0)),
			filter:      filterMain,
			wantExact:   `{"dst":"fd99::/64","dev":"goip0","metric":1024,"flags":[]}`,
			absentKeys:  []string{"users", "used", "age"},
		},
		{
			description: "corner: ipid marshals as a hex STRING, not a number — print_0xhex emits a string in JSON context",
			in:          base(ci(0, 0, 0, 0, 0, 0x2a, 0, 0)),
			filter:      filterMain,
			wantExact:   `{"dst":"fd99::/64","dev":"goip0","metric":1024,"flags":[],"ipid":"0x002a"}`,
		},
		{
			description: "corner: ts is a hex string while its guard-partner tsage is a number — one guard, two different JSON types",
			in:          base(ci(0, 0, 0, 0, 0, 0, 1, 2)),
			filter:      filterMain,
			wantExact:   `{"dst":"fd99::/64","dev":"goip0","metric":1024,"flags":[],"ts":"0x1","tsage":2}`,
		},
		{
			description: "corner: a negative expires marshals as a negative number rather than as 4294967295",
			in:          base(ci(0, 0, -100, 0, 0, 0, 0, 0)),
			filter:      filterMain,
			wantExact:   `{"dst":"fd99::/64","dev":"goip0","metric":1024,"flags":[],"expires":-1}`,
		},
		{
			description: "boundary: all seven keys at once pin the full emission order under -s",
			in:          base(ci(2, 30000, 100, 7, 5, 0x2a, 1, 2)),
			filter:      filterMainStats,
			wantExact: `{"dst":"fd99::/64","dev":"goip0","metric":1024,"flags":[],` +
				`"expires":1,"error":7,"users":2,"used":5,"age":300,"ipid":"0x002a","ts":"0x1","tsage":2}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			b, err := json.Marshal(RouteViewOf(tt.in, routeTabNames, tt.filter))
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if got := string(b); got != tt.wantExact {
				t.Errorf("got  %s\nwant %s", got, tt.wantExact)
			}
			for _, k := range tt.absentKeys {
				if strings.Contains(string(b), `"`+k+`":`) {
					t.Errorf("key %q present, want absent: %s", k, b)
				}
			}
		})
	}
}
