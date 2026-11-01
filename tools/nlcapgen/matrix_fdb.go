package main

import "strings"

// The bridge fdb spaces (W5a). iproute2's side is bridge/fdb.c fdb_modify;
// the kernel's is rtnl_fdb_add and rtnl_fdb_del (net/core/rtnetlink.c),
// which hand a bridge port's NTF_MASTER request to br_fdb_add and
// br_fdb_delete (net/bridge/br_fdb.c), and an NTF_SELF one to the device's
// own ndo_fdb_add: the bridge's br_fdb_add, vxlan_fdb_add
// (drivers/net/vxlan/vxlan_core.c), or ndo_dflt_fdb_add for a dummy, which
// has none. fdb_modify sets NTF_SELF when neither self nor master is named
// and NUD_PERMANENT when no state is, so ndm_flags is never 0 and the
// master path runs only when asked. A refusal by the kernel ends fdb_modify
// with return -1, so bridge exits 255 where ip exits 2.

// fdbBrSetup: d0 is a port of br0, which filters VLANs; both carry VLAN 1
// (the default pvid) and 10. A port forwards as soon as it is up, as STP
// is off, so br_fdb_add's learning-state check passes.
const fdbBrSetup = "ip link add br0 type bridge vlan_filtering 1 && ip link add d0 type dummy && ip link set d0 master br0" +
	" && ip link set d0 up && ip link set br0 up && bridge vlan add vid 10 dev d0 && bridge vlan add vid 10 dev br0 self"

// fdbVxSetup: vx0 is a VXLAN with VNI 100 and an IPv4 local address, so
// its remote family is AF_INET (vxlan_config_validate). Nexthop 10 is an
// fdb group, the only kind vxlan_fdb_nh_update takes; 1 is an fdb nexthop
// but no group, and 2 is no fdb nexthop at all.
const fdbVxSetup = "ip link add d1 type dummy && ip link set d1 up && ip addr add 192.0.2.10/24 dev d1" +
	" && ip link add vx0 type vxlan id 100 dstport 4789 local 192.0.2.10 nolearning && ip link set vx0 up" +
	" && ip nexthop add id 1 via 192.0.2.1 fdb && ip nexthop add id 10 group 1 fdb && ip nexthop add id 2 via 192.0.2.1 dev d1"

var (
	// expBridgeEINVAL and its kin are a kernel refusal as bridge reports it.
	expBridgeEINVAL = &expect{RC: intp(255), Errno: "EINVAL", RefusedBy: "kernel"}
	expBridgeENOENT = &expect{RC: intp(255), Errno: "ENOENT", RefusedBy: "kernel"}
	// expBridgeEOPNOTSUPP is EOPNOTSUPP, which Go names ENOTSUP (both 95).
	expBridgeEOPNOTSUPP = &expect{RC: intp(255), Errno: "ENOTSUP", RefusedBy: "kernel"}
	// expBridgeArgs is bridge refusing its arguments (fdb_modify's return
	// -1, or invarg). do_fdb has already dumped the links (ll_init_map), so
	// the run sent a request and exits 255 after its acknowledgment; no
	// fdb request goes out, which the corpus test checks.
	expBridgeArgs = expect{RC: intp(255), RefusedBy: "ack-then-fail"}
	// expBridgeENODEV is a device name ll_name_to_index cannot resolve:
	// ll_link_get's RTM_GETLINK for it is refused, and bridge stops there.
	expBridgeENODEV = expect{RC: intp(255), Errno: "ENODEV", RefusedBy: "kernel"}
)

// fdbVerbs: do_fdb's add is NLM_F_CREATE|EXCL, append CREATE|APPEND and
// replace CREATE|REPLACE; del is RTM_DELNEIGH, which first adds what it
// deletes.
var fdbVerbs = axis{Name: "verb", Values: []value{
	v("add", "add", f("verb", "add")),
	v("append", "append", f("verb", "append")),
	v("replace", "replace", f("verb", "replace")),
	v("del", "del", f("verb", "del")),
}}

