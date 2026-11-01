package main

import (
	"slices"
	"strings"
)

// The ip route spaces. Each row runs in a fresh namespace holding one
// dummy device, d0, with 192.0.2.10/24 and 2001:db8:1::10/64 on it, so a
// gateway in 192.0.2.0/24 or 2001:db8:1::/64 is on-link. The kernel rules
// the exclusions cite are in net/ipv4/fib_semantics.c fib_create_info and
// net/ipv4/fib_frontend.c rtm_to_fib_config (IPv4) and net/ipv6/route.c
// rtm_to_fib6_config (IPv6).

const routeSetup = "ip link add d0 type dummy && ip link set d0 addrgenmode none && ip link set d0 up" +
	" && ip addr add 192.0.2.10/24 dev d0 && ip -6 addr add 2001:db8:1::10/64 dev d0 nodad"

func intp(n int) *int { return &n }

func f(kv ...string) map[string]string {
	m := make(map[string]string, len(kv)/2)
	for i := 0; i+1 < len(kv); i += 2 {
		m[kv[i]] = kv[i+1]
	}
	return m
}

func v(name, arg string, facts map[string]string) value {
	return value{Name: name, Arg: arg, Facts: facts}
}

func bv(name, arg string, facts map[string]string) value {
	val := v(name, arg, facts)
	val.Boundary = true
	return val
}

func cv(name, arg string, facts map[string]string) value {
	val := v(name, arg, facts)
	val.Corner = true
	return val
}

var (
	// rejectEINVAL is fib_create_info's "Gateway, device and multipath can
	// not be specified for this route type".
	expKernelEINVAL = &expect{RC: intp(2), Errno: "EINVAL", RefusedBy: "kernel"}
	// a device-less nexthop: fib_check_nh_nongw finds no in_device.
	expKernelENODEV = &expect{RC: intp(2), Errno: "ENODEV", RefusedBy: "kernel"}
	// a delete that matches no route: fib_table_delete, ip6_route_del.
	expKernelESRCH = &expect{RC: intp(2), Errno: "ESRCH", RefusedBy: "kernel"}
	// invarg() exits -1 before anything is sent.
	expIproute2Invarg = expect{RC: intp(255), RefusedBy: "iproute2"}
	// invarg() after parse_one_nh has looked dev up (an acknowledged
	// RTM_GETLINK), as a nexthop's weight comes after its dev.
	expIproute2InvargAfterDev = expect{RC: intp(255), RefusedBy: "ack-then-fail"}
)

var routeVerbs = axis{Name: "verb", Values: []value{
	v("add", "add", f("verb", "add")),
	v("replace", "replace", f("verb", "replace")),
	v("append", "append", f("verb", "append")),
	v("prepend", "prepend", f("verb", "prepend")),
	v("del", "del", f("verb", "del")),
}}

func routeTables() axis {
	return axis{Name: "table", Values: []value{
		v("absent", "", nil),
		v("main", "table main", f("table", "254")),
		v("100", "table 100", f("table", "100")),
		bv("255", "table 255", f("table", "255")),
		bv("256", "table 256", f("table", "256")),
		bv("max", "table 4294967295", f("table", "4294967295")),
		cv("0", "table 0", f("table", "0")),
	}}
}

func routeMetrics() axis {
	return axis{Name: "metric", Values: []value{
		v("absent", "", nil),
		bv("0", "metric 0", f("metric", "0")),
		v("1024", "metric 1024", f("metric", "1024")),
		bv("max", "metric 4294967295", f("metric", "4294967295")),
		cv("preference", "preference 7", f("metric", "7")),
	}}
}

func routeProtos() axis {
	return axis{Name: "proto", Values: []value{
		v("absent", "", nil),
		v("static", "proto static", f("proto", "4")),
		bv("255", "proto 255", f("proto", "255")),
		cv("0", "proto 0", f("proto", "0")),
	}}
}

