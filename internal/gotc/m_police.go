package gotc

import (
	"encoding/binary"
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/m_police.c print_police and tc_print_police (iproute2 v7.2.0).

// rtaU64 is rta_getattr_u64 on an attribute that may be short.
func rtaU64(b []byte) uint64 { return binary.LittleEndian.Uint64(rta(b, 8)) }

// actShort is the refusal for a parms struct shorter than tc's cast: C
// reads past it (AC21).
func actShort(kind string, n int) error {
	return fmt.Errorf("%w: %s parameters of %d bytes (tc reads it unchecked)", errNotImplemented, kind, n)
}

// policeAttrU64 is a u64 police attribute tc reads only when its payload
// holds one.
func policeAttrU64(tb xtcpnl.AttrTable, t uint16) (uint64, bool) {
	if !tb.Has(t) || len(tb.Bytes(t)) < 8 {
		return 0, false
	}
	return rtaU64(tb.Bytes(t)), true
}

// printPolice is print_police: the rates, burst, mtu and avrate print in
// text only (AC14); a pps-only policer prints a rate of 0 (AC16); with
// TCA_POLICE_RESULT print_action_control runs twice, so JSON has
// "control_action" twice (AC15). tc checks TBF's length.
func (r *runner) printPolice(opt []byte, has bool) (int, error) {
	o := r.o
	o.str(printJSON, colorNone, "kind", "", xtcpnl.TcActKindPolice)
	if !has {
		return 0, nil
	}
	tb := r.nested(opt, xtcpnl.TcaPoliceMaxKnown)
	if !tb.Has(xtcpnl.TcaPoliceTbf) {
		r.errf("[NULL police tbf]")
		return -1, nil
	}
	if len(tb.Bytes(xtcpnl.TcaPoliceTbf)) < xtcpnl.TcPoliceSizeCst {
		r.errf("[truncated police tbf]")
		return -1, nil
	}
	po, err := xtcpnl.ParseTcPolice(opt[:validAttrPrefix(opt)])
	if err != nil {
		return 0, err
	}
	p := po.P
	rate := uint64(p.Rate.Rate)
	if v, ok := policeAttrU64(tb, xtcpnl.TcaPoliceRate64); ok {
		rate = v
	}
	o.fp(" police 0x" + u32hex(p.Index) + " ")
	o.uint(printJSON, "index", "", p.Index)
	o.rate(r.iec > 0, printFP, "", "rate ", " ", rate)
	o.size(printFP, "", "burst ", " ", r.xmitsize(rate, p.Burst))
	o.size(printFP, "", "mtu ", " ", uint64(p.Mtu))
	if r.raw > 0 {
		o.fp(fmt.Sprintf("[%08x] ", p.Burst))
	}
	prate := uint64(p.PeakRate.Rate)
	if v, ok := policeAttrU64(tb, xtcpnl.TcaPolicePeakrate64); ok {
		prate = v
	}
	if prate != 0 {
		o.rate(r.iec > 0, printFP, "", "peakrate ", " ", prate)
	}
	if tb.Has(xtcpnl.TcaPoliceAvrate) {
		o.rate(r.iec > 0, printFP, "", "avrate ", " ", uint64(attrU32(tb, xtcpnl.TcaPoliceAvrate)))
	}
	pps, okRate := policeAttrU64(tb, xtcpnl.TcaPolicePktrate64)
	ppsBurst, okBurst := policeAttrU64(tb, xtcpnl.TcaPolicePktburst64)
	if okRate && okBurst {
		// tc_calc_xmitsize's ticks are an unsigned int.
		b := r.xmitsize(pps, cint.Low32(ppsBurst))
		o.u64(printAny, "pkts_rate", "pkts_rate "+u64s(pps)+" ", pps)
		o.u64(printAny, "pkts_burst", "pkts_burst "+u64s(b)+" ", b)
	}
	r.printActionControl("action ", p.Action, "")
	if tb.Has(xtcpnl.TcaPoliceResult) {
		r.printActionControl("/", cint.Int(attrU32(tb, xtcpnl.TcaPoliceResult)), " ")
	} else {
		o.fp(" ")
	}
	o.size(printAny, "overhead", "overhead ", " ", uint64(p.Rate.Overhead))
	if ll := p.Rate.Linklayer & tcLinklayerMask; ll > 1 || r.details > 0 {
		s := sprintLinklayer(ll)
		o.str(printAny, colorNone, "linklayer", "linklayer "+s+" ", s)
	}
	o.nl()
	o.int(printAny, "ref", "\tref "+i32s(p.Refcnt)+" ", p.Refcnt)
	o.int(printAny, "bind", "bind "+i32s(p.Bindcnt)+" ", p.Bindcnt)
	if r.stats > 0 && po.HasTm {
		r.printTm(po.Tm)
	}
	o.nl()
	return 0, nil
}
