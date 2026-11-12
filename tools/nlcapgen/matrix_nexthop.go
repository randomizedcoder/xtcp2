package main

import (
	"slices"
	"strings"
)

// The ip nexthop spaces: single nexthops per family, and groups of the
// members nhMembers makes. The kernel rules the exclusions and bad values
// cite are in net/ipv4/nexthop.c: rtm_to_nh_config, nh_check_attr_group,
// nh_check_attr_group_rtnl, valid_group_nh, rtm_new_nexthop and
// nh_valid_get_del_req. iproute2's side is ip/ipnexthop.c ipnh_modify.

// nhMembers makes the nexthops a group row's members name. Their ids start
// at 21, so the id axis's 1 and 100 never name one.
const nhMembers = "ip nexthop add id 21 via 192.0.2.1 dev d0 && ip nexthop add id 22 via 192.0.2.2 dev d0" +
	" && ip nexthop add id 23 dev d0 && ip -6 nexthop add id 26 via 2001:db8:1::1 dev d0" +
	" && ip nexthop add id 29 blackhole && ip nexthop add id 31 via 192.0.2.1 fdb && ip nexthop add id 32 via 192.0.2.2 fdb"

var nhVerbs = axis{Name: "verb", Values: []value{
	v("add", "add", f("verb", "add")),
	v("replace", "replace", f("verb", "replace")),
	v("del", "del", f("verb", "del")),
}}

// nhIDs is the id axis; an id of 0 or none asks the kernel for one.
func nhIDs(def string) axis {
	return axis{Name: "id", Values: []value{
		v(def, "id "+def, f("id", def)),
		v("absent", "", nil),
		bv("1", "id 1", f("id", "1")),
		bv("max", "id 4294967295", f("id", "4294967295")),
		cv("0", "id 0", f("id", "0")),
	}}
}

func nhProtos() axis {
	return axis{Name: "protocol", Values: []value{
		v("absent", "", nil),
		v("static", "protocol static", f("protocol", "4")),
		bv("255", "protocol 255", f("protocol", "255")),
		cv("proto", "proto boot", f("protocol", "3")),
	}}
}

func nh4Space() *space {
	sp := &space{
		Name: "nh4",
		Axes: []axis{
			nhVerbs,
			nhIDs("10"),
			{Name: "kind", Values: []value{
				v("dev", "dev d0", f("oif", "d0")),
				v("via-dev", "via 192.0.2.1 dev d0", f("gw", "192.0.2.1", "oif", "d0")),
				v("onlink", "via 203.0.113.1 dev d0 onlink", f("gw", "203.0.113.1", "oif", "d0", "onlink", "1")),
				v("blackhole", "blackhole", f("blackhole", "1")),
				v("fdb", "via 192.0.2.1 fdb", f("gw", "192.0.2.1", "fdb", "1")),
			}},
			nhProtos(),
		},
		Bad: append(nhBad(),
			badValue{Axis: "kind", Value: v("via-only", "via 192.0.2.1", f("gw", "192.0.2.1")), Expect: *expKernelEINVAL,
				Why: "a gateway without a device (Device attribute required for non-blackhole and non-fdb nexthops)"},
			badValue{Axis: "kind", Value: v("fdb-dev", "via 192.0.2.1 dev d0 fdb", f("gw", "192.0.2.1", "oif", "d0", "fdb", "1")),
				Expect: *expKernelEINVAL, Why: "fdb with a device (Fdb attribute can not be used with encap, oif or blackhole)"},
			badValue{Axis: "kind", Value: v("bh-dev", "blackhole dev d0", f("blackhole", "1", "oif", "d0")),
				Expect: *expKernelEINVAL, Why: "blackhole with a device (Blackhole attribute can not be used with gateway, oif, encap or fdb)"},
			badValue{Axis: "kind", Value: v("v6-gw", "via inet6 2001:db8:1::1 dev d0", nil), Expect: expIproute2Invarg,
				Why: "an IPv6 gateway on an IPv4 nexthop (address family mismatch)"},
		),
	}
	sp.Build = func(vals []value) row { return buildNexthop("ip -4 nexthop ", "4", "", sp.Axes, vals) }
	sp.Judge = nhJudge
	sp.NegBase = nhNegBase
	sp.Derive = func(pos []row) []row { return deriveDupGone("nh4", pos, "ENOENT", nhHasID) }
	return sp
}

