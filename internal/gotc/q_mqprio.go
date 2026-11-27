package gotc

import (
	"encoding/binary"
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// tc/q_mqprio.c (iproute2 v7.2.0).

// mqprioIndent is the hard newline and indent mqprio's text uses, so -o
// leaves its lines broken (Q48).
const mqprioIndent = "\n             "

// mqprioAligned is RTA_ALIGN(sizeof(struct tc_mqprio_qopt)).
const mqprioAligned = (xtcpnl.TcMqprioQoptSizeCst + 3) &^ 3

// printMqprioOpt is mqprio_print_opt. The map's text label keeps its space
// (Q52); a queue is `(offset:offset+count-1)`, an int printed as unsigned
// (Q51); mode and shaper print "dcb" when absent and nothing when present
// at their default, and only when attributes follow the header, which the
// kernel always sends (Q49, K26). A num_tc past TC_QOPT_MAX_QUEUE reads past
// the struct, and is refused.
func printMqprioOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	n := len(opt) - mqprioAligned
	if n < 0 {
		r.errf("options size error\n")
		return
	}
	q, err := xtcpnl.DecodeTcMqprioQopt(opt)
	if err != nil {
		return
	}
	if q.NumTc > xtcpnl.TcQoptMaxQueueCst {
		r.refuseKind(actUnchecked(xtcpnl.TcKindMqprio, "num_tc "+strconv.Itoa(int(q.NumTc))))
		return
	}
	o := r.o
	json := o.isJSON()
	nt := uint32(q.NumTc)
	o.uint(printAny, "tc", "tc "+u32s(nt)+" ", nt)
	o.openArray(printAny, pick(json, "map", "map "))
	for _, m := range q.PrioTcMap {
		o.uint(printAny, "", u32s(uint32(m))+" ", uint32(m))
	}
	o.closeArray(printAny, "")
	o.openArray(printAny, pick(json, "queues", mqprioIndent+"queues:"))
	for i := range int(q.NumTc) {
		o.openArray(printJSON, "")
		off := uint32(q.Offset[i])
		last := cint.Bits(int32(q.Offset[i]) + int32(q.Count[i]) - 1) // int arithmetic, then %u
		o.uint(printAny, "", "("+u32s(off)+":", off)
		o.uint(printAny, "", u32s(last)+") ", last)
		o.closeArray(printJSON, "")
	}
	o.closeArray(printAny, "")
	if n == 0 {
		return
	}
	tail := opt[mqprioAligned:]
	tb := r.attrTable(tail, xtcpnl.TcaMqprioMaxKnown, 0)
	if !r.mqprioMode(tb, xtcpnl.TcaMqprioMode, "mode", xtcpnl.TcMqprioModeChannel, "channel") ||
		!r.mqprioMode(tb, xtcpnl.TcaMqprioShaper, "shaper", xtcpnl.TcMqprioShaperBwRate, "bw_rlimit") {
		return
	}
	for _, rl := range []struct {
		t          uint16
		key, label string
	}{
		{xtcpnl.TcaMqprioMinRate64, "min_rate", "\tmin_rate:"},
		{xtcpnl.TcaMqprioMaxRate64, "max_rate", "\tmax_rate:"},
	} {
		if !tb.Has(rl.t) {
			continue
		}
		if !r.mqprioRates(tb.Bytes(rl.t), rl.t, rl.key, pick(json, rl.key, rl.label), int(q.NumTc)) {
			return
		}
	}
	r.tcEntries(tail, xtcpnl.TcaMqprioTcEntry, xtcpnl.TcaMqprioTcEntryMaxKnown, 0, xtcpnl.TcaMqprioTcEntryFp,
		xtcpnl.TcKindMqprio, pick(json, "fp", mqprioIndent+"fp:"), "")
}

// pick is C's `cond ? a : b`.
func pick(cond bool, a, b string) string {
	if cond {
		return a
	}
	return b
}

// mqprioMode prints mqprio's mode or shaper: "dcb" when absent, the name
// when the u16 (read unchecked, Q54) is the one value tc names, nothing
// otherwise.
func (r *runner) mqprioMode(tb xtcpnl.AttrTable, t uint16, key string, named uint16, name string) bool {
	if !tb.Has(t) {
		r.o.str(printAny, colorNone, key, mqprioIndent+key+":dcb", "dcb")
		return true
	}
	if !r.kindNeed(xtcpnl.TcKindMqprio, key, tb.Bytes(t), 2) {
		return false
	}
	if v, _ := tb.U16(t); v == named {
		r.o.str(printAny, colorNone, key, mqprioIndent+key+":"+name, name)
	}
	return true
}

