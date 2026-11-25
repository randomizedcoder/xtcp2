package main

// The tc actions space (gotc 12C): tc(8)'s read-only `actions list/get`
// over rtnetlink, and `filter show` of filters holding those actions, each
// command plain and -j, in one corpus directory, ws/tcaction.
//
// tc's side is tc/m_action.c: do_action, tc_act_list_or_flush (list),
// tc_action_gd (get) and print_action, with tc_print_one_action at bind 0
// for `tc actions` and bind 1 for a filter's actions, and the kind
// printers of m_police.c, m_mirred.c, m_skbedit.c, m_vlan.c, m_csum.c,
// m_nat.c, m_simple.c, m_connmark.c, m_ctinfo.c, m_sample.c, m_skbmod.c,
// m_mpls.c and m_gact.c.
//
// The kernel's side is net/sched/act_api.c: tc_dump_action (a dump's
// nests numbered from 0, TCA_ROOT_COUNT, the terse dump of -br) and
// tcf_action_get_1 (a get's nest numbered from 1, no count), and each
// kind's tcf_<kind>_dump.
//
// The rows pin the plan's leads (docs/netlink/upstream/iproute2-tc-action.md):
// "total acts" and its object (AC1), a get's count of 0 (AC2), the failed
// get's second error (AC8), a get with no kind (AC9), the 15-byte kind
// (AC10), order from 0 and from 1 (AC12), police's JSON (AC14), its two
// control_actions (AC15), a pps-only policer (AC16), a mirred to a deleted
// device (AC18), nat's "type" and double space (AC22, AC23), skbmod's
// etype and line breaks (AC24-AC26), ctinfo's statemask (AC28), csum's
// string (AC29) and mpls's protocol (AC30); and gets of 3, 32 and 33
// actions, the last answered with 32 (K8).
//
// tc 7.2.0 can't add some forms the printers read, so those are unit-only:
// police avrate (with rate the parser returns -1 with no message; alone
// the kernel wants an estimator), skbedit queue_mapping hash-type (not in
// tc's parser) and a nat mask that isn't a prefix (get_prefix refuses it).
//
// Determinism: every action has an explicit index, every device a fixed
// MAC and addrgenmode none before it comes up, `since` is far past every
// install, and nothing sends a packet, so every -s counter is 0; the tcf_t
// timers are the one allow-listed difference. Writes appear only as
// negatives.

import (
	"strconv"
	"strings"
)

// tcActionModules are the qdisc, classifier and action modules the
// fixtures use; act_connmark and act_ctinfo pull in nf_conntrack.
var tcActionModules = []string{
	"sch_ingress", "cls_u32", "cls_matchall", "act_gact", "act_police", "act_mirred", "act_skbedit",
	"act_vlan", "act_csum", "act_nat", "act_simple", "act_connmark", "act_ctinfo", "act_sample",
	"act_skbmod", "act_mpls",
}

// acpolFix: police with rate, burst and mtu; with peakrate; with
// conform-exceed drop/pipe (AC15); pps-only (AC16); a rate past 2^32
// bit/s (RATE64); then a u32 filter's own police and a matchall bound to
// police 1.
var acpolFix = []string{
	tcDummies("d0"),
	"tc actions add action police rate 1mbit burst 10k mtu 2k index 1",
	"tc actions add action police rate 1mbit burst 10k peakrate 2mbit mtu 2k index 2",
	"tc actions add action police rate 1mbit burst 10k conform-exceed drop/pipe index 3",
	"tc actions add action police pkts_rate 1000 pkts_burst 10 index 4",
	"tc actions add action police rate 40gbit burst 1m index 5",
	"tc qdisc add dev d0 clsact",
	"tc filter add dev d0 ingress pref 1 protocol ip handle 800::1 u32 match ip dst 10.0.0.1/32 police rate 1mbit burst 10k drop",
	"tc filter add dev d0 ingress pref 2 protocol all handle 1 matchall action police index 1",
}

