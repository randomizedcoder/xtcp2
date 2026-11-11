package render

import (
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// The bodies `ip stats show` prints through ipstats_process_ifsm
// (ip/ipstats.c, iproute2 7.3.0), each a transcription of its print_*
// sequence onto a Printer.

// The column headings and JSON keys every table here shares.
const (
	statsPacketsCst = "packets"
	statsErrorsCst  = "errors"
	statsDroppedCst = "dropped"
)

// RtnlHwStats64 is struct rtnl_hw_stats64, the L3_STATS offload counters.
type RtnlHwStats64 struct {
	RxPackets, TxPackets, RxBytes, TxBytes   uint64
	RxErrors, TxErrors, RxDropped, TxDropped uint64
	Multicast                                uint64
}

// IPStats64 is print_stats64 (ip/ipaddress.c:624) with no carrier_changes
// and no `what`: under -j the rx and tx objects, the error counters only
// when extended (show_stats > 1); in text linkStatsText's block.
func IPStats64(p *Printer, s xtcpnl.RtnlLinkStats64, extended bool) {
	if !p.JSON() {
		p.FPf("%s", linkStatsText(s, extended))
		return
	}
	p.OpenObject("rx")
	for _, f := range []struct {
		key string
		v   uint64
	}{
		{"bytes", s.RxBytes}, {statsPacketsCst, s.RxPackets}, {statsErrorsCst, s.RxErrors},
		{statsDroppedCst, s.RxDropped}, {"over_errors", s.RxOverErrors}, {"multicast", s.Multicast},
	} {
		p.U64(PrintJSON, f.key, "", f.v)
	}
	if s.RxCompressed != 0 {
		p.U64(PrintJSON, "compressed", "", s.RxCompressed)
	}
	if extended {
		p.U64(PrintJSON, "length_errors", "", s.RxLengthErrors)
		p.U64(PrintJSON, "crc_errors", "", s.RxCrcErrors)
		p.U64(PrintJSON, "frame_errors", "", s.RxFrameErrors)
		p.U64(PrintJSON, "fifo_errors", "", s.RxFifoErrors)
		p.U64(PrintJSON, "missed_errors", "", s.RxMissedErrors)
		if s.RxNohandler != 0 {
			p.U64(PrintJSON, "nohandler", "", s.RxNohandler)
		}
		if s.RxOtherhostDropped != 0 {
			p.U64(PrintJSON, "otherhost", "", s.RxOtherhostDropped)
		}
	}
	p.CloseObject()
	p.OpenObject("tx")
	for _, f := range []struct {
		key string
		v   uint64
	}{
		{"bytes", s.TxBytes}, {statsPacketsCst, s.TxPackets}, {statsErrorsCst, s.TxErrors},
		{statsDroppedCst, s.TxDropped}, {"carrier_errors", s.TxCarrierErrors}, {"collisions", s.Collisions},
	} {
		p.U64(PrintJSON, f.key, "", f.v)
	}
	if s.TxCompressed != 0 {
		p.U64(PrintJSON, "compressed", "", s.TxCompressed)
	}
	if extended {
		p.U64(PrintJSON, "aborted_errors", "", s.TxAbortedErrors)
		p.U64(PrintJSON, "fifo_errors", "", s.TxFifoErrors)
		p.U64(PrintJSON, "window_errors", "", s.TxWindowErrors)
		p.U64(PrintJSON, "heartbeat_errors", "", s.TxHeartbeatErrors)
	}
	p.CloseObject()
}

// fiveCols is the five-column table print_hw_stats64 and
// print_mpls_link_stats share: header widths, the fifth's given apart from
// its heading, then size_columns over both rows.
func fiveCols(fifthWidth int, rx [5]uint64, tx [4]uint64) [5]int {
	c := statsCols{len("*X: bytes"), len(statsPacketsCst), len(statsErrorsCst), len(statsDroppedCst), fifthWidth}
	c.size(rx[:]...)
	c.size(tx[0], tx[1], tx[2], tx[3], 0)
	return [5]int{c[0], c[1], c[2], c[3], c[4]}
}

// fiveColsText is the two RX lines and two TX lines, indent before each,
// with no newline after the last.
func fiveColsText(indent, fifth string, fifthWidth int, rx [5]uint64, tx [4]uint64) string {
	cols := fiveCols(fifthWidth, rx, tx)
	var b strings.Builder
	b.WriteString(indent)
	b.WriteString("RX: ")
	writePadded(&b, cols[0]-4, "bytes")
	for i, h := range []string{statsPacketsCst, statsErrorsCst, statsDroppedCst, fifth} {
		b.WriteByte(' ')
		writePadded(&b, cols[i+1], h)
	}
	b.WriteString("\n" + indent)
	for i, v := range rx {
		printNum(&b, cols[i], v)
	}
	b.WriteString("\n" + indent + "TX: ")
	writePadded(&b, cols[0]-4, "bytes")
	for i, h := range []string{statsPacketsCst, statsErrorsCst, statsDroppedCst} {
		b.WriteByte(' ')
		writePadded(&b, cols[i+1], h)
	}
	b.WriteString("\n" + indent)
	for i, v := range tx {
		printNum(&b, cols[i], v)
	}
	return b.String()
}

// writePadded is "%*s": s right-aligned in width.
func writePadded(b *strings.Builder, width int, s string) {
	for i := len(s); i < width; i++ {
		b.WriteByte(' ')
	}
	b.WriteString(s)
}

// IPHwStats64 is print_hw_stats64 (ip/ipstats.c:172), whose mcast column
// is as wide as "overrun".
func IPHwStats64(p *Printer, s RtnlHwStats64) {
	if !p.JSON() {
		p.FPf("%s", fiveColsText("    ", "mcast", len("overrun"),
			[5]uint64{s.RxBytes, s.RxPackets, s.RxErrors, s.RxDropped, s.Multicast},
			[4]uint64{s.TxBytes, s.TxPackets, s.TxErrors, s.TxDropped}))
		return
	}
	p.OpenObject("rx")
	p.U64(PrintJSON, "bytes", "", s.RxBytes)
	p.U64(PrintJSON, statsPacketsCst, "", s.RxPackets)
	p.U64(PrintJSON, statsErrorsCst, "", s.RxErrors)
	p.U64(PrintJSON, statsDroppedCst, "", s.RxDropped)
	p.U64(PrintJSON, "multicast", "", s.Multicast)
	p.CloseObject()
	p.OpenObject("tx")
	p.U64(PrintJSON, "bytes", "", s.TxBytes)
	p.U64(PrintJSON, statsPacketsCst, "", s.TxPackets)
	p.U64(PrintJSON, statsErrorsCst, "", s.TxErrors)
	p.U64(PrintJSON, statsDroppedCst, "", s.TxDropped)
	p.CloseObject()
}

// IPMplsLinkStats is print_mpls_link_stats (ip/iplink.c:1529) with indent.
func IPMplsLinkStats(p *Printer, s xtcpnl.MplsLinkStats, indent string) {
	if !p.JSON() {
		p.FPf("%s", fiveColsText(indent, "noroute", len("noroute"),
			[5]uint64{s.RxBytes, s.RxPackets, s.RxErrors, s.RxDropped, s.RxNoroute},
			[4]uint64{s.TxBytes, s.TxPackets, s.TxErrors, s.TxDropped}))
		return
	}
	p.OpenObject("rx")
	p.U64(PrintJSON, "bytes", "", s.RxBytes)
	p.U64(PrintJSON, statsPacketsCst, "", s.RxPackets)
	p.U64(PrintJSON, statsErrorsCst, "", s.RxErrors)
	p.U64(PrintJSON, statsDroppedCst, "", s.RxDropped)
	p.U64(PrintJSON, "noroute", "", s.RxNoroute)
	p.CloseObject()
	p.OpenObject("tx")
	p.U64(PrintJSON, "bytes", "", s.TxBytes)
	p.U64(PrintJSON, statsPacketsCst, "", s.TxPackets)
	p.U64(PrintJSON, statsErrorsCst, "", s.TxErrors)
	p.U64(PrintJSON, statsDroppedCst, "", s.TxDropped)
	p.CloseObject()
}

// ipstatsPad16 is "%-16s" of "", the IFNAMSIZ column the xstats bodies
// indent by.
const ipstatsPad16 = "                "

// IPBridgeVlan is bridge_print_stats_vlan (ip/iplink_bridge.c:981) and
// bridge_print_vlan_stats (lib/bridge.c:37).
func IPBridgeVlan(p *Printer, v xtcpnl.BridgeVlanXstats) {
	p.FPf("%s  ", ipstatsPad16)
	p.OpenObject("")
	p.U64(PrintAny, "vid", "%d", uint64(v.Vid))
	if v.Flags != 0 {
		p.OpenArray("flags")
		if v.Flags&bridgeVlanInfoPvidCst != 0 {
			p.String(PrintAny, "", " %s", "PVID")
		}
		if v.Flags&bridgeVlanInfoUntaggedCst != 0 {
			p.String(PrintAny, "", " %s", "Egress Untagged")
		}
		p.CloseArray()
	}
	p.NL()
	p.FPf("%s    ", ipstatsPad16)
	p.U64(PrintAny, "rx_bytes", "RX: %d bytes", v.RxBytes)
	p.U64(PrintAny, "rx_packets", " %d packets\n", v.RxPackets)
	p.FPf("%s    ", ipstatsPad16)
	p.U64(PrintAny, "tx_bytes", "TX: %d bytes", v.TxBytes)
	p.U64(PrintAny, "tx_packets", " %d packets\n", v.TxPackets)
	p.CloseObject()
}

// BRIDGE_VLAN_INFO_PVID and _UNTAGGED.
const (
	bridgeVlanInfoPvidCst     = 1 << 1
	bridgeVlanInfoUntaggedCst = 1 << 2
)

// mcastDir is one of br_mcast_stats' [RX, TX] pairs.
type mcastDir = [2]uint64

// IPBridgeMcast is bridge_print_stats_mcast (ip/iplink_bridge.c:850).
func IPBridgeMcast(p *Printer, m xtcpnl.BrMcastStats) {
	p.OpenObject("multicast")
	ipMcastVersions(p, "IGMP queries", "igmp_queries", m.IgmpV1queries, m.IgmpV2queries, &m.IgmpV3queries)
	ipMcastVersions(p, "IGMP reports", "igmp_reports", m.IgmpV1reports, m.IgmpV2reports, &m.IgmpV3reports)
	ipMcastLeaves(p, "IGMP leaves", "igmp_leaves", m.IgmpLeaves)
	p.FPf("%s    IGMP parse errors: ", ipstatsPad16)
	p.U64(PrintAny, "igmp_parse_errors", "%d\n", m.IgmpParseErrors)
	ipMcastVersions(p, "MLD queries", "mld_queries", m.MldV1queries, m.MldV2queries, nil)
	ipMcastVersions(p, "MLD reports", "mld_reports", m.MldV1reports, m.MldV2reports, nil)
	ipMcastLeaves(p, "MLD leaves", "mld_leaves", m.MldLeaves)
	p.FPf("%s    MLD parse errors: ", ipstatsPad16)
	p.U64(PrintAny, "mld_parse_errors", "%d\n", m.MldParseErrors)
	p.CloseObject()
}

// ipMcastVersions is one queries or reports object: an RX and a TX line of
// v1, v2 and, for IGMP, v3.
func ipMcastVersions(p *Printer, title, key string, v1, v2 mcastDir, v3 *mcastDir) {
	p.OpenObject(key)
	p.FPf("%s    %s:\n", ipstatsPad16, title)
	for dir, d := range []string{"rx", "tx"} {
		p.FPf("%s      ", ipstatsPad16)
		p.U64(PrintAny, d+"_v1", strings.ToUpper(d)+": v1 %d ", v1[dir])
		if v3 == nil {
			p.U64(PrintAny, d+"_v2", "v2 %d\n", v2[dir])
			continue
		}
		p.U64(PrintAny, d+"_v2", "v2 %d ", v2[dir])
		p.U64(PrintAny, d+"_v3", "v3 %d\n", v3[dir])
	}
	p.CloseObject()
}

// ipMcastLeaves is the leaves object: RX and TX on one line.
func ipMcastLeaves(p *Printer, title, key string, l mcastDir) {
	p.OpenObject(key)
	p.FPf("%s    %s: ", ipstatsPad16, title)
	p.U64(PrintAny, "rx", "RX: %d ", l[0])
	p.U64(PrintAny, "tx", "TX: %d\n", l[1])
	p.CloseObject()
}

// IPBridgeStp is bridge_print_stats_stp (ip/iplink_bridge.c:954).
func IPBridgeStp(p *Printer, s xtcpnl.BridgeStpXstats) {
	p.OpenObject("stp")
	p.FPf("%s    STP BPDU:  ", ipstatsPad16)
	p.U64(PrintAny, "rx_bpdu", "RX: %d ", s.RxBpdu)
	p.U64(PrintAny, "tx_bpdu", "TX: %d\n", s.TxBpdu)
	p.FPf("%s    STP TCN:   ", ipstatsPad16)
	p.U64(PrintAny, "rx_tcn", "RX: %d ", s.RxTcn)
	p.U64(PrintAny, "tx_tcn", "TX: %d\n", s.TxTcn)
	p.FPf("%s    STP Transitions: ", ipstatsPad16)
	p.U64(PrintAny, "transition_blk", "Blocked: %d ", s.TransitionBlk)
	p.U64(PrintAny, "transition_fwd", "Forwarding: %d\n", s.TransitionFwd)
	p.CloseObject()
}

// bond3adLines is bond_print_3ad_stats' key and text per BOND_3AD_STAT_*.
var bond3adLines = [xtcpnl.Bond3adStatCountCst]struct{ key, text string }{
	{"lacpdu_rx", "LACPDU Rx %d\n"},
	{"lacpdu_tx", "LACPDU Tx %d\n"},
	{"lacpdu_unknown_rx", "LACPDU Unknown type Rx %d\n"},
	{"lacpdu_illegal_rx", "LACPDU Illegal Rx %d\n"},
	{"marker_rx", "Marker Rx %d\n"},
	{"marker_tx", "Marker Tx %d\n"},
	{"marker_response_rx", "Marker response Rx %d\n"},
	{"marker_response_tx", "Marker response Tx %d\n"},
	{"marker_unknown_rx", "Marker unknown type Rx %d\n"},
}

// IPBond3ad is bond_print_3ad_stats (ip/iplink_bond.c:807): each counter
// the nest holds, in attribute order.
func IPBond3ad(p *Printer, s *xtcpnl.Bond3adStats) {
	p.OpenObject("802.3ad")
	for i, l := range bond3adLines {
		if !s.Present[i] {
			continue
		}
		p.FPf("%s    ", ipstatsPad16)
		p.U64(PrintAny, l.key, l.text, s.Values[i])
	}
	p.CloseObject()
}
