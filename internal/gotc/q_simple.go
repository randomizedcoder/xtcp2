package gotc

import (
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// The small printers: tc/q_prio.c prio_print_opt (pfifo_fast's too),
// tc/q_fifo.c fifo_print_opt, tc/q_ingress.c ingress_print_opt and
// tc/q_fq_codel.c.

// printPrioOpt is prio_print_opt: parse_rtattr_nested_compat, then bands
// and the priomap, and TCA_PRIO_MQ read with rta_getattr_u8 whatever its
// length (Q27; a zero-length one reads as 0 here).
func printPrioOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	hdr, nest, err := xtcpnl.SplitTcCompat(opt, xtcpnl.TcPrioQoptSizeCst)
	if err != nil {
		return
	}
	q, err := xtcpnl.DecodeTcPrioQopt(hdr)
	if err != nil {
		return
	}
	var tb xtcpnl.AttrTable
	if nest != nil {
		tb = r.nested(nest, xtcpnl.TcaPrioMaxKnown)
	}
	o := r.o
	bands := cint.Bits(q.Bands)
	o.uint(printAny, "bands", "bands "+u32s(bands)+" ", bands)
	o.openArray(printAny, "priomap")
	for _, b := range q.Priomap {
		o.uint(printAny, "", " "+strconv.Itoa(int(b)), uint32(b))
	}
	o.closeArray(printAny, "")
	mq, _ := tb.U8(xtcpnl.TcaPrioMQ)
	if tb.Has(xtcpnl.TcaPrioMQ) {
		word := "off"
		if mq != 0 {
			word = "on"
		}
		o.fp(" multiqueue: " + word + " ")
	}
	o.boolean(printJSON, "multiqueue", "", tb.Has(xtcpnl.TcaPrioMQ) && mq != 0)
}

// printFifoOpt is fifo_print_opt: bfifo's limit is a size, the others'
// packets.
func printFifoOpt(r *runner, q *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	f, err := xtcpnl.DecodeTcFifoQopt(opt)
	if err != nil {
		return
	}
	if q.id == xtcpnl.TcKindBfifo {
		r.o.size(printAny, "limit", "limit ", "", uint64(f.Limit))
		return
	}
	r.o.uint(printAny, "limit", "limit "+u32s(f.Limit)+"p", f.Limit)
}

// printIngressOpt is ingress_print_opt.
func printIngressOpt(r *runner, _ *qdiscUtil, _ []byte, _ bool) {
	r.o.fp("---------------- ")
}

// printFqCodelOpt is fq_codel_print_opt.
func printFqCodelOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	tb := r.nested(opt, xtcpnl.TcaFqCodelMaxKnown)
	o := r.o
	if v, ok := tb.U32(xtcpnl.TcaFqCodelLimit); ok {
		o.uint(printAny, "limit", "limit "+u32s(v)+"p ", v)
	}
	if v, ok := tb.U32(xtcpnl.TcaFqCodelFlows); ok {
		o.uint(printAny, "flows", "flows "+u32s(v)+" ", v)
	}
	if v, ok := tb.U32(xtcpnl.TcaFqCodelQuantum); ok {
		o.uint(printAny, "quantum", "quantum "+u32s(v)+" ", v)
	}
	r.jsonTime(tb, xtcpnl.TcaFqCodelTarget, "target")
	r.jsonTime(tb, xtcpnl.TcaFqCodelCeThreshold, "ce_threshold")
	sel, _ := tb.U8(xtcpnl.TcaFqCodelCeThresholdSelector)
	mask, _ := tb.U8(xtcpnl.TcaFqCodelCeThresholdMask)
	if sel != 0 || mask != 0 {
		o.hhu(printAny, "ce_threshold_selector", "ce_threshold_selector "+cHashX(uint64(sel)), sel)
		o.hhu(printAny, "ce_threshold_mask", "/"+cHashX(uint64(mask))+" ", mask)
	}
	r.jsonTime(tb, xtcpnl.TcaFqCodelInterval, "interval")
	if v, ok := tb.U32(xtcpnl.TcaFqCodelMemoryLimit); ok {
		o.size(printAny, "memory_limit", "memory_limit ", " ", uint64(v))
	}
	if v, ok := tb.U32(xtcpnl.TcaFqCodelEcn); ok && v != 0 {
		o.boolean(printAny, "ecn", "ecn ", true)
	}
	if v, ok := tb.U32(xtcpnl.TcaFqCodelDropBatchSize); ok && v != 0 {
		o.uint(printAny, "drop_batch", "drop_batch "+u32s(v)+" ", v)
	}
}

// jsonTime is the u32 microseconds pattern: print_uint(PRINT_JSON, key)
// then print_string(PRINT_FP, "key %s ", sprint_time()).
func (r *runner) jsonTime(tb xtcpnl.AttrTable, t uint16, key string) {
	v, ok := tb.U32(t)
	if !ok {
		return
	}
	r.o.uint(printJSON, key, "", v)
	r.o.fp(key + " " + sprintTime(v) + " ")
}

// printFqCodelXstats is fq_codel_print_xstats: a short payload is
// MIN-copied, so it never stops.
func printFqCodelXstats(r *runner, xs []byte) {
	st := xtcpnl.DecodeTcFqCodelXstats(xs)
	o := r.o
	if st.Type == xtcpnl.TcaFqCodelXstatsQdisc {
		q := st.Qdisc
		o.uint(printAny, "maxpacket", "  maxpacket "+u32s(q.Maxpacket), q.Maxpacket)
		o.uint(printAny, "drop_overlimit", " drop_overlimit "+u32s(q.DropOverlimit), q.DropOverlimit)
		o.uint(printAny, "new_flow_count", " new_flow_count "+u32s(q.NewFlowCount), q.NewFlowCount)
		o.uint(printAny, "ecn_mark", " ecn_mark "+u32s(q.EcnMark), q.EcnMark)
		if q.CeMark != 0 {
			o.uint(printAny, "ce_mark", " ce_mark "+u32s(q.CeMark), q.CeMark)
		}
		if q.MemoryUsage != 0 {
			o.uint(printAny, "memory_used", " memory_used "+u32s(q.MemoryUsage), q.MemoryUsage)
		}
		if q.DropOvermemory != 0 {
			o.uint(printAny, "drop_overmemory", " drop_overmemory "+u32s(q.DropOvermemory), q.DropOvermemory)
		}
		o.nl()
		o.uint(printAny, "new_flows_len", "  new_flows_len "+u32s(q.NewFlowsLen), q.NewFlowsLen)
		o.uint(printAny, "old_flows_len", " old_flows_len "+u32s(q.OldFlowsLen), q.OldFlowsLen)
	}
	if st.Type == xtcpnl.TcaFqCodelXstatsClass {
		c := st.Class
		o.int(printAny, "deficit", "  deficit "+strconv.Itoa(int(c.Deficit)), c.Deficit)
		o.uint(printAny, "count", " count "+u32s(c.Count), c.Count)
		o.uint(printAny, "lastcount", " lastcount "+u32s(c.Lastcount), c.Lastcount)
		o.uint(printJSON, "ldelay", "", c.Ldelay)
		o.fp(" ldelay " + sprintTime(c.Ldelay))
		if c.Dropping != 0 {
			o.boolean(printAny, "dropping", " dropping", true)
			o.int(printJSON, "drop_next", "", c.DropNext)
			if c.DropNext < 0 {
				o.fp(" drop_next -" + sprintTime(cint.Bits(-c.DropNext)))
			} else {
				o.fp(" drop_next " + sprintTime(cint.Bits(c.DropNext)))
			}
		}
	}
}
