package render

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file renders `ip stats show group xstats` and `group xstats_slave`. Each
// whole group expands to four leaves (bond 802.3ad, bridge vlan/mcast/stp),
// printed one stanza per leaf per interface with a body only where the
// sub-attribute is present (ipstats_process_ifsm, ip/ipstats.c:765-779; the
// bridge printers, ip/iplink_bridge.c + lib/bridge.c; the bond printer,
// bond_print_3ad_stats in ip/iplink_bond.c). The two groups share leaves and
// printers and differ only in the group name and in which attribute carries
// the bodies. goip renders the vlan, mcast and 802.3ad bodies and the empty stp
// header; an stp body is refused upstream in obj_stats.

// sp16 is iproute2's `%-16s` of "" — the 16-space field every xstats line opens
// with (IFNAMSIZ). Labels then add 4 more spaces, mcast value lines 6.
const sp16 = "                "

// bridgeXstatsLeaf is one enabled leaf of the expanded xstats group: its
// selector path (subgroup/suite) and which body, if any, goip renders for it.
type bridgeXstatsLeaf struct {
	subgroup string
	suite    string
	body     bridgeBodyKind
}

type bridgeBodyKind int

const (
	bridgeBodyNone bridgeBodyKind = iota
	bridgeBodyVlan
	bridgeBodyMcast
	bridgeBodyBond3ad
)

// Group names a view renders under, the `group` token of the header and the
// JSON `group` key.
const (
	XstatsGroupCst      = "xstats"
	XstatsSlaveGroupCst = "xstats_slave"
)

// bridgeXstatsLeaves is the four leaves `group xstats` (and `group
// xstats_slave`) expands to, in iproute2's emission order. iproute2 qsorts the
// enabled descriptors by struct pointer (ip/ipstats.c:1053-1059), stable per
// binary but not derivable from source, so this order is PINNED TO THE CAPTURED
// GOLDENS (testdata .../ip_stats_xstats and link-write/*/ip_stats_xstats_slave):
// bond 802.3ad, bridge vlan, bridge mcast, bridge stp.
var bridgeXstatsLeaves = []bridgeXstatsLeaf{
	{subgroup: "bond", suite: "802.3ad", body: bridgeBodyBond3ad},
	{subgroup: "bridge", suite: "vlan", body: bridgeBodyVlan},
	{subgroup: "bridge", suite: "mcast", body: bridgeBodyMcast},
	{subgroup: "bridge", suite: "stp", body: bridgeBodyNone},
}

// XstatsSelect is a `subgroup X [suite Y]` narrowing of the whole group. The
// zero value selects every leaf. It is render-only: iproute2 sends no
// IFLA_STATS_GET_FILTERS nest for the xstats groups, since
// ipstats_stat_desc_pack_xstats enables the group bit with subgroup 0
// (ip/ipstats.c:578-585), so the request is the whole group's either way and
// the selection decides only which leaves print.
type XstatsSelect struct {
	Subgroup string
	Suite    string
}

// matches reports whether leaf is inside the selection.
func (s XstatsSelect) matches(leaf bridgeXstatsLeaf) bool {
	return (s.Subgroup == "" || s.Subgroup == leaf.subgroup) &&
		(s.Suite == "" || s.Suite == leaf.suite)
}

// Valid reports whether the selection names a subgroup, and a suite inside
// it, that the xstats groups hold. It is ipstats_enable_find_stat_desc's
// "no %s named %s found inside %s" check (ip/ipstats.c:999-1029) over the
// four leaves. A suite with no subgroup is the parser's to refuse.
func (s XstatsSelect) Valid() bool {
	for _, leaf := range bridgeXstatsLeaves {
		if s.matches(leaf) {
			return true
		}
	}
	return false
}

// leaves is the selected leaves, in the pinned golden order.
func (s XstatsSelect) leaves() []bridgeXstatsLeaf {
	out := make([]bridgeXstatsLeaf, 0, len(bridgeXstatsLeaves))
	for _, leaf := range bridgeXstatsLeaves {
		if s.matches(leaf) {
			out = append(out, leaf)
		}
	}
	return out
}

