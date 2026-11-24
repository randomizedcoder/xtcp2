package main

// The tc filter space (gotc 10C): tc(8)'s read-only `filter show/get` and
// `chain show/get` over rtnetlink, each command plain and -j, in one
// corpus directory, ws/tcfilter.
//
// tc's side is tc/tc_filter.c: tc_filter_list (RTM_GETTFILTER or
// RTM_GETCHAIN dump; no dev means ifindex 0, and block means
// TCM_IFINDEX_MAGIC_BLOCK after tc_qdisc_block_exists), tc_filter_get
// (NLM_F_ECHO, after the kind's parse_fopt adds an empty TCA_OPTIONS and
// sets tcm_handle) and print_filter, whose display filters are statics
// never reset between batch lines (Q29f). The printers are each kind's
// print_fopt (f_u32.c, f_fw.c, f_route.c, f_basic.c, f_cgroup.c, f_flow.c,
// f_matchall.c), m_ematch.c with em_*.c, and m_action.c with m_gact.c.
//
// The kernel's side is net/sched/cls_api.c (tc_dump_tfilter,
// tc_get_tfilter, tc_dump_chain, tc_ctl_chain) and each classifier's and
// action's dump.
//
// The rows pin the plan's leads (docs/netlink/upstream/iproute2-tc-filter.md):
// u32's nameless " eat " (L1), its duplicate "match" (L2) and "success"
// (L3) keys, -p's key decoding (L4, F4, F13), the unchecked pref (L40),
// the leading-space " What is" (L41), matchall's hard newline (L47), the
// filter statics (Q29f) and the unused list handle (Q29h), fprintf'd
// ematches in JSON (F1), route's two "name" keys (F3), strtol handles (F5),
// get_u32_handle's separators (F6), show help dumping (F7), no dev (F8),
// chain get's ignored words (F9), cmp's %d (F10), get's exit codes (F15),
// the handle's JSON type by kind (F18), gact's made-up prob (F19), -br's
// TERSE for every kind (F21), the fully filtered chain line (F22) and a
// typo taken as the kind (F23).
//
// Determinism: every filter has an explicit pref and handle where its
// kind takes one (u32's automatic handles are per block, so they repeat),
// every device has a fixed MAC and addrgenmode none before it comes up,
// and nothing sends a packet, so every -s counter is 0; gact's -s timers
// (installed, lastuse, firstuse) are the one allow-listed difference.
// Writes appear only as negatives.

// tcFilterModules are the qdisc, classifier, action and ematch modules
// the fixtures use; fout's act_mirred, act_police and cls_flower are there
// for gotc's refusals.
var tcFilterModules = []string{
	"sch_htb", "sch_ingress",
	"cls_basic", "cls_cgroup", "cls_flow", "cls_flower", "cls_fw", "cls_matchall", "cls_route", "cls_u32",
	"act_gact", "act_mirred", "act_police",
	"em_canid", "em_cmp", "em_meta", "em_nbyte", "em_u32",
}

// fu32Fix: u32 on d0's clsact ingress. Pref 1 has one key and gact; pref 2
// two keys (L2) with dport (F13) and a flowid; pref 3 a hash table of 16
// buckets, and a link with offset ... eat (L1), which lands in pref 1's
// table 800:; pref 5 an ip6 src (L4); pref 6 a u8 key -p cannot name (F4).
var fu32Fix = []string{
	tcDummies("d0"),
	"tc qdisc add dev d0 clsact",
	"tc filter add dev d0 ingress pref 1 protocol ip handle 800::1 u32 match ip src 10.0.0.0/8 action pass",
	"tc filter add dev d0 ingress pref 2 protocol ip u32 match ip src 10.0.0.0/8 match ip dport 80 0xffff flowid 1:1",
	"tc filter add dev d0 ingress pref 3 protocol ip handle 2: u32 divisor 16",
	"tc filter add dev d0 ingress pref 3 protocol ip u32 ht 800:: match ip protocol 6 0xff" +
		" link 2: offset at 0 mask 0x0f00 shift 6 plus 0 eat",
	"tc filter add dev d0 ingress pref 5 protocol ipv6 u32 match ip6 src 2001:db8::/32 action drop",
	"tc filter add dev d0 ingress pref 6 protocol ip u32 match u8 64 0xff at 8 action pass",
}

