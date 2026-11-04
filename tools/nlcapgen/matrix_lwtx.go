package main

// The lwtx spaces: goip-lwt-srv6.md's forms, the encap types after mpls,
// ip and ip6 (seg6, seg6local, ila, xfrm, ioam6 and rpl) on a route, in a
// hop and on a nexthop object. Built as lwt is: a verb and a form per
// space, ip-only setup, and the ila and xfrm_interface modules loaded
// before the batch so no row autoloads one mid-batch.

import "strings"

// lwtxModules are the modules the lwtx rows' encaps need: seg6 and
// seg6local are built in; ila's and xfrm's lwtunnel ops are modules.
var lwtxModules = []string{"ila", "xfrm_interface"}

// The forms' prefixes and segments.
const (
	lwtxP6    = "-6 route add 2001:db8:2::/64"
	lwtxP4    = "-4 route add 198.51.100.0/24"
	lwtxLocal = "-6 route add 2001:db8:5::/64 encap seg6local"
	lwtxIla   = "-6 route add 2001:db8:0:1::/64 encap ila 2001:0:0:2"
	lwtxS1    = "2001:db8:9::1"
	lwtxS3    = "2001:db8:9::1,2001:db8:9::2,2001:db8:9::3"
	lwtxSrc   = "2001:db8:1::10"
	lwtxTrace = " trace prealloc type 0x800000 ns 1 size 12 dev d0"
)

// lwtxVerb is the one verb, add, with no word of its own: each form holds
// its family and `route add`, as lwthop's do.
var lwtxVerb = axis{Name: "verb", Values: []value{v("add", "", f("verb", "add"))}}

var (
	// expKernelENOTSUP is EOPNOTSUPP, which Linux numbers as ENOTSUP.
	expKernelENOTSUP = &expect{RC: intp(2), Errno: "ENOTSUP", RefusedBy: "kernel"}
	// expIproute2Exit1 is a refusal ip ends with exit(1) before sending.
	expIproute2Exit1 = expect{RC: intp(1), RefusedBy: "iproute2"}
)

// seg6Facts are an `encap seg6`'s axes: the mode's number and the slots,
// with more pairs after.
func seg6Facts(mode, nsegs string, kv ...string) map[string]string {
	return f(append([]string{"encap", "seg6", "mode", mode, "nsegs", nsegs}, kv...)...)
}

// localFacts are an `encap seg6local`'s: the action's number, then more.
func localFacts(action string, kv ...string) map[string]string {
	return f(append([]string{"encap", "seg6local", "action", action}, kv...)...)
}

func seg6Route6Space() *space {
	seg := lwtxP6 + " encap seg6 mode "
	sp := &space{
		Name: "seg6route6",
		Axes: []axis{lwtxVerb, {Name: "form", Values: []value{
			v("encap", seg+"encap segs "+lwtxS1+" dev d0", seg6Facts("1", "1")),
			v("encap-three", seg+"encap segs "+lwtxS3+" dev d0", seg6Facts("1", "3")),
			v("inline", seg+"inline segs "+lwtxS1+" dev d0", seg6Facts("0", "2")),
			v("l2encap", seg+"l2encap segs "+lwtxS1+" dev d0", seg6Facts("2", "1")),
			v("encap-red", seg+"encap.red segs "+lwtxS3+" dev d0", seg6Facts("3", "3")),
			v("l2encap-red", seg+"l2encap.red segs "+lwtxS1+" dev d0", seg6Facts("4", "1")),
			v("hmac", seg+"encap segs "+lwtxS1+" hmac 7 dev d0", seg6Facts("1", "1", "hmac", "7")),
			v("tunsrc", seg+"encap tunsrc "+lwtxSrc+" segs "+lwtxS1+" dev d0", seg6Facts("1", "1", "tunsrc", lwtxSrc)),
			cv("hmac-x", seg+"encap segs "+lwtxS1+" hmac x dev d0", seg6Facts("1", "1")),
		}}},
		Bad: []badValue{
			{Axis: "form", Value: v("no-mode", lwtxP6+" encap seg6 hmac 1 dev d0", seg6Facts("-1", "1", "hmac", "1")),
				Expect: *expKernelEINVAL, Why: "no mode, sent as -1 (seg6_build_state: invalid SRv6 encapsulation mode)"},
			{Axis: "form", Value: v("tunsrc-inline", seg+"inline tunsrc "+lwtxSrc+" segs "+lwtxS1+" dev d0", f("encap", "seg6")),
				Expect: expIproute2Invarg, Why: `tunsrc with inline (invarg("\"tunsrc\" invalid with inline mode"))`},
			{Axis: "form", Value: v("segs-first", lwtxP6+" encap seg6 segs "+lwtxS1+" mode encap dev d0", f("encap", "seg6")),
				Expect: expIproute2Invarg, Why: `segs before mode (invarg("\"segs\" provided before \"mode\""))`},
			{Axis: "form", Value: v("mode-unknown", seg+"foo segs "+lwtxS1+" dev d0", f("encap", "seg6")),
				Expect: expIproute2Invarg, Why: `an unknown mode (invarg("\"mode\" value is invalid"))`},
		},
	}
	sp.Build = func(vals []value) row { return buildLwtx("ip ", "ip -6 route show table all", vals) }
	sp.Derive = func(pos []row) []row { return deriveDupGone(sp.Name, pos, "ESRCH", nil) }
	return sp
}

