package main

// The ip address spaces. Each row runs in a fresh namespace holding one
// dummy device, d0, up with no address, so the address under test is the
// only one on it. iproute2's side is ip/ipaddress.c ipaddr_modify; the
// kernel's is net/ipv4/devinet.c inet_validate_rtm, inet_rtm_newaddr and
// inet_rtm_deladdr (IPv4), and net/ipv6/addrconf.c inet6_rtm_newaddr,
// inet6_addr_add, ipv6_add_addr and inet6_rtm_deladdr (IPv6).

const addrSetup = "ip link add d0 type dummy && ip link set d0 addrgenmode none && ip link set d0 up"

// addrVerbs: add asks NLM_F_CREATE|EXCL, replace CREATE|REPLACE and change
// REPLACE alone; change and del first add what they work on.
var addrVerbs = axis{Name: "verb", Values: []value{
	v("add", "add", f("verb", "add")),
	v("replace", "replace", f("verb", "replace")),
	v("change", "change", f("verb", "change")),
	v("del", "del", f("verb", "del")),
}}

// addrMetrics: metric, priority and preference all send IFA_RT_PRIORITY.
func addrMetrics() axis {
	return axis{Name: "metric", Values: []value{
		v("absent", "", nil),
		v("100", "metric 100", f("metric", "100")),
		bv("0", "metric 0", f("metric", "0")),
		bv("max", "metric 4294967295", f("metric", "4294967295")),
		cv("priority", "priority 5", f("metric", "5")),
		cv("preference", "preference 7", f("metric", "7")),
	}}
}

// addrLifetimes: naming either lifetime sends IFA_CACHEINFO, the other
// left at INFINITY_LIFE_TIME.
func addrLifetimes() axis {
	return axis{Name: "lft", Values: []value{
		v("absent", "", nil),
		v("finite", "valid_lft 100 preferred_lft 50", f("valid", "100", "preferred", "50")),
		cv("forever", "valid_lft forever preferred_lft forever", f("valid", "4294967295", "preferred", "4294967295")),
		bv("deprecated", "preferred_lft 0", f("valid", "4294967295", "preferred", "0")),
		bv("equal", "valid_lft 60 preferred_lft 60", f("valid", "60", "preferred", "60")),
	}}
}

// addrProtos: IFA_PROTO, named from lib/rt_names.c's addrprot table or as
// a number.
func addrProtos() axis {
	return axis{Name: "proto", Values: []value{
		v("absent", "", nil),
		v("kernel_ra", "proto kernel_ra", f("proto", "2")),
		v("99", "proto 99", f("proto", "99")),
		bv("unspec", "proto unspec", f("proto", "0")),
		bv("255", "proto 255", f("proto", "255")),
	}}
}

// expAddrFailAfterDev is ipaddr_modify's return -1 after ll_name_to_index
// has looked d0 up (an acknowledged RTM_GETLINK); do_cmd negates it, so ip
// exits 1, where invarg() exits 255.
var expAddrFailAfterDev = expect{RC: intp(1), RefusedBy: "ack-then-fail"}

// addrBad holds the bad values both families share. invarg() exits before
// the device lookup; the lifetime checks come after it.
func addrBad() []badValue {
	return []badValue{
		{Axis: "metric", Value: v("2^32", "metric 4294967296", nil), Expect: expIproute2Invarg, Why: "a metric past u32"},
		{Axis: "proto", Value: v("256", "proto 256", nil), Expect: expIproute2Invarg, Why: "a protocol past u8"},
		{Axis: "proto", Value: v("nosuch", "proto nosuch", nil), Expect: expIproute2Invarg, Why: "an unknown protocol name"},
		{Axis: "lft", Value: v("valid-0", "valid_lft 0", nil), Expect: expAddrFailAfterDev,
			Why: "a zero valid lifetime (valid_lft is zero)"},
		{Axis: "lft", Value: v("pref-over", "valid_lft 10 preferred_lft 20", nil), Expect: expAddrFailAfterDev,
			Why: "a preferred lifetime past the valid one (preferred_lft is greater than valid_lft)"},
		{Axis: "lft", Value: v("valid-2^32", "valid_lft 4294967296", nil), Expect: expIproute2Invarg,
			Why: "a lifetime past u32 (invalid valid_lft value)"},
	}
}

