package gotc

import (
	"fmt"
	"math"
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/q_htb.c, tc/q_tbf.c and tc/q_sfq.c.

// tcLinklayerMask is TC_LINKLAYER_MASK.
const tcLinklayerMask = 0x0F

// cellOf is `1 << cell_log` as tc prints it with %u: an int shift, which
// x86 masks to five bits, so a cell_log of 31 is 2147483648 and 32 is 1
// (Q13).
func cellOf(cellLog uint8) uint32 { return uint32(1) << (cellLog & 31) }

// printHtbOpt is htb_print_opt, for a qdisc and a class alike. Its prio,
// quantum and r2q are u32s printed with print_int (Q11); -r's buffer words
// are a bare fprintf, inside -j's JSON too (Q12).
func printHtbOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	tb := r.nested(opt, xtcpnl.TcaHtbMaxKnown)
	o := r.o
	if tb.Has(xtcpnl.TcaHtbParms) {
		h, err := xtcpnl.DecodeTcHtbOpt(tb.Bytes(xtcpnl.TcaHtbParms))
		if err != nil {
			return
		}
		r.htbParms(tb, &h)
	}
	if tb.Has(xtcpnl.TcaHtbInit) {
		g, err := xtcpnl.DecodeTcHtbGlob(tb.Bytes(xtcpnl.TcaHtbInit))
		if err != nil {
			return
		}
		r2q := cint.Int(g.Rate2Quantum)
		o.int(printAny, "r2q", "r2q "+strconv.Itoa(int(r2q)), r2q)
		o.hex0x(printAny, "default", " default "+cHashX(uint64(g.Defcls)), uint64(g.Defcls))
		o.uint(printAny, "direct_packets_stat", " direct_packets_stat "+u32s(g.DirectPkts), g.DirectPkts)
		if r.details > 0 {
			ver := strconv.Itoa(int(g.Version>>16)) + "." + strconv.Itoa(int(g.Version&0xffff))
			o.str(printAny, colorNone, "ver", " ver "+ver, ver)
		}
	}
	if v, ok := tb.U32(xtcpnl.TcaHtbDirectQlen); ok {
		o.uint(printAny, "direct_qlen", " direct_qlen "+u32s(v), v)
	}
	if tb.Has(xtcpnl.TcaHtbOffload) {
		o.null(printAny, "offload", " offload")
	}
}

func (r *runner) htbParms(tb xtcpnl.AttrTable, h *xtcpnl.TcHtbOpt) {
	o := r.o
	if h.Level == 0 {
		prio := cint.Int(h.Prio)
		o.int(printAny, "prio", "prio "+strconv.Itoa(int(prio))+" ", prio)
		if r.details > 0 {
			q := cint.Int(h.Quantum)
			o.int(printAny, "quantum", "quantum "+strconv.Itoa(int(q))+" ", q)
		}
	}
	rate64 := uint64(h.Rate.Rate)
	if v, ok := tb.U64(xtcpnl.TcaHtbRate64); ok {
		rate64 = v
	}
	ceil64 := uint64(h.Ceil.Rate)
	if v, ok := tb.U64(xtcpnl.TcaHtbCeil64); ok {
		ceil64 = v
	}
	o.rate(r.iec > 0, printAny, "rate", "rate ", " ", rate64)
	if h.Rate.Overhead != 0 {
		ov := uint32(h.Rate.Overhead)
		o.uint(printAny, "overhead", "overhead "+u32s(ov)+" ", ov)
	}
	buffer := r.xmitsize(rate64, h.Buffer)
	o.rate(r.iec > 0, printAny, "ceil", "ceil ", " ", ceil64)
	cbuffer := r.xmitsize(ceil64, h.Cbuffer)
	ll := h.Rate.Linklayer & tcLinklayerMask
	if ll > 1 || r.details > 0 {
		o.str(printAny, colorNone, "linklayer", "linklayer "+sprintLinklayer(ll)+" ", sprintLinklayer(ll))
	}
	if r.details > 0 {
		o.size(printAny, "burst", "burst ", "/", buffer)
		c := cellOf(h.Rate.CellLog)
		o.uint(printAny, "burst_cell", u32s(c), c)
		o.size(printAny, "mpu_rate", "mpu ", " ", uint64(h.Rate.Mpu))
		o.size(printAny, "cburst", "cburst ", "/", cbuffer)
		c = cellOf(h.Ceil.CellLog)
		o.uint(printAny, "cburst_cell", u32s(c), c)
		o.size(printAny, "mpu_ceil", "mpu ", " ", uint64(h.Ceil.Mpu))
		lvl := cint.Int(h.Level)
		o.int(printAny, "level", "level "+strconv.Itoa(int(lvl))+" ", lvl)
	} else {
		o.size(printAny, "burst", "burst ", " ", buffer)
		o.size(printAny, "cburst", "cburst ", "", cbuffer)
	}
	if r.raw > 0 {
		o.raw(fmt.Sprintf("buffer [%08x] cbuffer [%08x] ", h.Buffer, h.Cbuffer))
	}
}

