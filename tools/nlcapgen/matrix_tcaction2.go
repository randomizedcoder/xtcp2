package main

// The second tc actions space (gotc 13C): `tc actions list/get` of the six
// action kinds increment 12 left (pedit, tunnel_key, ife, ct, gate, bpf),
// and `filter show` of the bpf filter and of a matchall holding all six at
// bind 1, each command plain and -j, in one corpus directory, ws/tcaction2.
//
// tc's side is tc/m_pedit.c, m_tunnel_key.c, m_ife.c, m_ct.c, m_gate.c,
// m_bpf.c and f_bpf.c, with lib/bpf_legacy.c's bpf_print_ops; the frame is
// increment 12's (m_action.c). The kernel's side is each kind's
// tcf_<kind>_dump and cls_bpf_dump.
//
// The rows pin the plan's leads (docs/netlink/upstream/iproute2-tc-action.md):
// pedit's "keys %d\n" under -o (AC39), its htypes and offsets (AC42), val
// against set (AC43), its double space (AC44); tunnel_key's erspan fields
// (AC48), csum's key (AC49); ife's 0X (AC51), its meta keys (AC53); ct's
// helper family (AC56), a bare nat (AC57); gate's missing kind (AC59), its
// newlines under -o (AC61); bpf's stray space (AC65) and the bpf filter's
// handle and flowid (AC66).
//
// Only cBPF (bytecode): an eBPF program's id, name and tag come from bpf(2),
// which gotc refuses, so no row loads an object file.
//
// Determinism: as increment 12's: explicit indexes, fixed MACs, no packets,
// a gate base-time in the past; the tcf_t timers are the one allow-listed
// difference. Writes appear only as negatives.

// tcAction2Modules are the qdisc, classifier and action modules the
// fixtures use. act_ct pulls in nf_conntrack and nf_nat; act_ife loads its
// act_meta_* modules, and act_ct the ftp helper, on demand.
var tcAction2Modules = []string{
	"sch_ingress", "cls_matchall", "cls_bpf", "act_gact", "act_police", "act_pedit", "act_tunnel_key",
	"act_ife", "act_ct", "act_gate", "act_bpf",
}

// acpedFix: pedit legacy raw; extended ip ttl add; extended eth, ip6, tcp
// and udp keys (each htype); a two-key raw with retain, drop; a legacy
// layered ip tos, pipe.
var acpedFix = []string{
	tcDummies("d0"),
	"tc actions add action pedit munge offset 0 u32 set 0x1 index 1",
	"tc actions add action pedit ex munge ip ttl add 1 index 2",
	"tc actions add action pedit ex munge eth dst set 02:00:00:00:00:09 munge ip6 dst set 2001:db8::1" +
		" munge tcp dport set 80 munge udp sport set 53 index 3",
	"tc actions add action pedit munge offset 12 u16 set 0x0800 retain 0xff00 munge offset 16 u32 set 0x0a000001 drop index 4",
	"tc actions add action pedit munge ip tos set 0x10 pipe index 5",
}

// actkFix: tunnel_key unset; set v4 with every scalar and nocsum/nofrag;
// v6 with two geneve options; vxlan gbp; erspan v1 and v2 (AC48).
var actkFix = []string{
	tcDummies("d0"),
	"tc actions add action tunnel_key unset index 1",
	"tc actions add action tunnel_key set src_ip 10.0.0.1 dst_ip 10.0.0.2 id 7 dst_port 4789 tos 0x10 ttl 64 nocsum nofrag index 2",
	"tc actions add action tunnel_key set src_ip 2001:db8::1 dst_ip 2001:db8::2 id 8 dst_port 6081" +
		" geneve_opts 0102:80:00880022,0103:81:00a000b0 index 3",
	"tc actions add action tunnel_key set src_ip 10.0.0.1 dst_ip 10.0.0.2 id 9 vxlan_opts 4660 index 4",
	"tc actions add action tunnel_key set src_ip 10.0.0.1 dst_ip 10.0.0.2 id 10 erspan_opts 1:12:0:0 index 5",
	"tc actions add action tunnel_key set src_ip 10.0.0.1 dst_ip 10.0.0.2 id 11 erspan_opts 2:0:1:7 index 6",
}

// acifeFix: ife encode with type, MACs and a used mark; allowed mark and
// prio with a used tcindex; decode; encode with a dst only (AC52).
var acifeFix = []string{
	tcDummies("d0"),
	"tc actions add action ife encode type 0xED3E dst 02:00:00:00:00:01 src 02:00:00:00:00:02 use mark 7 index 1",
	"tc actions add action ife encode allow mark allow prio use tcindex 3 index 2",
	"tc actions add action ife decode index 3",
	"tc actions add action ife encode dst 02:00:00:00:00:01 allow mark index 4",
}

