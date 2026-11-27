package gotc

import (
	"bytes"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/q_hfsc.c (iproute2 v7.2.0).

// printHfscOpt is hfsc_print_opt: the default class through print_0xhex
// with a leading space, so text has two spaces after `refcnt N`, JSON a
// string, and nothing at 0 (Q40). Nothing for a short struct.
func printHfscOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	q, err := xtcpnl.DecodeTcHfscQopt(opt)
	if err != nil {
		return
	}
	if q.Defcls != 0 {
		d := uint64(q.Defcls)
		r.o.hex0x(printAny, "default", " default "+cHashX(d)+" ", d)
	}
}

// printHfscXstats is hfsc_print_xstats: work and rtwork only when nonzero
// (Q41), then a text-only _SL_ that, with print_class's own newline, leaves
// a blank line (Q42). Nothing for a short payload.
func printHfscXstats(r *runner, xs []byte) {
	st, err := xtcpnl.DecodeTcHfscStats(xs)
	if err != nil {
		return
	}
	o := r.o
	o.uint(printAny, "period", " period "+u32s(st.Period)+" ", st.Period)
	if st.Work != 0 {
		o.u64(printAny, "work", "work "+u64s(st.Work)+" bytes ", st.Work)
	}
	if st.Rtwork != 0 {
		o.u64(printAny, "rtwork", "rtwork "+u64s(st.Rtwork)+" bytes ", st.Rtwork)
	}
	o.uint(printAny, "level", " level "+u32s(st.Level)+" ", st.Level)
	o.fp(o.sl)
}

// hfscCurve is one class curve as hfsc_print_class_opt takes it: the
// payload when it holds a whole struct, nil otherwise.
func (r *runner) hfscCurve(tb xtcpnl.AttrTable, t uint16, name string) []byte {
	if !tb.Has(t) {
		return nil
	}
	b := tb.Bytes(t)
	if len(b) < xtcpnl.TcServiceCurveSizeCst {
		r.errf("HFSC: truncated %s option\n", name)
		return nil
	}
	return b[:xtcpnl.TcServiceCurveSizeCst]
}

// printHfscClassOpt is hfsc_print_class_opt: rt and ls are one `sc` when
// their bytes match (Q43), and ul follows. A truncated curve is warned
// about and skipped.
func printHfscClassOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	tb := r.nested(opt, xtcpnl.TcaHfscMaxKnown)
	rsc := r.hfscCurve(tb, xtcpnl.TcaHfscRsc, "realtime")
	fsc := r.hfscCurve(tb, xtcpnl.TcaHfscFsc, "linkshare")
	usc := r.hfscCurve(tb, xtcpnl.TcaHfscUsc, "upperlimit")
	if rsc != nil && fsc != nil && bytes.Equal(rsc, fsc) {
		r.hfscSc("sc", rsc)
	} else {
		if rsc != nil {
			r.hfscSc("rt", rsc)
		}
		if fsc != nil {
			r.hfscSc("ls", fsc)
		}
	}
	if usc != nil {
		r.hfscSc("ul", usc)
	}
}

// hfscSc is hfsc_print_sc: d through tc_core_ktime2time, sprint_time in
// text and the number in JSON (Q44).
func (r *runner) hfscSc(name string, b []byte) {
	sc, err := xtcpnl.DecodeTcServiceCurve(b)
	if err != nil {
		return
	}
	d := cDoubleToU32(float64(sc.D) / r.core.clockFactor)
	o := r.o
	o.openObject(name)
	o.fp(name + " ")
	o.rate(r.iec > 0, printAny, "m1", "m1 ", " ", uint64(sc.M1))
	o.fp("d " + sprintTime(d) + " ")
	o.uint(printJSON, "d", "", d)
	o.rate(r.iec > 0, printAny, "m2", "m2 ", " ", uint64(sc.M2))
	o.closeObject()
}
