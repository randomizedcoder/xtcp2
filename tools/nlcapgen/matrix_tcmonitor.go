package main

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// The tc monitor space (gotc 14C): `tc monitor` live, under the
// xtcp2-nl-tcmon helper (nix/microvms/netlink-capture.nix), which starts it
// in the background, waits for its RTNLGRP_TC subscription, runs a trigger
// and stops it with SIGTERM once its output matches; and `tc monitor file`
// over records nlcapgen writes, in one corpus directory, ws/tcmonitor.
//
// tc's side is tc/tc_monitor.c (accept_tcmsg, do_tcmonitor), lib/libnetlink.c
// (rtnl_listen, rtnl_from_file), lib/utils.c (print_timestamp), and the
// printers print_qdisc, print_class, print_filter and print_action, whose
// event branches print deleted, added, replaced, "Added action" and the like.
// The kernel's side is each object's notify: qdisc_notify, tclass_notify,
// tfilter_notify, tc_chain_notify and tcf_add_notify/tcf_del_notify.
//
// The rows pin the plan's leads (docs/netlink/upstream/iproute2-tc-monitor.md):
// help's usage (MO1), the stamp ahead of a non-event (MO2), %08d (MO3), -j
// ignored (MO4), the 16 KiB receive (MO5), qdisc added/replaced never
// printed (MO6, K19, K20), the three replaced rules (MO7), the action
// events' "total acts 0" (MO9), the stale link cache (MO10), a batch's
// filter statics (MO11), file mode's gets (MO12), the file errors (MO13)
// and the argument refusals (MO14).
//
// Determinism: explicit handles and indexes, fixed MACs, no packets; a
// live row's events are waited for by ERE before SIGTERM. The -t stamps are
// the wall clock, masked by the corpus tests. Writes happen only in
// triggers and pre steps.

// tcMonitorModules are the qdiscs, classifiers and actions the triggers use.
var tcMonitorModules = []string{
	"sch_htb", "sch_ingress", "sch_fq_codel", "cls_basic", "cls_matchall", "cls_bpf", "cls_fw",
	"act_gact", "act_mirred",
}

// tcMonFixD0 is one dummy, d0.
var tcMonFixD0 = []string{tcDummies("d0")}

// tcMonFixHtb is d0 with an htb root, for the class triggers.
var tcMonFixHtb = []string{tcDummies("d0"), "tc qdisc add dev d0 root handle 1: htb"}

// tcMonFixClsact is d0 with clsact, for the filter triggers.
var tcMonFixClsact = []string{tcDummies("d0"), "tc qdisc add dev d0 clsact"}

// tcMonFixFile is d0 and d1, ifindex 2 and 3 in the row's fresh namespace,
// which the file records name.
var tcMonFixFile = []string{tcDummies("d0", "d1")}

// Bodies of real messages, from the 7.3.1 corpus, for the file records:
// an htb qdisc (tcqdisc/ec-035), an htb class (tcclass/ec-013), an fw
// filter with a classid (tcfilter/ec-024) and a gact action's tab
// (tcaction/ec-057). Each tcmsg's ifindex is rewritten to the row's device.
const (
	monQdiscHtbHex = "000000000300000000000100ffffffff0200000008000100687462002400020018000200110003000500000010000000000000000000000008000500e803000005000c00000000003000070014000100000000000000000000000000000000001800030000000000000000000000000000000000000000002c00030000000000000000000000000000000000000000000000000000000000000000000000000000000000"
	monClassHtbHex = "000000000300000010000100010001000000000008000100687462003400020030000100000100000000000048e80100000100000000000090d00300004c1d00a0860100005ed0b2000000000200000048000700140001000000000000000000000000000000000018000300000000000000000000000000000000000000000018000400000000000000000000000000004c1d00a08601002c0003000000000000000000000000000000000000000000000000000000000000000000000000000000000018000400000000000000000000000000004c1d00a0860100"
	monFilterFwHex = "0000000004000000050000000000010008000a00070001006677000008000b00000000000c0002000800010001000100"
	monActGactHex  = "00000000a00001009c000100090001006761637400000000440004001400010000000000000000000000000000000000140007000000000000000000000000000000000018000300000000000000000000000000000000000000000008000a000000000040000200180002002100000000000000020000000100000000000000240001001000000000000000100000000000000000000000000000000000000000000000"
)

