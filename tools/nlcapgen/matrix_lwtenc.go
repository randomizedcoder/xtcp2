package main

// The lwtenc spaces: the encaps no capture had working, rpl, ioam6 and
// bpf, on a route, on a nexthop object and in a multipath hop (NC8-T03 of
// docs/netlink/goip-parity-next.md). rpl and ioam6 need the workbench's
// kernel, which adds IPV6_RPL_LWTUNNEL and IPV6_IOAM6_LWTUNNEL (NC8-T01);
// lwtx's lwtxreq rows, captured on the stock kernel, stay ENOTSUP. bpf
// needs `nlcap-ws batch --bpf`, which links the workbench's object at
// lwtencBPF (NC8-T02). Each positive row reads its object back three ways:
// plain, -j and -d.
//
// The expectations come from iproute2 7.2.0's ip/iproute_lwtunnel.c
// (parse_encap_rpl, parse_encap_ioam6, parse_encap_bpf) and the kernel's
// rpl_build_state, ioam6_build_state and bpf_build_state.

import "strings"

// lwtencBPF is where `nlcap-ws batch --bpf` links the object. ip names a
// program "<basename>:[<section>]", so the readbacks name lwt_bpf.o.
const lwtencBPF = "/run/nlcap-ws/lwt_bpf.o"

// The bpf programs, one per section of nix/nlcap-lwt-bpf/lwt_bpf.c.
const (
	lwtencIn   = " in obj " + lwtencBPF + " sec lwt_in"
	lwtencOut  = " out obj " + lwtencBPF + " sec lwt_out"
	lwtencXmit = " xmit obj " + lwtencBPF + " sec lwt_xmit"
)

// expKernelERANGE is bpf_build_state's headroom above LWT_BPF_MAX_HEADROOM
// (256).
var expKernelERANGE = &expect{RC: intp(2), Errno: "ERANGE", RefusedBy: refKernel}

func rplFacts(nsegs string, kv ...string) map[string]string {
	return f(append([]string{"encap", "rpl", "nsegs", nsegs}, kv...)...)
}

func ioamFacts(kv ...string) map[string]string {
	return f(append([]string{"encap", "ioam6"}, kv...)...)
}

func bpfFacts(kv ...string) map[string]string {
	return f(append([]string{"encap", "bpf"}, kv...)...)
}

// ioamTrace is lwtxTrace's trace with the type, ns and size given.
func ioamTrace(typ, ns, size string) string {
	return " trace prealloc type " + typ + " ns " + ns + " size " + size + " dev d0"
}

// ioamTraceFacts are an inline trace's axes: freq 1/1 (ip's default) and
// mode inline, then the trace's.
func ioamTraceFacts(typ, ns, size string, kv ...string) map[string]string {
	return ioamFacts(append([]string{"freq_k", "1", "freq_n", "1", "mode", "1",
		"type", typ, "ns", ns, "size", size}, kv...)...)
}

func rplRouteSpace() *space {
	rpl := lwtxP6 + " encap rpl segs "
	sp := &space{
		Name: "rplroute",
		Axes: []axis{lwtxVerb, {Name: "form", Values: []value{
			v("one", rpl+lwtxS1+" dev d0", rplFacts("1", "seg0", lwtxS1)),
			v("three", rpl+lwtxS3+" dev d0", rplFacts("3", "seg0", "2001:db8:9::1", "seg2", "2001:db8:9::3")),
			// No segs: parse_rpl_srh still counts one segment, ::, and the
			// kernel takes it (rpl_validate_srh checks only its count).
			cv("no-segs", lwtxP6+" encap rpl dev d0", rplFacts("1", "seg0", "::")),
		}}},
		Bad: []badValue{
			{Axis: "form", Value: v("ipv4-seg", rpl+"192.0.2.1 dev d0", f("encap", "rpl")),
				Expect: expIproute2Exit1, Why: "an IPv4 seg (get_addr: inet6 address is expected, exit(1))"},
			{Axis: "form", Value: v("dup-segs", rpl+lwtxS1+" segs 2001:db8:9::2 dev d0", f("encap", "rpl")),
				Expect: expIproute2Invarg, Why: `segs twice (duparg2("segs"), exit(-1))`},
			{Axis: "form", Value: v("mcast", rpl+"ff02::1 dev d0", rplFacts("1", "seg0", "ff02::1")),
				Expect: *expKernelEINVAL, Why: "a multicast last segment (rpl_validate_srh)"},
			{Axis: "form", Value: v("ipv4-route", lwtxP4+" encap rpl segs "+lwtxS1+" dev d0", rplFacts("1", "seg0", lwtxS1)),
				Expect: *expKernelEINVAL, Why: "rpl on IPv4 (rpl_build_state: family != AF_INET6)"},
		},
	}
	sp.Build = func(vals []value) row { return buildLwtenc("ip ", []string{"ip -6 route show table all"}, vals) }
	sp.Derive = func(pos []row) []row { return deriveDupGone(sp.Name, pos, "ESRCH", nil) }
	return sp
}