func routeMTUs() axis {
	return axis{Name: "mtu", Values: []value{
		v("absent", "", nil),
		v("1280", "mtu 1280", f("mtu", "1280")),
		cv("lock", "mtu lock 1400", f("mtu", "1400", "mtu_lock", "1")),
		v("advmss", "advmss 1300", f("advmss", "1300")),
	}}
}

// The types each family's rows use. A reject type (blackhole, unreachable,
// prohibit, throw) takes no gateway, device or multipath in IPv4.
var rejectTypes = []string{"blackhole", "unreachable", "prohibit", "throw"}

func typeAxis(extra ...value) axis {
	vals := make([]value, 0, 2+len(extra)+len(rejectTypes))
	vals = append(vals, v("absent", "", nil), v("unicast", "unicast", f("type", "unicast")))
	vals = append(vals, extra...)
	for _, t := range rejectTypes {
		vals = append(vals, v(t, t, f("type", t)))
	}
	return axis{Name: "type", Values: vals}
}

func route4Space() *space {
	sp := &space{
		Name: "route4",
		Axes: []axis{
			routeVerbs,
			{Name: "dst", Values: []value{
				v("24", "198.51.100.0/24", f("dst", "198.51.100.0", "dst_len", "24")),
				cv("default", "default", f("dst", "default", "dst_len", "0")),
				bv("0", "0.0.0.0/0", f("dst", "0.0.0.0", "dst_len", "0")),
				bv("1", "128.0.0.0/1", f("dst", "128.0.0.0", "dst_len", "1")),
				bv("31", "198.51.100.6/31", f("dst", "198.51.100.6", "dst_len", "31")),
				bv("32", "198.51.100.7/32", f("dst", "198.51.100.7", "dst_len", "32")),
				cv("to", "to 203.0.113.0/24", f("dst", "203.0.113.0", "dst_len", "24")),
			}},
			typeAxis(v("local", "local", f("type", "local")), v("broadcast", "broadcast", f("type", "broadcast")),
				v("multicast", "multicast", f("type", "multicast"))),
			routeTables(),
			routeMetrics(),
			routeProtos(),
			{Name: "scope", Values: []value{
				v("absent", "", nil),
				v("global", "scope global", f("scope", "0")),
				v("link", "scope link", f("scope", "253")),
				v("host", "scope host", f("scope", "254")),
			}},
			routeMTUs(),
			{Name: "src", Values: []value{
				v("absent", "", nil),
				v("local", "src 192.0.2.10", f("prefsrc", "192.0.2.10")),
			}},
			{Name: "nh", Values: []value{
				v("dev", "dev d0", f("oif", "d0")),
				v("via", "via 192.0.2.1", f("gw", "192.0.2.1")),
				v("via-dev", "via 192.0.2.1 dev d0", f("gw", "192.0.2.1", "oif", "d0")),
				v("onlink", "via 203.0.113.1 dev d0 onlink", f("gw", "203.0.113.1", "oif", "d0", "onlink", "1")),
				v("multipath", "nexthop via 192.0.2.1 dev d0 weight 1 nexthop via 192.0.2.2 dev d0 weight 256",
					f("mp", "192.0.2.1/1;192.0.2.2/256")),
				{Name: "nhid", Arg: "nhid 10", Facts: f("nhid", "10"),
					Pre: []string{"ip nexthop add id 10 via 192.0.2.1 dev d0"}},
				v("via6", "via inet6 2001:db8:1::1 dev d0", f("via_family", "10", "via", "2001:db8:1::1", "oif", "d0")),
				v("none", "", nil),
			}},
		},
		Bad: []badValue{
			{Axis: "table", Value: v("2^32", "table 4294967296", nil), Expect: expIproute2Invarg, Why: "a table past u32"},
			{Axis: "table", Value: v("name", "table nosuch", nil), Expect: expIproute2Invarg, Why: "a table name rt_tables lacks"},
			{Axis: "metric", Value: v("2^32", "metric 4294967296", nil), Expect: expIproute2Invarg, Why: "a metric past u32"},
			{Axis: "proto", Value: v("256", "proto 256", nil), Expect: expIproute2Invarg, Why: "a protocol past u8"},
			{Axis: "dst", Value: v("33", "198.51.100.0/33", nil), Expect: expect{RC: intp(1), RefusedBy: "iproute2"},
				Why: "a prefix length past 32 (get_prefix exits 1)"},
			{Axis: "dst", Value: v("hostbits", "198.51.100.1/24", f("dst", "198.51.100.1", "dst_len", "24")),
				Expect: *expKernelEINVAL, Why: "host bits under the prefix length (Invalid prefix for given prefix length)"},
			{Axis: "scope", Value: v("nowhere", "scope nowhere", f("scope", "255")), Expect: *expKernelEINVAL,
				Why: "scope nowhere is past host (Invalid scope)"},
			{Axis: "nh", Value: v("weight0", "nexthop via 192.0.2.1 dev d0 weight 0", nil),
				Expect: expIproute2InvargAfterDev, Why: "a nexthop weight of 0"},
			{Axis: "nh", Value: v("weight257", "nexthop via 192.0.2.1 dev d0 weight 257", nil),
				Expect: expIproute2InvargAfterDev,
				Why:    "a nexthop weight past 256"},
			{Axis: "nh", Value: v("nodev", "dev nosuch", nil), Expect: expect{RC: intp(1)},
				Why: "a device that does not exist (nodev returns -1)"},
			{Axis: "type", Value: v("type-to", "unreachable to", f("type", "unreachable")),
				Expect: expect{RC: intp(1), RefusedBy: "iproute2"},
				Why:    "to after the type: iproute_modify takes to only before it, as ip-route(8)'s to TYPE PREFIX"},
		},
	}
	for _, t := range rejectTypes {
		for _, nh := range []string{"dev", "via", "via-dev", "onlink", "multipath", "via6"} {
			sp.Excl = append(sp.Excl, exclusion{A: "type=" + t, B: "nh=" + nh, Expect: expKernelEINVAL,
				Why: "a " + t + " route takes no gateway, device or multipath"})
		}
	}
	for _, t := range []string{"absent", "unicast", "local", "broadcast", "multicast"} {
		sp.Excl = append(sp.Excl, exclusion{A: "type=" + t, B: "nh=none", Expect: expKernelENODEV,
			Why: "a " + t + " route needs a device"})
	}
	for _, s := range []string{"global", "link"} {
		sp.Excl = append(sp.Excl, exclusion{A: "type=local", B: "scope=" + s, Expect: expKernelEINVAL,
			Why: "a local route's scope is at least host (Invalid scope)"})
	}
	sp.Excl = append(sp.Excl, exclusion{A: "type=broadcast", B: "scope=global", Expect: expKernelEINVAL,
		Why: "a broadcast route's scope is at least link (Invalid scope)"})
	sp.Build = func(vals []value) row { return buildRoute("4", sp.Axes, vals, route4AddVerdict) }
	sp.Judge = routeJudge(route4AddVerdict, nil)
	sp.NegBase = routeNegBase
	sp.Derive = func(pos []row) []row { return deriveRoute("route4", pos) }
	return sp
}