// printHtbXstats is htb_print_xstats: nothing for a short payload.
func printHtbXstats(r *runner, xs []byte) {
	st, err := xtcpnl.DecodeTcHtbXstats(xs)
	if err != nil {
		return
	}
	o := r.o
	o.uint(printAny, "lended", " lended: "+u32s(st.Lends)+" ", st.Lends)
	o.uint(printAny, "borrowed", "borrowed: "+u32s(st.Borrows)+" ", st.Borrows)
	o.uint(printAny, "giants", "giants: "+u32s(st.Giants), st.Giants)
	o.nl()
	o.int(printAny, "tokens", " tokens: "+strconv.Itoa(int(st.Tokens))+" ", st.Tokens)
	o.int(printAny, "ctokens", "ctokens: "+strconv.Itoa(int(st.Ctokens)), st.Ctokens)
	o.nl()
}

// printTbfOpt is tbf_print_opt. Under -d its burst and mtu are strings
// ("1514b/1"), else sizes (Q3); with a peakrate under -d, "mpu" goes out
// twice (Q4); the peakrate itself is text only (Q5); the latency, a
// double, is printed through sprint_time's __u32 in text and as a u64 in
// JSON (Q6).
func printTbfOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	tb := r.nested(opt, xtcpnl.TcaTbfMaxKnown)
	if !tb.Has(xtcpnl.TcaTbfParms) {
		return
	}
	q, err := xtcpnl.DecodeTcTbfQopt(tb.Bytes(xtcpnl.TcaTbfParms))
	if err != nil {
		return
	}
	o := r.o
	rate64 := uint64(q.Rate.Rate)
	if v, ok := tb.U64(xtcpnl.TcaTbfRate64); ok {
		rate64 = v
	}
	o.rate(r.iec > 0, printAny, "rate", "rate ", " ", rate64)
	buffer := r.xmitsize(rate64, q.Buffer)
	if r.details > 0 {
		b := sprintSize(buffer) + "/" + u32s(cellOf(q.Rate.CellLog))
		o.str(printAny, colorNone, "burst", "burst "+b+" ", b)
		o.size(printAny, "mpu", "mpu ", " ", uint64(q.Rate.Mpu))
	} else {
		o.size(printAny, "burst", "burst ", " ", buffer)
	}
	if r.raw > 0 {
		o.hex(printAny, "burst_raw", fmt.Sprintf("[%08x] ", q.Buffer), q.Buffer)
	}
	prate64 := uint64(q.Peakrate.Rate)
	if v, ok := tb.U64(xtcpnl.TcaTbfPrate64); ok {
		prate64 = v
	}
	if prate64 != 0 {
		r.tbfPeak(&q, prate64)
	}
	latency := timeUnitsPerSec*(float64(q.Limit)/float64(rate64)) - r.core.tick2time(float64(q.Buffer))
	if prate64 != 0 {
		lat2 := timeUnitsPerSec*(float64(q.Limit)/float64(prate64)) - r.core.tick2time(float64(q.Mtu))
		if lat2 > latency {
			latency = lat2
		}
	}
	if latency >= 0 {
		o.u64(printJSON, "lat", "", cDoubleToU64(latency))
		o.fp("lat " + sprintTime(cDoubleToU32(latency)) + " ")
	}
	if r.raw > 0 || latency < 0 {
		o.size(printAny, "limit", "limit ", " ", uint64(q.Limit))
	}
	if q.Rate.Overhead != 0 {
		ov := int32(q.Rate.Overhead)
		o.int(printAny, "overhead", "overhead "+strconv.Itoa(int(ov))+" ", ov)
	}
	ll := q.Rate.Linklayer & tcLinklayerMask
	if ll > 1 || r.details > 0 {
		o.str(printAny, colorNone, "linklayer", "linklayer "+sprintLinklayer(ll)+" ", sprintLinklayer(ll))
	}
}