// monBody is a hex body with its tcmsg ifindex set; ifindex 0 leaves it.
func monBody(hexBody string, ifindex uint32) []byte {
	b, err := hex.DecodeString(hexBody)
	if err != nil {
		panic(err)
	}
	if ifindex != 0 {
		binary.LittleEndian.PutUint32(b[4:8], ifindex)
	}
	return b
}

// monRec is one rtnl_from_file record: a header whose length is lenField
// (0: the true length), then the body padded to 4 bytes.
func monRec(typ, flags uint16, body []byte, lenField uint32) []byte {
	l := uint32(16 + len(body))
	if lenField != 0 {
		l = lenField
	}
	b := binary.LittleEndian.AppendUint32(nil, l)
	b = binary.LittleEndian.AppendUint16(b, typ)
	b = binary.LittleEndian.AppendUint16(b, flags)
	b = binary.LittleEndian.AppendUint32(b, 0)
	b = binary.LittleEndian.AppendUint32(b, 0)
	b = append(b, body...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

// monEvents is a file of one event of each object, and the qdisc deleted:
// htb on d0, its class, an fw filter on d1, a gact added, htb deleted.
func monEvents() []byte {
	b := make([]byte, 0, 1600)
	b = append(b, monRec(unix.RTM_NEWQDISC, 0, monBody(monQdiscHtbHex, 2), 0)...)
	b = append(b, monRec(unix.RTM_NEWTCLASS, 0, monBody(monClassHtbHex, 2), 0)...)
	b = append(b, monRec(unix.RTM_NEWTFILTER, unix.NLM_F_CREATE|unix.NLM_F_EXCL, monBody(monFilterFwHex, 3), 0)...)
	b = append(b, monRec(unix.RTM_NEWACTION, unix.NLM_F_CREATE|unix.NLM_F_EXCL, monBody(monActGactHex, 0), 0)...)
	b = append(b, monRec(unix.RTM_NEWQDISC, 0, monBody(monQdiscHtbHex, 3), 0)...)
	b = append(b, monRec(unix.RTM_DELQDISC, 0, monBody(monQdiscHtbHex, 2), 0)...)
	return b
}

// monNonEvents is a DONE, an ERROR and a NOOP, which accept_tcmsg stamps
// but does not print (MO2), then a NEWLINK, which it calls unknown (MO3).
func monNonEvents() []byte {
	b := make([]byte, 0, 128)
	b = append(b, monRec(unix.NLMSG_DONE, unix.NLM_F_MULTI, make([]byte, 4), 0)...)
	b = append(b, monRec(unix.NLMSG_ERROR, 0, make([]byte, 20), 0)...)
	b = append(b, monRec(unix.NLMSG_NOOP, 0, nil, 0)...)
	b = append(b, monRec(unix.RTM_NEWLINK, 0, make([]byte, 16), 0)...)
	return b
}

// monFile is a pre step writing data to m.bin.
func monFile(data []byte) string {
	return "echo " + base64.StdEncoding.EncodeToString(data) + " | base64 -d > m.bin"
}

// monBigOps is a pre step writing a cBPF program of n "ret 0" instructions
// to ops.txt, for bytecode-file.
func monBigOps(n int) string {
	return "{ printf '" + strconv.Itoa(n) + "'; i=0; while [ $i -lt " + strconv.Itoa(n) +
		" ]; do printf ',6 0 0 0'; i=$((i+1)); done; echo; } > ops.txt"
}

// monLive is a live row: tc's args, the fixture, the trigger and the EREs
// the monitor's output must match before SIGTERM.
func monLive(kind brKind, name, args string, pre []string, trigger string, until ...string) value {
	val := bc(kind, name, args, pre)
	val.Name = name + ":live:" + strings.ReplaceAll(args, " ", "_")
	val.Facts["mode"] = "live"
	val.Facts["trigger"] = trigger
	val.Facts["until"] = strings.Join(until, "\n")
	return val
}

// monOffline is a `tc ...` row: a file or argument row.
func monOffline(kind brKind, name, args string, pre []string) value {
	val := bc(kind, name, args, pre)
	val.Facts["mode"] = "file"
	return val
}

// tcMonBuild makes a tcmonitor row: a live value runs tc under the helper,
// any other as itself.
func tcMonBuild(vals []value) row {
	val := vals[0]
	r := row{
		Proto:   "route",
		Modules: tcMonitorModules,
		Expect:  expect{RC: intp(0), RefusedBy: refNone},
		Axes:    mergeFacts(vals),
	}
	for _, p := range val.Pre {
		r.Pre = append(r.Pre, preStep{Cmd: p})
	}
	if val.Facts["mode"] != "live" {
		r.Cmd = "tc " + val.Arg
		return r
	}
	words := []string{"xtcp2-nl-tcmon", shq(val.Arg), shq(val.Facts["trigger"])}
	if u := val.Facts["until"]; u != "" {
		for _, re := range strings.Split(u, "\n") {
			words = append(words, shq(re))
		}
	}
	r.Cmd = strings.Join(words, " ")
	return r
}

// tcmonitorSpace: `tc monitor` live over four fixtures and `tc monitor
// file` over generated records.
func tcmonitorSpace() *space {
	const (
		qdiscLife = "tc qdisc add dev d0 root handle 1: fq_codel && tc qdisc change dev d0 root handle 1: fq_codel limit 500" +
			" && tc qdisc replace dev d0 root handle 2: htb && tc qdisc del dev d0 root"
		classLife = "tc class add dev d0 parent 1: classid 1:10 htb rate 1mbit" +
			" && tc class change dev d0 parent 1: classid 1:10 htb rate 2mbit && tc class del dev d0 classid 1:10"
		filterLife = "tc filter add dev d0 ingress pref 1 handle 1 protocol all basic action gact pass" +
			" && tc filter replace dev d0 ingress pref 1 handle 1 protocol all basic action gact drop" +
			" && tc filter change dev d0 ingress pref 1 handle 1 protocol all basic action gact pipe" +
			" && tc filter del dev d0 ingress pref 1 handle 1 protocol all basic" +
			" && tc chain add dev d0 ingress chain 3 && tc chain del dev d0 ingress chain 3"
		filterActs = "tc filter add dev d0 ingress pref 2 handle 2 protocol all matchall action gact pass" +
			" action mirred egress redirect dev d0 && tc filter del dev d0 ingress pref 2"
		actLife = "tc actions add action gact pass index 7 && tc actions replace action gact drop index 7" +
			" && tc actions del action gact index 7 && tc actions add action gact pass index 8" +
			" && tc actions flush action gact"
		qdiscAdd  = "tc qdisc add dev d0 root handle 1: htb"
		qdiscFilt = "tc qdisc add dev d0 root handle 1: htb && tc filter add dev d0 parent 1: pref 1 handle 1 protocol all basic classid 1:1"
		rename    = "tc qdisc add dev d0 root handle 1: htb && ip link set d0 down && ip link set d0 name d1" +
			" && tc qdisc del dev d1 root"
		bigOps = "tc filter add dev d0 ingress pref 1 handle 1 protocol all bpf bytecode-file ops.txt"
	)
	// tc's request for an n-op bpf filter is 60 + 8n bytes and the
	// kernel's event 76 + 8n: at 2040 the request still fits tc's 16384-byte
	// send buffer and the event no longer fits its 16384-byte receive
	// (MO5); at 2038 the event is 16380 and fits.
	big := append(clonePre(tcMonFixClsact), monBigOps(2040))
	under := append(clonePre(tcMonFixClsact), monBigOps(2038))
	events := append(clonePre(tcMonFixFile), monFile(monEvents()))
	nonEvents := append(clonePre(tcMonFixFile), monFile(monNonEvents()))
	cmds := make([]value, 0, 20)
	cmds = append(cmds,
		monLive(brPos, "qdisc", "monitor", tcMonFixD0, qdiscLife, "deleted qdisc htb 2:"),     // MO6, K19, K20
		monLive(brPos, "class", "monitor", tcMonFixHtb, classLife, "deleted class htb 1:10"),  // MO8
		monLive(brPos, "filter", "monitor", tcMonFixClsact, filterLife, "deleted chain"),      // MO7, K22
		monLive(brPos, "filteracts", "monitor", tcMonFixClsact, filterActs, "deleted filter"), // bind 1
		monLive(brPos, "actions", "monitor", tcMonFixD0, actLife, "Flushed table"),            // MO9
		monLive(brPos, "stats", "-s monitor", tcMonFixD0, qdiscFilt, "added filter"),          // -s
		monLive(brPos, "details", "-d monitor", tcMonFixD0, qdiscFilt, "added filter"),        // -d
		monLive(brCorner, "stamp", "-t monitor", tcMonFixD0, qdiscAdd, "qdisc htb 1:"),
		monLive(brCorner, "stampshort", "-tshort monitor", tcMonFixD0, qdiscAdd, "qdisc htb 1:"),
		monLive(brCorner, "json", "-j monitor", tcMonFixD0, qdiscAdd, "qdisc htb 1:"),   // MO4
		monLive(brCorner, "rename", "monitor", tcMonFixD0, rename, "deleted qdisc htb"), // MO10
		monLive(brBoundary, "under", "monitor", under, bigOps, "bytecode"),              // MO5's edge
		// Offline: file mode and the batch.
		monOffline(brPos, "file", "monitor file m.bin", events), // MO12
		monOffline(brCorner, "file", "-t monitor file m.bin", events),
		monOffline(brCorner, "file", "-tshort monitor file m.bin", events),
		monOffline(brCorner, "file", "-j monitor file m.bin", events), // MO4
		monOffline(brCorner, "file", "-s -d monitor file m.bin", events),
		monOffline(brCorner, "nonevents", "-t monitor file m.bin", nonEvents), // MO2, MO3
		monOffline(brBoundary, "empty", "monitor file m.bin", append(clonePre(tcMonFixFile), ": > m.bin")),
	)
	batch := bb(brCorner, "file", "show-then-file", "", false, batchFile(
		"qdisc show dev d0",
		"monitor file m.bin"), events) // MO11
	batch.Facts["mode"] = "file"
	cmds = append(cmds, batch)

	tool := expect{RC: intp(255), RefusedBy: refIproute2}
	bigVal := monLive(brPos, "big", "monitor", big, bigOps)
	short := monRec(unix.RTM_NEWQDISC, 0, monBody(monQdiscHtbHex, 2), 0)
	bad := []badValue{
		{Axis: "cmd", Value: bigVal, Expect: expect{RC: intp(2), RefusedBy: refAckThenFail},
			Why: "a 2040-op filter's event is past rtnl_listen's 16 KiB though its request fit tc's: Truncated message, exit 2 (MO5)"},
		monBad("bare", "monitor help", nil, tool, "help prints the usage and exits 255 (MO1)"),
		monBad("bare", "monitor bogus", nil, tool, "an unknown word (MO14)"),
		monBad("bare", "monitor file", nil, tool, "file with no value (MO14)"),
		monBad("bare", "monitor file /nonexistent", nil, tool, "Cannot fopen (MO13)"),
		monBad("trunc", "monitor file m.bin", append(clonePre(tcMonFixFile), monFile(short[:len(short)-8])), tool,
			"a record cut inside its body (MO13)"),
		monBad("malformed", "monitor file m.bin",
			append(clonePre(tcMonFixFile), monFile(monRec(unix.RTM_NEWQDISC, 0, nil, 8))), tool,
			"a record whose length is under the header's (MO13)"),
	}
	return &space{
		Name:    "tcmonitor",
		Axes:    []axis{{Name: "cmd", Values: cmds}},
		Bad:     bad,
		Build:   tcMonBuild,
		NegBase: func([]value) bool { return true },
	}
}

// monBad is a refused `tc ...` row.
func monBad(fix, words string, pre []string, exp expect, why string) badValue {
	b := brBad(fix, words, pre, exp, why)
	b.Value.Facts["mode"] = "file"
	return b
}