func route6Space() *space {
	sp := &space{
		Name: "route6",
		Axes: []axis{
			routeVerbs,
			{Name: "dst", Values: []value{
				v("64", "2001:db8:2::/64", f("dst", "2001:db8:2::", "dst_len", "64")),
				cv("default", "default", f("dst", "default", "dst_len", "0")),
				bv("0", "::/0", f("dst", "::", "dst_len", "0")),
				bv("1", "8000::/1", f("dst", "8000::", "dst_len", "1")),
				bv("127", "2001:db8:2::/127", f("dst", "2001:db8:2::", "dst_len", "127")),
				bv("128", "2001:db8:2::7/128", f("dst", "2001:db8:2::7", "dst_len", "128")),
				cv("mapped", "::ffff:198.51.100.0/120", f("dst", "::ffff:198.51.100.0", "dst_len", "120")),
			}},
			typeAxis(v("local", "local", f("type", "local")), v("anycast", "anycast", f("type", "anycast")),
				v("multicast", "multicast", f("type", "multicast"))),
			routeTables(),
			routeMetrics(),
			routeProtos(),
			{Name: "pref", Values: []value{
				v("absent", "", nil),
				v("low", "pref low", f("pref", "3")),
				v("medium", "pref medium", f("pref", "0")),
				v("high", "pref high", f("pref", "1")),
			}},
			{Name: "expires", Values: []value{
				v("absent", "", nil),
				v("60", "expires 60", f("expires", "60")),
			}},
			routeMTUs(),
			{Name: "src", Values: []value{
				v("absent", "", nil),
				v("local", "src 2001:db8:1::10", f("prefsrc", "2001:db8:1::10")),
			}},
			{Name: "nh", Values: []value{
				v("dev", "dev d0", f("oif", "d0")),
				v("via-dev", "via 2001:db8:1::1 dev d0", f("gw", "2001:db8:1::1", "oif", "d0")),
				v("via", "via 2001:db8:1::1", f("gw", "2001:db8:1::1")),
				v("onlink", "via 2001:db8:3::1 dev d0 onlink", f("gw", "2001:db8:3::1", "oif", "d0", "onlink", "1")),
				v("multipath", "nexthop via 2001:db8:1::1 dev d0 weight 1 nexthop via 2001:db8:1::2 dev d0 weight 256",
					f("mp", "2001:db8:1::1/1;2001:db8:1::2/256")),
				{Name: "nhid", Arg: "nhid 10", Facts: f("nhid", "10"),
					Pre: []string{"ip -6 nexthop add id 10 via 2001:db8:1::1 dev d0"}},
				v("none", "", nil),
			}},
		},
		Bad: []badValue{
			{Axis: "table", Value: v("2^32", "table 4294967296", nil), Expect: expIproute2Invarg, Why: "a table past u32"},
			{Axis: "metric", Value: v("2^32", "metric 4294967296", nil), Expect: expIproute2Invarg, Why: "a metric past u32"},
			{Axis: "pref", Value: v("256", "pref 256", nil), Expect: expIproute2Invarg, Why: "a router preference past u8"},
			{Axis: "dst", Value: v("129", "2001:db8:2::/129", nil), Expect: expect{RC: intp(1), RefusedBy: "iproute2"},
				Why: "a prefix length past 128 (get_prefix exits 1)"},
			{Axis: "nh", Value: v("via4", "via inet 192.0.2.1 dev d0", f("via_family", "2", "via", "192.0.2.1", "oif", "d0")),
				Expect: *expKernelEINVAL, Why: "IPv6 does not support RTA_VIA"},
			{Axis: "nh", Value: v("weight257", "nexthop via 2001:db8:1::1 dev d0 weight 257", nil),
				Expect: expIproute2InvargAfterDev,
				Why:    "a nexthop weight past 256"},
		},
	}
	// ip6_route_info_create resolves a device for every type but the
	// reject ones, which fib6_nh_init moves to lo.
	for _, t := range []string{"absent", "unicast", "local", "anycast", "multicast"} {
		sp.Excl = append(sp.Excl, exclusion{A: "type=" + t, B: "nh=none", Expect: expKernelENODEV,
			Why: "a " + t + " route needs a device"})
	}
	// fib6_nh_init puts a reject route's nexthop on lo before it stores a
	// gateway (net/ipv6/route.c), so every nexthop of a multipath reject
	// route is the same, and the second is a duplicate of the first
	// (rt6_duplicate_nexthop in fib6_add_rt2node, net/ipv6/ip6_fib.c).
	for _, t := range rejectTypes {
		sp.Excl = append(sp.Excl, exclusion{A: "type=" + t, B: "nh=multipath",
			Expect: &expect{RC: intp(2), Errno: "EEXIST", RefusedBy: "kernel"},
			Why:    "a " + t + " route's nexthops all become lo without a gateway, so the second duplicates the first (EEXIST)"})
	}
	sp.Build = func(vals []value) row { return buildRoute("6", sp.Axes, vals, nil) }
	sp.Judge = routeJudge(nil, route6DelVerdict)
	sp.NegBase = routeNegBase
	sp.Derive = func(pos []row) []row { return deriveRoute("route6", pos) }
	return sp
}

