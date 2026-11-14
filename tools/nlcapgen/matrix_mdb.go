package main

import "strings"

// The bridge mdb spaces (W5b). iproute2's side is bridge/mdb.c mdb_modify:
// add is RTM_NEWMDB with NLM_F_CREATE|EXCL, replace CREATE|REPLACE and del
// RTM_DELMDB. It sends MDBA_SET_ENTRY (struct br_mdb_entry: the port's
// index, the state, the VLAN and the group) and, when any of src,
// filter_mode, source_list, proto, dst, dst_port, vni, src_vni or via is
// given, MDBA_SET_ENTRY_ATTRS. permanent sets MDB_PERMANENT on an add only.
// do_mdb dumps the links first (ll_init_map), so a refused argument is
// ack-then-fail, as in bridge fdb.
//
// The kernel's side is rtnl_mdb_add and rtnl_mdb_del (net/core/rtnetlink.c),
// which validate the entry (rtnl_validate_mdb_entry) and hand it to the
// device's ndo_mdb_add: br_mdb_add (net/bridge/br_mdb.c) or vxlan_mdb_add
// (drivers/net/vxlan/vxlan_mdb.c). A dummy has none (EOPNOTSUPP).

// mdbBrSetup: d0 is a port of br0, which filters VLANs (1 and 10 on both)
// and snoops multicast (the default). d9 is no port; d2 is br1's.
const mdbBrSetup = "ip link add br0 type bridge vlan_filtering 1 && ip link add d0 type dummy && ip link set d0 master br0" +
	" && ip link set d0 up && ip link set br0 up && bridge vlan add vid 10 dev d0 && bridge vlan add vid 10 dev br0 self" +
	" && ip link add d9 type dummy && ip link add br1 type bridge && ip link add d2 type dummy && ip link set d2 master br1" +
	" && ip link set d2 up && ip link set br1 up"

// mdbVxSetup: vx0 is a VXLAN with VNI 100, port 4789 and an IPv4 local
// address, over d1.
const mdbVxSetup = "ip link add d1 type dummy && ip link set d1 up && ip addr add 192.0.2.10/24 dev d1" +
	" && ip link add vx0 type vxlan id 100 dstport 4789 local 192.0.2.10 nolearning && ip link set vx0 up"

// expBridgeERANGE is a value a policy's range refuses.
var expBridgeERANGE = &expect{RC: intp(255), Errno: "ERANGE", RefusedBy: refKernel}

var mdbVerbs = axis{Name: "verb", Values: []value{
	v("add", "add", f("verb", "add")),
	v("replace", "replace", f("verb", "replace")),
	v("del", "del", f("verb", "del")),
}}

// mdbProtos: MDBE_ATTR_RTPROT, which both policies want at least
// RTPROT_STATIC (4).
var mdbProtos = axis{Name: "proto", Values: []value{
	v("absent", "", nil),
	v("static", "proto static", f("proto", "4")),
	v("bgp", "proto bgp", f("proto", "186")),
	cv("200", "proto 200", f("proto", "200")),
}}

// mdbBadProtos: proto kernel (2) is under the policy's minimum, and a
// name rt_protos lacks is refused by bridge.
var mdbBadProtos = []badValue{
	{Axis: "proto", Value: v("kernel", "proto kernel", f("proto", "2")), Expect: *expBridgeERANGE,
		Why: "a protocol under RTPROT_STATIC, which the policy's minimum refuses"},
	{Axis: "proto", Value: v("nosuch", "proto nosuchproto", nil), Expect: expBridgeArgs,
		Why: "a protocol name rt_protos lacks (Invalid protocol value)"},
}

