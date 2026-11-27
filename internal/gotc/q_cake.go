package gotc

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/q_cake.c cake_print_opt, cake_print_mode, cake_print_xstats and
// cake_print_json_tin; cake_mq shares them (K39).

var (
	cakeDiffservNames = [xtcpnl.CakeDiffservMax]string{"diffserv3", "diffserv4", "diffserv8", "besteffort", "precedence"}
	cakeFlowmodeNames = [xtcpnl.CakeFlowMax]string{"flowblind", "srchost", "dsthost", "hosts", "flows",
		"dual-srchost", "dual-dsthost", "triple-isolate"}
)

// cakePrintMode is cake_print_mode: an unknown value's text "(?key?)" has
// no trailing space (Q82).
func cakePrintMode(o *out, v uint32, table []string, key string) {
	if int64(v) < int64(len(table)) {
		o.str(printAny, colorNone, key, table[v]+" ", table[v])
		return
	}
	o.str(printJSON, colorNone, key, "", "unknown")
	o.fp("(?" + key + "?)")
}

// cakeU32 is the guarded `if (tb[t] && RTA_PAYLOAD >= 4) v = u32` read,
// 0 otherwise.
func cakeU32(tb xtcpnl.AttrTable, t uint16) uint32 {
	v, _ := tb.U32(t)
	return v
}

// cakeFlag is a word in text when on, and a JSON bool always.
func cakeFlag(o *out, on bool, key, yes, no string) {
	if on {
		o.fp(yes)
	} else if no != "" {
		o.fp(no)
	}
	o.boolean(printJSON, key, "", on)
}

// printCakeOpt is cake_print_opt. The flags are JSON bools beside an
// "ack-filter" string (Q80); rtt and fwmark print in text when nonzero,
// in JSON always, fwmark as a hex string (Q81). TARGET is not read (K38).
func printCakeOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	tb := r.nested(opt, xtcpnl.TcaCakeMaxKnown)
	o := r.o
	if bw, ok := tb.U64(xtcpnl.TcaCakeBaseRate64); ok {
		if bw != 0 {
			o.rate(r.iec > 0, printAny, "bandwidth", "bandwidth ", " ", bw)
		} else {
			o.str(printAny, colorNone, "bandwidth", "bandwidth unlimited ", "unlimited")
		}
	}
	if v, ok := tb.U32(xtcpnl.TcaCakeAutorate); ok {
		switch {
		case v == 1:
			o.str(printAny, colorNone, "autorate", "autorate-ingress ", "autorate-ingress")
		case v != 0:
			o.str(printAny, colorNone, "autorate", "(?autorate?) ", "unknown")
		}
	}
	if v, ok := tb.U32(xtcpnl.TcaCakeDiffservMode); ok {
		cakePrintMode(o, v, cakeDiffservNames[:], "diffserv")
	}
	if v, ok := tb.U32(xtcpnl.TcaCakeFlowMode); ok {
		cakePrintMode(o, v, cakeFlowmodeNames[:], "flowmode")
	}
	cakeFlag(o, cakeU32(tb, xtcpnl.TcaCakeNat) != 0, "nat", "nat ", "nonat ")
	atm := cakeU32(tb, xtcpnl.TcaCakeAtm)
	overhead := cint.Int(cakeU32(tb, xtcpnl.TcaCakeOverhead))
	mpu := cakeU32(tb, xtcpnl.TcaCakeMpu)
	ack := cakeU32(tb, xtcpnl.TcaCakeAckFilter)
	raw := tb.Has(xtcpnl.TcaCakeRaw)
	interval := cakeU32(tb, xtcpnl.TcaCakeRtt)
	memlimit := cakeU32(tb, xtcpnl.TcaCakeMemory)
	fwmark := cakeU32(tb, xtcpnl.TcaCakeFwmark)
	cakeFlag(o, cakeU32(tb, xtcpnl.TcaCakeWash) != 0, "wash", "wash ", "nowash ")
	cakeFlag(o, cakeU32(tb, xtcpnl.TcaCakeIngress) != 0, "ingress", "ingress ", "")
	switch ack {
	case xtcpnl.CakeAckAggressive:
		o.str(printAny, colorNone, "ack-filter", "ack-filter-aggressive ", "aggressive")
	case xtcpnl.CakeAckFilter:
		o.str(printAny, colorNone, "ack-filter", "ack-filter ", "enabled")
	default:
		o.str(printAny, colorNone, "ack-filter", "no-ack-filter ", "disabled")
	}
	cakeFlag(o, cakeU32(tb, xtcpnl.TcaCakeSplitGso) != 0, "split_gso", "split-gso ", "no-split-gso ")
	if interval != 0 {
		o.fp("rtt " + sprintTime(interval) + " ")
	}
	o.uint(printJSON, "rtt", "", interval)
	cakeFlag(o, raw, "raw", "raw ", "")
	switch {
	case atm == xtcpnl.CakeAtmAtm:
		o.str(printAny, colorNone, "atm", "atm ", "atm")
	case atm == xtcpnl.CakeAtmPtm:
		o.str(printAny, colorNone, "atm", "ptm ", "ptm")
	case !raw:
		o.str(printAny, colorNone, "atm", "noatm ", "noatm")
	}
	o.int(printAny, "overhead", "overhead "+strconv.Itoa(int(overhead))+" ", overhead)
	if mpu != 0 {
		o.uint(printAny, "mpu", "mpu "+u32s(mpu)+" ", mpu)
	}
	if memlimit != 0 {
		o.size(printAny, "memlimit", "memlimit ", " ", uint64(memlimit))
	}
	if fwmark != 0 {
		o.fp("fwmark 0x" + strconv.FormatUint(uint64(fwmark), 16) + " ")
	}
	o.hex0x(printJSON, "fwmark", "", uint64(fwmark))
}

