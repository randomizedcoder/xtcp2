package main

import "strings"

// The ip route get spaces: the lookups that send the route attributes no
// write does (coverage-status.md, "W4b"). Each row runs in routeSetup's
// namespace plus a default route per family, so every destination
// resolves. iproute2's side is ip/iproute.c iproute_get, which adds
// RTA_IIF, RTA_OIF and RTA_MARK after its loop (mark only when not 0); the
// kernel's is net/ipv4/route.c inet_rtm_getroute and net/ipv6/route.c
// inet6_rtm_getroute.

const routeGetSetup = routeSetup + " && ip route add default via 192.0.2.1 dev d0" +
	" && ip -6 route add default via 2001:db8:1::1 dev d0"

func routeGetMarks() axis {
	return axis{Name: "mark", Values: []value{
		v("absent", "", nil),
		v("16", "mark 0x10", f("mark", "16")),
		// iproute_get sends RTA_MARK only when the mark is not 0.
		bv("0", "mark 0", nil),
		bv("max", "mark 4294967295", f("mark", "4294967295")),
	}}
}

func routeGetUIDs() axis {
	return axis{Name: "uid", Values: []value{
		v("absent", "", nil),
		v("1000", "uid 1000", f("uid", "1000")),
		bv("0", "uid 0", f("uid", "0")),
	}}
}

// routeGetProtos: rtm_getroute_parse_ip_proto (net/ipv4/netlink.c) takes
// TCP and UDP, and ICMP only in its own family; icmp is the family's.
func routeGetProtos(icmp value) axis {
	return axis{Name: "ipproto", Values: []value{
		v("absent", "", nil),
		v("tcp", "ipproto tcp", f("ipproto", "6")),
		v("udp", "ipproto udp", f("ipproto", "17")),
		icmp,
	}}
}

// expUnsupportedProto is rtm_getroute_parse_ip_proto's EOPNOTSUPP, which
// Linux numbers as ENOTSUP ("Unsupported ip proto").
var expUnsupportedProto = expect{RC: intp(2), Errno: "ENOTSUP", RefusedBy: refKernel}

// routeGetPorts: get_be16 reads them, so they are sent in network order.
func routeGetPorts() axis {
	return axis{Name: "ports", Values: []value{
		v("absent", "", nil),
		v("sport", "sport 1234", f("sport", "1234")),
		v("dport", "dport 80", f("dport", "80")),
		cv("both", "sport 1234 dport 443", f("sport", "1234", "dport", "443")),
		bv("dport-max", "dport 65535", f("dport", "65535")),
	}}
}

func routeGetDevs() axis {
	return axis{Name: "dev", Values: []value{
		v("absent", "", nil),
		v("oif", "oif d0", f("oif", "d0")),
		cv("dev", "dev d0", f("oif", "d0")),
	}}
}

// routeGetFlags: IPv6 takes RTM_F_FIB_MATCH alone
// (inet6_rtm_valid_getroute_req), so notify is IPv4's.
func routeGetFlags(extra ...value) axis {
	return axis{Name: "flags", Values: append([]value{
		v("absent", "", nil),
		v("fibmatch", "fibmatch", f("fibmatch", "1")),
	}, extra...)}
}

