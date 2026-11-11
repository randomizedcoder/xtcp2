package main

import (
	"strconv"
	"strings"
)

// The MPLS route space: `ip -f mpls route add|replace|del LABEL …` with
// the keywords only an AF_MPLS route sends, RTA_NEWDST (as), RTA_VIA (a
// via in another family) and RTA_TTL_PROPAGATE. iproute2's side is
// ip/iproute.c iproute_modify and parse_one_nh; the kernel's is
// net/mpls/af_mpls.c rtm_to_route_config, mpls_nh_build_from_cfg and
// mpls_route_del. Every row loads mpls_router and sets platform_labels in
// its own namespace, so labels 16 to 999 are routable.

// mplsModules: mpls_router for the family, mpls_iptunnel for the encap
// mpls contrast row.
var mplsModules = []string{"mpls_router", "mpls_iptunnel"}

// mplsSetup adds d0 with an IPv4 and an IPv6 address, so a via in either
// family resolves, and turns MPLS on in the namespace.
const mplsSetup = "ip link add d0 type dummy && ip link set d0 addrgenmode none && ip link set d0 up" +
	" && ip addr add 10.0.0.1/24 dev d0 && ip -6 addr add 2001:db8::1/64 dev d0 nodad" +
	" && echo 1000 > /proc/sys/net/mpls/platform_labels && echo 1 > /proc/sys/net/mpls/conf/d0/input"

// mplsVia4 is the plain next hop the derived rows use.
const mplsVia4 = "via inet 10.0.0.2 dev d0"

// mplsLabels returns n labels from 100 up, slash-separated, as the `as`
// keyword takes them.
func mplsLabels(n int) string {
	ls := make([]string, n)
	for i := range ls {
		ls[i] = strconv.Itoa(100 + i)
	}
	return strings.Join(ls, "/")
}

