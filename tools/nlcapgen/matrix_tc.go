package main

import "strings"

// The tc show spaces (gotc 9C): tc(8)'s read-only `qdisc show` and
// `class show` over rtnetlink, each command plain and -j, in two corpus
// directories, ws/tcqdisc and ws/tcclass.
//
// tc's side is iproute2's tc/: main (tc/tc.c) matches its options by
// prefix in a fixed order, so -c is -cf (Q24) and -b is -batch; an unknown
// option or object, and every argument tc_qdisc_list or tc_class_list
// refuses, fails before any request. A list then asks the link by name
// (ll_name_to_index) when given a dev, else qdisc show dumps the links
// (ll_init_map), and sends its RTM_GETQDISC or RTM_GETTCLASS dump. A batch
// dumps the links once, before it opens its file.
//
// The kernel's side is net/sched/sch_api.c (tc_dump_qdisc, tc_dump_tclass,
// tc_fill_qdisc, tc_fill_tclass) and each kind's dump and dump_stats.
//
// The rows pin the plan's leads (docs/netlink/gotc.md, Q1-Q33): fq's two
// "throttled" (Q1) and its trailing-space keys (Q2), tbf's burst a string
// under -d (Q3) with mpu twice (Q4) and no peakrate in JSON (Q5), its
// latency's u32 wrap (Q6), netem's and htb's u32s printed signed (Q7,
// Q11), -r's bare buffer text in JSON (Q12), the /1 cell suffix (Q13),
// requeues twice (Q18), qdisc stats at top level and class stats under
// "stats" (Q19), the dev key dropped by a dev filter (Q20), -g under -j
// (Q22), -c as -cf (Q24), show help dumping anyway (Q25), the batch
// statics (Q29, Q30), -nm in a batch (Q31), -g's freed nodes read by the next batch line (Q34) and -nm -cf printing numbers
// when the default databases are missing (Q33).
//
// Determinism: every qdisc tc adds has an explicit handle (the kernel's
// autohandle is global, so an automatic one differs between batches),
// netem has a fixed seed, every device has a fixed MAC and addrgenmode
// none before it comes up, and nothing sends a packet, so every -s
// counter is 0. Writes appear only as negatives.

// tcModules are the qdisc kinds the fixtures add; pfifo, bfifo,
// pfifo_head_drop, pfifo_fast, mq and noqueue are built in.
var tcModules = []string{"sch_fq", "sch_fq_codel", "sch_htb", "sch_ingress", "sch_netem", "sch_prio", "sch_sfq", "sch_tbf"}

// tcDummies adds dummy devices with fixed MACs, each up after addrgenmode
// none. A name of the form "d1/4" has four tx queues, so it gets mq.
func tcDummies(names ...string) string {
	var b strings.Builder
	for i, n := range names {
		if i > 0 {
			b.WriteString(" && ")
		}
		q := ""
		if dev, nq, ok := strings.Cut(n, "/"); ok {
			n, q = dev, " numtxqueues "+nq
		}
		b.WriteString("ip link add " + n + " address 02:00:00:00:02:0" + string(rune('0'+i)) + q + " type dummy" +
			" && ip link set " + n + " addrgenmode none && ip link set " + n + " up")
	}
	return b.String()
}

// tcPrioMap is a 16-entry priomap over five bands.
const tcPrioMap = "0 1 2 3 4 0 1 2 3 4 0 1 2 3 4 0"

// qbaseFix: d0 keeps noqueue; d1 has four tx queues, so mq with an
// fq_codel per queue; d2 pfifo_fast; d3 prio with five bands.
var qbaseFix = []string{
	tcDummies("d0", "d1/4", "d2", "d3"),
	"tc qdisc add dev d1 root handle 1: mq",
	"tc qdisc add dev d2 root handle 2: pfifo_fast",
	"tc qdisc add dev d3 root handle 3: prio bands 5 priomap " + tcPrioMap,
}

// qfifoFix: prio on d0 with a pfifo, a bfifo and a pfifo_head_drop as its
// first three bands' children.
var qfifoFix = []string{
	tcDummies("d0"),
	"tc qdisc add dev d0 root handle 1: prio",
	"tc qdisc add dev d0 parent 1:1 handle 10: pfifo limit 10",
	"tc qdisc add dev d0 parent 1:2 handle 20: bfifo limit 3000",
	"tc qdisc add dev d0 parent 1:3 handle 30: pfifo_head_drop limit 7",
}

