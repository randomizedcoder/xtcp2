package main

import "strings"

// The ip rule spaces. Each row runs in routeSetup's namespace. The kernel
// rules the exclusions and bad values cite are in net/core/fib_rules.c
// (fib_nl2rule, fib_nl2rule_locked, rule_exists, rule_find) and the
// families' configure hooks, fib4_rule_configure (net/ipv4/fib_rules.c)
// and fib6_rule_configure (net/ipv6/fib6_rules.c). iproute2's side is
// ip/iprule.c iprule_modify.

var ruleVerbs = axis{Name: "verb", Values: []value{
	v("add", "add", f("verb", "add")),
	v("del", "del", f("verb", "del")),
}}

func rulePrefs() axis {
	return axis{Name: "pref", Values: []value{
		v("absent", "", nil),
		v("100", "pref 100", f("priority", "100")),
		bv("0", "pref 0", f("priority", "0")),
		bv("max", "pref 4294967295", f("priority", "4294967295")),
		cv("order", "order 200", f("priority", "200")),
		cv("priority", "priority 300", f("priority", "300")),
	}}
}

func ruleFwmarks() axis {
	return axis{Name: "fwmark", Values: []value{
		v("absent", "", nil),
		v("16", "fwmark 0x10", f("fwmark", "16")),
		v("mask", "fwmark 0x10/0xff", f("fwmark", "16", "fwmask", "255")),
		bv("0", "fwmark 0", f("fwmark", "0")),
		bv("max", "fwmark 0xffffffff/0xffffffff", f("fwmark", "4294967295", "fwmask", "4294967295")),
	}}
}

func ruleIifs() axis {
	return axis{Name: "iif", Values: []value{
		v("absent", "", nil),
		v("d0", "iif d0", f("iif", "d0")),
		v("lo", "iif lo", f("iif", "lo")),
		cv("nosuch", "iif nosuch", f("iif", "nosuch")),
		cv("dev", "dev d0", f("iif", "d0")),
	}}
}

func ruleOifs() axis {
	return axis{Name: "oif", Values: []value{
		v("absent", "", nil),
		v("d0", "oif d0", f("oif", "d0")),
		cv("nosuch", "oif nosuch", f("oif", "nosuch")),
	}}
}

// dsAxis is the DS field: the header's tos byte, or FRA_DSCP and its mask
// (iprule_dscp_parse leaves the mask out at DSCP_MAX_MASK, 0x3f).
func dsAxis(extra ...value) axis {
	return axis{Name: "ds", Values: append([]value{
		v("absent", "", nil),
		v("tos", "tos 0x10", f("tos", "16")),
		cv("dsfield", "dsfield 0x08", f("tos", "8")),
		v("dscp", "dscp 4", f("dscp", "4")),
		v("dscp-mask", "dscp 32/0x30", f("dscp", "32", "dscp_mask", "48")),
		cv("dscp-fullmask", "dscp 46/0x3f", f("dscp", "46")),
		bv("dscp-0", "dscp 0", f("dscp", "0")),
		bv("dscp-63", "dscp 63", f("dscp", "63")),
	}, extra...)}
}

func ruleIPProtos(icmp value) axis {
	return axis{Name: "ipproto", Values: []value{
		v("absent", "", nil),
		v("tcp", "ipproto tcp", f("ipproto", "6")),
		v("udp", "ipproto udp", f("ipproto", "17")),
		icmp,
		bv("255", "ipproto 255", f("ipproto", "255")),
	}}
}

// rulePorts is a port axis. fib_rule_port_range_valid wants
// 0 < start <= end < 0xffff, so 1 and 65534 are the edges.
func rulePorts(name string) axis {
	return axis{Name: name, Values: []value{
		v("absent", "", nil),
		v("80", name+" 80", f(name, "80-80")),
		v("range", name+" 1000-2000", f(name, "1000-2000")),
		v("mask", name+" 0x50/0xfff0", f(name, "80-80", name+"_mask", "65520")),
		bv("1", name+" 1", f(name, "1-1")),
		bv("65534", name+" 65534", f(name, "65534-65534")),
		bv("full", name+" 1-65534", f(name, "1-65534")),
	}}
}

func ruleUIDs() axis {
	return axis{Name: "uidrange", Values: []value{
		v("absent", "", nil),
		v("1000", "uidrange 1000-2000", f("uidrange", "1000-2000")),
		bv("0", "uidrange 0-0", f("uidrange", "0-0")),
		bv("max", "uidrange 0-4294967294", f("uidrange", "0-4294967294")),
	}}
}

