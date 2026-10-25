package xtcpnl

import (
	"bytes"
	"fmt"
	"maps"
	"net/netip"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The ip6tnl encoder's tests, against the ip6tnl rows of the 7.3.0 kind
// corpus (docs/netlink/goip-link-kinds.md, "KP4-T04 The ip6tnl parser").

var ip6tnlKind = greRow(ip6tnlKindCst)

// ip6tnlCorpusWords are the option words after `type ip6tnl` of every row
// that sent a request (kind-write-corpus.exp), keyed by slug: 29 accepted
// and 7 refused by the kernel.
func ip6tnlCorpusWords() map[string]string {
	const r1 = "remote 2001:db8::1"
	return map[string]string{
		"add_default":            r1,
		"add_no_option":          "",
		"add_mode_ip6ip6":        r1 + " mode ip6ip6",
		"add_mode_ipip6":         r1 + " mode ipip6",
		"add_mode_any":           r1 + " mode any",
		"add_mode_alias":         r1 + " mode ip4ip6",
		"add_local":              r1 + " local 2001:db8::2",
		"add_dev":                r1 + " dev d0",
		"add_hoplimit":           r1 + " hoplimit 255",
		"add_hoplimit_inherit":   r1 + " hoplimit inherit",
		"add_hoplimit_twice":     r1 + " hoplimit 10 ttl 20",
		"add_encaplimit":         r1 + " encaplimit 0",
		"add_encaplimit_none":    r1 + " encaplimit none",
		"add_tclass":             r1 + " tclass ff",
		"add_tclass_inherit":     r1 + " tclass inherit",
		"add_dsfield_abbrev":     r1 + " dsf 10",
		"add_flowlabel":          r1 + " flowlabel 0xfffff",
		"add_flowlabel_inherit":  r1 + " fl inherit",
		"add_dscp_inherit":       r1 + " dscp inherit",
		"add_fwmark":             r1 + " fwmark 0xffffffff",
		"add_fwmark_inherit":     r1 + " fwmark inherit",
		"add_allow_localremote":  r1 + " allow-localremote",
		"add_external":           "external",
		"add_noencap":            r1 + " noencap encap-sport 1000 encap-dport 5555 encap-csum noencap-udp6-csum encap-remcsum",
		"set_changeable":         "hlim 32 encaplimit none tclass inherit flowlabel 0x12345 dscp inherit",
		"set_mode":               "mode ipip6",
		"set_remote":             "remote 2001:db8::2",
		"set_keeps_prefetch":     "tclass 0x20",
		"set_no_option":          "",
		"add_encap_fou":          r1 + " encap fou encap-dport 5555",
		"add_duplicate":          r1,
		"add_duplicate_wildcard": "",
		"add_external_second":    "external",
		"set_collide":            "remote 2001:db8::2",
		"set_on_external":        "hoplimit 5",
		"set_external":           "external",
	}
}

// ip6tnlModes are link_ip6tnl.c's mode words' protocols.
var ip6tnlModes = map[string]uint64{
	"ipv6/ipv6": IptunProtoIPv6, "ip6ip6": IptunProtoIPv6, "ip/ipv6": IptunProtoIPIP, "ipv4/ipv6": IptunProtoIPIP,
	"ipip6": IptunProtoIPIP, "ip4ip6": IptunProtoIPIP, "any/ipv6": IptunProtoAny, "any": IptunProtoAny,
}

// ip6tnlShared are the keywords link_ip6tnl.c parses as link_iptnl.c does,
// with the number of values each takes.
var ip6tnlShared = map[string]int{
	"remote": 1, "local": 1, "dev": 1, "ttl": 1, "hoplimit": 1, "hlim": 1, "noencap": 0, "encap": 1,
	"encap-sport": 1, "encap-dport": 1, "external": 0,
}

// ip6tnlArgs parses option words as ip6tunnel_parse_opt does, for
// well-formed words: its own words here, the rest through iptunArgs.
func ip6tnlArgs(t testing.TB, words string, ifindex map[string]uint64) []IptunSetting {
	t.Helper()
	argv := strings.Fields(words)
	hex := func(s string) uint64 {
		v, err := strconv.ParseUint(strings.TrimPrefix(s, "0x"), 16, 64)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	flag := func(bit uint16, on uint64) IptunSetting {
		return IptunSetting{Option: IptunOptionTnlFlag, Attr: bit, Value: on}
	}
	var out []IptunSetting
	for i := 0; i < len(argv); i++ {
		kw := argv[i]
		for _, k := range []string{"dev", "dsfield"} {
			if greMatchesWord(kw, k) {
				kw = k
				break
			}
		}
		if _, ok := greEncapFlagWords[strings.TrimPrefix(kw, "no")]; ok {
			out = append(out, iptunArgs(t, kw, ifindex)...)
			continue
		}
		if n, ok := ip6tnlShared[kw]; ok {
			out = append(out, iptunArgs(t, strings.Join(append([]string{kw}, argv[i+1:i+1+n]...), " "), ifindex)...)
			i += n
			continue
		}
		next := func() string { i++; return argv[i] }
		switch kw {
		case "mode":
			out = append(out, IptunSetting{Option: IptunOptionMode, Value: ip6tnlModes[next()]})
		case "encaplimit":
			if v := next(); v == "none" {
				out = append(out, flag(Ip6TnlFlagIgnEncapLimit, 1))
			} else {
				n, err := strconv.ParseUint(v, 0, 8)
				if err != nil {
					t.Fatal(err)
				}
				out = append(out, IptunSetting{Option: IptunOptionEncapLimit, Value: n})
			}
		case "tos", "tclass", "tc", "dsfield":
			if v := next(); v == "inherit" {
				out = append(out, flag(Ip6TnlFlagUseOrigTclass, 1))
			} else {
				out = append(out, IptunSetting{Option: IptunOptionTClass, Value: hex(v)})
			}
		case "flowlabel", "fl":
			if v := next(); v == "inherit" {
				out = append(out, flag(Ip6TnlFlagUseOrigFlowlabel, 1))
			} else {
				out = append(out, IptunSetting{Option: IptunOptionFlowLabel, Value: hex(v)})
			}
		case "dscp":
			next()
			out = append(out, flag(Ip6TnlFlagRcvDscpCopy, 1))
		case "fwmark":
			if v := next(); v == "inherit" {
				out = append(out, flag(Ip6TnlFlagUseOrigFwmark, 1))
			} else {
				out = append(out, iptunArgs(t, "fwmark "+v, nil)...)
			}
		case "allow-localremote", "noallow-localremote":
			out = append(out, flag(Ip6TnlFlagAllowLocalRemote, vxlanOn(kw)))
		default:
			t.Fatalf("no ip6tnl keyword for %q", argv[i])
		}
	}
	return out
}

// ip6tnlView renders g as ip6tunnel_print_opt prints it under -j; names
// maps an ifindex to its device's name.
func ip6tnlView(g *IptunInfo, names map[uint64]string) map[string]any {
	out := map[string]any{
		"remote": tunnelEndpoint(g.Remote, true), "local": tunnelEndpoint(g.Local, true), "ttl": float64(g.TTL),
	}
	if g.CollectMetadata {
		out["external"] = true
	}
	if name, ok := Ip6tnlProtoNames[g.Proto]; ok && g.HasProto {
		out["proto"] = name
	}
	if g.HasLink && g.Link != 0 {
		name, ok := names[uint64(g.Link)]
		if !ok {
			name = fmt.Sprintf("if%d", g.Link)
		}
		out["link"] = name
	}
	// The encap limit, tclass, flowlabel and dscp pairs print as
	// link_gre6.c's do.
	gre6FlowView(out, &GreInfo{EncapLimit: g.EncapLimit, HasEncapLimit: g.HasEncapLimit, FlowInfo: g.FlowInfo,
		HasFlowInfo: g.HasFlowInfo, Flags: g.Flags})
	for key, bit := range map[string]uint32{
		"ip6_tnl_f_mip6_dev": Ip6TnlFlagMIP6Dev, "ip6_tnl_f_allow_local_remote": Ip6TnlFlagAllowLocalRemote,
	} {
		if g.Flags&bit != 0 {
			out[key] = true
		}
	}
	switch {
	case g.Flags&Ip6TnlFlagUseOrigFwmark != 0:
		out["ip6_tnl_f_use_orig_fwmark"] = true
	case g.FwMark != 0:
		out["fwmark"] = bridgeHex(uint64(g.FwMark))
	}
	if e := tunnelEncapView(g.Encap); e != nil {
		out["encap"] = e
	}
	return out
}

func TestIp6tnlDumpMatchesReadback(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	slugs := ip6tnlKind.accepted(t)
	for _, slug := range slugs {
		t.Run("positive: "+slug, func(t *testing.T) {
			got := ip6tnlView(ParseIptunInfoData(ip6tnlKind.dump(t, slug, pcap)), ip6tnlKind.names(t, slug, pcap))
			want := ip6tnlKind.readback(t, slug)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("dump renders\n %v\nreadback\n %v", got, want)
			}
		})
	}
	if len(slugs) != 29 {
		t.Errorf("%d accepted ip6tnl rows, want 29", len(slugs))
	}
}

func TestParseIp6tnlInfoData(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	dump := func(slug string) []byte { return ip6tnlKind.dump(t, slug, pcap) }
	tests := []struct {
		description string
		nest        []byte
		nilInfo     bool
		// expected are keys of the -j view; with exact, the whole view.
		expected map[string]any
		exact    bool
	}{
		{
			description: "positive: an ip6tnl's dump",
			nest:        dump("add_default"),
			expected: map[string]any{"proto": "any", "remote": "2001:db8::1", "ttl": float64(64),
				"encap_limit": float64(4)},
		},
		{
			description: "positive: a flow label",
			nest:        dump("add_flowlabel"),
			expected:    map[string]any{"flowlabel": "0xfffff"},
		},
		{
			description: "positive: the tclass inherit flag",
			nest:        dump("add_tclass_inherit"),
			expected:    map[string]any{"ip6_tnl_f_use_orig_tclass": true},
		},
		{
			description: "positive: the flowlabel inherit flag",
			nest:        dump("add_flowlabel_inherit"),
			expected:    map[string]any{"ip6_tnl_f_use_orig_flowlabel": true},
		},
		{
			description: "positive: the fwmark inherit flag",
			nest:        dump("add_fwmark_inherit"),
			expected:    map[string]any{"ip6_tnl_f_use_orig_fwmark": true},
		},
		{
			description: "negative: a nil nest",
			nest:        nil,
			nilInfo:     true,
		},
		{
			description: "negative: a 4-byte local",
			nest:        rtattr(IflaIptunLocal, []byte{10, 0, 0, 1}),
			expected:    map[string]any{"local": "unknown"},
		},
		{
			description: "boundary: an empty nest",
			nest:        []byte{},
			expected:    map[string]any{"remote": "any", "local": "any", "ttl": float64(0)},
			exact:       true,
		},
		{
			description: "corner: mip6 comes only from the kernel",
			nest:        rtattr(IflaIptunFlags, []byte{8, 0, 0, 0}),
			expected:    map[string]any{"remote": "any", "local": "any", "ttl": float64(0), "ip6_tnl_f_mip6_dev": true},
			exact:       true,
		},
		{
			description: "corner: an ipip-in-IPv6 tunnel",
			nest:        dump("add_mode_ipip6"),
			expected:    map[string]any{"proto": "ipip6"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			g := ParseIptunInfoData(tt.nest)
			if (g == nil) != tt.nilInfo {
				t.Fatalf("nil info: got %v, want %v", g == nil, tt.nilInfo)
			}
			if g == nil {
				return
			}
			got := ip6tnlView(g, nil)
			if tt.exact && !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got %v, want %v", got, tt.expected)
			}
			for _, k := range slices.Sorted(maps.Keys(tt.expected)) {
				if !reflect.DeepEqual(got[k], tt.expected[k]) {
					t.Errorf("%s: got %v, want %v", k, got[k], tt.expected[k])
				}
			}
		})
	}
}

