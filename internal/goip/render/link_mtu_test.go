package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestLinkViewMTU checks print_linkinfo's `mtu %u ` (ip/ipaddress.c:1021-1024):
// printed whenever IFLA_MTU is present, 0 included (NX7b; linkset bd-001's
// `mtu 0`).
//
// go test ./internal/goip/render/ -run TestLinkViewMTU
func TestLinkViewMTU(t *testing.T) {
	tests := []struct {
		description string
		link        xtcpnl.LinkInfo
		expected    string // the text's mtu token, "" for none
		json        string // the JSON's mtu member, "" for none
	}{
		{description: "positive: a present 1500", link: xtcpnl.LinkInfo{MTU: 1500, HasMTU: true},
			expected: "mtu 1500 ", json: `"mtu":1500`},
		{description: "boundary: a present 0 prints", link: xtcpnl.LinkInfo{HasMTU: true},
			expected: "mtu 0 ", json: `"mtu":0`},
		{description: "negative: no IFLA_MTU prints nothing", link: xtcpnl.LinkInfo{}},
		{description: "corner: a hand-built non-zero MTU without HasMTU was present",
			link: xtcpnl.LinkInfo{MTU: 65536}, expected: "mtu 65536 ", json: `"mtu":65536`},
		{description: "boundary: the largest u32", link: xtcpnl.LinkInfo{MTU: 4294967295, HasMTU: true},
			expected: "mtu 4294967295 ", json: `"mtu":4294967295`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			tc.link.Index, tc.link.Name = 3, "d0"
			v := LinkViewOf(tc.link, fakeNames{})
			text := v.Text()
			if has := strings.Contains(text, "mtu "); has != (tc.expected != "") ||
				tc.expected != "" && !strings.Contains(text, "> "+tc.expected) {
				t.Errorf("text %q, expected %q after the flags", text, tc.expected)
			}
			b, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			if has := strings.Contains(string(b), `"mtu":`); has != (tc.json != "") ||
				tc.json != "" && !strings.Contains(string(b), tc.json) {
				t.Errorf("JSON %s, expected %q", b, tc.json)
			}
		})
	}
}