// BridgeXstatsView is one interface's xstats (or xstats_slave) group, decoded.
// Group is XstatsGroupCst or XstatsSlaveGroupCst. Vlans is the per-VLAN list
// (the vlan leaf always emits a `vlans` array in JSON, empty on a non-bridge
// device); Mcast and Bond3ad are set only when the reply carried that body.
// Select narrows the leaves printed; the zero value prints all four.
type BridgeXstatsView struct {
	Select  XstatsSelect
	Group   string
	Ifindex uint32
	IfName  string
	Vlans   []xtcpnl.BridgeVlanXstats
	Mcast   *xtcpnl.BrMcastStats
	Bond3ad *xtcpnl.Bond3adStats
}

// BridgeXstatsViewOf builds the `group xstats` view from a decoded message,
// resolving the ifindex via the bundled link dump (ll_index_to_name).
func BridgeXstatsViewOf(info xtcpnl.IfStatsInfo, names NameTab) BridgeXstatsView {
	return xstatsViewOf(XstatsGroupCst, info.Ifindex, info.XstatsBodies, names)
}

// BridgeXstatsSlaveViewOf is BridgeXstatsViewOf for `group xstats_slave`: the
// same leaves, read from the IFLA_STATS_LINK_XSTATS_SLAVE bodies.
func BridgeXstatsSlaveViewOf(info xtcpnl.IfStatsInfo, names NameTab) BridgeXstatsView {
	return xstatsViewOf(XstatsSlaveGroupCst, info.Ifindex, info.Slave, names)
}

func xstatsViewOf(group string, ifindex uint32, x xtcpnl.XstatsBodies, names NameTab) BridgeXstatsView {
	v := BridgeXstatsView{
		Group:   group,
		Ifindex: ifindex,
		IfName:  names.IndexToName(int32(ifindex)),
		Vlans:   x.BridgeVlans,
		Bond3ad: x.Bond3ad,
	}
	if x.HasBridgeMcast {
		m := x.BridgeMcast
		v.Mcast = &m
	}
	return v
}

