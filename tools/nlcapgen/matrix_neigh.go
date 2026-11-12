package main

import "strings"

// The ip neigh spaces. Each row runs in a fresh namespace holding a veth
// pair, d0 and d1, both up, with 192.0.2.10/24 and 2001:db8:1::10/64 on
// d0, so d0 resolves neighbors (a dummy device is NOARP). iproute2's side
// is ip/ipneigh.c ipneigh_modify; the kernel's is neigh_add,
// __neigh_update and neigh_delete in net/core, with arp_constructor
// (net/ipv4/arp.c) and ndisc_constructor (net/ipv6/ndisc.c) giving a
// multicast or broadcast destination NUD_NOARP from the start.

const neighSetup = "ip link add d0 type veth peer name d1 && ip link set d0 addrgenmode none && ip link set d1 addrgenmode none" +
	" && ip link set d0 up && ip link set d1 up" +
	" && ip addr add 192.0.2.10/24 dev d0 && ip -6 addr add 2001:db8:1::10/64 dev d0 nodad"

// neighVerbs: add asks NLM_F_CREATE|EXCL, replace CREATE|REPLACE and
// change REPLACE alone; change and del first add what they work on.
var neighVerbs = axis{Name: "verb", Values: []value{
	v("add", "add", f("verb", "add")),
	v("replace", "replace", f("verb", "replace")),
	v("change", "change", f("verb", "change")),
	v("del", "del", f("verb", "del")),
}}

// neighLLAddrs: "null" sends no NDA_LLADDR.
func neighLLAddrs() axis {
	return axis{Name: "lladdr", Values: []value{
		v("absent", "", nil),
		v("mac", "lladdr 02:00:00:00:00:01", f("lladdr", "02:00:00:00:00:01")),
		cv("abbrev", "lla 02:00:00:00:00:02", f("lladdr", "02:00:00:00:00:02")),
		cv("null", "lladdr null", nil),
	}}
}

// neighStates: ndm_state, NUD_PERMANENT when no nud is named. none,
// incomplete and failed are not NUD_VALID, so __neigh_update sets them
// without wanting a link-layer address.
func neighStates() axis {
	return axis{Name: "nud", Values: []value{
		v("absent", "", nil),
		v("reachable", "nud reachable", f("state", "2")),
		v("stale", "nud stale", f("state", "4")),
		v("noarp", "nud noarp", f("state", "64")),
		cv("perm", "nud perm", f("state", "128")),
		cv("hex", "nud 0x80", f("state", "128")),
		cv("delay", "nud delay", f("state", "8")),
		cv("probe", "nud probe", f("state", "16")),
		cv("none", "nud none", f("state", "0")),
		cv("incomplete", "nud incomplete", f("state", "1")),
		cv("failed", "nud failed", f("state", "32")),
	}}
}

// neighFlags: router, extern_learn and use are ndm_flags; managed and
// extern_valid are NDA_FLAGS_EXT bits, and managed also sets the state to
// NUD_NONE (it comes after nud on the line, so it wins).
func neighFlags() axis {
	return axis{Name: "flags", Values: []value{
		v("absent", "", nil),
		v("router", "router", f("ntf", "128")),
		v("extern_learn", "extern_learn", f("ntf", "16")),
		cv("use", "use", f("ntf", "1")),
		cv("managed", "managed", f("ntf_ext", "1", "state", "0")),
		cv("extern_valid", "extern_valid", f("ntf_ext", "4")),
	}}
}

func neighProtos() axis {
	return axis{Name: "proto", Values: []value{
		v("absent", "", nil),
		v("static", "protocol static", f("protocol", "4")),
		cv("abbrev", "proto 99", f("protocol", "99")),
		bv("0", "protocol 0", f("protocol", "0")),
		bv("255", "protocol 255", f("protocol", "255")),
	}}
}

