package gotc

import (
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/q_ets.c (iproute2 v7.2.0).

// printEtsOpt is ets_print_opt: NBANDS and PRIOMAP required ("Incomplete
// ETS options"), NSTRICT only when the kernel sends it, QUANTA only when
// some band is not strict (K25). A failed list leaves its JSON array open,
// as tc's `return -1` does (Q47).
func printEtsOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	tb := r.nested(opt, xtcpnl.TcaEtsMaxKnown)
	if !tb.Has(xtcpnl.TcaEtsNbands) || !tb.Has(xtcpnl.TcaEtsPriomap) {
		r.errf("Incomplete ETS options\n")
		return
	}
	o := r.o
	if !r.kindNeed(xtcpnl.TcKindEts, "nbands", tb.Bytes(xtcpnl.TcaEtsNbands), 1) {
		return
	}
	nb, _ := tb.U8(xtcpnl.TcaEtsNbands)
	o.uint(printAny, "bands", "bands "+u32s(uint32(nb))+" ", uint32(nb))
	if tb.Has(xtcpnl.TcaEtsNstrict) {
		if !r.kindNeed(xtcpnl.TcKindEts, "nstrict", tb.Bytes(xtcpnl.TcaEtsNstrict), 1) {
			return
		}
		ns, _ := tb.U8(xtcpnl.TcaEtsNstrict)
		o.uint(printAny, "strict", "strict "+u32s(uint32(ns))+" ", uint32(ns))
	}
	if tb.Has(xtcpnl.TcaEtsQuanta) {
		if !r.etsList(tb.Bytes(xtcpnl.TcaEtsQuanta), "quanta", xtcpnl.TcaEtsQuantaBand, 4, 0, "No ETS band quantum") {
			return
		}
	}
	r.etsList(tb.Bytes(xtcpnl.TcaEtsPriomap), "priomap", xtcpnl.TcaEtsPriomapBand, 1, 3, "No ETS priomap band")
}

// etsList is ets_print_opt_quanta and ets_print_opt_priomap: at each
// offset, parse_rtattr from there to the end, the first band attribute
// printed as " %u", and the offset stepped by the rta_len there plus pad;
// the array closes with a text " " (Q45). No band attribute at an offset
// is tc's message and `return -1`, the array left open; ok is false then
// or after a refusal.
func (r *runner) etsList(b []byte, name string, band uint16, width, pad int, missing string) bool {
	o := r.o
	o.openArray(printAny, name)
	for off := 0; off < len(b); {
		tb := r.attrTable(b[off:], xtcpnl.TcaEtsMaxKnown, 0)
		// The step reads the rta_len at the offset; with fewer than two
		// bytes left that is past the list, but the missing band below
		// returns before the offset is used again.
		step := int(attrLenAt(b, off)) + pad
		if !tb.Has(band) {
			r.errf("%s\n", missing)
			return false
		}
		if !r.kindNeed(xtcpnl.TcKindEts, name, tb.Bytes(band), width) {
			return false
		}
		var v uint32
		if width == 4 {
			v, _ = tb.U32(band)
		} else {
			u, _ := tb.U8(band)
			v = uint32(u)
		}
		o.uint(printAny, "", " "+u32s(v), v)
		off += step
	}
	o.closeArray(printAny, " ")
	return true
}

// printEtsCopt is ets_print_copt: a class's quantum, absent for a strict
// band, whose nest the kernel sends empty (Q46, K25).
func printEtsCopt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	tb := r.nested(opt, xtcpnl.TcaEtsMaxKnown)
	if !tb.Has(xtcpnl.TcaEtsQuantaBand) {
		return
	}
	if !r.kindNeed(xtcpnl.TcKindEts, "quantum", tb.Bytes(xtcpnl.TcaEtsQuantaBand), 4) {
		return
	}
	v, _ := tb.U32(xtcpnl.TcaEtsQuantaBand)
	r.o.uint(printAny, "quantum", "quantum "+u32s(v)+" ", v)
}
