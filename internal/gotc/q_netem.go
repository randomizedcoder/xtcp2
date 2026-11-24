package gotc

import (
	"math"
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/q_netem.c netem_print_opt and netem_print_xstats.

// netemOpt is what netem_print_opt collects before it prints.
type netemOpt struct {
	q                    xtcpnl.TcNetemQopt
	cor                  *xtcpnl.TcNetemCorr
	reorder, corrupt     *xtcpnl.TcNetemProbCorr
	gi                   *xtcpnl.TcNetemGimodel
	ge                   *xtcpnl.TcNetemGemodel
	rate                 *xtcpnl.TcNetemRate
	slot                 *xtcpnl.TcNetemSlot
	ecn                  bool // the pointer, not its value (Q9)
	rate64, lat64, jit64 uint64
	seed                 uint64
	seedPresent          bool
}

// printNetemOpt is netem_print_opt. Its limit is "%d" of a u32 in text
// (Q7); ecn is the attribute's presence (Q9); LATENCY64 and JITTER64 are
// read as u64 (the kernel's s64, Q10).
func printNetemOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	n, ok := r.netemRead(opt)
	if !ok {
		return
	}
	o := r.o
	o.uint(printAny, "limit", "limit "+strconv.Itoa(int(cint.Int(n.q.Limit))), n.q.Limit)
	if n.lat64 != 0 {
		o.openObject("delay")
		if o.isJSON() {
			o.float(printJSON, "delay", "", float64(n.lat64)/1e9)
			o.float(printJSON, "jitter", "", float64(n.jit64)/1e9)
		} else {
			o.fp(" delay " + sprintTime64(cint.LongLong(n.lat64)))
			if n.jit64 != 0 {
				o.fp("  " + sprintTime64(cint.LongLong(n.jit64)))
			}
		}
		var dc uint32
		if n.cor != nil {
			dc = n.cor.DelayCorr
		}
		r.netemCorr(n.jit64 != 0 && dc != 0, dc)
		o.closeObject()
	}
	r.netemLoss(&n)
	if n.q.Duplicate != 0 {
		o.openObject("duplicate")
		r.percent("duplicate", " duplicate", n.q.Duplicate)
		var c uint32
		if n.cor != nil {
			c = n.cor.DupCorr
		}
		r.netemCorr(c != 0, c)
		o.closeObject()
	}
	for _, pc := range []struct {
		name string
		v    *xtcpnl.TcNetemProbCorr
	}{{"reorder", n.reorder}, {"corrupt", n.corrupt}} {
		if pc.v != nil && pc.v.Probability != 0 {
			o.openObject(pc.name)
			r.percent(pc.name, " "+pc.name, pc.v.Probability)
			r.netemCorr(pc.v.Correlation != 0, pc.v.Correlation)
			o.closeObject()
		}
	}
	r.netemRateSlot(&n)
	if n.seedPresent {
		o.u64(printAny, "seed", " seed "+u64s(n.seed), n.seed)
	}
	o.boolean(printJSON, "ecn", "", n.ecn)
	if n.ecn {
		o.boolean(printFP, "", " ecn ", true)
	}
	o.u64(printJSON, "gap", "", uint64(n.q.Gap))
	if n.q.Gap != 0 {
		o.fp(" gap " + u32s(n.q.Gap))
	}
}

// netemRead is netem_print_opt's reads: false where it returns -1.
func (r *runner) netemRead(opt []byte) (netemOpt, bool) {
	var n netemOpt
	q, err := xtcpnl.DecodeTcNetemQopt(opt)
	if err != nil {
		r.errf("options size error\n")
		return n, false
	}
	n.q = q
	if len(opt) <= xtcpnl.TcNetemQoptSizeCst {
		return n, true
	}
	tb := r.attrTable(opt[xtcpnl.TcNetemQoptSizeCst:], xtcpnl.TcaNetemMaxKnown, 0)
	if !netemStructs(tb, &n) || !netemWords(tb, &n) {
		return n, false
	}
	if tb.Has(xtcpnl.TcaNetemLoss) {
		lb := r.nested(tb.Bytes(xtcpnl.TcaNetemLoss), xtcpnl.NetemLossMaxKnown)
		// tc casts these with no length check (Q8); a short one is not
		// printed here.
		if g, err := xtcpnl.DecodeTcNetemGimodel(lb.Bytes(xtcpnl.NetemLossGI)); err == nil {
			n.gi = &g
		}
		if g, err := xtcpnl.DecodeTcNetemGemodel(lb.Bytes(xtcpnl.NetemLossGE)); err == nil {
			n.ge = &g
		}
	}
	return n, true
}

func netemStructs(tb xtcpnl.AttrTable, n *netemOpt) bool {
	if tb.Has(xtcpnl.TcaNetemCorr) {
		c, err := xtcpnl.DecodeTcNetemCorr(tb.Bytes(xtcpnl.TcaNetemCorr))
		if err != nil {
			return false
		}
		n.cor = &c
	}
	for _, pc := range []struct {
		t   uint16
		dst **xtcpnl.TcNetemProbCorr
	}{{xtcpnl.TcaNetemReorder, &n.reorder}, {xtcpnl.TcaNetemCorrupt, &n.corrupt}} {
		if tb.Has(pc.t) {
			v, err := xtcpnl.DecodeTcNetemProbCorr(tb.Bytes(pc.t))
			if err != nil {
				return false
			}
			*pc.dst = &v
		}
	}
	if tb.Has(xtcpnl.TcaNetemRate) {
		v, err := xtcpnl.DecodeTcNetemRate(tb.Bytes(xtcpnl.TcaNetemRate))
		if err != nil {
			return false
		}
		n.rate = &v
	}
	if tb.Has(xtcpnl.TcaNetemSlot) {
		v, err := xtcpnl.DecodeTcNetemSlot(tb.Bytes(xtcpnl.TcaNetemSlot))
		if err != nil {
			return false
		}
		n.slot = &v
	}
	return true
}