func (r *runner) tbfPeak(q *xtcpnl.TcTbfQopt, prate64 uint64) {
	o := r.o
	o.rate(r.iec > 0, printFP, "peakrate", "peakrate ", " ", prate64)
	if q.Mtu == 0 && q.Peakrate.Mpu == 0 {
		return
	}
	mtu := r.xmitsize(prate64, q.Mtu)
	if r.details > 0 {
		b := sprintSize(mtu) + "/" + u32s(cellOf(q.Peakrate.CellLog))
		o.str(printAny, colorNone, "mtu", "mtu "+b+" ", b)
		o.size(printAny, "mpu", "mpu ", " ", uint64(q.Peakrate.Mpu))
	} else {
		o.size(printAny, "minburst", "minburst ", " ", mtu)
	}
	if r.raw > 0 {
		o.hex(printAny, "mtu_raw", fmt.Sprintf("[%08x] ", q.Mtu), q.Mtu)
	}
}

// printSfqOpt is sfq_print_opt.
func printSfqOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	q, err := xtcpnl.DecodeTcSfqQopt(opt)
	if err != nil {
		return
	}
	var ext *xtcpnl.TcSfqQoptV1
	if v, err := xtcpnl.DecodeTcSfqQoptV1(opt); err == nil {
		ext = &v
	}
	o := r.o
	o.uint(printAny, "limit", "limit "+u32s(q.Limit)+"p ", q.Limit)
	o.size(printAny, "quantum", "quantum ", " ", uint64(q.Quantum))
	if ext != nil && ext.Depth != 0 {
		o.uint(printAny, "depth", "depth "+u32s(ext.Depth)+" ", ext.Depth)
	}
	if ext != nil && ext.Headdrop != 0 {
		o.boolean(printAny, "headdrop", "headdrop ", true)
	}
	if r.details > 0 {
		o.uint(printAny, "flows", "flows "+u32s(q.Flows)+" ", q.Flows)
	}
	o.uint(printAny, "divisor", "divisor "+u32s(q.Divisor)+" ", q.Divisor)
	if q.PerturbPeriod != 0 {
		o.int(printAny, "perturb", "perturb "+strconv.Itoa(int(q.PerturbPeriod))+"sec ", q.PerturbPeriod)
	}
	if ext != nil && ext.QthMin != 0 {
		r.sfqRed(ext)
	}
}

func (r *runner) sfqRed(ext *xtcpnl.TcSfqQoptV1) {
	o := r.o
	o.uint(printAny, "ewma", "ewma "+u32s(uint32(ext.Wlog))+" ", uint32(ext.Wlog))
	o.size(printAny, "min", "min ", " ", uint64(ext.QthMin))
	o.size(printAny, "max", "max ", " ", uint64(ext.QthMax))
	p := float64(ext.MaxP) / math.Pow(2, 32)
	o.float(printAny, "probability", "probability "+cG(p)+" ", p)
	printRedFlags(o, uint32(ext.Flags))
	if r.stats == 0 {
		return
	}
	s := ext.Stats
	o.nl()
	o.uint(printAny, "prob_mark", "  prob_mark "+u32s(s.ProbMark), s.ProbMark)
	o.uint(printAny, "prob_mark_head", " prob_mark_head "+u32s(s.ProbMarkHead), s.ProbMarkHead)
	o.uint(printAny, "prob_drop", " prob_drop "+u32s(s.ProbDrop), s.ProbDrop)
	o.nl()
	o.uint(printAny, "forced_mark", "  forced_mark "+u32s(s.ForcedMark), s.ForcedMark)
	o.uint(printAny, "forced_mark_head", " forced_mark_head "+u32s(s.ForcedMarkHead), s.ForcedMarkHead)
	o.uint(printAny, "forced_drop", " forced_drop "+u32s(s.ForcedDrop), s.ForcedDrop)
}

// printSfqXstats is sfq_print_xstats: nothing for a short payload.
func printSfqXstats(r *runner, xs []byte) {
	st, err := xtcpnl.DecodeTcSfqXstats(xs)
	if err != nil {
		return
	}
	r.o.int(printAny, "allot", "  allot "+strconv.Itoa(int(st.Allot)), st.Allot)
}