// fdbStates: fdb_modify starts from NUD_NOARP; permanent and local add
// NUD_PERMANENT, static and temp NUD_REACHABLE, and dynamic NUD_REACHABLE
// with NOARP cleared. With neither PERMANENT nor REACHABLE it adds
// PERMANENT.
func fdbStates() axis {
	return axis{Name: "state", Values: []value{
		v("absent", "", f("state", "192")),
		v("permanent", "permanent", f("state", "192")),
		v("static", "static", f("state", "66")),
		v("dynamic", "dynamic", f("state", "2")),
		cv("local", "local", f("state", "192")),
		cv("temp", "temp", f("state", "66")),
	}}
}

func fdbBrSpace() *space {
	sp := &space{
		Name: "fdbbr",
		Axes: []axis{
			fdbVerbs,
			{Name: "mac", Values: []value{
				v("uni", "02:00:00:00:00:01", f("lladdr", "02:00:00:00:00:01")),
				cv("to", "to 02:00:00:00:00:02", f("lladdr", "02:00:00:00:00:02")),
				cv("mcast", "01:00:5e:00:00:01", f("lladdr", "01:00:5e:00:00:01")),
				cv("zero", "00:00:00:00:00:00", f("lladdr", "00:00:00:00:00:00")),
			}},
			{Name: "dev", Values: []value{
				v("port", "dev d0", f("dev", "d0")),
				v("bridge", "dev br0", f("dev", "br0")),
			}},
			// target: the ndm_flags NTF_SELF (2) and NTF_MASTER (4) bits;
			// absent is NTF_SELF.
			{Name: "target", Values: []value{
				v("absent", "", f("target", "2")),
				v("self", "self", f("target", "2")),
				v("master", "master", f("target", "4")),
				cv("both", "master self", f("target", "6")),
			}},
			fdbStates(),
			// flags: NTF_STICKY 0x40, NTF_EXT_LEARNED 0x10, NTF_USE 1 and
			// NTF_ROUTER 0x80, which a bridge ignores.
			{Name: "flags", Values: []value{
				v("absent", "", nil),
				v("sticky", "sticky", f("ntf", "64")),
				v("extern_learn", "extern_learn", f("ntf", "16")),
				cv("use", "use", f("ntf", "1")),
				cv("router", "router", f("ntf", "128")),
			}},
			// vlan: fdb_modify reads it with atoi into a short and sends it
			// when it is not negative, so 65535 is -1 and sends nothing.
			{Name: "vlan", Values: []value{
				v("absent", "", nil),
				v("1", "vlan 1", f("vlan", "1")),
				v("10", "vlan 10", f("vlan", "10")),
				bv("4094", "vlan 4094", f("vlan", "4094")),
				cv("65535", "vlan 65535", nil),
			}},
			// ext: NDA_FDB_EXT_ATTRS, NFEA_ACTIVITY_NOTIFY's FDB_NOTIFY_BIT
			// 1 and FDB_NOTIFY_INACTIVE_BIT 2, and NFEA_DONT_REFRESH.
			{Name: "ext", Values: []value{
				v("absent", "", nil),
				v("notify", "activity_notify", f("notify", "1")),
				v("norefresh", "norefresh", f("norefresh", "1")),
				cv("both", "activity_notify inactive", f("notify", "3")),
				cv("inactive", "inactive", f("notify", "2")),
			}},
		},
		// A delete only finds the entry; its flags and ext attributes
		// change nothing, so it takes neither.
		Excl: []exclusion{
			{A: "verb=del", B: "flags=sticky"}, {A: "verb=del", B: "flags=extern_learn"},
			{A: "verb=del", B: "flags=use"}, {A: "verb=del", B: "flags=router"},
			{A: "verb=del", B: "ext=notify"}, {A: "verb=del", B: "ext=norefresh"},
			{A: "verb=del", B: "ext=both"}, {A: "verb=del", B: "ext=inactive"},
		},
		Bad: []badValue{
			{Axis: "vlan", Value: v("0", "vlan 0", f("vlan", "0")), Expect: *expBridgeEINVAL,
				Why: "VLAN 0, which fdb_vid_parse refuses (invalid vlan id)"},
			{Axis: "vlan", Value: v("4095", "vlan 4095", f("vlan", "4095")), Expect: *expBridgeEINVAL,
				Why: "VLAN 4095, VLAN_VID_MASK (invalid vlan id)"},
			{Axis: "vlan", Value: v("word", "vlan ten", f("vlan", "0")), Expect: *expBridgeEINVAL,
				Why: "a VLAN atoi reads as 0, which is sent and refused (invalid vlan id)"},
			{Axis: "mac", Value: v("short", "02:00:00:00:01", nil), Expect: expBridgeArgs,
				Why: "a MAC of five octets (Invalid mac address)"},
			{Axis: "dev", Value: v("nosuch", "dev nosuch0", nil), Expect: expBridgeENODEV,
				Why: "a device that does not exist, which ll_link_get asks the kernel for (Cannot find device)"},
		},
	}
	sp.Build = func(vals []value) row { return buildFdb(sp, vals, fdbBrSetup) }
	sp.Judge = fdbBrJudge
	// Every bad value is refused before anything else the row asks, so
	// any accepted add on d0 will do.
	sp.NegBase = func(vals []value) bool {
		p := pickOf(sp.Axes, vals)
		return p["verb"] == "add" && p["mac"] == "uni" && p["dev"] == "port"
	}
	sp.Derive = func(pos []row) []row { return deriveFdbDupGone(sp.Name, pos) }
	return sp
}