// netemWords reads ECN and the u64s in tc's order, each with its
// length check.
func netemWords(tb xtcpnl.AttrTable, n *netemOpt) bool {
	if tb.Has(xtcpnl.TcaNetemEcn) {
		if len(tb.Bytes(xtcpnl.TcaNetemEcn)) < 4 {
			return false
		}
		n.ecn = true
	}
	for _, w := range []struct {
		t   uint16
		dst *uint64
	}{
		{xtcpnl.TcaNetemRate64, &n.rate64}, {xtcpnl.TcaNetemLatency64, &n.lat64},
		{xtcpnl.TcaNetemJitter64, &n.jit64}, {xtcpnl.TcaNetemPrngSeed, &n.seed},
	} {
		if !tb.Has(w.t) {
			continue
		}
		v, ok := tb.U64(w.t)
		if !ok {
			return false
		}
		*w.dst = v
	}
	n.seedPresent = tb.Has(xtcpnl.TcaNetemPrngSeed)
	return true
}

func (r *runner) netemLoss(n *netemOpt) {
	o := r.o
	if n.q.Loss != 0 {
		o.openObject("loss-random")
		r.percent("loss", " loss", n.q.Loss)
		var c uint32
		if n.cor != nil {
			c = n.cor.LossCorr
		}
		r.netemCorr(c != 0, c)
		o.closeObject()
	}
	if g := n.gi; g != nil {
		o.openObject("loss-state")
		r.percent("p13", " loss state p13", g.P13)
		r.percent("p31", " p31", g.P31)
		r.percent("p32", " p32", g.P32)
		r.percent("p23", " p23", g.P23)
		r.percent("p14", " p14", g.P14)
		o.closeObject()
	}
	if g := n.ge; g != nil {
		o.openObject("loss-gemodel")
		r.percent("p", " loss gemodel p", g.P)
		r.percent("r", " r", g.R)
		r.percent("1-h", " 1-h", math.MaxUint32-g.H)
		r.percent("1-k", " 1-k", g.K1)
		o.closeObject()
	}
}

func (r *runner) netemRateSlot(n *netemOpt) {
	o := r.o
	if rt := n.rate; rt != nil && rt.Rate != 0 {
		o.openObject("rate")
		rate64 := n.rate64
		if rate64 == 0 {
			rate64 = uint64(rt.Rate)
		}
		o.rate(r.iec > 0, printAny, "rate", " rate ", "", rate64)
		r.intOpt("packetoverhead", rt.PacketOverhead)
		o.uint(printJSON, "cellsize", "", rt.CellSize)
		if rt.CellSize != 0 {
			o.fp(" cellsize " + u32s(rt.CellSize))
		}
		r.intOpt("celloverhead", rt.CellOverhead)
		o.closeObject()
	}
	if s := n.slot; s != nil {
		o.openObject("slot")
		if s.DistJitter > 0 {
			r.time64("distribution", " slot distribution", cint.U64Bits(s.DistDelay))
			r.time64("jitter", "", cint.U64Bits(s.DistJitter))
		} else {
			r.time64("min-delay", " slot", cint.U64Bits(s.MinDelay))
			r.time64("max-delay", "", cint.U64Bits(s.MaxDelay))
		}
		r.intOpt("packets", s.MaxPackets)
		r.intOpt("bytes", s.MaxBytes)
		o.closeObject()
	}
}

// netemCorr is print_corr: in text the bare percent when present; in JSON
// always "correlation".
func (r *runner) netemCorr(present bool, v uint32) {
	if !r.o.isJSON() {
		if present {
			r.o.fp(" " + cG(100*float64(v)/math.MaxUint32) + "%")
		}
		return
	}
	r.percent("correlation", " correlation", v)
}

// percent is __print_percent: percentage points in text, the fraction in
// JSON.
func (r *runner) percent(key, label string, per uint32) {
	r.o.fp(label + " " + cG(100*float64(per)/math.MaxUint32) + "%")
	r.o.float(printJSON, key, "", float64(per)/math.MaxUint32)
}

// intOpt is __print_int_opt: always in JSON, in text when non-zero.
func (r *runner) intOpt(key string, v int32) {
	r.o.int(printJSON, key, "", v)
	if v != 0 {
		r.o.fp(" " + key + " " + strconv.Itoa(int(v)))
	}
}

// time64 is __print_time64 of a __u64: sprint_time64 reads its bits as
// __s64; JSON is the seconds of the u64.
func (r *runner) time64(key, label string, v uint64) {
	r.o.fp(label + " " + sprintTime64(cint.LongLong(v)))
	r.o.float(printJSON, key, "", float64(v)/1e9)
}

// printNetemXstats is netem_print_xstats: the MIN-copy.
func printNetemXstats(r *runner, xs []byte) {
	st := xtcpnl.DecodeTcNetemXstats(xs)
	o := r.o
	for _, f := range []struct {
		key, label string
		v          uint64
	}{
		{"delayed", "  delayed ", st.Delayed}, {"dropped", " dropped ", st.Dropped},
		{"corrupted", " corrupted ", st.Corrupted}, {"duplicated", " duplicated ", st.Duplicated},
		{"reordered", " reordered ", st.Reordered}, {"ecn_marked", " ecn_marked ", st.EcnMarked},
	} {
		o.u64(printAny, f.key, f.label+u64s(f.v), f.v)
	}
	if st.AllocationErrors != 0 {
		o.u64(printAny, "alloc_errors", " alloc_errors "+u64s(st.AllocationErrors), st.AllocationErrors)
	}
}
