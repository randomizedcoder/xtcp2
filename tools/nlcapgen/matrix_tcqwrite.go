package main

import (
	"slices"
	"strings"
)

// The qdisc and class write space (gotc 17C): `tc qdisc add|change|
// replace|link|delete` and `tc class add|change|replace|delete` of the
// thirteen kinds of increment 9 (pfifo, bfifo, pfifo_head_drop, prio,
// pfifo_fast, fq_codel, fq, htb, tbf, sfq, netem, ingress, clsact), in one
// corpus directory, ws/tcqwrite.
//
// tc's side is tc_qdisc.c's tc_qdisc_modify and tc_class.c's
// tc_class_modify, the units parsers of tc_util.c, tc_core.c and
// tc_stab.c, and each kind's parse_qopt/parse_copt. The kernel's side is
// net/sched/sch_api.c (tc_modify_qdisc, tc_get_qdisc, tc_ctl_tclass) and
// each sch_<kind>.c init and change.
//
// Each write is the captured command; its setup is in Pre, and a readback
// (`tc -d qdisc show`, plus `tc -d class show` for classes) records what
// the kernel kept. The rows pin the plan's leads
// (docs/netlink/upstream/iproute2-tc-write.md): help that carries on (W1),
// ingress and clsact ending the frame (W2), the zero-handle duplicate
// (W3), parse_qopt's rc 1 (W4), class Garbage with no newline (W5), the
// estimator's argv step (W7), a kind with no parse_qopt (W8) and a percent
// rate on a dummy (W9).
//
// Writes are plain only: tc prints nothing for a write, so -j adds
// nothing. Determinism: explicit handles, fixed MACs, no packets.

// tcQwriteModules are the qdisc modules the rows use.
var tcQwriteModules = []string{
	"sch_prio", "sch_fq_codel", "sch_fq", "sch_htb", "sch_tbf", "sch_sfq", "sch_netem", "sch_ingress",
}

// tcQwriteDev is the dummy every row writes to.
var tcQwriteDev = []string{tcDummies("d0")}

// tcQwritePre is the dummy plus setup lines.
func tcQwritePre(lines ...string) []string {
	return append(append([]string{}, tcQwriteDev...), lines...)
}

// tcQwriteRoot is a root qdisc of handle 1: on d0, as a pre step.
func tcQwriteRoot(qdisc string) string { return "tc qdisc add dev d0 root handle 1: " + qdisc }

// tcQwriteKinds is each kind's add body (after "root handle 1:"), the
// option variants each documented option at least once.
var tcQwriteKinds = []struct {
	name string // the value name's prefix
	body []string
}{
	{"pfifo", []string{"pfifo", "pfifo limit 100"}},
	{"bfifo", []string{"bfifo", "bfifo limit 10kb"}},
	{"phd", []string{"pfifo_head_drop limit 50"}},
	{"prio", []string{"prio", "prio bands 5", "prio bands 3 priomap 0 1 2 2 1 2 0 0 1 1 1 1 1 1 1 1"}},
	{"pfast", []string{"pfifo_fast"}},
	{"fqcodel", []string{
		"fq_codel",
		"fq_codel limit 1000 flows 512 target 4ms interval 50ms quantum 1514 ecn",
		"fq_codel noecn ce_threshold 2ms ce_threshold_selector 0x01/0x03 memory_limit 16Mb drop_batch 32",
	}},
	{"fq", []string{
		"fq",
		"fq limit 1000 flow_limit 50 quantum 3028 initial_quantum 15140 maxrate 1gbit buckets 256 nopacing",
		"fq pacing refill_delay 50ms low_rate_threshold 1Mbit orphan_mask 511 timer_slack 20us ce_threshold 4ms horizon 2s horizon_drop",
		"fq horizon_cap weights 589824 196608 65536 bands 3 priomap 1 2 2 2 1 2 0 0 1 1 1 1 1 1 1 1",
	}},
	{"htb", []string{"htb", "htb default 10 r2q 20", "htb direct_qlen 50"}},
	{"tbf", []string{
		"tbf rate 1mbit burst 10kb latency 50ms",
		"tbf rate 10mbit burst 20kb/8 limit 30000 peakrate 12mbit mtu 2000 mpu 64",
		"tbf rate 5gbit burst 1mb latency 1s",
		"tbf rate 1Mbps burst 10k latency 70ms linklayer atm overhead 10",
	}},
	{"sfq", []string{
		"sfq",
		"sfq perturb 10 quantum 1514 limit 100 divisor 512 depth 64 flows 128",
		"sfq headdrop redflowlimit 100000 min 8000 max 60000 probability 0.2 ecn harddrop",
	}},
	{"netem", []string{
		"netem delay 100ms",
		"netem delay 100ms 10ms 25% loss 1% 30% duplicate 1% corrupt 0.1% reorder 25% 50% gap 5",
		"netem delay 10ms 2ms distribution normal",
		"netem loss state 1% 10% 5% 1% 2% limit 3000",
		"netem loss gemodel 1% 10% 70% 0.1% ecn",
		"netem rate 1mbit 20 100 5 slot 800us 1ms packets 10 bytes 3000",
		"netem slot distribution pareto 1ms 100us",
		"netem delay 1s seed 7",
	}},
}