func mplsSpace() *space {
	sp := &space{
		Name: "mpls",
		Axes: []axis{
			// A del's setup adds the label first; mpls_route_del matches on
			// the label alone, so the rest of a del is sent and ignored.
			{Name: "verb", Values: []value{
				v("add", "add", f("verb", "add")),
				v("replace", "replace", f("verb", "replace")),
				v("del", "del", f("verb", "del")),
			}},
			// mpls_label_ok: 16 (MPLS_LABEL_FIRST_UNRESERVED) up to
			// platform_labels - 1. get_prefix takes LABEL/20 too, the only
			// length there is.
			{Name: "label", Values: []value{
				v("100", "100", f("dst", "100")),
				bv("16", "16", f("dst", "16")),
				bv("999", "999", f("dst", "999")),
				cv("prefix", "100/20", f("dst", "100")),
			}},
			// mpls_pton's labels, top first; ip sets the bottom-of-stack
			// bit on the last. MAX_NEW_LABELS is 30.
			{Name: "as", Values: []value{
				v("absent", "", nil),
				v("1", "as 200", f("as", "200")),
				v("2", "as 200/300", f("as", "200/300")),
				v("3", "as 200/300/400", f("as", "200/300/400")),
				cv("to", "as to 200", f("as", "200")),
				bv("0", "as 0", f("as", "0")),
				bv("30", "as "+mplsLabels(30), f("as", mplsLabels(30))),
			}},
			// ttl-propagate comes before the next hop: parse_nexthops reads
			// every word after the first nexthop as part of a hop.
			{Name: "ttl", Values: []value{
				v("absent", "", nil),
				v("enabled", "ttl-propagate enabled", f("ttl_propagate", "1")),
				v("disabled", "ttl-propagate disabled", f("ttl_propagate", "0")),
				cv("abbrev", "ttl-propagate en", f("ttl_propagate", "1")),
			}},
			// A via in the route's own family would be RTA_GATEWAY; an
			// MPLS route's is in another, so ip sends RTA_VIA. With
			// nexthops, a top-level as is sent too, and the kernel keeps
			// only each hop's own.
			{Name: "via", Values: []value{
				v("inet", mplsVia4, f("via", "inet:10.0.0.2", "oif", "d0")),
				v("inet6", "via inet6 2001:db8::2 dev d0", f("via", "inet6:2001:db8::2", "oif", "d0")),
				cv("ll", "via inet6 fe80::2 dev d0", f("via", "inet6:fe80::2", "oif", "d0")),
				cv("nodev", "via inet 10.0.0.2", f("via", "inet:10.0.0.2")),
				v("dev", "dev d0", f("oif", "d0")),
				cv("offlink", "via inet 192.0.2.2 dev d0", f("via", "inet:192.0.2.2", "oif", "d0")),
				v("multipath", "nexthop as 300 via inet 10.0.0.2 dev d0 nexthop as 400 via inet 10.0.0.3 dev d0",
					f("hops", "300@inet:10.0.0.2,400@inet:10.0.0.3")),
				cv("mixed", "nexthop via inet 10.0.0.2 dev d0 nexthop via inet6 2001:db8::2 dev d0",
					f("hops", "@inet:10.0.0.2,@inet6:2001:db8::2")),
			}},
		},
		Bad: []badValue{
			{Axis: "label", Value: bv("15", "15", f("dst", "15")), Expect: *expKernelEINVAL,
				Why: "a reserved label (Invalid label - must be MPLS_LABEL_FIRST_UNRESERVED or higher)"},
			{Axis: "label", Value: v("3", "3", f("dst", "3")), Expect: *expKernelEINVAL,
				Why: "implicit null as the label (Implicit NULL Label (3) can not be used in encapsulation)"},
			{Axis: "label", Value: bv("1000", "1000", f("dst", "1000")), Expect: *expKernelEINVAL,
				Why: "platform_labels itself (Label >= configured maximum in platform_labels)"},
			{Axis: "label", Value: bv("1048575", "1048575", f("dst", "1048575")), Expect: *expKernelEINVAL,
				Why: "the largest 20-bit label, past platform_labels"},
			{Axis: "label", Value: bv("1048576", "1048576", nil), Expect: expIproute2Rc1,
				Why: "a label past 20 bits (mpls prefix is expected)"},
			{Axis: "as", Value: v("3", "as 3", f("as", "3")), Expect: *expKernelEINVAL,
				Why: "implicit null in the stack (Implicit NULL Label (3) can not be used in encapsulation)"},
			{Axis: "as", Value: bv("31", "as "+mplsLabels(31), f("as", mplsLabels(31))), Expect: *expKernelEINVAL,
				Why: "31 labels, past MAX_NEW_LABELS (Too many labels)"},
			{Axis: "as", Value: v("word", "as implicit-null", nil), Expect: expIproute2Rc1,
				Why: "a label by name, which mpls_pton doesn't take (mpls address is expected)"},
			{Axis: "as", Value: bv("2pow20", "as 1048576", nil), Expect: expIproute2Rc1,
				Why: "a label past 20 bits in the stack"},
			{Axis: "as", Value: v("slash", "as 200/", nil), Expect: expIproute2Rc1,
				Why: "a stack ending in a slash"},
			{Axis: "as", Value: v("empty", "as", nil), Expect: expIproute2Rc1,
				Why: "as with no labels, which reads the next word as one"},
			{Axis: "via", Value: v("nofamily", "via 10.0.0.2 dev d0", nil), Expect: expIproute2Rc1,
				Why: "a via with no family, read as an MPLS address (mpls address is expected)"},
			{Axis: "via", Value: v("twice", "via inet 10.0.0.2 via inet 10.0.0.3 dev d0", nil), Expect: expIproute2Invarg,
				Why: "two vias (use nexthop syntax to specify multiple via)"},
			{Axis: "via", Value: v("nosuchdev", "via inet 10.0.0.2 dev nosuch", nil), Expect: expect{RC: intp(1), Errno: "ENODEV", RefusedBy: refKernel},
				Why: "a device that doesn't exist: ip's RTM_GETLINK for it is ENODEV (Cannot find device)"},
			{Axis: "via", Value: v("offlink-nodev", "via inet 192.0.2.2", f("via", "inet:192.0.2.2")),
				Expect: expect{RC: intp(2), Errno: "ENETUNREACH", RefusedBy: refKernel},
				Why:    "an off-link via with no device, which no route reaches"},
			{Axis: "via", Value: v("ll-nodev", "via inet6 fe80::2", f("via", "inet6:fe80::2")),
				Expect: expect{RC: intp(2), Errno: "ENETUNREACH", RefusedBy: refKernel},
				Why:    "a link-local via with no device"},
			{Axis: "via", Value: v("none", "", nil), Expect: expect{RC: intp(2), Errno: "ENODEV", RefusedBy: refKernel},
				Why: "no next hop at all"},
			{Axis: "ttl", Value: v("maybe", "ttl-propagate maybe", nil), Expect: expIproute2Invarg,
				Why: "a ttl-propagate that isn't enabled or disabled"},
			{Axis: "ttl", Value: v("bare", "ttl-propagate", nil), Expect: expIproute2Invarg,
				Why: "ttl-propagate with no value, which reads the next word as one"},
		},
	}
	sp.Build = buildMpls
	sp.NegBase = func(vals []value) bool { return vals[0].Name == "add" }
	sp.Derive = deriveMpls
	return sp
}

