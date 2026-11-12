package main

import "strings"

// The link attribute spaces: the ip link keywords that send the link
// attributes no linkset or linkadd row does (coverage-status.md, "W4c").
// iproute2's side is ip/iplink.c iplink_parse (master, vrf, nomaster,
// addrgenmode, carrier, netns, index, protodown, protodown_reason,
// link-netnsid), ip/iplink_xdp.c xdp_parse and iplink_prop_mod; the
// kernel's is net/core/rtnetlink.c do_setlink, do_set_proto_down and
// rtnl_linkprop.

// linkAttrSetup adds d0 with a bridge and a VRF beside it to enslave it to.
const linkAttrSetup = linkSetup + " && ip link add br0 type bridge && ip link add v0 type vrf table 10"

func linkAttrSpace() *space {
	sp := &space{
		Name: "linkattr",
		Axes: []axis{
			{Name: "master", Values: []value{
				v("absent", "", nil),
				v("bridge", "master br0", f("master", "br0")),
				cv("vrf", "vrf v0", f("master", "v0")),
				cv("nomaster", "nomaster", f("master", "")),
			}},
			// get_addr_gen_mode's names; IFLA_INET6_ADDR_GEN_MODE nests in
			// IFLA_AF_SPEC's AF_INET6.
			{Name: "addrgenmode", Values: []value{
				v("absent", "", nil),
				v("eui64", "addrgenmode eui64", f("addrgenmode", "0")),
				v("none", "addrgenmode none", f("addrgenmode", "1")),
				cv("random", "addrgenmode random", f("addrgenmode", "3")),
			}},
			// dummy_change_carrier is a dummy's ndo_change_carrier.
			{Name: "carrier", Values: []value{
				v("absent", "", nil),
				v("on", "carrier on", f("carrier", "1")),
				v("off", "carrier off", f("carrier", "0")),
			}},
			// netns names a pid ($$, the row's own shell, so the move is
			// to the namespace d0 is already in, which
			// __dev_change_net_namespace never reaches). index sends
			// IFLA_NEW_IFINDEX, which iplink_parse allows only with netns and
			// do_setlink reads only on a move.
			{Name: "netns", Values: []value{
				v("absent", "", nil),
				cv("self", "netns $$", f("netns", "fd")),
				cv("self-index", "netns $$ index 100", f("netns", "fd", "new_ifindex", "100")),
			}},
			// xdp_delete sends IFLA_XDP_FD -1, with
			// XDP_FLAGS_UPDATE_IF_NOEXIST unless -force, and each mode's
			// flag. xdp_parse takes off or none only as the last argument
			// (deriveLinkAttrCorners), so this axis comes last. Detaching
			// nothing succeeds in every mode, a dummy's native one too.
			{Name: "xdp", Values: []value{
				v("absent", "", nil),
				v("off", "xdp off", f("xdp_flags", "1")),
				cv("generic", "xdpgeneric none", f("xdp_flags", "3")),
				cv("drv", "xdpdrv off", f("xdp_flags", "5")),
			}},
		},
		Bad: []badValue{
			// ll_name_to_index's RTM_GETLINK is refused, then invarg.
			{Axis: "master", Value: v("nosuch", "master nosuch", nil), Expect: expect{RC: intp(255), Errno: "ENODEV", RefusedBy: refKernel},
				Why: "a master the kernel doesn't know: its RTM_GETLINK is ENODEV (Device does not exist)"},
			{Axis: "master", Value: v("vrf-bridge", "vrf br0", nil), Expect: expIproute2InvargAfterDev,
				Why: "a vrf that is a bridge (Not a valid VRF name)"},
			{Axis: "addrgenmode", Value: v("nosuch", "addrgenmode nosuch", nil), Expect: expIproute2Invarg,
				Why: "an unknown address generation mode"},
			{Axis: "addrgenmode", Value: v("stable", "addrgenmode stable_secret", f("addrgenmode", "2")), Expect: *expKernelEINVAL,
				Why: "stable_secret with no secret set (check_stable_privacy)"},
			{Axis: "carrier", Value: v("maybe", "carrier maybe", nil), Expect: expOnOff, Why: "a carrier that isn't on or off"},
			{Axis: "netns", Value: v("index", "index 100", nil), Expect: expIproute2Invarg,
				Why: "an index with no netns (index can be used only when creating devices or when moving device to another netns)"},
			{Axis: "netns", Value: v("nosuch", "netns nosuch", nil), Expect: expIproute2Invarg,
				Why: "a namespace neither named nor a pid (Invalid netns value)"},
			{Axis: "netns", Value: v("linknsid", "link-netnsid 5", f("link_netnsid", "5")), Expect: *expKernelEINVAL,
				Why: "a link namespace id no peer has (Unknown network namespace id)"},
			{Axis: "carrier", Value: v("protodown", "protodown on", f("protodown", "1")), Expect: expect{RC: intp(2), Errno: "ENOTSUP", RefusedBy: refKernel},
				Why: "protodown on a dummy, which doesn't set change_proto_down (Protodown not supported by device)"},
		},
	}
	sp.Build = func(vals []value) row {
		return buildLinkCmd("ip link set dev d0 ", linkAttrSetup, "ip -d link show", vals)
	}
	sp.NegBase = func(vals []value) bool { return vals[0].Name == "absent" && vals[3].Name == "absent" }
	sp.Derive = deriveLinkAttrCorners
	return sp
}