func routeGet4Space() *space {
	sp := &space{
		Name: "routeget4",
		Axes: []axis{
			{Name: "dst", Values: []value{
				v("connected", "192.0.2.1", f("dst", "192.0.2.1")),
				v("default", "198.51.100.7", f("dst", "198.51.100.7")),
				bv("32", "192.0.2.5/32", f("dst", "192.0.2.5")),
				cv("to", "to 198.51.100.9", f("dst", "198.51.100.9")),
			}},
			{Name: "from", Values: []value{
				v("absent", "", nil),
				v("local", "from 192.0.2.10", f("src", "192.0.2.10")),
			}},
			routeGetDevs(),
			routeGetMarks(),
			routeGetUIDs(),
			routeGetProtos(v("icmp", "ipproto icmp", f("ipproto", "1"))),
			routeGetPorts(),
			{Name: "tos", Values: []value{
				v("absent", "", nil),
				v("0x10", "tos 0x10", f("tos", "16")),
				cv("dsfield", "dsfield 0x08", f("tos", "8")),
			}},
			routeGetFlags(cv("notify", "notify", f("notify", "1"))),
		},
		Bad: []badValue{
			{Axis: "mark", Value: v("2^32", "mark 4294967296", nil), Expect: expIproute2Invarg, Why: "a mark past u32"},
			{Axis: "ports", Value: v("65536", "dport 65536", nil), Expect: expIproute2Invarg, Why: "a port past u16 (invalid dport)"},
			{Axis: "ipproto", Value: v("255", "ipproto 255", f("ipproto", "255")), Expect: expUnsupportedProto,
				Why: "a protocol other than TCP, UDP or ICMP (Unsupported ip proto)"},
			{Axis: "ipproto", Value: v("icmpv6", "ipproto ipv6-icmp", f("ipproto", "58")), Expect: expUnsupportedProto,
				Why: "ICMPv6 in an IPv4 lookup (Unsupported ip proto)"},
			{Axis: "ipproto", Value: v("nosuch", "ipproto nosuch", nil), Expect: expIproute2Invarg, Why: "an unknown protocol"},
			// ll_name_to_index's RTM_GETLINK is refused, and ip exits 1
			// (nodev) without sending the lookup.
			{Axis: "dev", Value: v("nosuch", "oif nosuch", nil), Expect: expect{RC: intp(1), Errno: "ENODEV", RefusedBy: refKernel},
				Why: "a device the kernel doesn't know: its RTM_GETLINK is ENODEV (Cannot find device)"},
		},
	}
	sp.Build = func(vals []value) row { return buildRouteGet("4", vals) }
	return sp
}

func routeGet6Space() *space {
	sp := &space{
		Name: "routeget6",
		Axes: []axis{
			{Name: "dst", Values: []value{
				v("connected", "2001:db8:1::1", f("dst", "2001:db8:1::1")),
				v("default", "2001:db8:9::7", f("dst", "2001:db8:9::7")),
				bv("128", "2001:db8:1::5/128", f("dst", "2001:db8:1::5")),
				cv("to", "to 2001:db8:9::9", f("dst", "2001:db8:9::9")),
			}},
			{Name: "from", Values: []value{
				v("absent", "", nil),
				v("local", "from 2001:db8:1::10", f("src", "2001:db8:1::10")),
			}},
			routeGetDevs(),
			routeGetMarks(),
			routeGetUIDs(),
			routeGetProtos(v("icmp", "ipproto ipv6-icmp", f("ipproto", "58"))),
			routeGetPorts(),
			{Name: "flowlabel", Values: []value{
				v("absent", "", nil),
				v("label", "flowlabel 0x12345", f("flowlabel", "74565")),
				bv("max", "flowlabel 0xfffff", f("flowlabel", "1048575")),
			}},
			routeGetFlags(),
		},
		Bad: []badValue{
			{Axis: "mark", Value: v("2^32", "mark 4294967296", nil), Expect: expIproute2Invarg, Why: "a mark past u32"},
			{Axis: "ipproto", Value: v("icmp4", "ipproto icmp", f("ipproto", "1")), Expect: expUnsupportedProto,
				Why: "ICMP in an IPv6 lookup (Unsupported ip proto)"},
			{Axis: "flags", Value: v("notify", "notify", f("notify", "1")), Expect: *expKernelEINVAL,
				Why: "RTM_F_NOTIFY, which IPv6 lookups refuse (Invalid flags for get route request)"},
			{Axis: "flowlabel", Value: v("2^32", "flowlabel 0x100000000", nil), Expect: expIproute2Invarg, Why: "a flow label past u32"},
			{Axis: "ipproto", Value: v("nosuch", "ipproto nosuch", nil), Expect: expIproute2Invarg, Why: "an unknown protocol"},
		},
	}
	sp.Build = func(vals []value) row { return buildRouteGet("6", vals) }
	return sp
}

// buildRouteGet assembles `ip -F route get DST ...`. The reply is ip's
// own capture, so there is no readback.
func buildRouteGet(fam string, vals []value) row {
	args := make([]string, 0, len(vals))
	for _, val := range vals {
		args = append(args, val.Arg)
	}
	r := row{
		Cmd:    strings.TrimSpace("ip -" + fam + " route get " + joinArgs(args...)),
		Pre:    []preStep{{Cmd: routeGetSetup}},
		Expect: expect{RC: intp(0), RefusedBy: refNone},
		Axes:   mergeFacts(vals),
	}
	r.Axes["family"] = fam
	return r
}
