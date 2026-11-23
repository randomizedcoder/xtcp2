package main

// The bridge mdb read space (gobridge 7C): `bridge mdb show` and `bridge mdb
// get` (bridge/mdb.c mdb_show, mdb_get, print_mdb_entry,
// br_print_router_ports), each plain and -j, over fixed-MAC fixtures. The
// 7_3_0 mdbbr/mdbvx write rows read back only `mdb show dev X`, plain; these
// add -d (filter mode, source lists, protocol, router ports), -s (timers),
// -o, vid, the get forms and batch.
//
// mdb_show sends two RTM_GETMDB dumps, a br_port_msg naming no device: the
// entries, then the router ports. dev and vid filter in userspace (the
// kernel's strict check refuses a device index). mdb_get sends one
// RTM_GETMDB get; the kernel answers with one RTM_NEWMDB, or refuses (rc
// 254, mdb_get's -2).
//
// The rows pin increment 7's mdb leads (M1-M11 in gobridge.md): unknown
// words and even `help` are ignored by mdb show (M1); get and help want an
// exact match (M2); vid outlives its batch line (M3); router ports are
// keyed by the bridge's name and printed only with -d (M7); mdb show and
// mdb get print different JSON shapes (M10).
//
// Determinism: fixed MACs, addrgenmode none. Permanent entries and a
// permanent router port have no running timer; the temp entry's timer runs,
// which -s shows (the allow-list in V108).

// The kernel installs an entry added without a vid on every VLAN of
// the port (1 and 10), so a get names vid 10: vid 0 finds nothing.
//
// mdbFix is br0 snooping with IGMPv3 and MLDv2, ports d1 and d2 on vid 10,
// and entries: 239.1.1.1 on d1 vid 10, ff0e::1 temp on d2, the *,G
// 239.2.2.2 including two sources, the S,G 232.1.1.1 from 10.0.0.1, an L2
// group, and a host join; d1 is a permanent router port.
var mdbFix = []string{
	// IPv6 off on br0: its own MLD report would otherwise add a host
	// join to ff02::6a at a random moment, so two runs would differ.
	brCreate(false) + " && ip link set br0 type bridge mcast_igmp_version 3 mcast_mld_version 2" +
		" && echo 1 > /proc/sys/net/ipv6/conf/br0/disable_ipv6",
	brPorts("d1", "d2"),
	"bridge vlan add dev d1 vid 10 && bridge vlan add dev d2 vid 10 && bridge vlan add dev br0 vid 10 self",
	"bridge mdb add dev br0 port d1 grp 239.1.1.1 permanent vid 10",
	"bridge mdb add dev br0 port d2 grp ff0e::1 temp",
	"bridge mdb add dev br0 port d1 grp 239.2.2.2 permanent filter_mode include source_list 10.0.0.1,10.0.0.2 proto static",
	"bridge mdb add dev br0 port d2 grp 232.1.1.1 src 10.0.0.1 permanent",
	"bridge mdb add dev br0 port d2 grp 01:02:03:04:05:06 permanent",
	"bridge mdb add dev br0 port br0 grp 239.3.3.3 temp", // a host join takes no flags: the kernel refuses permanent
	"bridge link set dev d1 mcast_router 2",
}

// mdbVxFix is vx0, a VXLAN with VNI 100 over d5, with remote entries: a
// plain one, one with a port, VNI, source VNI, via and protocol, and an
// IPv6 group.
var mdbVxFix = []string{
	"ip link add d5 address 02:00:00:00:01:05 type dummy && ip link set d5 addrgenmode none && ip link set d5 up" +
		" && ip addr add 192.0.2.10/24 dev d5",
	"ip link add vx0 address 02:00:00:00:01:10 type vxlan id 100 dstport 4789 local 192.0.2.10 nolearning" +
		" && ip link set vx0 addrgenmode none && ip link set vx0 up",
	"bridge mdb add dev vx0 port vx0 grp 239.1.1.1 permanent dst 192.0.2.1",
	"bridge mdb add dev vx0 port vx0 grp 239.1.1.2 permanent dst 192.0.2.2 dst_port 4790 vni 200 src_vni 300 via d5 proto static",
	"bridge mdb add dev vx0 port vx0 grp ff0e::1 permanent dst 192.0.2.3",
}