// fdbBrMaster is whether rtnl_fdb_add takes the master path: NTF_MASTER
// on a bridge port.
func fdbBrMaster(p map[string]string) bool {
	return (p["target"] == "master" || p["target"] == "both") && p["dev"] == "port"
}

// fdbBrSelf is whether it takes the self path.
func fdbBrSelf(p map[string]string) bool { return p["target"] != "master" }

// fdbBrVlanBad is fdb_vid_parse's refusal, before either path.
func fdbBrVlanBad(p map[string]string) bool {
	return p["vlan"] == "0" || p["vlan"] == "4095" || p["vlan"] == "word"
}

// fdbBrNotPerm is a state without NUD_PERMANENT.
func fdbBrNotPerm(p map[string]string) bool {
	return p["state"] == "static" || p["state"] == "temp" || p["state"] == "dynamic"
}

// fdbBrVid is whether a VLAN is sent and is not 0.
func fdbBrVid(p map[string]string) bool {
	return p["vlan"] != "absent" && p["vlan"] != "65535" && p["vlan"] != "0" && p["vlan"] != "word"
}

// fdbBridgeAdd is br_fdb_add on dev, the port (p != NULL) or the bridge:
// the zero MAC, then a VLAN br0 and d0 lack (4094), then __br_fdb_add:
// NTF_USE needs a port, NTF_EXT_LEARNED towards the bridge must be
// permanent, and fdb_add_entry refuses a non-permanent entry towards the
// bridge, a sticky permanent one, and the inactive bit without the notify
// bit.
func fdbBridgeAdd(p map[string]string, port bool) (*expect, string) {
	switch {
	case p["mac"] == "zero":
		return expBridgeEINVAL, "the zero MAC (RTM_NEWNEIGH with invalid ether address)"
	case p["vlan"] == "4094":
		return expBridgeEINVAL, "a VLAN the device lacks (RTM_NEWNEIGH with unconfigured vlan)"
	case p["flags"] == "use":
		if port {
			return nil, ""
		}
		return expBridgeEINVAL, "use towards the bridge (RTM_NEWNEIGH with NTF_USE is not supported)"
	case p["flags"] == "extern_learn":
		if port || !fdbBrNotPerm(p) {
			return nil, ""
		}
		return expBridgeEINVAL, "extern_learn towards the bridge, not permanent (FDB entry towards bridge must be permanent)"
	case !port && fdbBrNotPerm(p):
		return expBridgeEINVAL, "an entry towards the bridge that is not permanent (RTM_NEWNEIGH without NUD_PERMANENT)"
	case p["flags"] == "sticky" && !fdbBrNotPerm(p):
		return expBridgeEINVAL, "a sticky permanent entry"
	case p["ext"] == "inactive":
		return expBridgeEINVAL, "the inactive bit without activity_notify"
	}
	return nil, ""
}

