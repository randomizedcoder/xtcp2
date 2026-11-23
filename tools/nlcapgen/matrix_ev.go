package main

// The ev family: the event rows (docs/netlink/goip-parity-events.md,
// EC0-T02). A row's cmd is one monitor-helper invocation —
// `xtcp2-nl-ipmon c0 "<ip-args>" "<trigger>" "<until-ERE>..."` — run by
// `xtcp2-nlcap write` inside the capture window, write mode's one attempt,
// so one pcap holds the monitor's deliveries, the trigger's requests and
// the canary's, and the row's ip.out holds the monitor's decoded output of
// the same run: the capio discipline, "both halves land together or neither
// does", applied to events.
//
// The seed rows are the rtnl-write corpus's four proven event rows
// (scripts/rtnl-write-corpus.exp), restated as workbench rows: the same
// pre steps, triggers and until-EREs. The floors are the workbench's own
// measurements (EC1's probe): 16, 25, 11 and 54 datagrams, the minimum each
// row's window was observed to hold in EITHER context the row can run in —
// alone (`xtcp2-nlcap-ws run`) or with the batch's default -j 8, four
// monitors racing one guest. The parallel batch is the racer: ev/nsid-del
// alone joins its canary ahead of the monitor's init dump (22 and 31
// datagrams captured), the helper's join retry adding a delete/re-add pair
// and two extra GETNSID roundtrips, and the others pick up load-dependent
// datagrams the same way (prefix 17-26, anycast 31-40, monitor-all 54-60).
// The retry is the helper working, and those captures are valid corpus,
// just above the floor. The canary rule stays: a monitor is subscribed only
// when it has proved it, so every ipmon row's groups name maddress and the
// canary join's events stay in the pcap and the output, counted in the floor.
//
// c0 is the up dummy the canary joins; d0 the anycast row's; r0/r0p the
// veth the router advertisement crosses. The pre sysctls are the
// workbench's = form (fibt's rows); the guest's procps rejects the
// `key value` form, which the EC1 probe measured live.

// evCanary is c0, up.
const evCanary = "ip link add c0 type dummy && ip link set c0 up"

// evAnycast is the anycast row's state: forwarding on `default` so d0
// inherits it, c0 for the canary, and d0 with no kernel link-local of its
// own. The sysctl is the workbench's = form (fibt's rows; the guest's
// procsys rejects `key value`, measured by the EC1 probe).
const evAnycast = "sysctl -wq net.ipv6.conf.default.forwarding=1 && " + evCanary +
	" && ip link add d0 type dummy && ip link set d0 addrgenmode none"

// evRA is the prefix row's state: a veth whose r0 accepts RAs from local
// (accept_ra 2 and accept_ra_from_local 1, the kernel's two gates) with
// r0p's link-local added without DAD, so the sender's source matches.
const evRA = "ip link add r0 type veth peer name r0p && ip link set r0 addrgenmode none" +
	" && ip link set r0p addrgenmode none && sysctl -wq net.ipv6.conf.r0.accept_ra=2" +
	" && sysctl -wq net.ipv6.conf.r0.accept_ra_from_local=1 && ip link set r0p up" +
	" && ip link set r0 up && ip -6 addr add fe80::2/64 dev r0p nodad"

// evRASend is one router advertisement out of r0p: 2001:db8:5::/64,
// on-link, from fe80::2 (xtcp2-nl-ra, on xtcp2-nlcap's PATH).
const evRASend = "xtcp2-nl-ra r0p fe80::2 2001:db8:5::/64"

// evTriggerAll is monitor-all's: a link state, an address, a netconf, a
// rule, a nexthop, an l3_stats state, an nsid's life and the RA, in the
// order `ip monitor all` prints them.
const evTriggerAll = "ip link set d0 up; ip -6 addr add 2001:db8:1::1/64 dev d0 nodad;" +
	" echo 1 > /proc/sys/net/ipv4/conf/d0/forwarding;" +
	" ip rule add pref 100 from 192.0.2.0/24 table 100; ip nexthop add id 1 blackhole;" +
	" ip stats set dev d0 l3_stats on;" +
	" ip netns add c4p && ip netns set c4p 9 && ip netns del c4p; " + evRASend

// evUntilAll is what `ip monitor all` prints for each, after its [LABEL]
// banner: the netconf family prints as `inet`, the stats message as the
// device and, on the next line, its l3_stats state.
var evUntilAll = []string{
	"any 2001:db8:1:: ",
	"inet d0 forwarding on",
	`100:.*from 192.0.2.0/24 lookup 100`,
	"id 1 blackhole",
	"l3_stats on used",
	"nsid 9 ",
	"Deleted nsid 9 ",
	"prefix 2001:db8:5::/64",
}

// evRow builds one event row. groups is the `ip monitor` objects; maddress
// is appended unless the groups are all, because the canary that proves the
// monitor subscribed is a multicast join and `ip monitor all maddress`
// would be maddress alone — any object named replaces all
// (ip/ipmonitor.c's do_ipmonitor, as rtnl-write-corpus.exp's ipmon notes).
func evRow(id, desc, groups, trigger string, until []string, floor int, pre string, modules []string) row {
	args := "monitor " + groups
	if groups != "all" {
		args += " maddress"
	}
	cmd := "xtcp2-nl-ipmon c0 \"" + args + "\" \"" + trigger + "\""
	for _, re := range until {
		cmd += " \"" + re + "\""
	}
	return row{
		ID: "ev/" + id, Desc: desc, Cmd: cmd, Pre: []preStep{{Cmd: pre}},
		Floor: floor, Modules: modules,
		Expect: expect{RC: intp(0), RefusedBy: refNone},
	}
}

func evFamily() []row {
	return []row{
		evRow("nsid-del",
			"positive: an nsid's whole life, added, assigned 7 and deleted, under a monitor",
			"nsid",
			"ip netns add c1del && ip netns set c1del 7 && ip netns del c1del",
			[]string{"^nsid 7 ", "^Deleted nsid 7 "},
			16, evCanary, nil),
		evRow("anycast-del",
			"positive: the subnet-router anycast joins with d0's first v6 address and leaves with its delete",
			"acaddress",
			"ip link set d0 up; ip -6 addr add 2001:db8:1::1/64 dev d0 nodad;"+
				" ip -6 addr del 2001:db8:1::1/64 dev d0",
			[]string{"any 2001:db8:1:: ", "Deleted .*any 2001:db8:1:: "},
			25, evAnycast, nil),
		evRow("prefix",
			"positive: one router advertisement's on-link prefix, through a monitor",
			"prefix",
			evRASend,
			[]string{"prefix 2001:db8:5::/64"},
			11, evCanary+" && "+evRA, []string{"veth"}),
		evRow("monitor-all",
			"positive: one ip monitor all over a link, an address, a netconf, a rule, a nexthop, stats, an nsid and the RA",
			"all",
			evTriggerAll,
			evUntilAll,
			54, evAnycast+" && "+evRA, []string{"veth"}),
	}
}
