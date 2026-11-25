package gotc

import "github.com/randomizedcoder/xtcp2/pkg/xtcpnl"

// tc/m_sample.c print_sample (iproute2 v7.2.0).

// printSample is print_sample: PARMS, RATE and PSAMPLE_GROUP are all
// required.
func (r *runner) printSample(opt []byte, has bool) (int, error) {
	o := r.o
	o.str(printAny, colorNone, "kind", "sample ", xtcpnl.TcActKindSample)
	if !has {
		return 0, nil
	}
	r.nested(opt, xtcpnl.TcaSampleMaxKnown)
	s, err := xtcpnl.ParseTcSample(opt[:validAttrPrefix(opt)])
	if err != nil {
		return 0, err
	}
	tb := s.Tb
	if !s.HasParms || !tb.Has(xtcpnl.TcaSampleRate) || !tb.Has(xtcpnl.TcaSamplePsampleGroup) {
		r.errf("Missing sample parameters\n")
		return -1, nil
	}
	if s.ParmsShort(xtcpnl.TcGactSizeCst) {
		return 0, actShort(xtcpnl.TcActKindSample, s.ParmsLen)
	}
	rate := attrU32(tb, xtcpnl.TcaSampleRate)
	o.uint(printAny, "rate", "rate 1/"+u32s(rate)+" ", rate)
	g := attrU32(tb, xtcpnl.TcaSamplePsampleGroup)
	o.uint(printAny, "group", "group "+u32s(g), g)
	if tb.Has(xtcpnl.TcaSampleTruncSize) {
		t := attrU32(tb, xtcpnl.TcaSampleTruncSize)
		o.uint(printAny, "trunc_size", " trunc_size "+u32s(t), t)
	}
	r.printActionControl(" ", s.P.Action, "")
	o.nl()
	r.actTail(s.P, "\t index ", s.TcActCommon)
	o.nl()
	return 0, nil
}
