package main

import "strings"

// The v730 family: the forms iproute2 7.3.0 changed (goip-parity-next.md,
// NC3-T02), each run at 7.2.0 and at 7.3.0 on one kernel. Every row is
// named, and both versions' rows files carry the same ids with the
// expectation of their own version, so the two captures pair up row by
// row (nlcapgen verdiff). The changes:
//   - 30365d09 bond lacp_strict (IFLA_BOND_LACP_STRICT);
//   - ad2b2d95 nexthop fdb dst_port (NHA_DST_PORT, be16);
//   - 144c45b6 seg6 encap lookup (SEG6_IPTUNNEL_TABLE);
//   - 92f89d0e ovpn mode (IFLA_OVPN_MODE);
//   - b8cdda55 maddress show by RTM_GETMULTICAST rather than procfs;
//   - 5faf18f6 a strict ll_addr_a2n.
// The expectations are the probe batches' (V017).

// v730Row is one form with what each version does with it.
type v730Row struct {
	id, desc, cmd string
	pre           []string
	readback      []readback
	modules       []string
	at720, at730  expect
	axes          map[string]string
}

var (
	v730OK       = expect{RC: intp(0), RefusedBy: refNone}
	v730IPRc1    = expect{RC: intp(1), RefusedBy: refIproute2}
	v730IPInvarg = expect{RC: intp(255), RefusedBy: refIproute2}
	// v730AckRc1 and v730AckInvarg are a refusal after ip looked the
	// device up, an acknowledged request.
	v730AckRc1    = expect{RC: intp(1), RefusedBy: refAckThenFail}
	v730AckInvarg = expect{RC: intp(255), RefusedBy: refAckThenFail}
)

// v730LinkSetup is a dummy, up, with addresses in both families.
const v730LinkSetup = "ip link add d0 type dummy && ip link set d0 addrgenmode none && ip link set d0 up"

const v730RouteSetup = v730LinkSetup + " && ip addr add 192.0.2.10/24 dev d0 && ip -6 addr add 2001:db8:1::10/64 dev d0 nodad"

// v730MaddrSetup joins a link group, an IPv4 group and an IPv6 one.
const v730MaddrSetup = v730LinkSetup + " && ip maddr add 33:33:00:00:00:99 dev d0" +
	" && ip addr add 239.1.1.1/32 dev d0 autojoin && ip -6 addr add ff05::1234/128 dev d0 autojoin"

const v730MacvlanSetup = v730LinkSetup + " && ip link add m0 link d0 type macvlan mode source"

