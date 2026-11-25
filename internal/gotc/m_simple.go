package gotc

import (
	"bytes"
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/m_simple.c print_simple (iproute2 v7.2.0).

// printSimple is print_simple: no kind at all, in text or JSON (AC22), and
// its own index/ref/bind spacing (AC26). sdata is printed "%s" with no NUL
// check: one without a NUL is refused (AC21).
func (r *runner) printSimple(opt []byte, has bool) (int, error) {
	o := r.o
	if !has {
		return 0, nil
	}
	r.nested(opt, xtcpnl.TcaDefMaxKnown)
	s, err := xtcpnl.ParseTcSimple(opt[:validAttrPrefix(opt)])
	if err != nil {
		return 0, err
	}
	if !s.HasParms {
		r.errf("Missing simple parameters\n")
		return -1, nil
	}
	if s.ParmsShort(xtcpnl.TcGactSizeCst) {
		return 0, actShort(xtcpnl.TcActKindSimple, s.ParmsLen)
	}
	if !s.Tb.Has(xtcpnl.TcaDefData) {
		r.errf("Missing simple string\n")
		return -1, nil
	}
	d := s.Tb.Bytes(xtcpnl.TcaDefData)
	i := bytes.IndexByte(d, 0)
	if i < 0 {
		return 0, fmt.Errorf("%w: simple sdata of %d bytes with no NUL (tc reads it unchecked)", errNotImplemented, len(d))
	}
	sd := string(d[:i])
	o.str(printAny, colorNone, "simple", "Simple <"+sd+">", sd)
	o.nl()
	r.actTailSpaced(s.P, s.TcActCommon)
	o.nl()
	return 0, nil
}
