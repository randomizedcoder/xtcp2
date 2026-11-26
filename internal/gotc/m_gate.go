package gotc

import (
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/m_gate.c print_gate and print_gate_list, with tc/tc_util.c
// get_clock_name (iproute2 v7.2.0).

// clockidInvalid is tc_util.h CLOCKID_INVALID.
const clockidInvalid = -1

// getClockName is get_clock_name over clockt_map (glibc's CLOCK_*): an id
// not in it, CLOCKID_INVALID included, is "invalid" (AC62).
func getClockName(id int32) string {
	switch id {
	case 7:
		return "BOOTTIME"
	case 1:
		return "MONOTONIC"
	case 0:
		return "REALTIME"
	case 11:
		return "TAI"
	}
	return "invalid"
}

// gateChecks is each top-level attribute print_gate reads unchecked, and
// its width.
var gateChecks = []struct {
	t    uint16
	what string
	n    int
}{
	{xtcpnl.TcaGatePriority, wordPriority, 4},
	{xtcpnl.TcaGateClockid, "clockid", 4},
	{xtcpnl.TcaGateFlags, "flags", 4},
	{xtcpnl.TcaGateBaseTime, "base-time", 8},
	{xtcpnl.TcaGateCycleTime, "cycle-time", 8},
	{xtcpnl.TcaGateCycleTimeExt, "cycle-time-ext", 8},
}

// printGate is print_gate: no kind in text or JSON, and no options is -1
// (AC59); hard "\n"s even under -o, and "%-8d" and "%-10u" pad the text
// with trailing spaces (AC61); the s32 ipv goes through "%u", the s32
// max-octets through print_size and the s64 times through print_lluint
// (AC60).
func (r *runner) printGate(opt []byte, has bool) (int, error) {
	o := r.o
	const kind = xtcpnl.TcActKindGate
	if !has {
		return -1, nil
	}
	r.nested(opt, xtcpnl.TcaGateMaxKnown)
	g, err := xtcpnl.ParseTcGate(opt[:validAttrPrefix(opt)])
	if err != nil {
		return 0, err
	}
	if !g.HasParms {
		r.errf("Missing gate parameters\n")
		return -1, nil
	}
	if g.ParmsShort(xtcpnl.TcGateSizeCst) {
		return 0, actShort(kind, g.ParmsLen)
	}
	tb := g.Tb
	for _, c := range gateChecks {
		if err := attrNeed(kind, c.what, tb.Bytes(c.t), c.n); err != nil {
			return 0, err
		}
	}
	for i, e := range g.Entries {
		if e.Short {
			return 0, actUnchecked(kind, fmt.Sprintf("schedule entry %d with a short field", i))
		}
	}
	o.fp("\n")
	prio := int32(-1)
	if b := tb.Bytes(xtcpnl.TcaGatePriority); b != nil {
		prio = cint.Int(rtaU32(b))
	}
	if prio != -1 {
		o.int(printAny, wordPriority, fmt.Sprintf("\tpriority %-8d", prio), prio)
	} else {
		o.fp("\tpriority wildcard")
		o.int(printJSON, wordPriority, "", prio)
	}
	clockid := int32(clockidInvalid)
	if b := tb.Bytes(xtcpnl.TcaGateClockid); b != nil {
		clockid = cint.Int(rtaU32(b))
	}
	cn := getClockName(clockid)
	o.str(printAny, colorNone, "clockid", "\tclockid "+cn, cn)
	if b := tb.Bytes(xtcpnl.TcaGateFlags); b != nil {
		f := rtaU32(b)
		o.hex0x(printAny, "flags", "\tflags "+cHashX(uint64(f)), uint64(f))
	}
	o.fp("\n")
	for _, t := range []struct {
		a         uint16
		key, word string
	}{
		{xtcpnl.TcaGateBaseTime, "base_time", "base-time"},
		{xtcpnl.TcaGateCycleTime, "cycle_time", "cycle-time"},
		{xtcpnl.TcaGateCycleTimeExt, "cycle_time_ext", "cycle-time-ext"},
	} {
		var v int64
		if b := tb.Bytes(t.a); b != nil {
			v = cint.LongLong(rtaU64(b))
		}
		o.fp("\t" + t.word + " " + sprintTime64(v))
		o.u64(printJSON, t.key, "", cint.U64Bits(v))
	}
	if g.HasList {
		r.gateList(g)
	}
	r.printActionControl("\t", g.P.Action, "")
	r.actTail(g.P, "\n\t index ", g.TcActCommon)
	o.fp("\n")
	return 0, nil
}

// gateList is print_gate_list.
func (r *runner) gateList(g xtcpnl.TcGateOpt) {
	o := r.o
	r.gateEntryDeficits(g.Tb.Bytes(xtcpnl.TcaGateEntryList))
	o.fp(o.sl)
	o.fp("\tschedule:" + o.sl)
	o.openArray(printJSON, "schedule")
	for _, e := range g.Entries {
		o.openObject("")
		o.uint(printAny, "number", fmt.Sprintf("\t number %4d", e.Index), e.Index)
		st := "close"
		if e.Gate {
			st = "open"
		}
		o.str(printAny, colorNone, "gate_state", "\tgate-state "+st+" ", st)
		o.uint(printJSON, "interval", "", e.Interval)
		o.fp("\tinterval " + sprintTime64(int64(e.Interval)))
		if e.Ipv != -1 {
			u := cint.Bits(e.Ipv)
			o.uint(printAny, "ipv", fmt.Sprintf("\t ipv %-10d", u), u)
		} else {
			o.int(printJSON, "ipv", "", e.Ipv)
			o.fp("\t ipv wildcard")
		}
		if e.MaxOctets != -1 {
			o.size(printAny, "max_octets", "\t max-octets ", "", cint.U64Bits(int64(e.MaxOctets)))
		} else {
			o.fp("\t max-octets wildcard")
			o.int(printJSON, "max_octets", "", e.MaxOctets)
		}
		o.closeObject()
		o.fp(o.sl)
	}
	o.closeArray(printAny, "")
}

// gateEntryDeficits is print_gate_list's parse_rtattr_nested of each
// entry, for the "!!!Deficit" line a malformed one prints.
func (r *runner) gateEntryDeficits(list []byte) {
	n := validAttrPrefix(list)
	for off := 0; off+xtcpnl.RTAttrSizeCst <= n; {
		l := int(attrLenAt(list, off))
		r.nested(list[off+xtcpnl.RTAttrSizeCst:off+l], xtcpnl.TcaGateEntryMaxKnown)
		off += (l + 3) &^ 3
	}
}