// neighBad holds the bad values both families share.
func neighBad() []badValue {
	return []badValue{
		{Axis: "lladdr", Value: v("short", "lladdr 02:00", f("lladdr", "02:00")), Expect: *expKernelEINVAL,
			Why: "a link-layer address shorter than the device's (Invalid link address)"},
		{Axis: "lladdr", Value: v("garbage", "lladdr zz", nil), Expect: expect{RC: intp(1), RefusedBy: refIproute2},
			Why: "a link-layer address ll_addr_a2n can't read"},
		{Axis: "nud", Value: v("two-bits", "nud 3", nil), Expect: expIproute2Invarg, Why: "a numeric state of two bits (nud state is bad)"},
		{Axis: "nud", Value: v("nosuch", "nud nosuch", nil), Expect: expIproute2Invarg, Why: "an unknown state"},
		{Axis: "nud", Value: v("256", "nud 256", nil), Expect: expIproute2Invarg, Why: "a numeric state past 8 bits"},
		{Axis: "proto", Value: v("256", "protocol 256", nil), Expect: expIproute2Invarg, Why: "a protocol past u8"},
	}
}

func neigh4Space() *space {
	sp := &space{
		Name: "neigh4",
		Axes: []axis{
			neighVerbs,
			{Name: "dst", Values: []value{
				v("onlink", "192.0.2.1", f("dst", "192.0.2.1")),
				cv("to", "to 192.0.2.2", f("dst", "192.0.2.2")),
				cv("offlink", "198.51.100.1", f("dst", "198.51.100.1")),
				cv("bcast", "192.0.2.255", f("dst", "192.0.2.255")),
				cv("mcast", "224.0.0.5", f("dst", "224.0.0.5")),
				cv("proxy", "proxy 192.0.2.50", f("dst", "192.0.2.50", "proxy", "1")),
			}},
			neighLLAddrs(),
			neighStates(),
			neighFlags(),
			neighProtos(),
		},
		Bad: append(neighBad(),
			badValue{Axis: "dst", Value: v("256", "192.0.2.256", nil), Expect: expect{RC: intp(1), RefusedBy: refIproute2},
				Why: "an address get_addr can't read"},
			badValue{Axis: "dst", Value: v("v6", "2001:db8:1::1", nil), Expect: expect{RC: intp(1), RefusedBy: refIproute2},
				Why: "an IPv6 destination under -4"},
		),
	}
	finishNeigh(sp, "4")
	return sp
}

func neigh6Space() *space {
	sp := &space{
		Name: "neigh6",
		Axes: []axis{
			neighVerbs,
			{Name: "dst", Values: []value{
				v("onlink", "2001:db8:1::1", f("dst", "2001:db8:1::1")),
				cv("to", "to 2001:db8:1::2", f("dst", "2001:db8:1::2")),
				cv("offlink", "2001:db8:9::1", f("dst", "2001:db8:9::1")),
				cv("ll", "fe80::1", f("dst", "fe80::1")),
				cv("mcast", "ff02::1", f("dst", "ff02::1")),
				cv("proxy", "proxy 2001:db8:1::50", f("dst", "2001:db8:1::50", "proxy", "1")),
			}},
			neighLLAddrs(),
			neighStates(),
			neighFlags(),
			neighProtos(),
		},
		Bad: append(neighBad(),
			badValue{Axis: "dst", Value: v("v4", "192.0.2.1", nil), Expect: expect{RC: intp(1), RefusedBy: refIproute2},
				Why: "an IPv4 destination under -6"},
		),
	}
	finishNeigh(sp, "6")
	return sp
}

func finishNeigh(sp *space, fam string) {
	sp.Build = func(vals []value) row { return buildNeigh(fam, sp.Axes, vals) }
	sp.Judge = neighJudge
	// neigh_add checks the link-layer address's length before anything
	// the flags change, so any flag will do.
	sp.NegBase = func(vals []value) bool {
		return vals[0].Name == "add" && vals[1].Name == "onlink" && vals[2].Name == "mac"
	}
	sp.Derive = func(pos []row) []row {
		out := deriveDupGone(sp.Name, pos, "ENOENT", neighCanDup)
		return append(out, deriveNeighChangeGone(sp.Name, pos)...)
	}
}

// neighValid is NUD_VALID: every state but none, incomplete and failed.
func neighValid(nud string) bool {
	return nud != "none" && nud != "incomplete" && nud != "failed"
}

