package gotc

import (
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/m_skbmod.c print_skbmod (iproute2 v7.2.0).

// llAddrMaxCst is the longest address ll_addr_n2a writes whole into its
// SPRINT_BUF (64 bytes: "%02x" and 20 ":%02x").
const llAddrMaxCst = 21

// printSkbmod is print_skbmod: no kind key (AC22); etype is a u64 given
// to "%X", which prints its low 32 bits on x86-64 (AC24); the break after
// etype depends on etype alone, as has_optional is tested before dmac and
// smac set it (AC25); and the line ends in a hard "\n", even under -o
// (AC26).
func (r *runner) printSkbmod(opt []byte, has bool) (int, error) {
	o := r.o
	if !has {
		return 0, nil
	}
	r.nested(opt, xtcpnl.TcaSkbmodMaxKnown)
	s, err := xtcpnl.ParseTcSkbmod(opt[:validAttrPrefix(opt)])
	if err != nil {
		return 0, err
	}
	if !s.HasParms {
		r.errf("Missing skbmod parameters\n")
		return -1, nil
	}
	if s.ParmsShort(xtcpnl.TcSkbmodSizeCst) {
		return 0, actShort(xtcpnl.TcActKindSkbmod, s.ParmsLen)
	}
	tb := s.Tb
	o.fp("skbmod ")
	r.printActionControl("", s.P.Action, " ")
	if tb.Has(xtcpnl.TcaSkbmodEtype) {
		e := rtaU16(tb.Bytes(xtcpnl.TcaSkbmodEtype))
		o.hex0x(printAny, "etype", fmt.Sprintf("set etype 0x%X ", e), uint64(e))
		o.fp(o.sl + "\t ")
	}
	for _, m := range []struct {
		t   uint16
		key string
	}{{xtcpnl.TcaSkbmodDmac, "dmac"}, {xtcpnl.TcaSkbmodSmac, "smac"}} {
		if !tb.Has(m.t) {
			continue
		}
		b := tb.Bytes(m.t)
		if len(b) == 0 || len(b) > llAddrMaxCst {
			return 0, fmt.Errorf("%w: skbmod %s of %d bytes (tc reads it unchecked)", errNotImplemented, m.key, len(b))
		}
		a := llAddrN2A(b)
		o.str(printAny, colorMac, m.key, "set "+m.key+" "+a+" ", a)
	}
	if s.P.Flags&xtcpnl.SkbmodFSwapmac != 0 {
		o.null(printAny, "swapmac", "swap mac ")
	}
	if s.P.Flags&xtcpnl.SkbmodFEcn != 0 {
		o.null(printAny, "ecn", "ecn ")
	}
	o.nl()
	r.actTailSpaced(s.P.TcGen, s.TcActCommon)
	o.fp("\n")
	return 0, nil
}