func mdbBrSpace() *space {
	sp := &space{
		Name: "mdbbr",
		Axes: []axis{
			mdbVerbs,
			{Name: "dev", Values: []value{v("br0", "dev br0", f("dev", "br0"))}},
			// bport: a port (d0) or the bridge itself, a host join.
			{Name: "bport", Values: []value{
				v("d0", "port d0", f("bport", "d0")),
				v("br0", "port br0", f("bport", "br0")),
			}},
			{Name: "grp", Values: []value{
				v("v4", "grp 239.1.1.1", f("grp", "239.1.1.1")),
				v("v6", "grp ff0e::1", f("grp", "ff0e::1")),
				cv("l2", "grp 01:00:5e:01:01:01", f("grp", "01:00:5e:01:01:01")),
			}},
			// state: MDB_TEMPORARY 0 unless permanent; temp sets nothing.
			{Name: "state", Values: []value{
				v("absent", "", f("state", "0")),
				v("permanent", "permanent", f("state", "1")),
				cv("temp", "temp", f("state", "0")),
			}},
			// vid: br_mdb_add installs an entry without a VLAN on each of
			// the port's VLANs; one the port lacks is taken as given.
			{Name: "vid", Values: []value{
				v("absent", "", f("vid", "0")),
				v("1", "vid 1", f("vid", "1")),
				v("10", "vid 10", f("vid", "10")),
				bv("4094", "vid 4094", f("vid", "4094")),
			}},
			{Name: "src", Values: []value{
				v("absent", "", nil),
				v("v4", "src 192.0.2.1", f("src", "192.0.2.1")),
				cv("v6", "src 2001:db8::1", f("src", "2001:db8::1")),
			}},
			// mode: MDBE_ATTR_GROUP_MODE, MCAST_EXCLUDE 0 or MCAST_INCLUDE 1.
			{Name: "mode", Values: []value{
				v("absent", "", nil),
				v("exclude", "filter_mode exclude", f("mode", "0")),
				v("include", "filter_mode include", f("mode", "1")),
			}},
			{Name: "srclist", Values: []value{
				v("absent", "", nil),
				v("one", "source_list 192.0.2.5", f("srclist", "192.0.2.5")),
				cv("two", "source_list 192.0.2.5,192.0.2.6", f("srclist", "192.0.2.5,192.0.2.6")),
			}},
			mdbProtos,
		},
		Bad: append([]badValue{
			{Axis: "dev", Value: v("d0", "dev d0", f("dev", "d0")), Expect: *expBridgeEOPNOTSUPP,
				Why: "a dummy, which has no ndo_mdb_add (Device does not support MDB operations)"},
			{Axis: "dev", Value: v("nosuch", "dev nosuch0", nil), Expect: expBridgeENODEV,
				Why: "a device that does not exist, which ll_link_get asks the kernel for"},
			{Axis: "bport", Value: v("d9", "port d9", f("bport", "d9")), Expect: *expBridgeEINVAL,
				Why: "a port that is no bridge port (Net device is not a bridge port)"},
			{Axis: "bport", Value: v("d2", "port d2", f("bport", "d2")), Expect: *expBridgeEINVAL,
				Why: "another bridge's port (Port belongs to a different bridge device)"},
			{Axis: "grp", Value: v("local", "grp 224.0.0.5", f("grp", "224.0.0.5")), Expect: *expBridgeEINVAL,
				Why: "a link-local IPv4 group (IPv4 entry group address is local multicast)"},
			{Axis: "grp", Value: v("allnodes", "grp ff02::1", f("grp", "ff02::1")), Expect: *expBridgeEINVAL,
				Why: "the IPv6 all-nodes group (IPv6 entry group address is link-local all nodes)"},
			{Axis: "grp", Value: v("uni", "grp 192.0.2.1", f("grp", "192.0.2.1")), Expect: *expBridgeEINVAL,
				Why: "a unicast IPv4 group (IPv4 entry group address is not multicast or 0.0.0.0)"},
			{Axis: "grp", Value: v("zero", "grp 0.0.0.0", f("grp", "0.0.0.0")), Expect: *expBridgeEINVAL,
				Why: "the all-zeros IPv4 group, which only a VXLAN takes (IPv4 entry group address 0.0.0.0 is not allowed)"},
			{Axis: "grp", Value: v("l2uni", "grp 02:00:00:00:00:01", f("grp", "02:00:00:00:00:01")), Expect: *expBridgeEINVAL,
				Why: "a unicast L2 group (L2 entry group is not multicast)"},
			{Axis: "grp", Value: v("word", "grp nonsense", nil), Expect: expBridgeArgs, Why: "a group that is no address (Invalid address)"},
			{Axis: "vid", Value: v("4095", "vid 4095", f("vid", "4095")), Expect: *expBridgeEINVAL,
				Why: "VLAN 4095, VLAN_VID_MASK (Invalid entry VLAN id)"},
			{Axis: "vid", Value: v("65535", "vid 65535", f("vid", "65535")), Expect: *expBridgeEINVAL,
				Why: "a VLAN atoi makes -1, sent as 65535 (Invalid entry VLAN id)"},
			{Axis: "src", Value: v("mcast", "src 239.0.0.1", f("src", "239.0.0.1")), Expect: *expBridgeEINVAL,
				Why: "a multicast source (IPv4 multicast source address is not allowed)"},
			{Axis: "src", Value: v("word", "src nonsense", nil), Expect: expBridgeArgs, Why: "a source that is no address"},
			{Axis: "mode", Value: v("word", "filter_mode both", nil), Expect: expBridgeArgs, Why: "a filter mode that is neither include nor exclude"},
			{Axis: "srclist", Value: v("word", "source_list 192.0.2.5,nonsense", nil), Expect: expBridgeArgs,
				Why: "a source list with an entry that is no address"},
		}, mdbBadProtos...),
	}
	sp.Build = func(vals []value) row { return buildMdb(sp, vals, mdbBrSetup) }
	sp.Judge = mdbBrJudge
	sp.NegBase = func(vals []value) bool {
		p := pickOf(sp.Axes, vals)
		return p["verb"] == "add" && p["bport"] == "d0" && p["grp"] == "v4"
	}
	sp.Derive = func(pos []row) []row {
		return append(deriveBridgeDupGone(sp.Name, pos, "EINVAL"), mdbBrAllVlans(sp))
	}
	return sp
}