// verdict predicts a refusal from the value names, or nil for success.
type verdict func(pick map[string]string) (*expect, string)

// routeJudge predicts a row's refusal: addV judges what the row writes,
// and a delete fails with ESRCH when the add it deletes would fail; delV
// then judges a delete whose add succeeds.
func routeJudge(addV, delV verdict) func(map[string]string) (*expect, string) {
	return func(pick map[string]string) (*expect, string) {
		if addV != nil {
			if exp, why := addV(pick); exp != nil {
				if pick["verb"] != "del" {
					return exp, why
				}
				return expKernelESRCH, "deletes a route whose add is refused (" + why + ")"
			}
		}
		if pick["verb"] == "del" && delV != nil {
			return delV(pick)
		}
		return nil, ""
	}
}

// route4Scope is the scope an IPv4 write carries: the given one, or the
// one iproute_modify picks for the type (local host; broadcast, multicast
// link; unicast link without a gateway, multipath or nhid).
func route4Scope(pick map[string]string) string {
	if s := pick["scope"]; s != "absent" {
		return s
	}
	switch pick["type"] {
	case "local":
		return "host"
	case "broadcast", "multicast":
		return "link"
	case "absent", "unicast":
		if pick["nh"] == "dev" || pick["nh"] == "none" {
			return "link"
		}
	}
	return "global"
}