func addr4Space() *space {
	sp := &space{
		Name: "addr4",
		Axes: []axis{
			addrVerbs,
			{Name: "local", Values: []value{
				v("24", "192.0.2.1/24", f("local", "192.0.2.1", "prefixlen", "24")),
				bv("32", "192.0.2.1/32", f("local", "192.0.2.1", "prefixlen", "32")),
				bv("31", "192.0.2.0/31", f("local", "192.0.2.0", "prefixlen", "31")),
				bv("0", "192.0.2.1/0", f("local", "192.0.2.1", "prefixlen", "0")),
				// No prefix length: get_prefix gives 32, and a delete
				// leaves IFA_ADDRESS out (the wildcard deletion).
				cv("bare", "192.0.2.1", f("local", "192.0.2.1", "prefixlen", "32", "bare", "1")),
				cv("keyword", "local 192.0.2.1/24", f("local", "192.0.2.1", "prefixlen", "24")),
				// default_scope gives 127/8 RT_SCOPE_HOST.
				cv("loopback", "127.0.0.2/8", f("local", "127.0.0.2", "prefixlen", "8")),
				cv("mcast", "239.1.1.1/32", f("local", "239.1.1.1", "prefixlen", "32")),
				// inet_rtm_newaddr returns 0 for a zero IFA_LOCAL and
				// stores nothing.
				cv("zero", "0.0.0.0/24", f("local", "0.0.0.0", "prefixlen", "24", "noop", "1")),
			}},
			{Name: "peer", Values: []value{
				v("absent", "", nil),
				v("32", "peer 198.51.100.1/32", f("peer", "198.51.100.1", "peer_len", "32")),
				cv("remote", "remote 198.51.100.0/24", f("peer", "198.51.100.0", "peer_len", "24")),
			}},
			{Name: "brd", Values: []value{
				v("absent", "", nil),
				v("addr", "broadcast 192.0.2.255", f("brd", "192.0.2.255")),
				v("plus", "brd +", f("brd", "+")),
				cv("minus", "brd -", f("brd", "-")),
				cv("abbrev", "bro 192.0.2.127", f("brd", "192.0.2.127")),
			}},
			{Name: "label", Values: []value{
				v("absent", "", nil),
				v("alias", "label d0:1", f("label", "d0:1")),
				bv("15", "label d0:abcdefghijkl", f("label", "d0:abcdefghijkl")),
				cv("other", "label foo", f("label", "foo")),
			}},
			{Name: "scope", Values: []value{
				v("absent", "", nil),
				v("link", "scope link", f("scope", "253")),
				v("host", "scope host", f("scope", "254")),
				cv("global", "scope global", f("scope", "0")),
				bv("255", "scope 255", f("scope", "255")),
			}},
			addrMetrics(),
			addrLifetimes(),
			{Name: "flags", Values: []value{
				v("absent", "", nil),
				v("noprefixroute", "noprefixroute", f("flags", "512")),
				cv("autojoin", "autojoin", f("flags", "1024")),
				// IPv6-only flags draw a warning and are left out.
				cv("nodad", "nodad", nil),
			}},
			addrProtos(),
		},
		// ip_mc_autojoin_config joins ifa_address, which a peer replaces,
		// and __ip_mc_join_group refuses a unicast group; Derive writes
		// the negative row.
		Excl: []exclusion{
			{A: "peer=32", B: "flags=autojoin"},
			{A: "peer=remote", B: "flags=autojoin"},
		},
		Bad: append(addrBad(),
			badValue{Axis: "local", Value: v("33", "192.0.2.1/33", nil), Expect: expect{RC: intp(1), RefusedBy: "iproute2"},
				Why: "a prefix length past 32 (get_prefix exits 1)"},
			badValue{Axis: "label", Value: v("16", "label d0:abcdefghijklm", f("label", "d0:abcdefghijklm")),
				Expect: expect{RC: intp(2), Errno: "ERANGE", RefusedBy: "kernel"},
				Why:    "a label of IFNAMSIZ characters, past ifa_ipv4_policy's IFNAMSIZ - 1"},
			badValue{Axis: "scope", Value: v("nosuch", "scope nosuch", nil), Expect: expIproute2Invarg, Why: "an unknown scope"},
		),
	}
	sp.Build = func(vals []value) row { return buildAddr("4", sp.Axes, vals, addr4Judge) }
	sp.Judge = addr4Judge
	sp.NegBase = addrNegBase
	sp.Derive = func(pos []row) []row {
		r := sp.Build(sp.assign(map[string]string{"local": "mcast", "peer": "32", "flags": "autojoin"}))
		r.ID = "addr4/mcpeer-1"
		r.Desc = "negative: autojoin on a multicast address with a peer, which IPv4 joins as the group (Multicast auto join failed)"
		r.Expect = *expKernelEINVAL
		return append(deriveDupGone("addr4", pos, "EADDRNOTAVAIL", addrCanDup), r)
	}
	return sp
}