// expIproute2Rc1 is a get_addr or get_prefix refusal, which exits 1.
var expIproute2Rc1 = expect{RC: intp(1), RefusedBy: refIproute2}

// mplsRow is one MPLS row's shape: the setup with extra steps after it,
// the two show readbacks, and the modules.
func mplsRow(cmd string, extra ...string) row {
	pre := make([]preStep, 0, 1+len(extra))
	pre = append(pre, preStep{Cmd: mplsSetup})
	for _, e := range extra {
		pre = append(pre, preStep{Cmd: e})
	}
	return row{
		Cmd: cmd,
		Pre: pre,
		Readback: []readback{
			{Name: "show", Cmd: "ip -f mpls route show"},
			{Name: "showj", Cmd: "ip -j -f mpls route show"},
		},
		Modules: mplsModules,
		Expect:  expect{RC: intp(0), RefusedBy: refNone},
	}
}

func buildMpls(vals []value) row {
	args := make([]string, 0, len(vals)-1)
	for _, val := range vals[1:] {
		args = append(args, val.Arg)
	}
	var extra []string
	if vals[0].Name == "del" {
		extra = append(extra, "ip -f mpls route add "+vals[1].Facts["dst"]+" dev d0")
	}
	r := mplsRow("ip -f mpls route "+vals[0].Arg+" "+joinArgs(args...), extra...)
	r.Axes = mergeFacts(vals)
	return r
}

