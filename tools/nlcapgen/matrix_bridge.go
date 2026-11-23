package main

import "strings"

// The bridge show space (gobridge 6C): bridge(8)'s read-only link, vlan,
// vni and mst commands over rtnetlink. bridge's side is iproute2's
// bridge/: main (bridge/bridge.c) matches its options by prefix, with
// -color tested before -compressvlans, so -c is color and compression is
// -com (bridge.8). An unknown option exits 255 before any socket; an
// unknown object returns -1 (rc 255) after rtnl_open but before any
// request. Each object's show then runs ll_init_map (an AF_UNSPEC link
// dump) before it parses its words, so every later refusal by bridge is an
// ack-then-fail at rc 255.
//
// The kernel's side is net/bridge/br_netlink.c (br_getlink, br_fill_ifinfo
// for link, vlan, tunnel and mst), br_vlan.c / br_vlan_options.c
// (RTM_GETVLAN), br_netlink.c's bridge xstats (RTM_GETSTATS) and
// drivers/net/vxlan/vxlan_vnifilter.c (RTM_GETTUNNEL).
//
// The rows pin the bug-class leads of gobridge.md (B1-B24): `link show dev
// master` (B1), unknown words ignored (B2), vid parsed with atoi (B5), the
// mst dump aborted by a port without IFLA_AF_SPEC (B10), `-s vlan
// tunnelshow` taking the stats path (B22), and `dev if999` accepted as an
// ifindex (B24).
//
// Determinism: every device has a fixed MAC and addrgenmode none before it
// comes up (no IPv6 link-local or DAD traffic), STP is off so ports forward
// at once, and each row answers in its own fresh namespace. The vlan and
// vni stats counters are what may differ between boots.
//
// This space is read-only: setup runs in `pre`, outside the capture
// window. The clone (internal/gobridge) replays these captures and must
// reproduce bridge's stdout, stderr and exit status byte for byte.

var bridgeModules = []string{"bridge", "vxlan"}

// brPorts adds dummy ports to br0 with fixed MACs, each up after
// addrgenmode none, then brings br0 up.
func brPorts(names ...string) string {
	var b strings.Builder
	for i, n := range names {
		if i > 0 {
			b.WriteString(" && ")
		}
		b.WriteString("ip link add " + n + " address 02:00:00:00:01:0" + string(rune('1'+i)) + " type dummy" +
			" && ip link set " + n + " addrgenmode none && ip link set " + n + " master br0 && ip link set " + n + " up")
	}
	return b.String() + " && ip link set br0 up"
}

// brCreate is br0, VLAN filtering on, with mst_enabled set at creation when
// mst: the kernel refuses to change MST while any port has VLANs.
func brCreate(mst bool) string {
	s := "ip link add br0 address 02:00:00:00:01:00 type bridge vlan_filtering 1"
	if mst {
		s += " mst_enabled 1"
	}
	return s + " && ip link set br0 addrgenmode none"
}

// brVlans: d1 carries vid 10 pvid untagged and 20-25, d2 vid 30, and br0
// itself vid 10; every port also has the default pvid 1.
const brVlans = "bridge vlan add dev d1 vid 10 pvid untagged && bridge vlan add dev d1 vid 20-25" +
	" && bridge vlan add dev d2 vid 30 && bridge vlan add dev br0 vid 10 self"

// brPortOpts gives d2 protinfo that differs from the defaults.
const brPortOpts = "bridge link set dev d2 learning off cost 7 priority 5 hairpin on"

// brFix is the base fixture: br0 with ports d1 and d2, their VLANs and d2's
// port options.
var brFix = []string{brCreate(false), brPorts("d1", "d2"), brVlans, brPortOpts}

// brGlobFix adds per-VLAN global options on br0's vid 10.
var brGlobFix = append(clonePre(brFix),
	"ip link set br0 type bridge mcast_vlan_snooping 1",
	"bridge vlan global set dev br0 vid 10 mcast_snooping 0 mcast_igmp_version 3")

// brMstFix enables MST before any port exists, maps vid 10 to MSTI 3 and
// sets d1's state in it to learning (2).
var brMstFix = []string{
	brCreate(true), brPorts("d1", "d2"), brVlans,
	"bridge vlan global set dev br0 vid 10 msti 3",
	"bridge mst set dev d1 msti 3 state 2",
}