func nh6Space() *space {
	sp := &space{
		Name: "nh6",
		Axes: []axis{
			nhVerbs,
			nhIDs("10"),
			{Name: "kind", Values: []value{
				v("dev", "dev d0", f("oif", "d0")),
				v("via-dev", "via 2001:db8:1::1 dev d0", f("gw", "2001:db8:1::1", "oif", "d0")),
				v("onlink", "via 2001:db8:3::1 dev d0 onlink", f("gw", "2001:db8:3::1", "oif", "d0", "onlink", "1")),
				v("blackhole", "blackhole", f("blackhole", "1")),
				v("fdb", "via 2001:db8:1::1 fdb", f("gw", "2001:db8:1::1", "fdb", "1")),
				cv("lladdr", "via fe80::1 dev d0", f("gw", "fe80::1", "oif", "d0")),
			}},
			nhProtos(),
		},
		Bad: append(nhBad(),
			badValue{Axis: "kind", Value: v("via-only", "via 2001:db8:1::1", f("gw", "2001:db8:1::1")), Expect: *expKernelEINVAL,
				Why: "a gateway without a device (Device attribute required for non-blackhole and non-fdb nexthops)"},
			badValue{Axis: "kind", Value: v("v4-gw", "via inet 192.0.2.1 dev d0", nil), Expect: expIproute2Invarg,
				Why: "an IPv4 gateway on an IPv6 nexthop (address family mismatch)"},
		),
	}
	sp.Build = func(vals []value) row { return buildNexthop("ip -6 nexthop ", "6", "", sp.Axes, vals) }
	sp.Judge = nhJudge
	sp.NegBase = nhNegBase
	sp.Derive = func(pos []row) []row { return deriveDupGone("nh6", pos, "ENOENT", nhHasID) }
	return sp
}

// nhNegBase bases bad values on an add of kind dev, so a value after the
// kind (protocol) always follows ll_name_to_index's acknowledged lookup.
func nhNegBase(vals []value) bool { return vals[0].Name == "add" && vals[2].Name == "dev" }

// nhBad holds the bad values both single-nexthop families share.
func nhBad() []badValue {
	return []badValue{
		{Axis: "id", Value: v("2^32", "id 4294967296", nil), Expect: expIproute2Invarg, Why: "an id past u32"},
		{Axis: "kind", Value: v("nodev", "dev nosuch", nil), Expect: expect{RC: intp(255), Errno: "ENODEV", RefusedBy: refKernel},
			Why: "a device that does not exist: ipnh_modify's ll_name_to_index asks with an RTM_GETLINK the kernel refuses, then invarg()"},
		{Axis: "kind", Value: v("onlink-nogw", "dev d0 onlink", f("oif", "d0", "onlink", "1")), Expect: *expKernelEINVAL,
			Why: "onlink without a gateway (ONLINK flag can not be set for nexthop without a gateway)"},
		{Axis: "kind", Value: v("dst-port", "dev d0 dst_port 4789", nil), Expect: expIproute2InvargAfterDev,
			Why: "dst_port, which iproute2 7.2.0 does not parse (ad2b2d95 came after it)"},
		{Axis: "kind", Value: v("hw-stats", "dev d0 hw_stats on", f("oif", "d0", "hw_stats", "1")), Expect: *expKernelEINVAL,
			Why: "hw_stats on a single nexthop (Cannot enable nexthop hardware statistics for non-group nexthops)"},
		{Axis: "protocol", Value: v("256", "protocol 256", nil), Expect: expIproute2InvargAfterDev, Why: "a protocol past u8, after the base row's dev lookup"},
	}
}