// neighAddVerdict is what neigh_add does with a new entry: refuse
// NTF_EXT_VALIDATED in an invalid state, then, in __neigh_update, a valid
// state with no link-layer address unless the entry already has one (a
// multicast or broadcast destination starts NUD_NOARP). use and managed
// return before that check. managed's NUD_NONE needs no case of its own:
// flags is one axis, so it never meets extern_valid.
func neighAddVerdict(pick map[string]string) (*expect, string) {
	state := pick["nud"]
	switch {
	case pick["flags"] == "extern_valid" && !neighValid(state):
		return expKernelEINVAL, "extern_valid in an invalid state (Cannot create externally validated neighbor with an invalid state)"
	case pick["flags"] == "use" || pick["flags"] == "managed" || !neighValid(state):
		return nil, ""
	case pick["lladdr"] != "mac" && pick["lladdr"] != "abbrev" && pick["dst"] != "bcast" && pick["dst"] != "mcast":
		return expKernelEINVAL, "a valid state with no link-layer address (No link layer address given)"
	}
	return nil, ""
}

// neighJudge: a proxy entry refuses managed and extern_valid and takes
// anything else; a change or delete whose add is refused finds nothing.
func neighJudge(pick map[string]string) (*expect, string) {
	if pick["dst"] == "proxy" {
		if pick["flags"] == "managed" || pick["flags"] == "extern_valid" {
			return expKernelEINVAL, "a proxy entry with managed or extern_valid (Invalid NTF_* flag combination)"
		}
		return nil, ""
	}
	exp, why := neighAddVerdict(pick)
	if exp == nil {
		return nil, ""
	}
	if v := pick["verb"]; v == "change" || v == "del" {
		return &expect{RC: intp(2), Errno: "ENOENT", RefusedBy: refKernel}, "a " + v + " of an entry its add could not create"
	}
	return exp, why
}

// neighCanDup: a proxy entry added twice is pneigh_create finding it, not
// EEXIST.
func neighCanDup(r *row) bool { return r.Axes["proxy"] == "" }

// deriveNeighChangeGone adds up to three changes of an entry never added:
// change has no NLM_F_CREATE, so neigh_add finds nothing (ENOENT). A proxy
// change creates its entry, so none is taken.
func deriveNeighChangeGone(name string, pos []row) []row {
	var out []row
	for i := range pos {
		p := &pos[i]
		if len(out) == 3 || p.Axes["verb"] != "change" || p.Axes["proxy"] != "" || p.Expect.RC == nil || *p.Expect.RC != 0 {
			continue
		}
		r := *p
		r.Pre = append([]preStep{}, p.Pre[:len(p.Pre)-1]...)
		r.ID = name + "/chgone-" + string(rune('1'+len(out)))
		r.Desc = "negative: " + strings.TrimPrefix(dropPrefix(p.Desc), " ") + ": changed but never added (ENOENT)"
		r.Expect = expect{RC: intp(2), Errno: "ENOENT", RefusedBy: refKernel}
		out = append(out, r)
	}
	return out
}

// buildNeigh assembles `ip -F neigh VERB DST ... dev d0`. change and del
// first add the same entry, unless that add is refused. A proxy row reads
// back the proxy table, which the plain dump leaves out.
func buildNeigh(fam string, axes []axis, vals []value) row {
	args := make([]string, 0, len(vals))
	for _, val := range vals[1:] {
		args = append(args, val.Arg)
	}
	verb := vals[0]
	tail := joinArgs(args...) + " dev d0"
	head := "ip -" + fam + " neigh "
	show := "ip -" + fam + " neigh show dev d0"
	r := row{
		Cmd:      head + verb.Arg + " " + tail,
		Pre:      []preStep{{Cmd: neighSetup}},
		Readback: []readback{{Name: "dump", Cmd: show}},
		Expect:   expect{RC: intp(0), RefusedBy: refNone},
		Axes:     mergeFacts(vals),
	}
	r.Axes["family"] = fam
	if _, ok := r.Axes["state"]; !ok {
		r.Axes["state"] = "128"
	}
	if r.Axes["proxy"] != "" {
		r.Readback = []readback{{Name: "dump", Cmd: "ip -" + fam + " neigh show proxy dev d0"}}
	}
	// neighJudge refuses a change or delete exactly when its add is
	// refused.
	if verb.Name == "change" || verb.Name == "del" {
		if exp, _ := neighJudge(pickOf(axes, vals)); exp == nil {
			r.Pre = append(r.Pre, preStep{Cmd: head + "add " + tail})
		}
	}
	return r
}
