package render

import (
	"testing"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestRenderBondLacpStrict is IFLA_BOND_LACP_STRICT under each release:
// print_on_off after all_slaves_active from 7.3.0 (30365d09), nothing
// before it.
//
// go test ./internal/goip/render/ -run TestRenderBondLacpStrict
func TestRenderBondLacpStrict(t *testing.T) {
	data := func(strict byte) []byte {
		return append(nla(unix.IFLA_BOND_ALL_SLAVES_ACTIVE, []byte{0}),
			append(nla(xtcpnl.IflaBondLacpStrict, []byte{strict}), nlaU32(unix.IFLA_BOND_MIN_LINKS, 0)...)...)
	}
	tests := []struct {
		description string
		knob        bool
		strict      byte
		expected    string
		json        string
	}{
		{description: "positive: 7.3.0 prints off, between all_slaves_active and min_links", knob: true, strict: 0,
			expected: "all_slaves_active 0 lacp_strict off min_links 0 ",
			json:     `{"all_slaves_active":0,"lacp_strict":false,"min_links":0}`},
		{description: "positive: 7.3.0 prints on", knob: true, strict: 1,
			expected: "all_slaves_active 0 lacp_strict on min_links 0 ",
			json:     `{"all_slaves_active":0,"lacp_strict":true,"min_links":0}`},
		{description: "corner: any nonzero byte is on", knob: true, strict: 7,
			expected: "all_slaves_active 0 lacp_strict on min_links 0 ",
			json:     `{"all_slaves_active":0,"lacp_strict":true,"min_links":0}`},
		{description: "negative: 7.2.0 drops the attribute", knob: false, strict: 1,
			expected: "all_slaves_active 0 min_links 0 ",
			json:     `{"all_slaves_active":0,"min_links":0}`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			old := RenderBondLacpStrict
			RenderBondLacpStrict = tc.knob
			defer func() { RenderBondLacpStrict = old }()
			k := bondData(xtcpnl.ParseBondInfoData(data(tc.strict)), fakeNames{})
			if got := k.Text(); got != tc.expected {
				t.Errorf("text %q, want %q", got, tc.expected)
			}
			b, err := k.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tc.json {
				t.Errorf("json %s, want %s", b, tc.json)
			}
		})
	}
}

// TestRenderNhDstPort is a nexthop under each release (ad2b2d95): from
// 7.3.0 fdb ends in a space and a nonzero NHA_DST_PORT follows it; before,
// the line ends at "fdb" and no port prints.
//
// go test ./internal/goip/render/ -run TestRenderNhDstPort
func TestRenderNhDstPort(t *testing.T) {
	tests := []struct {
		description string
		knob        bool
		fdb         bool
		port        uint16
		expected    string
		json        string
	}{
		{description: "positive: 7.3.0 ends fdb with a space", knob: true, fdb: true,
			expected: "id 1 via 192.0.2.1 scope link fdb ",
			json:     `{"id":1,"gateway":"192.0.2.1","scope":"link","flags":[],"fdb":null}`},
		{description: "positive: 7.3.0 prints the port after fdb", knob: true, fdb: true, port: 4789,
			expected: "id 1 via 192.0.2.1 scope link fdb dst_port 4789 ",
			json:     `{"id":1,"gateway":"192.0.2.1","scope":"link","flags":[],"fdb":null,"dst_port":4789}`},
		{description: "boundary: the largest port", knob: true, fdb: true, port: 65535,
			expected: "id 1 via 192.0.2.1 scope link fdb dst_port 65535 ",
			json:     `{"id":1,"gateway":"192.0.2.1","scope":"link","flags":[],"fdb":null,"dst_port":65535}`},
		{description: "negative: 7.2.0 ends the line at fdb and drops the port", knob: false, fdb: true, port: 4789,
			expected: "id 1 via 192.0.2.1 scope link fdb",
			json:     `{"id":1,"gateway":"192.0.2.1","scope":"link","flags":[],"fdb":null}`},
		{description: "corner: no fdb, no token, under 7.3.0", knob: true,
			expected: "id 1 via 192.0.2.1 scope link ",
			json:     `{"id":1,"gateway":"192.0.2.1","scope":"link","flags":[]}`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			old := RenderNhDstPort
			RenderNhDstPort = tc.knob
			defer func() { RenderNhDstPort = old }()
			nh := xtcpnl.NexthopInfo{ID: 1, Family: unix.AF_INET, Gateway: []byte{192, 0, 2, 1}, Scope: unix.RT_SCOPE_LINK,
				Fdb: tc.fdb, DstPort: tc.port, HasDstPort: tc.port != 0}
			got, err := NexthopText(nh, false, fakeNames{})
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.expected {
				t.Errorf("text %q, want %q", got, tc.expected)
			}
			v, err := NexthopViewOf(nh, false, fakeNames{})
			if err != nil {
				t.Fatal(err)
			}
			b, err := v.MarshalJSON()
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != tc.json {
				t.Errorf("json %s, want %s", b, tc.json)
			}
		})
	}
}
