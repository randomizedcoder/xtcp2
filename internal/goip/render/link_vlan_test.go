package render

import (
	"encoding/json"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestVlanFamilyDataRender holds the VLAN family's print_opts to what no
// KC3 readback reaches (docs/netlink/goip-link-kinds.md, KG2-T04): each
// row's text and its info_data JSON.
func TestVlanFamilyDataRender(t *testing.T) {
	type expected struct {
		text string
		json string
	}
	tests := []struct {
		description string
		data        *KindData
		expected    expected
	}{
		{"positive: a vlan with no protocol attribute",
			vlanData(&xtcpnl.VlanInfo{ID: 5, HasID: true}),
			expected{"protocol 802.1q id 5 ", `{"protocol":"802.1q","id":5}`}},
		{"positive: macvlan's two flags",
			macvlanData(&xtcpnl.MacvlanInfo{Mode: xtcpnl.MacvlanModePassthru, HasMode: true, Flags: 3, HasFlags: true}),
			expected{"mode passthru nopromisc nodst ", `{"mode":"passthru","nopromisc":true,"nodst":true}`}},
		{"negative: a vlan without an id",
			vlanData(&xtcpnl.VlanInfo{Protocol: 0x88a8, HasProtocol: true}),
			expected{"", `{}`}},
		{"negative: a macvlan without a mode",
			macvlanData(&xtcpnl.MacvlanInfo{BcQueueLen: 10, HasBcQueueLen: true}),
			expected{"", `{}`}},
		{"boundary: an empty flags attribute",
			vlanData(&xtcpnl.VlanInfo{ID: 1, HasID: true, HasFlags: true}),
			expected{"protocol 802.1q id 1 <> ", `{"protocol":"802.1q","id":1,"flags":[]}`}},
		{"boundary: a macvlan mode past the table",
			macvlanData(&xtcpnl.MacvlanInfo{Mode: 32, HasMode: true}),
			expected{"mode unknown ", `{"mode":"unknown"}`}},
		{"boundary: an ipvlan mode past the table",
			ipvlanData(&xtcpnl.IpvlanInfo{Mode: 3, HasMode: true}),
			expected{" mode unknown ", `{"mode":"unknown"}`}},
		{"boundary: a remote count past an int",
			macvlanData(&xtcpnl.MacvlanInfo{Mode: xtcpnl.MacvlanModeSource, HasMode: true, MacaddrCount: 0xffffffff, HasMacaddrCount: true}),
			expected{"mode source remotes (-1) ", `{"mode":"source","macaddr_count":-1}`}},
		{"corner: an unnamed vlan flag bit",
			vlanData(&xtcpnl.VlanInfo{ID: 1, HasID: true, Flags: 0x22, HasFlags: true}),
			expected{"protocol 802.1q id 1 <GVRP,20> ", `{"protocol":"802.1q","id":1,"flags":["GVRP","20"]}`}},
		{"corner: vlan flags in print order, not bit order",
			vlanData(&xtcpnl.VlanInfo{ID: 1, HasID: true, Flags: 0xc, HasFlags: true}),
			expected{"protocol 802.1q id 1 <MVRP,LOOSE_BINDING> ", `{"protocol":"802.1q","id":1,"flags":["MVRP","LOOSE_BINDING"]}`}},
		{"corner: an empty qos map",
			vlanData(&xtcpnl.VlanInfo{ID: 1, HasID: true, HasIngress: true}),
			expected{"protocol 802.1q id 1 \n      ingress-qos-map { } ", `{"protocol":"802.1q","id":1,"ingress_qos":[]}`}},
		{"corner: ipvlan private over vepa",
			ipvlanData(&xtcpnl.IpvlanInfo{Flags: 3, HasFlags: true}),
			expected{"private ", `{"private":true}`}},
		{"corner: remotes outside source mode",
			macvlanData(&xtcpnl.MacvlanInfo{Mode: xtcpnl.MacvlanModeBridge, HasMode: true, MacaddrCount: 2, HasMacaddrCount: true}),
			expected{"mode bridge ", `{"mode":"bridge"}`}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.data.Text(); got != tc.expected.text {
				t.Errorf("text %q, want %q", got, tc.expected.text)
			}
			b, err := json.Marshal(tc.data)
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tc.expected.json {
				t.Errorf("json %s, want %s", b, tc.expected.json)
			}
		})
	}
}