// acctFix: ct restore; commit with zone, masked mark and label; commit
// force; clear; src nat range with ports; v6 dst nat; a bare nat (AC57);
// the ftp helper (AC56); a short label.
var acctFix = []string{
	tcDummies("d0"),
	"tc actions add action ct index 1",
	"tc actions add action ct commit zone 404 mark 0x42/0xf0" +
		" label 12312300000000000000000000000001/ffffffff000000000000000000000001 index 2",
	"tc actions add action ct commit force index 3",
	"tc actions add action ct clear index 4",
	"tc actions add action ct commit nat src addr 10.0.0.1-10.0.0.9 port 1000-2000 index 5",
	"tc actions add action ct commit nat dst addr 2001:db8::1 index 6",
	"tc actions add action ct nat index 7",
	"tc actions add action ct commit helper ipv4-tcp-ftp index 8",
	"tc actions add action ct commit label 0102/0fff index 9",
}

// acgateFix: gate with every scalar and a three-entry schedule (an open
// entry with ipv and max-octets, a wildcard close, an open with ipv
// only); then a single wildcard entry with no priority.
var acgateFix = []string{
	tcDummies("d0"),
	"tc actions add action gate priority 2 clockid CLOCK_TAI base-time 200000000000ns cycle-time 300000000ns" +
		" cycle-time-ext 1000ns sched-entry open 100000000ns 3 8000 sched-entry close 100000000ns -1 -1" +
		" sched-entry open 100000000ns 1 -1 index 1",
	"tc actions add action gate sched-entry close 100000000ns index 2",
}

// acbpfFix: bpf actions of four and one instructions; bpf filters with a
// classid, with da and skip_hw, with an action and with a police; then a
// matchall whose list holds all six kinds (bind 1).
var acbpfFix = []string{
	tcDummies("d0"),
	"tc actions add action bpf bytecode '4,40 0 0 12,21 0 1 2048,6 0 0 262144,6 0 0 0' index 1",
	"tc actions add action bpf bytecode '1,6 0 0 4294967295' drop index 2",
	"tc qdisc add dev d0 clsact",
	"tc filter add dev d0 ingress pref 1 protocol ip handle 1 bpf bytecode '4,40 0 0 12,21 0 1 2048,6 0 0 262144,6 0 0 0' classid 1:1",
	"tc filter add dev d0 ingress pref 2 protocol all handle 2 bpf bytecode '1,6 0 0 0' da skip_hw",
	"tc filter add dev d0 ingress pref 3 protocol all handle 3 bpf bytecode '1,6 0 0 4294967295' action gact pass",
	"tc filter add dev d0 ingress pref 4 protocol all handle 4 bpf bytecode '1,6 0 0 4294967295' police rate 1mbit burst 10k drop",
	"tc filter add dev d0 ingress pref 5 protocol all handle 5 matchall action pedit ex munge ip ttl add 1" +
		" action ct commit zone 3 action gate sched-entry open 1ms action tunnel_key unset action ife decode" +
		" action bpf bytecode '1,6 0 0 0'",
}

