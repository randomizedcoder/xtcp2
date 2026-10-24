package render

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// linkXstatsPort is v2's port group as the bridge_slave capture holds it:
// the bridge type with zeroed mcast and stp bodies.
var linkXstatsPort = xtcpnl.XstatsBodies{HasBridgeXstats: true, HasBridgeMcast: true, HasBridgeStp: true}

// TestLinkXstatsViewText is bridge_print_stats_attr's text stanza.
//
// go test ./internal/goip/render/ -run TestLinkXstatsViewText
func TestLinkXstatsViewText(t *testing.T) {
	golden, err := os.ReadFile("../../../pkg/xtcpnl/testdata/7_1_4/dumps/reads/ip_link_xstats_bridge_slave")
	if err != nil {
		t.Fatal(err)
	}
	names := fakeNames{16: {name: "v2"}, 3: {name: "a-name-of-16chrs"}}
	master := xtcpnl.XstatsBodies{HasBridgeXstats: true, HasBridgeMcast: true, HasBridgeVlan: true,
		BridgeVlans: []xtcpnl.BridgeVlanXstats{{Vid: 1}}}
	counted := linkXstatsPort
	counted.BridgeStp = xtcpnl.BridgeStpXstats{TransitionBlk: 1, TransitionFwd: 2, RxBpdu: 3, TxBpdu: 4, RxTcn: 5, TxTcn: 6}
	stpText := "                    STP BPDU:  RX: 3 TX: 4\n" +
		"                    STP TCN:   RX: 5 TX: 6\n" +
		"                    STP Transitions: Blocked: 1 Forwarding: 2\n"
	mcastLines := strings.Count(bridgeMcastText(xtcpnl.BrMcastStats{}), "\n")

	tests := []struct {
		description string
		ifindex     uint32
		x           xtcpnl.XstatsBodies
		sel         string
		expected    string // whole text, or "" to count lines instead
		lines       int
	}{
		{"positive: v2's port stanza is ip's sidecar", 16, linkXstatsPort, "", string(golden), 0},
		{"positive: stp counters land in bridge_print_stats_stp's order", 16, counted, LinkXstatsStpCst,
			"v2              \n" + stpText, 0},
		{"positive: the master's vlans print nothing, mcast does", 16, master, "", "", 1 + mcastLines},
		{"corner: `stp` on a master leaves the name alone", 16, master, LinkXstatsStpCst, "v2              \n", 0},
		{"corner: `mcast` on a port drops the stp block", 16, linkXstatsPort, LinkXstatsMcastCst, "", 1 + mcastLines},
		{"boundary: a 16-character name gets no padding", 3, xtcpnl.XstatsBodies{HasBridgeXstats: true}, "",
			"a-name-of-16chrs\n", 0},
		{"boundary: an empty bridge nest is the name alone", 16, xtcpnl.XstatsBodies{HasBridgeXstats: true}, "",
			"v2              \n", 0},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v, ok := LinkXstatsViewOf(tc.ifindex, &tc.x, tc.sel, names)
			if !ok {
				t.Fatal("ok = false, want true")
			}
			got := v.Text()
			if tc.expected != "" && got != tc.expected {
				t.Errorf("Text =\n%q\nwant\n%q", got, tc.expected)
			}
			if tc.expected == "" && strings.Count(got, "\n") != tc.lines {
				t.Errorf("Text has %d lines, want %d:\n%s", strings.Count(got, "\n"), tc.lines, got)
			}
		})
	}
}

// TestLinkXstatsViewOfSkips pins the early return: a nest with no bridge
// type, a bond's, prints nothing at all.
//
// go test ./internal/goip/render/ -run TestLinkXstatsViewOfSkips
func TestLinkXstatsViewOfSkips(t *testing.T) {
	tests := []struct {
		description string
		x           xtcpnl.XstatsBodies
		expected    bool
	}{
		{"positive: the bridge type is a stanza", xtcpnl.XstatsBodies{HasBridgeXstats: true}, true},
		{"negative: a bond's 802.3ad nest is not", xtcpnl.XstatsBodies{Bond3ad: &xtcpnl.Bond3adStats{}}, false},
		{"negative: an empty nest is not", xtcpnl.XstatsBodies{}, false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if _, ok := LinkXstatsViewOf(5, &tc.x, "", fakeNames{}); ok != tc.expected {
				t.Errorf("ok = %v, want %v", ok, tc.expected)
			}
		})
	}
}

// TestLinkXstatsViewJSON is the stanza's -j object.
//
// go test ./internal/goip/render/ -run TestLinkXstatsViewJSON
func TestLinkXstatsViewJSON(t *testing.T) {
	counted := linkXstatsPort
	counted.BridgeStp = xtcpnl.BridgeStpXstats{TransitionBlk: 1, TransitionFwd: 2, RxBpdu: 3, TxBpdu: 4, RxTcn: 5, TxTcn: 6}
	tests := []struct {
		description string
		x           xtcpnl.XstatsBodies
		sel         string
		expected    string
	}{
		{"positive: stp keys in print order", counted, LinkXstatsStpCst,
			`{"ifname":"v2","stp":{"rx_bpdu":3,"tx_bpdu":4,"rx_tcn":5,"tx_tcn":6,"transition_blk":1,"transition_fwd":2}}`},
		{"boundary: the name alone", xtcpnl.XstatsBodies{HasBridgeXstats: true}, "", `{"ifname":"v2"}`},
		{"positive: multicast carries its parse-error counters inside", linkXstatsPort, LinkXstatsMcastCst,
			`{"ifname":"v2","multicast":{"igmp_queries":{"rx_v1":0,"rx_v2":0,"rx_v3":0,"tx_v1":0,"tx_v2":0,"tx_v3":0},` +
				`"igmp_reports":{"rx_v1":0,"rx_v2":0,"rx_v3":0,"tx_v1":0,"tx_v2":0,"tx_v3":0},"igmp_leaves":{"rx":0,"tx":0},` +
				`"igmp_parse_errors":0,"mld_queries":{"rx_v1":0,"rx_v2":0,"tx_v1":0,"tx_v2":0},` +
				`"mld_reports":{"rx_v1":0,"rx_v2":0,"tx_v1":0,"tx_v2":0},"mld_leaves":{"rx":0,"tx":0},"mld_parse_errors":0}}`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v, _ := LinkXstatsViewOf(16, &tc.x, tc.sel, fakeNames{16: {name: "v2"}})
			got, err := json.Marshal(v.LinkXstatsJSON())
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.expected {
				t.Errorf("JSON = %s\nwant   %s", got, tc.expected)
			}
		})
	}
}