// expOnOff is parse_on_off's refusal: iplink_parse returns its -EINVAL,
// which ip exits with, before sending anything.
var expOnOff = expect{RC: intp(22), RefusedBy: refIproute2}

// deriveLinkAttrCorners adds two rows. An xdp off that isn't last:
// xdp_parse hands it to bpf_parse_and_load_common, which reads off as a
// program mode (What mode is "off"?). And parentdev, which iplink_parse
// sends as IFLA_PARENT_DEV_NAME on a set too; the kernel only ever dumps
// that attribute (rtnl_fill_ifinfo, from the device's bus parent), so the
// set succeeds and changes nothing.
func deriveLinkAttrCorners([]row) []row {
	return []row{{
		ID:       "linkattr/xdp-not-last",
		Desc:     "corner: xdp off before another keyword, which xdp_parse reads as a program mode",
		Cmd:      "ip link set dev d0 xdp off carrier on",
		Pre:      []preStep{{Cmd: linkAttrSetup}},
		Readback: []readback{{Name: "dump", Cmd: "ip -d link show"}},
		Expect:   expIproute2Invarg,
		Axes:     map[string]string{"xdp_flags": "1", "carrier": "1"},
	}, {
		ID:       "linkattr/parentdev",
		Desc:     "corner: parentdev on a set is sent, and the kernel ignores it",
		Cmd:      "ip link set dev d0 parentdev br0",
		Pre:      []preStep{{Cmd: linkAttrSetup}},
		Readback: []readback{{Name: "dump", Cmd: "ip -d link show"}},
		Expect:   expect{RC: intp(0), RefusedBy: refNone},
		Axes:     map[string]string{"parent_dev_name": "br0"},
	}}
}

// linkNSSpace is the forms netns_get_fd takes (lib/namespace.c): a name
// under /var/run/netns, a path, or a pid, each opened and sent as
// IFLA_NET_NS_FD. The name is the row's own namespace, ws<slot>: one that
// a pre step adds is bind-mounted inside ip netns exec's private mount
// namespace and gone by the time the row runs, so ip opens the empty file
// under it and the kernel refuses that (EINVAL), as it does /proc/self/stat
// below. Every form but init names the namespace d0 is already in; init
// (pid 1, the guest's own) is the one real move.
func linkNSSpace() *space {
	sp := &space{
		Name: "linkns",
		Axes: []axis{
			{Name: "target", Values: []value{
				v("pid", "netns $$", f("netns", "fd", "target", "pid")),
				cv("name", "netns $(ip netns identify $$)", f("netns", "fd", "target", "name")),
				cv("self-path", "netns /proc/self/ns/net", f("netns", "fd", "target", "path")),
				v("pid-path", "netns /proc/$$/ns/net", f("netns", "fd", "target", "path")),
				cv("init", "netns 1", f("netns", "fd", "target", "init")),
			}},
		},
		Bad: []badValue{
			{Axis: "target", Value: bv("zero", "netns 0", nil), Expect: expIproute2Invarg,
				Why: "pid 0, which has no /proc entry (Invalid netns value)"},
			{Axis: "target", Value: bv("minus", "netns -1", nil), Expect: expIproute2Invarg,
				Why: "a negative pid (Invalid netns value)"},
			{Axis: "target", Value: v("nopath", "netns /nonexistent", nil), Expect: expIproute2Invarg,
				Why: "a path that doesn't exist and isn't a pid (Invalid netns value)"},
			{Axis: "target", Value: v("notns", "netns /proc/self/stat", f("netns", "fd", "target", "path")), Expect: *expKernelEINVAL,
				Why: "a file that isn't a namespace: ip opens it, and the kernel refuses the descriptor"},
			{Axis: "target", Value: v("dup", "netns $$ netns $$", nil), Expect: expIproute2Invarg,
				Why: "netns twice (duplicate \"netns\")"},
			{Axis: "target", Value: v("linknsid", "netns $$ link-netnsid 5", f("netns", "fd", "target", "pid", "link_netnsid", "5")),
				Expect: *expKernelEINVAL, Why: "a link-netnsid the target namespace doesn't know (Unknown network namespace id)"},
		},
	}
	sp.Build = func(vals []value) row {
		r := buildLinkCmd("ip link set dev d0 ", linkSetup, "ip -d link show", vals)
		// The guest's namespace keeps d0, so a second move there would
		// find the name taken.
		r.Serial = vals[0].Name == "init"
		return r
	}
	sp.Derive = deriveLinkNSAdd
	return sp
}

