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

// evVeth is the link family's state: a veth pair, both ends up.
const evVeth = "ip link add r0 type veth peer name r0p" +
	" && ip link set r0 up && ip link set r0p up"

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

// evNH is the nexthop family's state: two nexthops through c0's subnet
// and the resilient group over them. The unbalanced timer is one second
// so a forced migration lands inside the helper's window: the only
// RTM_NEWNEXTHOPBUCKET notification the kernel ever sends is the deferred
// upkeep work's migration (nexthop_bucket_notify, net/ipv4/nexthop.c) —
// a group replace migrates idle buckets silently, and a bucket has to
// have carried traffic to be busy enough to defer. The 64 /dev/udp
// writes mark the buckets used; their flow hash spreads them over the
// four buckets, both of the overweight member's included (the EC2 probe
// measured it; the escape keeps the dollars for the helper's shell, the
// row cmd's own shell would expand them).
const evNH = evCanary + " && ip addr add 192.0.2.1/24 dev c0" +
	" && ip nexthop add id 1 via 192.0.2.254 dev c0" +
	" && ip nexthop add id 2 via 192.0.2.253 dev c0" +
	" && ip nexthop add id 10 group 1/2 type resilient buckets 4 idle_timer 60 unbalanced_timer 1"

// evSelfID pre-assigns the row's netns its own id, so `ip monitor`'s init
// probe (ip/ipnetns.c's ipnetns_have_nsid, which iproute2 itself expects to
// fail — "Continuing anyway") is answered with the id, not the EINVAL of
// an unassigned netns (net_namespace.c's rtnl_net_getid uses the
// non-allocating peernet2id, and preinit_net starts the table empty).
// The answer is kernel-state dependent otherwise, and a row's expect is
// static, so the row cannot express both shapes.
const evSelfID = `n=$(ip netns identify $$) && ip netns set "$n" 0`

// evRow builds one event row. groups is the `ip monitor` objects; maddress
// is appended unless the groups are all (they include it) or maddress
// itself (everr's row), because the canary that proves the monitor
// subscribed is a multicast join and `ip monitor all maddress` would be
// maddress alone — any object named replaces all (ip/ipmonitor.c's
// do_ipmonitor, as rtnl-write-corpus.exp's ipmon notes).
func evRowNoSelf(id, desc, groups, trigger string, until []string, floor int, pre string, modules []string) row {
	args := "monitor " + groups
	if groups != "all" && groups != "maddress" {
		args += " maddress"
	}
	cmd := "xtcp2-nl-ipmon c0 \"" + args + "\" \"" + trigger + "\""
	for _, re := range until {
		cmd += " \"" + re + "\""
	}
	return row{
		ID: "ev/" + id, Desc: desc, Cmd: cmd,
		// No evSelfID: the row's own trigger assigns the id, and the
		// monitor's init probe is answered the unassigned netns's own
		// reply — RTM_NEWNSID carrying NETNSA_NSID -1
		// (NETNSA_NSID_NOT_ASSIGNED; rtnl_net_getid's peernet2id), the
		// shape only this row holds (V381's probe measured it).
		Pre:   []preStep{{Cmd: pre}},
		Floor: floor, Modules: modules,
		Serial: true,
		Expect: expect{RC: intp(0), RefusedBy: refNone},
	}
}

func evRow(id, desc, groups, trigger string, until []string, floor int, pre string, modules []string) row {
	args := "monitor " + groups
	if groups != "all" && groups != "maddress" {
		args += " maddress"
	}
	cmd := "xtcp2-nl-ipmon c0 \"" + args + "\" \"" + trigger + "\""
	for _, re := range until {
		cmd += " \"" + re + "\""
	}
	return row{
		ID: "ev/" + id, Desc: desc, Cmd: cmd,
		Pre:   []preStep{{Cmd: evSelfID + " && " + pre}},
		Floor: floor, Modules: modules,
		// Serial: `ip monitor`'s init probe (ipnetns_have_nsid) is answered
		// EINVAL at random when another row's netns churn runs concurrently —
		// reproduced with the self-id provably in the table the whole time
		// (the EC1 probe's diag runs, V380) — so an ev row runs with no other
		// row active, and its window is deterministic.
		Serial: true,
		Expect: expect{RC: intp(0), RefusedBy: refNone},
	}
}