// fsimpleFix: fw and route under d1's htb, matchall with gact on d0's
// clsact egress.
var fsimpleFix = []string{
	tcDummies("d0", "d1"),
	"tc qdisc add dev d0 clsact",
	"tc qdisc add dev d1 root handle 1: htb",
	"tc class add dev d1 parent 1: classid 1:1 htb rate 1mbit",
	"tc filter add dev d1 parent 1: pref 10 protocol ip handle 5 fw classid 1:1",
	"tc filter add dev d1 parent 1: pref 11 protocol ip handle 6/0xff fw action pass",
	"tc filter add dev d1 parent 1: pref 20 protocol ip route from 1 to 2 classid 1:1",
	"tc filter add dev d1 parent 1: pref 21 protocol ip route fromif d0 to 3 classid 1:1",
	"tc filter add dev d0 egress pref 2 protocol all handle 1 matchall skip_hw action drop",
	"tc filter add dev d0 egress pref 3 protocol all handle 1 matchall action drop",
}

// fflowFix: flow's hash and map modes under d0's htb.
var fflowFix = []string{
	tcDummies("d0"),
	"tc qdisc add dev d0 root handle 1: htb",
	"tc class add dev d0 parent 1: classid 1:1 htb rate 1mbit",
	"tc filter add dev d0 parent 1: pref 30 protocol ip handle 1 flow hash keys src,dst divisor 1024 baseclass 1:1",
	"tc filter add dev d0 parent 1: pref 31 protocol ip handle 2 flow map key mark addend 1 divisor 16",
}

// fematchFix: basic with each ematch and an and/or/not tree on d0's
// clsact ingress, cgroup with meta on its egress. Every match is followed
// by a classid or an action: an ematch that ends the line is refused by
// parse_ematch's look-ahead (F25).
var fematchFix = []string{
	tcDummies("d0"),
	"tc qdisc add dev d0 clsact",
	`tc filter add dev d0 ingress pref 1 protocol all handle 1 basic match "cmp(u16 at 0 layer 1 mask 0xff eq 8)" action pass`,
	`tc filter add dev d0 ingress pref 2 protocol all handle 2 basic match "u32(u32 0x0a000000 0xff000000 at 12)"` +
		` and "meta(nf_mark eq 5)" classid 1:1`,
	`tc filter add dev d0 ingress pref 3 protocol all handle 3 basic match "not nbyte(\"abc\" at 0 layer 2)"` +
		` or "meta(priority gt 3)" classid 1:2`,
	`tc filter add dev d0 ingress pref 4 protocol all handle 4 basic match "cmp(u32 at 0 layer 2 eq 0x80000000)" classid 1:3`,
	`tc filter add dev d0 ingress pref 5 protocol can handle 5 basic match "canid(sff 0x123 eff 0x456)" classid 1:4`,
	`tc filter add dev d0 ingress pref 6 protocol all handle 6 basic match "meta(nf_mark eq 0x80000000)" classid 1:5`,
	`tc filter add dev d0 egress pref 1 protocol all handle 1 cgroup match "meta(nf_mark eq 1)" action pass`,
}

// fchainFix: d0's clsact ingress with filters in chains 0, 5 and 7 and an
// empty chain 9 (F22); d1's ingress in shared block 10.
var fchainFix = []string{
	tcDummies("d0", "d1"),
	"tc qdisc add dev d0 clsact",
	"tc chain add dev d0 ingress chain 9",
	"tc filter add dev d0 ingress pref 1 protocol all handle 1 matchall action goto chain 5",
	`tc filter add dev d0 ingress chain 5 pref 1 protocol ip handle 1 basic match "cmp(u8 at 9 layer network eq 6)" action drop`,
	"tc filter add dev d0 ingress chain 7 pref 1 protocol all handle 1 matchall action pass",
	"tc qdisc add dev d1 ingress_block 10 clsact",
	"tc filter add block 10 pref 1 protocol all handle 1 matchall action pass",
}