// nhGroupMembers are the member lists, with each member's weight as the
// facts spell them ("id/weight;..."); a group's weight is 1 unless named.
var nhGroupMembers = []struct {
	name, arg, facts string
	fdb              bool // the members are fdb nexthops
}{
	{"one", "group 21", "21/1", false},
	{"two", "group 21/22", "21/1;22/1", false},
	{"weights", "group 21,10/22,20", "21/10;22/20", false},
	{"three", "group 21/22/23", "21/1;22/1;23/1", false},
	{"mixed", "group 21/26", "21/1;26/1", false},
	{"blackhole", "group 29", "29/1", false},
	{"fdb", "group 31/32", "31/1;32/1", true},
}

func nhGroupSpace() *space {
	members := axis{Name: "members"}
	for _, m := range nhGroupMembers {
		members.Values = append(members.Values, v(m.name, m.arg, f("group", m.facts)))
	}
	members.Values = append(members.Values,
		bv("weight-max", "group 21,1/22,65535", f("group", "21/1;22/65535")))
	sp := &space{
		Name: "nhgrp",
		Axes: []axis{
			nhVerbs,
			{Name: "fam", Values: []value{v("absent", "", nil)}},
			nhIDs("100"),
			members,
			{Name: "type", Values: []value{
				v("absent", "", nil),
				v("mpath", "type mpath", f("grp_type", "0")),
				v("res-buckets", "type resilient buckets 8", f("grp_type", "1", "buckets", "8")),
				v("res-all", "type resilient buckets 8 idle_timer 10 unbalanced_timer 20",
					f("grp_type", "1", "buckets", "8", "idle_timer", "1000", "unbalanced_timer", "2000")),
				bv("res-1", "type resilient buckets 2", f("grp_type", "1", "buckets", "2")),
				bv("res-idle-0", "type resilient buckets 4 idle_timer 0", f("grp_type", "1", "buckets", "4", "idle_timer", "0")),
			}},
			{Name: "fdb", Values: []value{v("absent", "", nil), v("fdb", "fdb", f("fdb", "1"))}},
			{Name: "hw_stats", Values: []value{
				v("absent", "", nil),
				v("on", "hw_stats on", f("hw_stats", "1")),
				v("off", "hw_stats off", f("hw_stats", "0")),
			}},
			{Name: "protocol", Values: []value{v("absent", "", nil), v("static", "protocol static", f("protocol", "4"))}},
		},
		Bad: []badValue{
			{Axis: "fam", Value: v("inet", "-4", f("family", "4")), Expect: *expKernelEINVAL,
				Why: "a group with a family (Invalid family for group)"},
			{Axis: "members", Value: v("weight-65536", "group 21,1/22,65536", f("group", "21/1;22/65536")), Expect: *expKernelEINVAL,
				Why: "weight 65536, which iproute2 sends as 0xffff and the kernel refuses (Invalid value for weight)"},
			{Axis: "members", Value: v("weight-65537", "group 21,65537", nil), Expect: expIproute2Invarg,
				Why: "a weight past 65536"},
			{Axis: "members", Value: v("weight-0", "group 21,0", nil), Expect: expIproute2Invarg, Why: "a weight of 0"},
			{Axis: "members", Value: v("twice", "group 21/21", f("group", "21/1;21/1")), Expect: *expKernelEINVAL,
				Why: "a member named twice (Nexthop id can not be used twice in a group)"},
			{Axis: "members", Value: v("unknown", "group 21/77", f("group", "21/1;77/1")), Expect: *expKernelEINVAL,
				Why: "a member that does not exist (Invalid nexthop id)"},
			{Axis: "members", Value: v("bh-multi", "group 21/29", f("group", "21/1;29/1")), Expect: *expKernelEINVAL,
				Why: "a blackhole in a group of two (Blackhole nexthop can not be used in a group with more than 1 path)"},
			{Axis: "type", Value: v("res-0", "type resilient buckets 0", f("grp_type", "1", "buckets", "0")), Expect: *expKernelEINVAL,
				Why: "a resilient group of no buckets (Number of buckets needs to be non-0)"},
			{Axis: "type", Value: v("res", "type resilient", f("grp_type", "1")), Expect: *expKernelEINVAL,
				Why: "a new resilient group without buckets (Number of buckets not specified for nexthop group insertion)"},
			{Axis: "type", Value: v("nosuch", "type nosuch", nil), Expect: expIproute2Invarg, Why: "an unknown group type"},
		},
	}
	for _, m := range nhGroupMembers {
		if m.fdb {
			sp.Excl = append(sp.Excl, exclusion{A: "members=" + m.name, B: "fdb=absent", Expect: expKernelEINVAL,
				Why: "fdb members in a group without fdb (Non FDB nexthop group cannot have fdb nexthops)"})
			continue
		}
		sp.Excl = append(sp.Excl, exclusion{A: "members=" + m.name, B: "fdb=fdb", Expect: expKernelEINVAL,
			Why: "a non-fdb member in an fdb group (FDB nexthop group can only have fdb nexthops)"})
	}
	sp.Excl = append(sp.Excl, exclusion{A: "members=weight-max", B: "fdb=fdb", Expect: expKernelEINVAL,
		Why: "a non-fdb member in an fdb group (FDB nexthop group can only have fdb nexthops)"})
	sp.Build = func(vals []value) row { return buildNexthop("ip ", "", nhMembers, sp.Axes, vals) }
	sp.Judge = nhJudge
	sp.NegBase = routeNegBase
	sp.Derive = func(pos []row) []row { return deriveDupGone("nhgrp", pos, "ENOENT", nhHasID) }
	return sp
}

