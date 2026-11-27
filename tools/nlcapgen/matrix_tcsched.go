package main

// The scheduling qdisc space (gotc 15C): `tc qdisc show` and `tc class
// show` of the nine classful and scheduling kinds increment 9 left (drr,
// qfq, hfsc, ets, mqprio, taprio, multiq, cbs, etf), each command plain
// and -j, in one corpus directory, ws/tcsched.
//
// tc's side is tc/q_drr.c, q_qfq.c, q_hfsc.c, q_ets.c, q_mqprio.c,
// q_taprio.c, q_multiq.c, q_cbs.c and q_etf.c under tc_qdisc.c's and
// tc_class.c's frames. The kernel's side is each net/sched/sch_<kind>.c
// dump, dump_class and dump_class_stats.
//
// The rows pin the plan's leads (docs/netlink/upstream/iproute2-tc-sched.md):
// drr's text-only quantum and deficit (Q37, Q38); qfq's unread class
// xstats (Q39); hfsc's double space and string default (Q40), its blank
// -s line (Q42), sc against rt and ls (Q43), its raw JSON d (Q44); ets's
// trailing spaces (Q45) and strict classes (Q46); mqprio's hard newlines
// (Q48), its dcb mode and shaper (Q49), its 16 fp entries (Q50), queues
// (Q51) and map (Q52); taprio's txtime delay (Q55), max-sdu (Q56), the
// unlabelled admin schedule (Q57), the blank line (Q58) and an admin-only
// dump (K28); multiq's bands (Q60); cbs's and etf's offload (Q61) and
// etf's skip_sock_check (Q63).
//
// mqprio, taprio and multiq need a multi-queue device: a dummy with eight
// tx queues. mqprio's children are the default qdisc, a sysctl of the
// guest's, so its rows show the root only. Offload needs ndo_setup_tc,
// which a dummy lacks: offload appears only as negatives. (Replacing an
// existing etf fails first, "Change operation not supported", EINVAL, so
// the etf negative grafts a new one on a drr class.)
//
// Determinism: explicit handles, fixed MACs, no packets. taprio's
// base-times are a past one (the oper schedule starts at once) and a far
// future one (an admin schedule that never starts). Writes appear only as
// negatives.

// tcSchedModules are the qdisc modules the fixtures use; prio holds cbs
// and etf.
var tcSchedModules = []string{
	"sch_cbs", "sch_drr", "sch_etf", "sch_ets", "sch_hfsc", "sch_mqprio", "sch_multiq", "sch_prio",
	"sch_qfq", "sch_taprio",
}

// schdrrFix: drr with a class of quantum 1514 and one at the default (the
// device's MTU).
var schdrrFix = []string{
	tcDummies("d0"),
	"tc qdisc add dev d0 root handle 1: drr",
	"tc class add dev d0 parent 1: classid 1:1 drr quantum 1514",
	"tc class add dev d0 parent 1: classid 1:2 drr",
}

// schqfqFix: qfq with two classes of different weights and maxpkt. (qfqFix
// in matrix_tc.go is fq.)
var schqfqFix = []string{
	tcDummies("d0"),
	"tc qdisc add dev d0 root handle 1: qfq",
	"tc class add dev d0 parent 1: classid 1:1 qfq weight 1 maxpkt 1514",
	"tc class add dev d0 parent 1: classid 1:2 qfq weight 30 maxpkt 9000",
}

// schhfscFix: hfsc default 10; a parent with sc and ul, a child with rt
// unlike ls (m1, d and m2), and a child whose rt and ls are the same (sc).
var schhfscFix = []string{
	tcDummies("d0"),
	"tc qdisc add dev d0 root handle 1: hfsc default 10",
	"tc class add dev d0 parent 1: classid 1:1 hfsc sc rate 10mbit ul rate 20mbit",
	"tc class add dev d0 parent 1:1 classid 1:10 hfsc rt m1 2mbit d 10ms m2 1mbit ls m2 3mbit",
	"tc class add dev d0 parent 1:1 classid 1:20 hfsc rt m2 1mbit ls m2 1mbit",
}

// schhfsc0Fix: hfsc with no default, so no default key.
var schhfsc0Fix = []string{
	tcDummies("d0"),
	"tc qdisc add dev d0 root handle 2: hfsc",
}

// schetsFix: ets with one strict band and two quanta, a short priomap.
var schetsFix = []string{
	tcDummies("d0"),
	"tc qdisc add dev d0 root handle 1: ets bands 3 strict 1 quanta 1500 3000 priomap 2 1 0",
}