func ruleSuppress() axis {
	return axis{Name: "suppress", Values: []value{
		v("absent", "", nil),
		v("pl0", "suppress_prefixlength 0", f("sup_pl", "0")),
		cv("sup_pl", "sup_pl 24", f("sup_pl", "24")),
		bv("pl-max", "suppress_prefixlength 2147483647", f("sup_pl", "2147483647")),
		v("group", "suppress_ifgroup 5", f("sup_group", "5")),
		cv("group-default", "sup_group default", f("sup_group", "0")),
	}}
}

func ruleProtos() axis {
	return axis{Name: "protocol", Values: []value{
		v("absent", "", nil),
		v("static", "protocol static", f("protocol", "4")),
		bv("255", "protocol 255", f("protocol", "255")),
		cv("kernel", "protocol kernel", f("protocol", "2")),
	}}
}

// ruleActions is the rule's action, last on the line; its facts give the
// action only for a type, as a table leaves the default (FR_ACT_TO_TBL on
// an add, FR_ACT_UNSPEC on a delete). FR_ACT_BLACKHOLE, _UNREACHABLE and
// _PROHIBIT share RTN_*'s numbers, which is what rtnl_rtntype_a2n returns.
// A table below 256 goes in the header, one above in FRA_TABLE with the
// header's table RT_TABLE_UNSPEC.
func ruleActions(extra ...value) axis {
	return axis{Name: "action", Values: append([]value{
		v("absent", "", nil),
		v("main", "lookup main", f("table", "254")),
		v("100", "table 100", f("table", "100")),
		bv("255", "table 255", f("table", "255")),
		bv("256", "table 256", f("table", "256")),
		bv("max", "table 4294967295", f("table", "4294967295")),
		v("goto", "goto 40000", f("action", "2", "goto", "40000")),
		v("nop", "nop", f("action", "3")),
		v("blackhole", "blackhole", f("action", "6")),
		v("unreachable", "unreachable", f("action", "7")),
		cv("type-prohibit", "type prohibit", f("action", "8")),
		v("l3mdev", "l3mdev", f("l3mdev", "1")),
	}, extra...)}
}

func rule4Space() *space {
	sp := &space{
		Name: "rule4",
		Axes: []axis{
			ruleVerbs,
			{Name: "not", Values: []value{v("absent", "", nil), v("not", "not", f("invert", "1"))}},
			{Name: "from", Values: []value{
				v("absent", "", nil),
				v("24", "from 198.51.100.0/24", f("src", "198.51.100.0", "src_len", "24")),
				bv("0", "from 0.0.0.0/0", f("src", "0.0.0.0", "src_len", "0")),
				bv("32", "from 198.51.100.7/32", f("src", "198.51.100.7", "src_len", "32")),
				cv("all", "from all", f("src", "all", "src_len", "0")),
			}},
			{Name: "to", Values: []value{
				v("absent", "", nil),
				v("24", "to 203.0.113.0/24", f("dst", "203.0.113.0", "dst_len", "24")),
				bv("32", "to 203.0.113.9/32", f("dst", "203.0.113.9", "dst_len", "32")),
			}},
			rulePrefs(),
			ruleFwmarks(),
			ruleIifs(),
			ruleOifs(),
			dsAxis(),
			ruleIPProtos(v("icmp", "ipproto icmp", f("ipproto", "1"))),
			rulePorts("sport"),
			rulePorts("dport"),
			ruleUIDs(),
			ruleSuppress(),
			ruleProtos(),
			{Name: "extra", Values: []value{
				v("absent", "", nil),
				v("realms", "realms 10", f("flow", "10")),
				v("tun_id", "tun_id 7", f("tun_id", "7")),
			}},
			// fib4_rule_configure gives an action-to-table rule with no
			// table the first empty one (fib_empty_table).
			ruleActions(cv("table-0", "table 0", f("table", "0"))),
		},
		Bad: append(ruleBad(),
			badValue{Axis: "ds", Value: v("tos-high", "tos 0xb8", f("tos", "184")), Expect: *expKernelEINVAL,
				Why: "a tos past IPTOS_TOS_MASK (Invalid tos)"},
			badValue{Axis: "extra", Value: v("flowlabel", "flowlabel 1", f("flowlabel", "1", "flowlabel_mask", "1048575")),
				Expect: *expKernelEINVAL, Why: "a flow label on an IPv4 rule (Flow label cannot be specified for IPv4 FIB rules)"},
			badValue{Axis: "from", Value: v("33", "from 198.51.100.0/33", nil), Expect: expect{RC: intp(1), RefusedBy: refIproute2},
				Why: "a prefix length past 32 (get_prefix exits 1)"},
		),
	}
	sp.Build = func(vals []value) row { return buildRule("4", sp.Axes, vals) }
	sp.Judge = ruleJudge
	sp.NegBase = routeNegBase
	sp.Derive = func(pos []row) []row { return deriveDupGone("rule4", pos, "ENOENT", ruleHasPref) }
	return sp
}