// nhJudge: rtm_new_nexthop wants an id to replace (Replace requires
// nexthop id), and nh_valid_get_del_req one to delete (Nexthop id is
// missing; Invalid nexthop id for 0).
func nhJudge(pick map[string]string) (*expect, string) {
	if pick["id"] != "absent" && pick["id"] != "0" {
		return nil, ""
	}
	switch pick["verb"] {
	case "replace":
		return expKernelEINVAL, "a replace without an id (Replace requires nexthop id)"
	case "del":
		return expKernelEINVAL, "a delete without an id (Nexthop id is missing, or Invalid nexthop id)"
	}
	return nil, ""
}

// nhHasID: an add without an id gets a new one, so only an add with an id
// can meet itself again.
func nhHasID(r *row) bool { id := r.Axes["id"]; return id != "" && id != "0" }

// buildNexthop assembles `ip [-F] nexthop VERB ...`. A delete names only
// the id, as nh_valid_get_del_req takes nothing else (and wants the
// header's protocol 0), after adding what it deletes unless Judge refuses
// it; its axes keep only the id and family.
func buildNexthop(head, fam, members string, axes []axis, vals []value) row {
	verb := vals[0]
	args := make([]string, 0, len(vals))
	for i, val := range vals[1:] {
		if axes[i+1].Name == "fam" {
			head = joinArgs("ip", val.Arg, "nexthop") + " "
			continue
		}
		args = append(args, val.Arg)
	}
	tail := joinArgs(args...)
	pre := []preStep{{Cmd: routeSetup}}
	if members != "" {
		pre = append(pre, preStep{Cmd: members})
	}
	r := row{
		Cmd:      strings.TrimSpace(head + verb.Arg + " " + tail),
		Pre:      pre,
		Readback: []readback{{Name: "dump", Cmd: "ip nexthop show"}},
		Expect:   expect{RC: intp(0), RefusedBy: refNone},
		Axes:     mergeFacts(vals),
	}
	if fam != "" {
		r.Axes["family"] = fam
	}
	if verb.Name == "del" {
		id := vals[slices.IndexFunc(axes, func(a axis) bool { return a.Name == "id" })]
		r.Cmd = strings.TrimSpace(head + "del " + id.Arg)
		if exp, _ := nhJudge(pickOf(axes, vals)); exp == nil {
			r.Pre = append(r.Pre, preStep{Cmd: head + "add " + tail})
		}
		r.Axes = map[string]string{"verb": "del"}
		if id.Facts != nil {
			r.Axes["id"] = id.Facts["id"]
		}
		if fam != "" {
			r.Axes["family"] = fam
		}
	}
	return r
}