// mdbFamilyOf is a value's address family: "4", "6", or "" for an L2 group
// or nothing.
func mdbFamilyOf(axisVal string) string {
	switch axisVal {
	case "v4", "zero", "one", "two":
		return "4"
	case "v6", "v6zero":
		return "6"
	}
	return ""
}

// mdbAttrsProblem is br_mdb_config_attrs_init's verdict on the entry's
// attributes (all EINVAL), or "" if it takes them.
func mdbAttrsProblem(p map[string]string, host bool) string {
	sg := p["src"] != "absent"
	starG := !sg && p["grp"] != "l2"
	mode, list := p["mode"] != "absent", p["srclist"] != "absent"
	switch {
	case sg && mdbFamilyOf(p["src"]) != mdbFamilyOf(p["grp"]):
		return "a source of another family than the group, or on an L2 group"
	case mode && host:
		return "a filter mode on a host group"
	case mode && !starG:
		return "a filter mode on an entry that is no (*, G)"
	case list && host:
		return "a source list on a host group"
	case list && !starG:
		return "a source list on an entry that is no (*, G)"
	case list && !mode:
		return "a source list without a filter mode"
	case list && mdbFamilyOf(p["grp"]) != "4":
		return "an IPv4 source list on an IPv6 group (IPv6 invalid source address length)"
	case !list && p["mode"] == "include":
		return "(*, G) INCLUDE with an empty source list"
	case p["proto"] != "absent" && host:
		return "a protocol on a host group"
	}
	return ""
}

