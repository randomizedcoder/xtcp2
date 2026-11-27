package gotc

import (
	"encoding/binary"
	"math"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// The flat AQM printers: tc/q_sfb.c, q_codel.c, q_pie.c, q_fq_pie.c,
// q_hhf.c, q_plug.c and q_skbprio.c. Every option read is length-checked,
// so none refuses.

// The option names more than one AQM printer uses, text and JSON alike.
const (
	wordLimit  = "limit"
	wordTarget = "target"
	wordEcn    = "ecn"
	wordAlpha  = "alpha"
	wordBeta   = "beta"
)

// sfbMaxProb is q_sfb.c's SFB_MAX_PROB.
const sfbMaxProb = 0xFFFF

// aqmField is one flat option: a u32 attribute printed when it holds one.
type aqmField struct {
	t    uint16
	key  string
	kind aqmFieldKind
}

type aqmFieldKind uint8

const (
	aqmPkts aqmFieldKind = iota // "key %up "
	aqmUint                     // "key %u "
	aqmTime                     // JSON uint, text "key %s " of sprint_time
	aqmSize                     // print_size "key %s "
	aqmFlag                     // print_bool "key " when nonzero
)

// printAqmFields prints fs in order, each when its attribute holds a u32.
func (r *runner) printAqmFields(tb xtcpnl.AttrTable, fs []aqmField) {
	o := r.o
	for _, f := range fs {
		v, ok := tb.U32(f.t)
		if !ok {
			continue
		}
		switch f.kind {
		case aqmPkts:
			o.uint(printAny, f.key, f.key+" "+u32s(v)+"p ", v)
		case aqmUint:
			o.uint(printAny, f.key, f.key+" "+u32s(v)+" ", v)
		case aqmTime:
			o.uint(printJSON, f.key, "", v)
			o.fp(f.key + " " + sprintTime(v) + " ")
		case aqmSize:
			o.size(printAny, f.key, f.key+" ", " ", uint64(v))
		case aqmFlag:
			if v != 0 {
				o.boolean(printAny, f.key, f.key+" ", true)
			}
		}
	}
}

// aqmXstats is the kind's xstats decoded, ok false where tc's printer
// returns -1 on a short payload.
func aqmXstats(kind string, xs []byte) (xtcpnl.TcAqmXstats, bool) {
	x, err := xtcpnl.ParseTcXstats(kind, xs)
	return x.Aqm, err == nil
}

// printSfbOpt is sfb_print_opt. rehash and db are u32 milliseconds times
// 1000 in u32, which wraps above 4294967 ms (Q73).
func printSfbOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	tb := r.nested(opt, xtcpnl.TcaSfbMaxKnown)
	q, err := xtcpnl.DecodeTcSfbQopt(tb.Bytes(xtcpnl.TcaSfbParms))
	if err != nil {
		return
	}
	o := r.o
	rehash, db := q.RehashInterval*1000, q.WarmupTime*1000
	o.uint(printJSON, "rehash", "", rehash)
	o.fp("rehash " + sprintTime(rehash) + " ")
	o.uint(printJSON, "db", "", db)
	o.fp("db " + sprintTime(db) + " ")
	o.uint(printAny, "limit", "limit "+u32s(q.Limit)+"p ", q.Limit)
	o.uint(printAny, "max", "max "+u32s(q.Max)+"p ", q.Max)
	o.uint(printAny, "target", "target "+u32s(q.BinSize)+"p ", q.BinSize)
	inc, dec := float64(q.Increment)/sfbMaxProb, float64(q.Decrement)/sfbMaxProb
	o.float(printAny, "increment", "increment "+cG(inc)+" ", inc)
	o.float(printAny, "decrement", "decrement "+cG(dec)+" ", dec)
	o.uint(printAny, "penalty_rate", "penalty_rate "+u32s(q.PenaltyRate)+"pps ", q.PenaltyRate)
	o.uint(printAny, "penalty_burst", "penalty_burst "+u32s(q.PenaltyBurst)+"p ", q.PenaltyBurst)
}

// printSfbXstats is sfb_print_xstats.
func printSfbXstats(r *runner, xs []byte) {
	a, ok := aqmXstats(xtcpnl.TcKindSfb, xs)
	if !ok {
		return
	}
	st := a.Sfb
	o := r.o
	o.uint(printAny, "earlydrop", "  earlydrop "+u32s(st.Earlydrop), st.Earlydrop)
	o.uint(printAny, "penaltydrop", " penaltydrop "+u32s(st.Penaltydrop), st.Penaltydrop)
	o.uint(printAny, "bucketdrop", " bucketdrop "+u32s(st.Bucketdrop), st.Bucketdrop)
	o.uint(printAny, "queuedrop", " queuedrop "+u32s(st.Queuedrop), st.Queuedrop)
	o.uint(printAny, "childdrop", " childdrop "+u32s(st.Childdrop), st.Childdrop)
	o.uint(printAny, "marked", " marked "+u32s(st.Marked), st.Marked)
	o.nl()
	o.uint(printAny, "maxqlen", "  maxqlen "+u32s(st.Maxqlen), st.Maxqlen)
	mx, av := float64(st.Maxprob)/sfbMaxProb, float64(st.Avgprob)/sfbMaxProb
	o.float(printAny, "maxprob", " maxprob "+cG(mx), mx)
	o.float(printAny, "avgprob", " avgprob "+cG(av), av)
}

