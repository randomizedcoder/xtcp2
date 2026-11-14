package main

// The kwlive spaces: one row for each keyword form goip-write-keywords.md's
// GW5 found no workbench row for with an ip-only setup
// (docs/netlink/goip-live-forms.md, "The forms"). Each space is a verb and
// a form, so pairwise gives one positive row per form; a form the kernel
// refuses is a bad value. Every setup step is ip, so goip-parity can take
// the rows' setups as they are.

// kwVrfSetup makes the two VRFs the route spaces' vrf forms name: one
// table below 256 (rtm_table) and one above (RTA_TABLE).
const kwVrfSetup = "ip link add vrf0 type vrf table 100 && ip link set vrf0 up" +
	" && ip link add vrf1 type vrf table 1111 && ip link set vrf1 up"

// kwLinkSetup puts two dummies in group 5 for the group-wide set.
const kwLinkSetup = "ip link add d0 type dummy && ip link set d0 group 5" +
	" && ip link add d1 type dummy && ip link set d1 group 5"

// kwNhModules are the MPLS tunnel's modules, loaded before the batch.
var kwNhModules = []string{"mpls_router", "mpls_iptunnel"}

var kwAdd = axis{Name: "verb", Values: []value{v("add", "add", f("verb", "add"))}}

// kwvrf is a form whose row needs kwVrfSetup.
func kwvrf(name, arg string, facts map[string]string) value {
	val := v(name, arg, facts)
	val.Pre = []string{kwVrfSetup}
	return val
}

func kwRoute4Space() *space {
	sp := &space{
		Name: "kwroute4",
		Axes: []axis{kwAdd, {Name: "form", Values: []value{
			v("tos", "198.51.100.0/24 dev d0 tos 0x10", f("tos", "16")),
			v("dsfield", "198.51.100.0/24 dev d0 dsfield 0x10", f("tos", "16")),
			v("realms-to", "198.51.100.0/24 dev d0 realms 5", f("flow", "5")),
			v("realms-from-to", "198.51.100.0/24 dev d0 realms 1/2", f("flow", "65538")),
			kwvrf("vrf-100", "198.51.100.0/24 dev d0 vrf vrf0", f("table", "100")),
			kwvrf("vrf-1111", "198.51.100.0/24 dev d0 vrf vrf1", f("table", "1111")),
			v("hop-onlink", "198.51.100.0/24 nexthop via 203.0.113.1 dev d0 onlink nexthop via 192.0.2.1 dev d0",
				f("hop1_onlink", "1")),
			v("hop-via6", "198.51.100.0/24 nexthop via inet6 2001:db8:1::1 dev d0 nexthop via 192.0.2.1 dev d0",
				f("hop1_via_family", "10")),
			v("hop-realms", "198.51.100.0/24 nexthop via 192.0.2.1 dev d0 realms 3 nexthop via 192.0.2.2 dev d0",
				f("hop1_flow", "3")),
		}}},
		Bad: []badValue{
			{Axis: "form", Value: v("tos-ecn", "198.51.100.0/24 dev d0 tos 0x01", f("tos", "1")), Expect: *expKernelEINVAL,
				Why: "a tos with an ECN bit (Invalid dsfield (tos): ECN bits must be 0)"},
		},
	}
	sp.Build = func(vals []value) row { return buildKw("ip -4 route ", routeSetup, "ip -4 route show table all", vals) }
	sp.Derive = func(pos []row) []row { return deriveDupGone(sp.Name, pos, "ESRCH", nil) }
	return sp
}

func kwRoute6Space() *space {
	sp := &space{
		Name: "kwroute6",
		Axes: []axis{kwAdd, {Name: "form", Values: []value{
			v("pref-1", "2001:db8:2::/64 dev d0 pref 1", f("pref", "1")),
			cv("pref-2", "2001:db8:2::/64 dev d0 pref 2", f("pref", "2")),
			bv("pref-255", "2001:db8:2::/64 dev d0 pref 255", f("pref", "255")),
			kwvrf("vrf-100", "2001:db8:2::/64 dev d0 vrf vrf0", f("table", "100")),
			kwvrf("vrf-1111", "2001:db8:2::/64 dev d0 vrf vrf1", f("table", "1111")),
		}}},
		Bad: []badValue{
			{Axis: "form", Value: v("dsfield", "2001:db8:2::/64 dev d0 dsfield 0x10", f("tos", "16")), Expect: *expKernelEINVAL,
				Why: "a dsfield on IPv6 (Invalid dsfield (tos): option not available for IPv6)"},
		},
	}
	sp.Build = func(vals []value) row { return buildKw("ip -6 route ", routeSetup, "ip -6 route show table all", vals) }
	sp.Derive = func(pos []row) []row { return deriveDupGone(sp.Name, pos, "ESRCH", nil) }
	return sp
}