// mdbBrAdd predicts br_mdb_add: the attributes, then the host-join and L2
// checks. A second add of the same entry is EEXIST (the derived dup rows).
func mdbBrAdd(p map[string]string) (*expect, string) {
	host := p["bport"] == "br0"
	l2 := p["grp"] == "l2"
	if why := mdbAttrsProblem(p, host); why != "" {
		return expBridgeEINVAL, why
	}
	switch {
	case host && !l2 && p["state"] == "permanent":
		return expBridgeEINVAL, "a permanent host group (Flags are not allowed for host groups)"
	case host && !l2 && p["src"] != "absent":
		return expBridgeEINVAL, "a host join with a source (Groups with sources cannot be manually host joined)"
	case l2 && p["state"] != "permanent":
		return expBridgeEINVAL, "an L2 entry that is not permanent (Only permanent L2 entries allowed)"
	}
	return nil, ""
}

// mdbBrJudge: a delete is accepted iff its add was, since its pre step is
// that add; otherwise br_mdb_del refuses the attributes or finds nothing,
// both EINVAL (__br_mdb_del starts from -EINVAL).
func mdbBrJudge(p map[string]string) (*expect, string) {
	if p["verb"] != "del" {
		return mdbBrAdd(p)
	}
	if exp, _ := mdbBrAdd(p); exp == nil {
		return nil, ""
	}
	return expBridgeEINVAL, "a delete of an entry its add could not create (__br_mdb_del finds nothing: EINVAL)"
}

// mdbBrAllVlans is a delete without a VLAN after an add in VLAN 1 only.
// br_mdb_del then deletes on each of d0's VLANs and returns the last one's
// result: VLAN 1's entry goes, but VLAN 10 has none, so the delete reports
// EINVAL though it removed the entry.
func mdbBrAllVlans(sp *space) row {
	r := sp.Build(sp.assign(map[string]string{"verb": "del", "vid": "absent", "src": "absent", "mode": "absent",
		"srclist": "absent", "proto": "absent", "state": "absent"}))
	r.Pre = append(r.Pre[:1:1], preStep{Cmd: "bridge mdb add dev br0 port d0 grp 239.1.1.1 vid 1"})
	r.ID = sp.Name + "/del-all-vlans"
	r.Desc = "corner: a delete without a VLAN after an add in VLAN 1 removes it, but reports VLAN 10's EINVAL"
	r.Expect = *expBridgeEINVAL
	return r
}