// route4AddVerdict holds fib_create_info's scope rules for the nexthop.
// At host scope (fib_semantics.c, nexthop.c nexthop_check_scope) a route
// takes neither more than one nexthop nor a gateway. At link scope,
// fib_check_nh_v4_gw looks the gateway up at scope host, where only local
// routes live, and returns that lookup's ENETUNREACH; it refuses onlink
// outright (EINVAL); an IPv6 gateway
// (fib_check_nh_v6_gw) and a nexthop object without onlink pass. A reject
// type skips all of it (goto link_it), so it even takes an nhid, which the
// gateway, device and multipath test before the jump leaves out.
func route4AddVerdict(pick map[string]string) (*expect, string) {
	if slices.Contains(rejectTypes, pick["type"]) {
		return nil, ""
	}
	nh := pick["nh"]
	switch route4Scope(pick) {
	case "host":
		switch nh {
		case "multipath":
			return expKernelEINVAL, "a host-scope route takes one nexthop"
		case "via", "via-dev", "onlink", "via6", "nhid":
			return expKernelEINVAL, "a host-scope route takes no gateway"
		}
	case "link":
		switch nh {
		case "via", "via-dev", "multipath":
			return &expect{RC: intp(2), Errno: "ENETUNREACH", RefusedBy: "kernel"},
				"a link-scope route's gateway must be local (Nexthop has invalid gateway)"
		case "onlink":
			return expKernelEINVAL, "onlink needs a scope below link (Nexthop has invalid scope)"
		}
	}
	return nil, ""
}

// route6DelVerdict: fib6_nh_init moves a reject route to lo and drops its
// gateway, so a delete naming the device or gateway it was added with
// matches nothing in ip6_route_del.
func route6DelVerdict(pick map[string]string) (*expect, string) {
	if !slices.Contains(rejectTypes, pick["type"]) {
		return nil, ""
	}
	switch pick["nh"] {
	case "dev", "via", "via-dev", "onlink":
		return expKernelESRCH, "a reject route was moved to lo, so a delete naming its device or gateway finds nothing"
	}
	return nil, ""
}

// routeNegBase makes negatives from adds only.
func routeNegBase(vals []value) bool { return vals[0].Name == "add" }