// qcodelFix: fq_codel with every option on d0, its defaults on d1.
var qcodelFix = []string{
	tcDummies("d0", "d1"),
	"tc qdisc add dev d0 root handle 1: fq_codel limit 1000 flows 512 target 3ms interval 50ms quantum 300" +
		" ecn ce_threshold 2ms memory_limit 4mb drop_batch 32",
	"tc qdisc add dev d1 root handle 2: fq_codel",
}

// qfqFix: fq with rate, flow and horizon options, weights and a priomap on
// d0 (Q1, Q2), its defaults on d1.
var qfqFix = []string{
	tcDummies("d0", "d1"),
	"tc qdisc add dev d0 root handle 1: fq maxrate 1gbit flow_limit 50 horizon 5s horizon_cap" +
		" bands 3 priomap 2 1 1 1 2 1 1 1 1 1 1 1 1 1 1 1 weights 65536 32768 16384",
	"tc qdisc add dev d1 root handle 2: fq",
}

// qhtbFix: htb on d0 with classes 1:1 (rate 1000gbit, a RATE64, Q15),
// 1:10 (quantum 3000000000, Q11) and 1:20; on d1, a chain of classes
// eight deep for -g.
var qhtbFix = []string{
	tcDummies("d0", "d1"),
	"tc qdisc add dev d0 root handle 1: htb default 10 r2q 5",
	"tc class add dev d0 parent 1: classid 1:1 htb rate 1000gbit",
	"tc class add dev d0 parent 1:1 classid 1:10 htb rate 1mbit ceil 2mbit burst 15k prio 2 quantum 3000000000",
	"tc class add dev d0 parent 1:1 classid 1:20 htb rate 2mbit ceil 4mbit cburst 3k prio 1",
	"tc qdisc add dev d0 parent 1:20 handle 20: pfifo limit 5",
	"tc qdisc add dev d1 root handle 1: htb" +
		" && tc class add dev d1 parent 1: classid 1:1 htb rate 10mbit" +
		" && tc class add dev d1 parent 1:1 classid 1:2 htb rate 9mbit" +
		" && tc class add dev d1 parent 1:2 classid 1:3 htb rate 8mbit" +
		" && tc class add dev d1 parent 1:3 classid 1:4 htb rate 7mbit" +
		" && tc class add dev d1 parent 1:4 classid 1:5 htb rate 6mbit" +
		" && tc class add dev d1 parent 1:5 classid 1:6 htb rate 5mbit" +
		" && tc class add dev d1 parent 1:6 classid 1:7 htb rate 4mbit" +
		" && tc class add dev d1 parent 1:1 classid 1:9 htb rate 1mbit",
}

// qtbfFix: tbf plain on d0; with peakrate and mtu on d1 (Q4, Q5); with a
// limit far above its rate on d2, so its latency passes 2^32 us (Q6).
var qtbfFix = []string{
	tcDummies("d0", "d1", "d2"),
	"tc qdisc add dev d0 root handle 1: tbf rate 1mbit burst 10k latency 50ms",
	"tc qdisc add dev d1 root handle 2: tbf rate 1mbit burst 10k latency 50ms peakrate 2mbit mtu 1600",
	"tc qdisc add dev d2 root handle 3: tbf rate 8kbit burst 1600 limit 100mb",
}

// qsfqFix: sfq with perturb on d0; with RED options and headdrop on d1.
var qsfqFix = []string{
	tcDummies("d0", "d1"),
	"tc qdisc add dev d0 root handle 1: sfq perturb 10 quantum 1514 divisor 1024",
	"tc qdisc add dev d1 root handle 2: sfq limit 100 redflowlimit 100000 min 8000 max 60000 probability 0.2 ecn headdrop",
}

// qnetemFix: netem with most options on d0; the Gilbert-Elliott loss
// model on d1; limit 3000000000 on d2 (Q7). Each has a fixed seed.
var qnetemFix = []string{
	tcDummies("d0", "d1", "d2"),
	"tc qdisc add dev d0 root handle 1: netem delay 10ms 1ms distribution normal loss 1% corrupt 0.1%" +
		" duplicate 1% reorder 25% 50% gap 5 rate 1mbit slot 1ms 2ms seed 42 ecn",
	"tc qdisc add dev d1 root handle 2: netem loss gemodel 1% 10% 70% 0.1% seed 7",
	"tc qdisc add dev d2 root handle 3: netem limit 3000000000 delay 10ms seed 1",
}

