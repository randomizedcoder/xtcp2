package main

// The tc flower space (gotc 11C): tc(8)'s read-only `filter show/get` of
// flower filters over rtnetlink, each command plain and -j, in one corpus
// directory, ws/tcflower.
//
// tc's side is tc/f_flower.c: flower_print_opt and its helpers, and
// flower_parse_opt as far as `get ... flower` reaches it (get_u32 of the
// handle, then TCA_OPTIONS with TCA_FLOWER_FLAGS 0 and, unless the
// protocol is all, TCA_FLOWER_KEY_ETH_TYPE). The frame is tc_filter.c's
// print_filter and m_action.c with m_gact.c, as in ws/tcfilter.
//
// The kernel's side is net/sched/cls_flower.c: fl_dump, whose
// fl_dump_key_val puts a key and its mask only when the mask is non-zero,
// and fl_terse_dump, which -br asks for: handle, TCA_FLOWER_FLAGS and the
// actions' kind and stats, with no TCA_ACT_INDEX (tcf_action_dump_terse's
// from_act is false).
//
// The rows pin the plan's leads (docs/netlink/upstream/iproute2-tc-flower.md):
// gtp options missing from JSON (FL1), pfcp's seid through print_uint
// (FL2), a classid read as hw_tc (FL3), tcp_flags' bare hex mask (FL4),
// the ICMP masks in decimal (FL5), dst_port's type by match (FL6), an
// unknown eth_type without 0x (FL7), get_u32's strict handle (FL16), the
// get request's match keys (FL17), pfcp's text (FL18), -br (FL20) and
// get's options handed to parse_fopt by value (FL21) and the parent a
// block get must name for nothing (FL22).
//
// Determinism: every filter has an explicit pref and handle, every device
// a fixed MAC and addrgenmode none before it comes up, and nothing sends a
// packet, so every -s counter is 0; gact's -s timers are the one
// allow-listed difference. Writes appear only as negatives.

// tcFlowerModules are the qdisc, classifier and action modules the
// fixtures use; cls_flower pulls in nf_conntrack for the ct_* keys.
var tcFlowerModules = []string{"sch_ingress", "cls_flower", "act_gact"}

// fll2Fix: link-layer keys on d0's clsact ingress, one per pref: MACs with
// a prefix and a non-contiguous mask, VLAN and QinQ, num_of_vlans, ARP,
// PPPoE, both MPLS forms, CFM, indev, l2_miss and an eth_type tc has no
// name for (FL7).
var fll2Fix = []string{
	tcDummies("d0", "d1"),
	"tc qdisc add dev d0 clsact",
	"tc filter add dev d0 ingress pref 1 protocol ip handle 1 flower dst_mac aa:bb:cc:dd:ee:ff src_mac 02:00:00:00:00:01/24 action pass",
	"tc filter add dev d0 ingress pref 2 protocol ip handle 1 flower dst_mac aa:bb:cc:dd:ee:ff/ff:00:ff:00:ff:00 action drop",
	"tc filter add dev d0 ingress pref 3 protocol 802.1q handle 1 flower vlan_id 100 vlan_prio 3 vlan_ethtype ipv4 action pass",
	"tc filter add dev d0 ingress pref 4 protocol 802.1ad handle 1 flower vlan_id 10 vlan_ethtype 802.1q" +
		" cvlan_id 20 cvlan_prio 2 cvlan_ethtype ipv6 action pass",
	"tc filter add dev d0 ingress pref 5 protocol 802.1q handle 1 flower num_of_vlans 2 action pass",
	"tc filter add dev d0 ingress pref 6 protocol arp handle 1 flower arp_sip 10.0.0.1/24 arp_tip 10.0.0.2" +
		" arp_op request arp_sha 02:00:00:00:00:01 arp_tha 02:00:00:00:00:02/24 action pass",
	"tc filter add dev d0 ingress pref 7 protocol ppp_ses handle 1 flower pppoe_sid 1234 ppp_proto ip action pass",
	"tc filter add dev d0 ingress pref 8 protocol mpls_uc handle 1 flower mpls_label 100 mpls_tc 3 mpls_bos 1 mpls_ttl 64 action pass",
	"tc filter add dev d0 ingress pref 9 protocol mpls_uc handle 1 flower mpls lse depth 1 label 100 tc 3" +
		" lse depth 2 label 200 bos 1 ttl 9 action pass",
	"tc filter add dev d0 ingress pref 10 protocol 0x8902 handle 1 flower cfm mdl 5 op 1 action pass",
	"tc filter add dev d0 ingress pref 11 protocol all handle 1 flower indev d1 action pass",
	"tc filter add dev d0 ingress pref 12 protocol all handle 1 flower l2_miss 1 action pass",
	"tc filter add dev d0 ingress pref 13 protocol 0x88b5 handle 1 flower action pass",
}