func addr6Space() *space {
	sp := &space{
		Name: "addr6",
		Axes: []axis{
			addrVerbs,
			{Name: "local", Values: []value{
				v("64", "2001:db8:1::1/64", f("local", "2001:db8:1::1", "prefixlen", "64")),
				bv("128", "2001:db8:1::1/128", f("local", "2001:db8:1::1", "prefixlen", "128")),
				bv("0", "2001:db8:1::1/0", f("local", "2001:db8:1::1", "prefixlen", "0")),
				cv("bare", "2001:db8:1::1", f("local", "2001:db8:1::1", "prefixlen", "128")),
				cv("ll", "fe80::1/64", f("local", "fe80::1", "prefixlen", "64")),
				// ipv6_add_addr takes a multicast address only with
				// IFA_F_MCAUTOJOIN, and never the unspecified one.
				cv("mcast", "ff0e::1:1/128", f("local", "ff0e::1:1", "prefixlen", "128")),
				cv("any", "::/64", f("local", "::", "prefixlen", "64")),
			}},
			{Name: "peer", Values: []value{
				v("absent", "", nil),
				v("128", "peer 2001:db8:9::1/128", f("peer", "2001:db8:9::1", "peer_len", "128")),
				cv("64", "peer 2001:db8:9::/64", f("peer", "2001:db8:9::", "peer_len", "64")),
			}},
			// IPv6 has no broadcast, label or scope: iproute2 sends what it
			// is given and the kernel ignores it.
			{Name: "brd", Values: []value{
				v("absent", "", nil),
				cv("addr", "broadcast 2001:db8:1::ff", f("brd", "2001:db8:1::ff")),
			}},
			{Name: "label", Values: []value{
				v("absent", "", nil),
				cv("alias", "label d0:1", f("label", "d0:1")),
			}},
			{Name: "scope", Values: []value{
				v("absent", "", nil),
				cv("link", "scope link", f("scope", "253")),
			}},
			addrMetrics(),
			addrLifetimes(),
			{Name: "flags", Values: []value{
				v("absent", "", nil),
				v("nodad", "nodad", f("flags", "2")),
				// Dropped while optimistic_dad is off, its default, so
				// nodad optimistic is not the pair the kernel refuses.
				cv("optimistic", "optimistic", f("flags", "4")),
				cv("nodad-optimistic", "nodad optimistic", f("flags", "6")),
				v("home", "home", f("flags", "16")),
				v("mngtmpaddr", "mngtmpaddr", f("flags", "256")),
				v("noprefixroute", "noprefixroute", f("flags", "512")),
				cv("autojoin", "autojoin", f("flags", "1024")),
			}},
			addrProtos(),
		},
		Bad: append(addrBad(),
			badValue{Axis: "local", Value: v("129", "2001:db8:1::1/129", nil), Expect: expect{RC: intp(1), RefusedBy: "iproute2"},
				Why: "a prefix length past 128 (get_prefix exits 1)"},
			badValue{Axis: "brd", Value: v("plus", "brd +", f("brd", "+")), Expect: expect{RC: intp(1), RefusedBy: "iproute2"},
				Why: "a computed broadcast on IPv6 (Broadcast can be set only for IPv4 addresses)"},
		),
	}
	sp.Build = func(vals []value) row { return buildAddr("6", sp.Axes, vals, addr6Judge) }
	sp.Judge = addr6Judge
	sp.NegBase = addrNegBase
	sp.Derive = func(pos []row) []row { return deriveDupGone("addr6", pos, "EADDRNOTAVAIL", addrCanDup) }
	return sp
}

