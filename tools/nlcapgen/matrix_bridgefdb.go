package main

// The bridge fdb read space (gobridge 7C): `bridge fdb show` and `bridge fdb
// get` (bridge/fdb.c fdb_show, fdb_get, print_fdb), each plain and -j, over
// fixed-MAC fixtures. The 7_3_0 fdbbr/fdbvx write rows read back only
// `fdb show dev X`, plain, over random MACs; these rows add the filters
// (br, brport/dev, vlan, state, dynamic), -s, -d, -o, the get forms, and
// batch.
//
// fdb_show sends one RTM_GETNEIGH dump, an AF_BRIDGE ndmsg with the dev
// filter's index and NDA_MASTER for br; vlan, state and dynamic filter in
// userspace. fdb_get sends one RTM_GETNEIGH get, which the kernel checks
// strictly (rtnl_fdb_get): it refuses br with dev, a vlan outside 1-4094,
// and a device that has no fdb get (rc 254, fdb_get's -2).
//
// The rows pin increment 7's fdb leads (F1-F14 in gobridge.md): dev and br
// given twice override silently (F1); the filter statics outlive their
// batch line, `state` ORs across lines and `fdb get ... dynamic` leaks into
// the next show (F2); the dev column is dropped while filter_index is set
// (F3); `dynamic` keeps static entries (F4); `state s` is static and
// `state 0` no filter (F5); a REACHABLE entry's state is "" (F7); -o is
// ignored (F11); fdb_get's vlan is a short (F13).
//
// Determinism: fixed MACs, addrgenmode none, STP off. -s prints how many
// seconds since each entry was used and updated, which may tick between
// batches (the allow-list in V108).

// fdbFix is br0 with ports d1 and d2, both on vid 10, and an entry of each
// kind: 0a:01 static on vid 10, 0a:02 permanent (local), 0a:03 dynamic
// (REACHABLE), 0a:04 static sticky, 0a:05 extern_learn, 0a:06 static with
// activity_notify (shown under -d), and 0a:07 in d1's own table (self).
var fdbFix = []string{
	brCreate(false), brPorts("d1", "d2"),
	"bridge vlan add dev d1 vid 10 && bridge vlan add dev d2 vid 10",
	"bridge fdb add 02:00:00:00:0a:01 dev d1 master static vlan 10",
	"bridge fdb add 02:00:00:00:0a:02 dev d1 master permanent",
	"bridge fdb add 02:00:00:00:0a:03 dev d2 master dynamic",
	"bridge fdb add 02:00:00:00:0a:04 dev d2 master static sticky",
	"bridge fdb add 02:00:00:00:0a:05 dev d1 master extern_learn",
	"bridge fdb add 02:00:00:00:0a:06 dev d1 master static activity_notify",
	"bridge fdb add 02:00:00:00:0a:07 dev d1 self permanent",
}

// fdbVxFix is vx0, a VXLAN with VNI 100 over d5, with remote entries: the
// all-zeros default to 192.0.2.1 and appended to 192.0.2.2 with a port,
// VNI and via; 0b:01 with a source VNI; 0b:02 through fdb nexthop group 10.
var fdbVxFix = []string{
	"ip link add d5 address 02:00:00:00:01:05 type dummy && ip link set d5 addrgenmode none && ip link set d5 up" +
		" && ip addr add 192.0.2.10/24 dev d5",
	"ip link add vx0 address 02:00:00:00:01:10 type vxlan id 100 dstport 4789 local 192.0.2.10 nolearning" +
		" && ip link set vx0 addrgenmode none && ip link set vx0 up",
	"ip nexthop add id 1 via 192.0.2.1 fdb && ip nexthop add id 10 group 1 fdb",
	"bridge fdb add 00:00:00:00:00:00 dev vx0 dst 192.0.2.1",
	"bridge fdb append 00:00:00:00:00:00 dev vx0 dst 192.0.2.2 port 4790 vni 200 via d5",
	"bridge fdb add 02:00:00:00:0b:01 dev vx0 dst 192.0.2.3 src_vni 300",
	"bridge fdb add 02:00:00:00:0b:02 dev vx0 nhid 10 self",
}

