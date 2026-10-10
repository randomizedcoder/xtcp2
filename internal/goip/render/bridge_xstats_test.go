package render

import (
	"encoding/json"
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
			view:        BridgeXstatsView{Ifindex: 5, IfName: "br0", Vlans: sentinelVlans(), Mcast: sentinelMcast()},
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
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.view.Text(); got != tc.want {
				t.Errorf("Text() mismatch:\n got %q\nwant %q", got, tc.want)
			}
		})
	}
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
			view:        BridgeXstatsView{Ifindex: 5, IfName: "br0", Vlans: sentinelVlans(), Mcast: sentinelMcast()},
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
			view:        BridgeXstatsView{Ifindex: 2, IfName: "eth0"},
			want: `[` +
				`{"ifindex":2,"ifname":"eth0","group":"xstats","subgroup":"bond","suite":"802.3ad"},` +
				`{"ifindex":2,"ifname":"eth0","group":"xstats","subgroup":"bridge","suite":"vlan","vlans":[]},` +
				`{"ifindex":2,"ifname":"eth0","group":"xstats","subgroup":"bridge","suite":"mcast"},` +
				`{"ifindex":2,"ifname":"eth0","group":"xstats","subgroup":"bridge","suite":"stp"}` +
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
		{subgroup: "bond", suite: "802.3ad", body: bridgeBodyNone},
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