func TestEncodeIp6tnlInfoData(t *testing.T) {
	type row struct {
		description string
		settings    []IptunSetting
		cur         *IptunInfo
		expected    []byte
		err         error
	}
	u8 := func(atype uint16, v byte) []byte { return rtattr(atype, []byte{v}) }
	zero4, zero2 := []byte{0, 0, 0, 0}, []byte{0, 0}
	// body is what follows the endpoints.
	body := func(ttl byte, flowinfo []byte) []byte {
		return slices.Concat(u8(IflaIptunTTL, ttl), u8(IflaIptunEncapLimit, 4), rtattr(IflaIptunFlowInfo, flowinfo),
			rtattr(IflaIptunFlags, zero4), rtattr(IflaIptunLink, zero4), rtattr(IflaIptunFwMark, zero4),
			rtattr(IflaIptunEncapType, zero2), rtattr(IflaIptunEncapFlags, []byte{2, 0}),
			rtattr(IflaIptunEncapSport, zero2), rtattr(IflaIptunEncapDport, zero2))
	}
	proto := u8(IflaIptunProto, 0)
	remote := rtattr(IflaIptunRemote, netip.MustParseAddr("2001:db8::1").AsSlice())
	args := func(words string) []IptunSetting { return ip6tnlArgs(t, words, nil) }
	tests := []row{
		{
			description: "negative: a flow label past 20 bits",
			settings:    args("flowlabel 100000"),
			err:         ErrKindSettingRange,
		},
		{
			description: "negative: the mplsip mode",
			settings:    []IptunSetting{{Option: IptunOptionMode, Value: IptunProtoMPLS}},
			err:         ErrKindSettingRange,
		},
		{
			description: "negative: sit's isatap",
			settings:    []IptunSetting{{Option: IptunOptionIsatap, Value: 1}},
			err:         ErrKindSettingOption,
		},
		{
			description: "negative: a setting with no option",
			settings:    []IptunSetting{{}},
			err:         ErrKindSettingOption,
		},
		{
			description: "boundary: no settings",
			settings:    nil,
			expected:    nil,
		},
		{
			description: "boundary: the last hoplimit wins",
			settings:    args("remote 2001:db8::1 hoplimit 10 ttl 20"),
			expected:    slices.Concat(proto, remote, body(20, zero4)),
		},
		{
			description: "corner: a set resets the encap",
			settings:    args("tclass 0x20"),
			cur: &IptunInfo{Remote: netip.MustParseAddr("2001:db8::1"), TTL: 10, HasTTL: true, EncapLimit: 4,
				HasEncapLimit: true, Encap: TunnelEncap{Type: TunnelEncapFOU, HasType: true, Dport: 5555, HasDport: true}},
			expected: slices.Concat(proto, remote, body(10, []byte{0x02, 0, 0, 0})),
		},
		{
			description: "corner: PROTO first, then metadata",
			settings:    args("external hoplimit 5"),
			expected:    slices.Concat(proto, rtattr(IflaIptunCollectMetadata, nil)),
		},
	}
	words := ip6tnlCorpusWords()
	ents, err := os.ReadDir(tdLinkWrite_7_3_0 + "/kinds/" + ip6tnlKindCst)
	if err != nil {
		t.Fatal(err)
	}
	sent := 0
	for _, e := range ents {
		slug := e.Name()
		data, _, ok := ip6tnlKind.request(t, slug)
		w, listed := words[slug]
		if ok != listed {
			t.Errorf("%s: sent a request %v, listed %v", slug, ok, listed)
		}
		if !ok || !listed {
			continue
		}
		sent++
		var cur *IptunInfo
		if strings.HasPrefix(slug, "set_") {
			cur = ParseIptunInfoData(tunnelPrefetch(t, ip6tnlKind, slug))
		}
		tests = append(tests, row{
			description: fmt.Sprintf("positive: %s's request", slug),
			settings:    ip6tnlArgs(t, w, greIfindex(t, ip6tnlKind, slug)),
			cur:         cur,
			expected:    append([]byte{}, data...),
		})
	}
	if sent != 36 {
		t.Errorf("%d ip6tnl rows sent a request, want 36", sent)
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := EncodeIp6tnlInfoData(tt.settings, tt.cur)
			if err != tt.err {
				t.Fatalf("err: got %v, want %v", err, tt.err)
			}
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("got  % x\nwant % x", got, tt.expected)
			}
		})
	}
}