// mqprioRates is a MIN_RATE64 or MAX_RATE64 list: each member must have
// the list's type (tc returns -1 otherwise, before printing) and holds a
// u64 into a 16-entry array; then num_tc rates are printed. tc writes past
// the array from the 17th member (Q53); gotc stops at 16. A short member
// is refused.
func (r *runner) mqprioRates(b []byte, t uint16, key, label string, numTc int) bool {
	var rates [xtcpnl.TcQoptMaxQueueCst]uint64
	n, ok, foreign := 0, true, false
	rtaWalk(b, func(raw uint16, v []byte) {
		switch {
		case !ok || foreign:
		case raw != t:
			foreign = true
		case !r.kindNeed(xtcpnl.TcKindMqprio, key, v, 8):
			ok = false
		case n < len(rates):
			rates[n] = binary.LittleEndian.Uint64(v)
			n++
		}
	})
	if !ok || foreign {
		return false
	}
	o := r.o
	o.openArray(printAny, label)
	for i := range numTc {
		o.rate(r.iec > 0, printAny, "", "", " ", rates[i])
	}
	o.closeArray(printAny, "")
	return true
}

// tcEntries is dump_tc_entries (q_mqprio.c and q_taprio.c): an RTA_OK walk
// for the raw type entry|NLA_F_NESTED, each parsed nested; no index or one
// of 16 or more is tc's message and the entry skipped. Then max-sdu (sdu
// nonzero: taprio) and fp, each up to the highest class set, with a
// print_nl after each that, with the caller's newline, leaves a blank line
// (Q50, Q58). The kernel sends all 16 entries (K26).
func (r *runner) tcEntries(opt []byte, entry, maxType, sdu, fp uint16, kind, fpLabel, sduLabel string) {
	var maxSdu, fps [xtcpnl.TcQoptMaxQueueCst]uint32
	maxTcSdu, maxTcFp := -1, -1
	ok := true
	rtaWalk(opt, func(raw uint16, v []byte) {
		if !ok || raw != entry|unix.NLA_F_NESTED {
			return
		}
		tb := r.nested(v, maxType)
		if !tb.Has(1) { // TCA_*_TC_ENTRY_INDEX
			r.errf("Missing tc entry index\n")
			return
		}
		if ok = r.kindNeed(kind, "tc entry index", tb.Bytes(1), 4); !ok {
			return
		}
		tc, _ := tb.U32(1)
		if tc >= xtcpnl.TcQoptMaxQueueCst {
			r.errf("Unexpected tc entry index %d\n", cint.Int(tc))
			return
		}
		if sdu != 0 && tb.Has(sdu) {
			if ok = r.kindNeed(kind, "max-sdu", tb.Bytes(sdu), 4); !ok {
				return
			}
			maxSdu[tc], _ = tb.U32(sdu)
			maxTcSdu = max(maxTcSdu, int(tc))
		}
		if tb.Has(fp) {
			if ok = r.kindNeed(kind, "fp", tb.Bytes(fp), 4); !ok {
				return
			}
			fps[tc], _ = tb.U32(fp)
			maxTcFp = max(maxTcFp, int(tc))
		}
	})
	if !ok {
		return
	}
	o := r.o
	if maxTcSdu >= 0 {
		o.openArray(printAny, sduLabel)
		for tc := 0; tc <= maxTcSdu; tc++ {
			o.uint(printAny, "", " "+u32s(maxSdu[tc]), maxSdu[tc])
		}
		o.closeArray(printAny, "")
		o.nl()
	}
	if maxTcFp >= 0 {
		o.openArray(printAny, fpLabel)
		for tc := 0; tc <= maxTcFp; tc++ {
			s := "?"
			switch fps[tc] {
			case xtcpnl.TcFpPreemptible:
				s = "P"
			case xtcpnl.TcFpExpress:
				s = "E"
			}
			o.str(printAny, colorNone, "", " "+s, s)
		}
		o.closeArray(printAny, "")
		o.nl()
	}
}