func rule6Space() *space {
	sp := &space{
		Name: "rule6",
		Axes: []axis{
			ruleVerbs,
			{Name: "not", Values: []value{v("absent", "", nil), v("not", "not", f("invert", "1"))}},
			{Name: "from", Values: []value{
				v("absent", "", nil),
				v("64", "from 2001:db8:2::/64", f("src", "2001:db8:2::", "src_len", "64")),
				bv("0", "from ::/0", f("src", "::", "src_len", "0")),
				bv("128", "from 2001:db8:2::7/128", f("src", "2001:db8:2::7", "src_len", "128")),
				cv("all", "from all", f("src", "all", "src_len", "0")),
			}},
			{Name: "to", Values: []value{
				v("absent", "", nil),
				v("64", "to 2001:db8:3::/64", f("dst", "2001:db8:3::", "dst_len", "64")),
				bv("128", "to 2001:db8:3::9/128", f("dst", "2001:db8:3::9", "dst_len", "128")),
			}},
			rulePrefs(),
			ruleFwmarks(),
			ruleIifs(),
			ruleOifs(),
			// IPv6 takes the whole DSCP in the tos byte.
			dsAxis(bv("tos-ef", "tos 0xb8", f("tos", "184"))),
			ruleIPProtos(v("icmp", "ipproto ipv6-icmp", f("ipproto", "58"))),
			rulePorts("sport"),
			rulePorts("dport"),
			ruleUIDs(),
			ruleSuppress(),
			ruleProtos(),
			{Name: "extra", Values: []value{
				v("absent", "", nil),
				v("flowlabel", "flowlabel 0x12345", f("flowlabel", "74565", "flowlabel_mask", "1048575")),
				v("flowlabel-mask", "flowlabel 0x1/0x1", f("flowlabel", "1", "flowlabel_mask", "1")),
				v("tun_id", "tun_id 7", f("tun_id", "7")),
			}},
			ruleActions(),
		},
		Bad: append(ruleBad(),
			badValue{Axis: "action", Value: v("table-0", "table 0", f("table", "0")), Expect: *expKernelEINVAL,
				Why: "an IPv6 action-to-table rule with no table (Invalid table)"},
			badValue{Axis: "extra", Value: v("flowlabel-mismatch", "flowlabel 0x2/0x1", f("flowlabel", "2", "flowlabel_mask", "1")), Expect: *expKernelEINVAL,
				Why: "flow label bits outside its mask (Flow label and mask do not match)"},
			badValue{Axis: "extra", Value: v("flowlabel-wide", "flowlabel 0x100000", f("flowlabel", "1048576", "flowlabel_mask", "1048575")), Expect: *expKernelEINVAL,
				Why: "a flow label past 20 bits under the default mask (Flow label and mask do not match)"},
			badValue{Axis: "from", Value: v("129", "from 2001:db8:2::/129", nil), Expect: expect{RC: intp(1), RefusedBy: refIproute2},
				Why: "a prefix length past 128 (get_prefix exits 1)"},
		),
	}
	sp.Build = func(vals []value) row { return buildRule("6", sp.Axes, vals) }
	sp.Judge = ruleJudge
	sp.NegBase = routeNegBase
	sp.Derive = func(pos []row) []row { return deriveDupGone("rule6", pos, "ENOENT", ruleHasPref) }
	return sp
}