// fdbBrAdd is the add's verdict: rtnl_fdb_add's VLAN check, the master
// path, then the self path, which on d0 is ndo_dflt_fdb_add (permanent
// only, no VLAN) and on br0 is br_fdb_add again. master on br0 takes
// neither path (EOPNOTSUPP).
func fdbBrAdd(p map[string]string) (*expect, string) {
	if fdbBrVlanBad(p) {
		return expBridgeEINVAL, "VLAN 0 or 4095 (invalid vlan id)"
	}
	if fdbBrMaster(p) {
		if exp, why := fdbBridgeAdd(p, true); exp != nil {
			return exp, why
		}
	}
	if !fdbBrSelf(p) {
		if p["dev"] == "bridge" {
			return expBridgeEOPNOTSUPP, "master on the bridge itself, which is no port"
		}
		return nil, ""
	}
	if p["dev"] == "bridge" {
		return fdbBridgeAdd(p, false)
	}
	switch {
	case fdbBrNotPerm(p):
		return expBridgeEINVAL, "self on a dummy, whose default FDB takes only permanent entries"
	case fdbBrVid(p):
		return expBridgeEINVAL, "self on a dummy with a VLAN (vlans aren't supported yet for dev_uc|mc_add())"
	}
	return nil, ""
}

// fdbBrDelGone is a delete's verdict when there is nothing to delete:
// rtnl_fdb_del checks the VLAN, br_fdb_delete finds nothing (ENOENT), and
// ndo_dflt_fdb_del refuses a non-permanent state before it looks.
func fdbBrDelGone(p map[string]string) (*expect, string) {
	switch {
	case fdbBrVlanBad(p):
		return expBridgeEINVAL, "VLAN 0 or 4095 (invalid vlan id)"
	case fdbBrMaster(p):
		return expBridgeENOENT, "a delete of an entry its add could not create"
	case !fdbBrSelf(p):
		return expBridgeEOPNOTSUPP, "master on the bridge itself, which is no port"
	case p["dev"] == "port" && fdbBrNotPerm(p):
		return expBridgeEINVAL, "self on a dummy, whose default FDB deletes only permanent entries"
	}
	return expBridgeENOENT, "a delete of an entry its add could not create"
}

// fdbBrJudge: a delete follows its add, which buildFdb runs first when it
// is accepted.
func fdbBrJudge(p map[string]string) (*expect, string) {
	if p["verb"] != "del" {
		return fdbBrAdd(p)
	}
	if exp, _ := fdbBrAdd(p); exp == nil {
		return nil, ""
	}
	return fdbBrDelGone(p)
}

