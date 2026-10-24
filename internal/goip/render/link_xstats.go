package render

import (
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file renders `ip link xstats type bridge` and `type bridge_slave`:
// bridge_print_stats_attr (ip/iplink_bridge.c:1001-1031) over one record's
// LINK_XSTATS_TYPE_BRIDGE nest, the same callbacks `ip stats show` reaches
// through its leaf table but laid out as one stanza per device: the name
// padded to 16, then the mcast block, then a port's stp block. The vlan
// bodies have no case there and print nothing.

// Link xstats selectors, bridge_parse_xstats (ip/iplink_bridge.c:1059-1080):
// `igmp` and `mcast` both pick the mcast body, `stp` the stp body. Each is
// strcmp and a later one overrides an earlier one.
const (
	LinkXstatsIgmpCst  = "igmp"
	LinkXstatsMcastCst = "mcast"
	LinkXstatsStpCst   = "stp"
)

// LinkXstatsView is one device's bridge xstats stanza. Mcast and Stp are nil
// when the body is absent or deselected.
type LinkXstatsView struct {
	Ifindex uint32
	IfName  string
	Mcast   *xtcpnl.BrMcastStats
	Stp     *xtcpnl.BridgeStpXstats
}

// LinkXstatsViewOf builds the stanza for one xstats nest, the master group's
// or the port group's, keeping the body sel names ("" keeps both). ok is false
// when the nest carries no LINK_XSTATS_TYPE_BRIDGE, which
// bridge_print_stats_attr returns on before printing anything, the name
// included.
func LinkXstatsViewOf(ifindex uint32, x *xtcpnl.XstatsBodies, sel string, names NameTab) (v LinkXstatsView, ok bool) {
	if !x.HasBridgeXstats {
		return LinkXstatsView{}, false
	}
	v = LinkXstatsView{Ifindex: ifindex, IfName: names.IndexToName(int32(ifindex))}
	if x.HasBridgeMcast && sel != LinkXstatsStpCst {
		m := x.BridgeMcast
		v.Mcast = &m
	}
	if x.HasBridgeStp && (sel == "" || sel == LinkXstatsStpCst) {
		s := x.BridgeStp
		v.Stp = &s
	}
	return v, true
}

// Text is the stanza, in the kernel's body order: mcast, then stp
// (br_fill_linkxstats, net/bridge/br_netlink.c).
func (v LinkXstatsView) Text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%-16s\n", v.IfName)
	if v.Mcast != nil {
		b.WriteString(bridgeMcastText(*v.Mcast))
	}
	if v.Stp != nil {
		b.WriteString(bridgeStpText(*v.Stp))
	}
	return b.String()
}

// bridgeStpText is bridge_print_stats_stp (ip/iplink_bridge.c:954-980).
func bridgeStpText(s xtcpnl.BridgeStpXstats) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s    STP BPDU:  RX: %d TX: %d\n", sp16, s.RxBpdu, s.TxBpdu)
	fmt.Fprintf(&b, "%s    STP TCN:   RX: %d TX: %d\n", sp16, s.RxTcn, s.TxTcn)
	fmt.Fprintf(&b, "%s    STP Transitions: Blocked: %d Forwarding: %d\n", sp16, s.TransitionBlk, s.TransitionFwd)
	return b.String()
}

// linkXstatsJSON is the stanza's object: "ifname", then the "multicast" and
// "stp" objects in print order.
type linkXstatsJSON struct {
	Ifname    string           `json:"ifname"`
	Multicast *bridgeMcastJSON `json:"multicast,omitempty"`
	Stp       *bridgeStpJSON   `json:"stp,omitempty"`
}

// bridgeStpJSON is bridge_print_stats_stp's keys, in its print order.
type bridgeStpJSON struct {
	RxBpdu        uint64 `json:"rx_bpdu"`
	TxBpdu        uint64 `json:"tx_bpdu"`
	RxTcn         uint64 `json:"rx_tcn"`
	TxTcn         uint64 `json:"tx_tcn"`
	TransitionBlk uint64 `json:"transition_blk"`
	TransitionFwd uint64 `json:"transition_fwd"`
}

// LinkXstatsJSON is the object the view encodes to under -j, one element of
// the array new_json_obj opens.
func (v LinkXstatsView) LinkXstatsJSON() any {
	j := linkXstatsJSON{Ifname: v.IfName}
	if v.Mcast != nil {
		j.Multicast = bridgeMcastJSONOf(*v.Mcast)
	}
	if v.Stp != nil {
		s := v.Stp
		j.Stp = &bridgeStpJSON{s.RxBpdu, s.TxBpdu, s.RxTcn, s.TxTcn, s.TransitionBlk, s.TransitionFwd}
	}
	return j
}
