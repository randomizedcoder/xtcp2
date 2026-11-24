package gotc

import (
	"encoding/binary"
	"math"
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/q_fq.c fq_print_opt and fq_print_xstats.

// printFqOpt is fq_print_opt. Its priomap and weights arrays are named
// "priomap " and "weights ", trailing space and all, in JSON too (Q2).
func printFqOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	tb := r.nested(opt, xtcpnl.TcaFqMaxKnown)
	o := r.o
	if v, ok := tb.U32(xtcpnl.TcaFqPlimit); ok {
		o.uint(printAny, "limit", "limit "+u32s(v)+"p ", v)
	}
	if v, ok := tb.U32(xtcpnl.TcaFqFlowPlimit); ok {
		o.uint(printAny, "flow_limit", "flow_limit "+u32s(v)+"p ", v)
	}
	if v, ok := tb.U32(xtcpnl.TcaFqBucketsLog); ok {
		// 1U << buckets_log: x86 masks the count to five bits.
		b := uint32(1) << (v & 31)
		o.uint(printAny, "buckets", "buckets "+u32s(b)+" ", b)
	}
	if v, ok := tb.U32(xtcpnl.TcaFqOrphanMask); ok {
		o.uint(printAny, "orphan_mask", "orphan_mask "+u32s(v)+" ", v)
	}
	if v, ok := tb.U32(xtcpnl.TcaFqRateEnable); ok && v == 0 {
		o.boolean(printAny, "pacing", "nopacing ", false)
	}
	printFqBands(r, tb)
	if v, ok := tb.U32(xtcpnl.TcaFqQuantum); ok {
		o.size(printAny, "quantum", "quantum ", " ", uint64(v))
	}
	if v, ok := tb.U32(xtcpnl.TcaFqInitialQuantum); ok {
		o.size(printAny, "initial_quantum", "initial_quantum ", " ", uint64(v))
	}
	if v, ok := tb.U32(xtcpnl.TcaFqFlowMaxRate); ok && v != math.MaxUint32 {
		o.rate(r.iec > 0, printAny, "maxrate", "maxrate ", " ", uint64(v))
	}
	if v, ok := tb.U32(xtcpnl.TcaFqFlowDefaultRate); ok && v != 0 {
		o.rate(r.iec > 0, printAny, "defrate", "defrate ", " ", uint64(v))
	}
	if v, ok := tb.U32(xtcpnl.TcaFqLowRateThreshold); ok && v != 0 {
		o.rate(r.iec > 0, printAny, "low_rate_threshold", "low_rate_threshold ", " ", uint64(v))
	}
	r.jsonTime(tb, xtcpnl.TcaFqFlowRefillDelay, "refill_delay")
	if v, ok := tb.U32(xtcpnl.TcaFqCeThreshold); ok && v != math.MaxUint32 {
		r.jsonTime(tb, xtcpnl.TcaFqCeThreshold, "ce_threshold")
	}
	if v, ok := tb.U32(xtcpnl.TcaFqTimerSlack); ok {
		o.uint(printJSON, "timer_slack", "", v)
		o.fp("timer_slack " + sprintTime64(int64(v)) + " ")
	}
	r.jsonTime(tb, xtcpnl.TcaFqHorizon, "horizon")
	if v, ok := tb.U8(xtcpnl.TcaFqHorizonDrop); ok {
		if v == 0 {
			o.null(printAny, "horizon_cap", "horizon_cap ")
		} else {
			o.null(printAny, "horizon_drop", "horizon_drop ")
		}
	}
	r.jsonTime(tb, xtcpnl.TcaFqOffloadHorizon, "offload_horizon")
}