// fll3Fix: network and transport keys on d0's clsact ingress: addresses,
// masked and ranged ports (FL6), tcp_flags (FL4), masked ICMP (FL5),
// L2TPv3, ESP and AH SPIs, an unnamed ip_proto with tos and ttl, and
// ip_flags.
var fll3Fix = []string{
	tcDummies("d0"),
	"tc qdisc add dev d0 clsact",
	"tc filter add dev d0 ingress pref 1 protocol ip handle 1 flower dst_ip 10.0.0.0/8 src_ip 10.1.0.0/16" +
		" ip_proto tcp dst_port 80 src_port 1000/0xff00 tcp_flags 0x2/0x12 action pass",
	"tc filter add dev d0 ingress pref 2 protocol ipv6 handle 1 flower dst_ip 2001:db8::/32 src_ip ::ffff:1.2.3.4" +
		" ip_proto udp dst_port 1000-2000 src_port 53 action pass",
	"tc filter add dev d0 ingress pref 3 protocol ip handle 1 flower ip_proto sctp dst_port 5000 action pass",
	"tc filter add dev d0 ingress pref 4 protocol ip handle 1 flower ip_proto icmp type 8/15 code 0 action pass",
	"tc filter add dev d0 ingress pref 5 protocol ipv6 handle 1 flower ip_proto icmpv6 type 128 code 0/15 action pass",
	"tc filter add dev d0 ingress pref 6 protocol ip handle 1 flower ip_proto l2tp l2tpv3_sid 1234 action pass",
	"tc filter add dev d0 ingress pref 7 protocol ip handle 1 flower ip_proto esp spi 0x1234 action pass",
	"tc filter add dev d0 ingress pref 8 protocol ip handle 1 flower ip_proto ah spi 5 action pass",
	"tc filter add dev d0 ingress pref 9 protocol ip handle 1 flower ip_proto 0x99 ip_tos 0x10/0xf0 ip_ttl 64 action pass",
	"tc filter add dev d0 ingress pref 10 protocol ip handle 1 flower ip_flags frag/nofirstfrag action pass",
	"tc filter add dev d0 ingress pref 11 protocol ipv6 handle 1 flower dst_ip ::1 src_ip 2001:db8:: action pass",
}

// flencFix: tunnel keys on d0's clsact ingress: IPv4 and IPv6 enc
// addresses with key id, port, tos and ttl, then each enc option kind:
// geneve with two options, vxlan, erspan v1 and v2, gtp (FL1), pfcp with
// a seid past 2^32 (FL2, FL18), and enc_flags.
var flencFix = []string{
	tcDummies("d0"),
	"tc qdisc add dev d0 clsact",
	"tc filter add dev d0 ingress pref 1 protocol ip handle 1 flower enc_dst_ip 10.0.0.1 enc_src_ip 10.0.0.0/24" +
		" enc_key_id 100 enc_dst_port 4789 enc_tos 0x10/0xf0 enc_ttl 64 action pass",
	"tc filter add dev d0 ingress pref 2 protocol ip handle 1 flower enc_dst_ip 2001:db8::1 enc_src_ip 2001:db8::/64 action pass",
	"tc filter add dev d0 ingress pref 3 protocol ip handle 1 flower" +
		" geneve_opts 0102:80:11223344/ffff:ff:ffffffff,0103:81:3344556677889900/0fff:ff:ffffffffffffffff action pass",
	"tc filter add dev d0 ingress pref 4 protocol ip handle 1 flower vxlan_opts 1234/0xffff action pass",
	"tc filter add dev d0 ingress pref 5 protocol ip handle 1 flower erspan_opts 1:12:0:0/1:0xffff:0:0 action pass",
	"tc filter add dev d0 ingress pref 6 protocol ip handle 1 flower erspan_opts 2:0:1:3/2:0:1:0x3f action pass",
	"tc filter add dev d0 ingress pref 7 protocol ip handle 1 flower gtp_opts 1:8/ff:ff action pass",
	"tc filter add dev d0 ingress pref 8 protocol ip handle 1 flower pfcp_opts 1:123456789/ff:ffffffffff action pass",
	"tc filter add dev d0 ingress pref 9 protocol ip handle 1 flower enc_flags tuncsum/notundf action pass",
}