// foutFix: kinds out of the slice, recorded so gotc's refusal names each
// one: mirred, police and flower.
var foutFix = []string{
	tcDummies("d0", "d1"),
	"tc qdisc add dev d0 clsact",
	"tc filter add dev d0 egress pref 1 protocol all handle 1 matchall action mirred egress redirect dev d1",
	"tc filter add dev d0 ingress pref 2 protocol ip handle 5 fw police rate 1mbit burst 10k drop",
	"tc filter add dev d0 ingress pref 3 protocol ip handle 1 flower dst_ip 10.0.0.1 action drop",
}

// The expectations of the kernel's filter refusals.
var (
	// tcNoent is a get the kernel answers ENOENT: rtnl_talk's -2.
	tcNoent = expect{RC: intp(2), Errno: "ENOENT", RefusedBy: refKernel}
	// tcDump is a dump the kernel ends with an error: rc 1.
	tcDump = expect{RC: intp(1), Errno: "EMSGSIZE", RefusedBy: refKernel}
)

// tcfilterSpace: `tc filter` and `tc chain` show and get over six
// fixtures.
func tcfilterSpace() *space {
	cmds := []value{
		// bare: lo only, no dev.
		bc(brBoundary, "bare", "filter", nil),         // no verb is show
		bc(brBoundary, "bare", "filter show", nil),    // ifindex 0: an empty dump (F8)
		bc(brCorner, "bare", "filter show help", nil), // usage, then the dump anyway (F7)
		bc(brCorner, "bare", "filter help", nil),
		bc(brBoundary, "bare", "chain show", nil),
		bc(brCorner, "bare", "chain help", nil),
		// fu32.
		bc(brPos, "fu32", "filter show dev d0 ingress", fu32Fix),    // L1, L2
		bc(brPos, "fu32", "-s filter show dev d0 ingress", fu32Fix), // L3
		bc(brPos, "fu32", "-d filter show dev d0 ingress", fu32Fix),
		bc(brCorner, "fu32", "-p filter show dev d0 ingress", fu32Fix), // L4, F4, F13
		bc(brCorner, "fu32", "-o filter show dev d0 ingress", fu32Fix),
		bc(brPos, "fu32", "filter show dev d0", fu32Fix),
		bc(brPos, "fu32", "filter show dev d0 ingress pref 2", fu32Fix),
		bc(brPos, "fu32", "filter show dev d0 ingress protocol ipv6", fu32Fix),
		bc(brPos, "fu32", "filter show dev d0 parent ffff:fff2", fu32Fix),
		bc(brCorner, "fu32", "filter show dev d0 ingress handle 9", fu32Fix), // parsed, never used (Q29h)
		bc(brCorner, "fu32", "filter ls dev d0 ingress", fu32Fix),
		bc(brCorner, "fu32", "filter lst dev d0 ingress", fu32Fix),
		bc(brBoundary, "fu32", "filter show dev d0 ingress pref 70000", fu32Fix), // no 0xffff check (L40)
		bc(brPos, "fu32", "filter get dev d0 ingress protocol ip pref 1 handle 800::1 u32", fu32Fix),
		bc(brPos, "fu32", "filter get dev d0 ingress protocol ip pref 1 handle 800::800 u32", fu32Fix),
		bc(brPos, "fu32", "filter get dev d0 ingress protocol ip pref 3 handle 2: u32", fu32Fix),
		bc(brCorner, "fu32", "filter get dev d0 ingress protocol ip pref 1 handle 800.0:1zz u32", fu32Fix), // F6: any separator, trailing garbage
		// fsimple.
		bc(brPos, "fsimple", "filter show dev d1", fsimpleFix), // route's two "name" keys (F3)
		bc(brPos, "fsimple", "-s filter show dev d1", fsimpleFix),
		bc(brPos, "fsimple", "-d filter show dev d1", fsimpleFix),
		bc(brPos, "fsimple", "filter show dev d1 parent 1: pref 20", fsimpleFix),
		bc(brPos, "fsimple", "filter show dev d0 egress", fsimpleFix), // gact's prob (F19)
		bc(brPos, "fsimple", "-s filter show dev d0 egress", fsimpleFix),
		bc(brCorner, "fsimple", "-o filter show dev d0 egress", fsimpleFix), // matchall's "\n" (L47)
		bc(brCorner, "fsimple", "-nm -cf cls.txt filter show dev d1", append(clonePre(fsimpleFix), tcNamesFile)),
		bc(brPos, "fsimple", "filter get dev d1 parent 1: protocol ip pref 10 handle 5 fw", fsimpleFix),
		bc(brPos, "fsimple", "filter get dev d1 parent 1: protocol ip pref 11 handle 6/0xff fw", fsimpleFix),
		bc(brPos, "fsimple", "filter get dev d1 parent 1: protocol ip pref 20 handle 0x10002 route", fsimpleFix),
		bc(brPos, "fsimple", "filter get dev d0 egress protocol all pref 3 handle 1 matchall", fsimpleFix),
		bc(brCorner, "fsimple", "filter get dev d0 egress protocol all pref 3 handle 1junk matchall", fsimpleFix),        // F5
		bc(brBoundary, "fsimple", "filter get dev d0 egress protocol all pref 3 handle 4294967297 matchall", fsimpleFix), // F5
		// fflow.
		bc(brPos, "fflow", "filter show dev d0", fflowFix),
		bc(brPos, "fflow", "-s filter show dev d0", fflowFix),
		bc(brPos, "fflow", "-d filter show dev d0", fflowFix),
		bc(brPos, "fflow", "filter get dev d0 parent 1: protocol ip pref 30 handle 1 flow", fflowFix),
		// fematch.
		bc(brPos, "fematch", "filter show dev d0 ingress", fematchFix), // fprintf in JSON (F1), %d (F10)
		bc(brPos, "fematch", "-s filter show dev d0 ingress", fematchFix),
		bc(brPos, "fematch", "-d filter show dev d0 ingress", fematchFix),
		bc(brPos, "fematch", "filter show dev d0 egress", fematchFix),
		bc(brPos, "fematch", "filter show dev d0 ingress protocol can", fematchFix),
		bc(brPos, "fematch", "filter get dev d0 ingress protocol all pref 1 handle 1 basic", fematchFix),
		// fchain.
		bc(brPos, "fchain", "filter show dev d0 ingress", fchainFix),
		bc(brPos, "fchain", "-s filter show dev d0 ingress", fchainFix),
		bc(brPos, "fchain", "filter show dev d0 ingress chain 5", fchainFix),
		bc(brBoundary, "fchain", "filter show dev d0 ingress chain 0", fchainFix),
		bc(brBoundary, "fchain", "filter show dev d0 ingress chain 9", fchainFix),
		bc(brPos, "fchain", "filter show block 10", fchainFix),
		bc(brPos, "fchain", "chain show dev d0 ingress", fchainFix),
		bc(brCorner, "fchain", "chain ls dev d0 ingress", fchainFix),
		bc(brCorner, "fchain", "chain show dev d0 ingress chain 5", fchainFix), // fully filtered: "chain " (F22)
		bc(brPos, "fchain", "chain show block 10", fchainFix),
		bc(brPos, "fchain", "chain get dev d0 ingress chain 9", fchainFix),
		bc(brPos, "fchain", "chain get dev d0 ingress chain 5", fchainFix),
		bc(brCorner, "fchain", "chain get dev d0 ingress", fchainFix),                                         // no chain: chain 0
		bc(brCorner, "fchain", "chain get dev d0 ingress chain 9 handle 3 pref 4 protocol ip u32", fchainFix), // F9
		bc(brPos, "fchain", "filter get dev d0 ingress chain 5 protocol ip pref 1 handle 1 basic", fchainFix),
		// fout: tc prints them; gotc refuses each by name.
		bc(brPos, "fout", "filter show dev d0 ingress", foutFix),
		bc(brPos, "fout", "filter show dev d0 egress", foutFix),
		// Batches: one link dump, then each line.
		bb(brPos, "fu32", "two", "", false, batchFile("filter show dev d0 ingress", "chain show dev d0 ingress"), fu32Fix),
		// Q29f: filter_prio is a static, so the second line is still
		// filtered to pref 1.
		bb(brCorner, "fu32", "sticky-pref", "", false,
			batchFile("filter show dev d0 ingress pref 1", "filter show dev d0 ingress"), fu32Fix),
		// Q29f: get sets filter_ifindex, so the show's dev key is dropped.
		bb(brCorner, "fsimple", "get-then-show", "", false,
			batchFile("filter get dev d0 egress protocol all pref 3 handle 1 matchall", "filter show dev d0 egress"), fsimpleFix),
	}
	bad := []badValue{
		{Axis: "opt", Value: v("badopt", "-zz", nil), Expect: tcTool, Why: "an unknown option exits 255 before any socket"},
		brBad("bare", "filter bogus", nil, tcTool, "an unknown filter command returns -1"),
		brBad("bare", "chain bogus", nil, tcTool, "an unknown chain command returns -1"),
		brBad("bare", "filter show bogus", nil, tcTool, "an unknown word: \" What is\" with a leading space (L41)"),
		brBad("bare", "filter show dev lo dev lo", nil, tcTool, "dev given twice: duparg"),
		brBad("bare", "filter show root root", nil, tcTool, "a second root: duplicate parent"),
		brBad("bare", "filter show parent zz", nil, tcTool, "an invalid parent ID"),
		brBad("bare", "filter show chain zz", nil, tcTool, "an invalid chain index"),
		brBad("bare", "filter show pref zz", nil, tcTool, "an invalid priority"),
		brBad("bare", "filter show dev nosuch", nil, tcNodev, "no such device: Cannot find device, rc 1"),
		brBad("bare", "chain show dev nosuch", nil, tcNodev, "no such device: Cannot find device, rc 1"),
		brBad("bare", "filter show block 99", nil, tcAck1, "a block with no qdisc: tc_qdisc_block_exists dumps the qdiscs and finds none"),
		brBad("fsimple", "filter get dev d0 egress pref 3 handle 1 matchall", fsimpleFix, tcTool,
			"get with no protocol"),
		brBad("fsimple", "filter get dev d0 egress protocol all handle 1 matchall", fsimpleFix, tcTool,
			"get with no pref"),
		brBad("fsimple", "filter get dev d0 egress protocol all pref 3 matchall", fsimpleFix, tcAck,
			"get with no handle: refused after the link lookup"),
		brBad("fsimple", "filter get dev d0 egress protocol all pref 3 handle 1", fsimpleFix, tcTool,
			"get with no kind"),
		brBad("fsimple", "filter get dev d0 egress protocol all pref 3 hndle 1 matchall", fsimpleFix, tcAck1,
			"a typo taken as the kind, after the link lookup: Unknown filter \"hndle\" (F23)"),
		brBad("fsimple", "filter get dev d0 egress protocol all pref 3 handle 9 matchall", fsimpleFix,
			tcNoent, "no such handle: the kernel's ENOENT (F15)"),
		// F21: -br asks every kind for a terse dump, which only flower
		// has; the kernel ends the dump with EMSGSIZE after one line.
		brBad("fu32", "-br filter show dev d0 ingress", fu32Fix, tcDump, "a terse dump of u32 (F21)"),
		brBad("fu32", "-br -j filter show dev d0 ingress", fu32Fix, tcDump, "a terse dump of u32 in JSON (F21)"),
		// cls_cgroup_get always returns NULL, so no cgroup filter can be
		// got.
		brBad("fematch", "filter get dev d0 egress protocol all pref 1 handle 1 cgroup", fematchFix, tcNoent,
			"cgroup's get finds nothing"),
		brBad("fu32", "filter get dev d0 ingress protocol ip pref 1 handle zz u32", fu32Fix, tcAck1,
			"an invalid u32 handle: parse_fopt fails after the link lookup, rc 1 (F15)"),
		brBad("fchain", "chain get dev d0 ingress chain 99", fchainFix,
			expect{RC: intp(2), Errno: "EINVAL", RefusedBy: refKernel}, "no such chain"),
		brBad("bare", "filter add dev nosuch ingress matchall action pass", nil, tcNodev,
			"a write, recorded so gotc's refusal is never a pass"),
		// Q29f: chain_index_set is a static, so a second chain is a
		// duparg, and the batch stops.
		bbBad("fchain", "chain-twice", "", false,
			batchFile("filter show dev d0 ingress chain 5", "filter show dev d0 ingress chain 5"), fchainFix, tcAck,
			"the second line's chain is a duparg (Q29f)"),
	}
	sp := tcSpace("tcfilter", cmds, bad)
	sp.Build = func(vals []value) row { return tcBuildMods(tcFilterModules, vals) }
	return sp
}