// qingressFix: ingress on d0, clsact on d1, clsact with shared blocks on
// d2.
var qingressFix = []string{
	tcDummies("d0", "d1", "d2"),
	"tc qdisc add dev d0 ingress",
	"tc qdisc add dev d1 clsact",
	"tc qdisc add dev d2 ingress_block 5 egress_block 6 clsact",
}

// tcNamesFile is a pre step writing a class names file, cls.txt, in the
// row's directory.
const tcNamesFile = "printf '1:1 top\\n1:10 web\\n0x10020 bulk\\n' > cls.txt"

// tcBuild makes a tc row of the opt and cmd values, loading tcModules.
func tcBuild(vals []value) row { return tcBuildMods(tcModules, vals) }

// tcBuildMods makes a tc row of the opt and cmd values, loading mods.
func tcBuildMods(mods []string, vals []value) row {
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
		Cmd:     strings.TrimSpace("tc " + joinArgs(optArg, cmdArg)),
		Proto:   "route",
		Modules: mods,
		Expect:  expect{RC: intp(0), RefusedBy: refNone},
		Axes:    mergeFacts(vals),
	}
	for _, p := range pre {
		r.Pre = append(r.Pre, preStep{Cmd: p})
	}
	// One row reads /proc/net/psched beside its capture: the clock
	// values tc_core_init scans, which gotc's replay reads as a file.
	if r.Cmd == "tc qdisc show" && r.Axes["fix"] == "bare" {
		r.Readback = []readback{{Name: "psched", Cmd: "cat /proc/net/psched"}}
	}
	return r
}

// tcOptAxis is each command plain and -j.
func tcOptAxis() axis {
	return axis{Name: "opt", Values: []value{
		v("plain", "", f("json", "0")),
		v("json", "-j", f("json", "1")),
	}}
}

// tcSpace is a tc space of commands and refusals, each positive plain and
// -j, each negative plain.
func tcSpace(name string, cmds []value, bad []badValue) *space {
	sp := &space{
		Name:  name,
		Axes:  []axis{tcOptAxis(), {Name: "cmd", Values: cmds}},
		Bad:   bad,
		Build: tcBuild,
	}
	sp.NegBase = func(vals []value) bool {
		return pickOf(sp.Axes, vals)["opt"] == "plain"
	}
	return sp
}

// The expectations of tc's refusals.
var (
	// tcTool is a refusal before any request: exit(-1) or a return of -1.
	tcTool = expect{RC: intp(255), RefusedBy: refIproute2}
	// tcAck is the same refusal in a batch, after its link dump.
	tcAck  = expect{RC: intp(255), RefusedBy: refAckThenFail}
	tcAck1 = expect{RC: intp(1), RefusedBy: refAckThenFail}
	// tcNodev is a dev the kernel does not know: -nodev() is 1.
	tcNodev = expect{RC: intp(1), Errno: "ENODEV", RefusedBy: refKernel}
)

