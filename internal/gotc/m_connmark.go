package gotc

import "github.com/randomizedcoder/xtcp2/pkg/xtcpnl"

// tc/m_connmark.c print_connmark (iproute2 v7.2.0).

// printConnmark is print_connmark.
func (r *runner) printConnmark(opt []byte, has bool) (int, error) {
	o := r.o
	o.str(printAny, colorNone, "kind", "connmark ", xtcpnl.TcActKindConnmark)
	if !has {
		return 0, nil
	}
	r.nested(opt, xtcpnl.TcaConnmarkMaxKnown)
	c, err := xtcpnl.ParseTcConnmark(opt[:validAttrPrefix(opt)])
	if err != nil {
		return 0, err
	}
	if !c.HasParms {
		r.errf("Missing connmark parameters\n")
		return -1, nil
	}
	if c.ParmsShort(xtcpnl.TcConnmarkSizeCst) {
		return 0, actShort(xtcpnl.TcActKindConnmark, c.ParmsLen)
	}
	z := uint32(c.P.Zone)
	o.uint(printAny, "zone", "zone "+u32s(z), z)
	r.printActionControl(" ", c.P.Action, "")
	o.nl()
	r.actTail(c.P.TcGen, "\t index ", c.TcActCommon)
	o.nl()
	return 0, nil
}
