package gotc

import (
	"errors"
	"fmt"
	"math"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/q_gred.c gred_print_opt, gred_parse_vqs and gred_print_stats.

// printGredOpt is gred_print_opt. Where tc would dereference a missing
// DPS (Q67), write past infos[] for a VQ_DP of MAX_DPs or more (Q68),
// read a short VQ attribute (Q69) or spin on an rta_len under 4 (Q71),
// gotc refuses. The "message smaller" line is raw, inside -j's JSON too
// (Q72).
func printGredOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	tb := r.nested(opt, xtcpnl.TcaGredMaxKnown)
	if !tb.Has(xtcpnl.TcaGredParms) {
		return
	}
	q, err := xtcpnl.ParseTcQopt(xtcpnl.TcKindGred, opt[:validAttrPrefix(opt)])
	g := q.Aqm.Gred
	if g != nil {
		for _, w := range g.Warn {
			r.errf("%s\n", w)
		}
	}
	switch {
	case errors.Is(err, xtcpnl.ErrTcGredSmall):
		r.o.raw("\n GRED received message smaller than expected\n")
		return
	case err != nil:
		r.refuseKind(fmt.Errorf("%w: gred %w", errNotImplemented, err))
		return
	case g.VqDpOver:
		r.refuseKind(actUnchecked(xtcpnl.TcKindGred, "VQ_DP past MAX_DPs (Q68)"))
		return
	}
	o := r.o
	o.uint(printAny, "dp_cnt", "vqs "+u32s(g.Sopt.DPs)+" ", g.Sopt.DPs)
	o.uint(printAny, "dp_default", "default "+u32s(g.Sopt.DefDP)+" ", g.Sopt.DefDP)
	if g.Sopt.Grio != 0 {
		o.boolean(printAny, "grio", "grio ", true)
	} else {
		o.boolean(printAny, "grio", "", false)
	}
	if g.HasLimit {
		o.size(printAny, "limit", "limit ", " ", uint64(g.Limit))
	}
	printRedFlags(o, uint32(g.Sopt.Flags))
	o.openArray(printJSON, "vqs")
	for i := range g.Qopts {
		qo := &g.Qopts[i]
		if qo.DP >= xtcpnl.GredMaxDPsCst {
			continue
		}
		o.openObject("")
		o.uint(printAny, "vq", "\n vq "+u32s(qo.DP)+" ", qo.DP)
		o.hhu(printAny, "prio", "prio "+u32s(uint32(qo.Prio))+" ", qo.Prio)
		o.size(printAny, "limit", "limit ", " ", uint64(qo.Limit))
		o.size(printAny, "min", "min ", " ", uint64(qo.QthMin))
		o.size(printAny, "max", "max ", " ", uint64(qo.QthMax))
		if g.Info[i].FlagsPresent {
			printRedFlags(o, g.Info[i].Flags)
		}
		if r.details > 0 {
			o.uint(printAny, "ewma", "ewma "+u32s(uint32(qo.Wlog))+" ", uint32(qo.Wlog))
			if g.HasMaxP {
				p := float64(g.MaxP[i]) / math.Pow(2, 32)
				o.float(printAny, "probability", "probability "+cG(p)+" ", p)
			} else {
				o.uint(printAny, "Plog", "Plog "+u32s(uint32(qo.Plog))+" ", uint32(qo.Plog))
			}
			o.uint(printAny, "Scell_log", "Scell_log "+u32s(uint32(qo.ScellLog))+" ", uint32(qo.ScellLog))
		}
		if r.stats > 0 {
			var in *xtcpnl.TcGredInfo
			if g.HasVQ {
				in = &g.Info[i]
			}
			r.gredStats(in, qo)
		}
		o.closeObject()
	}
	o.closeArray(printJSON, "vqs")
}

// gredStats is gred_print_stats: the VQ list's counters when the kernel
// sent one, else the qopt's. bytes is tc's u32 read of the u64 attribute
// (Q70).
func (r *runner) gredStats(in *xtcpnl.TcGredInfo, qo *xtcpnl.TcGredQopt) {
	o := r.o
	bytes := uint64(qo.Bytesin)
	if in != nil {
		bytes = uint64(in.Bytes)
	}
	o.fp("\n  Queue size: ")
	o.size(printAny, "qave", "average ", " ", uint64(qo.Qave))
	o.size(printAny, "backlog", "current ", " ", uint64(qo.Backlog))
	o.fp("\n  Dropped packets: ")
	if in != nil {
		o.uint(printAny, "forced_drop", "forced "+u32s(in.ForcedDrop)+" ", in.ForcedDrop)
		o.uint(printAny, "prob_drop", "early "+u32s(in.ProbDrop)+" ", in.ProbDrop)
		o.uint(printAny, "pdrop", "pdrop "+u32s(in.Pdrop)+" ", in.Pdrop)
		o.uint(printAny, "other", "other "+u32s(in.Other)+" ", in.Other)
		o.fp("\n  Marked packets: ")
		o.uint(printAny, "forced_mark", "forced "+u32s(in.ForcedMark)+" ", in.ForcedMark)
		o.uint(printAny, "prob_mark", "early "+u32s(in.ProbMark)+" ", in.ProbMark)
	} else {
		o.uint(printAny, "forced_drop", "forced "+u32s(qo.Forced)+" ", qo.Forced)
		o.uint(printAny, "prob_drop", "early "+u32s(qo.Early)+" ", qo.Early)
		o.uint(printAny, "pdrop", "pdrop "+u32s(qo.Pdrop)+" ", qo.Pdrop)
		o.uint(printAny, "other", "other "+u32s(qo.Other)+" ", qo.Other)
	}
	o.fp("\n  Total packets: ")
	o.uint(printAny, "packets", u32s(qo.Packets)+" ", qo.Packets)
	o.size(printAny, "bytes", "(", ") ", bytes)
}