func mdbVxSpace() *space {
	sp := &space{
		Name: "mdbvx",
		Axes: []axis{
			mdbVerbs,
			{Name: "dev", Values: []value{v("vx0", "dev vx0", f("dev", "vx0"))}},
			{Name: "bport", Values: []value{v("vx0", "port vx0", f("bport", "vx0"))}},
			// grp: a VXLAN takes the all-zeros groups, its catch-all.
			{Name: "grp", Values: []value{
				v("v4", "grp 239.1.1.1", f("grp", "239.1.1.1")),
				v("zero", "grp 0.0.0.0", f("grp", "0.0.0.0")),
				cv("v6", "grp ff0e::1", f("grp", "ff0e::1")),
				cv("v6zero", "grp ::", f("grp", "::")),
			}},
			// state: vxlan_mdb_config_init wants an add or replace permanent.
			{Name: "state", Values: []value{
				v("permanent", "permanent", f("state", "1")),
				cv("absent", "", f("state", "0")),
			}},
			{Name: "vid", Values: []value{v("absent", "", f("vid", "0"))}},
			// dst: MDBE_ATTR_DST, which vxlan_mdb_config_attrs_init requires.
			{Name: "dst", Values: []value{
				v("v4", "dst 192.0.2.1", f("dst", "192.0.2.1")),
				cv("v6", "dst 2001:db8::1", f("dst", "2001:db8::1")),
				cv("absent", "", nil),
			}},
			// dport: MDBE_ATTR_DST_PORT, a host-order u16; 0 is sent.
			{Name: "dport", Values: []value{
				v("absent", "", nil),
				v("4790", "dst_port 4790", f("dport", "4790")),
				bv("0", "dst_port 0", f("dport", "0")),
				bv("65535", "dst_port 65535", f("dport", "65535")),
			}},
			{Name: "vni", Values: []value{
				v("absent", "", nil),
				v("200", "vni 200", f("vni", "200")),
				bv("0", "vni 0", f("vni", "0")),
				bv("max", "vni 16777215", f("vni", "16777215")),
			}},
			{Name: "srcvni", Values: []value{
				v("absent", "", nil),
				v("own", "src_vni 100", f("src_vni", "100")),
				cv("other", "src_vni 300", f("src_vni", "300")),
			}},
			{Name: "via", Values: []value{
				v("absent", "", nil),
				v("d1", "via d1", f("via", "d1")),
			}},
			{Name: "src", Values: []value{
				v("absent", "", nil),
				v("v4", "src 192.0.2.1", f("src", "192.0.2.1")),
			}},
			{Name: "mode", Values: []value{
				v("absent", "", nil),
				v("exclude", "filter_mode exclude", f("mode", "0")),
				v("include", "filter_mode include", f("mode", "1")),
			}},
			{Name: "srclist", Values: []value{
				v("absent", "", nil),
				v("one", "source_list 192.0.2.5", f("srclist", "192.0.2.5")),
			}},
			mdbProtos,
		},
		Bad: append([]badValue{
			{Axis: "bport", Value: v("d1", "port d1", f("bport", "d1")), Expect: *expBridgeEINVAL,
				Why: "a port other than the VXLAN (Port net device must be the VXLAN net device)"},
			{Axis: "grp", Value: v("l2", "grp 01:00:5e:01:01:01", f("grp", "01:00:5e:01:01:01")), Expect: *expBridgeEINVAL,
				Why: "an L2 group (Group address must be an IPv4 / IPv6 address)"},
			{Axis: "grp", Value: v("uni", "grp 192.0.2.1", f("grp", "192.0.2.1")), Expect: *expBridgeEINVAL,
				Why: "a unicast IPv4 group (IPv4 entry group address is not multicast or 0.0.0.0)"},
			{Axis: "vid", Value: v("10", "vid 10", f("vid", "10")), Expect: *expBridgeEINVAL,
				Why: "a VLAN on a VXLAN entry (VID must not be specified)"},
			{Axis: "dst", Value: v("word", "dst nonsense", nil), Expect: expBridgeArgs, Why: "an underlay destination that is no address"},
			{Axis: "dport", Value: v("65536", "dst_port 65536", nil), Expect: expBridgeArgs, Why: "a port past u16 (Invalid destination port)"},
			{Axis: "dport", Value: v("nosuch", "dst_port nosuchsvc", nil), Expect: expBridgeArgs,
				Why: "a port that is no number and no UDP service"},
			{Axis: "vni", Value: v("2to24", "vni 16777216", f("vni", "16777216")), Expect: *expBridgeERANGE,
				Why: "a VNI past 24 bits, which mdb_parse_vni sends and the policy's range refuses"},
			{Axis: "srcvni", Value: v("2to24", "src_vni 16777216", f("src_vni", "16777216")), Expect: *expBridgeERANGE,
				Why: "a source VNI past 24 bits, which the policy's range refuses"},
			{Axis: "via", Value: v("nosuch", "via nosuch0", nil), Expect: expBridgeENODEV,
				Why: "a via device that does not exist, which ll_link_get asks the kernel for"},
			{Axis: "src", Value: v("zero", "src 0.0.0.0", f("src", "0.0.0.0")), Expect: *expBridgeEINVAL,
				Why: "the all-zeros source (IPv4 all-zeros source address is not allowed)"},
		}, mdbBadProtos...),
	}
	sp.Build = func(vals []value) row { return buildMdb(sp, vals, mdbVxSetup) }
	sp.Judge = mdbVxJudge
	sp.NegBase = func(vals []value) bool {
		p := pickOf(sp.Axes, vals)
		return p["verb"] == "add" && p["grp"] == "v4" && p["state"] == "permanent" && p["dst"] == "v4" && p["src"] == "absent"
	}
	sp.Derive = func(pos []row) []row { return deriveBridgeDupGone(sp.Name, pos, "ENOENT") }
	return sp
}