// tcaction2Space: `tc actions list/get` of the six kinds over six
// fixtures, and `filter show` of bpf filters and of a matchall holding
// all six.
func tcaction2Space() *space {
	cmds := []value{
		bc(brBoundary, "bare", "actions ls action pedit", nil), // total acts 0
		// acped.
		bc(brPos, "acped", "actions ls action pedit", acpedFix), // AC42, AC44
		bc(brPos, "acped", "-s actions ls action pedit", acpedFix),
		bc(brCorner, "acped", "-o actions ls action pedit", acpedFix), // AC39
		bc(brCorner, "acped", "-br actions ls action pedit", acpedFix),
		bc(brPos, "acped", "actions get action pedit index 1", acpedFix),
		bc(brPos, "acped", "actions get action pedit index 3", acpedFix), // AC43
		bc(brPos, "acped", "actions get action pedit index 4", acpedFix),
		bc(brCorner, "acped", "actions get action pedit index 5", acpedFix),
		// actk.
		bc(brPos, "actk", "actions ls action tunnel_key", actkFix),
		bc(brPos, "actk", "-s actions ls action tunnel_key", actkFix),
		bc(brCorner, "actk", "-o actions ls action tunnel_key", actkFix),
		bc(brPos, "actk", "actions get action tunnel_key index 2", actkFix), // AC49
		bc(brPos, "actk", "actions get action tunnel_key index 3", actkFix),
		bc(brPos, "actk", "actions get action tunnel_key index 4", actkFix),
		bc(brCorner, "actk", "actions get action tunnel_key index 5", actkFix), // AC48
		bc(brCorner, "actk", "actions get action tunnel_key index 6", actkFix), // AC48
		// acife.
		bc(brPos, "acife", "actions ls action ife", acifeFix), // AC51, AC53
		bc(brPos, "acife", "-s actions ls action ife", acifeFix),
		bc(brCorner, "acife", "-o actions ls action ife", acifeFix),
		bc(brPos, "acife", "actions get action ife index 1", acifeFix),
		bc(brPos, "acife", "actions get action ife index 3", acifeFix),
		bc(brCorner, "acife", "actions get action ife index 4", acifeFix), // AC52
		// acct.
		bc(brPos, "acct", "actions ls action ct", acctFix),
		bc(brPos, "acct", "-s actions ls action ct", acctFix),
		bc(brCorner, "acct", "-br actions ls action ct", acctFix),
		bc(brPos, "acct", "actions get action ct index 2", acctFix),
		bc(brPos, "acct", "actions get action ct index 5", acctFix),
		bc(brPos, "acct", "actions get action ct index 6", acctFix),
		bc(brCorner, "acct", "actions get action ct index 7", acctFix), // AC57
		bc(brCorner, "acct", "actions get action ct index 8", acctFix), // AC56
		bc(brBoundary, "acct", "actions get action ct index 9", acctFix),
		// acgate.
		bc(brPos, "acgate", "actions ls action gate", acgateFix), // AC59
		bc(brPos, "acgate", "-s actions ls action gate", acgateFix),
		bc(brPos, "acgate", "actions get action gate index 1", acgateFix),
		bc(brBoundary, "acgate", "actions get action gate index 2", acgateFix),  // wildcards
		bc(brCorner, "acgate", "-o actions get action gate index 1", acgateFix), // AC61
		bc(brCorner, "acgate", "-d actions get action gate index 1", acgateFix),
		bc(brCorner, "acgate", "-p actions get action gate index 1", acgateFix),
		// acbpf.
		bc(brPos, "acbpf", "actions ls action bpf", acbpfFix), // AC65
		bc(brPos, "acbpf", "-s actions ls action bpf", acbpfFix),
		bc(brCorner, "acbpf", "-br actions ls action bpf", acbpfFix),
		bc(brPos, "acbpf", "actions get action bpf index 2", acbpfFix),
		bc(brPos, "acbpf", "filter show dev d0 ingress", acbpfFix), // AC66, bind 1
		bc(brPos, "acbpf", "-s filter show dev d0 ingress", acbpfFix),
		bc(brCorner, "acbpf", "-o filter show dev d0 ingress", acbpfFix),
		bc(brCorner, "acbpf", "-d filter show dev d0 ingress", acbpfFix),
		// Batches: each line in turn.
		bb(brPos, "acbpf", "ls-then-filter", "", false, batchFile(
			"actions ls action pedit",
			"actions ls action gate",
			"filter show dev d0 ingress"), acbpfFix),
		bb(brCorner, "acct", "get-then-ls", "", false, batchFile(
			"actions get action ct index 8",
			"actions ls action ct"), acctFix),
	}
	noent := expect{RC: intp(255), Errno: "ENOENT", RefusedBy: refKernel}
	bad := []badValue{
		brBad("acped", "actions get action pedit index 999", acpedFix, noent, "no such pedit index"),
		brBad("actk", "actions get action tunnel_key index 999", actkFix, noent, "no such tunnel_key index"),
		brBad("acife", "actions get action ife index 999", acifeFix, noent, "no such ife index"),
		brBad("acct", "actions get action ct index 999", acctFix, noent, "no such ct index"),
		brBad("acgate", "actions get action gate index 999", acgateFix, noent, "no such gate index"),
		brBad("acbpf", "actions get action bpf index 999", acbpfFix, noent, "no such bpf index"),
		brBad("bare", "actions delete action pedit index 9", nil, noent,
			"a write, recorded so gotc's refusal is never a pass"),
	}
	sp := tcSpace("tcaction2", cmds, bad)
	sp.Build = func(vals []value) row { return tcBuildMods(tcAction2Modules, vals) }
	return sp
}
