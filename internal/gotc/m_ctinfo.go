package gotc

import (
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/m_ctinfo.c print_ctinfo and print_ctinfo_stats (iproute2 v7.2.0).

// cHash010x is C's "%#010llx": 0 is ten zeros (no prefix), else "0x" and
// the hex digits zero-padded to ten characters in all.
func cHash010x(v uint64) string {
	if v == 0 {
		return "0000000000"
	}
	h := strconv.FormatUint(v, 16)
	if len(h) < 8 {
		h = strings.Repeat("0", 8-len(h)) + h
	}
	return "0x" + h
}

// ctinfoMask is one of print_ctinfo's u32 masks: read only when its
// payload holds one, else the bracketed note in text (AC27).
func (r *runner) ctinfoMask(tb xtcpnl.AttrTable, t uint16, def uint32, what string) uint32 {
	if !tb.Has(t) {
		return def
	}
	if len(tb.Bytes(t)) >= 4 {
		return attrU32(tb, t)
	}
	r.o.fp("[invalid " + what + " parameter]")
	return def
}

// printCtinfo is print_ctinfo: its notes go to stdout with no newline
// (AC27), and the dscp statemask prints with no label (AC28).
func (r *runner) printCtinfo(opt []byte, has bool) (int, error) {
	o := r.o
	o.str(printAny, colorNone, "kind", "ctinfo ", xtcpnl.TcActKindCtinfo)
	if !has {
		return 0, nil
	}
	r.nested(opt, xtcpnl.TcaCtinfoMaxKnown)
	c, err := xtcpnl.ParseTcCtinfo(opt[:validAttrPrefix(opt)])
	if err != nil {
		return 0, err
	}
	if !c.HasParms {
		o.fp("[NULL ctinfo action parameters]")
		return -1, nil
	}
	if c.ParmsShort(xtcpnl.TcGactSizeCst) {
		return 0, actShort(xtcpnl.TcActKindCtinfo, c.ParmsLen)
	}
	tb := c.Tb
	dscp := r.ctinfoMask(tb, xtcpnl.TcaCtinfoParmsDscpMask, 0, "dscp mask")
	state := r.ctinfoMask(tb, xtcpnl.TcaCtinfoParmsDscpStatemask, 0, "dscp statemask")
	cpmark := r.ctinfoMask(tb, xtcpnl.TcaCtinfoParmsCpmarkMask, ^uint32(0), "cpmark mask")
	var zone uint32
	if tb.Has(xtcpnl.TcaCtinfoZone) && len(tb.Bytes(xtcpnl.TcaCtinfoZone)) >= 2 {
		zone = uint32(rtaU16(tb.Bytes(xtcpnl.TcaCtinfoZone)))
	}
	o.uint(printAny, "zone", "zone "+u32s(zone), zone)
	r.printActionControl(" ", c.P.Action, "")
	o.nl()
	r.actTailFmt(c.P, xtcpnl.TcActCommon{}, "\t index ", "", " ref ", "", " bind ")
	if tb.Has(xtcpnl.TcaCtinfoParmsDscpMask) {
		o.hex0x(printAny, "dscpmask", " dscp "+cHash010x(uint64(dscp)), uint64(dscp))
		o.hex0x(printAny, "dscpstatemask", " "+cHash010x(uint64(state)), uint64(state))
	}
	if tb.Has(xtcpnl.TcaCtinfoParmsCpmarkMask) {
		o.hex0x(printAny, "cpmark", " cpmark "+cHash010x(uint64(cpmark)), uint64(cpmark))
	}
	if r.stats > 0 {
		if c.HasTm {
			r.printTm(c.Tm)
		}
		for _, s := range []struct {
			t        uint16
			key, txt string
		}{
			{xtcpnl.TcaCtinfoStatsDscpSet, "dscpset", " DSCP set "},
			{xtcpnl.TcaCtinfoStatsDscpError, "dscperror", " error "},
			{xtcpnl.TcaCtinfoStatsCpmarkSet, "cpmarkset", " CPMARK set "},
		} {
			if tb.Has(s.t) {
				v := rtaU64(tb.Bytes(s.t))
				o.u64(printAny, s.key, s.txt+u64s(v), v)
			}
		}
	}
	o.nl()
	return 0, nil
}