// fdbA1 is fdbFix's static vid 10 entry, the one most gets ask for.
const fdbA1 = "02:00:00:00:0a:01"

// bridgefdbSpace: fdb show and get, each plain and -j.
func bridgefdbSpace() *space {
	ack := expect{RC: intp(255), RefusedBy: refAckThenFail}
	get := func(errno, extack string) expect {
		return expect{RC: intp(254), Errno: errno, Extack: extack, RefusedBy: refKernel}
	}
	sp := &space{
		Name: "bridgefdb",
		Axes: []axis{
			{Name: "opt", Values: []value{
				v("plain", "", f("json", "0")),
				v("json", "-j", f("json", "1")),
			}},
			{Name: "cmd", Values: []value{
				bc(brBoundary, "bare", "fdb show", nil),
				bc(brPos, "fdb", "fdb show", fdbFix),
				bc(brCorner, "fdb", "fdb", fdbFix), // no words: show
				bc(brPos, "fdb", "-s fdb show", fdbFix),
				bc(brPos, "fdb", "-d fdb show", fdbFix),
				bc(brPos, "fdb", "fdb show br br0", fdbFix),
				bc(brPos, "fdb", "fdb show brport d1", fdbFix),
				bc(brPos, "fdb", "fdb show dev d2", fdbFix),
				bc(brPos, "fdb", "fdb show br br0 dev d1", fdbFix),
				bc(brPos, "fdb", "fdb show vlan 10", fdbFix),
				bc(brPos, "fdb", "fdb show state permanent", fdbFix),
				bc(brPos, "fdb", "fdb show state static", fdbFix),
				bc(brPos, "fdb", "fdb show state reachable", fdbFix),
				bc(brCorner, "fdb", "fdb show state all", fdbFix),
				bc(brBoundary, "fdb", "fdb show state 0", fdbFix),      // no filter (F5)
				bc(brCorner, "fdb", "fdb show state s", fdbFix),        // a prefix of static (F5)
				bc(brCorner, "fdb", "fdb show dynamic", fdbFix),        // static entries kept (F4)
				bc(brCorner, "fdb", "fdb show dev d1 dev d2", fdbFix),  // the second wins (F1)
				bc(brCorner, "fdb", "fdb show vlan 0 vlan 10", fdbFix), // vlan 0 escapes duparg
				bc(brCorner, "fdb", "fdb show bogus", fdbFix),          // unknown words ignored
				bc(brCorner, "fdb", "-o fdb show", fdbFix),             // -o ignored (F11)
				bc(brCorner, "fdb", "fdb sh", fdbFix),                  // matches(): a prefix is show
				bc(brPos, "fdb", "fdb get "+fdbA1+" br br0 vlan 10", fdbFix),
				bc(brPos, "fdb", "fdb get 02:00:00:00:0a:02 dev d1", fdbFix),
				bc(brCorner, "fdb", "fdb g "+fdbA1+" br br0 vlan 10", fdbFix),      // get by prefix
				bc(brCorner, "fdb", "fdb get to "+fdbA1+" br br0 vlan 10", fdbFix), // optional to
				bc(brCorner, "fdb", "-s -d fdb get 02:00:00:00:0a:06 br br0", fdbFix),
				// dynamic also filters a get: a permanent entry prints nothing, rc 0.
				bc(brCorner, "fdb", "fdb get 02:00:00:00:0a:02 dev d1 dynamic", fdbFix),
				// vxlan: dst, port, vni, src_vni, via and nhid.
				bc(brPos, "fdbvx", "fdb show dev vx0", fdbVxFix),
				bc(brPos, "fdbvx", "-d fdb show dev vx0", fdbVxFix),
				bc(brPos, "fdbvx", "-s fdb show dev vx0", fdbVxFix),
				bc(brPos, "fdbvx", "fdb show", fdbVxFix),
				// Batch: the filter statics outlive their line (F2, F3).
				bb(brCorner, "fdb", "sticky-dev", "", false, batchFile("fdb show dev d1", "fdb show"), fdbFix),
				bb(brCorner, "fdb", "sticky-br", "", false, batchFile("fdb show br br0", "fdb show"), fdbFix),
				bb(brCorner, "fdb", "state-or", "", false,
					batchFile("fdb show state permanent", "fdb show state static"), fdbFix),
				bb(brCorner, "fdb", "get-dynamic-leak", "", false,
					batchFile("fdb get "+fdbA1+" br br0 vlan 10 dynamic", "fdb show"), fdbFix),
				bb(brCorner, "fdb", "sticky-vlan-get", "", false,
					batchFile("fdb show vlan 10", "fdb get 02:00:00:00:0a:02 dev d1"), fdbFix),
			}},
		},
		Bad: []badValue{
			brBad("bare", "fdb badcmd", nil, ack, "an unknown fdb sub-command, after ll_init_map"),
			brBad("bare", "fdb help", nil, ack, "fdb help prints the usage and exits 255"),
			brBad("bare", "fdb show h", nil, ack, "any prefix of help in fdb show's words prints the usage"),
			brBad("bare", "fdb show dev nosuch", nil, expBridgeENODEV, "no such device"),
			brBad("bare", "fdb show br nosuch", nil, expBridgeENODEV, "no such bridge: Cannot find bridge device"),
			brBad("bare", "fdb show dev", nil, ack, "dev with no value: the command is incomplete"),
			brBad("bare", "fdb show state bogus", nil, ack, "invarg: an invalid state"),
			brBad("fdb", "fdb show vlan 10 vlan 20", fdbFix, ack, "vlan given twice"),
			brBad("fdb", "fdb get 02:00:00:00:0f:0f br br0", fdbFix, get("ENOENT", "Fdb entry not found"),
				"no such entry: rtnl_talk fails and fdb_get returns -2"),
			brBad("fdb", "fdb get "+fdbA1+" br br0 dev d1", fdbFix,
				get("EINVAL", "Master and device are mutually exclusive"),
				"the usage shows br with dev, the kernel refuses the pair (K4)"),
			brBad("fdb", "fdb get "+fdbA1+" br br0 vlan 0", fdbFix, get("EINVAL", "invalid vlan id"),
				"vlan 0 is sent and refused"),
			brBad("fdb", "fdb get "+fdbA1+" br br0 vlan 65535", fdbFix, get("ENOENT", "Fdb entry not found"),
				"vlan 65535 is the short -1, unset: no NDA_VLAN, vid 0 is looked up (F13)"),
			brBad("fdb", "fdb get "+fdbA1+" br br0 vlan 70000", fdbFix, get("EINVAL", "invalid vlan id"),
				"vlan 70000 wraps to 4464 in a short (F13)"),
			brBad("fdb", "fdb get 02:00:00:00:0a:07 dev d1 self", fdbFix,
				get("ENOTSUP", "Fdb get operation not supported by device"),
				"a dummy device has no fdb get of its own"),
			brBad("fdb", "fdb get zz br br0", fdbFix, ack, "Invalid mac address"),
			brBad("fdb", "fdb get br br0", fdbFix, ack, "Device or master and address are required arguments"),
			brBad("fdb", "fdb get "+fdbA1+" 02:00:00:00:0a:02 br br0", fdbFix, ack,
				"a second address: duparg2"),
			brBad("fdb", "fdb get "+fdbA1+" br br0 vni 16777216", fdbFix, ack,
				"invarg: a VNI over 24 bits; the message ends in two newlines (F13)"),
			brBad("fdb", "fdb get "+fdbA1+" dev nosuch", fdbFix, expBridgeENODEV, "no such device"),
			bbBad("fdb", "sticky-vlan-duparg", "-force", false, batchFile("fdb show vlan 10", "fdb show vlan 10"), fdbFix,
				ack, "the first vlan is still set, so the second line is a duparg, which exits 255 through -force (F2)"),
		},
	}
	sp.Build = bridgeSpace().Build
	sp.NegBase = func(vals []value) bool {
		return pickOf(sp.Axes, vals)["opt"] == "plain"
	}
	return sp
}