// schets0Fix: ets with every band strict: no quanta.
var schets0Fix = []string{
	tcDummies("d0"),
	"tc qdisc add dev d0 root handle 1: ets strict 3",
}

// schtsnFix: a two-band prio holding cbs and etf with skip_sock_check.
var schtsnFix = []string{
	tcDummies("d0"),
	"tc qdisc add dev d0 root handle 1: prio bands 2 priomap 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0 0",
	"tc qdisc add dev d0 parent 1:1 handle 10: cbs idleslope 20000 sendslope -980000 hicredit 30 locredit -1470",
	"tc qdisc add dev d0 parent 1:2 handle 20: etf clockid CLOCK_TAI delta 300000 skip_sock_check",
}

// schmultiqFix: multiq on eight queues.
var schmultiqFix = []string{
	tcDummies("d1/8"),
	"tc qdisc add dev d1 root handle 1: multiq",
}

// schmqprioFix: software mqprio, two traffic classes over eight queues.
var schmqprioFix = []string{
	tcDummies("d1/8"),
	"tc qdisc add dev d1 root handle 100: mqprio num_tc 2 map 0 0 1 1 queues 4@0 4@4 hw 0",
}

// tcSchedTaprio is the common head of the taprio fixtures.
const tcSchedTaprio = "tc qdisc add dev d1 root handle 100: taprio num_tc 2 map 0 0 1 1 queues 4@0 4@4 clockid CLOCK_TAI"

// schtaprioFix: taprio in txtime-assist mode (flags 0x1) with a txtime
// delay, a past base-time and two entries.
var schtaprioFix = []string{
	tcDummies("d1/8"),
	tcSchedTaprio + " base-time 1000000000 sched-entry S 01 300000 sched-entry S 02 200000 flags 0x1 txtime-delay 200000",
}

// schtaprio2Fix: a running oper schedule and, replaced over it, an admin
// schedule whose base-time is far in the future (Q57).
var schtaprio2Fix = []string{
	tcDummies("d1/8"),
	tcSchedTaprio + " base-time 1000000000 sched-entry S 01 300000 sched-entry S 02 200000",
	"tc qdisc replace dev d1 root handle 100: taprio clockid CLOCK_TAI base-time 4000000000000000000 sched-entry S 03 100000",
}

// schtaprio3Fix: only an admin schedule, never started, so no oper, no
// max-sdu and no fp (K28).
var schtaprio3Fix = []string{
	tcDummies("d1/8"),
	tcSchedTaprio + " base-time 4000000000000000000 sched-entry S 01 300000 sched-entry S 02 200000",
}