// Text reproduces one interface's stanzas: for each leaf the
// `%d: %s: group %s subgroup %s suite %s` header (ipstats_show_group), then,
// where a body is present, the leaf body. A body leaf carries a trailing blank
// line (the print_nl at :778 after show's own print_nl); a bodiless leaf is just
// its header line. The dump's between-interface blank (:862) is the caller's,
// the IfStatsView precedent.
func (v BridgeXstatsView) Text() string {
	var b strings.Builder
	for _, leaf := range v.Select.leaves() {
		fmt.Fprintf(&b, "%d: %s: group %s subgroup %s suite %s\n", v.Ifindex, v.IfName, v.Group, leaf.subgroup, leaf.suite)
		if body, ok := v.bodyText(leaf); ok {
			b.WriteString(body)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

// bodyText returns the leaf's body and whether there is one. The vlan leaf has a
// text body only when it carries VLANs (a non-bridge device still emits the
// empty `vlans` array in JSON, but no text, iplink_bridge.c). The 802.3ad body
// is present whenever BOND_XSTATS_3AD was, even with no counter in it: the
// print_nl ipstats_stat_desc_show_xstats emits before calling the printer
// (ip/ipstats.c:603-606) still ends in a blank line.
func (v BridgeXstatsView) bodyText(leaf bridgeXstatsLeaf) (string, bool) {
	switch leaf.body {
	case bridgeBodyVlan:
		if len(v.Vlans) > 0 {
			return bridgeVlanText(v.Vlans), true
		}
	case bridgeBodyMcast:
		if v.Mcast != nil {
			return bridgeMcastText(*v.Mcast), true
		}
	case bridgeBodyBond3ad:
		if v.Bond3ad != nil {
			return bond3adText(*v.Bond3ad), true
		}
	}
	return "", false
}

// bond3adStatNames are bond_print_3ad_stats's text labels and JSON keys, in
// counter order (ip/iplink_bond.c). The labels are not derivable from the keys:
// "Unknown type" is capitalized for LACPDUs and not for markers.
var bond3adStatNames = [xtcpnl.Bond3adStatCountCst]struct{ label, key string }{
	{"LACPDU Rx", "lacpdu_rx"},
	{"LACPDU Tx", "lacpdu_tx"},
	{"LACPDU Unknown type Rx", "lacpdu_unknown_rx"},
	{"LACPDU Illegal Rx", "lacpdu_illegal_rx"},
	{"Marker Rx", "marker_rx"},
	{"Marker Tx", "marker_tx"},
	{"Marker response Rx", "marker_response_rx"},
	{"Marker response Tx", "marker_response_tx"},
	{"Marker unknown type Rx", "marker_unknown_rx"},
}

// bond3adText is bond_print_3ad_stats' terminal output: one sp16 + 4-space
// line per counter present, in counter order.
func bond3adText(s xtcpnl.Bond3adStats) string {
	var b strings.Builder
	for i, n := range bond3adStatNames {
		if s.Present[i] {
			fmt.Fprintf(&b, "%s    %s %d\n", sp16, n.label, s.Values[i])
		}
	}
	return b.String()
}

// bond3adJSON is the `802.3ad` object: the counters present, in counter order.
type bond3adJSON xtcpnl.Bond3adStats

// MarshalJSON writes only the present counters, so an empty BOND_XSTATS_3AD
// is `{}`, which is what open_json_object with no print_u64 inside writes.
func (j bond3adJSON) MarshalJSON() ([]byte, error) {
	var k KindData
	for i, n := range bond3adStatNames {
		if j.Present[i] {
			k.add(n.key, "", j.Values[i])
		}
	}
	return k.MarshalJSON()
}

// bridgeVlanText is bridge_print_stats_vlan over each VLAN (lib/bridge.c:22-47):
// a vid+flags line (sp16+2) then RX/TX bytes+packets lines (sp16+4). The
// show-side print_nl between VLANs (ip/ipstats.c:604) renders as a blank line
// separating one VLAN's block from the next.
func bridgeVlanText(vlans []xtcpnl.BridgeVlanXstats) string {
	var b strings.Builder
	for i, vl := range vlans {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(bridgeOneVlanText(vl))
	}
	return b.String()
}

func bridgeOneVlanText(v xtcpnl.BridgeVlanXstats) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  %d", sp16, v.Vid)
	for _, name := range bridgeVlanFlagNames(v.Flags) {
		b.WriteByte(' ')
		b.WriteString(name)
	}
	b.WriteByte('\n')
	fmt.Fprintf(&b, "%s    RX: %d bytes %d packets\n", sp16, v.RxBytes, v.RxPackets)
	fmt.Fprintf(&b, "%s    TX: %d bytes %d packets\n", sp16, v.TxBytes, v.TxPackets)
	return b.String()
}

// bridgeVlanFlagNames maps the rendered flag bits to their labels, in bit order
// (bridge_print_vlan_flags, lib/bridge.c:8-20). A flags value of 0 never calls
// this (the caller omits the whole flags locus).
func bridgeVlanFlagNames(flags uint16) []string {
	out := []string{}
	if flags&xtcpnl.BridgeVlanInfoPvid != 0 {
		out = append(out, "PVID")
	}
	if flags&xtcpnl.BridgeVlanInfoUntagged != 0 {
		out = append(out, "Egress Untagged")
	}
	return out
}

// bridgeMcastText is bridge_print_stats_mcast (ip/iplink_bridge.c:850-952): the
// IGMP (v1/v2/v3) then MLD (v1/v2) query/report/leave/parse-error block. Label
// lines open with sp16 + 4 spaces, RX/TX value lines with sp16 + 6. The trailing
// mcast_bytes/mcast_packets are not printed.
func bridgeMcastText(s xtcpnl.BrMcastStats) string {
	const rx, tx = 0, 1
	var b strings.Builder
	fmt.Fprintf(&b, "%s    IGMP queries:\n", sp16)
	fmt.Fprintf(&b, "%s      RX: v1 %d v2 %d v3 %d\n", sp16, s.IgmpV1queries[rx], s.IgmpV2queries[rx], s.IgmpV3queries[rx])
	fmt.Fprintf(&b, "%s      TX: v1 %d v2 %d v3 %d\n", sp16, s.IgmpV1queries[tx], s.IgmpV2queries[tx], s.IgmpV3queries[tx])
	fmt.Fprintf(&b, "%s    IGMP reports:\n", sp16)
	fmt.Fprintf(&b, "%s      RX: v1 %d v2 %d v3 %d\n", sp16, s.IgmpV1reports[rx], s.IgmpV2reports[rx], s.IgmpV3reports[rx])
	fmt.Fprintf(&b, "%s      TX: v1 %d v2 %d v3 %d\n", sp16, s.IgmpV1reports[tx], s.IgmpV2reports[tx], s.IgmpV3reports[tx])
	fmt.Fprintf(&b, "%s    IGMP leaves: RX: %d TX: %d\n", sp16, s.IgmpLeaves[rx], s.IgmpLeaves[tx])
	fmt.Fprintf(&b, "%s    IGMP parse errors: %d\n", sp16, s.IgmpParseErrors)
	fmt.Fprintf(&b, "%s    MLD queries:\n", sp16)
	fmt.Fprintf(&b, "%s      RX: v1 %d v2 %d\n", sp16, s.MldV1queries[rx], s.MldV2queries[rx])
	fmt.Fprintf(&b, "%s      TX: v1 %d v2 %d\n", sp16, s.MldV1queries[tx], s.MldV2queries[tx])
	fmt.Fprintf(&b, "%s    MLD reports:\n", sp16)
	fmt.Fprintf(&b, "%s      RX: v1 %d v2 %d\n", sp16, s.MldV1reports[rx], s.MldV2reports[rx])
	fmt.Fprintf(&b, "%s      TX: v1 %d v2 %d\n", sp16, s.MldV1reports[tx], s.MldV2reports[tx])
	fmt.Fprintf(&b, "%s    MLD leaves: RX: %d TX: %d\n", sp16, s.MldLeaves[rx], s.MldLeaves[tx])
	fmt.Fprintf(&b, "%s    MLD parse errors: %d\n", sp16, s.MldParseErrors)
	return b.String()
}

// bridgeXstatsLeafJSON is one `-j` array element: the ifindex/ifname and the
// group selector keys, plus the body for a body leaf. The vlan leaf always emits
// `vlans` (empty on a non-bridge device); mcast emits `multicast` only when
// present. A non-nil Vlans pointer is what forces the empty-array emission.
type bridgeXstatsLeafJSON struct {
	Ifindex   uint32            `json:"ifindex"`
	Ifname    string            `json:"ifname"`
	Group     string            `json:"group"`
	Subgroup  string            `json:"subgroup"`
	Suite     string            `json:"suite"`
	Vlans     *[]vlanXstatsJSON `json:"vlans,omitempty"`
	Multicast *bridgeMcastJSON  `json:"multicast,omitempty"`
	Bond3ad   *bond3adJSON      `json:"802.3ad,omitempty"`
}

// vlanXstatsJSON is one VLAN object. Flags is a pointer so flags==0 omits the
// key entirely (bridge_print_vlan_flags returns before opening the array).
type vlanXstatsJSON struct {
	Vid       uint16    `json:"vid"`
	Flags     *[]string `json:"flags,omitempty"`
	RxBytes   uint64    `json:"rx_bytes"`
	RxPackets uint64    `json:"rx_packets"`
	TxBytes   uint64    `json:"tx_bytes"`
	TxPackets uint64    `json:"tx_packets"`
}

type mcastRxTxJSON struct {
	Rx uint64 `json:"rx"`
	Tx uint64 `json:"tx"`
}

type mcastV3JSON struct {
	RxV1 uint64 `json:"rx_v1"`
	RxV2 uint64 `json:"rx_v2"`
	RxV3 uint64 `json:"rx_v3"`
	TxV1 uint64 `json:"tx_v1"`
	TxV2 uint64 `json:"tx_v2"`
	TxV3 uint64 `json:"tx_v3"`
}

type mcastV2JSON struct {
	RxV1 uint64 `json:"rx_v1"`
	RxV2 uint64 `json:"rx_v2"`
	TxV1 uint64 `json:"tx_v1"`
	TxV2 uint64 `json:"tx_v2"`
}

type bridgeMcastJSON struct {
	IgmpQueries     mcastV3JSON   `json:"igmp_queries"`
	IgmpReports     mcastV3JSON   `json:"igmp_reports"`
	IgmpLeaves      mcastRxTxJSON `json:"igmp_leaves"`
	IgmpParseErrors uint64        `json:"igmp_parse_errors"`
	MldQueries      mcastV2JSON   `json:"mld_queries"`
	MldReports      mcastV2JSON   `json:"mld_reports"`
	MldLeaves       mcastRxTxJSON `json:"mld_leaves"`
	MldParseErrors  uint64        `json:"mld_parse_errors"`
}

// BridgeXstatsJSON flattens views into the `-j` array: one element per leaf per
// interface (ipstats_process_ifsm's open_json_object(NULL) per enabled leaf).
func BridgeXstatsJSON(views []BridgeXstatsView) []bridgeXstatsLeafJSON {
	out := make([]bridgeXstatsLeafJSON, 0, len(views)*len(bridgeXstatsLeaves))
	for _, v := range views {
		for _, leaf := range v.Select.leaves() {
			e := bridgeXstatsLeafJSON{
				Ifindex: v.Ifindex, Ifname: v.IfName,
				Group: v.Group, Subgroup: leaf.subgroup, Suite: leaf.suite,
			}
			switch leaf.body {
			case bridgeBodyVlan:
				vs := make([]vlanXstatsJSON, 0, len(v.Vlans))
				for _, vl := range v.Vlans {
					vs = append(vs, vlanXstatsJSONOf(vl))
				}
				e.Vlans = &vs
			case bridgeBodyMcast:
				if v.Mcast != nil {
					e.Multicast = bridgeMcastJSONOf(*v.Mcast)
				}
			case bridgeBodyBond3ad:
				if v.Bond3ad != nil {
					j := bond3adJSON(*v.Bond3ad)
					e.Bond3ad = &j
				}
			}
			out = append(out, e)
		}
	}
	return out
}

// MarshalJSON lets a single view encode to its leaf elements; the object layer
// uses BridgeXstatsJSON for the flat dump array.
func (v BridgeXstatsView) MarshalJSON() ([]byte, error) {
	return json.Marshal(BridgeXstatsJSON([]BridgeXstatsView{v}))
}

func vlanXstatsJSONOf(v xtcpnl.BridgeVlanXstats) vlanXstatsJSON {
	j := vlanXstatsJSON{
		Vid:     v.Vid,
		RxBytes: v.RxBytes, RxPackets: v.RxPackets,
		TxBytes: v.TxBytes, TxPackets: v.TxPackets,
	}
	if v.Flags != 0 {
		f := bridgeVlanFlagNames(v.Flags)
		j.Flags = &f
	}
	return j
}

func bridgeMcastJSONOf(s xtcpnl.BrMcastStats) *bridgeMcastJSON {
	const rx, tx = 0, 1
	return &bridgeMcastJSON{
		IgmpQueries:     mcastV3JSON{s.IgmpV1queries[rx], s.IgmpV2queries[rx], s.IgmpV3queries[rx], s.IgmpV1queries[tx], s.IgmpV2queries[tx], s.IgmpV3queries[tx]},
		IgmpReports:     mcastV3JSON{s.IgmpV1reports[rx], s.IgmpV2reports[rx], s.IgmpV3reports[rx], s.IgmpV1reports[tx], s.IgmpV2reports[tx], s.IgmpV3reports[tx]},
		IgmpLeaves:      mcastRxTxJSON{s.IgmpLeaves[rx], s.IgmpLeaves[tx]},
		IgmpParseErrors: s.IgmpParseErrors,
		MldQueries:      mcastV2JSON{s.MldV1queries[rx], s.MldV2queries[rx], s.MldV1queries[tx], s.MldV2queries[tx]},
		MldReports:      mcastV2JSON{s.MldV1reports[rx], s.MldV2reports[rx], s.MldV1reports[tx], s.MldV2reports[tx]},
		MldLeaves:       mcastRxTxJSON{s.MldLeaves[rx], s.MldLeaves[tx]},
		MldParseErrors:  s.MldParseErrors,
	}
}