func ioam6RouteSpace() *space {
	io := lwtxP6 + " encap ioam6"
	tr := ioamTrace("0x800000", "1", "12")
	trF := func(kv ...string) []string {
		return append([]string{"type", "8388608", "ns", "1", "size", "12"}, kv...)
	}
	sp := &space{
		Name: "ioam6route",
		Axes: []axis{lwtxVerb, {Name: "form", Values: []value{
			v("inline", io+tr, ioamTraceFacts("8388608", "1", "12")),
			v("encap", io+" freq 1/2 mode encap tunsrc "+lwtxSrc+" tundst "+lwtxS1+tr,
				ioamFacts(trF("freq_k", "1", "freq_n", "2", "mode", "2", "tunsrc", lwtxSrc, "tundst", lwtxS1)...)),
			v("encap-no-tunsrc", io+" mode encap tundst "+lwtxS1+tr,
				ioamFacts(trF("freq_k", "1", "freq_n", "1", "mode", "2", "tundst", lwtxS1)...)),
			v("auto", io+" mode auto tundst "+lwtxS1+tr,
				ioamFacts(trF("freq_k", "1", "freq_n", "1", "mode", "3", "tundst", lwtxS1)...)),
			v("type-opaque", io+ioamTrace("0x000002", "1", "12"), ioamTraceFacts("2", "1", "12")),
			bv("freq-max", io+" freq 1000000/1000000"+tr,
				ioamFacts(trF("freq_k", "1000000", "freq_n", "1000000", "mode", "1")...)),
			bv("size-4", io+ioamTrace("0x800000", "1", "4"), ioamTraceFacts("8388608", "1", "4")),
			bv("size-244", io+ioamTrace("0x800000", "1", "244"), ioamTraceFacts("8388608", "1", "244")),
			bv("ns-0", io+ioamTrace("0x800000", "0", "12"), ioamTraceFacts("8388608", "0", "12")),
			bv("ns-65535", io+ioamTrace("0x800000", "65535", "12"), ioamTraceFacts("8388608", "65535", "12")),
			// The namespace the trace names, made first: whether the kernel
			// cares is what the row records (ioam6_build_state reads none).
			{Name: "ns-added", Arg: io + tr, Facts: ioamTraceFacts("8388608", "1", "12", "ns_added", "1"),
				Pre: []string{"ip ioam namespace add 1"}, Corner: true},
		}}},
		Bad: []badValue{
			{Axis: "form", Value: v("size-248", io+ioamTrace("0x800000", "1", "248"), ioamFacts()),
				Expect: expIproute2Invarg, Why: `size above 244 (invarg("Trace size is too big"))`},
			{Axis: "form", Value: v("size-13", io+ioamTrace("0x800000", "1", "13"), ioamFacts()),
				Expect: expIproute2Invarg, Why: `a size not a multiple of 4 (invarg("Trace size must be a 4-octet multiple"))`},
			{Axis: "form", Value: v("k-over-n", io+" freq 3/2"+tr, ioamFacts()),
				Expect: expIproute2Invarg, Why: `k over n (invarg("Frequency with k > n is forbidden"))`},
			{Axis: "form", Value: v("freq-over", io+" freq 1/1000001"+tr, ioamFacts()),
				Expect: expIproute2Invarg, Why: `n above 1000000 (invarg("Out of bound \"n\" frequency"))`},
			{Axis: "form", Value: v("tundst-inline", io+" mode inline tundst "+lwtxS1+tr, ioamFacts()),
				Expect: expIproute2Invarg, Why: `tundst with inline (invarg("Inline mode does not need tundst"))`},
			{Axis: "form", Value: v("encap-no-tundst", io+" mode encap"+tr, ioamFacts()),
				Expect: expIproute2Invarg, Why: `encap without tundst (missarg("tundst"))`},
			{Axis: "form", Value: v("mode-unknown", io+" mode foo"+tr, ioamFacts()),
				Expect: expIproute2Invarg, Why: `an unknown mode (invarg("Invalid mode"))`},
			{Axis: "form", Value: v("type-bit12", io+ioamTrace("0x800", "1", "12"), ioamTraceFacts("2048", "1", "12")),
				Expect: *expKernelEINVAL, Why: "trace type bit 12, which ioam6_validate_trace_hdr refuses"},
			{Axis: "form", Value: v("ipv4-route", lwtxP4+" encap ioam6"+tr, ioamTraceFacts("8388608", "1", "12")),
				Expect: *expKernelEINVAL, Why: "ioam6 on IPv4 (ioam6_build_state: family != AF_INET6)"},
		},
	}
	sp.Build = func(vals []value) row { return buildLwtenc("ip ", []string{"ip -6 route show table all"}, vals) }
	sp.Derive = func(pos []row) []row { return deriveDupGone(sp.Name, pos, "ESRCH", nil) }
	return sp
}