// tcschedSpace: `tc qdisc show` and `tc class show` of the nine kinds over
// twelve fixtures.
func tcschedSpace() *space {
	cmds := []value{
		// drr.
		bc(brCorner, "schdrr", "qdisc show dev d0", schdrrFix), // no options (K29)
		bc(brPos, "schdrr", "class show dev d0", schdrrFix),    // Q37
		bc(brPos, "schdrr", "-s class show dev d0", schdrrFix), // Q38
		bc(brCorner, "schdrr", "-g class show dev d0", schdrrFix),
		// qfq.
		bc(brCorner, "schqfq", "qdisc show dev d0", schqfqFix),
		bc(brPos, "schqfq", "class show dev d0", schqfqFix),
		bc(brCorner, "schqfq", "-s class show dev d0", schqfqFix), // Q39
		// hfsc.
		bc(brPos, "schhfsc", "qdisc show dev d0", schhfscFix),       // Q40
		bc(brPos, "schhfsc", "class show dev d0", schhfscFix),       // Q43, Q44
		bc(brCorner, "schhfsc", "-s class show dev d0", schhfscFix), // Q42
		bc(brCorner, "schhfsc", "-g class show dev d0", schhfscFix),
		bc(brPos, "schhfsc", "class show dev d0 classid 1:10", schhfscFix),
		bc(brBoundary, "schhfsc0", "qdisc show dev d0", schhfsc0Fix), // Q40: no default
		bc(brBoundary, "schhfsc0", "class show dev d0", schhfsc0Fix), // the root class only
		// ets.
		bc(brPos, "schets", "qdisc show dev d0", schetsFix), // Q45
		bc(brPos, "schets", "class show dev d0", schetsFix), // Q46
		bc(brPos, "schets", "-s class show dev d0", schetsFix),
		bc(brCorner, "schets", "-o qdisc show dev d0", schetsFix),
		bc(brBoundary, "schets0", "qdisc show dev d0", schets0Fix), // no quanta (K25)
		bc(brBoundary, "schets0", "class show dev d0", schets0Fix),
		// cbs and etf.
		bc(brPos, "schtsn", "qdisc show dev d0", schtsnFix), // Q61, Q63
		bc(brPos, "schtsn", "-s qdisc show dev d0", schtsnFix),
		bc(brCorner, "schtsn", "qdisc show dev d0 handle 10:", schtsnFix),
		bc(brCorner, "schtsn", "qdisc show dev d0 handle 20:", schtsnFix),
		bc(brCorner, "schtsn", "-d qdisc show dev d0", schtsnFix),
		// multiq.
		bc(brPos, "schmultiq", "qdisc show dev d1 root", schmultiqFix), // Q60
		bc(brPos, "schmultiq", "class show dev d1", schmultiqFix),
		// mqprio: the root only.
		bc(brPos, "schmqprio", "qdisc show dev d1 root", schmqprioFix), // Q49, Q50, Q51, Q52
		bc(brPos, "schmqprio", "-s qdisc show dev d1 root", schmqprioFix),
		bc(brCorner, "schmqprio", "-o qdisc show dev d1 root", schmqprioFix), // Q48
		bc(brCorner, "schmqprio", "-d qdisc show dev d1 root", schmqprioFix),
		bc(brPos, "schmqprio", "class show dev d1", schmqprioFix),
		// taprio.
		bc(brPos, "schtaprio", "qdisc show dev d1 root", schtaprioFix), // Q55, Q56, Q58
		bc(brPos, "schtaprio", "-s qdisc show dev d1 root", schtaprioFix),
		bc(brCorner, "schtaprio", "-o qdisc show dev d1 root", schtaprioFix),
		bc(brPos, "schtaprio", "qdisc show dev d1", schtaprioFix), // its pfifo children
		bc(brPos, "schtaprio", "class show dev d1", schtaprioFix),
		bc(brCorner, "schtaprio2", "qdisc show dev d1 root", schtaprio2Fix),   // Q57
		bc(brBoundary, "schtaprio3", "qdisc show dev d1 root", schtaprio3Fix), // K28
		// Batches: each line in turn.
		bb(brPos, "schhfsc", "qdisc-then-class", "", false, batchFile(
			"qdisc show dev d0",
			"class show dev d0",
			"class show dev d0 classid 1:20"), schhfscFix),
		// A second "root" is "parent is already specified" (tcqdisc's
		// sticky-root); a plain show after it keeps the first line's root.
		bb(brCorner, "schtaprio2", "root-then-all", "", false, batchFile(
			"qdisc show dev d1 root",
			"class show dev d1",
			"qdisc show dev d1"), schtaprio2Fix),
	}
	notsupp := expect{RC: intp(2), Errno: "ENOTSUP", RefusedBy: refKernel}
	bad := []badValue{
		brBad("schdrr", "qdisc add dev d0 parent 1:1 handle 10: mqprio num_tc 2 map 0 0 1 1 queues 1@0 1@1 hw 0",
			schdrrFix, notsupp, "mqprio is root only, on a multi-queue device: a write gotc refuses"),
		brBad("schmultiq", "qdisc replace dev d1 root handle 100: taprio num_tc 2 map 0 0 1 1 queues 4@0 4@4"+
			" base-time 0 sched-entry S 01 300000", schmultiqFix,
			expect{RC: intp(2), Errno: "EINVAL", RefusedBy: refKernel}, "taprio without a clockid"),
		brBad("schtsn", "qdisc replace dev d0 parent 1:1 handle 10: cbs idleslope 20000 sendslope -980000"+
			" hicredit 30 locredit -1470 offload 1", schtsnFix, notsupp, "cbs offload needs ndo_setup_tc"),
		brBad("schdrr", "qdisc add dev d0 parent 1:2 handle 20: etf clockid CLOCK_TAI delta 300000 offload",
			schdrrFix, notsupp, "etf offload needs ndo_setup_tc"),
		brBad("schdrr", "class show dev nosuch", schdrrFix, tcNodev, "no such device: Cannot find device, rc 1"),
	}
	sp := tcSpace("tcsched", cmds, bad)
	sp.Build = func(vals []value) row { return tcBuildMods(tcSchedModules, vals) }
	return sp
}
