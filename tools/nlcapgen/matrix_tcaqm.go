package main

import "strings"

// The AQM and leaf qdisc space (gotc 16C): `tc qdisc show` (and `tc class
// show` where the kind has classes) of the thirteen kinds increment 15
// left (red, gred, choke, sfb, codel, pie, fq_pie, hhf, cake, cake_mq,
// dualpi2, plug, skbprio), each command plain and -j, in one corpus
// directory, ws/tcaqm.
//
// tc's side is tc/q_red.c, q_gred.c, q_choke.c, q_sfb.c, q_codel.c,
// q_pie.c, q_fq_pie.c, q_hhf.c, q_cake.c, q_dualpi2.c, q_plug.c and
// q_skbprio.c, with tc_red.c and tc_qevent.c, under tc_qdisc.c's and
// tc_class.c's frames. The kernel's side is each net/sched/sch_<kind>.c
// dump and dump_stats.
//
// The rows pin the plan's leads (docs/netlink/upstream/iproute2-tc-aqm.md):
// red's Scell_log with a qevent (Q66) and its flags (Q87); sfb's wrapped
// rehash and db (Q73); cake's hard newlines (Q79), ack-filter (Q80), rtt
// and fwmark (Q81) and every diffserv mode; dualpi2's keys (Q83), its
// split_gso (Q84) and its blank -s line (Q86).
//
// cake_mq needs a multi-queue device: a dummy with four tx queues.
//
// Determinism: explicit handles, fixed MACs, no packets, so every
// counter, delay and probability is idle. sfb's times are multiples of
// 4 ms (K34). Writes appear only as negatives.

// tcAqmModules are the qdisc modules the fixtures use; cake_mq is in
// sch_cake.
var tcAqmModules = []string{
	"sch_cake", "sch_choke", "sch_codel", "sch_dualpi2", "sch_fq_pie", "sch_gred", "sch_hhf", "sch_pie",
	"sch_plug", "sch_red", "sch_sfb", "sch_skbprio",
}

// tcAqmFix is a dummy with one qdisc at the root.
func tcAqmFix(qdisc string) []string {
	return []string{tcDummies("d0"), "tc qdisc add dev d0 root handle 1: " + qdisc}
}

// tcAqmRed is red's common head.
const tcAqmRed = "red limit 400000 min 30000 max 90000 avpkt 1000 burst 55 bandwidth 10Mbit"

var (
	aqmredFix   = tcAqmFix(tcAqmRed)
	aqmred2Fix  = tcAqmFix(tcAqmRed + " ecn harddrop adaptive qevent early_drop block 10") // Q66, Q87
	aqmchokeFix = tcAqmFix("choke limit 1000 bandwidth 10Mbit ecn min 100 max 300 burst 200")
	// gred: four VQs, two configured, with grio.
	aqmgredFix = []string{
		tcDummies("d0"),
		"tc qdisc add dev d0 root handle 1: gred setup vqs 4 default 0 grio",
		"tc qdisc change dev d0 root handle 1: gred vq 0 prio 1 limit 60KB min 15KB max 45KB avpkt 1000 burst 20 bandwidth 10Mbit probability 0.1",
		"tc qdisc change dev d0 root handle 1: gred vq 1 prio 2 limit 60KB min 15KB max 45KB avpkt 1000 burst 20 bandwidth 10Mbit probability 0.2 ecn",
	}
	aqmgred0Fix   = tcAqmFix("gred setup vqs 2 default 1") // no VQ configured
	aqmsfbFix     = tcAqmFix("sfb")
	aqmsfb2Fix    = tcAqmFix("sfb rehash 5000000 db 5000000 limit 100") // Q73
	aqmcodelFix   = tcAqmFix("codel")
	aqmcodel2Fix  = tcAqmFix("codel limit 500 target 4ms interval 50ms ecn ce_threshold 2ms")
	aqmpieFix     = tcAqmFix("pie")
	aqmpie2Fix    = tcAqmFix("pie limit 500 target 10ms tupdate 20ms alpha 2 beta 20 bytemode ecn dq_rate_estimator")
	aqmfqpieFix   = tcAqmFix("fq_pie")
	aqmfqpie2Fix  = tcAqmFix("fq_pie limit 2048 flows 256 quantum 1514 memory_limit 16Mb ecn_prob 20 ecn bytemode dq_rate_estimator")
	aqmhhfFix     = tcAqmFix("hhf")
	aqmhhf2Fix    = tcAqmFix("hhf limit 500 quantum 1514 hh_limit 1024 reset_timeout 50ms admit_bytes 65536 evict_timeout 500ms non_hh_weight 4")
	aqmcakeFix    = tcAqmFix("cake") // diffserv3, raw (K38)
	aqmcake2Fix   = tcAqmFix("cake bandwidth 100Mbit diffserv4 dual-srchost nat wash ingress ack-filter rtt 20ms atm overhead 44 mpu 64 memlimit 4Mb fwmark 0xff no-split-gso")
	aqmcake3Fix   = tcAqmFix("cake autorate-ingress diffserv8 dual-dsthost ack-filter-aggressive")
	aqmcake4Fix   = tcAqmFix("cake besteffort flowblind")
	aqmcake5Fix   = tcAqmFix("cake bandwidth 10Mbit precedence triple-isolate docsis")
	aqmcakemqFix  = []string{tcDummies("d1/4"), "tc qdisc add dev d1 root handle 1: cake_mq bandwidth 100Mbit"}
	aqmdpiFix     = tcAqmFix("dualpi2")
	aqmdpi2Fix    = tcAqmFix("dualpi2 limit 5000 memlimit 4000000 step_thresh 10 drop_on_overload drop_enqueue any_ect no_split_gso classic_protection 20")
	aqmdpi3Fix    = tcAqmFix("dualpi2 step_thresh 1ms max_rtt 50ms typical_rtt 10ms")
	aqmplugFix    = tcAqmFix("plug limit 1000")
	aqmskbprioFix = tcAqmFix("skbprio limit 100")
)