// acmirFix: mirred egress redirect, egress mirror and ingress redirect to
// d1, to a block, and a mirror to d2, which is then deleted (AC18).
var acmirFix = []string{
	tcDummies("d0", "d1", "d2"),
	"tc actions add action mirred egress redirect dev d1 index 1",
	"tc actions add action mirred egress mirror dev d1 index 2",
	"tc actions add action mirred ingress redirect dev d1 index 3",
	"tc actions add action mirred egress redirect blockid 7 index 4",
	"tc actions add action mirred egress mirror dev d2 index 5",
	"ip link del d2",
}

// acskbFix: skbedit priority as a classid, then queue_mapping, a masked
// mark, ptype and inheritdsfield, then a bare priority.
var acskbFix = []string{
	tcDummies("d0"),
	"tc actions add action skbedit priority 1:2 index 1",
	"tc actions add action skbedit queue_mapping 3 mark 0x10/0xff ptype host inheritdsfield index 2",
	"tc actions add action skbedit priority 5 index 3",
}

// acl2Fix: vlan's five operations, mpls's five (AC30) and skbmod's MACs,
// etype and swap (AC24, AC25).
var acl2Fix = []string{
	tcDummies("d0"),
	"tc actions add action vlan pop index 1",
	"tc actions add action vlan push id 100 protocol 802.1ad priority 3 index 2",
	"tc actions add action vlan modify id 200 index 3",
	"tc actions add action vlan push_eth dst_mac 02:00:00:00:00:01 src_mac 02:00:00:00:00:02 index 4",
	"tc actions add action vlan pop_eth index 5",
	"tc actions add action mpls pop protocol ipv4 index 1",
	"tc actions add action mpls push label 100 tc 3 ttl 64 index 2",
	"tc actions add action mpls modify label 200 index 3",
	"tc actions add action mpls dec_ttl index 4",
	"tc actions add action mpls mac_push label 300 bos 1 index 5",
	"tc actions add action skbmod set dmac 02:00:00:00:00:01 set smac 02:00:00:00:00:02 index 1",
	"tc actions add action skbmod set etype 0xbeef index 2",
	"tc actions add action skbmod swap mac index 3",
	"tc actions add action skbmod set dmac 02:00:00:00:00:01 index 4",
}

// acl3Fix: csum with flags, one flag and none (AC29); nat ingress with a
// prefix, egress with a host, ingress with a /24 (AC22, AC23); connmark;
// ctinfo dscp with a statemask and without (AC28), and cpmark.
var acl3Fix = []string{
	tcDummies("d0"),
	"tc actions add action csum iph tcp index 1",
	"tc actions add action csum udp index 2",
	"tc actions add action csum index 3",
	"tc actions add action nat ingress 10.0.0.0/8 192.168.0.1 index 1",
	"tc actions add action nat egress 10.1.2.3 10.4.5.6 index 2",
	"tc actions add action nat ingress 10.0.0.0/24 192.168.0.1 index 3",
	"tc actions add action connmark zone 5 index 1",
	"tc actions add action ctinfo dscp 0xfc000000 0x01000000 index 1",
	"tc actions add action ctinfo dscp 0xfc000000 index 2",
	"tc actions add action ctinfo cpmark 0xffffffff zone 3 index 3",
}

// acmiscFix: simple, sample, a gact with a cookie and 32 more gacts (a
// dump past one message), then a matchall on d0 whose action list holds
// gact, mirred and skbedit (bind 1).
var acmiscFix = []string{
	tcDummies("d0", "d1"),
	"tc actions add action simple sdata hello index 1",
	"tc actions add action sample rate 100 group 5 trunc 64 index 1",
	"tc actions add action gact pass index 1 cookie 0102",
	"i=2; while [ $i -le 33 ]; do tc actions add action gact drop index $i || exit 1; i=$((i+1)); done",
	"tc qdisc add dev d0 clsact",
	"tc filter add dev d0 ingress pref 1 protocol all handle 1 matchall action gact pass" +
		" action mirred egress redirect dev d1 action skbedit priority 5",
}