// flctFix: conntrack keys on d0's clsact ingress: a masked state, zone,
// mark and label, then every state flag with a full label mask.
var flctFix = []string{
	tcDummies("d0"),
	"tc qdisc add dev d0 clsact",
	"tc filter add dev d0 ingress pref 1 protocol ip handle 1 flower ct_state +trk+est-new ct_zone 5" +
		" ct_mark 0x10/0xff ct_label 0102/ffff action pass",
	"tc filter add dev d0 ingress pref 2 protocol ip handle 1 flower ct_state +trk+rel+rpl-inv" +
		" ct_label 0102030405060708090a0b0c0d0e0f10 action drop",
}

// flmiscFix: classid against hw_tc (FL3), skip_hw, the largest handle,
// two actions and a chain on d0's clsact ingress; d1's ingress in shared
// block 20.
var flmiscFix = []string{
	tcDummies("d0", "d1"),
	"tc qdisc add dev d0 clsact",
	"tc filter add dev d0 ingress pref 1 protocol ip handle 1 flower classid 1:1 action pass",
	"tc filter add dev d0 ingress pref 2 protocol ip handle 1 flower classid 1:ffe0 action pass",
	"tc filter add dev d0 ingress pref 3 protocol ip handle 1 flower hw_tc 2 action pass",
	"tc filter add dev d0 ingress pref 4 protocol ip handle 0xffffffff flower skip_hw action pass",
	"tc filter add dev d0 ingress pref 5 protocol ip handle 1 flower ip_proto udp action pass action drop",
	"tc filter add dev d0 ingress chain 3 pref 1 protocol ip handle 1 flower ip_proto tcp action drop",
	"tc qdisc add dev d1 ingress_block 20 clsact",
	"tc filter add block 20 pref 1 protocol ip handle 1 flower dst_ip 10.0.0.1 action pass",
}