// deriveMpls adds the rows outside the space: the route table's own
// answers (a duplicate, a replace and a del of what's there and isn't, no
// platform labels, a get), as given twice, and as on IPv4 and IPv6
// routes, where ip parses it as an address of the route's family.
func deriveMpls([]row) []row {
	named := func(id, desc string, r row, exp expect, axes map[string]string) row {
		r.ID, r.Desc, r.Expect, r.Axes = "mpls/"+id, desc, exp, axes
		return r
	}
	ok := expect{RC: intp(0), RefusedBy: refNone}
	add100 := "ip -f mpls route add 100 " + mplsVia4
	noPlatform := mplsRow("ip -f mpls route add 100 " + mplsVia4)
	noPlatform.Pre = []preStep{{Cmd: linkSetup + " && ip link set d0 up && ip addr add 10.0.0.1/24 dev d0"}}
	return []row{
		named("dup", "negative: an add of a label that's there (File exists)", mplsRow(add100, add100),
			expect{RC: intp(2), Errno: "EEXIST", RefusedBy: refKernel}, f("verb", "add", "dst", "100", "via", "inet:10.0.0.2", "oif", "d0")),
		named("replace-there", "positive: a replace of a label that's there", mplsRow("ip -f mpls route replace 100 as 300 "+mplsVia4, add100),
			ok, f("verb", "replace", "dst", "100", "as", "300", "via", "inet:10.0.0.2", "oif", "d0")),
		named("del-absent", "corner: a del of a label that isn't there, which mpls_route_del doesn't check", mplsRow("ip -f mpls route del 100"),
			ok, f("verb", "del", "dst", "100")),
		named("no-platform", "negative: no platform_labels set (Label >= configured maximum in platform_labels)", noPlatform,
			*expKernelEINVAL, f("verb", "add", "dst", "100", "via", "inet:10.0.0.2", "oif", "d0")),
		named("as-twice", "corner: as twice, which ip sends twice and the kernel takes the last of", mplsRow("ip -f mpls route add 100 as 200 as 300 "+mplsVia4),
			ok, f("verb", "add", "dst", "100", "as", "200", "as2", "300", "via", "inet:10.0.0.2", "oif", "d0")),
		named("get", "positive: a get of a label", mplsRow("ip -f mpls route get 100", add100), ok, f("verb", "get", "dst", "100")),
		named("get-as", "positive: a get with as", mplsRow("ip -f mpls route get 100 as 200", add100), ok, f("verb", "get", "dst", "100", "as", "200")),
		named("get-absent", "negative: a get of a label that isn't there (Network is unreachable)", mplsRow("ip -f mpls route get 101", add100),
			expect{RC: intp(2), Errno: "ENETUNREACH", RefusedBy: refKernel}, f("verb", "get", "dst", "101")),
		named("show", "positive: a show of a stack, a mixed multipath and a ttl-propagate", mplsRow("ip -f mpls route show",
			"ip -f mpls route add 100 as 200/300 "+mplsVia4,
			"ip -f mpls route add 101 nexthop as 200 "+mplsVia4+" nexthop via inet6 2001:db8::2 dev d0",
			"ip -f mpls route add 102 dev d0 ttl-propagate disabled"), ok, nil),
		named("show-json", "positive: the same, as JSON, whose ttl-propagate key ip misspells", mplsRow("ip -j -f mpls route show",
			"ip -f mpls route add 100 as 200/300 "+mplsVia4,
			"ip -f mpls route add 101 nexthop as 200 "+mplsVia4+" nexthop via inet6 2001:db8::2 dev d0",
			"ip -f mpls route add 102 dev d0 ttl-propagate disabled"), ok, nil),
		named("ttl-after-hops", "negative: ttl-propagate after the nexthops, refused once the hops' devices are looked up (\"nexthop\" or end of line is expected)",
			mplsRow("ip -f mpls route add 100 nexthop as 300 "+mplsVia4+" nexthop as 400 via inet 10.0.0.3 dev d0 ttl-propagate enabled"),
			expect{RC: intp(255), RefusedBy: refAckThenFail}, nil),
		named("inet-as", "corner: as on an IPv4 route, sent as the IPv4 address 200.0.0.0, which the kernel ignores",
			mplsRow("ip route add 10.1.0.0/24 as 200 via 10.0.0.2"), ok, f("verb", "add", "newdst", "c8000000")),
		named("inet6-as", "negative: as on an IPv6 route (inet6 address is expected)",
			mplsRow("ip -6 route add 2001:db8:9::/64 as 200 via 2001:db8::2"), expIproute2Rc1, nil),
		named("inet-encap", "positive: an IPv4 route with encap mpls, for contrast",
			mplsRow("ip route add 10.1.0.0/24 encap mpls 200/300 via 10.0.0.2"), ok, f("verb", "add", "encap", "200/300")),
		named("inet-get-as", "negative: as on an IPv4 get (Unsupported attribute in route get request)",
			mplsRow("ip route get 10.0.0.2 as 200"), *expKernelEINVAL, f("verb", "get", "newdst", "c8000000")),
	}
}