// printCakeXstats is cake_print_xstats. Its text lines end in hard "\n"s
// that ignore -o, the average offset's in two (Q79); a negative drop_next
// or blue_timer is text only (Q76), and blue_timer goes to sprint_time
// unnegated (Q75). Where tc reads a short stat (Q77) or, in text, a later
// tin's attribute that only tin 0 has (Q78), gotc refuses.
func printCakeXstats(r *runner, xs []byte) {
	x, err := xtcpnl.ParseTcXstats(xtcpnl.TcKindCake, xs)
	if err != nil {
		r.refuseKind(fmt.Errorf("%w: cake %w", errNotImplemented, err))
		return
	}
	c := x.Aqm.Cake
	st := &c.Stats
	o := r.o
	used, okU := st.U32(xtcpnl.TcaCakeStatsMemoryUsed)
	limit, okL := st.U32(xtcpnl.TcaCakeStatsMemoryLimit)
	if okU && okL {
		o.fp(" memory used: " + sprintSize(uint64(used)))
		o.fp(" of " + sprintSize(uint64(limit)) + "\n")
		o.uint(printJSON, "memory_used", "", used)
		o.uint(printJSON, "memory_limit", "", limit)
	}
	if v, ok := st.Get(xtcpnl.TcaCakeStatsCapacityEstimate64); ok {
		o.rate(r.iec > 0, printAny, "capacity_estimate", " capacity estimate: ", "\n", v)
	}
	r.cakePair(st, xtcpnl.TcaCakeStatsMinNetlen, xtcpnl.TcaCakeStatsMaxNetlen,
		"min_network_size", " min/max network layer size: ", 12, "max_network_size")
	r.cakePair(st, xtcpnl.TcaCakeStatsMinAdjlen, xtcpnl.TcaCakeStatsMaxAdjlen,
		"min_adj_size", " min/max overhead-adjusted size: ", 8, "max_adj_size")
	if v, ok := st.U32(xtcpnl.TcaCakeStatsActiveQueues); ok {
		o.uint(printAny, "active_queues", fmt.Sprintf(" active queues: %25d\n", v), v)
	}
	if v, ok := st.U32(xtcpnl.TcaCakeStatsAvgNetoff); ok {
		o.uint(printAny, "avg_hdr_offset", fmt.Sprintf(" average network hdr offset: %12d\n\n", v), v)
	}
	if v, ok := st.S32(xtcpnl.TcaCakeStatsDeficit); ok {
		o.int(printAny, "deficit", "  deficit "+strconv.Itoa(int(v)), v)
	}
	if v, ok := st.U32(xtcpnl.TcaCakeStatsCobaltCount); ok {
		o.uint(printAny, "count", " count "+u32s(v), v)
	}
	if d, ok := st.U32(xtcpnl.TcaCakeStatsDropping); ok && d != 0 {
		o.boolean(printAny, "dropping", " dropping", true)
		if v, ok := st.S32(xtcpnl.TcaCakeStatsDropNextUs); ok {
			if v < 0 {
				o.fp(" drop_next -" + sprintTime(cint.Bits(-v)))
			} else {
				o.uint(printJSON, "drop_next", "", cint.Bits(v))
				o.fp(" drop_next " + sprintTime(cint.Bits(v)))
			}
		}
	}
	if v, ok := st.U32(xtcpnl.TcaCakeStatsPDrop); ok {
		o.uint(printAny, "blue_prob", " blue_prob "+u32s(v), v)
		if t, ok := st.S32(xtcpnl.TcaCakeStatsBlueTimerUs); ok {
			if t < 0 {
				o.fp(" blue_timer -" + sprintTime(cint.Bits(t)))
			} else {
				o.uint(printJSON, "blue_timer", "", cint.Bits(t))
				o.fp(" blue_timer " + sprintTime(cint.Bits(t)))
			}
		}
	}
	if c.NTins == 0 {
		return
	}
	if o.isJSON() {
		o.openArray(printJSON, "tins")
		for i := range c.NTins {
			cakeJSONTin(o, &c.Tins[i])
		}
		o.closeArray(printJSON, "")
		return
	}
	if c.TinGap {
		r.refuseKind(actUnchecked(xtcpnl.TcKindCake, "tin attribute that only tin 0 has (Q78)"))
		return
	}
	r.cakeTinTable(c)
}

