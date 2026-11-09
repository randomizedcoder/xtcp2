package render

import (
	"encoding/json"
	"net/netip"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestGreDataRender holds the gre kinds' print_opts to what no KC5
// readback reaches (docs/netlink/goip-link-kinds.md, KG4-T07): each row's
// text and its info_data JSON.
func TestGreDataRender(t *testing.T) {
	type expected struct {
		text string
		json string
	}
	const anyAny = `"remote":"any","local":"any","ttl":0`
	names := fakeNames{4: {name: "d0"}}
	v6 := netip.MustParseAddr("2001:db8::1")
	tests := []struct {
		description string
		data        *KindData
		expected    expected
	}{
		{"positive: gue with a source port",
			greData(&xtcpnl.GreInfo{Encap: xtcpnl.TunnelEncap{Type: 2, HasType: true, Sport: 1234, HasSport: true}}, names, false),
			expected{"remote any local any ttl inherit encap gue encap-sport 1234 encap-dport 0 noencap-csum noencap-csum6 noencap-remcsum ",
				`{` + anyAny + `,"encap":{"type":"gue","sport":1234,"dport":0,"csum":false,"csum6":false,"remcsum":false}}`}},
		{"positive: the lower by name",
			greData(&xtcpnl.GreInfo{Link: 4, HasLink: true}, names, false),
			expected{"remote any local any dev d0 ttl inherit ", `{"remote":"any","local":"any","link":"d0","ttl":0}`}},
		{"negative: no attributes: gre",
			greData(&xtcpnl.GreInfo{}, names, false),
			expected{"remote any local any ttl inherit ", `{` + anyAny + `}`}},
		{"negative: no attributes: gre6",
			greData(&xtcpnl.GreInfo{}, names, true),
			expected{"remote any local any hoplimit inherit ", `{` + anyAny + `}`}},
		{"negative: no data", greData(nil, names, false), expected{"", `{}`}},
		{"negative: a key without its flag",
			greData(&xtcpnl.GreInfo{IKey: 5, HasIKey: true, HasIFlags: true}, names, false),
			expected{"remote any local any ttl inherit ", `{` + anyAny + `}`}},
		{"negative: a zero lower is hidden",
			greData(&xtcpnl.GreInfo{HasLink: true}, names, false),
			expected{"remote any local any ttl inherit ", `{` + anyAny + `}`}},
		{"boundary: tos 2",
			greData(&xtcpnl.GreInfo{TOS: 2, HasTOS: true}, names, false),
			expected{"remote any local any ttl inherit tos 0x2 ", `{` + anyAny + `,"tos":"0x2"}`}},
		{"boundary: ttl 255",
			greData(&xtcpnl.GreInfo{TTL: 255, HasTTL: true}, names, false),
			expected{"remote any local any ttl 255 ", `{"remote":"any","local":"any","ttl":255}`}},
		{"corner: an unnamed encap type",
			greData(&xtcpnl.GreInfo{Encap: xtcpnl.TunnelEncap{Type: 3, HasType: true}}, names, false),
			expected{"remote any local any ttl inherit encap unknown encap-sport auto encap-dport 0 noencap-csum noencap-csum6 noencap-remcsum ",
				`{` + anyAny + `,"encap":{"type":null,"sport":0,"dport":0,"csum":false,"csum6":false,"remcsum":false}}`}},
		{"corner: encap type none prints nothing",
			greData(&xtcpnl.GreInfo{Encap: xtcpnl.TunnelEncap{HasType: true}}, names, false),
			expected{"remote any local any ttl inherit ", `{` + anyAny + `}`}},
		{"corner: a wrong-length remote: 16 bytes on gre",
			greData(&xtcpnl.GreInfo{Remote: v6, HasRemote: true, RemoteLen: 16}, names, false),
			expected{"remote unknown local any ttl inherit ", `{"remote":"unknown","local":"any","ttl":0}`}},
		{"corner: a wrong-length remote: 4 bytes on gre6",
			greData(&xtcpnl.GreInfo{Remote: netip.MustParseAddr("10.0.0.1"), HasRemote: true, RemoteLen: 4}, names, true),
			expected{"remote unknown local any hoplimit inherit ", `{"remote":"unknown","local":"any","ttl":0}`}},
		{"corner: a wrong-length remote: 2 bytes",
			greData(&xtcpnl.GreInfo{HasRemote: true, RemoteLen: 2}, names, false),
			expected{"remote unknown local any ttl inherit ", `{"remote":"unknown","local":"any","ttl":0}`}},
		{"corner: a zero remote is any",
			greData(&xtcpnl.GreInfo{Remote: netip.IPv6Unspecified(), HasRemote: true, RemoteLen: 16}, names, true),
			expected{"remote any local any hoplimit inherit ", `{` + anyAny + `}`}},
		{"corner: erspan_dir past egress",
			greData(&xtcpnl.GreInfo{ErspanDir: 2, HasErspanDir: true}, names, false),
			expected{"remote any local any ttl inherit erspan_dir egress ", `{` + anyAny + `,"erspan_dir":"egress"}`}},
		{"corner: mip6 is not gre6's",
			greData(&xtcpnl.GreInfo{Flags: 0x8, HasFlags: true}, names, true),
			expected{"remote any local any hoplimit inherit ", `{` + anyAny + `}`}},
		{"corner: gre6 has no tos",
			greData(&xtcpnl.GreInfo{TOS: 2, HasTOS: true}, names, true),
			expected{"remote any local any hoplimit inherit ", `{` + anyAny + `}`}},
		{"corner: the seq and csum flags",
			greData(&xtcpnl.GreInfo{IFlags: xtcpnl.GreFlagSeq | xtcpnl.GreFlagCsum, OFlags: xtcpnl.GreFlagCsum}, names, false),
			expected{"remote any local any ttl inherit iseq icsum ocsum ", `{` + anyAny + `,"iseq":true,"icsum":true,"ocsum":true}`}},
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