func bpfRouteSpace() *space {
	bpf := lwtxP4 + " encap bpf"
	sp := &space{
		Name: "bpfroute",
		Axes: []axis{lwtxVerb, {Name: "form", Values: []value{
			v("in", bpf+lwtencIn+" dev d0", bpfFacts("in", "lwt_in")),
			v("out", bpf+lwtencOut+" dev d0", bpfFacts("out", "lwt_out")),
			v("xmit", bpf+lwtencXmit+" dev d0", bpfFacts("xmit", "lwt_xmit")),
			v("all", bpf+lwtencIn+lwtencOut+lwtencXmit+" headroom 16 dev d0",
				bpfFacts("in", "lwt_in", "out", "lwt_out", "xmit", "lwt_xmit", "headroom", "16")),
			bv("headroom-1", bpf+lwtencXmit+" headroom 1 dev d0", bpfFacts("xmit", "lwt_xmit", "headroom", "1")),
			bv("headroom-256", bpf+lwtencXmit+" headroom 256 dev d0", bpfFacts("xmit", "lwt_xmit", "headroom", "256")),
			// ip takes the hook from the keyword, not the section, so an
			// lwt_xmit program loads as the in hook.
			cv("in-from-xmit-sec", bpf+" in obj "+lwtencBPF+" sec lwt_xmit dev d0", bpfFacts("in", "lwt_xmit")),
			// Two `in`s: two LWT_BPF_IN nests, and the kernel keeps the last.
			cv("in-twice", bpf+lwtencIn+" in obj "+lwtencBPF+" sec lwt_out dev d0", bpfFacts("in", "lwt_out", "in_first", "lwt_in")),
			cv("headroom-twice", bpf+lwtencXmit+" headroom 8 headroom 9 dev d0", bpfFacts("xmit", "lwt_xmit", "headroom", "8")),
		}}},
		Bad: []badValue{
			{Axis: "form", Value: v("headroom-0", bpf+lwtencXmit+" headroom 0 dev d0", bpfFacts()),
				Expect: expIproute2Invarg, Why: `headroom 0 (invarg("headroom is invalid"))`},
			{Axis: "form", Value: v("headroom-257", bpf+lwtencXmit+" headroom 257 dev d0", bpfFacts("xmit", "lwt_xmit", "headroom", "257")),
				Expect: *expKernelERANGE, Why: "headroom above LWT_BPF_MAX_HEADROOM (bpf_build_state: -ERANGE)"},
			{Axis: "form", Value: v("headroom-only", bpf+" headroom 16 dev d0", bpfFacts("headroom", "16")),
				Expect: *expKernelEINVAL, Why: "no program (bpf_build_state: no in, out or xmit)"},
			{Axis: "form", Value: v("sec-nosuch", bpf+" in obj "+lwtencBPF+" sec nosuch dev d0", bpfFacts()),
				Expect: expIproute2Invarg, Why: "a section the object lacks (Failed to parse eBPF program, exit(-1))"},
			{Axis: "form", Value: v("obj-nosuch", bpf+" in obj /nonexistent/lwt.o sec lwt_in dev d0", bpfFacts()),
				Expect: expIproute2Invarg, Why: "an object that doesn't exist (Failed to parse eBPF program, exit(-1))"},
		},
	}
	sp.Build = func(vals []value) row { return buildLwtenc("ip ", []string{"ip -4 route show table all"}, vals) }
	sp.Derive = func(pos []row) []row { return deriveDupGone(sp.Name, pos, "ESRCH", nil) }
	return sp
}

