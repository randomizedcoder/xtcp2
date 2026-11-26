package gotc

import (
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// tc/m_vlan.c print_vlan (iproute2 v7.2.0).

// vlanActionNames is m_vlan.c action_names, TCA_VLAN_ACT_POP (1) to
// TCA_VLAN_ACT_PUSH_ETH (5).
var vlanActionNames = [...]string{"", "pop", "push", "modify", "pop_eth", "push_eth"}

// The tc_vlan v_action values print_vlan switches on.
const (
	tcaVlanActPush    = 2
	tcaVlanActModify  = 3
	tcaVlanActPushEth = 5
	ethAlenCst        = 6
)

// actName is action_names[v]: tc indexes its table unchecked, so a value
// with no name reads past it (or prints a NULL); gotc refuses (AC20).
func actName(kind string, names []string, v uint32) (string, error) {
	if v == 0 || int(v) >= len(names) {
		return "", fmt.Errorf("%w: %s action %d (tc reads it unchecked)", errNotImplemented, kind, v)
	}
	return names[v], nil
}

// printVlan is print_vlan.
func (r *runner) printVlan(opt []byte, has bool) (int, error) {
	o := r.o
	o.str(printAny, colorNone, "kind", "vlan ", xtcpnl.TcActKindVlan)
	if !has {
		return 0, nil
	}
	r.nested(opt, xtcpnl.TcaVlanMaxKnown)
	v, err := xtcpnl.ParseTcVlan(opt[:validAttrPrefix(opt)])
	if err != nil {
		return 0, err
	}
	if !v.HasParms {
		r.errf("Missing vlan parameters\n")
		return -1, nil
	}
	if v.ParmsShort(xtcpnl.TcVlanSizeCst) {
		return 0, actShort(xtcpnl.TcActKindVlan, v.ParmsLen)
	}
	name, err := actName(xtcpnl.TcActKindVlan, vlanActionNames[:], v.P.V)
	if err != nil {
		return 0, err
	}
	o.str(printAny, colorNone, "vlan_action", " "+name, name)
	tb := v.Tb
	switch v.P.V {
	case tcaVlanActPush, tcaVlanActModify:
		if tb.Has(xtcpnl.TcaVlanPushVlanID) {
			id := uint32(rtaU16(tb.Bytes(xtcpnl.TcaVlanPushVlanID)))
			o.uint(printAny, "id", " id "+u32s(id), id)
		}
		if tb.Has(xtcpnl.TcaVlanPushVlanProtocol) {
			p := r.llProtoN2A(rtaU16(tb.Bytes(xtcpnl.TcaVlanPushVlanProtocol)))
			o.str(printAny, colorNone, "protocol", " protocol "+p, p)
		}
		if tb.Has(xtcpnl.TcaVlanPushVlanPriority) {
			pr := uint32(rtaU8(tb.Bytes(xtcpnl.TcaVlanPushVlanPriority)))
			o.uint(printAny, wordPriority, " priority "+u32s(pr), pr)
		}
	case tcaVlanActPushEth:
		for _, m := range []struct {
			t   uint16
			key string
		}{{xtcpnl.TcaVlanPushEthDst, "dst_mac"}, {xtcpnl.TcaVlanPushEthSrc, "src_mac"}} {
			if b := tb.Bytes(m.t); tb.Has(m.t) && len(b) == ethAlenCst {
				a := llAddrN2A(b)
				o.str(printAny, colorNone, m.key, " "+m.key+" "+a, a)
			}
		}
	}
	r.printActionControl(" ", v.P.Action, "")
	o.nl()
	r.actTail(v.P.TcGen, "\t index ", v.TcActCommon)
	o.nl()
	return 0, nil
}