// addrNeedsMcast: ipaddr_modify refuses autojoin on a unicast address
// after its device lookup (autojoin needs multicast address).
func addrNeedsMcast(pick map[string]string) (*expect, string) {
	if pick["flags"] == "autojoin" && pick["local"] != "mcast" {
		e := expAddrFailAfterDev
		return &e, "autojoin on a unicast address (autojoin needs multicast address)"
	}
	return nil, ""
}

// addr4Judge: a delete of 0.0.0.0 finds nothing, since its add stored
// nothing.
func addr4Judge(pick map[string]string) (*expect, string) {
	if exp, why := addrNeedsMcast(pick); exp != nil {
		return exp, why
	}
	if pick["verb"] == "del" && pick["local"] == "zero" {
		return &expect{RC: intp(2), Errno: "EADDRNOTAVAIL", RefusedBy: "kernel"}, "a delete of 0.0.0.0, which its add never stored (Address not found)"
	}
	return nil, ""
}

// addr6Judge: inet6_addr_add refuses mngtmpaddr off a /64 before
// ipv6_add_addr refuses the unspecified address or a multicast one without
// autojoin. A delete of any of them finds nothing, as its add failed.
func addr6Judge(pick map[string]string) (*expect, string) {
	if exp, why := addrNeedsMcast(pick); exp != nil {
		return exp, why
	}
	// The prefix length sent is the peer's when there is one.
	plen64 := pick["local"] == "64" || pick["local"] == "ll" || pick["local"] == "any"
	if pick["peer"] != "absent" {
		plen64 = pick["peer"] == "64"
	}
	badMngtmp := pick["flags"] == "mngtmpaddr" && !plen64
	badAddr := pick["local"] == "any" || (pick["local"] == "mcast" && pick["flags"] != "autojoin")
	switch {
	case (badMngtmp || badAddr) && pick["verb"] == "del":
		return &expect{RC: intp(2), Errno: "EADDRNOTAVAIL", RefusedBy: "kernel"}, "a delete of an address its add could not store"
	case badMngtmp:
		return expKernelEINVAL, "mngtmpaddr off a /64 (address with \"mngtmpaddr\" flag must have a prefix length of 64)"
	case badAddr:
		return &expect{RC: intp(2), Errno: "EADDRNOTAVAIL", RefusedBy: "kernel"}, "the unspecified address, or a multicast one without autojoin (ipv6_add_addr)"
	}
	return nil, ""
}

// addrNegBase: a bad value goes on a plain add of a unicast address.
func addrNegBase(vals []value) bool {
	return vals[0].Name == "add" && (vals[1].Name == "24" || vals[1].Name == "64") && vals[8].Name == "absent"
}

// addrCanDup: adding 0.0.0.0 twice succeeds twice, as neither stores it.
func addrCanDup(r *row) bool { return r.Axes["noop"] == "" }

// buildAddr assembles `ip -F address VERB LOCAL ... dev d0`. change and
// del first add the same address, unless judge says that add is refused.
func buildAddr(fam string, axes []axis, vals []value, judge func(map[string]string) (*expect, string)) row {
	args := make([]string, 0, len(vals))
	for _, val := range vals[1:] {
		args = append(args, val.Arg)
	}
	verb := vals[0]
	tail := joinArgs(args...) + " dev d0"
	head := "ip -" + fam + " address "
	r := row{
		Cmd:      head + verb.Arg + " " + tail,
		Pre:      []preStep{{Cmd: addrSetup}},
		Readback: []readback{{Name: "dump", Cmd: "ip -" + fam + " address show dev d0"}},
		Expect:   expect{RC: intp(0), RefusedBy: "none"},
		Axes:     mergeFacts(vals),
	}
	r.Axes["family"] = fam
	if verb.Name == "change" || verb.Name == "del" {
		pick := pickOf(axes, vals)
		pick["verb"] = "add"
		if exp, _ := judge(pick); exp == nil {
			r.Pre = append(r.Pre, preStep{Cmd: head + "add " + tail})
		}
	}
	return r
}