// printFqBands is fq_print_opt's TCA_FQ_PRIOMAP and TCA_FQ_WEIGHTS. A
// weight is an int printed by print_uint: "%d" in text, its u32 bits in
// JSON.
func printFqBands(r *runner, tb xtcpnl.AttrTable) {
	o := r.o
	if b := tb.Bytes(xtcpnl.TcaFqPriomap); len(b) >= xtcpnl.TcPrioQoptSizeCst {
		p, err := xtcpnl.DecodeTcPrioQopt(b)
		if err == nil {
			bands := cint.Bits(p.Bands)
			o.uint(printAny, "bands", "bands "+u32s(bands)+" ", bands)
			o.openArray(printAny, "priomap ")
			for _, v := range p.Priomap {
				o.uint(printAny, "", strconv.Itoa(int(v))+" ", uint32(v))
			}
			o.closeArray(printAny, "")
		}
	}
	if b := tb.Bytes(xtcpnl.TcaFqWeights); len(b) >= xtcpnl.FqBandsCst*4 {
		o.openArray(printAny, "weights ")
		for i := range xtcpnl.FqBandsCst {
			w := binary.LittleEndian.Uint32(b[i*4 : i*4+4])
			o.uint(printAny, "", strconv.Itoa(int(cint.Int(w)))+" ", w)
		}
		o.closeArray(printAny, "")
	}
}

// printFqXstats is fq_print_xstats: the MIN-copy, so it never stops. The
// key "throttled" goes out twice, for throttled_flows and throttled (Q1).
func printFqXstats(r *runner, xs []byte) {
	st := xtcpnl.DecodeTcFqQdStats(xs)
	o := r.o
	o.uint(printAny, "flows", "  flows "+u32s(st.Flows), st.Flows)
	o.uint(printAny, "inactive", " (inactive "+u32s(st.InactiveFlows), st.InactiveFlows)
	o.uint(printAny, "throttled", " throttled "+u32s(st.ThrottledFlows)+")", st.ThrottledFlows)
	for i, n := range st.BandPktCount {
		k := "band" + strconv.Itoa(i) + "_pkts"
		o.uint(printAny, k, " "+k+" "+u32s(n), n)
	}
	if st.TimeNextDelayedFlow > 0 {
		o.u64(printJSON, "next_packet_delay", "", cint.U64Bits(st.TimeNextDelayedFlow))
		o.fp(" next_packet_delay " + sprintTime64(st.TimeNextDelayedFlow))
	}
	o.nl()
	o.u64(printAny, "gc", "  gc "+u64s(st.GcFlows), st.GcFlows)
	o.u64(printAny, "highprio", " highprio "+u64s(st.HighprioPackets), st.HighprioPackets)
	if st.FastpathPackets != 0 {
		o.u64(printAny, "fastpath", " fastpath "+u64s(st.FastpathPackets), st.FastpathPackets)
	}
	if st.TCPRetrans != 0 {
		o.u64(printAny, "retrans", " retrans "+u64s(st.TCPRetrans), st.TCPRetrans)
	}
	o.u64(printAny, "throttled", " throttled "+u64s(st.Throttled), st.Throttled)
	if st.UnthrottleLatencyNs != 0 {
		o.uint(printJSON, "latency", "", st.UnthrottleLatencyNs)
		o.fp(" latency " + sprintTime64(int64(st.UnthrottleLatencyNs)))
	}
	if st.CeMark != 0 {
		o.u64(printAny, "ce_mark", " ce_mark "+u64s(st.CeMark), st.CeMark)
	}
	if st.FlowsPlimit != 0 {
		o.u64(printAny, "flows_plimit", " flows_plimit "+u64s(st.FlowsPlimit), st.FlowsPlimit)
	}
	printFqDrops(r, &st)
}

func printFqDrops(r *runner, st *xtcpnl.TcFqQdStats) {
	if st.PktsTooLong == 0 && st.AllocationErrors == 0 && st.HorizonDrops == 0 && st.HorizonCaps == 0 &&
		st.BandDrops == [xtcpnl.FqBandsCst]uint64{} {
		return
	}
	o := r.o
	o.nl()
	for _, f := range []struct {
		key, label string
		v          uint64
	}{
		{"pkts_too_long", " pkts_too_long ", st.PktsTooLong},
		{"alloc_errors", " alloc_errors ", st.AllocationErrors},
		{"horizon_drops", " horizon_drops ", st.HorizonDrops},
		{"horizon_caps", "  horizon_caps ", st.HorizonCaps},
		{"band0_drops", " band0_drops ", st.BandDrops[0]},
		{"band1_drops", " band1_drops ", st.BandDrops[1]},
		{"band2_drops", " band2_drops ", st.BandDrops[2]},
	} {
		if f.v != 0 {
			o.u64(printAny, f.key, f.label+u64s(f.v), f.v)
		}
	}
}