// brMstNoVlanFix is MST on with d2 holding no VLAN at all: d2's MST reply
// has no IFLA_AF_SPEC, which aborts bridge's mst dump (B10).
var brMstNoVlanFix = []string{
	brCreate(true), brPorts("d1", "d2"),
	"bridge vlan add dev d1 vid 10 pvid untagged && bridge vlan del dev d2 vid 1",
}

// brVxTunFix adds vx0, an external vxlan, as a br0 port with VLAN-to-tunnel
// mapping: vid 100 to id 1000, and the range 200-202 to 2000-2002. A VLAN
// must be on the port before its tunnel_info (the kernel answers a bare
// EINVAL otherwise).
var brVxTunFix = append(clonePre(brFix),
	"ip link add vx0 address 02:00:00:00:01:10 type vxlan external dstport 4789"+
		" && ip link set vx0 addrgenmode none && ip link set vx0 master br0 && ip link set vx0 up",
	"bridge link set dev vx0 vlan_tunnel on",
	"bridge vlan add dev vx0 vid 100 && bridge vlan add dev vx0 vid 200-202",
	"bridge vlan add dev vx0 vid 100 tunnel_info id 1000 && bridge vlan add dev vx0 vid 200-202 tunnel_info id 2000-2002",
)

// brWideFix adds d3 with every VLAN from 2 to 4094: compressed, one range
// as wide as VLAN_ID_LEN (9) allows.
var brWideFix = append(clonePre(brFix),
	"ip link add d3 address 02:00:00:00:01:03 type dummy && ip link set d3 addrgenmode none"+
		" && ip link set d3 master br0 && ip link set d3 up",
	"bridge vlan add dev d3 vid 2-4094",
)

// vnifFix is vx1, an external vxlan with VNI filtering over a dummy (a
// multicast group needs a local interface), kept down: VNIs 100-102, 200
// with an IPv4 group, 300 with an IPv6 remote (GROUP6), and
// 16777214-16777215, exactly VXLAN_ID_LEN (17) wide.
var vnifFix = []string{
	"ip link add d9 address 02:00:00:00:01:09 type dummy && ip link set d9 addrgenmode none && ip link set d9 up",
	"ip link add vx1 address 02:00:00:00:01:11 type vxlan external vnifilter dstport 4790 dev d9" +
		" && ip link set vx1 addrgenmode none",
	"bridge vni add dev vx1 vni 100-102",
	"bridge vni add dev vx1 vni 200 group 239.1.1.1",
	"bridge vni add dev vx1 vni 300 remote 2001:db8::1",
	"bridge vni add dev vx1 vni 16777214-16777215",
}

// brMasterFix names a br0 port "master", the word link show's parser also
// takes as a keyword (B1). ip has the same keyword, so the setup names the
// device with `name` and `dev`.
var brMasterFix = []string{
	brCreate(false),
	"ip link add name master address 02:00:00:00:01:01 type dummy && ip link set dev master addrgenmode none" +
		" && ip link set dev master master br0 && ip link set dev master up && ip link set br0 up",
}

func clonePre(p []string) []string { return append([]string(nil), p...) }

// brKind marks a command value positive, boundary or corner.
type brKind int

const (
	brPos brKind = iota
	brBoundary
	brCorner
)

// bc is a command value: its fixture, the bridge words (options such as
// -s or -com included), and the fixture's setup. The value's name is the
// fixture and the words, so every row's facts name what it ran.
func bc(kind brKind, fix, words string, pre []string) value {
	name := fix + ":" + strings.ReplaceAll(words, " ", "_")
	facts := f("fix", fix, "words", words)
	var val value
	switch kind {
	case brBoundary:
		val = bv(name, words, facts)
	case brCorner:
		val = cv(name, words, facts)
	default:
		val = v(name, words, facts)
	}
	val.Pre = pre
	return val
}

// brBad is a refused command in a fixture: its words and expectation.
func brBad(fix, words string, pre []string, exp expect, why string) badValue {
	val := v("ng:"+fix+":"+strings.ReplaceAll(words, " ", "_"), words, f("fix", fix, "words", words))
	val.Pre = pre
	return badValue{Axis: "cmd", Value: val, Expect: exp, Why: why}
}