// buildRoute assembles `ip -F route VERB [to] [TYPE] DST ... NH`: the type
// must come just before the destination (after a to), and the nexthop
// last, since `nexthop` takes the rest of the line. A delete first adds
// what it deletes, unless addV says that add is refused.
func buildRoute(fam string, axes []axis, vals []value, addV verdict) row {
	var verb, typ, dst, nh value
	var mid []string
	var pre []string
	for i, val := range vals {
		pre = append(pre, val.Pre...)
		switch i {
		case 0:
			verb = val
		case 1:
			dst = val
		case 2:
			typ = val
		case len(vals) - 1:
			nh = val
		default:
			mid = append(mid, val.Arg)
		}
	}
	to, dstArg := "", dst.Arg
	if rest, ok := strings.CutPrefix(dstArg, "to "); ok {
		to, dstArg = "to", rest
	}
	tail := joinArgs(to, typ.Arg, dstArg, joinArgs(mid...), nh.Arg)
	head := "ip -" + fam + " route "
	r := row{
		Cmd:      head + verb.Arg + " " + tail,
		Pre:      []preStep{{Cmd: routeSetup}},
		Readback: []readback{{Name: "dump", Cmd: "ip -" + fam + " route show table all"}},
		Expect:   expect{RC: intp(0), RefusedBy: "none"},
		Axes:     mergeFacts(vals),
	}
	r.Axes["family"] = fam
	for _, p := range pre {
		r.Pre = append(r.Pre, preStep{Cmd: p})
	}
	if verb.Name == "del" {
		refused := false
		if addV != nil {
			exp, _ := addV(pickOf(axes, vals))
			refused = exp != nil
		}
		if !refused {
			r.Pre = append(r.Pre, preStep{Cmd: head + "add " + tail})
		}
	}
	return r
}

// deriveRoute adds, from the first positive rows that fit: a repeated add
// (EEXIST, since add sets NLM_F_EXCL) and a delete of a route never added
// (ESRCH).
func deriveRoute(name string, pos []row) []row { return deriveDupGone(name, pos, "ESRCH", nil) }

// deriveDupGone adds up to three repeated adds (EEXIST) from the positive
// adds canDup allows (nil allows all), and up to three deletes, each of
// something never added, failing with goneErrno.
func deriveDupGone(name string, pos []row, goneErrno string, canDup func(*row) bool) []row {
	const each = 3
	var out []row
	var dup, gone int
	for i := range pos {
		p := &pos[i]
		if p.Expect.RC == nil || *p.Expect.RC != 0 {
			continue
		}
		switch {
		case p.Axes["verb"] == "add" && dup < each && (canDup == nil || canDup(p)):
			dup++
			r := *p
			r.Pre = append(append([]preStep{}, p.Pre...), preStep{Cmd: p.Cmd})
			r.ID = name + "/dup-" + string(rune('0'+dup))
			r.Desc = "negative: " + strings.TrimPrefix(dropPrefix(p.Desc), " ") + ": added twice (EEXIST)"
			r.Expect = expect{RC: intp(2), Errno: "EEXIST", RefusedBy: "kernel"}
			out = append(out, r)
		case p.Axes["verb"] == "del" && gone < each:
			gone++
			r := *p
			r.Pre = append([]preStep{}, p.Pre[:len(p.Pre)-1]...)
			r.ID = name + "/gone-" + string(rune('0'+gone))
			r.Desc = "negative: " + strings.TrimPrefix(dropPrefix(p.Desc), " ") + ": deleted but never added (" + goneErrno + ")"
			r.Expect = expect{RC: intp(2), Errno: goneErrno, RefusedBy: "kernel"}
			out = append(out, r)
		}
	}
	return out
}

// dropPrefix strips a desc's positive:/boundary:/corner: prefix.
func dropPrefix(desc string) string {
	for _, p := range descPrefixes {
		if s, ok := strings.CutPrefix(desc, p); ok {
			return s
		}
	}
	return desc
}
