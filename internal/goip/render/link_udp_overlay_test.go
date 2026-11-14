package render

import (
	"encoding/json"
	"net/netip"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// vxlanAtDefaults is a vxlan whose every bool the kernel sent at its
// default.
func vxlanAtDefaults() *xtcpnl.VxlanInfo {
	v := &xtcpnl.VxlanInfo{}
	for i, opt := range xtcpnl.VxlanBoolOptions {
		v.HasBools[i] = true
		if opt.Default {
			v.Bools[i] = 1
		}
	}
	return v
}

// TestUdpOverlayDataRender holds the UDP overlays' print_opts to what no
// KC4 readback reaches (docs/netlink/goip-link-kinds.md, KG3-T04): each
// row's text and its info_data JSON.
func TestUdpOverlayDataRender(t *testing.T) {
	type expected struct {
		text string
		json string
	}
	const defaultsJSON = `{"external":false,"vnifilter":false,"learning":true,"proxy":false,"rsc":false,` +
		`"l2miss":false,"l3miss":false,"udp_csum":true,"udp_zero_csum6_tx":false,"udp_zero_csum6_rx":false,` +
		`"remcsum_tx":false,"remcsum_rx":false,"localbypass":true,"mcroute":false}`
	names := fakeNames{4: {name: "d0"}}
	// A 2-byte IFLA_VXLAN_ID, which vxlan_print_opt's RTA_PAYLOAD test skips.
	shortID := xtcpnl.ParseVxlanInfoData([]byte{6, 0, 1, 0, 42, 0, 0, 0})
	tests := []struct {
		description string
		data        *KindData
		expected    expected
	}{
		{"positive: vxlan at -d -d",
			vxlanData(vxlanAtDefaults(), names, 2),
			expected{"noexternal novnifilter learning noproxy norsc nol2miss nol3miss udp_csum noudp_zero_csum6_tx " +
				"noudp_zero_csum6_rx noremcsum_tx noremcsum_rx localbypass nomcroute ", defaultsJSON}},
		{"positive: vxlan at -d hides them",
			vxlanData(vxlanAtDefaults(), names, 1),
			expected{"", defaultsJSON}},
		{"positive: vxlan's dev by name",
			vxlanData(&xtcpnl.VxlanInfo{Link: 4, HasLink: true}, names, 1),
			expected{"dev d0 ", `{"link":"d0"}`}},
		{"positive: amt's mode and ports",
			amtData(&xtcpnl.AmtInfo{Mode: 1, HasMode: true, GatewayPort: 2268, HasGatewayPort: true, RelayPort: 2269, HasRelayPort: true}, names),
			expected{"relay gateway_port 2268 relay_port 2269 ", `{"mode":"relay","gateway_port":2268,"relay_port":2269}`}},
		{"negative: a short vxlan id",
			vxlanData(shortID, names, 1),
			expected{"", `{}`}},
		{"negative: no vxlan data", vxlanData(nil, names, 2), expected{"", `{}`}},
		{"negative: no geneve data", geneveData(nil), expected{"", `{}`}},
		{"negative: no bareudp data", bareudpData(nil), expected{"", `{}`}},
		{"negative: no amt data", amtData(nil, names), expected{"", `{}`}},
		{"negative: no gtp data", gtpData(nil), expected{"", `{}`}},
		{"negative: vxlan hides dev 0",
			vxlanData(&xtcpnl.VxlanInfo{HasLink: true}, names, 1),
			expected{"", `{}`}},
		{"boundary: amt's mode past relay",
			amtData(&xtcpnl.AmtInfo{Mode: 2, HasMode: true}, names),
			expected{"unknown ", `{"mode":"unknown"}`}},
		{"boundary: gtp's role past sgsn",
			gtpData(&xtcpnl.GtpInfo{Role: 2, HasRole: true}),
			expected{"role unknown ", `{"role":"unknown"}`}},
		{"boundary: a dev past INT_MAX",
			vxlanData(&xtcpnl.VxlanInfo{Link: 1 << 31, HasLink: true}, names, 1),
			expected{"dev if2147483648 ", `{"link":"if2147483648"}`}},
		{"boundary: amt's dev 0 is ll_index_to_name's *",
			amtData(&xtcpnl.AmtInfo{HasLink: true}, names),
			expected{"dev * ", `{"link":"*"}`}},
		{"corner: vxlan hides a zero group",
			vxlanData(&xtcpnl.VxlanInfo{Group: netip.IPv4Unspecified()}, names, 1),
			expected{"", `{}`}},
		{"corner: geneve shows a zero local",
			geneveData(&xtcpnl.GeneveInfo{Local: netip.IPv4Unspecified()}),
			expected{"local 0.0.0.0 ", `{"local":"0.0.0.0"}`}},
		{"corner: geneve hides a zero local6",
			geneveData(&xtcpnl.GeneveInfo{Local: netip.IPv6Unspecified()}),
			expected{"", `{}`}},
		{"corner: geneve hides a multicast remote6",
			geneveData(&xtcpnl.GeneveInfo{Remote: netip.MustParseAddr("ff05::1")}),
			expected{"", `{}`}},
		{"corner: a v4-mapped multicast group6 is a remote6",
			vxlanData(&xtcpnl.VxlanInfo{Group: netip.MustParseAddr("::ffff:239.1.1.1")}, names, 1),
			expected{"remote ::ffff:239.1.1.1 ", `{"remote6":"::ffff:239.1.1.1"}`}},
		{"corner: a multicast group6",
			vxlanData(&xtcpnl.VxlanInfo{Group: netip.MustParseAddr("ff05::1")}, names, 1),
			expected{"group ff05::1 ", `{"group6":"ff05::1"}`}},
		{"corner: vxlan's zero aging",
			vxlanData(&xtcpnl.VxlanInfo{HasAging: true}, names, 1),
			expected{xtcpnl.VxlanKeywordAgeing + " none ", `{"` + xtcpnl.VxlanKeywordAgeing + `":0}`}},
		{"corner: vxlan's zero limit",
			vxlanData(&xtcpnl.VxlanInfo{HasLimit: true}, names, 1),
			expected{"", `{}`}},
		{"corner: df unset",
			vxlanData(&xtcpnl.VxlanInfo{HasDF: true}, names, 1),
			expected{"", `{"df":"unset"}`}},
		{"corner: a df past inherit",
			geneveData(&xtcpnl.GeneveInfo{DF: 3, HasDF: true}),
			expected{"", `{}`}},
		{"corner: ttl inherit is text only",
			geneveData(&xtcpnl.GeneveInfo{TTLInherit: 1, HasTTLInherit: true, TTL: 64, HasTTL: true}),
			expected{"ttl inherit ", `{}`}},
		{"corner: zero reserved bits are C's %#llx",
			vxlanData(&xtcpnl.VxlanInfo{HasReservedBits: true}, names, 1),
			expected{"reserved_bits 0 ", `{"reserved_bits":"0"}`}},
		{"corner: gbp is a JSON null",
			vxlanData(&xtcpnl.VxlanInfo{GBP: true}, names, 1),
			expected{"gbp ", `{"gbp":null}`}},
		{"corner: vxlan's flowlabel",
			vxlanData(&xtcpnl.VxlanInfo{Label: 0x12345, HasLabel: true}, names, 1),
			expected{"flowlabel 0x12345 ", `{"label":"0x12345"}`}},
		{"corner: bareudp's empty nest",
			bareudpData(&xtcpnl.BareudpInfo{}),
			expected{"nomultiproto ", `{"multiproto":false}`}},
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