var codelFields = []aqmField{
	{xtcpnl.TcaCodelLimit, wordLimit, aqmPkts},
	{xtcpnl.TcaCodelTarget, wordTarget, aqmTime},
	{xtcpnl.TcaCodelCeThreshold, "ce_threshold", aqmTime},
	{xtcpnl.TcaCodelInterval, "interval", aqmTime},
	{xtcpnl.TcaCodelEcn, wordEcn, aqmFlag},
}

// printCodelOpt is codel_print_opt.
func printCodelOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if has {
		r.printAqmFields(r.nested(opt, xtcpnl.TcaCodelMaxKnown), codelFields)
	}
}

// printCodelXstats is codel_print_xstats: the MIN-copy, so it never
// stops. -drop_next of INT_MIN is INT_MIN again, whose u32 sprint_time
// prints (Q74).
func printCodelXstats(r *runner, xs []byte) {
	a, _ := aqmXstats(xtcpnl.TcKindCodel, xs)
	st := a.Codel
	o := r.o
	o.uint(printAny, "count", "  count "+u32s(st.Count), st.Count)
	o.uint(printAny, "lastcount", " lastcount "+u32s(st.Lastcount), st.Lastcount)
	o.uint(printJSON, "ldelay", "", st.Ldelay)
	o.fp(" ldelay " + sprintTime(st.Ldelay))
	if st.Dropping != 0 {
		o.boolean(printAny, "dropping", " dropping", true)
	}
	o.int(printJSON, "drop_next", "", st.DropNext)
	if st.DropNext < 0 {
		o.fp(" drop_next -" + sprintTime(cint.Bits(-st.DropNext)))
	} else {
		o.fp(" drop_next " + sprintTime(cint.Bits(st.DropNext)))
	}
	o.nl()
	o.uint(printAny, "maxpacket", "  maxpacket "+u32s(st.Maxpacket), st.Maxpacket)
	o.uint(printAny, "ecn_mark", " ecn_mark "+u32s(st.EcnMark), st.EcnMark)
	o.uint(printAny, "drop_overlimit", " drop_overlimit "+u32s(st.DropOverlimit), st.DropOverlimit)
	if st.CeMark != 0 {
		o.uint(printAny, "ce_mark", " ce_mark "+u32s(st.CeMark), st.CeMark)
	}
}

var pieFields = []aqmField{
	{xtcpnl.TcaPieLimit, wordLimit, aqmPkts},
	{xtcpnl.TcaPieTarget, wordTarget, aqmTime},
	{xtcpnl.TcaPieTupdate, "tupdate", aqmTime},
	{xtcpnl.TcaPieAlpha, wordAlpha, aqmUint},
	{xtcpnl.TcaPieBeta, wordBeta, aqmUint},
	{xtcpnl.TcaPieEcn, wordEcn, aqmFlag},
	{xtcpnl.TcaPieBytemode, "bytemode", aqmFlag},
	{xtcpnl.TcaPieDqRateEstimator, "dq_rate_estimator", aqmFlag},
}

// printPieOpt is pie_print_opt.
func printPieOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if has {
		r.printAqmFields(r.nested(opt, xtcpnl.TcaPieMaxKnown), pieFields)
	}
}

// printPieXstats is pie_print_xstats: prob as a fraction of UINT64_MAX.
func printPieXstats(r *runner, xs []byte) {
	a, ok := aqmXstats(xtcpnl.TcKindPie, xs)
	if !ok {
		return
	}
	st := a.Pie
	o := r.o
	p := float64(st.Prob) / float64(uint64(math.MaxUint64))
	o.float(printAny, "prob", "  prob "+cG(p), p)
	o.uint(printJSON, "delay", "", st.Delay)
	o.fp(" delay " + sprintTime(st.Delay))
	if st.DqRateEstimating != 0 {
		o.uint(printAny, "avg_dq_rate", " avg_dq_rate "+u32s(st.AvgDqRate), st.AvgDqRate)
	}
	o.nl()
	o.uint(printAny, "pkts_in", "  pkts_in "+u32s(st.PacketsIn), st.PacketsIn)
	o.uint(printAny, "overlimit", " overlimit "+u32s(st.Overlimit), st.Overlimit)
	o.uint(printAny, "dropped", " dropped "+u32s(st.Dropped), st.Dropped)
	o.uint(printAny, "maxq", " maxq "+u32s(st.Maxq), st.Maxq)
	o.uint(printAny, "ecn_mark", " ecn_mark "+u32s(st.EcnMark), st.EcnMark)
}

