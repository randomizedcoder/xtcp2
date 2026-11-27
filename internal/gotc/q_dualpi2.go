package gotc

import (
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/q_dualpi2.c dualpi2_print_opt and dualpi2_print_xstats. Its JSON keys
// are hyphenated and its text names differ from them (Q83).

const (
	dualpi2AlphaBetaScale = 1 << 8 // ALPHA_BETA_SCALE
	dualpi2EcnAnyEct      = 3      // TC_DUALPI2_ECN_MASK_ANY_ECT
	dualpi2EcnL4sEct      = 1      // TC_DUALPI2_ECN_MASK_L4S_ECT
)

// cF is C's "%f".
func cF(v float64) string { return strconv.FormatFloat(v, 'f', 6, 64) }

// dualpi2EcnType is get_ecn_type and get_ecn_type_json: CLA_ECT prints as
// any_ect (Q85); neither bit set is a warning and "".
func (r *runner) dualpi2EcnType(ect uint8, json bool) string {
	switch ect & dualpi2EcnAnyEct {
	case dualpi2EcnL4sEct:
		if json {
			return "l4s-ect"
		}
		return "l4s_ect"
	case 0:
		r.errf("Warning: Unexpected ecn type %d!\n", ect)
		return ""
	}
	if json {
		return "any-ect"
	}
	return "any_ect"
}

// printDualpi2Opt is dualpi2_print_opt.
func printDualpi2Opt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	tb := r.nested(opt, xtcpnl.TcaDualpi2MaxKnown)
	o := r.o
	if v, ok := tb.U32(xtcpnl.TcaDualpi2Limit); ok {
		o.uint(printAny, "limit", "limit "+u32s(v)+"p ", v)
	}
	if v, ok := tb.U32(xtcpnl.TcaDualpi2MemoryLimit); ok {
		o.uint(printAny, "memory-limit", "memlimit "+u32s(v)+"B ", v)
	}
	r.jsonTime(tb, xtcpnl.TcaDualpi2Target, "target")
	r.jsonTime(tb, xtcpnl.TcaDualpi2Tupdate, "tupdate")
	for _, ab := range []struct {
		t   uint16
		key string
	}{{xtcpnl.TcaDualpi2Alpha, wordAlpha}, {xtcpnl.TcaDualpi2Beta, wordBeta}} {
		if v, ok := tb.U32(ab.t); ok {
			// get_scaled_alpha_beta: a float, promoted for printf.
			f := float64(float32(v) / dualpi2AlphaBetaScale)
			o.float(printAny, ab.key, ab.key+" "+cF(f)+" ", f)
		}
	}
	if v, ok := tb.U32(xtcpnl.TcaDualpi2StepThreshPkts); ok {
		o.uint(printAny, "step-thresh-pkts", "step_thresh "+u32s(v)+"p ", v)
	}
	if v, ok := tb.U32(xtcpnl.TcaDualpi2StepThreshUs); ok {
		o.uint(printJSON, "step-thresh-us", "", v)
		o.fp("step_thresh " + sprintTime(v) + " ")
	}
	if v, ok := tb.U32(xtcpnl.TcaDualpi2MinQlenStep); ok {
		o.uint(printAny, "min-qlen-step", "min_qlen_step "+u32s(v)+"p ", v)
	}
	if v, ok := tb.U8(xtcpnl.TcaDualpi2Coupling); ok {
		o.uint(printAny, "coupling", "coupling_factor "+u32s(uint32(v))+" ", uint32(v))
	}
	printDualpi2Choice(o, tb, xtcpnl.TcaDualpi2DropOverload, "drop-overload",
		[2]string{"overflow", "drop_on_overload"}, [2]string{"overflow", "drop"})
	printDualpi2Choice(o, tb, xtcpnl.TcaDualpi2DropEarly, "drop-early",
		[2]string{"drop_dequeue", "drop_enqueue"}, [2]string{"drop-dequeue", "drop-enqueue"})
	if v, ok := tb.U8(xtcpnl.TcaDualpi2CProtection); ok {
		o.uint(printAny, "c-protection", "classic_protection "+u32s(uint32(v))+"% ", uint32(v))
	}
	if v, ok := tb.U8(xtcpnl.TcaDualpi2EcnMask); ok {
		// Both getters run in either mode, so a bad mask warns twice.
		text, js := r.dualpi2EcnType(v, false), r.dualpi2EcnType(v, true)
		o.fp(text + " ")
		o.str(printJSON, colorNone, "ecn-mask", "", js)
	}
	if v, ok := tb.U8(xtcpnl.TcaDualpi2SplitGso); ok {
		// split_gso true is a JSON null; false has no key (Q84).
		if v != 0 {
			o.fp("split_gso ")
			o.null(printJSON, "split-gso", "")
		} else {
			o.fp("no_split_gso ")
		}
	}
}

// printDualpi2Choice is a u8 printed as one of two words, text and JSON
// apart.
func printDualpi2Choice(o *out, tb xtcpnl.AttrTable, t uint16, key string, text, js [2]string) {
	v, ok := tb.U8(t)
	if !ok {
		return
	}
	i := 0
	if v != 0 {
		i = 1
	}
	o.fp(text[i] + " ")
	o.str(printJSON, colorNone, key, "", js[i])
}

// printDualpi2Xstats is dualpi2_print_xstats: bare print_nl's, the last
// one before the caller's, so text ends with a blank line and no indent
// (Q86).
func printDualpi2Xstats(r *runner, xs []byte) {
	a, ok := aqmXstats(xtcpnl.TcKindDualpi2, xs)
	if !ok {
		return
	}
	st := a.Dualpi2
	o := r.o
	p := float64(st.Prob) / float64(^uint32(0))
	o.float(printAny, "prob", "prob "+cF(p)+" ", p)
	o.uint(printAny, "delay-c", "delay_c "+u32s(st.DelayC)+"us ", st.DelayC)
	o.uint(printAny, "delay-l", "delay_l "+u32s(st.DelayL)+"us", st.DelayL)
	o.nl()
	o.uint(printAny, "pkts-in-c", "pkts_in_c "+u32s(st.PacketsInC)+" ", st.PacketsInC)
	o.uint(printAny, "pkts-in-l", "pkts_in_l "+u32s(st.PacketsInL)+" ", st.PacketsInL)
	o.uint(printAny, "maxq", "maxq "+u32s(st.Maxq), st.Maxq)
	o.nl()
	o.uint(printAny, "ecn-mark", "ecn_mark "+u32s(st.EcnMark)+" ", st.EcnMark)
	o.uint(printAny, "step-mark", "step_mark "+u32s(st.StepMarks), st.StepMarks)
	o.nl()
	o.int(printAny, "credit", "credit "+strconv.Itoa(int(st.Credit))+" ", st.Credit)
	q := "L-queue"
	if st.Credit > 0 {
		q = "C-queue"
	}
	o.fp("(" + q + ")")
	o.nl()
	o.uint(printAny, "memory-used", "memory_used "+u32s(st.MemoryUsed)+" ", st.MemoryUsed)
	o.uint(printAny, "max-memory-used", "(max "+u32s(st.MaxMemoryUsed)+") ", st.MaxMemoryUsed)
	o.uint(printAny, "memory-limit", "of memory limit "+u32s(st.MemoryLimit), st.MemoryLimit)
	o.nl()
}