// cakePair is the min/max pair printed when both are there.
func (r *runner) cakePair(st *xtcpnl.TcVals, minT, maxT uint16, minKey, label string, width int, maxKey string) {
	lo, ok1 := st.U32(minT)
	hi, ok2 := st.U32(maxT)
	if !ok1 || !ok2 {
		return
	}
	r.o.uint(printAny, minKey, fmt.Sprintf("%s%*d", label, width, lo), lo)
	r.o.uint(printAny, maxKey, fmt.Sprintf(" /%8d\n", hi), hi)
}

// cakeJSONTins are cake_print_json_tin's keys, in order.
var cakeJSONTins = [...]struct {
	key string
	t   uint16
}{
	{"threshold_rate", xtcpnl.TcaCakeTinStatsThresholdRate64},
	{"sent_bytes", xtcpnl.TcaCakeTinStatsSentBytes64},
	{"backlog_bytes", xtcpnl.TcaCakeTinStatsBacklogBytes},
	{"target_us", xtcpnl.TcaCakeTinStatsTargetUs},
	{"interval_us", xtcpnl.TcaCakeTinStatsIntervalUs},
	{"peak_delay_us", xtcpnl.TcaCakeTinStatsPeakDelayUs},
	{"avg_delay_us", xtcpnl.TcaCakeTinStatsAvgDelayUs},
	{"base_delay_us", xtcpnl.TcaCakeTinStatsBaseDelayUs},
	{"sent_packets", xtcpnl.TcaCakeTinStatsSentPackets},
	{"way_indirect_hits", xtcpnl.TcaCakeTinStatsWayIndirectHits},
	{"way_misses", xtcpnl.TcaCakeTinStatsWayMisses},
	{"way_collisions", xtcpnl.TcaCakeTinStatsWayCollisions},
	{"drops", xtcpnl.TcaCakeTinStatsDroppedPackets},
	{"ecn_mark", xtcpnl.TcaCakeTinStatsEcnMarkedPackets},
	{"ack_drops", xtcpnl.TcaCakeTinStatsAcksDroppedPackets},
	{"sparse_flows", xtcpnl.TcaCakeTinStatsSparseFlows},
	{"bulk_flows", xtcpnl.TcaCakeTinStatsBulkFlows},
	{"unresponsive_flows", xtcpnl.TcaCakeTinStatsUnresponsiveFlows},
	{"max_pkt_len", xtcpnl.TcaCakeTinStatsMaxSkblen},
	{"flow_quantum", xtcpnl.TcaCakeTinStatsFlowQuantum},
}