// tcaqmSpace: `tc qdisc show` of the thirteen kinds, and `class show`
// where the kind has classes.
func tcaqmSpace() *space {
	var cmds []value
	// show adds `qdisc show` plus each extra form for a fixture.
	show := func(kind brKind, fix string, pre []string, extra ...string) {
		dev := "d0"
		if fix == "aqmcakemq" {
			dev = "d1"
		}
		cmds = append(cmds, bc(kind, fix, "qdisc show dev "+dev, pre))
		for _, e := range extra {
			cmds = append(cmds, bc(brCorner, fix, strings.ReplaceAll(e, "DEV", dev), pre))
		}
	}
	full := []string{"-s qdisc show dev DEV", "-d qdisc show dev DEV", "-o qdisc show dev DEV"}
	show(brPos, "aqmred", aqmredFix, full...)
	show(brPos, "aqmred2", aqmred2Fix, full...)
	show(brPos, "aqmchoke", aqmchokeFix, full...)
	show(brPos, "aqmgred", aqmgredFix, full...)
	show(brBoundary, "aqmgred0", aqmgred0Fix, "-s qdisc show dev DEV")
	show(brPos, "aqmsfb", aqmsfbFix, "-s qdisc show dev DEV")
	show(brBoundary, "aqmsfb2", aqmsfb2Fix)
	show(brPos, "aqmcodel", aqmcodelFix, "-s qdisc show dev DEV")
	show(brPos, "aqmcodel2", aqmcodel2Fix)
	show(brPos, "aqmpie", aqmpieFix, "-s qdisc show dev DEV")
	show(brPos, "aqmpie2", aqmpie2Fix, "-s qdisc show dev DEV")
	show(brPos, "aqmfqpie", aqmfqpieFix, "-s qdisc show dev DEV")
	show(brPos, "aqmfqpie2", aqmfqpie2Fix)
	show(brPos, "aqmhhf", aqmhhfFix, "-s qdisc show dev DEV")
	show(brPos, "aqmhhf2", aqmhhf2Fix)
	show(brPos, "aqmcake", aqmcakeFix, "-s qdisc show dev DEV", "-d qdisc show dev DEV", "-s -o qdisc show dev DEV", "class show dev DEV")
	show(brPos, "aqmcake2", aqmcake2Fix, "-s qdisc show dev DEV")
	show(brPos, "aqmcake3", aqmcake3Fix, "-s qdisc show dev DEV")
	show(brPos, "aqmcake4", aqmcake4Fix, "-s qdisc show dev DEV")
	show(brPos, "aqmcake5", aqmcake5Fix, "-s qdisc show dev DEV")
	show(brPos, "aqmcakemq", aqmcakemqFix, "qdisc show dev DEV root", "-s qdisc show dev DEV", "class show dev DEV")
	show(brPos, "aqmdpi", aqmdpiFix, "-s qdisc show dev DEV", "-o qdisc show dev DEV")
	show(brPos, "aqmdpi2", aqmdpi2Fix, "-s qdisc show dev DEV")
	show(brPos, "aqmdpi3", aqmdpi3Fix)
	show(brCorner, "aqmplug", aqmplugFix, "-s qdisc show dev DEV")
	show(brPos, "aqmskbprio", aqmskbprioFix, "-s qdisc show dev DEV", "class show dev DEV")
	cmds = append(cmds,
		// Batch lines take no options; the batch's own apply to each line.
		bb(brPos, "aqmcake", "qdisc-then-class", "-s", false, batchFile(
			"qdisc show dev d0",
			"class show dev d0",
			"qdisc show dev d0"), aqmcakeFix),
		bb(brCorner, "aqmred2", "red-twice", "-d", false, batchFile(
			"qdisc show dev d0",
			"qdisc show dev d0"), aqmred2Fix),
	)
	bad := []badValue{
		brBad("aqmplug", "qdisc add dev d0 parent 1: handle 2: cake_mq", aqmplugFix,
			expect{RC: intp(2), Errno: "ENOTSUP", RefusedBy: refKernel}, "cake_mq under a classless parent: \"Parent qdisc is not classful\", a write gotc refuses"),
		brBad("aqmcakemq", "qdisc replace dev d1 root handle 1: cake_mq autorate-ingress", aqmcakemqFix,
			expect{RC: intp(2), Errno: "ENOTSUP", RefusedBy: refKernel}, "cake_mq rejects autorate (K39)"),
		brBad("aqmplug", "qdisc change dev d0 root handle 1: gred vq 0 limit 60KB min 15KB max 45KB avpkt 1000", aqmplugFix,
			expect{RC: intp(2), Errno: "EINVAL", RefusedBy: refKernel}, "gred change on a qdisc that is not gred: the kernel rejects the kind"),
		brBad("aqmred", "qdisc show dev nosuch", aqmredFix, tcNodev, "no such device: Cannot find device, rc 1"),
	}
	sp := tcSpace("tcaqm", cmds, bad)
	sp.Build = func(vals []value) row { return tcBuildMods(tcAqmModules, vals) }
	return sp
}
