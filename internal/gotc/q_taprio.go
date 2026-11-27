package gotc

import (
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/q_taprio.c (iproute2 v7.2.0).

// taprioCmd is entry_cmd_to_str over TC_TAPRIO_CMD_*.
func taprioCmd(cmd uint8) string {
	switch cmd {
	case 0:
		return "S" // SET_GATES
	case 1:
		return "H" // SET_AND_HOLD
	case 2:
		return "R" // SET_AND_RELEASE
	}
	return "Invalid"
}

// printTaprioOpt is taprio_print_opt. PRIOMAP is required; it is cast to
// struct tc_mqprio_qopt unchecked (Q54) and its num_tc indexes the queue
// arrays unchecked, so a short struct or a num_tc past 16 is refused.
// TXTIME_DELAY is read as s32 into a __u32 and printed with print_uint's
// "%d": signed in text, unsigned in JSON (Q55). The admin schedule has no
// text label (Q57). The JSON key "max-sdu" has a hyphen (Q56).
func printTaprioOpt(r *runner, _ *qdiscUtil, opt []byte, has bool) {
	if !has {
		return
	}
	tb := r.nested(opt, xtcpnl.TcaTaprioAttrMaxKnown)
	if !tb.Has(xtcpnl.TcaTaprioAttrPriomap) {
		return
	}
	pm := tb.Bytes(xtcpnl.TcaTaprioAttrPriomap)
	if len(pm) < xtcpnl.TcMqprioQoptSizeCst {
		r.refuseKind(actShort(xtcpnl.TcKindTaprio, len(pm)))
		return
	}
	q, err := xtcpnl.DecodeTcMqprioQopt(pm)
	if err != nil {
		return
	}
	if q.NumTc > xtcpnl.TcQoptMaxQueueCst {
		r.refuseKind(actUnchecked(xtcpnl.TcKindTaprio, "num_tc "+strconv.Itoa(int(q.NumTc))))
		return
	}
	o := r.o
	nt := uint32(q.NumTc)
	o.uint(printAny, "tc", "tc "+u32s(nt)+" ", nt)
	o.openArray(printAny, "map")
	for _, m := range q.PrioTcMap {
		o.uint(printAny, "", " "+u32s(uint32(m)), uint32(m))
	}
	o.closeArray(printAny, "")
	o.nl()
	o.openArray(printAny, "queues")
	for i := range int(q.NumTc) {
		o.openObject("")
		off, cnt := uint32(q.Offset[i]), uint32(q.Count[i])
		o.uint(printAny, "offset", " offset "+u32s(off), off)
		o.uint(printAny, "count", " count "+u32s(cnt), cnt)
		o.closeObject()
	}
	o.closeArray(printAny, "")
	o.nl()
	if !r.taprioTop(tb) || !r.taprioSchedule(tb) {
		return
	}
	if tb.Has(xtcpnl.TcaTaprioAttrAdminSched) {
		at := r.nested(tb.Bytes(xtcpnl.TcaTaprioAttrAdminSched), xtcpnl.TcaTaprioAttrMaxKnown)
		o.openObject("admin")
		ok := r.taprioSchedule(at)
		o.closeObject()
		if !ok {
			return
		}
	}
	r.tcEntries(opt, xtcpnl.TcaTaprioAttrTcEntry, xtcpnl.TcaTaprioTcEntryMaxKnown, xtcpnl.TcaTaprioTcEntryMaxSdu,
		xtcpnl.TcaTaprioTcEntryFp, xtcpnl.TcKindTaprio, "fp", "max-sdu")
}

// taprioTop is taprio_print_opt's clockid, flags and txtime delay, each
// read unchecked.
func (r *runner) taprioTop(tb xtcpnl.AttrTable) bool {
	o := r.o
	k := xtcpnl.TcKindTaprio
	if b := tb.Bytes(xtcpnl.TcaTaprioAttrSchedClockid); tb.Has(xtcpnl.TcaTaprioAttrSchedClockid) {
		if !r.kindNeed(k, "clockid", b, 4) {
			return false
		}
		v, _ := tb.U32(xtcpnl.TcaTaprioAttrSchedClockid)
		c := getClockName(cint.Int(v))
		o.str(printAny, colorNone, "clockid", "clockid "+c, c)
	}
	if b := tb.Bytes(xtcpnl.TcaTaprioAttrFlags); tb.Has(xtcpnl.TcaTaprioAttrFlags) {
		if !r.kindNeed(k, "flags", b, 4) {
			return false
		}
		v, _ := tb.U32(xtcpnl.TcaTaprioAttrFlags)
		o.hex0x(printAny, "flags", " flags "+cHashX(uint64(v)), uint64(v))
	}
	if b := tb.Bytes(xtcpnl.TcaTaprioAttrTxtimeDelay); tb.Has(xtcpnl.TcaTaprioAttrTxtimeDelay) {
		if !r.kindNeed(k, "txtime delay", b, 4) {
			return false
		}
		v, _ := tb.U32(xtcpnl.TcaTaprioAttrTxtimeDelay)
		o.emit(printAny, colorNone, "txtime_delay", u32s(v), " txtime delay "+i32s(cint.Int(v)))
	}
	return true
}

// taprioS64 prints an s64 schedule time through print_lluint with "%lld":
// signed in text, unsigned in JSON (Q59).
func (r *runner) taprioS64(tb xtcpnl.AttrTable, t uint16, key, label string) bool {
	if !tb.Has(t) {
		return true
	}
	if !r.kindNeed(xtcpnl.TcKindTaprio, key, tb.Bytes(t), 8) {
		return false
	}
	v, _ := tb.U64(t)
	r.o.emit(printAny, colorNone, key, u64s(v), label+strconv.FormatInt(cint.LongLong(v), 10))
	return true
}

// taprioSchedule is print_schedule: base-time, cycle-time and its
// extension, then the entry list.
func (r *runner) taprioSchedule(tb xtcpnl.AttrTable) bool {
	if !r.taprioS64(tb, xtcpnl.TcaTaprioAttrSchedBaseTime, "base_time", "\tbase-time ") ||
		!r.taprioS64(tb, xtcpnl.TcaTaprioAttrSchedCycleTime, "cycle_time", " cycle-time ") ||
		!r.taprioS64(tb, xtcpnl.TcaTaprioAttrSchedCycleTimeExtension, "cycle_time_extension", " cycle-time-extension ") {
		return false
	}
	if !tb.Has(xtcpnl.TcaTaprioAttrSchedEntryList) {
		return true
	}
	return r.taprioSchedList(tb.Bytes(xtcpnl.TcaTaprioAttrSchedEntryList))
}

// taprioSchedList is print_sched_list: a JSON-only "schedule" array, a
// newline, then each entry (of any type) parsed nested, its fields printed
// when present, each line ended by print_nl.
func (r *runner) taprioSchedList(list []byte) bool {
	o := r.o
	k := xtcpnl.TcKindTaprio
	o.openArray(printJSON, "schedule")
	o.nl()
	ok := true
	rtaWalk(list, func(_ uint16, v []byte) {
		if !ok {
			return
		}
		tb := r.nested(v, xtcpnl.TcaTaprioSchedEntryMaxKnown)
		o.openObject("")
		if b := tb.Bytes(xtcpnl.TcaTaprioSchedEntryIndex); tb.Has(xtcpnl.TcaTaprioSchedEntryIndex) {
			if ok = r.kindNeed(k, "entry index", b, 4); !ok {
				return
			}
			i, _ := tb.U32(xtcpnl.TcaTaprioSchedEntryIndex)
			o.uint(printAny, "index", "\tindex "+u32s(i), i)
		}
		if b := tb.Bytes(xtcpnl.TcaTaprioSchedEntryCmd); tb.Has(xtcpnl.TcaTaprioSchedEntryCmd) {
			if ok = r.kindNeed(k, "entry cmd", b, 1); !ok {
				return
			}
			c, _ := tb.U8(xtcpnl.TcaTaprioSchedEntryCmd)
			s := taprioCmd(c)
			o.str(printAny, colorNone, "cmd", " cmd "+s, s)
		}
		if b := tb.Bytes(xtcpnl.TcaTaprioSchedEntryGateMask); tb.Has(xtcpnl.TcaTaprioSchedEntryGateMask) {
			if ok = r.kindNeed(k, "entry gatemask", b, 4); !ok {
				return
			}
			g, _ := tb.U32(xtcpnl.TcaTaprioSchedEntryGateMask)
			o.hex0x(printAny, "gatemask", " gatemask "+cHashX(uint64(g)), uint64(g))
		}
		if b := tb.Bytes(xtcpnl.TcaTaprioSchedEntryInterval); tb.Has(xtcpnl.TcaTaprioSchedEntryInterval) {
			if ok = r.kindNeed(k, "entry interval", b, 4); !ok {
				return
			}
			iv, _ := tb.U32(xtcpnl.TcaTaprioSchedEntryInterval)
			o.uint(printAny, "interval", " interval "+u32s(iv), iv)
		}
		o.closeObject()
		o.nl()
	})
	if ok {
		o.closeArray(printAny, "")
	}
	return ok
}

// printTaprioXstats is taprio_print_xstats: the offload stats nest, each
// counter a u64 read unchecked. Software taprio on a dummy sends none
// (K28).
func printTaprioXstats(r *runner, xs []byte) {
	tb := r.nested(xs, xtcpnl.TcaTaprioOffloadStatsMaxKnown)
	for _, f := range []struct {
		t        uint16
		key, txt string
	}{
		{xtcpnl.TcaTaprioOffloadStatsWindowDrops, "window_drops", " window_drops "},
		{xtcpnl.TcaTaprioOffloadStatsTxOverruns, "tx_overruns", " tx_overruns "},
	} {
		if !tb.Has(f.t) {
			continue
		}
		if !r.kindNeed(xtcpnl.TcKindTaprio, f.key, tb.Bytes(f.t), 8) {
			return
		}
		v, _ := tb.U64(f.t)
		r.o.u64(printAny, f.key, f.txt+u64s(v), v)
	}
}