// cakeJSONTin is cake_print_json_tin: each attribute the tin has.
func cakeJSONTin(o *out, tin *xtcpnl.TcVals) {
	o.openObject("")
	for _, f := range cakeJSONTins {
		if v, ok := tin.Get(f.t); ok {
			o.u64(printJSON, f.key, "", v)
		}
	}
	o.closeObject()
}

// cakeTinRow is one PRINT_TSTAT row: the label and how a value prints.
type cakeTinRow struct {
	label string
	t     uint16
	cell  func(r *runner, v uint64) string
}

func cakeCellTime(_ *runner, v uint64) string { return sprintTime(uint32(v & 0xffffffff)) }
func cakeCellSize(_ *runner, v uint64) string { return sprintSize(v & 0xffffffff) }
func cakeCellUint(_ *runner, v uint64) string { return strconv.FormatUint(v, 10) }
func cakeCellRate(r *runner, v uint64) string { return sprintRate(r.iec > 0, v) }

var cakeTinRows = [...]cakeTinRow{
	{"  thresh  ", xtcpnl.TcaCakeTinStatsThresholdRate64, cakeCellRate},
	{"  target  ", xtcpnl.TcaCakeTinStatsTargetUs, cakeCellTime},
	{"  interval", xtcpnl.TcaCakeTinStatsIntervalUs, cakeCellTime},
	{"  pk_delay", xtcpnl.TcaCakeTinStatsPeakDelayUs, cakeCellTime},
	{"  av_delay", xtcpnl.TcaCakeTinStatsAvgDelayUs, cakeCellTime},
	{"  sp_delay", xtcpnl.TcaCakeTinStatsBaseDelayUs, cakeCellTime},
	{"  backlog ", xtcpnl.TcaCakeTinStatsBacklogBytes, cakeCellSize},
	{"  pkts    ", xtcpnl.TcaCakeTinStatsSentPackets, cakeCellUint},
	{"  bytes   ", xtcpnl.TcaCakeTinStatsSentBytes64, cakeCellUint},
	{"  way_inds", xtcpnl.TcaCakeTinStatsWayIndirectHits, cakeCellUint},
	{"  way_miss", xtcpnl.TcaCakeTinStatsWayMisses, cakeCellUint},
	{"  way_cols", xtcpnl.TcaCakeTinStatsWayCollisions, cakeCellUint},
	{"  drops   ", xtcpnl.TcaCakeTinStatsDroppedPackets, cakeCellUint},
	{"  marks   ", xtcpnl.TcaCakeTinStatsEcnMarkedPackets, cakeCellUint},
	{"  ack_drop", xtcpnl.TcaCakeTinStatsAcksDroppedPackets, cakeCellUint},
	{"  sp_flows", xtcpnl.TcaCakeTinStatsSparseFlows, cakeCellUint},
	{"  bk_flows", xtcpnl.TcaCakeTinStatsBulkFlows, cakeCellUint},
	{"  un_flows", xtcpnl.TcaCakeTinStatsUnresponsiveFlows, cakeCellUint},
	{"  max_len ", xtcpnl.TcaCakeTinStatsMaxSkblen, cakeCellUint},
	{"  quantum ", xtcpnl.TcaCakeTinStatsFlowQuantum, cakeCellUint},
}

// cakeTinTable is the text tin table: a header by tin count, then a row
// for each attribute tin 0 has, each line ending _SL_.
func (r *runner) cakeTinTable(c *xtcpnl.TcCakeXstats) {
	o := r.o
	switch c.NTins {
	case 3:
		o.raw("                   Bulk  Best Effort        Voice\n")
	case 4:
		o.raw("                   Bulk  Best Effort        Video        Voice\n")
	default:
		var b strings.Builder
		b.WriteString("          ")
		for i := range c.NTins {
			fmt.Fprintf(&b, "        Tin %d", i)
		}
		o.raw(b.String() + o.sl)
	}
	for _, row := range cakeTinRows {
		if _, ok := c.Tins[0].Get(row.t); !ok {
			continue
		}
		var b strings.Builder
		b.WriteString(row.label)
		for i := range c.NTins {
			v, _ := c.Tins[i].Get(row.t)
			fmt.Fprintf(&b, " %12s", row.cell(r, v))
		}
		o.raw(b.String() + o.sl)
	}
}