var fqPieFields = []aqmField{
	{xtcpnl.TcaFqPieLimit, wordLimit, aqmPkts},
	{xtcpnl.TcaFqPieFlows, "flows", aqmUint},
	{xtcpnl.TcaFqPieTarget, wordTarget, aqmTime},
	{xtcpnl.TcaFqPieTupdate, "tupdate", aqmTime},
	{xtcpnl.TcaFqPieAlpha, wordAlpha, aqmUint},
	{xtcpnl.TcaFqPieBeta, wordBeta, aqmUint},
	{xtcpnl.TcaFqPieQuantum, "quantum", aqmSize},
	{xtcpnl.TcaFqPieMemoryLimit, "memory_limit", aqmSize},
	{xtcpnl.TcaFqPieEcnProb, "ecn_prob", aqmUint},
	{xtcpnl.TcaFqPieEcn, wordEcn, aqmFlag},
	{xtcpnl.TcaFqPieBytemode, "bytemode", aqmFlag},
	{xtcpnl.TcaFqPieDqRateEstimator, "dq_rate_estimator", aqmFlag},
}

// printFqPieOpt is fq_pie_print_opt.
func printFqPieOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if has {
		r.printAqmFields(r.nested(opt, xtcpnl.TcaFqPieMaxKnown), fqPieFields)
	}
}

// printFqPieXstats is fq_pie_print_xstats: the MIN-copy, so it never
// stops.
func printFqPieXstats(r *runner, xs []byte) {
	a, _ := aqmXstats(xtcpnl.TcKindFqPie, xs)
	st := a.FqPie
	o := r.o
	o.uint(printAny, "pkts_in", "  pkts_in "+u32s(st.PacketsIn), st.PacketsIn)
	o.uint(printAny, "overlimit", " overlimit "+u32s(st.Overlimit), st.Overlimit)
	o.uint(printAny, "overmemory", " overmemory "+u32s(st.Overmemory), st.Overmemory)
	o.uint(printAny, "dropped", " dropped "+u32s(st.Dropped), st.Dropped)
	o.uint(printAny, "ecn_mark", " ecn_mark "+u32s(st.EcnMark), st.EcnMark)
	o.nl()
	o.uint(printAny, "new_flow_count", "  new_flow_count "+u32s(st.NewFlowCount), st.NewFlowCount)
	o.uint(printAny, "new_flows_len", " new_flows_len "+u32s(st.NewFlowsLen), st.NewFlowsLen)
	o.uint(printAny, "old_flows_len", " old_flows_len "+u32s(st.OldFlowsLen), st.OldFlowsLen)
	o.uint(printAny, "memory_used", " memory_used "+u32s(st.MemoryUsage), st.MemoryUsage)
}

var hhfFields = []aqmField{
	{xtcpnl.TcaHhfBacklogLimit, wordLimit, aqmPkts},
	{xtcpnl.TcaHhfQuantum, "quantum", aqmSize},
	{xtcpnl.TcaHhfHhFlowsLimit, "hh_limit", aqmUint},
	{xtcpnl.TcaHhfResetTimeout, "reset_timeout", aqmTime},
	{xtcpnl.TcaHhfAdmitBytes, "admit_bytes", aqmSize},
	{xtcpnl.TcaHhfEvictTimeout, "evict_timeout", aqmTime},
	{xtcpnl.TcaHhfNonHhWeight, "non_hh_weight", aqmUint},
}

// printHhfOpt is hhf_print_opt.
func printHhfOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if has {
		r.printAqmFields(r.nested(opt, xtcpnl.TcaHhfMaxKnown), hhfFields)
	}
}

// printHhfXstats is hhf_print_xstats.
func printHhfXstats(r *runner, xs []byte) {
	a, ok := aqmXstats(xtcpnl.TcKindHhf, xs)
	if !ok {
		return
	}
	st := a.Hhf
	o := r.o
	o.uint(printAny, "drop_overlimit", "  drop_overlimit "+u32s(st.DropOverlimit), st.DropOverlimit)
	o.uint(printAny, "hh_overlimit", " hh_overlimit "+u32s(st.HhOverlimit), st.HhOverlimit)
	o.uint(printAny, "tot_hh", " tot_hh "+u32s(st.HhTotCount), st.HhTotCount)
	o.uint(printAny, "cur_hh", " cur_hh "+u32s(st.HhCurCount), st.HhCurCount)
}

// printPlugOpt is plug_print_opt: sch_plug has no dump (K41).
func printPlugOpt(*runner, *qdiscUtil, []byte, bool) {}

// printSkbprioOpt is skbprio_print_opt: the options are the bare struct
// (K42).
func printSkbprioOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has || len(opt) < xtcpnl.TcSkbprioQoptSizeCst {
		return
	}
	l := binary.LittleEndian.Uint32(opt)
	r.o.uint(printAny, "limit", "limit "+u32s(l)+" ", l)
}