// tcflowerSpace: `tc filter show/get` of flower filters over five
// fixtures.
func tcflowerSpace() *space {
	cmds := []value{
		// fll2.
		bc(brPos, "fll2", "filter show dev d0 ingress", fll2Fix),
		bc(brPos, "fll2", "-s filter show dev d0 ingress", fll2Fix),
		bc(brPos, "fll2", "-d filter show dev d0 ingress", fll2Fix),
		bc(brCorner, "fll2", "-o filter show dev d0 ingress", fll2Fix),
		bc(brCorner, "fll2", "-r filter show dev d0 ingress", fll2Fix),
		bc(brCorner, "fll2", "-br filter show dev d0 ingress", fll2Fix), // terse (FL20)
		bc(brPos, "fll2", "filter show dev d0 ingress protocol 802.1q", fll2Fix),
		bc(brPos, "fll2", "filter show dev d0 ingress pref 9", fll2Fix),
		bc(brPos, "fll2", "filter get dev d0 ingress protocol ip pref 1 handle 1 flower", fll2Fix),
		bc(brPos, "fll2", "filter get dev d0 ingress protocol mpls_uc pref 9 handle 1 flower", fll2Fix),
		bc(brCorner, "fll2", "filter get dev d0 ingress protocol all pref 11 handle 1 flower", fll2Fix),    // no ETH_TYPE (FL17)
		bc(brCorner, "fll2", "filter get dev d0 ingress protocol 0x88b5 pref 13 handle 1 flower", fll2Fix), // FL7
		// fll3.
		bc(brPos, "fll3", "filter show dev d0 ingress", fll3Fix), // FL4, FL5, FL6
		bc(brPos, "fll3", "-s filter show dev d0 ingress", fll3Fix),
		bc(brCorner, "fll3", "-o filter show dev d0 ingress", fll3Fix),
		bc(brCorner, "fll3", "-p filter show dev d0 ingress", fll3Fix),
		bc(brCorner, "fll3", "-br filter show dev d0 ingress", fll3Fix),
		bc(brPos, "fll3", "filter show dev d0 ingress protocol ipv6", fll3Fix),
		bc(brPos, "fll3", "filter get dev d0 ingress protocol ip pref 1 handle 1 flower", fll3Fix),
		bc(brPos, "fll3", "filter get dev d0 ingress protocol ipv6 pref 2 handle 1 flower", fll3Fix),
		bc(brPos, "fll3", "filter get dev d0 ingress protocol ip pref 4 handle 1 flower", fll3Fix),
		// flenc.
		bc(brPos, "flenc", "filter show dev d0 ingress", flencFix), // FL1, FL2, FL18
		bc(brPos, "flenc", "-s filter show dev d0 ingress", flencFix),
		bc(brPos, "flenc", "-d filter show dev d0 ingress", flencFix),
		bc(brCorner, "flenc", "-o filter show dev d0 ingress", flencFix),
		bc(brPos, "flenc", "filter get dev d0 ingress protocol ip pref 3 handle 1 flower", flencFix),
		bc(brCorner, "flenc", "filter get dev d0 ingress protocol ip pref 7 handle 1 flower", flencFix),   // FL1
		bc(brBoundary, "flenc", "filter get dev d0 ingress protocol ip pref 8 handle 1 flower", flencFix), // FL2
		// flct.
		bc(brPos, "flct", "filter show dev d0 ingress", flctFix),
		bc(brPos, "flct", "-s filter show dev d0 ingress", flctFix),
		bc(brPos, "flct", "filter get dev d0 ingress protocol ip pref 2 handle 1 flower", flctFix),
		// flmisc.
		bc(brPos, "flmisc", "filter show dev d0 ingress", flmiscFix), // FL3
		bc(brPos, "flmisc", "-s filter show dev d0 ingress", flmiscFix),
		bc(brCorner, "flmisc", "-br filter show dev d0 ingress", flmiscFix),
		bc(brPos, "flmisc", "filter show dev d0 ingress chain 3", flmiscFix),
		bc(brPos, "flmisc", "filter show block 20", flmiscFix),
		bc(brPos, "flmisc", "filter show dev d0 ingress pref 5", flmiscFix),
		bc(brBoundary, "flmisc", "filter get dev d0 ingress protocol ip pref 4 handle 0xffffffff flower", flmiscFix),
		bc(brCorner, "flmisc", "filter get dev d0 ingress protocol ip pref 2 handle 1 flower", flmiscFix), // FL3
		bc(brPos, "flmisc", "filter get dev d0 ingress chain 3 protocol ip pref 1 handle 1 flower", flmiscFix),
		bc(brCorner, "flmisc", "filter get block 20 parent 1: protocol ip pref 1 handle 1 flower", flmiscFix), // any parent (FL22)
		// Batches: one link dump, then each line.
		bb(brPos, "fll3", "two-gets", "", false, batchFile(
			"filter get dev d0 ingress protocol ip pref 1 handle 1 flower",
			"filter get dev d0 ingress protocol ipv6 pref 2 handle 1 flower"), fll3Fix),
		bb(brPos, "flenc", "get-then-show", "", false, batchFile(
			"filter get dev d0 ingress protocol ip pref 8 handle 1 flower",
			"filter show dev d0 ingress"), flencFix),
		bb(brCorner, "flmisc", "brief", "-br", false, batchFile(
			"filter show dev d0 ingress", "filter show block 20"), flmiscFix),
	}
	bad := []badValue{
		brBad("flmisc", "filter get block 20 protocol ip pref 1 handle 1 flower", flmiscFix, tcTool,
			"a block get with no parent: Must specify filter parent, though the kernel ignores it (FL22)"),
		brBad("fll3", "filter get dev d0 ingress protocol ip pref 1 handle 1junk flower", fll3Fix, tcAck1,
			"get_u32's strict handle, after the link lookup: Illegal \"handle\", rc 1 (FL16)"),
		brBad("fll3", "filter get dev d0 ingress protocol ip pref 1 handle 0x1ffffffff flower", fll3Fix, tcAck1,
			"a handle past 2^32: Illegal \"handle\", rc 1 (FL16)"),
		brBad("fll3", "filter get dev d0 ingress protocol ip pref 1 handle 1 flower ip_proto tcp", fll3Fix, tcAck,
			"parse_fopt takes argc by value, so get's own check finds the keys left: Garbage, rc 255 (FL21)"),
		brBad("fll3", "filter get dev d0 ingress protocol ip pref 1 handle 1 flower bogus", fll3Fix, tcAck1,
			"an unknown flower word: What is, then the usage, rc 1"),
		brBad("fll3", "filter get dev d0 ingress protocol ip pref 1 handle 1 flower help", fll3Fix, tcAck1,
			"flower help: the usage, rc 1"),
		brBad("fll3", "filter get dev d0 ingress protocol ip pref 1 handle 9 flower", fll3Fix, tcNoent,
			"no such handle: the kernel's ENOENT"),
		brBad("fll3", "filter get dev d0 ingress protocol ip pref 1 flower", fll3Fix, tcAck,
			"get with no handle: refused after parse_fopt"),
		brBad("bare", "filter add dev nosuch ingress protocol ip flower skip_sw action pass", nil, tcNodev,
			"a write, recorded so gotc's refusal is never a pass"),
	}
	sp := tcSpace("tcflower", cmds, bad)
	sp.Build = func(vals []value) row { return tcBuildMods(tcFlowerModules, vals) }
	return sp
}