func lwtencNhSpace() *space {
	sp := &space{
		Name: "lwtencnh",
		Axes: []axis{kwAdd, {Name: "form", Values: []value{
			v("rpl", "id 81 encap rpl segs "+lwtxS1+" dev d0", rplFacts("1", "seg0", lwtxS1, "id", "81")),
			v("ioam6", "id 82 encap ioam6"+ioamTrace("0x800000", "1", "12"), ioamTraceFacts("8388608", "1", "12", "id", "82")),
			v("bpf", "id 83 encap bpf"+lwtencIn+" dev d0", bpfFacts("in", "lwt_in", "id", "83")),
			v("bpf-headroom", "id 84 encap bpf"+lwtencXmit+" headroom 32 dev d0", bpfFacts("xmit", "lwt_xmit", "headroom", "32", "id", "84")),
		}}},
	}
	sp.Build = func(vals []value) row { return buildLwtenc("ip -6 nexthop ", []string{"ip nexthop show"}, vals) }
	sp.Derive = func(pos []row) []row { return deriveDupGone(sp.Name, pos, "ENOENT", nhHasID) }
	return sp
}

func lwtencHopSpace() *space {
	hop := func(encap string) string {
		return lwtxP6 + " nexthop via 2001:db8:1::1 dev d0 encap " + encap + " nexthop via 2001:db8:1::2 dev d0"
	}
	sp := &space{
		Name: "lwtenchop",
		Axes: []axis{lwtxVerb, {Name: "form", Values: []value{
			v("rpl", hop("rpl segs "+lwtxS1), f("hop1_encap", "rpl", "hop1_nsegs", "1")),
			v("ioam6", lwtxP6+" nexthop via 2001:db8:1::1 encap ioam6"+ioamTrace("0x800000", "1", "12")+
				" nexthop via 2001:db8:1::2 dev d0", f("hop1_encap", "ioam6", "hop1_size", "12")),
			v("bpf", hop("bpf"+lwtencIn), f("hop1_encap", "bpf", "hop1_in", "lwt_in")),
		}}},
	}
	sp.Build = func(vals []value) row { return buildLwtenc("ip ", []string{"ip -6 route show table all"}, vals) }
	sp.Derive = func(pos []row) []row { return deriveDupGone(sp.Name, pos, "ESRCH", nil) }
	return sp
}

// buildLwtenc is buildLwtx with each show read back plain, as -j and as
// -d: readbacks dump, dump_j and dump_d (dump2, dump2_j, ... for a second
// show).
func buildLwtenc(head string, shows []string, vals []value) row {
	r := buildLwtx(head, shows[0], vals)
	r.Readback = nil
	for i, show := range shows {
		name := "dump"
		if i > 0 {
			name += string(rune('1' + i))
		}
		rest, _ := strings.CutPrefix(show, "ip ")
		r.Readback = append(r.Readback,
			readback{Name: name, Cmd: show},
			readback{Name: name + "_j", Cmd: "ip -j " + rest},
			readback{Name: name + "_d", Cmd: "ip -d " + rest})
	}
	return r
}
