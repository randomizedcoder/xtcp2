package main

// The nhstats space: `ip -s nexthop`'s readbacks (NC8-T04 of
// docs/netlink/goip-parity-next.md). Each row adds one nexthop object,
// after the nexthops it needs, and reads it back seven ways: the plain
// dump, -s, -s -s, -j -s, -j -s -s, and `-s nexthop get` and `-s nexthop
// show` of its id. Under -s ip sends NHA_OP_FLAGS in the dump
// (NHA_OP_FLAG_DUMP_STATS, plus DUMP_HW_STATS under -s -s); a get always
// sends it, 0 without -s (ip/ipnexthop.c ipnh_get_op_flags). No row sends
// traffic but the one corner row, so every counter is 0 and two boots agree.

import "strconv"

// nhstatsTwo is the two nexthops a group row's group holds.
const nhstatsTwo = "ip nexthop add id 1 via 192.0.2.1 dev d0 && ip nexthop add id 2 via 192.0.2.2 dev d0"

// nhv is a form of nexthop id id whose setup is pre.
func nhv(name string, id int, arg string, facts map[string]string, pre ...string) value {
	facts["id"] = strconv.Itoa(id)
	return value{Name: name, Arg: arg, Facts: facts, Pre: pre}
}

func nhStatsSpace() *space {
	sp := &space{
		Name: "nhstats",
		Axes: []axis{lwtxVerb, {Name: "form", Values: []value{
			nhv("group", 10, "nexthop add id 10 group 1/2", f("group", "1/2"), nhstatsTwo),
			nhv("resilient", 11, "nexthop add id 11 group 1/2 type resilient buckets 8",
				f("group", "1/2", "type", "resilient", "buckets", "8"), nhstatsTwo),
			nhv("hw-stats", 12, "nexthop add id 12 group 1/2 hw_stats on", f("group", "1/2", "hw_stats", "1"), nhstatsTwo),
			nhv("weighted", 13, "nexthop add id 13 group 1,3/2,7", f("group", "1,3/2,7"), nhstatsTwo),
			// A plain nexthop: -s changes the request, not the text.
			nhv("single", 3, "nexthop add id 3 via 192.0.2.3 dev d0", f("via", "192.0.2.3")),
			// seg6local's counters print only under -s (print_encap_seg6local).
			nhv("seg6local-count", 20, "-6 nexthop add id 20 encap seg6local action End count dev d0",
				localFacts("1", "count", "1")),
			// One entry, so no newline after it (print_nh_grp_stats).
			{Name: "group-one", Arg: "nexthop add id 14 group 1", Facts: f("group", "1", "id", "14"),
				Pre: []string{"ip nexthop add id 1 via 192.0.2.1 dev d0"}, Corner: true},
		}}},
	}
	sp.Build = func(vals []value) row {
		r := buildLwtx("ip ", "ip nexthop show", vals)
		id := r.Axes["id"]
		r.Readback = []readback{
			{Name: "dump", Cmd: "ip nexthop show"},
			{Name: "dump_s", Cmd: "ip -s nexthop show"},
			{Name: "dump_ss", Cmd: "ip -s -s nexthop show"},
			{Name: "dump_js", Cmd: "ip -j -s nexthop show"},
			{Name: "dump_jss", Cmd: "ip -j -s -s nexthop show"},
			{Name: "get_s", Cmd: "ip -s nexthop get id " + id},
			{Name: "show_id_s", Cmd: "ip -s nexthop show id " + id},
		}
		return r
	}
	sp.Derive = func(pos []row) []row {
		out := deriveDupGone(sp.Name, pos, "ENOENT", nhHasID)
		return append(out, nhStatsTraffic(pos))
	}
	return sp
}

// nhStatsTraffic is the one row that sends: three pings routed through
// group 10, so its stats count them. Which member the hash picks is up to
// the boot's hash seed, so its readbacks are a corner the gate compares by
// rc and message types only. The dummy answers nothing, so ping exits 1;
// ping sends no netlink request, so the row names no refuser and expects
// none sent.
func nhStatsTraffic(pos []row) row {
	var base row
	for i := range pos {
		if pos[i].Axes["id"] == "10" {
			base = pos[i]
			break
		}
	}
	r := base
	r.ID = "nhstats/traffic"
	r.Desc = "corner: three pings through group 10 count in its stats (not compared byte for byte)"
	r.Pre = append(append([]preStep{}, base.Pre...), preStep{Cmd: base.Cmd},
		preStep{Cmd: "ip route add 198.51.100.0/24 nhid 10"})
	r.Cmd = "ping -c 3 -W 1 -q 198.51.100.1"
	r.Expect = expect{RC: intp(1), Sent: intp(0)}
	r.Axes = f("verb", "traffic", "id", "10", "pings", "3")
	return r
}