func seg6Route4Space() *space {
	seg := lwtxP4 + " encap seg6 mode "
	sp := &space{
		Name: "seg6route4",
		Axes: []axis{lwtxVerb, {Name: "form", Values: []value{
			v("encap", seg+"encap segs "+lwtxS1+" dev d0", seg6Facts("1", "1")),
		}}},
		Bad: []badValue{
			{Axis: "form", Value: v("inline", seg+"inline segs "+lwtxS1+" dev d0", seg6Facts("0", "2")),
				Expect: *expKernelEINVAL, Why: "inline on IPv4 (seg6_build_state: inline mode requires the IPv6 address family)"},
		},
	}
	sp.Build = func(vals []value) row { return buildLwtx("ip ", "ip -4 route show table all", vals) }
	sp.Derive = func(pos []row) []row { return deriveDupGone(sp.Name, pos, "ESRCH", nil) }
	return sp
}

func seg6LocalSpace() *space {
	act := lwtxLocal + " action "
	sp := &space{
		Name: "seg6local",
		Axes: []axis{lwtxVerb, {Name: "form", Values: []value{
			v("end", act+"End dev d0", localFacts("1")),
			v("end-count", act+"End count dev d0", localFacts("1", "count", "1")),
			v("end-csid", act+"End flavors next-csid lblen 32 nflen 16 dev d0", localFacts("1", "flavors", "16", "lblen", "32", "nflen", "16")),
			v("end-psp", act+"End flavors psp dev d0", localFacts("1", "flavors", "2")),
			v("end-x", act+"End.X nh6 2001:db8:1::1 oif d0 dev d0", localFacts("2", "nh6", "2001:db8:1::1", "oif", "d0")),
			v("end-t", act+"End.T table 100 dev d0", localFacts("3", "table", "100")),
			v("end-dx2", act+"End.DX2 oif d0 dev d0", localFacts("4", "oif", "d0")),
			v("end-dx6", act+"End.DX6 nh6 2001:db8:1::1 dev d0", localFacts("5", "nh6", "2001:db8:1::1")),
			v("end-dx4", act+"End.DX4 nh4 192.0.2.1 dev d0", localFacts("6", "nh4", "192.0.2.1")),
			v("end-dt6", act+"End.DT6 table 100 dev d0", localFacts("7", "table", "100")),
			v("end-b6", act+"End.B6 srh segs "+lwtxS3+" dev d0", localFacts("9", "srh_nsegs", "4")),
			v("end-b6-encaps", act+"End.B6.Encaps srh segs "+lwtxS1+" hmac 7 dev d0", localFacts("10", "srh_nsegs", "1", "hmac", "7")),
		}}},
		Bad: []badValue{
			{Axis: "form", Value: v("end-usp", act+"End flavors usp dev d0", localFacts("1", "flavors", "4")),
				Expect: *expKernelENOTSUP, Why: "usp (seg6_parse_flavors: Unsupported Flavor operation(s))"},
			{Axis: "form", Value: v("end-x-bare", act+"End.X dev d0", localFacts("2")),
				Expect: *expKernelEINVAL, Why: "End.X without nh6, its required attribute"},
			{Axis: "form", Value: v("end-dt4", act+"End.DT4 vrftable 100 dev d0", localFacts("8", "vrftable", "100")),
				Expect: *expKernelEINVAL, Why: "End.DT4 with vrf strict mode off (Strict mode for VRF is disabled)"},
			{Axis: "form", Value: v("action-unknown", act+"End.Q dev d0", f("encap", "seg6local")),
				Expect: expIproute2Invarg, Why: `an unknown action (invarg("\"action\" value is invalid"))`},
			{Axis: "form", Value: v("no-action", lwtxLocal+" table 100 dev d0", f("encap", "seg6local")),
				Expect: expIproute2Invarg, Why: "no action (Missing action type, exit(-1))"},
			{Axis: "form", Value: v("oif-nosuch", act+"End.DX2 oif nosuch dev d0", f("encap", "seg6local")),
				Expect: expIproute2Invarg, Why: "an oif that doesn't exist (exit(nodev))"},
		},
	}
	sp.Build = func(vals []value) row { return buildLwtx("ip ", "ip -6 route show table all", vals) }
	sp.Derive = func(pos []row) []row { return deriveDupGone(sp.Name, pos, "ESRCH", nil) }
	return sp
}