// bridgeSpace: bridge's read-only show commands over nine namespace
// fixtures, each plain and -j.
func bridgeSpace() *space {
	ack := expect{RC: intp(255), RefusedBy: refAckThenFail}
	tool := expect{RC: intp(255), RefusedBy: refIproute2}
	sp := &space{
		Name: "bridge",
		Axes: []axis{
			{Name: "opt", Values: []value{
				v("plain", "", f("json", "0")),
				v("json", "-j", f("json", "1")),
			}},
			{Name: "cmd", Values: []value{
				// bare: modules loaded, no bridge. Every dump is empty;
				// vlan, vni and mst print only their headers.
				bc(brBoundary, "bare", "link show", nil),
				bc(brBoundary, "bare", "vlan show", nil),
				bc(brCorner, "bare", "vlan sh", nil), // matches(): a prefix is show
				bc(brBoundary, "bare", "vlan global show", nil),
				bc(brBoundary, "bare", "vni show", nil),
				bc(brBoundary, "bare", "mst show", nil),
				bc(brBoundary, "bare", "-d vlan show", nil),
				bc(brBoundary, "bare", "-s vlan show", nil),
				bc(brBoundary, "bare", "-s vni show", nil),
				// br: two ports, VLANs, d2's port options.
				bc(brPos, "br", "link show", brFix),
				bc(brPos, "br", "link show dev d1", brFix),
				bc(brPos, "br", "link show master br0", brFix),
				bc(brPos, "br", "-d link show", brFix),
				bc(brPos, "br", "vlan show", brFix),
				bc(brPos, "br", "-com vlan show", brFix),
				bc(brPos, "br", "vlan show dev d1", brFix),
				bc(brPos, "br", "vlan show vid 22", brFix),
				bc(brCorner, "br", "-com vlan show vid 22", brFix), // the range holding 22 (B14)
				bc(brPos, "br", "-d vlan show", brFix),
				bc(brPos, "br", "-s vlan show", brFix),
				bc(brPos, "br", "-s -d vlan show", brFix),
				bc(brCorner, "br", "-c vlan show", brFix),          // -c is -color, not -compressvlans
				bc(brCorner, "br", "link show bogus", brFix),       // unknown word ignored (B2)
				bc(brCorner, "br", "vlan show vid abc", brFix),     // atoi: vid 0, no filter (B5)
				bc(brCorner, "br", "vlan show vid 70000", brFix),   // atoi: out of range, empty (B5)
				bc(brCorner, "br", "vlan show vid 0 vid 5", brFix), // vid 0 escapes duparg (B5)
				bc(brCorner, "br", "-s vlan tunnelshow", brFix),    // takes the stats path (B22)
				bc(brCorner, "br", "vlan show dev br0", brFix),     // the bridge's own entry
				// brglob: per-VLAN global options.
				bc(brPos, "brglob", "vlan global show", brGlobFix),
				bc(brPos, "brglob", "vlan global show dev br0 vid 10", brGlobFix),
				bc(brPos, "brglob", "-d vlan global show", brGlobFix),
				// brmst: MST on, vid 10 in MSTI 3, d1 learning there.
				bc(brPos, "brmst", "mst show", brMstFix),
				bc(brPos, "brmst", "mst show dev d1", brMstFix),
				bc(brPos, "brmst", "vlan global show", brMstFix),
				// vxtun: VLAN-to-tunnel mapping on vx0.
				bc(brPos, "vxtun", "vlan tunnelshow", brVxTunFix),
				bc(brPos, "vxtun", "vlan tunnelshow dev vx0", brVxTunFix),
				bc(brPos, "vxtun", "-d link show dev vx0", brVxTunFix),
				// brwide: d3 with vids 2-4094, compressed only.
				bc(brBoundary, "brwide", "-com vlan show", brWideFix),
				bc(brBoundary, "brwide", "-com vlan show dev d3", brWideFix),
				// vnif: a VNI-filtering vxlan.
				bc(brPos, "vnif", "vni show", vnifFix),
				bc(brPos, "vnif", "vni show dev vx1", vnifFix),
				bc(brPos, "vnif", "-s vni show", vnifFix),
				// brmaster: a port named "master".
				// The dev value is re-read as the keyword, so the third word
				// is the master filter and br0 is dropped: empty, rc 0 (B1).
				bc(brCorner, "brmaster", "link show dev master master br0", brMasterFix),
				bc(brPos, "brmaster", "link show", brMasterFix),
			}},
		},
		Bad: []badValue{
			{Axis: "opt", Value: v("badopt", "-z", nil), Expect: tool,
				Why: "an unknown option exits 255 before any socket"},
			brBad("bare", "badobj", nil, tool, "an unknown object, refused before any request"),
			brBad("bare", "help", nil, tool, "help prints the usage and exits 255"),
			brBad("bare", "link badcmd", nil, ack, "an unknown link sub-command, after ll_init_map"),
			brBad("bare", "vlan badcmd", nil, ack, "an unknown vlan sub-command"),
			brBad("bare", "vni badcmd", nil, ack, "an unknown vni sub-command"),
			brBad("bare", "mst badcmd", nil, ack, "an unknown mst sub-command"),
			brBad("bare", "vni sh", nil, ack, "vni dispatches with strcmp: a prefix is refused"),
			brBad("bare", "vlan global sh", nil, ack, "vlan global dispatches with strcmp (B21)"),
			brBad("bare", "vlan help", nil, ack, "vlan help prints the usage and exits 255"),
			brBad("bare", "link show dev nosuch", nil, expBridgeENODEV, "no such device"),
			brBad("bare", "vlan show dev nosuch", nil, expBridgeENODEV, "no such device"),
			brBad("bare", "vni show dev nosuch", nil, expBridgeENODEV, "no such device"),
			brBad("bare", "mst show dev nosuch", nil, expBridgeENODEV, "no such device"),
			brBad("bare", "link show dev", nil, ack, "dev with no value: the command is incomplete"),
			brBad("br", "link show dev d1 dev d2", brFix, ack, "dev given twice"),
			brBad("br", "vlan show vid 5 vid 6", brFix, ack, "vid given twice"),
			brBad("br", "mst show", brFix, ack,
				"MST off: a port's reply has no IFLA_AF_SPEC and the mst dump aborts (B10)"),
			brBad("br", "-j mst show", brFix, ack, "B10 under -j"),
			brBad("brmstnov", "mst show dev d1", brMstNoVlanFix, ack,
				"d2 has no VLAN, so no IFLA_AF_SPEC: the dump aborts before the dev filter (B10)"),
			brBad("br", "vni show dev d1", brFix,
				expect{RC: intp(1), Errno: "EINVAL", Extack: "The device is not a vxlan device", RefusedBy: refKernel},
				"the vnifilter dump of a non-vxlan device; bridge prints \"Dump ternminated\" (B3)"),
			brBad("br", "vlan show dev if999", brFix,
				expect{RC: intp(0), Errno: "ENODEV", RefusedBy: refKernelIgnored},
				"if999 falls through to ll_idx_a2n as ifindex 999 (B24)"),
			brBad("brmaster", "link show dev master", brMasterFix, ack,
				"the device named master is re-read as the master keyword (B1)"),
			brBad("brmaster", "-j link show dev master", brMasterFix, ack, "B1 under -j"),
			brBad("brmaster", "link show master br0 dev master", brMasterFix, ack,
				"after dev, its value master is re-read as the keyword, which then has no value (B1)"),
		},
	}
	sp.Build = func(vals []value) row {
		var optArg, cmdArg string
		var pre []string
		for i, val := range vals {
			pre = append(pre, val.Pre...)
			switch i {
			case 0:
				optArg = val.Arg
			case 1:
				cmdArg = val.Arg
			}
		}
		r := row{
			Cmd:     strings.TrimSpace("bridge " + joinArgs(optArg, cmdArg)),
			Proto:   "route",
			Modules: bridgeModules,
			Expect:  expect{RC: intp(0), RefusedBy: refNone},
			Axes:    mergeFacts(vals),
		}
		for _, p := range pre {
			r.Pre = append(r.Pre, preStep{Cmd: p})
		}
		return r
	}
	// A bad value replaces one axis of the first plain row: a bad command
	// carries no -j unless its words do, and a bad option runs
	// `bridge -z link show`.
	sp.NegBase = func(vals []value) bool {
		return pickOf(sp.Axes, vals)["opt"] == "plain"
	}
	return sp
}