// deriveLinkNSAdd adds a create into the row's namespace with an index:
// iplink_modify puts index in ifi_index on a create, and sends
// IFLA_NEW_IFINDEX only on a move.
func deriveLinkNSAdd([]row) []row {
	return []row{{
		ID:       "linkns/add-index",
		Desc:     "positive: add with netns and index, which a create sends as ifi_index",
		Cmd:      "ip link add d9 netns $$ index 100 type dummy",
		Pre:      []preStep{{Cmd: linkSetup}},
		Readback: []readback{{Name: "dump", Cmd: "ip -d link show"}},
		Expect:   expect{RC: intp(0), RefusedBy: refNone},
		Axes:     f("netns", "fd", "target", "pid", "ifi_index", "100", "ifname", "d9", "kind", "dummy"),
	}}
}

// linkPDSetup adds a macvlan, m0, which sets change_proto_down.
const linkPDSetup = linkSetup + " && ip link add m0 link d0 type macvlan mode bridge"

func linkPDSpace() *space {
	sp := &space{
		Name: "linkpd",
		Axes: []axis{
			{Name: "protodown", Values: []value{
				v("absent", "", nil),
				v("on", "protodown on", f("protodown", "1")),
				v("off", "protodown off", f("protodown", "0")),
			}},
			// protodown_reason_a2n takes a bit number; the request carries
			// its mask and, when on, its value.
			{Name: "reason", Values: []value{
				v("absent", "", nil),
				v("0-on", "protodown_reason 0 on", f("pd_mask", "1", "pd_value", "1")),
				v("0-off", "protodown_reason 0 off", f("pd_mask", "1", "pd_value", "0")),
				bv("31-on", "protodown_reason 31 on", f("pd_mask", "2147483648", "pd_value", "2147483648")),
			}},
			{Name: "updown", Values: []value{
				v("absent", "", nil),
				v("up", "up", f("up", "1")),
			}},
		},
		Bad: []badValue{
			{Axis: "reason", Value: v("32", "protodown_reason 32 on", nil), Expect: expIproute2Invarg,
				Why: "a reason past PROTODOWN_REASON_NUM_BITS (invalid protodown reason)"},
			{Axis: "protodown", Value: v("maybe", "protodown maybe", nil), Expect: expOnOff,
				Why: "a protodown that isn't on or off"},
		},
	}
	sp.Build = func(vals []value) row {
		return buildLinkCmd("ip link set dev m0 ", linkPDSetup, "ip -d link show dev m0", vals)
	}
	sp.Judge = linkPDJudge
	sp.NegBase = func(vals []value) bool { return vals[1].Name == "absent" }
	return sp
}

// linkPDJudge: do_set_proto_down applies the reason first, then refuses to
// clear protodown while a reason is active.
func linkPDJudge(pick map[string]string) (*expect, string) {
	if pick["protodown"] == "off" && strings.HasSuffix(pick["reason"], "-on") {
		return &expect{RC: intp(2), Errno: "EBUSY", RefusedBy: refKernel}, "protodown off with a reason on (Cannot clear protodown, active reasons)"
	}
	return nil, ""
}

// linkAlt127 is an altname of ALTIFNAMSIZ - 1 characters, the longest
// check_altifname takes.
var linkAlt127 = strings.Repeat("b", 127)