// mdbVxAttrsProblem is vxlan_mdb_config_attrs_init's verdict (all EINVAL),
// or "" if it takes the attributes.
func mdbVxAttrsProblem(p map[string]string) string {
	sg := p["src"] != "absent"
	zero := p["grp"] == "zero" || p["grp"] == "v6zero"
	mode, list := p["mode"] != "absent", p["srclist"] != "absent"
	switch {
	case p["dst"] == "absent":
		return "no underlay destination (Missing remote destination IP address)"
	case sg && mdbFamilyOf(p["src"]) != mdbFamilyOf(p["grp"]):
		return "an IPv4 source on an IPv6 group (IPv6 invalid source address length)"
	case sg && zero:
		return "a source on the all-zeros group (Source cannot be specified for the all-zeros entry)"
	case mode && sg:
		return "a filter mode on an (S, G) entry"
	case list && sg:
		return "a source list on an (S, G) entry"
	case list && !mode:
		return "a source list without a filter mode"
	case list && mdbFamilyOf(p["grp"]) != "4":
		return "an IPv4 source list on an IPv6 group (IPv6 invalid source address length)"
	case !list && p["mode"] == "include":
		return "(*, G) INCLUDE with an empty source list"
	}
	return ""
}

// mdbVxJudge predicts vxlan_mdb_add and vxlan_mdb_del. An add or replace
// must be permanent (checked first); a delete ignores the state, so after
// an add refused only for that it finds nothing (ENOENT).
func mdbVxJudge(p map[string]string) (*expect, string) {
	attrs := mdbVxAttrsProblem(p)
	if p["verb"] != "del" {
		if p["state"] != "permanent" {
			return expBridgeEINVAL, "an entry that is not permanent (MDB entry must be permanent)"
		}
		if attrs != "" {
			return expBridgeEINVAL, attrs
		}
		return nil, ""
	}
	switch {
	case attrs != "":
		return expBridgeEINVAL, attrs
	case p["state"] != "permanent":
		return expBridgeENOENT, "a delete of an entry its add could not create (Did not find MDB entry)"
	}
	return nil, ""
}

// buildMdb makes a space's row: bridge mdb VERB and the values' arguments,
// read back with bridge mdb show dev DEV. A delete's pre step is its own
// add, when the judge would accept that add.
func buildMdb(sp *space, vals []value, setup string) row {
	args := make([]string, 0, len(vals))
	for _, val := range vals[1:] {
		args = append(args, val.Arg)
	}
	r := row{
		Cmd:    "bridge mdb " + vals[0].Arg + " " + joinArgs(args...),
		Pre:    []preStep{{Cmd: setup}},
		Expect: expect{RC: intp(0), RefusedBy: refNone},
		Axes:   mergeFacts(vals),
	}
	dev := r.Axes["dev"]
	if dev == "" || dev == "d0" {
		dev = "br0" // a device bridge cannot use; dump the bridge instead
	}
	r.Readback = []readback{{Name: "dump", Cmd: "bridge mdb show dev " + dev}}
	if vals[0].Name == "del" {
		add := pickOf(sp.Axes, vals)
		add["verb"] = "add"
		if exp, _ := sp.Judge(add); exp == nil {
			r.Pre = append(r.Pre, preStep{Cmd: strings.Replace(r.Cmd, "bridge mdb del ", "bridge mdb add ", 1)})
		}
	}
	return r
}

// deriveBridgeDupGone is deriveDupGone as bridge reports it (rc 255).
func deriveBridgeDupGone(name string, pos []row, gone string) []row {
	return bridgeRC(deriveDupGone(name, pos, gone, nil))
}

// bridgeRC sets each row's rc to bridge's 255.
func bridgeRC(rows []row) []row {
	for i := range rows {
		rows[i].Expect.RC = intp(255)
	}
	return rows
}
