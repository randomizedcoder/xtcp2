package render

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// sentinelVlans / sentinelMcast carry distinct values so a test can tell one
// field from another and prove the RX=slot0/TX=slot1 mapping.
func sentinelVlans() []xtcpnl.BridgeVlanXstats {
	return []xtcpnl.BridgeVlanXstats{{
		RxBytes: 100, RxPackets: 101, TxBytes: 200, TxPackets: 201,
		Vid: 1, Flags: xtcpnl.BridgeVlanInfoPvid | xtcpnl.BridgeVlanInfoUntagged,
	}}
}

func sentinelMcast() *xtcpnl.BrMcastStats {
	return &xtcpnl.BrMcastStats{
		IgmpV1queries: [2]uint64{1, 2}, IgmpV2queries: [2]uint64{3, 4}, IgmpV3queries: [2]uint64{5, 6},
		IgmpLeaves: [2]uint64{7, 8}, IgmpV1reports: [2]uint64{9, 10}, IgmpV2reports: [2]uint64{11, 12},
		IgmpV3reports: [2]uint64{13, 14}, IgmpParseErrors: 15,
		MldV1queries: [2]uint64{16, 17}, MldV2queries: [2]uint64{18, 19}, MldLeaves: [2]uint64{20, 21},
		MldV1reports: [2]uint64{22, 23}, MldV2reports: [2]uint64{24, 25}, MldParseErrors: 26,
	}
}