// tcqdiscSpace: `tc qdisc show` over nine fixtures.
func tcqdiscSpace() *space {
	cmds := []value{
		// bare: only lo, noqueue.
		bc(brBoundary, "bare", "qdisc", nil), // no verb is show
		bc(brPos, "bare", "qdisc show", nil),
		bc(brCorner, "bare", "qdisc list", nil),
		bc(brCorner, "bare", "qdisc ls", nil), // lst by prefix
		bc(brBoundary, "bare", "-s qdisc show", nil),
		bc(brCorner, "bare", "qdisc show help", nil), // usage, then the dump anyway (Q25)
		bc(brCorner, "bare", "qdisc help", nil),
		bc(brCorner, "bare", "-V", nil),
		bc(brCorner, "bare", "help", nil),
		// qbase: noqueue, mq with fq_codel children, pfifo_fast, prio.
		bc(brPos, "qbase", "qdisc show", qbaseFix),
		bc(brPos, "qbase", "-s qdisc show", qbaseFix), // requeues twice (Q18), top-level stats (Q19)
		bc(brPos, "qbase", "-d qdisc show", qbaseFix),
		bc(brPos, "qbase", "qdisc show dev d1", qbaseFix), // no dev key (Q20)
		bc(brPos, "qbase", "qdisc show dev d3", qbaseFix),
		bc(brPos, "qbase", "qdisc show root", qbaseFix),
		bc(brPos, "qbase", "qdisc show dev d1 parent 1:2", qbaseFix),
		bc(brPos, "qbase", "qdisc show handle 3:", qbaseFix),
		bc(brPos, "qbase", "qdisc show invisible", qbaseFix),
		bc(brCorner, "qbase", "-r qdisc show dev d3", qbaseFix),
		bc(brCorner, "qbase", "-p qdisc show dev d3", qbaseFix),
		bc(brCorner, "qbase", "-o -s qdisc show dev d1", qbaseFix),
		bc(brCorner, "qbase", "-nm qdisc show dev d1", qbaseFix),
		bc(brCorner, "qbase", "qdisc show dev d0 dev d3", qbaseFix), // the last dev wins: no duparg
		bc(brCorner, "qbase", "- qdisc show dev d0", qbaseFix),      // a lone - is -stats
		// qfifo: prio with fifo children.
		bc(brPos, "qfifo", "qdisc show", qfifoFix),
		bc(brPos, "qfifo", "-s qdisc show", qfifoFix),
		bc(brPos, "qfifo", "-d qdisc show dev d0", qfifoFix),
		bc(brPos, "qfifo", "qdisc show dev d0 parent 1:2", qfifoFix),
		// qcodel: fq_codel with every option, and its defaults.
		bc(brPos, "qcodel", "qdisc show", qcodelFix),
		bc(brPos, "qcodel", "-s qdisc show", qcodelFix),
		bc(brPos, "qcodel", "-d qdisc show", qcodelFix),
		// qfq: fq with options, and its defaults.
		bc(brPos, "qfq", "qdisc show", qfqFix),    // "priomap " and "weights " keys (Q2)
		bc(brPos, "qfq", "-s qdisc show", qfqFix), // throttled twice (Q1)
		bc(brPos, "qfq", "-d qdisc show", qfqFix),
		// qhtb: htb and its child pfifo.
		bc(brPos, "qhtb", "qdisc show", qhtbFix),
		bc(brPos, "qhtb", "-s qdisc show dev d0", qhtbFix),
		bc(brPos, "qhtb", "-d qdisc show dev d0", qhtbFix),
		// qtbf: plain, peakrate, and a 2^32 us latency.
		bc(brPos, "qtbf", "qdisc show", qtbfFix),
		bc(brPos, "qtbf", "-d qdisc show", qtbfFix), // burst a string (Q3), mpu twice (Q4)
		bc(brPos, "qtbf", "-s qdisc show", qtbfFix),
		bc(brCorner, "qtbf", "-iec qdisc show", qtbfFix),
		bc(brBoundary, "qtbf", "qdisc show dev d2", qtbfFix), // the latency wraps in text only (Q6)
		// qsfq: perturb, and RED with headdrop.
		bc(brPos, "qsfq", "qdisc show", qsfqFix),
		bc(brPos, "qsfq", "-s qdisc show", qsfqFix),
		bc(brPos, "qsfq", "-d qdisc show", qsfqFix),
		// qnetem: most options, gemodel, limit 3000000000.
		bc(brPos, "qnetem", "qdisc show", qnetemFix), // limit printed signed (Q7)
		bc(brPos, "qnetem", "-s qdisc show", qnetemFix),
		bc(brPos, "qnetem", "-d qdisc show", qnetemFix),
		// qingress: ingress, clsact, and shared blocks.
		bc(brPos, "qingress", "qdisc show", qingressFix),
		bc(brPos, "qingress", "-s qdisc show", qingressFix),
		bc(brPos, "qingress", "-d qdisc show", qingressFix),
		bc(brPos, "qingress", "qdisc show ingress", qingressFix),
		bc(brCorner, "qingress", "qdisc show clsact", qingressFix), // clsact is ingress's parent too
		bc(brPos, "qingress", "qdisc show dev d1 clsact", qingressFix),
		// Batches: one link dump, then each line.
		bb(brPos, "qbase", "two", "", false, batchFile("qdisc show dev d1", "qdisc show dev d3"), qbaseFix),
		bb(brPos, "qbase", "stdin", "", true, batchFile("qdisc show dev d1", "qdisc show dev d3"), qbaseFix),
		// Q29: filter_ifindex is a static, so the second line is still
		// filtered to d1.
		bb(brCorner, "qbase", "sticky-dev", "", false, batchFile("qdisc show dev d1", "qdisc show"), qbaseFix),
		bb(brCorner, "qbase", "sticky-handle", "", false, batchFile("qdisc show handle 3:", "qdisc show"), qbaseFix),
		bb(brCorner, "qbase", "comments", "", false,
			batchFile("# qdiscs", "", `qdisc \`, "show dev d3 # prio"), qbaseFix),
		bb(brPos, "qbase", "force", "-force", false, batchFile("qdisc show dev d1", "qdisc show dev d3"), qbaseFix),
	}
	bad := []badValue{
		{Axis: "opt", Value: v("badopt", "-zz", nil), Expect: tcTool, Why: "an unknown option exits 255 before any socket"},
		brBad("bare", "bogus", nil, tcTool, "an unknown object returns -1"),
		brBad("bare", "qdisc bogus", nil, tcTool, "an unknown qdisc command returns -1"),
		brBad("bare", "-c qdisc show", nil, tcTool, "-c is -cf: qdisc is the names file, show an unknown object (Q24)"),
		brBad("bare", "--json qdisc show", nil, tcTool, "tc gives -- no meaning: --json is an unknown option"),
		brBad("bare", "qdisc show bogus", nil, tcTool, "an unknown word: What is \"bogus\"?"),
		brBad("bare", "qdisc show root root", nil, tcTool, "a second root: parent is already specified"),
		brBad("bare", "qdisc show root handle 1:", nil, tcTool, "handle after root: parent is already specified"),
		brBad("bare", "qdisc show parent zz", nil, tcTool, "an invalid parent ID"),
		brBad("bare", "qdisc show handle zz", nil, tcTool, "an invalid handle ID"),
		brBad("bare", "qdisc show dev", nil, tcTool, "dev with no value: the command line is not complete"),
		brBad("bare", "qdisc show dev nosuch", nil, tcNodev, "no such device: Cannot find device, rc 1"),
		brBad("bare", "-nm -cf nosuch qdisc show", nil, tcTool, "a -cf file that is not there"),
		brBad("bare", "-batch", nil, tcTool, "-batch with no file"),
		brBad("bare", "-batch nosuch", nil, tcAck1, "the batch file is opened after the link dump"),
		brBad("bare", "-netns nosuch qdisc show", nil, tcTool, "no such named namespace"),
		brBad("bare", "-netns", nil, tcTool, "-netns with no name opens \"(null)\""),
		brBad("bare", "qdisc add dev nosuch root handle 1: pfifo", nil, tcNodev,
			"a write, recorded so gotc's refusal is never a pass"),
		bbBad("qbase", "fail-stops", "", false, batchFile("bogus", "qdisc show"), qbaseFix, tcAck1,
			"an unknown object: Command failed b.txt:1, and the batch stops"),
		bbBad("qbase", "fail-force", "-force", false, batchFile("bogus", "qdisc show dev d3"), qbaseFix, tcAck1,
			"with -force the batch goes on, and still exits 1"),
		bbBad("qbase", "sticky-root", "-force", false, batchFile("qdisc show root", "qdisc show root"), qbaseFix, tcAck,
			"filter_parent is still set, so the second root is an invarg exit, through -force (Q29)"),
		bbBad("qbase", "nodev-force", "-force", false, batchFile("qdisc show dev nosuch", "qdisc show dev d3"), qbaseFix,
			tcNodev, "nodev returns 1 and -force goes on"),
	}
	return tcSpace("tcqdisc", cmds, bad)
}

// tcclassSpace: `tc class show` over the fixtures with classes.
func tcclassSpace() *space {
	cmds := []value{
		// bare: no dev, so the kernel dumps nothing.
		bc(brBoundary, "bare", "class show", nil),
		bc(brBoundary, "bare", "class", nil),
		bc(brCorner, "bare", "class help", nil),
		bc(brCorner, "bare", "class show help", nil), // usage, then the dump anyway (Q25)
		// qbase: mq's and prio's classes.
		bc(brPos, "qbase", "class show dev d1", qbaseFix),
		bc(brPos, "qbase", "class show dev d3", qbaseFix),
		bc(brPos, "qbase", "-s class show dev d3", qbaseFix),
		bc(brCorner, "qbase", "class l dev d3", qbaseFix), // class has no link: l is list
		// qfifo: prio's classes with their leaves.
		bc(brPos, "qfifo", "class show dev d0", qfifoFix),
		bc(brPos, "qfifo", "-s class show dev d0", qfifoFix),
		bc(brPos, "qfifo", "-d class show dev d0", qfifoFix),
		// qcodel: fq_codel has classes only for live flows.
		bc(brBoundary, "qcodel", "class show dev d0", qcodelFix),
		// qhtb: htb classes.
		bc(brPos, "qhtb", "class show dev d0", qhtbFix),
		bc(brPos, "qhtb", "-s class show dev d0", qhtbFix),    // stats under "stats" (Q19)
		bc(brPos, "qhtb", "-d class show dev d0", qhtbFix),    // quantum signed (Q11), /1 (Q13)
		bc(brCorner, "qhtb", "-r class show dev d0", qhtbFix), // bare buffer text in JSON (Q12)
		bc(brPos, "qhtb", "class show dev d0 parent 1:1", qhtbFix),
		bc(brPos, "qhtb", "class show dev d0 classid 1:10", qhtbFix),
		bc(brPos, "qhtb", "class show dev d0 root", qhtbFix),
		bc(brPos, "qhtb", "class show dev d0 qdisc 1:", qhtbFix),
		bc(brCorner, "qhtb", "class show dev d0 qdisc 20:", qhtbFix),
		bc(brPos, "qhtb", "-g class show dev d1", qhtbFix), // -j prints [] then the graph (Q22)
		bc(brCorner, "qhtb", "-g -s class show dev d1", qhtbFix),
		bc(brCorner, "qhtb", "-g class show dev d1 classid 1:3", qhtbFix),                                 // the graph ignores the filter (Q22)
		bc(brCorner, "qhtb", "-nm -cf cls.txt class show dev d0", append(clonePre(qhtbFix), tcNamesFile)), // Q33
		bc(brCorner, "qhtb", "-iec -d class show dev d0", qhtbFix),
		bc(brCorner, "qhtb", "-o -s class show dev d0", qhtbFix),
		// qtbf, qsfq, qnetem: their single class or none.
		bc(brPos, "qtbf", "class show dev d0", qtbfFix),
		bc(brPos, "qsfq", "-s class show dev d0", qsfqFix),
		bc(brPos, "qnetem", "class show dev d0", qnetemFix),
		bc(brPos, "qingress", "class show dev d1", qingressFix),
		// Batches.
		bb(brPos, "qhtb", "two", "", false, batchFile("class show dev d0", "class show dev d1"), qhtbFix),
		// Q30: filter_ifindex carries over, but the second line's
		// request has ifindex 0, which the kernel answers with nothing.
		bb(brCorner, "qhtb", "sticky-dev", "", false, batchFile("class show dev d0", "class show"), qhtbFix),
		bb(brCorner, "qhtb", "classid-reset", "", false,
			batchFile("class show dev d0 classid 1:10", "class show dev d0"), qhtbFix),
		// Q31: in a batch -nm never loads the names.
		bb(brCorner, "qhtb", "names", "-nm", false, batchFile("class show dev d0"), qhtbFix),
	}
	bad := []badValue{
		brBad("bare", "class bogus", nil, tcTool, "an unknown class command returns -1"),
		brBad("bare", "class link", nil, tcTool, "class has no link verb"),
		brBad("bare", "class show bogus", nil, tcTool, "an unknown word: What is \"bogus\"?"),
		brBad("bare", "class show handle 1:", nil, tcTool, "class show takes no handle"),
		brBad("bare", "class show root root", nil, tcTool, "a second root: \"root\" is duplicate parent ID"),
		brBad("bare", "class show parent 1: parent 2:", nil, tcTool, "a second parent: duparg"),
		brBad("bare", "class show classid zz", nil, tcTool, "an invalid class ID"),
		brBad("bare", "class show qdisc zz", nil, tcTool, "an invalid qdisc ID"),
		brBad("bare", "class show dev lo dev lo", nil, tcTool, "dev given twice: duparg"),
		brBad("bare", "class show dev nosuch", nil, tcNodev, "no such device: Cannot find device, rc 1"),
		brBad("bare", "class add dev nosuch parent 1: classid 1:1 htb rate 1mbit", nil, tcNodev,
			"a write, recorded so gotc's refusal is never a pass"),
		// Q34: graph_cls_show frees every node it prints but leaves the
		// static list heads pointing at them, so a second -g line walks
		// freed memory: "!!!Deficit" with a garbage rta_len, then glibc's
		// double-free abort, rc 134. The garbage differs between runs.
		bbBad("qhtb", "graph-twice", "-g", false, batchFile("class show dev d1", "class show dev d1"), qhtbFix,
			expect{RC: intp(134), RefusedBy: refAckThenFail},
			"the second -g line reads the first line's freed nodes and aborts (Q34)"),
	}
	return tcSpace("tcclass", cmds, bad)
}