func ilaSpace() *space {
	ilaF := func(kv ...string) map[string]string {
		return f(append([]string{"encap", "ila", "locator", "2001:0:0:2"}, kv...)...)
	}
	sp := &space{
		Name: "ila",
		Axes: []axis{lwtxVerb, {Name: "form", Values: []value{
			v("luid", lwtxIla+" ident-type luid dev d0", ilaF("ident", "1")),
			v("every", lwtxIla+" csum-mode no-action ident-type luid hook-type output dev d0", ilaF("csum", "2", "ident", "1", "hook", "0")),
			v("adj-transport", lwtxIla+" csum-mode adj-transport ident-type luid dev d0", ilaF("csum", "0", "ident", "1")),
		}}},
		Bad: []badValue{
			{Axis: "form", Value: v("use-format", lwtxIla+" dev d0", ilaF()),
				Expect: *expKernelEINVAL, Why: "use-format on a /64 (ila_build_state: the prefix is shorter than 67)"},
			{Axis: "form", Value: v("ipv4", lwtxP4+" encap ila 2001:0:0:2 ident-type luid dev d0", ilaF("ident", "1")),
				Expect: *expKernelEINVAL, Why: "ila on IPv4 (ila_build_state: family)"},
			{Axis: "form", Value: v("five-groups", "-6 route add 2001:db8:0:1::/64 encap ila 2001:0:0:2:3 dev d0", f("encap", "ila")),
				Expect: expIproute2Exit1, Why: "a locator of five groups (Bad locator, exit(1))"},
			{Axis: "form", Value: v("csum-unknown", lwtxIla+" csum-mode foo dev d0", f("encap", "ila")),
				Expect: expIproute2Invarg, Why: `an unknown csum-mode (invarg("\"csum-mode\" value is invalid"))`},
		},
	}
	sp.Build = func(vals []value) row { return buildLwtx("ip ", "ip route show table all", vals) }
	sp.Derive = func(pos []row) []row { return deriveDupGone(sp.Name, pos, "ESRCH", nil) }
	return sp
}

func xfrmSpace() *space {
	xf := func(kv ...string) map[string]string { return f(append([]string{"encap", "xfrm"}, kv...)...) }
	sp := &space{
		Name: "xfrm",
		Axes: []axis{lwtxVerb, {Name: "form", Values: []value{
			v("if-id", lwtxP6+" dev d0 encap xfrm if_id 1", xf("if_id", "1")),
			v("link-dev", lwtxP6+" dev d0 encap xfrm if_id 1 link_dev d0", xf("if_id", "1", "link", "d0")),
			v("ipv4", lwtxP4+" dev d0 encap xfrm if_id 7", xf("if_id", "7")),
		}}},
		Bad: []badValue{
			{Axis: "form", Value: v("swallowed", lwtxP6+" encap xfrm if_id 1 dev d0", xf("if_id", "1", "oif", "none")),
				Expect: *expKernelENODEV, Why: "dev d0 swallowed by parse_encap_xfrm, so the route has no device"},
			{Axis: "form", Value: v("if-id-0", lwtxP6+" dev d0 encap xfrm if_id 0", xf()),
				Expect: expIproute2Invarg, Why: `if_id 0 (invarg("\"if_id\" value is invalid"))`},
			{Axis: "form", Value: v("no-if-id", lwtxP6+" dev d0 encap xfrm link_dev d0", xf()),
				Expect: expIproute2Invarg, Why: "no if_id (lwt_xfrm_usage, exit(-1))"},
		},
	}
	sp.Build = func(vals []value) row { return buildLwtx("ip ", "ip route show table all", vals) }
	sp.Derive = func(pos []row) []row { return deriveDupGone(sp.Name, pos, "ESRCH", nil) }
	return sp
}

