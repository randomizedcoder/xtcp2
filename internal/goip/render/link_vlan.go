package render

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is vlan_print_opt (ip/iplink_vlan.c, iproute2 v7.2.0) and the
// vlan_print_flags and vlan_print_map it calls: the tokens `ip -d link
// show` prints after `    vlan ` (KG2-T04, docs/netlink/goip-link-kinds.md).
//
// Without an id nothing prints, and `-j` gets an empty info_data. A
// missing protocol prints "802.1q", lower case, where ll_proto_n2a would
// print 802.1Q. The flags print in xtcpnl.VlanFlags' order, which is
// vlan_print_flags' and not the bits' (MVRP, 0x8, before LOOSE_BINDING,
// 0x4), then any bits left over in hex. Each qos map is print_nl, so its
// own line, and a JSON array of {from, to}.
//
// ip also stops when the protocol or the flags attribute is shorter than
// its type; the decoder drops a short attribute, so goip prints on past
// it. The kernel never sends one.

// VlanKindCst is the vlan kind, exported for the `-d` gate.
const VlanKindCst = "vlan"

// vlanQos is one ifla_vlan_qos_mapping in `-j`.
type vlanQos struct {
	From uint32 `json:"from"`
	To   uint32 `json:"to"`
}

// vlanData is vlan_print_opt over the decoded IFLA_INFO_DATA.
func vlanData(v *xtcpnl.VlanInfo) *KindData {
	k := &KindData{}
	if v == nil || !v.HasID {
		return k
	}
	proto := "802.1q"
	if v.HasProtocol {
		proto = xtcpnl.LLProtoN2A(v.Protocol)
	}
	k.add("protocol", "protocol "+proto+" ", proto)
	k.add("id", fmt.Sprintf("id %d ", v.ID), v.ID)
	if v.HasFlags {
		names := vlanFlagNames(v.Flags)
		k.add("flags", "<"+strings.Join(names, ",")+"> ", names)
	}
	if v.HasIngress {
		vlanQosMap(k, "ingress_qos", "ingress-qos-map", v.Ingress)
	}
	if v.HasEgress {
		vlanQosMap(k, "egress_qos", "egress-qos-map", v.Egress)
	}
	return k
}

// vlanFlagNames is vlan_print_flags' list: each named flag set, in print
// order, then the bits no name took as one "%x" entry.
func vlanFlagNames(flags uint32) []string {
	names := []string{}
	for _, f := range xtcpnl.VlanFlags {
		if flags&f.Bit != 0 {
			flags &^= f.Bit
			names = append(names, f.Name)
		}
	}
	if flags != 0 {
		names = append(names, strconv.FormatUint(uint64(flags), 16))
	}
	return names
}

// vlanQosMap is vlan_print_map: print_nl, then `      <name> { ` and a
// `from:to ` per entry and `} `, and the entries as a JSON array.
func vlanQosMap(k *KindData, key, name string, m []xtcpnl.VlanQosMapping) {
	k.addText("\n")
	var b strings.Builder
	b.WriteString("      " + name + " { ")
	entries := make([]vlanQos, 0, len(m))
	for _, e := range m {
		fmt.Fprintf(&b, "%d:%d ", e.From, e.To)
		entries = append(entries, vlanQos(e))
	}
	b.WriteString("} ")
	k.add(key, b.String(), entries)
}