func kwLinkSpace() *space {
	sp := &space{
		Name: "kwlink",
		Axes: []axis{{Name: "verb", Values: []value{v("none", "", nil)}}, {Name: "form", Values: []value{
			v("group", "set group 5 mtu 1400", f("verb", "set", "group", "5", "mtu", "1400")),
			cv("group-empty", "set group 6 mtu 1400", f("verb", "set", "group", "6", "mtu", "1400")),
			v("xdpoffload", "set d0 xdpoffload off", f("verb", "set", "xdp_fd", "-1", "xdp_flags", "8")),
			v("index", "add d7 index 77 type dummy", f("verb", "add", "ifindex", "77")),
		}}},
		Bad: []badValue{
			{Axis: "form", Value: v("index-used", "add d8 index 1 type dummy", f("verb", "add", "ifindex", "1")),
				Expect: expect{RC: intp(2), Errno: "EEXIST", RefusedBy: refKernel},
				Why:    "an index lo already has (rtnl_newlink finds it, and add sets NLM_F_EXCL)"},
		},
	}
	sp.Build = func(vals []value) row { return buildKw("ip link ", kwLinkSetup, "ip link show", vals) }
	return sp
}

func kwNhSpace() *space {
	sp := &space{
		Name: "kwnh",
		Axes: []axis{kwAdd, {Name: "form", Values: []value{
			v("mpls", "id 51 encap mpls 101 via 192.0.2.1 dev d0", f("id", "51", "labels", "101", "gw", "192.0.2.1")),
			v("mpls-two", "id 52 encap mpls 101/102 via 192.0.2.1 dev d0", f("id", "52", "labels", "101/102", "gw", "192.0.2.1")),
			v("mpls-ttl", "id 53 encap mpls 101 ttl 10 via 192.0.2.1 dev d0",
				f("id", "53", "labels", "101", "ttl", "10", "gw", "192.0.2.1")),
			v("mpls-v6", "id 54 encap mpls 101 via 2001:db8:1::1 dev d0", f("id", "54", "labels", "101", "gw", "2001:db8:1::1")),
		}}},
		Bad: []badValue{
			{Axis: "form", Value: v("hw-stats-off", "id 55 via 192.0.2.1 dev d0 hw_stats off", f("id", "55", "hw_stats", "0")),
				Expect: *expKernelEINVAL,
				Why:    "hw_stats on a single nexthop, even off (Cannot enable nexthop hardware statistics for non-group nexthops)"},
		},
	}
	sp.Build = func(vals []value) row {
		r := buildKw("ip nexthop ", routeSetup, "ip nexthop show", vals)
		r.Modules = kwNhModules
		return r
	}
	sp.Derive = func(pos []row) []row { return deriveDupGone(sp.Name, pos, "ENOENT", nhHasID) }
	return sp
}

func kwRuleSpace() *space {
	sp := &space{
		Name: "kwrule",
		Axes: []axis{kwAdd, {Name: "form", Values: []value{
			v("realms-from-to", "pref 200 realms 1/2 table 100", f("pref", "200", "flow", "65538", "table", "100")),
		}}},
	}
	sp.Build = func(vals []value) row { return buildKw("ip -4 rule ", "", "ip -4 rule show", vals) }
	return sp
}

// buildKw assembles `HEAD VERB FORM` after setup and each value's Pre,
// with one readback of the object.
func buildKw(head, setup, show string, vals []value) row {
	r := row{
		Cmd:      head + joinArgs(vals[0].Arg, vals[1].Arg),
		Readback: []readback{{Name: "dump", Cmd: show}},
		Expect:   expect{RC: intp(0), RefusedBy: refNone},
		Axes:     mergeFacts(vals),
	}
	if setup != "" {
		r.Pre = append(r.Pre, preStep{Cmd: setup})
	}
	for _, val := range vals {
		for _, p := range val.Pre {
			r.Pre = append(r.Pre, preStep{Cmd: p})
		}
	}
	return r
}