func v730Rows() []v730Row {
	bondRB := []readback{{Name: "d", Cmd: "ip -d link show dev b0"}, {Name: "dj", Cmd: "ip -d -j link show dev b0"}}
	nhRB := []readback{{Name: "show", Cmd: "ip nexthop show"}, {Name: "showj", Cmd: "ip -j nexthop show"}}
	seg6RB := []readback{{Name: "show", Cmd: "ip -6 route show"}, {Name: "showj", Cmd: "ip -j -6 route show"}}
	ovpnRB := []readback{{Name: "d", Cmd: "ip -d link show dev o0"}, {Name: "dj", Cmd: "ip -d -j link show dev o0"}}
	linkRB := []readback{{Name: "d", Cmd: "ip link show dev d0"}}
	mvRB := []readback{{Name: "d", Cmd: "ip -d link show dev m0"}}
	bond := []string{"bonding"}
	ovpn := []string{"ovpn"}
	seg6 := "ip -6 route add 2001:db8:2::/64 encap seg6 mode encap segs 2001:db8:9::1 "
	return []v730Row{
		// bond: 7.2.0 doesn't know the word (bond: unknown command).
		{id: "bond/plain", desc: "a bond with no lacp_strict, which 7.3.0's -d prints off", cmd: "ip link add b0 type bond mode 802.3ad",
			pre: []string{v730LinkSetup}, readback: bondRB, modules: bond, at720: v730OK, at730: v730OK, axes: f("kind", "bond")},
		{id: "bond/on", desc: "lacp_strict on", cmd: "ip link add b0 type bond mode 802.3ad lacp_strict on",
			pre: []string{v730LinkSetup}, readback: bondRB, modules: bond, at720: v730IPRc1, at730: v730OK, axes: f("kind", "bond", "lacp_strict", "1")},
		{id: "bond/off", desc: "lacp_strict off", cmd: "ip link add b0 type bond mode 802.3ad lacp_strict off",
			pre: []string{v730LinkSetup}, readback: bondRB, modules: bond, at720: v730IPRc1, at730: v730OK, axes: f("kind", "bond", "lacp_strict", "0")},
		{id: "bond/set", desc: "lacp_strict on an existing bond", cmd: "ip link set dev b0 type bond lacp_strict on",
			pre: []string{v730LinkSetup, "ip link add b0 type bond mode 802.3ad"}, readback: bondRB, modules: bond,
			at720: v730AckRc1, at730: v730OK, axes: f("kind", "bond", "lacp_strict", "1")},
		{id: "bond/x", desc: "lacp_strict neither on nor off", cmd: "ip link add b0 type bond mode 802.3ad lacp_strict x",
			pre: []string{v730LinkSetup}, readback: bondRB, modules: bond, at720: v730IPRc1, at730: v730IPRc1},
		{id: "bond/active-backup", desc: "lacp_strict outside 802.3ad (option not supported in mode)",
			cmd: "ip link add b0 type bond mode active-backup lacp_strict on", pre: []string{v730LinkSetup}, readback: bondRB, modules: bond,
			at720: v730IPRc1, at730: expect{RC: intp(2), Errno: "EACCES", RefusedBy: refKernel}, axes: f("kind", "bond", "lacp_strict", "1")},

		// nexthop dst_port: 7.2.0 refuses the word (invarg).
		{id: "nh/fdb", desc: "an fdb nexthop with no port, whose show 7.3.0 ends in a space", cmd: "ip nexthop add id 1 via 192.0.2.2 fdb",
			pre: []string{v730RouteSetup}, readback: nhRB, at720: v730OK, at730: v730OK, axes: f("fdb", "1")},
		{id: "nh/4789", desc: "dst_port 4789", cmd: "ip nexthop add id 1 via 192.0.2.2 fdb dst_port 4789",
			pre: []string{v730RouteSetup}, readback: nhRB, at720: v730IPInvarg, at730: v730OK, axes: f("fdb", "1", "dst_port", "4789")},
		{id: "nh/65535", desc: "the largest dst_port", cmd: "ip nexthop add id 1 via 192.0.2.2 fdb dst_port 65535",
			pre: []string{v730RouteSetup}, readback: nhRB, at720: v730IPInvarg, at730: v730OK, axes: f("fdb", "1", "dst_port", "65535")},
		{id: "nh/0", desc: "dst_port 0, under the policy's minimum (integer out of range)", cmd: "ip nexthop add id 1 via 192.0.2.2 fdb dst_port 0",
			pre: []string{v730RouteSetup}, readback: nhRB, at720: v730IPInvarg, at730: expect{RC: intp(2), Errno: "ERANGE", RefusedBy: refKernel},
			axes: f("fdb", "1", "dst_port", "0")},
		{id: "nh/65536", desc: "a dst_port past 16 bits", cmd: "ip nexthop add id 1 via 192.0.2.2 fdb dst_port 65536",
			pre: []string{v730RouteSetup}, readback: nhRB, at720: v730IPInvarg, at730: v730IPInvarg},
		{id: "nh/x", desc: "a dst_port that isn't a number", cmd: "ip nexthop add id 1 via 192.0.2.2 fdb dst_port x",
			pre: []string{v730RouteSetup}, readback: nhRB, at720: v730IPInvarg, at730: v730IPInvarg},
		{id: "nh/inet6", desc: "dst_port on an IPv6 fdb nexthop", cmd: "ip -6 nexthop add id 1 via 2001:db8:1::2 fdb dst_port 4789",
			pre: []string{v730RouteSetup}, readback: nhRB, at720: v730IPInvarg, at730: v730OK, axes: f("fdb", "1", "dst_port", "4789")},
		{id: "nh/nofdb", desc: "dst_port without fdb (Destination port can only be set on fdb nexthops that have a gateway)",
			cmd: "ip nexthop add id 1 via 192.0.2.2 dev d0 dst_port 4789", pre: []string{v730RouteSetup}, readback: nhRB,
			at720: v730AckInvarg, at730: *expKernelEINVAL, axes: f("dst_port", "4789")},
		{id: "nh/replace", desc: "a replace that adds a port", cmd: "ip nexthop replace id 1 via 192.0.2.2 fdb dst_port 4790",
			pre: []string{v730RouteSetup, "ip nexthop add id 1 via 192.0.2.2 fdb"}, readback: nhRB,
			at720: v730IPInvarg, at730: v730OK, axes: f("fdb", "1", "dst_port", "4790")},

		// seg6 lookup: 7.2.0 ends the encap at the word and iproute_modify
		// takes it for a second destination (either "to" is duplicate).
		{id: "seg6/main", desc: "lookup main", cmd: seg6 + "lookup main dev d0", pre: []string{v730RouteSetup}, readback: seg6RB,
			at720: v730IPInvarg, at730: v730OK, axes: f("table", "254")},
		{id: "seg6/100", desc: "lookup 100", cmd: seg6 + "lookup 100 dev d0", pre: []string{v730RouteSetup}, readback: seg6RB,
			at720: v730IPInvarg, at730: v730OK, axes: f("table", "100")},
		{id: "seg6/inline", desc: "lookup with mode inline", cmd: "ip -6 route add 2001:db8:2::/64 encap seg6 mode inline segs 2001:db8:9::1 lookup 100 dev d0",
			pre: []string{v730RouteSetup}, readback: seg6RB, at720: v730IPInvarg, at730: v730OK, axes: f("table", "100")},
		{id: "seg6/inet", desc: "lookup on an IPv4 route", cmd: "ip -4 route add 198.51.100.0/24 encap seg6 mode encap segs 2001:db8:9::1 lookup 100 dev d0",
			pre: []string{v730RouteSetup}, readback: seg6RB, at720: v730IPInvarg, at730: v730OK, axes: f("table", "100")},
		{id: "seg6/0", desc: "lookup 0, which ip takes for no table", cmd: seg6 + "lookup 0 dev d0", pre: []string{v730RouteSetup}, readback: seg6RB,
			at720: v730IPInvarg, at730: v730IPInvarg},
		{id: "seg6/nosuch", desc: "a table name ip doesn't know", cmd: seg6 + "lookup nosuch dev d0", pre: []string{v730RouteSetup}, readback: seg6RB,
			at720: v730IPInvarg, at730: v730IPInvarg},

		// ovpn mode: 7.2.0 has no ovpn link_util, so mode is garbage.
		{id: "ovpn/plain", desc: "an ovpn device with no mode", cmd: "ip link add o0 type ovpn", pre: []string{v730LinkSetup}, readback: ovpnRB,
			modules: ovpn, at720: v730OK, at730: v730OK, axes: f("kind", "ovpn")},
		{id: "ovpn/p2p", desc: "mode p2p", cmd: "ip link add o0 type ovpn mode p2p", pre: []string{v730LinkSetup}, readback: ovpnRB,
			modules: ovpn, at720: v730IPRc1, at730: v730OK, axes: f("kind", "ovpn", "ovpn_mode", "0")},
		{id: "ovpn/mp", desc: "mode mp", cmd: "ip link add o0 type ovpn mode mp", pre: []string{v730LinkSetup}, readback: ovpnRB,
			modules: ovpn, at720: v730IPRc1, at730: v730OK, axes: f("kind", "ovpn", "ovpn_mode", "1")},
		{id: "ovpn/x", desc: "a mode neither p2p nor mp", cmd: "ip link add o0 type ovpn mode x", pre: []string{v730LinkSetup}, readback: ovpnRB,
			modules: ovpn, at720: v730IPRc1, at730: v730IPRc1},

		// maddress: the same text from procfs (7.2.0) and from netlink
		// (7.3.0). A device ip can't find is no error in either.
		{id: "maddr/all", desc: "maddress show", cmd: "ip maddress show", pre: []string{v730MaddrSetup}, at720: v730OK, at730: v730OK, axes: f("maddr", "all")},
		{id: "maddr/inet", desc: "maddress show, IPv4", cmd: "ip -4 maddress show", pre: []string{v730MaddrSetup}, at720: v730OK, at730: v730OK, axes: f("maddr", "inet")},
		{id: "maddr/inet6", desc: "maddress show, IPv6", cmd: "ip -6 maddress show", pre: []string{v730MaddrSetup}, at720: v730OK, at730: v730OK, axes: f("maddr", "inet6")},
		{id: "maddr/dev", desc: "maddress show dev d0", cmd: "ip maddress show dev d0", pre: []string{v730MaddrSetup}, at720: v730OK, at730: v730OK, axes: f("maddr", "all")},
		{id: "maddr/nosuch", desc: "maddress show of a device that isn't there, rc 0 at both: 7.3.0 drops its lookup's ENODEV",
			cmd: "ip maddress show dev nosuch", pre: []string{v730MaddrSetup},
			at720: v730OK, at730: expect{RC: intp(0), Errno: "ENODEV", RefusedBy: refKernelIgnored}, axes: f("maddr", "all")},
		{id: "maddr/json", desc: "maddress show as JSON", cmd: "ip -j maddress show", pre: []string{v730MaddrSetup}, at720: v730OK, at730: v730OK, axes: f("maddr", "all")},

		// ll_addr_a2n: 7.3.0 rejects trailing garbage and names the whole
		// argument; 7.2.0 named the part it stopped at.
		{id: "lladdr/ok", desc: "a good address", cmd: "ip link set dev d0 address 02:11:22:33:44:55", pre: []string{v730LinkSetup}, readback: linkRB,
			at720: v730OK, at730: v730OK},
		{id: "lladdr/zz", desc: "a non-hex octet", cmd: "ip link set dev d0 address 00:11:zz", pre: []string{v730LinkSetup}, readback: linkRB,
			at720: v730IPRc1, at730: v730IPRc1},
		{id: "lladdr/0aq", desc: "hex then garbage, which 7.2.0 read as one octet and refused for its length, after the device's lookup",
			cmd: "ip link set dev d0 address 0aq", pre: []string{v730LinkSetup}, readback: linkRB, at720: v730AckRc1, at730: v730IPRc1},
		{id: "lladdr/1ff", desc: "an octet past 255", cmd: "ip link set dev d0 address 1ff", pre: []string{v730LinkSetup}, readback: linkRB,
			at720: v730IPRc1, at730: v730IPRc1},
		{id: "lladdr/seven", desc: "seven octets on a six-octet device", cmd: "ip link set dev d0 address 00:11:22:33:44:55:66",
			pre: []string{v730LinkSetup}, readback: linkRB, at720: v730AckRc1, at730: v730AckRc1},
		{id: "lladdr/trailing", desc: "a trailing colon", cmd: "ip link set dev d0 address 00:11:", pre: []string{v730LinkSetup}, readback: linkRB,
			at720: v730IPRc1, at730: v730IPRc1},
		{id: "lladdr/empty", desc: "an empty argument", cmd: "ip link set dev d0 address ''", pre: []string{v730LinkSetup}, readback: linkRB,
			at720: v730IPRc1, at730: v730IPRc1},
		{id: "lladdr/neigh-zz", desc: "a neighbor's non-hex octet", cmd: "ip neigh add 192.0.2.5 lladdr 00:11:zz dev d0",
			pre: []string{v730RouteSetup}, readback: []readback{{Name: "show", Cmd: "ip neigh show"}}, at720: v730IPRc1, at730: v730IPRc1},
		{id: "lladdr/neigh-seven", desc: "a seven-octet neighbor address, which both versions send", cmd: "ip neigh add 192.0.2.5 lladdr 00:11:22:33:44:55:66 dev d0",
			pre: []string{v730RouteSetup}, readback: []readback{{Name: "show", Cmd: "ip neigh show"}}, at720: v730OK, at730: v730OK},
		{id: "lladdr/macvlan-set", desc: "macaddr set then bcqueuelen, where 7.3.0 also prints that bcqueuelen is an invalid lladdr",
			cmd: "ip link set m0 type macvlan macaddr set 00:11:22:33:44:55 bcqueuelen 10", pre: []string{v730MacvlanSetup}, readback: mvRB,
			at720: v730OK, at730: v730OK},
		{id: "lladdr/macvlan-zz", desc: "a macvlan macaddr add with a non-hex octet", cmd: "ip link set m0 type macvlan macaddr add 00:11:zz",
			pre: []string{v730MacvlanSetup}, readback: mvRB, at720: v730AckRc1, at730: v730AckRc1},
	}
}

// v730Family is the rows at one version, "7.2.0" or "7.3.0".
func v730Family(version string) []row {
	src := v730Rows()
	out := make([]row, 0, len(src))
	for i := range src {
		s := &src[i]
		exp := s.at730
		if version == "7.2.0" {
			exp = s.at720
		}
		pre := make([]preStep, 0, len(s.pre))
		for _, p := range s.pre {
			pre = append(pre, preStep{Cmd: p})
		}
		kind := "positive:"
		if *exp.RC != 0 {
			kind = "negative:"
		}
		space, name, _ := strings.Cut(s.id, "/")
		out = append(out, row{
			ID: "v730" + space + "/" + name, Desc: kind + " " + s.desc, Cmd: s.cmd,
			Pre: pre, Readback: s.readback, Modules: s.modules, Expect: exp, Axes: s.axes,
		})
	}
	return out
}
