package render

import (
	"encoding/json"
	"net/netip"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestIptunDataRender holds ipip's, sit's, ip6tnl's, vti's and xfrm's
// print_opts to what no KC5 readback reaches (docs/netlink/goip-link-kinds.md,
// KG4-T08): each row's text and its info_data JSON.
func TestIptunDataRender(t *testing.T) {
	type expected struct {
		text string
		json string
	}
	const (
		ipipEmpty = `"remote":"any","local":"any","ttl":0,"pmtudisc":false`
		anyAny    = `"remote":"any","local":"any"`
	)
	names := fakeNames{4: {name: "d0"}}
	rd := &xtcpnl.IptunInfo{RdPrefix: netip.MustParseAddr("2001:db8::"), RdPrefixLen: 32, HasRdPrefixLen: true,
		RdRelayPrefix: netip.MustParseAddr("10.0.0.0"), RdRelayPrefixLen: 8, HasRdRelayPrefixLen: true}
	tests := []struct {
		description string
		data        *KindData
		expected    expected
	}{
		{"positive: 6rd with a relay", iptunData(rd, names, false),
			expected{"remote any local any ttl inherit nopmtudisc 6rd-prefix 2001:db8::/32 6rd-relay_prefix 10.0.0.0/8 ",
				`{` + ipipEmpty + `,"prefix":"2001:db8::","prefixlen":32,"relay_prefix":"10.0.0.0","relay_prefixlen":8}`}},
		{"positive: vti's lower and keys",
			vtiData(&xtcpnl.VtiInfo{Link: 4, HasLink: true, IKey: 0x01020304, HasIKey: true, FwMark: 16, HasFwMark: true}, names, false),
			expected{"remote any local any dev d0 ikey 1.2.3.4 fwmark 0x10 ",
				`{` + anyAny + `,"link":"d0","ikey":"1.2.3.4","fwmark":"0x10"}`}},
		{"negative: no attributes: ipip", iptunData(&xtcpnl.IptunInfo{}, names, false),
			expected{"remote any local any ttl inherit nopmtudisc ", `{` + ipipEmpty + `}`}},
		{"negative: no attributes: ip6tnl", iptunData(&xtcpnl.IptunInfo{}, names, true),
			expected{"remote any local any hoplimit inherit ", `{` + anyAny + `,"ttl":0}`}},
		{"negative: no attributes: vti", vtiData(&xtcpnl.VtiInfo{}, names, false),
			expected{"remote any local any ", `{` + anyAny + `}`}},
		{"negative: no attributes: xfrm", xfrmData(&xtcpnl.XfrmLinkInfo{}), expected{"", `{}`}},
		{"negative: no data", iptunData(nil, names, false), expected{"", `{}`}},
		{"negative: no data: vti", vtiData(nil, names, true), expected{"", `{}`}},
		{"negative: no data: xfrm", xfrmData(nil), expected{"", `{}`}},
		{"negative: a proto with no name", iptunData(&xtcpnl.IptunInfo{Proto: 47, HasProto: true}, names, false),
			expected{"remote any local any ttl inherit nopmtudisc ", `{` + ipipEmpty + `}`}},
		{"negative: a zero key doesn't print", vtiData(&xtcpnl.VtiInfo{HasIKey: true, HasOKey: true}, names, false),
			expected{"remote any local any ", `{` + anyAny + `}`}},
		{"boundary: xfrm's if_id 0", xfrmData(&xtcpnl.XfrmLinkInfo{HasIfID: true}),
			expected{"if_id 0 ", `{"if_id":"0"}`}},
		{"boundary: xfrm's if_id at its top", xfrmData(&xtcpnl.XfrmLinkInfo{IfID: 0xffffffff, HasIfID: true}),
			expected{"if_id 0xffffffff ", `{"if_id":"0xffffffff"}`}},
		{"boundary: tos 1 is inherit in text", iptunData(&xtcpnl.IptunInfo{TOS: 1, HasTOS: true}, names, false),
			expected{"remote any local any ttl inherit tos inherit nopmtudisc ", `{"remote":"any","local":"any","ttl":0,"tos":"0x1","pmtudisc":false}`}},
		{"corner: 6rd's zero relay is hidden",
			iptunData(&xtcpnl.IptunInfo{RdPrefix: rd.RdPrefix, RdPrefixLen: 32, HasRdPrefixLen: true,
				RdRelayPrefix: netip.IPv4Unspecified(), RdRelayPrefixLen: 8, HasRdRelayPrefixLen: true}, names, false),
			expected{"remote any local any ttl inherit nopmtudisc 6rd-prefix 2001:db8::/32 ",
				`{` + ipipEmpty + `,"prefix":"2001:db8::","prefixlen":32}`}},
		{"corner: 6rd's zero prefix length is hidden",
			iptunData(&xtcpnl.IptunInfo{RdPrefix: rd.RdPrefix, HasRdPrefixLen: true}, names, false),
			expected{"remote any local any ttl inherit nopmtudisc ", `{` + ipipEmpty + `}`}},
		{"corner: a 16-byte remote on ipip",
			iptunData(&xtcpnl.IptunInfo{Remote: netip.IPv6Loopback(), HasRemote: true, RemoteLen: 16}, names, false),
			expected{"remote unknown local any ttl inherit nopmtudisc ",
				`{"remote":"unknown","local":"any","ttl":0,"pmtudisc":false}`}},
		{"corner: a 4-byte remote on vti6",
			vtiData(&xtcpnl.VtiInfo{Remote: netip.MustParseAddr("10.0.0.1"), HasRemote: true, RemoteLen: 4}, names, true),
			expected{"remote unknown local any ", `{"remote":"unknown","local":"any"}`}},
		{"corner: mip6 on ip6tnl", iptunData(&xtcpnl.IptunInfo{Flags: 0x8, HasFlags: true}, names, true),
			expected{"remote any local any hoplimit inherit mip6 ", `{` + anyAny + `,"ttl":0,"ip6_tnl_f_mip6_dev":true}`}},
		{"corner: ip6tnl's proto names", iptunData(&xtcpnl.IptunInfo{Proto: 4, HasProto: true}, names, true),
			expected{"ipip6 remote any local any hoplimit inherit ", `{"proto":"ipip6",` + anyAny + `,"ttl":0}`}},
		{"corner: external hides if_id", xfrmData(&xtcpnl.XfrmLinkInfo{External: true, IfID: 5, HasIfID: true}),
			expected{"external ", `{"external":true}`}},
		{"corner: xfrm's link is never printed", xfrmData(&xtcpnl.XfrmLinkInfo{Link: 4, HasLink: true, IfID: 5, HasIfID: true}),
			expected{"if_id 0x5 ", `{"if_id":"0x5"}`}},
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
