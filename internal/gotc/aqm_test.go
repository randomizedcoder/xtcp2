package gotc

import (
	"bytes"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The increment-16 kinds (red, choke, gred, sfb, codel, pie, fq_pie, hhf,
// cake, cake_mq, dualpi2, plug, skbprio) against scripted replies: one row
// per lead the tcaqm corpus pins, so a mutant that breaks one fails here by
// name, and the crafted paths the kernel never sends (a VQ_DP past
// MAX_DPs, a zero-length VQ entry, a tin missing an attribute).
//
// go test ./internal/gotc/ -run TestGotcAqm

// redQopt is a struct tc_red_qopt.
func redQopt(limit, lo, hi uint32, wlog, plog, scell, flags uint8) []byte {
	return cat(le32(limit), le32(lo), le32(hi), []byte{wlog, plog, scell, flags})
}

// aqmQ is a qdisc 1: of kind k with a nested TCA_OPTIONS.
func aqmQ(k string, opts ...[]byte) Msg { return schedQ(k, nestOpts(opts...)) }

// aqmQs is aqmQ with zero basic stats and xs as TCA_STATS_APP.
func aqmQs(k string, xs []byte, opts ...[]byte) Msg {
	return schedQ(k, nestOpts(opts...), stats2(basic(0, 0), attr(xtcpnl.TcaStatsApp, xs)))
}

// gredParms is TCA_GRED_PARMS: sixteen struct tc_gred_qopt, slot i's DP
// dps[i] (MAX_DPs+i, the kernel's empty marker, past len(dps)).
func gredParms(dps ...uint32) []byte {
	b := make([]byte, 0, xtcpnl.GredMaxDPsCst*xtcpnl.TcGredQoptSizeCst)
	for i := range xtcpnl.GredMaxDPsCst {
		dp := uint32(xtcpnl.GredMaxDPsCst + i)
		if i < len(dps) {
			dp = dps[i]
		}
		q := make([]byte, xtcpnl.TcGredQoptSizeCst)
		copy(q[0:], le32(60000))
		copy(q[4:], le32(15000))
		copy(q[8:], le32(45000))
		copy(q[12:], le32(dp))
		q[43] = 1              // prio
		copy(q[44:], le32(7))  // packets
		copy(q[48:], le32(99)) // bytesin
		b = append(b, q...)
	}
	return attr(xtcpnl.TcaGredParms, b)
}

// gredDps is TCA_GRED_DPS: a struct tc_gred_sopt.
func gredDps(dps, def uint32, grio uint8) []byte {
	return attr(xtcpnl.TcaGredDps, cat(le32(dps), le32(def), []byte{grio, 0, 0, 0}))
}

// gredVqList is TCA_GRED_VQ_LIST of entries, each a TCA_GRED_VQ_ENTRY
// of attrs, without NLA_F_NESTED, as the kernel's nla_nest_start_noflag
// sends it and tc's plain parse_rtattr needs it.
func gredVqList(entries ...[]byte) []byte {
	b := make([]byte, 0, len(entries)*8)
	for _, e := range entries {
		b = append(b, attr(xtcpnl.TcaGredVqEntry, e)...)
	}
	return attr(xtcpnl.TcaGredVqList|unix.NLA_F_NESTED, b)
}

// cakeTinAttrs is one tin's stats: all twenty printed attributes, zero
// but threshold_rate and the times.
func cakeTinAttrs(skip uint16) []byte {
	var b []byte
	for _, f := range cakeJSONTins {
		if f.t == skip {
			continue
		}
		switch f.t {
		case xtcpnl.TcaCakeTinStatsThresholdRate64, xtcpnl.TcaCakeTinStatsSentBytes64:
			b = append(b, attr(f.t, le64(0))...)
		case xtcpnl.TcaCakeTinStatsTargetUs:
			b = append(b, attr(f.t, le32(5000))...)
		default:
			b = append(b, attr(f.t, le32(0))...)
		}
	}
	return b
}

// cakeXstats is a cake TCA_STATS_APP: stats then n tins, tin `gap`
// missing TARGET_US.
func cakeXstats(n, gap int, stats ...[]byte) []byte {
	var tins []byte
	for i := 1; i <= n; i++ {
		skip := uint16(0)
		if i-1 == gap {
			skip = xtcpnl.TcaCakeTinStatsTargetUs
		}
		tins = append(tins, attr(uint16(i)|unix.NLA_F_NESTED, cakeTinAttrs(skip))...)
	}
	return cat(cat(stats...), attr(xtcpnl.TcaCakeStatsTinStats|unix.NLA_F_NESTED, tins))
}

// cakeShortTins is two cake tins, tin 1's TARGET_US one byte long; tin 0
// has a full TARGET_US when has0.
func cakeShortTins(has0 bool) []byte {
	var t0 []byte
	if has0 {
		t0 = attr(xtcpnl.TcaCakeTinStatsTargetUs, le32(5000))
	}
	tins := cat(attr(1|unix.NLA_F_NESTED, t0),
		attr(2|unix.NLA_F_NESTED, attr(xtcpnl.TcaCakeTinStatsTargetUs, []byte{1})))
	return attr(xtcpnl.TcaCakeStatsTinStats|unix.NLA_F_NESTED, tins)
}

const aqmHead = "qdisc %s 1: root refcnt 2 "

func aqmLine(k, rest string) string { return strings.Replace(aqmHead, "%s", k, 1) + rest }

// aqmSent is tc's -s line for zero basic stats with no queue stats.
const aqmSent = "\n Sent 0 bytes 0 pkt\n"

func TestGotcAqm(t *testing.T) {
	show := []string{"qdisc", "show", "dev", "d0"}
	showS := []string{"-s", "qdisc", "show", "dev", "d0"}
	showD := []string{"-d", "qdisc", "show", "dev", "d0"}
	showJ := []string{"-j", "qdisc", "show", "dev", "d0"}
	showSO := []string{"-s", "-o", "qdisc", "show", "dev", "d0"}
	redParms := attr(xtcpnl.TcaRedParms, redQopt(400000, 30000, 90000, 5, 21, 15, 0))
	gred2 := []byte(nil)
	gred2 = cat(gredParms(0, 1), gredDps(4, 0, 1))
	cakeBase := []byte(nil)
	cakeBase = cat(attr(xtcpnl.TcaCakeBaseRate64, le64(0)), attr(xtcpnl.TcaCakeDiffservMode, le32(0)),
		attr(xtcpnl.TcaCakeFlowMode, le32(4)), attr(xtcpnl.TcaCakeRaw, nil), attr(xtcpnl.TcaCakeRtt, le32(0)))
	tests := []struct {
		description string
		args        []string
		replies     []Reply
		expected    result
	}{
		// Positive.
		{"positive: red -d with a qevent: Scell_log has no trailing space (Q66)", showD,
			showD0(aqmQ("red", redParms, attr(xtcpnl.TcaRedEarlyDropBlock, le32(10)))),
			result{stdout: aqmLine("red", "limit 400000b min 30000b max 90000b ewma 5 Plog 21 Scell_log 15qevent early_drop block 10 \n")}},
		{"positive: red flags in text, only the true ones (Q87)", show,
			showD0(aqmQ("red", redParms, attr(xtcpnl.TcaRedFlags, cat(le32(0x5), le32(0xff))))),
			result{stdout: aqmLine("red", "limit 400000b min 30000b max 90000b ecn adaptive \n")}},
		{"positive: red flags in JSON, all four bools (Q87)", showJ,
			showD0(aqmQ("red", redParms)),
			result{stdout: `[{"kind":"red","handle":"1:","root":true,"refcnt":2,"options":{"limit":400000,"min":30000,"max":90000,"ecn":false,"harddrop":false,"adaptive":false,"nodrop":false}}]` + "\n"}},
		{"positive: red -d with MAX_P prints the probability", showD,
			showD0(aqmQ("red", redParms, attr(xtcpnl.TcaRedMaxP, le32(85899346)))),
			result{stdout: aqmLine("red", "limit 400000b min 30000b max 90000b ewma 5 probability 0.02 Scell_log 15\n")}},
		{"positive: choke -s, counts in packets", showS,
			showD0(aqmQs("choke", cat(le32(1), le32(2), le32(3), le32(4), le32(5)),
				attr(xtcpnl.TcaChokeParms, redQopt(1000, 100, 300, 7, 17, 0, 1)))),
			result{stdout: aqmLine("choke", "limit 1000p min 100p max 300p ecn ") + aqmSent + "  marked 4 early 1 pdrop 2 other 3 matched 5\n"}},
		{"positive: gred with two DPs and -s, empty slots skipped (K33)", showS,
			showD0(aqmQ("gred", gred2, gredVqList(cat(attr(xtcpnl.TcaGredVqDp, le32(1)),
				attr(xtcpnl.TcaGredVqStatBytes, le64(1<<32+5)), attr(xtcpnl.TcaGredVqFlags, le32(1)))))),
			result{stdout: aqmLine("gred", "vqs 4 default 0 grio \n vq 0 prio 1 limit 60000b min 15000b max 45000b "+
				"\n  Queue size: average 0b current 0b \n  Dropped packets: forced 0 early 0 pdrop 0 other 0 "+
				"\n  Marked packets: forced 0 early 0 \n  Total packets: 7 (0b) "+
				"\n vq 1 prio 1 limit 60000b min 15000b max 45000b ecn "+
				"\n  Queue size: average 0b current 0b \n  Dropped packets: forced 0 early 0 pdrop 0 other 0 "+
				"\n  Marked packets: forced 0 early 0 \n  Total packets: 7 (5b) \n")}},
		{"positive: gred -s with no VQ list prints the qopt's counters", showS,
			showD0(aqmQ("gred", cat(gredParms(3), gredDps(4, 3, 0)))),
			result{stdout: aqmLine("gred", "vqs 4 default 3 \n vq 3 prio 1 limit 60000b min 15000b max 45000b "+
				"\n  Queue size: average 0b current 0b \n  Dropped packets: forced 0 early 0 pdrop 0 other 0 "+
				"\n  Total packets: 7 (99b) \n")}},
		{"positive: cake default: raw, no-ack-filter (K38, Q80)", show,
			showD0(aqmQ("cake", cakeBase)),
			result{stdout: aqmLine("cake", "bandwidth unlimited diffserv3 flows nonat nowash no-ack-filter no-split-gso raw overhead 0 \n")}},
		{"positive: cake default in JSON: ack-filter a string, rtt and fwmark present (Q80, Q81)", showJ,
			showD0(aqmQ("cake", cakeBase)),
			result{stdout: `[{"kind":"cake","handle":"1:","root":true,"refcnt":2,"options":{"bandwidth":"unlimited","diffserv":"diffserv3","flowmode":"flows","nat":false,"wash":false,"ingress":false,"ack-filter":"disabled","split_gso":false,"rtt":0,"raw":true,"overhead":0,"fwmark":"0"}}]` + "\n"}},
		{"positive: cake fwmark and rtt in text when nonzero (Q81)", show,
			showD0(aqmQ("cake", attr(xtcpnl.TcaCakeFwmark, le32(0xff)), attr(xtcpnl.TcaCakeRtt, le32(20000)),
				attr(xtcpnl.TcaCakeAckFilter, le32(2)), attr(xtcpnl.TcaCakeAtm, le32(1)))),
			result{stdout: aqmLine("cake", "nonat nowash ack-filter-aggressive no-split-gso rtt 20ms atm overhead 0 fwmark 0xff \n")}},
		{"positive: cake diffserv4 -s: the four-tin header", showS,
			showD0(aqmQs("cake", cakeXstats(4, -1, attr(xtcpnl.TcaCakeStatsMinNetlen, le32(65535)),
				attr(xtcpnl.TcaCakeStatsMaxNetlen, le32(0))), cakeBase)),
			result{stdout: aqmLine("cake", "bandwidth unlimited diffserv3 flows nonat nowash no-ack-filter no-split-gso raw overhead 0 ") + aqmSent +
				" min/max network layer size:        65535 /       0\n" +
				"                   Bulk  Best Effort        Video        Voice\n" +
				"  thresh           0bit         0bit         0bit         0bit\n" +
				"  target            5ms          5ms          5ms          5ms\n" +
				"  interval          0us          0us          0us          0us\n" +
				"  pk_delay          0us          0us          0us          0us\n" +
				"  av_delay          0us          0us          0us          0us\n" +
				"  sp_delay          0us          0us          0us          0us\n" +
				"  backlog            0b           0b           0b           0b\n" +
				"  pkts                0            0            0            0\n" +
				"  bytes               0            0            0            0\n" +
				"  way_inds            0            0            0            0\n" +
				"  way_miss            0            0            0            0\n" +
				"  way_cols            0            0            0            0\n" +
				"  drops               0            0            0            0\n" +
				"  marks               0            0            0            0\n" +
				"  ack_drop            0            0            0            0\n" +
				"  sp_flows            0            0            0            0\n" +
				"  bk_flows            0            0            0            0\n" +
				"  un_flows            0            0            0            0\n" +
				"  max_len             0            0            0            0\n" +
				"  quantum             0            0            0            0\n\n"}},
		{"positive: dualpi2 -j: hyphenated keys (Q83)", showJ,
			showD0(aqmQ("dualpi2", attr(xtcpnl.TcaDualpi2MemoryLimit, le32(4000000)),
				attr(xtcpnl.TcaDualpi2StepThreshUs, le32(1000)), attr(xtcpnl.TcaDualpi2Alpha, le32(41)),
				attr(xtcpnl.TcaDualpi2DropEarly, []byte{1}), attr(xtcpnl.TcaDualpi2EcnMask, []byte{3}))),
			result{stdout: `[{"kind":"dualpi2","handle":"1:","root":true,"refcnt":2,"options":{"memory-limit":4000000,"alpha":0.160156,"step-thresh-us":1000,"drop-early":"drop-enqueue","ecn-mask":"any-ect"}}]` + "\n"}},
		{"positive: dualpi2 -s: a blank line after the xstats (Q86)", showS,
			showD0(aqmQs("dualpi2", cat(le32(0), le32(0), le32(0), le32(0), le32(0), le32(0), le32(0), le32(0),
				le32(0xfffffff6), le32(0), le32(0), le32(9)), attr(xtcpnl.TcaDualpi2Limit, le32(5)))),
			result{stdout: aqmLine("dualpi2", "limit 5p ") + aqmSent + "prob 0.000000 delay_c 0us delay_l 0us\npkts_in_c 0 pkts_in_l 0 maxq 0\n" +
				"ecn_mark 0 step_mark 0\ncredit -10 (L-queue)\nmemory_used 0 (max 0) of memory limit 9\n\n"}},
		{"positive: plug prints no options (K41)", show,
			showD0(aqmQ("plug", attr(1, le32(5)))), result{stdout: aqmLine("plug", "\n")}},
		{"positive: skbprio's bare struct (K42)", show,
			showD0(schedQ("skbprio", rawOpts(le32(100)))), result{stdout: aqmLine("skbprio", "limit 100 \n")}},
		{"positive: hhf options", show,
			showD0(aqmQ("hhf", attr(xtcpnl.TcaHhfBacklogLimit, le32(500)), attr(xtcpnl.TcaHhfResetTimeout, le32(40000)),
				attr(xtcpnl.TcaHhfAdmitBytes, le32(65536)))),
			result{stdout: aqmLine("hhf", "limit 500p reset_timeout 40ms admit_bytes 64Kb \n")}},
		{"positive: codel options, ecn only when set", show,
			showD0(aqmQ("codel", attr(xtcpnl.TcaCodelLimit, le32(1000)), attr(xtcpnl.TcaCodelTarget, le32(4999)),
				attr(xtcpnl.TcaCodelEcn, le32(0)))),
			result{stdout: aqmLine("codel", "limit 1000p target 5ms \n")}},
		{"positive: pie -s with dq_rate_estimating", showS,
			showD0(aqmQs("pie", cat(le64(1<<63), le32(20), le32(7), le32(1), le32(0), le32(0), le32(0), le32(0), le32(0)),
				attr(xtcpnl.TcaPieBytemode, le32(1)))),
			result{stdout: aqmLine("pie", "bytemode ") + aqmSent + "  prob 0.5 delay 20us avg_dq_rate 7\n  pkts_in 0 overlimit 0 dropped 0 maxq 0 ecn_mark 0\n"}},
		{"positive: fq_pie options", show,
			showD0(aqmQ("fq_pie", attr(xtcpnl.TcaFqPieFlows, le32(256)), attr(xtcpnl.TcaFqPieQuantum, le32(1514)))),
			result{stdout: aqmLine("fq_pie", "flows 256 quantum 1514b \n")}},
		{"positive: sfb -s", showS,
			showD0(aqmQs("sfb", cat(le32(1), le32(2), le32(3), le32(4), le32(5), le32(6), le32(7), le32(0xffff), le32(0)),
				attr(xtcpnl.TcaSfbParms, cat(le32(600), le32(60), le32(25), le32(10), le32(5), le32(1), le32(100), le32(10), le32(20))))),
			result{stdout: aqmLine("sfb", "rehash 600ms db 60ms limit 100p max 25p target 10p increment 7.62951e-05 decrement 1.5259e-05 penalty_rate 10pps penalty_burst 20p ") +
				aqmSent + "  earlydrop 1 penaltydrop 2 bucketdrop 3 queuedrop 4 childdrop 5 marked 6\n  maxqlen 7 maxprob 1 avgprob 0\n"}},
		// Negative.
		{"negative: gred VQ_DP 99 is refused (Q68)", show,
			showD0(aqmQ("gred", gred2, gredVqList(attr(xtcpnl.TcaGredVqDp, le32(99))))),
			result{stdout: aqmLine("gred", ""), stderr: "gotc: not implemented: gred VQ_DP past MAX_DPs (Q68) (tc reads it unchecked)\n", rc: 255}},
		{"negative: a VQ entry with rta_len 2 is refused (Q71)", show,
			showD0(aqmQ("gred", gred2, attr(xtcpnl.TcaGredVqList, cat(le16(2), le16(1))))),
			result{stdout: aqmLine("gred", ""), stderr: "gotc: not implemented: gred xtcpnl: tc structure payload is short: gred VQ entry rta_len 2 at offset 0 (Q71)\n", rc: 255}},
		{"negative: gred without DPS is refused (Q67)", show,
			showD0(aqmQ("gred", gredParms(0))),
			result{stdout: aqmLine("gred", ""), stderr: "gotc: not implemented: gred xtcpnl: tc attribute missing: TCA_GRED_DPS (Q67)\n", rc: 255}},
		{"negative: gred PARMS short prints tc's raw message (Q72)", show,
			showD0(aqmQ("gred", attr(xtcpnl.TcaGredParms, le32(0)), gredDps(1, 0, 0))),
			result{stdout: aqmLine("gred", "\n GRED received message smaller than expected\n\n")}},
		{"negative: cake text with tin 1 missing an attribute tin 0 has is refused (Q78)", showS,
			showD0(aqmQs("cake", cakeXstats(3, 1))),
			result{stdout: aqmLine("cake", "nonat nowash no-ack-filter no-split-gso noatm overhead 0 ") + aqmSent,
				stderr: "gotc: not implemented: cake tin attribute that only tin 0 has (Q78) (tc reads it unchecked)\n", rc: 255}},
		{"corner: cake text skips a short tin 1 field tin 0 lacks (PR 353 review)", showS,
			showD0(aqmQs("cake", cakeShortTins(false))),
			result{stdout: aqmLine("cake", "nonat nowash no-ack-filter no-split-gso noatm overhead 0 ") + aqmSent + "                  Tin 0        Tin 1\n\n"}},
		{"negative: cake JSON reads a short tin 1 field tin 0 lacks", []string{"-s", "-j", "qdisc", "show", "dev", "d0"},
			showD0(aqmQs("cake", cakeShortTins(false))),
			result{stdout: "[{\"kind\":\"cake\",\"handle\":\"1:\",\"root\":true,\"refcnt\":2,\"options\":{\"nat\":false,\"wash\":false,\"ingress\":false,\"ack-filter\":\"disabled\",\"split_gso\":false,\"rtt\":0,\"raw\":false,\"atm\":\"noatm\",\"overhead\":0,\"fwmark\":\"0\"},\"bytes\":0,\"packets\":0", stderr: "gotc: not implemented: cake xtcpnl: tc structure payload is short: cake tin 1 attribute mask 0x2000\n", rc: 255}},
		{"negative: cake text reads a short tin 1 field tin 0 has", showS,
			showD0(aqmQs("cake", cakeShortTins(true))),
			result{stdout: aqmLine("cake", "nonat nowash no-ack-filter no-split-gso noatm overhead 0 ") + aqmSent, stderr: "gotc: not implemented: cake xtcpnl: tc structure payload is short: cake tin 1 attribute mask 0x2000\n", rc: 255}},
		{"negative: a short cake stat is refused (Q77)", showS,
			showD0(aqmQs("cake", attr(xtcpnl.TcaCakeStatsActiveQueues, []byte{1}))),
			result{stdout: aqmLine("cake", "nonat nowash no-ack-filter no-split-gso noatm overhead 0 ") + aqmSent,
				stderr: "gotc: not implemented: cake xtcpnl: tc structure payload is short: cake stats attribute mask 0x20000\n", rc: 255}},
		{"negative: red's short qevent block is refused", show,
			showD0(aqmQ("red", redParms, attr(xtcpnl.TcaRedMarkBlock, []byte{1}))),
			result{stdout: aqmLine("red", "limit 400000b min 30000b max 90000b "),
				stderr: "gotc: not implemented: red qevent mark block of 1 bytes (tc reads it unchecked)\n", rc: 255}},
		{"negative: red's zero qevent block is illegal", show,
			showD0(aqmQ("red", redParms, attr(xtcpnl.TcaRedMarkBlock, le32(0)))),
			result{stdout: aqmLine("red", "limit 400000b min 30000b max 90000b \n"), stderr: "Illegal qevent block index\n"}},
		{"negative: red without PARMS prints nothing", show,
			showD0(aqmQ("red", attr(xtcpnl.TcaRedMaxP, le32(1)))), result{stdout: aqmLine("red", "\n")}},
		// Boundary.
		{"boundary: sfb rehash 5000000 ms wraps in u32 (Q73)", show,
			showD0(aqmQ("sfb", attr(xtcpnl.TcaSfbParms, cat(le32(5000000), le32(5000000), le32(0), le32(0), le32(0), le32(0), le32(0), le32(0), le32(0))))),
			result{stdout: aqmLine("sfb", "rehash 705s db 705s limit 0p max 0p target 0p increment 0 decrement 0 penalty_rate 0pps penalty_burst 0p \n")}},
		{"boundary: dualpi2 split_gso true is a JSON null (Q84)", showJ,
			showD0(aqmQ("dualpi2", attr(xtcpnl.TcaDualpi2SplitGso, []byte{1}))),
			result{stdout: `[{"kind":"dualpi2","handle":"1:","root":true,"refcnt":2,"options":{"split-gso":null}}]` + "\n"}},
		{"boundary: dualpi2 split_gso false has no key, text no_split_gso; CLA_ECT prints any_ect (Q84, Q85)", show,
			showD0(aqmQ("dualpi2", attr(xtcpnl.TcaDualpi2SplitGso, []byte{0}), attr(xtcpnl.TcaDualpi2EcnMask, []byte{2}))),
			result{stdout: aqmLine("dualpi2", "any_ect no_split_gso \n")}},
		{"boundary: gred VQ_STAT_BYTES 2^32+5 prints tc's u32 (Q70)", showS,
			showD0(aqmQ("gred", cat(gredParms(0), gredDps(1, 0, 0)), gredVqList(cat(attr(xtcpnl.TcaGredVqDp, le32(0)),
				attr(xtcpnl.TcaGredVqStatBytes, le64(1<<32+5)))))),
			result{stdout: aqmLine("gred", "vqs 1 default 0 \n vq 0 prio 1 limit 60000b min 15000b max 45000b "+
				"\n  Queue size: average 0b current 0b \n  Dropped packets: forced 0 early 0 pdrop 0 other 0 "+
				"\n  Marked packets: forced 0 early 0 \n  Total packets: 7 (5b) \n")}},
		{"positive: filter show block finds red's qevent block (red_has_block)", []string{"filter", "show", "block", "10"},
			[]Reply{links(), rep(aqmQ("red", redParms, attr(xtcpnl.TcaRedEarlyDropBlock, le32(10))), done()), rep(done())},
			result{}},
		{"negative: filter show block with red on another block finds none", []string{"filter", "show", "block", "10"},
			[]Reply{links(), rep(aqmQ("red", redParms, attr(xtcpnl.TcaRedMarkBlock, le32(11))), done())},
			result{stderr: "Cannot find block \"10\"\n", rc: 1}},
		{"negative: filter show block over red's zero qevent block ends the dump", []string{"filter", "show", "block", "10"},
			[]Reply{links(), rep(aqmQ("red", redParms, attr(xtcpnl.TcaRedMarkBlock, le32(0))), done())},
			result{stderr: "Illegal qevent block index\nDump terminated\n: Success\nCannot find block \"10\"\n", rc: 1}},
		{"negative: filter show block over red's short qevent block is refused", []string{"filter", "show", "block", "10"},
			[]Reply{links(), rep(aqmQ("red", redParms, attr(xtcpnl.TcaRedMarkBlock, []byte{1})), done())},
			result{stderr: "gotc: not implemented: red qevent mark block of 1 bytes (tc reads it unchecked)\n", rc: 255}},
		// Corner.
		{"corner: cake -s -o keeps the hard newlines (Q79)", showSO,
			showD0(aqmQs("cake", cat(attr(xtcpnl.TcaCakeStatsActiveQueues, le32(0)), attr(xtcpnl.TcaCakeStatsAvgNetoff, le32(14))))),
			result{stdout: aqmLine("cake", "nonat nowash no-ack-filter no-split-gso noatm overhead 0 ") +
				"\\ Sent 0 bytes 0 pkt\\ active queues:                         0\n average network hdr offset:           14\n\n\\"}},
		{"corner: cake negative blue_timer: text unnegated, JSON none (Q75, Q76)", showS,
			showD0(aqmQs("cake", cat(attr(xtcpnl.TcaCakeStatsPDrop, le32(3)), attr(xtcpnl.TcaCakeStatsBlueTimerUs, le32(0xfffffc18))))),
			result{stdout: aqmLine("cake", "nonat nowash no-ack-filter no-split-gso noatm overhead 0 ") + aqmSent +
				" blue_prob 3 blue_timer -4.29e+03s\n"}},
		{"corner: cake negative drop_next is negated", showS,
			showD0(aqmQs("cake", cat(attr(xtcpnl.TcaCakeStatsDropping, le32(1)), attr(xtcpnl.TcaCakeStatsDropNextUs, le32(0xfffffc18))))),
			result{stdout: aqmLine("cake", "nonat nowash no-ack-filter no-split-gso noatm overhead 0 ") + aqmSent +
				" dropping drop_next -1ms\n"}},
		{"corner: codel drop_next INT_MIN (Q74)", showS,
			showD0(aqmQs("codel", cat(le32(0), le32(0), le32(0), le32(0), le32(0x80000000)))),
			result{stdout: aqmLine("codel", "") + aqmSent + "  count 0 lastcount 0 ldelay 0us drop_next -2.15e+03s\n  maxpacket 0 ecn_mark 0 drop_overlimit 0\n"}},
		{"corner: dualpi2 ecn mask with neither bit warns twice (Q89)", show,
			showD0(aqmQ("dualpi2", attr(xtcpnl.TcaDualpi2EcnMask, []byte{4}))),
			result{stdout: aqmLine("dualpi2", " \n"), stderr: "Warning: Unexpected ecn type 4!\nWarning: Unexpected ecn type 4!\n"}},
		{"corner: a VQ entry with NLA_F_NESTED is not found: tc's plain parse_rtattr (Q88)", showS,
			showD0(aqmQ("gred", cat(gredParms(0), gredDps(1, 0, 0)), attr(xtcpnl.TcaGredVqList|unix.NLA_F_NESTED,
				attr(xtcpnl.TcaGredVqEntry|unix.NLA_F_NESTED, cat(attr(xtcpnl.TcaGredVqDp, le32(0)),
					attr(xtcpnl.TcaGredVqStatBytes, le64(5))))))),
			result{stdout: aqmLine("gred", "vqs 1 default 0 \n vq 0 prio 1 limit 60000b min 15000b max 45000b "+
				"\n  Queue size: average 0b current 0b \n  Dropped packets: forced 0 early 0 pdrop 0 other 0 "+
				"\n  Marked packets: forced 0 early 0 \n  Total packets: 7 (0b) \n"),
				stderr: "ERROR: Failed to parse Virtual Queue entry\n"}},
		{"corner: cake_mq shares cake's printer; an unknown mode's (?diffserv?) has no space (K39, Q82)", show,
			showD0(aqmQ("cake_mq", attr(xtcpnl.TcaCakeDiffservMode, le32(9)))),
			result{stdout: aqmLine("cake_mq", "(?diffserv?)nonat nowash no-ack-filter no-split-gso noatm overhead 0 \n")}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := &fakeSource{replies: tc.replies}
			var out, errb bytes.Buffer
			cfg := config{src: src, stdin: strings.NewReader(""), sys: emptySys(t)}
			rc := run(cfg, tc.args, &out, &errb)
			got := result{stdout: out.String(), stderr: errb.String(), rc: rc, pending: len(src.replies)}
			if got != tc.expected {
				t.Errorf("tc %q\n got  %#v\n want %#v", tc.args, got, tc.expected)
			}
		})
	}
}