func evFamily() []row {
	rows := []row{
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
			48, evAnycast+" && "+evRA, []string{"veth"}),
		// EC1, the four base families. Every floor below starts as 1 —
		// the probe floor — and is renewed from the measurement the probe
		// batch and single-row runs make (the ev seeds' rule, V379).
		evRow("evlink-carrier",
			"positive: a veth's carrier leaves with its peer and returns, the NO-CARRIER state an admin down never prints",
			"link",
			"ip link set r0p down; ip link set r0p up",
			[]string{"r0@r0p: .*NO-CARRIER", "r0@r0p: .*state UP"},
			27, evCanary+" && "+evVeth, []string{"veth"}),
		evRow("evlink-admin",
			"positive: admin down and up, the same state DOWN without NO-CARRIER",
			"link",
			"ip link set r0 down; ip link set r0 up",
			[]string{"r0@r0p: <BROADCAST,MULTICAST> .*state DOWN", "r0@r0p: .*state UP"},
			27, evCanary+" && "+evVeth, []string{"veth"}),
		evRow("evlink-dummy",
			"positive: a dummy's add and delete, a clean RTM_DELLINK no peer entangles",
			"link",
			"ip link add d1 type dummy && ip link del d1",
			[]string{"d1: <", "^Deleted [0-9]+: d1: "},
			23, evCanary, nil),
		evRow("evlink-altname",
			"positive: an altname change, one RTM_NEWLINK carrying IFLA_PROP_LIST and no RTM_NEWLINKPROP",
			"link",
			"ip link property add dev c0 altname a0",
			[]string{"altname a0"},
			15, evCanary, nil),
		evRow("evaddr-v4",
			"positive: a v4 address's add and delete",
			"address",
			"ip addr add 192.0.2.1/24 dev c0; ip addr del 192.0.2.1/24 dev c0",
			[]string{"192.0.2.1/24", "Deleted .*192.0.2.1/24"},
			21, evCanary, nil),
		evRow("evaddr-v6",
			"positive: a v6 address's add and delete, nodad so one print each",
			"address",
			"ip -6 addr add 2001:db8:2::1/64 dev c0 nodad; ip -6 addr del 2001:db8:2::1/64 dev c0",
			[]string{"2001:db8:2::1/64", "Deleted .*2001:db8:2::1/64"},
			21, evCanary, nil),
		evRow("evaddr-dad",
			"positive: DAD's pair, the tentative RTM_NEWADDR and the final one after the flag clears",
			"address",
			"ip -6 addr add 2001:db8:1::1/64 dev d0",
			[]string{"2001:db8:1::1/64 scope global tentative", "2001:db8:1::1/64 scope global $"},
			17, evCanary+" && ip link add d0 type dummy && ip link set d0 addrgenmode none && ip link set d0 up", nil),
		evRow("evaddr-dadfailed",
			"positive: the dadfailed corner, a duplicate on the peer and one DAD probe",
			"address",
			"ip -6 addr add 2001:db8:1::1/64 dev r0",
			[]string{"2001:db8:1::1/64", "dadfailed"},
			18, evCanary+" && "+evVeth+
				" && sysctl -wq net.ipv6.conf.r0.dad_transmits=1"+
				" && ip -6 addr add 2001:db8:1::1/64 dev r0p nodad", []string{"veth"}),
		evRow("evroute-onlink",
			"positive: an on-link route's add, scope link",
			"route",
			"ip route add 192.0.2.0/24 dev c0",
			[]string{"192.0.2.0/24 dev c0 scope link"},
			16, evCanary, nil),
		evRow("evroute-via",
			"positive: a route through a gateway",
			"route",
			"ip route add 198.51.100.0/24 via 192.0.2.254 dev c0",
			[]string{"198.51.100.0/24 via 192.0.2.254"},
			16, evCanary+" && ip addr add 192.0.2.1/24 dev c0", nil),
		evRow("evroute-table",
			"positive: a route in table 100, the table the print names",
			"route",
			"ip route add table 100 203.0.113.0/24 dev c0",
			[]string{"203.0.113.0/24 dev c0"},
			16, evCanary, nil),
		evRow("evroute-blackhole",
			"positive: a blackhole route, no device at all",
			"route",
			"ip route add blackhole 198.18.0.0/15",
			[]string{"blackhole 198.18.0.0/15"},
			14, evCanary, nil),
		evRow("evroute-v6",
			"positive: a v6 route's add",
			"route",
			"ip -6 route add 2001:db8:9::/64 dev c0",
			[]string{"2001:db8:9::/64 dev c0"},
			16, evCanary, nil),
		evRow("evroute-autoconf",
			"positive: the kernel's own autoconf route, pid 0, the case the flags discriminator exists for",
			"route",
			evRASend,
			[]string{"2001:db8:5::/64 dev r0 proto kernel"},
			12, evCanary+" && "+evRA, []string{"veth"}),
		evRow("evneigh-v4",
			"positive: a v4 neigh's life, permanent, turned stale, deleted",
			"neigh",
			"ip neigh replace 192.0.2.254 lladdr 02:00:00:00:00:01 dev c0 nud permanent;"+
				" ip neigh chg 192.0.2.254 dev c0 nud stale; ip neigh del 192.0.2.254 dev c0",
			[]string{"192.0.2.254 dev c0 lladdr 02:00:00:00:00:01 PERMANENT",
				"192.0.2.254 dev c0 lladdr 02:00:00:00:00:01 STALE",
				"^Deleted 192.0.2.254 dev c0"},
			29, evCanary, nil),
		evRow("evneigh-v6",
			"positive: a v6 neigh's life, permanent, turned stale, deleted",
			"neigh",
			"ip neigh replace 2001:db8:1::2 lladdr 02:00:00:00:00:01 dev c0 nud permanent;"+
				" ip neigh chg 2001:db8:1::2 dev c0 nud stale; ip neigh del 2001:db8:1::2 dev c0",
			[]string{"2001:db8:1::2 dev c0 lladdr 02:00:00:00:00:01 PERMANENT",
				"2001:db8:1::2 dev c0 lladdr 02:00:00:00:00:01 STALE",
				"^Deleted 2001:db8:1::2 dev c0"},
			29, evCanary, nil),
		evRow("everr-esrch",
			"negative: a delete of a route that does not exist, one genuine NLMSG_ERROR -ESRCH in the window",
			"maddress",
			"ip route del 192.0.2.128/32 dev c0",
			[]string{"mcast 224.0.0.250 "},
			14, evCanary+" && ip addr add 192.0.2.1/24 dev c0", nil),
		evRow("evseq-mixed",
			"positive: one ip monitor all over a link, an address, a route, a neigh and a netconf, the mixed time sequence",
			"all",
			"ip link set d0 down; ip link set d0 up; ip addr add 192.0.2.1/24 dev d0;"+
				" ip route add 198.51.100.0/24 dev d0; ip neigh replace 192.0.2.254 lladdr 02:00:00:00:00:01 dev d0;"+
				" echo 1 > /proc/sys/net/ipv4/conf/d0/forwarding",
			[]string{"d0: <", "192.0.2.1/24", "198.51.100.0/24 dev d0",
				"192.0.2.254 dev d0 lladdr", "inet d0 forwarding on"},
			46, evAnycast+" && ip link set d0 up", nil),
		// EC2, the notification families with no capture
		// (goip-parity-events.md EC2). The floors are the minimum over
		// the probe and two clean batch runs (the ev seeds' rule, V379):
		// the canary join can race the monitor's subscription readiness
		// even serial, and the helper's retry adds the delete/re-add pair
		// and two GETNSID roundtrips to a window (V379).
		evRow("evnhb-bucket",
			"positive: a busy bucket forced to migrate by the unbalanced timer, RTM_NEWNEXTHOPBUCKET with NLM_F_REPLACE",
			"nexthop",
			"for i in \\$(seq 64); do echo x > /dev/udp/198.51.100.\\$i/9; done;"+
				" ip nexthop replace id 10 group 1,3/2,1 type resilient buckets 4 idle_timer 60 unbalanced_timer 1",
			[]string{"id 10 index [0-9]+ .*nhid 1"},
			14, evNH+" && ip route add 198.51.100.0/24 nhid 10", nil),
		evRow("evnhb-gw",
			"corner: a member's gateway replaced, no bucket notification — the buckets do not move",
			"nexthop",
			"ip nexthop replace id 1 via 192.0.2.250 dev c0",
			[]string{"id 1 via 192.0.2.250 dev c0 scope link"},
			15, evNH, nil),
		evRow("evconf-del",
			"positive: a forwarded device unregistered, RTM_DELNETCONF with its ifindex",
			"netconf",
			"ip link del d0",
			[]string{"Deleted inet d0"},
			18, evCanary+" && ip link add d0 type dummy && ip link set d0 up &&"+
				" echo 1 > /proc/sys/net/ipv4/conf/d0/forwarding", nil),
		evRow("evconf-clean",
			"corner: a device unregistered without forwarding ever set, RTM_DELNETCONF anyway",
			"netconf",
			"ip link del d1",
			[]string{"Deleted inet d1"},
			17, evCanary+" && ip link add d1 type dummy", nil),
		evRow("evrule-v4",
			"positive: a rule's add and del, the selector set",
			"rule",
			"ip rule add pref 100 from 192.0.2.0/24 to 198.51.100.0/24 tos 4 fwmark 0x64/0xff iif c0 oif c0 table 100;"+
				" ip rule del pref 100",
			[]string{"100:.*from 192.0.2.0/24 to 198.51.100.0/24 tos 0x04 fwmark 0x64/0xff iif c0 oif c0 lookup 100",
				"^Deleted 100:.*from 192.0.2.0/24"},
			16, evCanary, nil),
		evRow("evrule-v6",
			"positive: a v6 rule's add and del",
			"rule",
			"ip -6 rule add pref 200 to 2001:db8:9::/64 table 100; ip -6 rule del pref 200",
			[]string{"200:.*from all to 2001:db8:9::/64 lookup 100",
				"^Deleted 200:.*from all to 2001:db8:9::/64"},
			16, evCanary, nil),
		evRow("evstats-l3",
			"positive: the l3_stats off and on pair",
			"stats",
			"ip stats set dev c0 l3_stats off; ip stats set dev c0 l3_stats on",
			[]string{"l3_stats off used", "l3_stats on used"},
			20, evCanary+" && ip stats set dev c0 l3_stats on", nil),
		evRowNoSelf("evnsid-live",
			"positive: an id assigned to the row's own live netns, no add or del around it",
			"nsid",
			"n=\\$(ip netns identify \\$\\$) && ip netns set \\\"\\$n\\\" 3",
			[]string{"nsid 3 "},
			15, evCanary, nil),
		evRow("evnsid-eexist",
			"negative: a second id for a netns that already has one, the kernel's EEXIST",
			"nsid",
			"n=\\$(ip netns identify \\$\\$) && ip netns set \\\"\\$n\\\" 3",
			[]string{"mcast 224.0.0.250 "},
			12, evCanary, nil),
		evRow("evprefix-dep",
			"corner: the deprecated prefix, preferred lifetime zero",
			"prefix",
			"xtcp2-nl-ra r0p fe80::2 2001:db8:5::/64 0 0",
			[]string{"prefix 2001:db8:5::/64dev r0 valid 600 preferred 0"},
			11, evCanary+" && "+evRA, []string{"veth"}),
		evRow("evprefix-route",
			"positive: the RA's default route and on-link prefix, one window",
			"prefix route",
			"xtcp2-nl-ra r0p fe80::2 2001:db8:5::/64 600",
			[]string{"default via fe80::2 dev r0 proto ra", "prefix 2001:db8:5::/64dev r0"},
			14, evCanary+" && "+evRA, []string{"veth"}),
	}
	// everr's trigger is the failure itself: the helper's rc 1 (the
	// trigger's own exit) and the kernel's ESRCH are the row's oracle, so
	// its expect says the refusal, not a clean window. evnsid-eexist is
	// the same shape for a second id: the kernel's EEXIST. evnsid-live
	// keeps the default clean expect: without the self-id its probe is
	// answered the unassigned netns's own reply, RTM_NEWNSID with
	// NETNSA_NSID -1, and no refusal happens at all.
	for i := range rows {
		switch rows[i].ID {
		case "ev/everr-esrch":
			rc := 1
			rows[i].Expect.RC = &rc
			rows[i].Expect.RefusedBy = refKernel
			rows[i].Expect.Errno = "ESRCH"
		case "ev/evnsid-eexist":
			rc := 1
			rows[i].Expect.RC = &rc
			rows[i].Expect.RefusedBy = refKernel
			rows[i].Expect.Errno = "EEXIST"
		}
	}
	return rows
}
