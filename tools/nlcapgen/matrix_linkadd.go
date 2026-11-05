package main

import "strings"

// The ip link add space. Each row runs in a fresh namespace holding one
// dummy device, d0, up, as the lower device a vlan or macvlan names, and
// adds one link, n0, of a kind. iproute2's side is ip/iplink.c
// iplink_modify and each kind's parse_opt (ip/iplink_vlan.c,
// ip/iplink_macvlan.c, ip/link_veth.c, ip/iplink_bridge.c); the kernel's
// is net/core/rtnetlink.c rtnl_newlink and each kind's validate and
// newlink.

const linkAddSetup = "ip link add d0 type dummy && ip link set d0 addrgenmode none && ip link set d0 up"

// linkKinds are the kinds and their options. Each value's facts name the
// kind and what its IFLA_INFO_DATA carries; link d0 must come before type,
// since iplink_parse stops at type.
func linkKinds() axis {
	return axis{Name: "kind", Values: []value{
		v("dummy", "type dummy", f("kind", "dummy")),
		v("ifb", "type ifb", f("kind", "ifb")),
		v("veth", "type veth peer name p0", f("kind", "veth", "peer", "p0")),
		cv("veth-mtu", "type veth peer name p0 mtu 1400", f("kind", "veth", "peer", "p0", "peer_mtu", "1400")),
		v("vlan", "link d0 type vlan id 10", f("kind", "vlan", "link", "d0", "vlan_id", "10")),
		bv("vlan-4094", "link d0 type vlan id 4094", f("kind", "vlan", "link", "d0", "vlan_id", "4094")),
		cv("vlan-ad", "link d0 type vlan protocol 802.1ad id 20", f("kind", "vlan", "link", "d0", "vlan_proto", "34984", "vlan_id", "20")),
		v("macvlan", "link d0 type macvlan mode bridge", f("kind", "macvlan", "link", "d0", "macvlan_mode", "4")),
		cv("macvlan-private", "link d0 type macvlan mode private", f("kind", "macvlan", "link", "d0", "macvlan_mode", "1")),
		cv("macvlan-passthru", "link d0 type macvlan mode passthru", f("kind", "macvlan", "link", "d0", "macvlan_mode", "8")),
		v("bridge", "type bridge", f("kind", "bridge")),
		cv("bridge-stp", "type bridge stp_state 1", f("kind", "bridge", "br_stp", "1")),
	}}
}

