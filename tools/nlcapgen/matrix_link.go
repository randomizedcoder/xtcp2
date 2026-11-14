package main

import "strings"

// The ip link set space. Each row runs in a fresh namespace holding one
// dummy device, d0, down, and sets its attributes. iproute2's side is
// ip/iplink.c iplink_parse and iplink_modify; the kernel's is
// net/core/rtnetlink.c validate_linkmsg and do_setlink, with ifla_policy
// bounding IFLA_TXQLEN and IFLA_IFALIAS. A dummy has no MTU limits
// (drivers/net/dummy.c sets min_mtu and max_mtu to 0).

const linkSetup = "ip link add d0 type dummy && ip link set d0 addrgenmode none"

// linkAlias255 is an alias of IFALIASZ - 1 characters, the longest
// iplink_parse and ifla_policy take.
var linkAlias255 = strings.Repeat("a", 255)

func linkSetSpace() *space {
	sp := &space{
		Name: "linkset",
		Axes: []axis{
			{Name: "updown", Values: []value{
				v("absent", "", nil),
				v("up", "up", f("up", "1")),
				cv("down", "down", f("up", "0")),
			}},
			{Name: "mtu", Values: []value{
				v("absent", "", nil),
				v("1400", "mtu 1400", f("mtu", "1400")),
				bv("0", "mtu 0", f("mtu", "0")),
				bv("65535", "mtu 65535", f("mtu", "65535")),
			}},
			{Name: "txqlen", Values: []value{
				v("absent", "", nil),
				v("500", "txqueuelen 500", f("txqlen", "500")),
				bv("0", "txqlen 0", f("txqlen", "0")),
				bv("32767", "qlen 32767", f("txqlen", "32767")),
				cv("abbrev", "txq 100", f("txqlen", "100")),
			}},
			{Name: "group", Values: []value{
				v("absent", "", nil),
				v("5", "group 5", f("group", "5")),
				cv("default", "group default", f("group", "0")),
				bv("max", "group 2147483647", f("group", "2147483647")),
			}},
			{Name: "alias", Values: []value{
				v("absent", "", nil),
				v("word", "alias hello", f("alias", "hello")),
				bv("255", "alias "+linkAlias255, f("alias", linkAlias255)),
			}},
			{Name: "address", Values: []value{
				v("absent", "", nil),
				v("mac", "address 02:00:00:00:00:aa", f("address", "02:00:00:00:00:aa")),
				cv("abbrev", "addr 02:00:00:00:00:bb", f("address", "02:00:00:00:00:bb")),
				cv("broadcast", "broadcast ff:ff:ff:ff:ff:fe", f("broadcast", "ff:ff:ff:ff:ff:fe")),
			}},
			// Each sets one IFF_* bit in ifi_change, and in ifi_flags when
			// on (arp off and trailers off set IFF_NOARP and IFF_NOTRAILERS).
			{Name: "flag", Values: []value{
				v("absent", "", nil),
				v("multicast", "multicast on", f("flag", "4096", "flag_on", "1")),
				cv("multicast-off", "multicast off", f("flag", "4096", "flag_on", "0")),
				v("promisc", "promisc on", f("flag", "256", "flag_on", "1")),
				v("allmulti", "allmulticast on", f("flag", "512", "flag_on", "1")),
				v("noarp", "arp off", f("flag", "128", "flag_on", "1")),
				cv("dynamic", "dynamic on", f("flag", "32768", "flag_on", "1")),
				cv("notrailers", "trailers off", f("flag", "32", "flag_on", "1")),
			}},
			{Name: "gso", Values: []value{
				v("absent", "", nil),
				v("gso_size", "gso_max_size 32768", f("gso_max_size", "32768")),
				v("gso_segs", "gso_max_segs 100", f("gso_max_segs", "100")),
				v("gro_size", "gro_max_size 32768", f("gro_max_size", "32768")),
				bv("gso_segs_max", "gso_max_segs 65535", f("gso_max_segs", "65535")),
				bv("gro_size_max", "gro_max_size 524280", f("gro_max_size", "524280")),
				cv("gso_v4", "gso_ipv4_max_size 32768", f("gso_ipv4_max_size", "32768")),
				cv("gro_v4", "gro_ipv4_max_size 32768", f("gro_ipv4_max_size", "32768")),
			}},
			{Name: "mode", Values: []value{
				v("absent", "", nil),
				cv("dormant", "mode dormant", f("linkmode", "1")),
				cv("default", "mode default", f("linkmode", "0")),
				cv("state", "state dormant", f("operstate", "5")),
			}},
			{Name: "name", Values: []value{
				v("absent", "", nil),
				cv("rename", "name d9", f("name", "d9")),
			}},
		},
		Bad: []badValue{
			{Axis: "mtu", Value: v("-1", "mtu -1", f("mtu", "4294967295")), Expect: *expKernelEINVAL,
				Why: "mtu -1, which get_integer takes and the kernel reads as negative (mtu less than device minimum)"},
			{Axis: "mtu", Value: v("word", "mtu big", nil), Expect: expIproute2Invarg, Why: "an mtu that isn't a number"},
			{Axis: "txqlen", Value: v("32768", "txqlen 32768", f("txqlen", "32768")),
				Expect: expect{RC: intp(2), Errno: "ERANGE", RefusedBy: refKernel},
				Why:    "a txqueuelen past ifla_policy's S16_MAX"},
			{Axis: "group", Value: v("nosuch", "group nosuch", nil), Expect: expIproute2Invarg, Why: "an unknown group name"},
			{Axis: "alias", Value: v("256", "alias "+linkAlias255+"a", nil), Expect: expIproute2Invarg,
				Why: "an alias of IFALIASZ characters (alias too long)"},
			{Axis: "address", Value: v("mcast", "address 01:00:00:00:00:01", f("address", "01:00:00:00:00:01")),
				Expect: expect{RC: intp(2), Errno: "EADDRNOTAVAIL", RefusedBy: refKernel},
				Why:    "a multicast hardware address, which eth_mac_addr refuses"},
			{Axis: "address", Value: v("short", "address 02:00", nil), Expect: expect{RC: intp(1), RefusedBy: refAckThenFail},
				Why: "an address shorter than d0's, which iplink_parse refuses after looking its length up (Invalid address length)"},
			{Axis: "gso", Value: v("gso_segs_over", "gso_max_segs 65536", nil), Expect: expIproute2Invarg,
				Why: "gso_max_segs past GSO_MAX_SEGS"},
			{Axis: "gso", Value: v("gro_size_over", "gro_max_size 524281", f("gro_max_size", "524281")), Expect: *expKernelEINVAL,
				Why: "gro_max_size past GRO_MAX_SIZE (too big gro_max_size)"},
			{Axis: "name", Value: v("long", "name abcdefghijklmnop", nil), Expect: expIproute2Invarg,
				Why: "a name of IFNAMSIZ characters"},
		},
	}
	sp.Build = buildLinkSet
	sp.NegBase = func(vals []value) bool { return vals[len(vals)-1].Name == "absent" }
	return sp
}

// buildLinkSet assembles `ip link set dev d0 ...`. The readback dumps
// every link, so it finds d0 after a rename.
func buildLinkSet(vals []value) row {
	args := make([]string, 0, len(vals))
	for _, val := range vals {
		args = append(args, val.Arg)
	}
	r := row{
		Cmd:      strings.TrimSpace("ip link set dev d0 " + joinArgs(args...)),
		Pre:      []preStep{{Cmd: linkSetup}},
		Readback: []readback{{Name: "dump", Cmd: "ip -d link show"}},
		Expect:   expect{RC: intp(0), RefusedBy: refNone},
		Axes:     mergeFacts(vals),
	}
	return r
}