// tcactionSpace: `tc actions list/get` of 13 action kinds over six
// fixtures, and `filter show` of filters holding them.
func tcactionSpace() *space {
	cmds := []value{
		// bare: no actions.
		bc(brBoundary, "bare", "actions ls action gact", nil), // total acts 0
		bc(brCorner, "bare", "actions get", nil),              // no kind: an empty get (AC9)
		bc(brCorner, "bare", "actions get help", nil),         // the same (AC9)
		bc(brCorner, "bare", "actions ls action nosuch", nil), // bad action, rc 0 (AC6)
		// acpol.
		bc(brPos, "acpol", "actions ls action police", acpolFix),
		bc(brPos, "acpol", "-s actions ls action police", acpolFix),
		bc(brPos, "acpol", "-d actions ls action police", acpolFix),
		bc(brCorner, "acpol", "-o actions ls action police", acpolFix),
		bc(brCorner, "acpol", "-r actions ls action police", acpolFix),
		bc(brCorner, "acpol", "-br actions ls action police", acpolFix),
		bc(brCorner, "acpol", "actions list action police", acpolFix),
		bc(brCorner, "acpol", "actions show action police", acpolFix),
		bc(brPos, "acpol", "actions get action police index 1", acpolFix),
		bc(brCorner, "acpol", "actions get action police index 3", acpolFix),   // AC15
		bc(brBoundary, "acpol", "actions get action police index 4", acpolFix), // AC16
		bc(brBoundary, "acpol", "actions get action police index 5", acpolFix), // RATE64
		bc(brPos, "acpol", "filter show dev d0 ingress", acpolFix),
		bc(brPos, "acpol", "-s filter show dev d0 ingress", acpolFix),
		// acmir.
		bc(brPos, "acmir", "actions ls action mirred", acmirFix),
		bc(brPos, "acmir", "-s actions ls action mirred", acmirFix),
		bc(brCorner, "acmir", "-br actions ls action mirred", acmirFix),
		bc(brPos, "acmir", "actions get action mirred index 1", acmirFix),
		bc(brPos, "acmir", "actions get action mirred index 4", acmirFix),
		bc(brCorner, "acmir", "actions get action mirred index 5", acmirFix), // AC18
		// acskb.
		bc(brPos, "acskb", "actions ls action skbedit", acskbFix),
		bc(brPos, "acskb", "-s actions ls action skbedit", acskbFix),
		bc(brCorner, "acskb", "-o actions ls action skbedit", acskbFix),
		bc(brPos, "acskb", "actions get action skbedit index 2", acskbFix),
		bc(brCorner, "acskb", "actions get action skbedit index 3", acskbFix),
		// acl2.
		bc(brPos, "acl2", "actions ls action vlan", acl2Fix),
		bc(brPos, "acl2", "-s actions ls action vlan", acl2Fix),
		bc(brPos, "acl2", "actions get action vlan index 2", acl2Fix),
		bc(brPos, "acl2", "actions get action vlan index 4", acl2Fix),
		bc(brPos, "acl2", "actions ls action mpls", acl2Fix), // AC30
		bc(brCorner, "acl2", "actions get action mpls index 4", acl2Fix),
		bc(brPos, "acl2", "actions get action mpls index 5", acl2Fix),
		bc(brPos, "acl2", "actions ls action skbmod", acl2Fix),
		bc(brCorner, "acl2", "-o actions ls action skbmod", acl2Fix),       // AC26
		bc(brCorner, "acl2", "actions get action skbmod index 2", acl2Fix), // AC24, AC25
		// acl3.
		bc(brPos, "acl3", "actions ls action csum", acl3Fix), // AC29
		bc(brCorner, "acl3", "actions get action csum index 3", acl3Fix),
		bc(brPos, "acl3", "actions ls action nat", acl3Fix), // AC22, AC23
		bc(brPos, "acl3", "actions get action nat index 2", acl3Fix),
		bc(brPos, "acl3", "actions ls action connmark", acl3Fix),
		bc(brPos, "acl3", "actions ls action ctinfo", acl3Fix), // AC28
		bc(brPos, "acl3", "-s actions ls action ctinfo", acl3Fix),
		bc(brCorner, "acl3", "actions get action ctinfo index 2", acl3Fix),
		// acmisc.
		bc(brPos, "acmisc", "actions ls action simple", acmiscFix),
		bc(brPos, "acmisc", "actions ls action sample", acmiscFix),
		bc(brBoundary, "acmisc", "actions ls action gact", acmiscFix), // 33 actions
		bc(brPos, "acmisc", "-s actions get action gact index 1", acmiscFix),
		bc(brPos, "acmisc", "-d actions get action gact index 1", acmiscFix),
		bc(brCorner, "acmisc", "-p actions get action gact index 1", acmiscFix),
		bc(brCorner, "acmisc", "-br actions ls action gact", acmiscFix),
		bc(brCorner, "acmisc", "actions ls action gact since 100000000", acmiscFix),
		bc(brCorner, "acmisc", "actions list foo gact bar", acmiscFix), // AC7
		bc(brBoundary, "acmisc", "actions get action gact index 33", acmiscFix),
		bc(brPos, "acmisc", "filter show dev d0 ingress", acmiscFix), // bind 1
		bc(brPos, "acmisc", "-s filter show dev d0 ingress", acmiscFix),
		bc(brCorner, "acmisc", "-br filter show dev d0 ingress", acmiscFix),
		// Batches: each line in turn.
		bb(brPos, "acmisc", "two-gets", "", false, batchFile(
			"actions get action gact index 1",
			"actions get action simple index 1"), acmiscFix),
		bb(brPos, "acpol", "ls-then-filter", "", false, batchFile(
			"actions ls action police",
			"filter show dev d0 ingress"), acpolFix),
		bb(brCorner, "acl3", "get-then-ls", "", false, batchFile(
			"actions get action nat index 1",
			"actions ls action csum"), acl3Fix),
		// Gets of several actions, after the batches so the ids above
		// stay: three kinds; 32 gacts (TCA_ACT_MAX_PRIO); 33, of which the
		// kernel answers the first 32 (K8).
		bc(brPos, "acmisc", "actions get action gact index 1 action simple index 1 action sample index 1", acmiscFix),
		bc(brBoundary, "acmisc", "actions get"+actGetRange("gact", 2, 33), acmiscFix),
		bc(brCorner, "acmisc", "actions get"+actGetRange("gact", 1, 33), acmiscFix),
	}
	noent := expect{RC: intp(255), Errno: "ENOENT", RefusedBy: refKernel}
	inval := expect{RC: intp(255), Errno: "EINVAL", RefusedBy: refKernel}
	bad := []badValue{
		brBad("bare", "actions ls", nil, tcTool, "list with no kind: the usage, rc 255"),
		brBad("bare", "actions get action gact", nil, tcTool,
			"get with no index: Error: no index specified, before any request"),
		brBad("bare", "actions get action aaaaaaaaaaaaaaaaaa index 1", nil, tcTool,
			"a kind of 18 characters: cut to 15, then non existent action (AC10)"),
		brBad("acmisc", "actions get action gact index 999999", acmiscFix, noent,
			"no such index: the kernel's error, then Command \"action\" is unknown (AC8)"),
		brBad("bare", "actions get action pedit index 1", nil, inval,
			"a kind the kernel has not loaded: Specified TC action kind not found"),
		brBad("bare", "actions delete action gact index 7", nil, noent,
			"a write, recorded so gotc's refusal is never a pass"),
	}
	sp := tcSpace("tcaction", cmds, bad)
	sp.Build = func(vals []value) row { return tcBuildMods(tcActionModules, vals) }
	return sp
}

// actGetRange is " action kind index i" for i from lo to hi.
func actGetRange(kind string, lo, hi int) string {
	var b strings.Builder
	for i := lo; i <= hi; i++ {
		b.WriteString(" action " + kind + " index " + strconv.Itoa(i))
	}
	return b.String()
}