func linkAddSpace() *space {
	sp := &space{
		Name: "linkadd",
		Axes: []axis{
			{Name: "name", Values: []value{
				v("bare", "n0", f("name", "n0")),
				cv("dev", "dev n0", f("name", "n0")),
				cv("name", "name n0", f("name", "n0")),
			}},
			{Name: "up", Values: []value{
				v("absent", "", nil),
				v("up", "up", f("up", "1")),
			}},
			{Name: "mtu", Values: []value{
				v("absent", "", nil),
				v("1400", "mtu 1400", f("mtu", "1400")),
				bv("1500", "mtu 1500", f("mtu", "1500")),
				cv("9000", "mtu 9000", f("mtu", "9000")),
			}},
			{Name: "txqlen", Values: []value{
				v("absent", "", nil),
				v("100", "txqueuelen 100", f("txqlen", "100")),
				bv("0", "txqueuelen 0", f("txqlen", "0")),
			}},
			{Name: "address", Values: []value{
				v("absent", "", nil),
				v("mac", "address 02:00:00:00:01:01", f("address", "02:00:00:00:01:01")),
			}},
			{Name: "queues", Values: []value{
				v("absent", "", nil),
				v("tx", "numtxqueues 4", f("numtxqueues", "4")),
				v("rx", "numrxqueues 4", f("numrxqueues", "4")),
				cv("both", "numtxqueues 2 numrxqueues 3", f("numtxqueues", "2", "numrxqueues", "3")),
			}},
			linkKinds(),
		},
		Bad: []badValue{
			// A bare name is iplink_parse's dev, which check_altifname
			// allows up to ALTIFNAMSIZ; ifla_policy refuses IFLA_IFNAME
			// past IFNAMSIZ - 1. The name keyword's check_ifname would
			// refuse it in ip (W3c's linkset).
			{Axis: "name", Value: v("long", "abcdefghijklmnop", f("name", "abcdefghijklmnop")),
				Expect: expect{RC: intp(2), Errno: "ERANGE", RefusedBy: "kernel"}, Why: "a bare name of IFNAMSIZ characters"},
			{Axis: "kind", Value: v("vlan-4095", "link d0 type vlan id 4095", f("kind", "vlan", "link", "d0", "vlan_id", "4095")),
				Expect: expect{RC: intp(2), Errno: "ERANGE", RefusedBy: "kernel"}, Why: "VLAN id 4095, past VLAN_VID_MASK - 1 (Invalid VLAN id)"},
			// vlan_parse_opt runs after iplink_parse has looked d0 up.
			{Axis: "kind", Value: v("vlan-65536", "link d0 type vlan id 65536", nil), Expect: expIproute2InvargAfterDev,
				Why: "a VLAN id past u16 (id is invalid)"},
			{Axis: "kind", Value: v("vlan-nolink", "type vlan id 10", f("kind", "vlan", "vlan_id", "10")), Expect: *expKernelEINVAL,
				Why: "a vlan with no lower device"},
			{Axis: "kind", Value: v("nosuch", "type nosuch", f("kind", "nosuch")),
				Expect: expect{RC: intp(2), Errno: "ENOTSUP", RefusedBy: "kernel"},
				Why:    "a kind no module provides (Unknown device type; EOPNOTSUPP, which Linux numbers as ENOTSUP)"},
			{Axis: "mtu", Value: v("word", "mtu big", nil), Expect: expIproute2Invarg, Why: "an mtu that isn't a number"},
		},
	}
	sp.Build = buildLinkAdd
	sp.Judge = linkAddJudge
	sp.NegBase = func(vals []value) bool {
		return vals[0].Name == "bare" && vals[2].Name == "absent" && vals[6].Name == "dummy"
	}
	sp.Derive = deriveLinkAddDup
	return sp
}

// linkAddJudge: a vlan's or macvlan's MTU may not pass its lower device's
// (1500 on d0); the other kinds have no lower device.
func linkAddJudge(pick map[string]string) (*expect, string) {
	stacked := strings.HasPrefix(pick["kind"], "vlan") || strings.HasPrefix(pick["kind"], "macvlan")
	if stacked && pick["mtu"] == "9000" {
		return expKernelEINVAL, "an MTU past the lower device's 1500"
	}
	return nil, ""
}

// deriveLinkAddDup adds up to three repeated adds of a positive row's link
// (EEXIST, since add sets NLM_F_EXCL).
func deriveLinkAddDup(pos []row) []row {
	var out []row
	for i := range pos {
		p := &pos[i]
		if len(out) == 3 || p.Expect.RC == nil || *p.Expect.RC != 0 {
			continue
		}
		r := *p
		r.Pre = append(append([]preStep{}, p.Pre...), preStep{Cmd: p.Cmd})
		r.ID = "linkadd/dup-" + string(rune('1'+len(out)))
		r.Desc = "negative: " + dropPrefix(p.Desc)[1:] + ": added twice (EEXIST)"
		r.Expect = expect{RC: intp(2), Errno: "EEXIST", RefusedBy: "kernel"}
		out = append(out, r)
	}
	return out
}

// buildLinkAdd assembles `ip link add NAME ... KIND`; the kind and its
// options come last, since iplink_parse hands everything after type to the
// kind. The readback shows the new link alone.
func buildLinkAdd(vals []value) row {
	args := make([]string, 0, len(vals))
	for _, val := range vals {
		args = append(args, val.Arg)
	}
	return row{
		Cmd:      "ip link add " + joinArgs(args...),
		Pre:      []preStep{{Cmd: linkAddSetup}},
		Readback: []readback{{Name: "dump", Cmd: "ip -d link show dev n0"}},
		Expect:   expect{RC: intp(0), RefusedBy: "none"},
		Axes:     mergeFacts(vals),
	}
}