// bridgemdbSpace: mdb show and get, each plain and -j.
func bridgemdbSpace() *space {
	ack := expect{RC: intp(255), RefusedBy: refAckThenFail}
	get := func(errno, extack string) expect {
		return expect{RC: intp(254), Errno: errno, Extack: extack, RefusedBy: refKernel}
	}
	sp := &space{
		Name: "bridgemdb",
		Axes: []axis{
			{Name: "opt", Values: []value{
				v("plain", "", f("json", "0")),
				v("json", "-j", f("json", "1")),
			}},
			{Name: "cmd", Values: []value{
				bc(brBoundary, "bare", "mdb show", nil),
				bc(brPos, "mdb", "mdb show", mdbFix),
				bc(brCorner, "mdb", "mdb", mdbFix), // no words: show
				bc(brPos, "mdb", "-d mdb show", mdbFix),
				bc(brPos, "mdb", "-s mdb show", mdbFix),
				bc(brPos, "mdb", "-d -s mdb show", mdbFix),
				bc(brCorner, "mdb", "-o -d mdb show", mdbFix),    // oneline: \ for each newline
				bc(brCorner, "mdb", "-o -d -s mdb show", mdbFix), // the -s router lines ignore -o (F11)
				bc(brPos, "mdb", "mdb show dev br0", mdbFix),
				bc(brPos, "mdb", "mdb show vid 10", mdbFix),
				bc(brCorner, "mdb", "mdb show dev d1", mdbFix),  // a port: no bridge matches
				bc(brCorner, "mdb", "mdb show vid abc", mdbFix), // atoi: vid 0, no filter
				bc(brCorner, "mdb", "mdb show bogus", mdbFix),   // unknown words ignored (M1)
				bc(brCorner, "mdb", "mdb show help", mdbFix),    // even help (M1)
				bc(brCorner, "mdb", "mdb sh", mdbFix),           // matches(): a prefix is show
				bc(brPos, "mdb", "mdb get dev br0 grp 239.1.1.1 vid 10", mdbFix),
				bc(brPos, "mdb", "-d mdb get dev br0 grp 239.2.2.2 vid 10", mdbFix),
				bc(brPos, "mdb", "mdb get dev br0 grp 232.1.1.1 src 10.0.0.1 vid 10", mdbFix),
				bc(brPos, "mdb", "mdb get dev br0 grp ff0e::1 vid 10", mdbFix),
				bc(brPos, "mdb", "mdb get dev br0 grp 01:02:03:04:05:06 vid 10", mdbFix),
				bc(brCorner, "mdb", "mdb get dev br0 grp 239.1.1.1 vid 10 bogus", mdbFix), // ignored
				// vxlan: dst, dst_port, vni, src_vni, via.
				bc(brPos, "mdbvx", "mdb show", mdbVxFix),
				bc(brPos, "mdbvx", "-d mdb show", mdbVxFix),
				bc(brPos, "mdbvx", "mdb show dev vx0", mdbVxFix),
				bc(brPos, "mdbvx", "mdb get dev vx0 grp 239.1.1.2 src_vni 300", mdbVxFix), // src_vni is part of the key
				bc(brPos, "mdbvx", "mdb get dev vx0 grp 239.1.1.1", mdbVxFix),
				// Batch: vid outlives its line (M3).
				bb(brCorner, "mdb", "sticky-vid", "", false, batchFile("mdb show vid 10", "mdb show"), mdbFix),
				bb(brCorner, "mdb", "sticky-vid-get", "", false,
					batchFile("mdb show vid 1", "mdb get dev br0 grp ff0e::1 vid 10"), mdbFix),
				bb(brCorner, "mdb", "sticky-dev", "", false, batchFile("mdb show dev d1", "mdb show"), mdbFix),
			}},
		},
		Bad: []badValue{
			brBad("bare", "mdb badcmd", nil, ack, "an unknown mdb sub-command, after ll_init_map"),
			brBad("bare", "mdb help", nil, ack, "mdb help prints the usage and exits 255"),
			brBad("mdb", "mdb g dev br0 grp 239.1.1.1", mdbFix, ack, "mdb get dispatches with strcmp: a prefix is unknown (M2)"),
			brBad("bare", "mdb show dev nosuch", nil, expBridgeENODEV, "no such device"),
			brBad("bare", "mdb show dev", nil, ack, "dev with no value: the command is incomplete"),
			brBad("mdb", "mdb show vid 10 vid 20", mdbFix, ack, "vid given twice"),
			brBad("mdb", "mdb get dev br0 grp 239.9.9.9", mdbFix, get("ENOENT", "MDB entry not found"),
				"no such entry: rtnl_talk fails and mdb_get returns -2"),
			brBad("mdb", "mdb get dev br0 grp 239.2.2.2", mdbFix, get("ENOENT", "MDB entry not found"),
				"no vid: the entry lives on vids 1 and 10, and the kernel looks up vid 0"),
			brBad("mdb", "mdb get dev br0 grp 239.1.1.1 vid 5000", mdbFix, get("EINVAL", "Invalid entry VLAN id"),
				"a vid of 4095 or more"),
			brBad("mdb", "mdb get dev br0", mdbFix, ack, "Device and group address are required arguments"),
			brBad("mdb", "mdb get dev br0 grp bogus", mdbFix, ack, "Invalid address"),
			brBad("mdb", "mdb get dev br0 grp 239.1.1.1 src bogus", mdbFix, ack, "Invalid source address"),
			brBad("mdb", "mdb get dev br0 grp 239.1.1.1 src_vni x", mdbFix, ack, "Invalid source VNI"),
			brBad("mdbvx", "mdb get dev vx0 grp 239.1.1.2", mdbVxFix, get("ENOENT", "MDB entry not found"),
				"the entry was added with src_vni 300, which is part of the vxlan key: a get without it finds nothing"),
			brBad("mdb", "mdb get dev nosuch grp 239.1.1.1", mdbFix, expBridgeENODEV, "no such device"),
			bbBad("mdb", "sticky-vid-duparg", "-force", false, batchFile("mdb show vid 10", "mdb show vid 10"), mdbFix,
				ack, "the first vid is still set, so the second line is a duparg, which exits 255 through -force (M3)"),
		},
	}
	sp.Build = bridgeSpace().Build
	sp.NegBase = func(vals []value) bool {
		return pickOf(sp.Axes, vals)["opt"] == "plain"
	}
	return sp
}