func fdbVxSpace() *space {
	sp := &space{
		Name: "fdbvx",
		Axes: []axis{
			fdbVerbs,
			{Name: "mac", Values: []value{
				v("uni", "02:00:00:00:00:01", f("lladdr", "02:00:00:00:00:01")),
				v("zero", "00:00:00:00:00:00", f("lladdr", "00:00:00:00:00:00")),
				cv("mcast", "01:00:5e:00:00:01", f("lladdr", "01:00:5e:00:00:01")),
			}},
			// remote: vxlan_fdb_add wants NDA_DST or NDA_NH_ID.
			{Name: "remote", Values: []value{
				v("dst4", "dst 192.0.2.1", f("dst", "192.0.2.1")),
				v("nhid", "nhid 10", f("nhid", "10")),
				cv("dst6", "dst 2001:db8::1", f("dst", "2001:db8::1")),
				cv("absent", "", nil),
				cv("nhsingle", "nhid 1", f("nhid", "1")),
				cv("nhnotfdb", "nhid 2", f("nhid", "2")),
				cv("nhgone", "nhid 99", f("nhid", "99")),
			}},
			// port: fdb_modify sends NDA_PORT only when it is not 0.
			{Name: "port", Values: []value{
				v("absent", "", nil),
				v("4790", "port 4790", f("port", "4790")),
				bv("0", "port 0", nil),
				bv("65535", "port 65535", f("port", "65535")),
				cv("hex", "port 0x12b6", f("port", "4790")),
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
			{Name: "state", Values: []value{
				v("absent", "", f("state", "192")),
				v("static", "static", f("state", "66")),
				cv("dynamic", "dynamic", f("state", "2")),
			}},
		},
		Excl: []exclusion{
			// fdb_modify refuses an nhid with dst, port or vni before it
			// sends; it does not check via, which the kernel refuses.
			{A: "remote=nhid", B: "port=4790", Expect: &expBridgeArgs, Why: "nhid with port (dst, port, vni are mutually exclusive with nhid)"},
			{A: "remote=nhid", B: "vni=200", Expect: &expBridgeArgs, Why: "nhid with vni (dst, port, vni are mutually exclusive with nhid)"},
			{A: "remote=nhid", B: "port=65535"}, {A: "remote=nhid", B: "port=hex"},
			{A: "remote=nhid", B: "vni=0"}, {A: "remote=nhid", B: "vni=max"},
			{A: "remote=nhsingle", B: "port=4790"}, {A: "remote=nhsingle", B: "port=65535"}, {A: "remote=nhsingle", B: "port=hex"},
			{A: "remote=nhsingle", B: "vni=200"}, {A: "remote=nhsingle", B: "vni=0"}, {A: "remote=nhsingle", B: "vni=max"},
			{A: "remote=nhnotfdb", B: "port=4790"}, {A: "remote=nhnotfdb", B: "port=65535"}, {A: "remote=nhnotfdb", B: "port=hex"},
			{A: "remote=nhnotfdb", B: "vni=200"}, {A: "remote=nhnotfdb", B: "vni=0"}, {A: "remote=nhnotfdb", B: "vni=max"},
			{A: "remote=nhgone", B: "port=4790"}, {A: "remote=nhgone", B: "port=65535"}, {A: "remote=nhgone", B: "port=hex"},
			{A: "remote=nhgone", B: "vni=200"}, {A: "remote=nhgone", B: "vni=0"}, {A: "remote=nhgone", B: "vni=max"},
		},
		Bad: []badValue{
			{Axis: "port", Value: v("65536", "port 65536", nil), Expect: expBridgeArgs, Why: "a port past u16 (invalid port)"},
			{Axis: "port", Value: v("nosuch", "port nosuchsvc", nil), Expect: expBridgeArgs, Why: "a port that is no number and no UDP service (invalid port)"},
			{Axis: "vni", Value: v("2to24", "vni 16777216", nil), Expect: expBridgeArgs, Why: "a VNI past 24 bits (invalid VNI)"},
			{Axis: "srcvni", Value: v("2to24", "src_vni 16777216", nil), Expect: expBridgeArgs, Why: "a src VNI past 24 bits (invalid src VNI)"},
			{Axis: "via", Value: v("nosuch", "via nosuch0", nil), Expect: expBridgeENODEV,
				Why: "a via device that does not exist, which ll_link_get asks the kernel for (Cannot find device)"},
		},
	}
	sp.Build = func(vals []value) row { return buildFdb(sp, vals, fdbVxSetup) }
	sp.Judge = fdbVxJudge
	sp.NegBase = func(vals []value) bool {
		p := pickOf(sp.Axes, vals)
		return p["verb"] == "add" && p["mac"] == "uni" && p["remote"] == "dst4" && p["state"] == "absent"
	}
	sp.Derive = func(pos []row) []row {
		return append(deriveFdbDupGone(sp.Name, pos), fdbVxTwice(sp)...)
	}
	return sp
}

// fdbVxTwice is a unicast replace with dst 192.0.2.1, run twice, under
// the device's own src_vni and under a foreign one. The first finds its
// entry and updates it; the second finds nothing (see fdbVxJudge), so
// vxlan_fdb_create inserts the same key again and the hash table refuses
// it (EEXIST), though replace asks no NLM_F_EXCL.
func fdbVxTwice(sp *space) []row {
	out := make([]row, 0, 2)
	for _, src := range []string{"own", "other"} {
		r := sp.Build(sp.assign(map[string]string{"verb": "replace", "srcvni": src}))
		r.Pre = append(r.Pre, preStep{Cmd: r.Cmd})
		r.ID = sp.Name + "/twice-" + src
		if src == "own" {
			r.Desc = "corner: replace under the device's own src_vni, twice: the second updates the entry"
		} else {
			r.Desc = "negative: replace under a foreign src_vni, twice: the second finds nothing and inserts a duplicate key (EEXIST)"
			r.Expect = expect{RC: intp(255), Errno: "EEXIST", RefusedBy: "kernel"}
		}
		out = append(out, r)
	}
	return out
}

// fdbVxNh is whether the remote is an nhid.
func fdbVxNh(p map[string]string) bool { return strings.HasPrefix(p["remote"], "nh") }

// fdbVxAdd is vxlan_fdb_add's verdict on a new entry: a remote, then
// vxlan_fdb_parse (an nhid with via), an nhid with a dynamic state, the
// remote's family, then vxlan_fdb_update_create (replace makes no
// multicast or zero entry) and vxlan_fdb_nh_update (the nexthop must be an
// fdb group). vxlan_fdb_add ignores ndm_flags but NTF_USE.
func fdbVxAdd(p map[string]string) (*expect, string) {
	switch {
	case p["remote"] == "absent":
		return expBridgeEINVAL, "neither dst nor nhid"
	case fdbVxNh(p) && p["via"] == "d1":
		return expBridgeEINVAL, "nhid with via (DST, VNI, ifindex and port are mutually exclusive with NH_ID)"
	case fdbVxNh(p) && p["state"] == "dynamic":
		return expBridgeEINVAL, "a dynamic nhid entry (A nexthop fdb cannot be dynamic)"
	case p["remote"] == "dst6":
		return &expect{RC: intp(255), Errno: "EAFNOSUPPORT", RefusedBy: "kernel"}, "an IPv6 dst on an IPv4 VXLAN"
	case p["verb"] == "replace" && p["mac"] != "uni":
		return expBridgeEOPNOTSUPP, "replace making a multicast or zero entry"
	case p["remote"] == "nhsingle" || p["remote"] == "nhnotfdb" || p["remote"] == "nhgone":
		return expBridgeEINVAL, "an nhid that is no fdb group (Nexthop id does not exist, is not a fdb nexthop, or is not a multipath group)"
	}
	return nil, ""
}

// fdbVxJudge: a delete of an entry its add could not create is
// vxlan_fdb_parse's refusal or __vxlan_fdb_delete finding no MAC. An
// entry added with a src_vni other than the device's cannot be deleted at
// all: vxlan_fdb_alloc keys it by that src_vni, but on a VXLAN without
// external (VXLAN_F_COLLECT_METADATA) vxlan_find_mac_rcu looks every MAC up
// under the device's own VNI, so the delete finds nothing.
func fdbVxJudge(p map[string]string) (*expect, string) {
	if p["verb"] != "del" {
		return fdbVxAdd(p)
	}
	if exp, _ := fdbVxAdd(p); exp == nil {
		if p["srcvni"] == "other" {
			return expBridgeENOENT, "a delete of an entry added under a foreign src_vni, which no lookup finds"
		}
		return nil, ""
	}
	if fdbVxNh(p) && p["via"] == "d1" {
		return expBridgeEINVAL, "nhid with via (DST, VNI, ifindex and port are mutually exclusive with NH_ID)"
	}
	return expBridgeENOENT, "a delete of an entry its add could not create"
}

// buildFdb assembles `bridge fdb VERB MAC ARGS...`. A delete first adds
// the same entry, unless Judge refuses that add. The readback dumps the
// device's FDB.
func buildFdb(sp *space, vals []value, setup string) row {
	args := make([]string, 0, len(vals))
	for _, val := range vals[1:] {
		args = append(args, val.Arg)
	}
	tail := joinArgs(args...)
	r := row{
		Cmd:    "bridge fdb " + vals[0].Arg + " " + tail,
		Pre:    []preStep{{Cmd: setup}},
		Expect: expect{RC: intp(0), RefusedBy: "none"},
		Axes:   mergeFacts(vals),
	}
	dev := r.Axes["dev"]
	switch {
	case sp.Name == "fdbvx":
		dev = "vx0"
		r.Cmd += " dev vx0"
		r.Axes["dev"] = dev
		r.Axes["target"] = "2"
	case dev == "":
		dev = "d0" // a device bridge cannot find; dump d0's instead
	}
	r.Readback = []readback{{Name: "dump", Cmd: "bridge fdb show dev " + dev}}
	if vals[0].Name == "del" {
		add := pickOf(sp.Axes, vals)
		add["verb"] = "add"
		if exp, _ := sp.Judge(add); exp == nil {
			r.Pre = append(r.Pre, preStep{Cmd: strings.Replace(r.Cmd, "bridge fdb del ", "bridge fdb add ", 1)})
		}
	}
	return r
}

// deriveFdbDupGone is deriveDupGone as bridge reports it (rc 255). Entries
// made by use or extern_learn take no NLM_F_EXCL, so they are not
// duplicated.
func deriveFdbDupGone(name string, pos []row) []row {
	out := deriveDupGone(name, pos, "ENOENT", func(r *row) bool { return r.Axes["ntf"] != "1" && r.Axes["ntf"] != "16" })
	for i := range out {
		out[i].Expect.RC = intp(255)
	}
	return out
}