func TestLinkInfoIp6tnlKinds(t *testing.T) {
	tests := []struct {
		description string
		row         string
		expected    bool
	}{
		{description: "positive: an ip6tnl decodes Iptun", row: "ip6tnl/add_default", expected: true},
		{description: "positive: an ipip6 ip6tnl decodes Iptun", row: "ip6tnl/add_mode_ipip6", expected: true},
		{description: "negative: a vti6 does not", row: "vti6/add_default"},
		{description: "corner: an external ip6tnl decodes Iptun", row: "ip6tnl/add_external", expected: true},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			var body []byte
			for _, m := range pcapNlMsgs(t, tdLinkWrite_7_3_0+"/kinds/"+tt.row+"/netlink_route_getlink_detail.pcap") {
				if m.h.Type == unix.RTM_NEWLINK && m.h.Flags&unix.NLM_F_REQUEST == 0 && len(m.body) > len(body) {
					body = m.body
				}
			}
			li, err := ParseNewLink(body)
			if err != nil {
				t.Fatal(err)
			}
			if got := li.Iptun != nil; got != tt.expected {
				t.Errorf("Iptun %v for kind %q, want %v", got, li.Kind, tt.expected)
			}
		})
	}
}

func BenchmarkParseIp6tnlInfoData(b *testing.B) {
	nest := ip6tnlKind.dump(b, "add_flowlabel", "netlink_route_getlink_detail_json.pcap")
	b.ReportAllocs()
	for b.Loop() {
		if ParseIptunInfoData(nest) == nil {
			b.Fatal("nil")
		}
	}
}