func linkPropSpace() *space {
	sp := &space{
		Name: "linkprop",
		Axes: []axis{
			{Name: "op", Values: []value{
				v("add", "add", f("op", "add")),
				v("del", "del", f("op", "del")),
				v("show", "show", f("op", "show")),
			}},
			{Name: "altname", Values: []value{
				v("short", "altname alt0", f("altname", "alt0")),
				v("long", "altname alternate-name-0", f("altname", "alternate-name-0")),
				bv("127", "altname "+linkAlt127, f("altname", linkAlt127)),
				cv("two", "altname alt1 altname alt2", f("altname", "alt1", "altname2", "alt2")),
				// check_altifname allows a slash, and so does the kernel:
				// netdev_name_node_alt_create checks only that the name is free.
				cv("slash", "altname a/b", f("altname", "a/b")),
			}},
			{Name: "dev", Values: []value{
				v("dev", "dev d0", nil),
				cv("bare", "d0", nil),
			}},
		},
		Bad: []badValue{
			{Axis: "altname", Value: v("128", "altname "+linkAlt127+"b", nil), Expect: expIproute2Invarg,
				Why: "an altname of ALTIFNAMSIZ characters (not a valid altname)"},
			{Axis: "altname", Value: v("taken", "altname br0", f("altname", "br0")),
				Expect: expect{RC: intp(2), Errno: "EEXIST", RefusedBy: refKernel}, Why: "an altname another device is called"},
		},
	}
	sp.Build = buildLinkProp
	sp.NegBase = func(vals []value) bool { return vals[0].Name == "add" }
	sp.Derive = deriveLinkPropAbsent
	return sp
}

// buildLinkCmd assembles one ip link command on a device made by setup.
func buildLinkCmd(prefix, setup, readbackCmd string, vals []value) row {
	args := make([]string, 0, len(vals))
	for _, val := range vals {
		args = append(args, val.Arg)
	}
	return row{
		Cmd:      strings.TrimSpace(prefix + joinArgs(args...)),
		Pre:      []preStep{{Cmd: setup}},
		Readback: []readback{{Name: "dump", Cmd: readbackCmd}},
		Expect:   expect{RC: intp(0), RefusedBy: refNone},
		Axes:     mergeFacts(vals),
	}
}

// linkPropSetup is linkSetup with br0 for the taken-name row.
const linkPropSetup = linkSetup + " && ip link add br0 type bridge"

// buildLinkProp assembles `ip link property add|del ALTNAMES DEV`, or
// `ip link show dev ALTNAME` (iplink_get sends a name longer than
// IFNAMSIZ - 1 as IFLA_ALT_IFNAME). del and show run after an add of the
// same names.
func buildLinkProp(vals []value) row {
	op, alt, dev := vals[0], vals[1], vals[2]
	setup := linkPropSetup
	if op.Name != "add" {
		setup += " && ip link property add dev d0 " + alt.Arg
	}
	cmd := "ip link property " + op.Name + " " + alt.Arg + " " + dev.Arg
	if op.Name == "show" {
		cmd = "ip link show dev " + alt.Facts["altname"]
		if dev.Name == "bare" {
			cmd = "ip link show " + alt.Facts["altname"]
		}
	}
	return row{
		Cmd:      cmd,
		Pre:      []preStep{{Cmd: setup}},
		Readback: []readback{{Name: "dump", Cmd: "ip -d link show dev d0"}},
		Expect:   expect{RC: intp(0), RefusedBy: refNone},
		Axes:     mergeFacts(vals),
	}
}

// deriveLinkPropAbsent adds a delete of an altname d0 lacks (ENOENT) and a
// repeated add (EEXIST).
func deriveLinkPropAbsent(pos []row) []row {
	gone := row{
		ID:       "linkprop/del-absent",
		Desc:     "negative: property del of an altname d0 lacks (ENOENT)",
		Cmd:      "ip link property del dev d0 altname alt9",
		Pre:      []preStep{{Cmd: linkPropSetup}},
		Readback: []readback{{Name: "dump", Cmd: "ip -d link show dev d0"}},
		Expect:   expect{RC: intp(2), Errno: "ENOENT", RefusedBy: refKernel},
		Axes:     map[string]string{"op": "del", "altname": "alt9"},
	}
	out := []row{gone}
	for i := range pos {
		p := &pos[i]
		if p.Axes["op"] != "add" || p.Expect.RC == nil || *p.Expect.RC != 0 {
			continue
		}
		r := *p
		r.Pre = append(append([]preStep{}, p.Pre...), preStep{Cmd: p.Cmd})
		r.ID = "linkprop/dup-1"
		r.Desc = "negative: " + dropPrefix(p.Desc)[1:] + ": added twice (EEXIST)"
		r.Expect = expect{RC: intp(2), Errno: "EEXIST", RefusedBy: refKernel}
		return append(out, r)
	}
	return out
}