// tcQwriteVerbs are the qdisc verbs other than add, each run on a root
// qdisc an add made first.
var tcQwriteVerbs = []struct {
	verb string
	kind brKind
}{
	{"change", brPos}, {"replace", brPos}, {"link", brCorner}, {"delete", brPos},
}

// tcqwriteSpace: the qdisc and class writes of the increment-9 kinds.
func tcqwriteSpace() *space {
	var cmds []value
	rb := map[string][]readback{}
	add := func(kind brKind, name, words string, pre []string, class bool) {
		val := bc(kind, name, words, pre)
		cmds = append(cmds, val)
		r := []readback{{Name: "dump", Cmd: "tc -d qdisc show dev d0"}}
		if class {
			r = append(r, readback{Name: "class", Cmd: "tc -d class show dev d0"})
		}
		rb[val.Name] = r
	}
	// sfq and htb have no kernel qdisc change op: change, replace and link on
	// them are the kernel's EINVAL, "Change operation not supported by specified qdisc." (K44).
	var verbBad []badValue
	noChange := expect{RC: intp(2), Errno: "EINVAL", RefusedBy: refKernel}
	// Each kind's add bodies, then the other verbs on its first body.
	for _, k := range tcQwriteKinds {
		for i, body := range k.body {
			add(brPos, k.name+string(rune('0'+i)), "qdisc add dev d0 root handle 1: "+body, tcQwriteDev, false)
		}
		for _, vb := range tcQwriteVerbs {
			body := k.body[len(k.body)-1]
			words := "qdisc " + vb.verb + " dev d0 root handle 1: " + body
			if vb.verb == "delete" {
				words = "qdisc delete dev d0 root"
			}
			if (k.name == "sfq" || k.name == "htb") && vb.verb != "delete" {
				verbBad = append(verbBad, brBad(k.name+"-"+vb.verb, words, tcQwritePre(tcQwriteRoot(k.body[0])), noChange,
					k.name+" has no change op: EINVAL, rc 2 (K44)"))
				continue
			}
			add(vb.kind, k.name+"-"+vb.verb, words, tcQwritePre(tcQwriteRoot(k.body[0])), false)
		}
	}
	// ingress and clsact: the frame ends at the kind (W2).
	add(brPos, "ingress", "qdisc add dev d0 ingress", tcQwriteDev, false)
	add(brPos, "ingress-blk", "qdisc add dev d0 ingress_block 5 ingress", tcQwriteDev, false)
	add(brPos, "ingress-del", "qdisc del dev d0 ingress", tcQwritePre("tc qdisc add dev d0 ingress"), false)
	add(brPos, "clsact", "qdisc add dev d0 clsact", tcQwriteDev, false)
	add(brPos, "clsact-blk", "qdisc add dev d0 ingress_block 5 egress_block 6 clsact", tcQwriteDev, false)
	add(brPos, "clsact-del", "qdisc delete dev d0 clsact", tcQwritePre("tc qdisc add dev d0 clsact"), false)
	add(brCorner, "w2-handle", "qdisc add dev d0 handle 5: ingress", tcQwriteDev, false)
	// The common frame: parent, estimator, stab, prefixes.
	htbPre := tcQwritePre(tcQwriteRoot("htb default 10"))
	add(brPos, "parent", "qdisc add dev d0 parent 1:10 handle 10: pfifo limit 10",
		tcQwritePre(tcQwriteRoot("htb default 10"), "tc class add dev d0 parent 1: classid 1:10 htb rate 1mbit"), false)
	add(brPos, "est", "qdisc add dev d0 root handle 1: estimator 1s 8s pfifo", tcQwriteDev, false) // W7
	add(brCorner, "est-prefix", "qdisc add dev d0 root handle 1: est 250ms 1s pfifo", tcQwriteDev, false)
	add(brPos, "stab", "qdisc add dev d0 root handle 1: stab overhead 20 linklayer atm mtu 2048 tsize 128 mpu 64 pfifo",
		tcQwriteDev, false)
	add(brBoundary, "stab0", "qdisc add dev d0 root handle 1: stab overhead 4 pfifo", tcQwriteDev, false)
	add(brPos, "stab-eth", "qdisc add dev d0 root handle 1: stab linklayer ethernet overhead 14 tbf rate 1mbit burst 10kb latency 50ms",
		tcQwriteDev, false)
	add(brCorner, "verb-prefix", "qdisc a dev d0 root handle 1: pfifo", tcQwriteDev, false)
	add(brCorner, "nohandle", "qdisc add dev d0 root pfifo", tcQwriteDev, false)
	add(brCorner, "w1-help", "qdisc add dev d0 root handle 1: help pfifo", tcQwriteDev, false) // W1
	add(brCorner, "w3-zero", "qdisc add dev d0 root handle 0: handle 1: pfifo", tcQwriteDev, false)
	// htb classes.
	add(brPos, "class-add", "class add dev d0 parent 1: classid 1:10 htb rate 1mbit", htbPre, true)
	add(brPos, "class-full", "class add dev d0 parent 1: classid 1:10 htb rate 1mbit ceil 2mbit burst 15k cburst 20k prio 2 quantum 1500 mtu 1600 mpu 64 overhead 4",
		htbPre, true)
	add(brBoundary, "class-64", "class add dev d0 parent 1: classid 1:10 htb rate 40gbit ceil 50gbit", htbPre, true)
	add(brPos, "class-est", "class add dev d0 parent 1: classid 1:10 estimator 1s 4s htb rate 1mbit", htbPre, true)
	add(brPos, "class-atm", "class add dev d0 parent 1: classid 1:10 htb rate 1mbit linklayer atm", htbPre, true)
	add(brPos, "class-root", "class add dev d0 root classid 1:1 htb rate 5mbit", htbPre, true)
	classPre := append(append([]string{}, htbPre...), "tc class add dev d0 parent 1: classid 1:10 htb rate 1mbit")
	add(brPos, "class-change", "class change dev d0 parent 1: classid 1:10 htb rate 2mbit ceil 3mbit", classPre, true)
	add(brPos, "class-replace", "class replace dev d0 parent 1: classid 1:10 htb rate 3mbit", classPre, true)
	add(brPos, "class-del", "class del dev d0 classid 1:10", classPre, true)
	add(brCorner, "class-help", "class add dev d0 parent 1: classid 1:20 help htb rate 1mbit", htbPre, true) // W1
	// Batches: two writes, then a write that fails and one after it.
	cmds = append(cmds,
		bb(brPos, "batch", "two", "", false, batchFile(
			"qdisc add dev d0 root handle 1: htb default 10",
			"class add dev d0 parent 1: classid 1:10 htb rate 1mbit"), tcQwriteDev),
	)
	rb[cmds[len(cmds)-1].Name] = []readback{{Name: "dump", Cmd: "tc -d qdisc show dev d0"}, {Name: "class", Cmd: "tc -d class show dev d0"}}
	exists := expect{RC: intp(2), Errno: "EEXIST", RefusedBy: refKernel}
	bad := slices.Concat([]badValue{
		brBad("exists", "qdisc add dev d0 root handle 1: pfifo", tcQwritePre(tcQwriteRoot("pfifo")), exists,
			"add over an existing root: the kernel's EEXIST, rc 2"),
		brBad("nochange", "qdisc change dev d0 root handle 1: pfifo limit 5", tcQwriteDev,
			expect{RC: intp(2), Errno: "ENOENT", RefusedBy: refKernel}, "change with no such qdisc (K44)"),
		brBad("deldefault", "qdisc del dev d0 root", tcQwriteDev,
			expect{RC: intp(2), Errno: "ENOENT", RefusedBy: refKernel}, "delete of the default root (K44)"),
		brBad("classless", "class add dev d0 parent 1: classid 1:10 pfifo", tcQwritePre(tcQwriteRoot("pfifo")),
			expect{RC: intp(1), RefusedBy: refIproute2}, "Qdisc \"pfifo\" is classless, rc 1 (W4)"),
		brBad("badopt", "qdisc add dev d0 root handle 1: pfifo limit zz", tcQwriteDev,
			expect{RC: intp(1), RefusedBy: refIproute2}, "a parse_qopt failure returns 1 (W4)"),
		brBad("badunit", "qdisc add dev d0 root handle 1: tbf rate 1zbit burst 10kb latency 50ms", tcQwriteDev,
			expect{RC: intp(1), RefusedBy: refIproute2}, "a bad rate unit"),
		brBad("nokind", "qdisc add dev d0 root handle 1: nosuchkind", tcQwriteDev,
			expect{RC: intp(2), Errno: "ENOENT", RefusedBy: refKernel}, "an unknown kind: tc sends it and the kernel refuses"),
		brBad("w5-classless", "class add dev d0 parent 1: classid 1:10 nosuchkind rate 1", htbPre, expect{RC: intp(1), RefusedBy: refIproute2},
			"an unknown class kind is \"classless\", rc 1, not W5's Garbage"),
		brBad("qunknown", "qdisc add dev d0 root handle 1: nosuchkind rate 1", tcQwriteDev, expect{RC: intp(1), RefusedBy: refIproute2},
			"Unknown qdisc, hence option unparsable, rc 1"),
		brBad("w8-clsact", "qdisc add dev d0 clsact foo", tcQwriteDev, expect{RC: intp(1), RefusedBy: refIproute2},
			"clsact's own parse_qopt refuses foo, rc 1 (not W8)"),
		brBad("w2-after", "qdisc add ingress dev d0", tcQwriteDev, expect{RC: intp(1), RefusedBy: refIproute2},
			"the words after ingress go to its parse_qopt: What is \"dev\"?, rc 1 (W2)"),
		brBad("noroot", "qdisc add dev d0 handle 1: pfifo", tcQwriteDev,
			expect{RC: intp(2), Errno: "ENOENT", RefusedBy: refKernel}, "no parent: Specified qdisc not found (K44)"),
		brBad("w9-pct", "class add dev d0 parent 1: classid 1:10 htb rate 10%", htbPre, expect{RC: intp(1), RefusedBy: refIproute2},
			"a percent rate on a dummy with no speed, rc 1 (W9)"),
		brBad("htboffload", "qdisc add dev d0 root handle 1: htb offload", tcQwriteDev,
			expect{RC: intp(2), Errno: "ENOTSUP", RefusedBy: refKernel}, "offload needs hw-tc-offload: ENOTSUP, rc 2"),
		brBad("dupdev", "qdisc add dev d0 dev d0 root pfifo", tcQwriteDev, tcTool, "dev twice: duparg"),
		brBad("dupparent", "qdisc add dev d0 root root pfifo", tcQwriteDev, tcTool, "root twice"),
		brBad("classhandle", "class add dev d0 parent 1: handle 1:10 htb rate 1mbit", htbPre, tcTool,
			"class handle: try classid"),
		brBad("nodev", "qdisc add dev nosuch root handle 1: pfifo", nil, tcNodev, "a missing device: Cannot find device, rc 1"),
		bbBad("batch", "fail-force", "-force", false, batchFile(
			"qdisc add dev d0 root handle 1: pfifo limit zz",
			"qdisc add dev d0 root handle 1: pfifo"), tcQwriteDev, tcAck1,
			"with -force the failing write is passed and the next one sent; rc 1"),
	}, verbBad)
	sp := &space{
		Name: "tcqwrite",
		Axes: []axis{{Name: "opt", Values: []value{v("plain", "", f("json", "0"))}}, {Name: "cmd", Values: cmds}},
		Bad:  bad,
	}
	sp.NegBase = func([]value) bool { return true }
	sp.Build = func(vals []value) row {
		r := tcBuildMods(tcQwriteModules, vals)
		r.Readback = rb[vals[len(vals)-1].Name]
		return r
	}
	return sp
}

// tcQwriteWords is a write row's words, for tests.
func tcQwriteWords(r row) string { return strings.TrimPrefix(r.Cmd, "tc ") }
