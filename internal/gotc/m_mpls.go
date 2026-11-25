package gotc

import "github.com/randomizedcoder/xtcp2/pkg/xtcpnl"

// tc/m_mpls.c print_mpls (iproute2 v7.2.0).

// mplsActionNames is m_mpls.c action_names, TCA_MPLS_ACT_POP (1) to
// TCA_MPLS_ACT_MAC_PUSH (5).
var mplsActionNames = [...]string{"", "pop", "push", "modify", "dec_ttl", "mac_push"}

// The tc_mpls m_action values print_mpls switches on.
const (
	tcaMplsActPop     = 1
	tcaMplsActPush    = 2
	tcaMplsActModify  = 3
	tcaMplsActMacPush = 5
)

// printMpls is print_mpls: the protocol prints for pop, push and mac_push
// only, and dec_ttl prints no fields (AC30).
func (r *runner) printMpls(opt []byte, has bool) (int, error) {
	o := r.o
	o.str(printAny, colorNone, "kind", "mpls ", xtcpnl.TcActKindMpls)
	if !has {
		return 0, nil
	}
	r.nested(opt, xtcpnl.TcaMplsMaxKnown)
	m, err := xtcpnl.ParseTcMpls(opt[:validAttrPrefix(opt)])
	if err != nil {
		return 0, err
	}
	if !m.HasParms {
		r.errf("[NULL mpls parameters]\n")
		return -1, nil
	}
	if m.ParmsShort(xtcpnl.TcMplsSizeCst) {
		return 0, actShort(xtcpnl.TcActKindMpls, m.ParmsLen)
	}
	name, err := actName(xtcpnl.TcActKindMpls, mplsActionNames[:], m.P.V)
	if err != nil {
		return 0, err
	}
	o.str(printAny, colorNone, "mpls_action", " "+name, name)
	tb := m.Tb
	proto := func() {
		if tb.Has(xtcpnl.TcaMplsProto) {
			p := r.llProtoN2A(rtaU16(tb.Bytes(xtcpnl.TcaMplsProto)))
			o.str(printAny, colorNone, "protocol", " protocol "+p, p)
		}
	}
	switch m.P.V {
	case tcaMplsActPop:
		proto()
	case tcaMplsActPush, tcaMplsActMacPush, tcaMplsActModify:
		if m.P.V != tcaMplsActModify {
			proto()
		}
		if tb.Has(xtcpnl.TcaMplsLabel) {
			v := attrU32(tb, xtcpnl.TcaMplsLabel)
			o.uint(printAny, "label", " label "+u32s(v), v)
		}
		for _, f := range []struct {
			t   uint16
			key string
		}{{xtcpnl.TcaMplsTC, "tc"}, {xtcpnl.TcaMplsBos, "bos"}, {xtcpnl.TcaMplsTTL, "ttl"}} {
			if tb.Has(f.t) {
				v := uint32(rtaU8(tb.Bytes(f.t)))
				o.uint(printAny, f.key, " "+f.key+" "+u32s(v), v)
			}
		}
	}
	r.printActionControl(" ", m.P.Action, "")
	o.nl()
	r.actTail(m.P.TcGen, "\t index ", m.TcActCommon)
	o.nl()
	return 0, nil
}