// lwtxReqVerdicts are the lwtxreq forms' refusals. The guest kernel has
// neither IOAM6_LWTUNNEL nor RPL_LWTUNNEL, so even the forms ip sends are
// refused, and the space has no positive row: Judge makes every form a
// negative.
var lwtxReqVerdicts = map[string]struct {
	exp *expect
	why string
}{
	"ioam6-inline": {expKernelENOTSUP, "no IOAM6_LWTUNNEL in the guest kernel (lwt encapsulation type not supported)"},
	"ioam6-encap":  {expKernelENOTSUP, "no IOAM6_LWTUNNEL in the guest kernel (lwt encapsulation type not supported)"},
	"ioam6-tundst-inline": {&expIproute2Invarg,
		`tundst with inline (invarg("Inline mode does not need tundst"))`},
	"ioam6-size-13":  {&expIproute2Invarg, `a size not a multiple of 4 (invarg("Trace size must be a 4-octet multiple"))`},
	"ioam6-k-over-n": {&expIproute2Invarg, `k over n (invarg("Frequency with k > n is forbidden"))`},
	"rpl":            {expKernelENOTSUP, "no RPL_LWTUNNEL in the guest kernel (lwt encapsulation type not supported)"},
}

func lwtxReqSpace() *space {
	io := lwtxP6 + " encap ioam6"
	iof := func(kv ...string) map[string]string { return f(append([]string{"encap", "ioam6"}, kv...)...) }
	trace := []string{"type", "8388608", "ns", "1", "size", "12"}
	sp := &space{
		Name: "lwtxreq",
		Axes: []axis{lwtxVerb, {Name: "form", Values: []value{
			v("ioam6-inline", io+lwtxTrace, iof(append([]string{"freq_k", "1", "freq_n", "1", "mode", "1"}, trace...)...)),
			v("ioam6-encap", io+" freq 1/2 mode encap tunsrc "+lwtxSrc+" tundst "+lwtxS1+lwtxTrace,
				iof(append([]string{"freq_k", "1", "freq_n", "2", "mode", "2", "tunsrc", lwtxSrc, "tundst", lwtxS1}, trace...)...)),
			v("ioam6-tundst-inline", io+" mode inline tundst "+lwtxS1+lwtxTrace, iof()),
			v("ioam6-size-13", io+" trace prealloc type 0x800000 ns 1 size 13 dev d0", iof()),
			v("ioam6-k-over-n", io+" freq 3/2"+lwtxTrace, iof()),
			v("rpl", lwtxP6+" encap rpl segs 2001:db8:9::1,2001:db8:9::2 dev d0", f("encap", "rpl", "nsegs", "2")),
		}}},
		Judge: func(pick map[string]string) (*expect, string) {
			vd := lwtxReqVerdicts[pick["form"]]
			return vd.exp, vd.why
		},
	}
	sp.Build = func(vals []value) row { return buildLwtx("ip ", "ip -6 route show table all", vals) }
	return sp
}

func lwtxHopSpace() *space {
	sp := &space{
		Name: "lwtxhop",
		Axes: []axis{lwtxVerb, {Name: "form", Values: []value{
			v("hop6", lwtxP6+" nexthop via 2001:db8:1::1 dev d0 encap seg6 mode encap segs "+lwtxS1+" nexthop via 2001:db8:1::2 dev d0",
				f("hop1_encap", "seg6", "hop1_mode", "1", "hop1_nsegs", "1")),
			v("hop4", lwtxP4+" nexthop via 192.0.2.1 dev d0 encap seg6 mode encap segs "+lwtxS1+" nexthop via 192.0.2.2 dev d0",
				f("hop1_encap", "seg6", "hop1_mode", "1", "hop1_nsegs", "1")),
		}}},
	}
	sp.Build = func(vals []value) row { return buildLwtx("ip ", "ip route show table all", vals) }
	sp.Derive = func(pos []row) []row { return deriveDupGone(sp.Name, pos, "ESRCH", nil) }
	return sp
}

func lwtxNhSpace() *space {
	sp := &space{
		Name: "lwtxnh",
		Axes: []axis{kwAdd, {Name: "form", Values: []value{
			v("seg6", "id 71 encap seg6 mode encap segs "+lwtxS1+" dev d0", seg6Facts("1", "1", "id", "71")),
			v("seg6local", "id 72 encap seg6local action End dev d0", localFacts("1", "id", "72")),
		}}},
	}
	sp.Build = func(vals []value) row { return buildLwtx("ip -6 nexthop ", "ip nexthop show", vals) }
	sp.Derive = func(pos []row) []row { return deriveDupGone(sp.Name, pos, "ENOENT", nhHasID) }
	return sp
}

// buildLwtx is buildKw over routeSetup with lwtxModules. A form that holds
// its own `route add` is written after head with no space of its own.
func buildLwtx(head, show string, vals []value) row {
	r := buildKw(head, routeSetup, show, vals)
	r.Cmd = strings.Join(strings.Fields(r.Cmd), " ")
	r.Modules = lwtxModules
	return r
}