// TestBridgeXstatsViewText pins the exact stanzas: four per interface in the
// golden order (bond, vlan, mcast, stp), bodies where present, with the
// 18/20/22-space indents.
//
// go test ./internal/goip/render/ -run TestBridgeXstatsViewText
func TestBridgeXstatsViewText(t *testing.T) {
	sp18 := strings.Repeat(" ", 18)
	sp20 := strings.Repeat(" ", 20)
	sp22 := strings.Repeat(" ", 22)

	vlanBody := sp18 + "1 PVID Egress Untagged\n" +
		sp20 + "RX: 100 bytes 101 packets\n" +
		sp20 + "TX: 200 bytes 201 packets\n"
	mcastBody := sp20 + "IGMP queries:\n" +
		sp22 + "RX: v1 1 v2 3 v3 5\n" +
		sp22 + "TX: v1 2 v2 4 v3 6\n" +
		sp20 + "IGMP reports:\n" +
		sp22 + "RX: v1 9 v2 11 v3 13\n" +
		sp22 + "TX: v1 10 v2 12 v3 14\n" +
		sp20 + "IGMP leaves: RX: 7 TX: 8\n" +
		sp20 + "IGMP parse errors: 15\n" +
		sp20 + "MLD queries:\n" +
		sp22 + "RX: v1 16 v2 18\n" +
		sp22 + "TX: v1 17 v2 19\n" +
		sp20 + "MLD reports:\n" +
		sp22 + "RX: v1 22 v2 24\n" +
		sp22 + "TX: v1 23 v2 25\n" +
		sp20 + "MLD leaves: RX: 20 TX: 21\n" +
		sp20 + "MLD parse errors: 26\n"

	names := fakeNames{5: {name: "br0"}}

	tests := []struct {
		description string
		view        BridgeXstatsView
		want        string
	}{
		{
			description: "positive: bridge with vlan and mcast bodies, empty bond and stp headers",
			view:        BridgeXstatsView{Group: XstatsGroupCst, Ifindex: 5, IfName: "br0", Vlans: sentinelVlans(), Mcast: sentinelMcast()},
			want: "5: br0: group xstats subgroup bond suite 802.3ad\n" +
				"5: br0: group xstats subgroup bridge suite vlan\n" + vlanBody + "\n" +
				"5: br0: group xstats subgroup bridge suite mcast\n" + mcastBody + "\n" +
				"5: br0: group xstats subgroup bridge suite stp\n",
		},
		{
			description: "corner: a non-bridge device prints four header-only stanzas",
			view:        BridgeXstatsViewOf(xtcpnl.IfStatsInfo{Ifindex: 2}, names),
			want: "2: if2: group xstats subgroup bond suite 802.3ad\n" +
				"2: if2: group xstats subgroup bridge suite vlan\n" +
				"2: if2: group xstats subgroup bridge suite mcast\n" +
				"2: if2: group xstats subgroup bridge suite stp\n",
		},
		{
			description: "boundary: ifindex 0 renders as *",
			view:        BridgeXstatsViewOf(xtcpnl.IfStatsInfo{Ifindex: 0}, names),
			want: "0: *: group xstats subgroup bond suite 802.3ad\n" +
				"0: *: group xstats subgroup bridge suite vlan\n" +
				"0: *: group xstats subgroup bridge suite mcast\n" +
				"0: *: group xstats subgroup bridge suite stp\n",
		},
		{
			description: "positive: a bond with all nine counters prints the 802.3ad body and its blank line",
			view:        BridgeXstatsView{Group: XstatsGroupCst, Ifindex: 5, IfName: "bond0", Bond3ad: sentinel3ad()},
			want: "5: bond0: group xstats subgroup bond suite 802.3ad\n" + bond3adBody + "\n" +
				"5: bond0: group xstats subgroup bridge suite vlan\n" +
				"5: bond0: group xstats subgroup bridge suite mcast\n" +
				"5: bond0: group xstats subgroup bridge suite stp\n",
		},
		{
			description: "positive: the slave group names itself in every header and reads the same body",
			view:        BridgeXstatsSlaveViewOf(slaveInfo(3), fakeNames{3: {name: "la1"}}),
			want: "3: la1: group xstats_slave subgroup bond suite 802.3ad\n" + bond3adBody + "\n" +
				"3: la1: group xstats_slave subgroup bridge suite vlan\n" +
				"3: la1: group xstats_slave subgroup bridge suite mcast\n" +
				"3: la1: group xstats_slave subgroup bridge suite stp\n",
		},
		{
			description: "negative: the xstats view of a record whose 3ad body is in the slave group prints none",
			view:        BridgeXstatsViewOf(slaveInfo(3), fakeNames{3: {name: "la1"}}),
			want: "3: la1: group xstats subgroup bond suite 802.3ad\n" +
				"3: la1: group xstats subgroup bridge suite vlan\n" +
				"3: la1: group xstats subgroup bridge suite mcast\n" +
				"3: la1: group xstats subgroup bridge suite stp\n",
		},
		{
			description: "boundary: an empty 3ad nest is still a body: header, then a blank line",
			view:        BridgeXstatsView{Group: XstatsGroupCst, Ifindex: 5, IfName: "bond0", Bond3ad: &xtcpnl.Bond3adStats{}},
			want: "5: bond0: group xstats subgroup bond suite 802.3ad\n\n" +
				"5: bond0: group xstats subgroup bridge suite vlan\n" +
				"5: bond0: group xstats subgroup bridge suite mcast\n" +
				"5: bond0: group xstats subgroup bridge suite stp\n",
		},
		{
			description: "corner: only the counters present print, in counter order",
			view:        BridgeXstatsView{Group: XstatsGroupCst, Ifindex: 5, IfName: "bond0", Bond3ad: partial3ad()},
			want: "5: bond0: group xstats subgroup bond suite 802.3ad\n" +
				sp20 + "LACPDU Tx 24\n" + sp20 + "Marker unknown type Rx 0\n\n" +
				"5: bond0: group xstats subgroup bridge suite vlan\n" +
				"5: bond0: group xstats subgroup bridge suite mcast\n" +
				"5: bond0: group xstats subgroup bridge suite stp\n",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.view.Text(); got != tc.want {
				t.Errorf("Text() mismatch:\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

// sentinel3ad is a 3ad body with every counter present and distinct (1..9), so
// a label wired to the wrong counter shows. bond3adBody is its text.
func sentinel3ad() *xtcpnl.Bond3adStats {
	s := &xtcpnl.Bond3adStats{}
	for i := range s.Values {
		s.Values[i] = uint64(i) + 1
		s.Present[i] = true
	}
	return s
}

// bond3adIndent is the sp16 + 4 every 802.3ad line opens with.
const bond3adIndent = sp16 + "    "

const bond3adBody = bond3adIndent + "LACPDU Rx 1\n" +
	bond3adIndent + "LACPDU Tx 2\n" +
	bond3adIndent + "LACPDU Unknown type Rx 3\n" +
	bond3adIndent + "LACPDU Illegal Rx 4\n" +
	bond3adIndent + "Marker Rx 5\n" +
	bond3adIndent + "Marker Tx 6\n" +
	bond3adIndent + "Marker response Rx 7\n" +
	bond3adIndent + "Marker response Tx 8\n" +
	bond3adIndent + "Marker unknown type Rx 9\n"

// partial3ad has two counters present: lacpdu_tx 24 and a zero
// marker_unknown_rx, the first and last label the presence check can drop.
func partial3ad() *xtcpnl.Bond3adStats {
	s := &xtcpnl.Bond3adStats{}
	s.Values[xtcpnl.Bond3adStatLacpduTx] = 24
	s.Present[xtcpnl.Bond3adStatLacpduTx] = true
	s.Present[xtcpnl.Bond3adStatMarkerUnknownRx] = true
	return s
}

// slaveInfo is a decoded record whose sentinel 3ad body is in the xstats_slave
// group only.
func slaveInfo(ifindex uint32) xtcpnl.IfStatsInfo {
	info := xtcpnl.IfStatsInfo{Ifindex: ifindex, HasXstatsSlave: true}
	info.Slave.Bond3ad = sentinel3ad()
	return info
}

// TestBridgeXstatsViewJSON pins the flat array: one object per leaf. The vlan
// leaf always carries a `vlans` array (empty on a non-bridge device); mcast
// carries `multicast` only when present.
//
// go test ./internal/goip/render/ -run TestBridgeXstatsViewJSON
func TestBridgeXstatsViewJSON(t *testing.T) {
	tests := []struct {
		description string
		view        BridgeXstatsView
		want        string
	}{
		{
			description: "positive: vlan and mcast bodies, empty bond and stp objects",
			view:        BridgeXstatsView{Group: XstatsGroupCst, Ifindex: 5, IfName: "br0", Vlans: sentinelVlans(), Mcast: sentinelMcast()},
			want: `[` +
				`{"ifindex":5,"ifname":"br0","group":"xstats","subgroup":"bond","suite":"802.3ad"},` +
				`{"ifindex":5,"ifname":"br0","group":"xstats","subgroup":"bridge","suite":"vlan","vlans":[{"vid":1,"flags":["PVID","Egress Untagged"],"rx_bytes":100,"rx_packets":101,"tx_bytes":200,"tx_packets":201}]},` +
				`{"ifindex":5,"ifname":"br0","group":"xstats","subgroup":"bridge","suite":"mcast","multicast":{` +
				`"igmp_queries":{"rx_v1":1,"rx_v2":3,"rx_v3":5,"tx_v1":2,"tx_v2":4,"tx_v3":6},` +
				`"igmp_reports":{"rx_v1":9,"rx_v2":11,"rx_v3":13,"tx_v1":10,"tx_v2":12,"tx_v3":14},` +
				`"igmp_leaves":{"rx":7,"tx":8},"igmp_parse_errors":15,` +
				`"mld_queries":{"rx_v1":16,"rx_v2":18,"tx_v1":17,"tx_v2":19},` +
				`"mld_reports":{"rx_v1":22,"rx_v2":24,"tx_v1":23,"tx_v2":25},` +
				`"mld_leaves":{"rx":20,"tx":21},"mld_parse_errors":26}},` +
				`{"ifindex":5,"ifname":"br0","group":"xstats","subgroup":"bridge","suite":"stp"}` +
				`]`,
		},
		{
			description: "corner: a non-bridge device emits the empty vlans array and three bodiless objects",
			view:        BridgeXstatsView{Group: XstatsGroupCst, Ifindex: 2, IfName: "eth0"},
			want: `[` +
				`{"ifindex":2,"ifname":"eth0","group":"xstats","subgroup":"bond","suite":"802.3ad"},` +
				`{"ifindex":2,"ifname":"eth0","group":"xstats","subgroup":"bridge","suite":"vlan","vlans":[]},` +
				`{"ifindex":2,"ifname":"eth0","group":"xstats","subgroup":"bridge","suite":"mcast"},` +
				`{"ifindex":2,"ifname":"eth0","group":"xstats","subgroup":"bridge","suite":"stp"}` +
				`]`,
		},
		{
			description: "positive: the slave group's 802.3ad object, keys in counter order",
			view:        BridgeXstatsView{Group: XstatsSlaveGroupCst, Ifindex: 3, IfName: "la1", Bond3ad: sentinel3ad()},
			want: `[` +
				`{"ifindex":3,"ifname":"la1","group":"xstats_slave","subgroup":"bond","suite":"802.3ad","802.3ad":{` +
				`"lacpdu_rx":1,"lacpdu_tx":2,"lacpdu_unknown_rx":3,"lacpdu_illegal_rx":4,"marker_rx":5,` +
				`"marker_tx":6,"marker_response_rx":7,"marker_response_tx":8,"marker_unknown_rx":9}},` +
				`{"ifindex":3,"ifname":"la1","group":"xstats_slave","subgroup":"bridge","suite":"vlan","vlans":[]},` +
				`{"ifindex":3,"ifname":"la1","group":"xstats_slave","subgroup":"bridge","suite":"mcast"},` +
				`{"ifindex":3,"ifname":"la1","group":"xstats_slave","subgroup":"bridge","suite":"stp"}` +
				`]`,
		},
		{
			description: "boundary: an empty 3ad nest is an empty object, not an absent key",
			view:        BridgeXstatsView{Group: XstatsGroupCst, Ifindex: 5, IfName: "bond0", Bond3ad: &xtcpnl.Bond3adStats{}},
			want: `[` +
				`{"ifindex":5,"ifname":"bond0","group":"xstats","subgroup":"bond","suite":"802.3ad","802.3ad":{}},` +
				`{"ifindex":5,"ifname":"bond0","group":"xstats","subgroup":"bridge","suite":"vlan","vlans":[]},` +
				`{"ifindex":5,"ifname":"bond0","group":"xstats","subgroup":"bridge","suite":"mcast"},` +
				`{"ifindex":5,"ifname":"bond0","group":"xstats","subgroup":"bridge","suite":"stp"}` +
				`]`,
		},
		{
			description: "corner: an absent counter is an absent key, a present zero is 0",
			view:        BridgeXstatsView{Group: XstatsGroupCst, Ifindex: 5, IfName: "bond0", Bond3ad: partial3ad()},
			want: `[` +
				`{"ifindex":5,"ifname":"bond0","group":"xstats","subgroup":"bond","suite":"802.3ad","802.3ad":{"lacpdu_tx":24,"marker_unknown_rx":0}},` +
				`{"ifindex":5,"ifname":"bond0","group":"xstats","subgroup":"bridge","suite":"vlan","vlans":[]},` +
				`{"ifindex":5,"ifname":"bond0","group":"xstats","subgroup":"bridge","suite":"mcast"},` +
				`{"ifindex":5,"ifname":"bond0","group":"xstats","subgroup":"bridge","suite":"stp"}` +
				`]`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := json.Marshal(tc.view)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("JSON mismatch:\n got %s\nwant %s", got, tc.want)
			}
		})
	}
}

// TestBridgeXstatsLeafOrder guards the golden-pinned leaf table against an
// accidental reorder or a body wired to the wrong leaf.
//
// go test ./internal/goip/render/ -run TestBridgeXstatsLeafOrder
func TestBridgeXstatsLeafOrder(t *testing.T) {
	want := []bridgeXstatsLeaf{
		{subgroup: "bond", suite: "802.3ad", body: bridgeBodyBond3ad},
		{subgroup: "bridge", suite: "vlan", body: bridgeBodyVlan},
		{subgroup: "bridge", suite: "mcast", body: bridgeBodyMcast},
		{subgroup: "bridge", suite: "stp", body: bridgeBodyNone},
	}
	if len(bridgeXstatsLeaves) != len(want) {
		t.Fatalf("leaf count = %d, want %d", len(bridgeXstatsLeaves), len(want))
	}
	seen := map[string]bool{}
	for i, leaf := range bridgeXstatsLeaves {
		if leaf != want[i] {
			t.Errorf("leaf %d = %+v, want %+v", i, leaf, want[i])
		}
		key := leaf.subgroup + "/" + leaf.suite
		if seen[key] {
			t.Errorf("duplicate leaf %q", key)
		}
		seen[key] = true
	}
}

// TestBridgeXstatsLeafFilter pins what a `subgroup X [suite Y]` selection
// prints: the matching leaves only, still in the golden order, in both the
// text and the JSON render. expected lists each printed stanza as
// "subgroup/suite" plus "+body" when a body followed its header line.
//
// go test ./internal/goip/render/ -run TestBridgeXstatsLeafFilter
func TestBridgeXstatsLeafFilter(t *testing.T) {
	t.Parallel()
	bridge := BridgeXstatsView{Group: XstatsGroupCst, Ifindex: 5, IfName: "br0", Vlans: sentinelVlans(), Mcast: sentinelMcast()}
	bond := BridgeXstatsView{Group: XstatsSlaveGroupCst, Ifindex: 3, IfName: "la1", Bond3ad: sentinel3ad()}
	tests := []struct {
		description string
		view        BridgeXstatsView
		sel         XstatsSelect
		expected    []string
	}{
		{
			description: "positive: no filter keeps the golden order",
			view:        bridge,
			expected:    []string{"bond/802.3ad", "bridge/vlan+body", "bridge/mcast+body", "bridge/stp"},
		},
		{
			description: "positive: the bond suite alone",
			view:        bond,
			sel:         XstatsSelect{Subgroup: "bond", Suite: "802.3ad"},
			expected:    []string{"bond/802.3ad+body"},
		},
		{
			description: "positive: the bridge subgroup",
			view:        bridge,
			sel:         XstatsSelect{Subgroup: "bridge"},
			expected:    []string{"bridge/vlan+body", "bridge/mcast+body", "bridge/stp"},
		},
		{
			description: "boundary: the bond subgroup with no suite is its one leaf",
			view:        bond,
			sel:         XstatsSelect{Subgroup: "bond"},
			expected:    []string{"bond/802.3ad+body"},
		},
		{
			description: "boundary: the last leaf alone",
			view:        bridge,
			sel:         XstatsSelect{Subgroup: "bridge", Suite: "stp"},
			expected:    []string{"bridge/stp"},
		},
		{
			description: "corner: a bridge suite on a bond port prints the header, no body",
			view:        bond,
			sel:         XstatsSelect{Subgroup: "bridge", Suite: "vlan"},
			expected:    []string{"bridge/vlan"},
		},
		{
			description: "negative: the bond body is not printed under a bridge filter",
			view:        bond,
			sel:         XstatsSelect{Subgroup: "bridge"},
			expected:    []string{"bridge/vlan", "bridge/mcast", "bridge/stp"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			v := tc.view
			v.Select = tc.sel
			if got := textStanzas(t, v); !slices.Equal(got, tc.expected) {
				t.Errorf("Text stanzas = %q, want %q\n%s", got, tc.expected, v.Text())
			}
			var leaves []map[string]any
			raw, err := json.Marshal(v)
			if err != nil {
				t.Fatalf("MarshalJSON: %v", err)
			}
			if err := json.Unmarshal(raw, &leaves); err != nil {
				t.Fatalf("unmarshal %s: %v", raw, err)
			}
			got := make([]string, 0, len(leaves))
			for _, leaf := range leaves {
				got = append(got, leaf["subgroup"].(string)+"/"+leaf["suite"].(string))
			}
			want := make([]string, 0, len(tc.expected))
			for _, e := range tc.expected {
				want = append(want, strings.TrimSuffix(e, "+body"))
			}
			if !slices.Equal(got, want) {
				t.Errorf("JSON leaves = %q, want %q\n%s", got, want, raw)
			}
		})
	}
}

// textStanzas reduces a Text render to "subgroup/suite[+body]" per stanza.
func textStanzas(t *testing.T, v BridgeXstatsView) []string {
	t.Helper()
	prefix := fmt.Sprintf("%d: %s: group %s subgroup ", v.Ifindex, v.IfName, v.Group)
	var out []string
	for _, line := range strings.Split(v.Text(), "\n") {
		if rest, ok := strings.CutPrefix(line, prefix); ok {
			sub, suite, _ := strings.Cut(rest, " suite ")
			out = append(out, sub+"/"+suite)
			continue
		}
		if strings.TrimSpace(line) != "" && len(out) > 0 && !strings.HasSuffix(out[len(out)-1], "+body") {
			out[len(out)-1] += "+body"
		}
	}
	return out
}