// ruleBad holds the bad values both families share.
func ruleBad() []badValue {
	return []badValue{
		{Axis: "pref", Value: v("2^32", "pref 4294967296", nil), Expect: expIproute2Invarg, Why: "a preference past u32"},
		{Axis: "fwmark", Value: v("2^32", "fwmark 4294967296", nil), Expect: expIproute2Invarg, Why: "a fwmark past u32"},
		{Axis: "iif", Value: v("long", "iif abcdefghijklmnop", nil), Expect: expIproute2Invarg,
			Why: "an interface name of IFNAMSIZ characters (check_ifname)"},
		{Axis: "ds", Value: v("ecn", "tos 0x01", f("tos", "1")), Expect: *expKernelEINVAL,
			Why: "a tos with an ECN bit (Invalid dsfield (tos): ECN bits must be 0)"},
		{Axis: "ds", Value: v("dscp-mask-short", "dscp 32/0x10", f("dscp", "32", "dscp_mask", "16")), Expect: *expKernelEINVAL,
			Why: "DSCP bits outside its mask (Invalid DSCP mask)"},
		{Axis: "ds", Value: v("dscp-64", "dscp 64", f("dscp", "64")),
			Expect: expect{RC: intp(2), Errno: "ERANGE", RefusedBy: refKernel},
			Why:    "a DSCP past 6 bits, which iproute2 sends and fib_rule_policy's NLA_POLICY_MAX refuses (Error parsing msg)"},
		{Axis: "sport", Value: v("0", "sport 0", f("sport", "0-0")), Expect: *expKernelEINVAL,
			Why: "port 0 (Invalid sport range)"},
		{Axis: "sport", Value: v("65535", "sport 65535", f("sport", "65535-65535")), Expect: *expKernelEINVAL,
			Why: "port 65535: a range must end below 0xffff (Invalid sport range)"},
		{Axis: "sport", Value: v("backward", "sport 2000-1000", f("sport", "2000-1000")), Expect: *expKernelEINVAL,
			Why: "a range that ends before it starts (Invalid sport range)"},
		{Axis: "sport", Value: v("65536", "sport 65536", nil), Expect: expIproute2Invarg, Why: "a port past u16"},
		{Axis: "dport", Value: v("badmask", "dport 0x51/0xfff0", f("dport", "81-81", "dport_mask", "65520")),
			Expect: *expKernelEINVAL, Why: "port bits outside its mask (Invalid port mask)"},
		{Axis: "uidrange", Value: v("backward", "uidrange 2000-1000", f("uidrange", "2000-1000")), Expect: *expKernelEINVAL,
			Why: "a uid range that ends before it starts (Invalid uid range)"},
		{Axis: "uidrange", Value: v("one", "uidrange 1000", nil), Expect: expIproute2Invarg, Why: "a uid range without its end"},
		{Axis: "suppress", Value: v("pl-neg", "suppress_prefixlength -1", nil), Expect: expIproute2Invarg,
			Why: "a negative suppress_prefixlength"},
		{Axis: "protocol", Value: v("256", "protocol 256", nil), Expect: expIproute2Invarg, Why: "a protocol past u8"},
		{Axis: "action", Value: v("nosuch", "type nosuch", nil), Expect: expIproute2Invarg, Why: "an unknown rule type"},
	}
}

// ruleJudge: iprule_modify refuses an add or delete with no arguments
// before it sends anything; fib_nl2rule_locked refuses a goto at or below
// the rule's own preference (Backward goto not supported), on a delete
// too.
func ruleJudge(pick map[string]string) (*expect, string) {
	bare := true
	for name, val := range pick {
		bare = bare && (name == "verb" || val == "absent")
	}
	if bare {
		return &expect{RC: intp(1), RefusedBy: refIproute2}, "a rule with no arguments (\"ip rule " + pick["verb"] + "\" requires arguments)"
	}
	if pick["action"] == "goto" && pick["pref"] == "max" {
		return expKernelEINVAL, "a goto target below the rule's preference (Backward goto not supported)"
	}
	return nil, ""
}

// ruleHasPref: a rule added without a preference gets one below the
// first rule after local's (fib_default_rule_pref), so adding it again
// makes a second rule rather than meeting the first (rule_exists compares
// preferences).
func ruleHasPref(r *row) bool { return r.Axes["priority"] != "" }

// buildRule assembles `ip -F rule VERB ... ACTION`. A delete first adds
// what it deletes, unless Judge refuses it. iprule_modify gives an add
// with no action the main table; a delete with none sends table 0.
func buildRule(fam string, axes []axis, vals []value) row {
	args := make([]string, 0, len(vals))
	var pre []string
	for _, val := range vals[1:] {
		pre = append(pre, val.Pre...)
		args = append(args, val.Arg)
	}
	verb := vals[0]
	tail := joinArgs(args...)
	head := "ip -" + fam + " rule "
	r := row{
		Cmd:      strings.TrimSpace(head + verb.Arg + " " + tail),
		Pre:      []preStep{{Cmd: routeSetup}},
		Readback: []readback{{Name: "dump", Cmd: "ip -" + fam + " rule show"}},
		Expect:   expect{RC: intp(0), RefusedBy: refNone},
		Axes:     mergeFacts(vals),
	}
	r.Axes["family"] = fam
	_, typed := r.Axes["action"]
	_, tabled := r.Axes["table"]
	if !typed && !tabled && r.Axes["l3mdev"] == "" && verb.Name == "add" {
		r.Axes["table"] = "254"
	}
	for _, p := range pre {
		r.Pre = append(r.Pre, preStep{Cmd: p})
	}
	if verb.Name == "del" {
		if exp, _ := ruleJudge(pickOf(axes, vals)); exp == nil {
			r.Pre = append(r.Pre, preStep{Cmd: head + "add " + tail})
		}
	}
	return r
}
