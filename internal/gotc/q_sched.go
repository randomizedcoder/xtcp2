package gotc

import (
	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/q_drr.c, tc/q_qfq.c, tc/q_multiq.c, tc/q_cbs.c and tc/q_etf.c
// (iproute2 v7.2.0).

// printDrrOpt is drr_print_opt, the qdisc's and the class's: the quantum
// through print_size(PRINT_FP), so text only (Q37). A drr qdisc has no
// options at all (sch_drr.c has no .dump, K29).
func printDrrOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	tb := r.nested(opt, xtcpnl.TcaDrrMaxKnown)
	if !tb.Has(xtcpnl.TcaDrrQuantum) {
		return
	}
	if !r.kindNeed(xtcpnl.TcKindDrr, "quantum", tb.Bytes(xtcpnl.TcaDrrQuantum), 4) {
		return
	}
	v, _ := tb.U32(xtcpnl.TcaDrrQuantum)
	r.o.size(printFP, "", "quantum ", " ", uint64(v))
}

// printDrrXstats is drr_print_xstats: the deficit, text only (Q38);
// nothing for a short payload.
func printDrrXstats(r *runner, xs []byte) {
	st, err := xtcpnl.DecodeTcDrrStats(xs)
	if err != nil {
		return
	}
	r.o.size(printFP, "", " deficit ", " ", uint64(st.Deficit))
}

// printQfqOpt is qfq_print_opt, the qdisc's (no options, K29) and the
// class's. qfq has no print_xstats, so the tc_qfq_stats the kernel sends
// are never printed (Q39, K24).
func printQfqOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	tb := r.nested(opt, xtcpnl.TcaQfqMaxKnown)
	for _, f := range []struct {
		t        uint16
		key, txt string
	}{{xtcpnl.TcaQfqWeight, "weight", "weight "}, {xtcpnl.TcaQfqLmax, "maxpkt", "maxpkt "}} {
		if !tb.Has(f.t) {
			continue
		}
		if !r.kindNeed(xtcpnl.TcKindQfq, f.key, tb.Bytes(f.t), 4) {
			return
		}
		v, _ := tb.U32(f.t)
		r.o.uint(printAny, f.key, f.txt+u32s(v)+" ", v)
	}
}

// printMultiqOpt is multiq_print_opt: "bands %u/%u" in text, two numbers
// in JSON; nothing for a short struct. The kernel sets bands to the
// device's queue count, whatever was asked (K27).
func printMultiqOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	q, err := xtcpnl.DecodeTcMultiqQopt(opt)
	if err != nil {
		return
	}
	b, m := uint32(q.Bands), uint32(q.MaxBands)
	r.o.fp("bands " + u32s(b) + "/" + u32s(m) + " ")
	r.o.uint(printJSON, "bands", "", b)
	r.o.uint(printJSON, "max_bands", "", m)
}

// printCbsOpt is cbs_print_opt: TCA_CBS_PARMS required and full, each
// field through print_int, offload a number (Q61).
func printCbsOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	tb := r.nested(opt, xtcpnl.TcaCbsMaxKnown)
	if !tb.Has(xtcpnl.TcaCbsParms) {
		return
	}
	q, err := xtcpnl.DecodeTcCbsQopt(tb.Bytes(xtcpnl.TcaCbsParms))
	if err != nil {
		return
	}
	o := r.o
	for _, f := range []struct {
		key string
		v   int32
	}{
		{"hicredit", q.Hicredit}, {"locredit", q.Locredit}, {"sendslope", q.Sendslope},
		{"idleslope", q.Idleslope}, {"offload", int32(q.Offload)},
	} {
		o.int(printAny, f.key, f.key+" "+i32s(f.v)+" ", f.v)
	}
}

// printEtfOpt is etf_print_opt: TCA_ETF_PARMS required and full. delta, an
// s32, goes through print_uint with "%d": signed in text, unsigned in
// JSON (Q62); offload is "on"/"off" (Q61); skip_sock_check has no
// trailing space (Q63).
func printEtfOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	tb := r.nested(opt, xtcpnl.TcaEtfMaxKnown)
	if !tb.Has(xtcpnl.TcaEtfParms) {
		return
	}
	q, err := xtcpnl.DecodeTcEtfQopt(tb.Bytes(xtcpnl.TcaEtfParms))
	if err != nil {
		return
	}
	o := r.o
	c := getClockName(q.Clockid)
	o.str(printAny, colorNone, "clockid", "clockid "+c+" ", c)
	o.emit(printAny, colorNone, "delta", u32s(cint.Bits(q.Delta)), "delta "+i32s(q.Delta)+" ")
	onOff := func(f uint32) string {
		if q.Flags&f != 0 {
			return "on"
		}
		return "off"
	}
	v := onOff(xtcpnl.TcEtfOffloadOn)
	o.str(printAny, colorNone, "offload", "offload "+v+" ", v)
	v = onOff(xtcpnl.TcEtfDeadlineModeOn)
	o.str(printAny, colorNone, "deadline_mode", "deadline_mode "+v+" ", v)
	v = onOff(xtcpnl.TcEtfSkipSockCheckOn)
	o.str(printAny, colorNone, "skip_sock_check", "skip_sock_check "+v, v)
}
