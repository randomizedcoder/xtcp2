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

// The ip tunnel codec's tests, against the ipip and sit rows of the 7.3.0
// kind corpus (docs/netlink/goip-link-kinds.md, "KP4-T03 The ipip and sit
// parser").

// iptunCorpusWords are the option words after `type <kind>` of every row
// that sent a request (kind-write-corpus.exp), keyed by kind and slug: 46
// accepted and 14 refused by the kernel.
func iptunCorpusWords() map[string]string {
	const r1 = "remote 10.0.0.1"
	return map[string]string{
		"ipip/add_default":            r1,
		"ipip/add_no_option":          "",
		"ipip/add_mode_ipip":          r1 + " mode ipip",
		"ipip/add_mode_mplsip":        r1 + " mode mplsip",
		"ipip/add_mode_any":           r1 + " mode any",
		"ipip/add_mode_alias":         r1 + " mode ip4ip4",
		"ipip/add_local":              r1 + " local 10.0.0.2",
		"ipip/add_dev":                r1 + " dev d0",
		"ipip/add_ttl":                r1 + " ttl 255",
		"ipip/add_ttl_inherit":        r1 + " ttl inherit",
		"ipip/add_tos":                r1 + " tos 10",
		"ipip/add_tos_inherit":        r1 + " tos inherit",
		"ipip/add_nopmtudisc":         r1 + " nopmtudisc",
		"ipip/add_fwmark":             r1 + " fwmark 0xffffffff",
		"ipip/add_external":           "external",
		"ipip/add_encap_fou":          r1 + " encap fou encap-sport auto encap-dport 5555",
		"ipip/add_encap_gue":          r1 + " encap gue encap-sport 1000 encap-dport 6000 encap-csum encap-udp6-csum encap-remcsum",
		"ipip/add_noencap":            r1 + " encap fou encap-csum noencap noencap-csum",
		"ipip/add_6rd_ignored":        r1 + " 6rd-prefix 2001:db8::/32",
		"ipip/add_remote_multicast":   "remote 239.1.1.1",
		"ipip/set_changeable":         "hlim 64 dsf 10 fwmark 5 local 10.0.0.2",
		"ipip/set_dev":                "d d0",
		"ipip/set_mode":               "mode mplsip",
		"ipip/set_encap":              "encap fou encap-dport 5555",
		"ipip/set_nopmtudisc":         "nopmtudisc",
		"ipip/set_no_option":          "",
		"ipip/add_duplicate":          r1,
		"ipip/add_external_second":    "external",
		"ipip/set_remote_any":         "remote any",
		"ipip/set_remote_on_wildcard": r1,
		"ipip/set_remote_multicast":   "remote 239.1.1.1",
		"ipip/set_collide":            "remote 10.0.0.2",
		"ipip/set_on_external":        "ttl 64",
		"ipip/set_external":           "external",
		"sit/add_default":             r1,
		"sit/add_no_option":           "",
		"sit/add_mode_ip6ip":          r1 + " mode ip6ip",
		"sit/add_mode_ipip":           r1 + " mode ipip",
		"sit/add_mode_mplsip":         r1 + " mode mplsip",
		"sit/add_mode_any":            r1 + " mode any",
		"sit/add_mode_alias":          r1 + " mode ipv6/ipv4",
		"sit/add_local_dev":           r1 + " local 10.0.0.2 dev d0",
		"sit/add_ttl_tos":             r1 + " ttl 64 tos inherit",
		"sit/add_nopmtudisc":          r1 + " nopmtudisc",
		"sit/add_isatap":              "isatap",
		"sit/add_fwmark":              r1 + " fwmark 7",
		"sit/add_6rd_prefix":          r1 + " 6rd-prefix 2001:db8::/32 6rd-relay_prefix 10.0.0.0/8",
		"sit/add_6rd_overlong":        r1 + " 6rd-prefix 2001:db8::/48 6rd-relay_prefix 10.0.0.0/8",
		"sit/add_external":            "external",
		"sit/add_encap_fou":           r1 + " encap fou encap-sport 1000 encap-dport 5555",
		"sit/set_changeable":          "ttl 64 tos 10 local 10.0.0.2",
		"sit/set_mode":                "mode ipip",
		"sit/set_isatap":              "isatap",
		"sit/set_no_option":           "",
		"sit/add_duplicate":           r1,
		"sit/add_duplicate_wildcard":  "",
		"sit/set_remote_any":          "remote any",
		"sit/set_remote_on_inherit":   r1,
		"sit/set_collide":             "remote 10.0.0.2",
		"sit/set_external":            "external",
	}
}

// iptunModes are the mode words' protocols.
var iptunModes = map[string]uint64{
	"ipv6/ipv4": IptunProtoIPv6, "ip6ip": IptunProtoIPv6, "ipv4/ipv4": IptunProtoIPIP, "ipip": IptunProtoIPIP,
	"ip4ip4": IptunProtoIPIP, "mpls/ipv4": IptunProtoMPLS, "mplsip": IptunProtoMPLS, "any/ipv4": IptunProtoAny,
	"any": IptunProtoAny,
}

// iptunPrefix parses get_prefix's ADDR[/LEN]: the family's full length
// when no length is given.
func iptunPrefix(t testing.TB, s string) (netip.Addr, uint64) {
	t.Helper()
	addr, plen, ok := strings.Cut(s, "/")
	a := netip.MustParseAddr(addr)
	if !ok {
		return a, uint64(a.BitLen())
	}
	n, err := strconv.ParseUint(plen, 10, 8)
	if err != nil {
		t.Fatal(err)
	}
	return a, n
}

// iptunArgs parses option words as iptunnel_parse_opt does, for well-formed
// words: dev and dsfield are matches, the rest strcmp.
func iptunArgs(t testing.TB, words string, ifindex map[string]uint64) []IptunSetting {
	t.Helper()
	argv := strings.Fields(words)
	num := func(s string, base int) uint64 {
		v, err := strconv.ParseUint(s, base, 64)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	var out []IptunSetting
	for i := 0; i < len(argv); i++ {
		kw := argv[i]
		for _, k := range []string{"dev", "dsfield"} {
			if greMatchesWord(kw, k) {
				kw = k
			}
		}
		next := func() string { i++; return argv[i] }
		addr := func() netip.Addr {
			if v := next(); v != "any" {
				return netip.MustParseAddr(v)
			}
			return netip.Addr{}
		}
		if bit, ok := greEncapFlagWords[strings.TrimPrefix(kw, "no")]; ok {
			out = append(out, IptunSetting{Option: IptunOptionEncapFlag, Attr: bit, Value: vxlanOn(kw)})
			continue
		}
		switch kw {
		case "mode":
			out = append(out, IptunSetting{Option: IptunOptionMode, Value: iptunModes[next()]})
		case "remote":
			out = append(out, IptunSetting{Option: IptunOptionRemote, Addr: addr()})
		case "local":
			out = append(out, IptunSetting{Option: IptunOptionLocal, Addr: addr()})
		case "dev":
			out = append(out, IptunSetting{Option: IptunOptionLink, Value: ifindex[next()]})
		case "ttl", "hoplimit", "hlim":
			s := IptunSetting{Option: IptunOptionTTL}
			if v := next(); v != "inherit" {
				s.Value = num(v, 0)
			}
			out = append(out, s)
		case "tos", "tclass", "tc", "dsfield":
			s := IptunSetting{Option: IptunOptionTOS, Value: 1}
			if v := next(); v != "inherit" {
				s.Value = num(v, 16)
			}
			out = append(out, s)
		case "pmtudisc", "nopmtudisc":
			out = append(out, IptunSetting{Option: IptunOptionPMTUDisc, Value: vxlanOn(kw)})
		case "isatap":
			out = append(out, IptunSetting{Option: IptunOptionIsatap, Value: 1})
		case "noencap":
			out = append(out, IptunSetting{Option: IptunOptionEncapType})
		case "encap":
			out = append(out, IptunSetting{Option: IptunOptionEncapType,
				Value: map[string]uint64{"fou": TunnelEncapFOU, "gue": TunnelEncapGUE}[next()]})
		case "encap-sport":
			s := IptunSetting{Option: IptunOptionEncapSport}
			if v := next(); v != "auto" {
				s.Value = num(v, 0)
			}
			out = append(out, s)
		case "encap-dport":
			out = append(out, IptunSetting{Option: IptunOptionEncapDport, Value: num(next(), 0)})
		case "external":
			out = append(out, IptunSetting{Option: IptunOptionExternal})
		case "6rd-prefix":
			a, n := iptunPrefix(t, next())
			out = append(out, IptunSetting{Option: IptunOption6rdPrefix, Addr: a, Value: n})
		case "6rd-relay_prefix":
			a, n := iptunPrefix(t, next())
			out = append(out, IptunSetting{Option: IptunOption6rdRelayPrefix, Addr: a, Value: n})
		case "fwmark":
			out = append(out, IptunSetting{Option: IptunOptionFwMark, Value: num(next(), 0)})
		default:
			t.Fatalf("no ip tunnel keyword for %q", argv[i])
		}
	}
	return out
}

// iptunView renders g as iptunnel_print_opt prints it under -j; names maps
// an ifindex to its device's name.
func iptunView(g *IptunInfo, names map[uint64]string) map[string]any {
	out := map[string]any{
		"remote": tunnelEndpoint(g.Remote, false), "local": tunnelEndpoint(g.Local, false), "ttl": float64(g.TTL),
		"pmtudisc": g.HasPMTUDisc && g.PMTUDisc != 0,
	}
	if g.CollectMetadata {
		out["external"] = true
	}
	if name, ok := IptunProtoNames[g.Proto]; ok && g.HasProto {
		out["proto"] = name
	}
	if g.HasLink && g.Link != 0 {
		name, ok := names[uint64(g.Link)]
		if !ok {
			name = fmt.Sprintf("if%d", g.Link)
		}
		out["link"] = name
	}
	if g.TOS != 0 {
		out["tos"] = bridgeHex(uint64(g.TOS))
	}
	if g.HasFlags && g.Flags&IptunFlagSitIsatap != 0 {
		out["isatap"] = true
	}
	if g.HasRdPrefixLen && g.RdPrefixLen != 0 {
		out["prefix"] = g.RdPrefix.String()
		out["prefixlen"] = float64(g.RdPrefixLen)
		if g.RdRelayPrefix.IsValid() && !g.RdRelayPrefix.IsUnspecified() {
			out["relay_prefix"] = g.RdRelayPrefix.String()
			out["relay_prefixlen"] = float64(g.RdRelayPrefixLen)
		}
	}
	if g.FwMark != 0 {
		out["fwmark"] = bridgeHex(uint64(g.FwMark))
	}
	if e := tunnelEncapView(g.Encap); e != nil {
		out["encap"] = e
	}
	return out
}

func TestIptunDumpMatchesReadback(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	n := 0
	for _, kind := range []string{ipipKindCst, sitKindCst} {
		k := greRow(kind)
		for _, slug := range k.accepted(t) {
			n++
			t.Run("positive: "+kind+"/"+slug, func(t *testing.T) {
				got := iptunView(ParseIptunInfoData(k.dump(t, slug, pcap)), k.names(t, slug, pcap))
				want := k.readback(t, slug)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("dump renders\n %v\nreadback\n %v", got, want)
				}
			})
		}
	}
	if n != 46 {
		t.Errorf("%d accepted ipip and sit rows, want 46", n)
	}
}

func TestParseIptunInfoData(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	ipip, sit := greRow(ipipKindCst), greRow(sitKindCst)
	tests := []struct {
		description string
		nest        []byte
		nilInfo     bool
		// expected are keys of the -j view; with exact, the whole view.
		expected map[string]any
		exact    bool
		// absent are keys the view must not have.
		absent []string
	}{
		{
			description: "positive: an ipip's dump keeps ip's default proto, any",
			nest:        ipip.dump(t, "add_default", pcap),
			expected:    map[string]any{"proto": "any", "remote": "10.0.0.1", "pmtudisc": true},
		},
		{
			description: "positive: sit's isatap",
			nest:        sit.dump(t, "add_isatap", pcap),
			expected:    map[string]any{"isatap": true},
		},
		{
			description: "positive: the mpls mode",
			nest:        ipip.dump(t, "add_mode_mplsip", pcap),
			expected:    map[string]any{"proto": "mplsip"},
		},
		{
			description: "negative: a nil nest",
			nest:        nil,
			nilInfo:     true,
		},
		{
			description: "negative: a short local is dropped",
			nest:        rtattr(IflaIptunLocal, []byte{10, 0}),
			expected:    map[string]any{"local": "any"},
		},
		{
			description: "boundary: an empty nest",
			nest:        []byte{},
			expected:    map[string]any{"remote": "any", "local": "any", "ttl": float64(0), "pmtudisc": false},
			exact:       true,
		},
		{
			description: "corner: a proto with no name",
			nest:        rtattr(IflaIptunProto, []byte{47}),
			absent:      []string{"proto"},
		},
		{
			description: "corner: a 6rd prefix with no relay",
			nest: slices.Concat(rtattr(IflaIptun6rdPrefix, netip.MustParseAddr("2001:db8::").AsSlice()),
				rtattr(IflaIptun6rdRelayPrefix, []byte{0, 0, 0, 0}), rtattr(IflaIptun6rdPrefixLen, []byte{32, 0}),
				rtattr(IflaIptun6rdRelayPrefixLen, []byte{0, 0})),
			expected: map[string]any{"prefix": "2001:db8::", "prefixlen": float64(32)},
			absent:   []string{"relay_prefix", "relay_prefixlen"},
		},
		{
			description: "corner: the guest's sit has no 6rd",
			nest:        sit.dump(t, "add_6rd_prefix", pcap),
			absent:      []string{"prefix", "prefixlen", "relay_prefix", "relay_prefixlen"},
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
			got := iptunView(g, nil)
			if tt.exact && !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got %v, want %v", got, tt.expected)
			}
			for _, k := range slices.Sorted(maps.Keys(tt.expected)) {
				if !reflect.DeepEqual(got[k], tt.expected[k]) {
					t.Errorf("%s: got %v, want %v", k, got[k], tt.expected[k])
				}
			}
			for _, k := range tt.absent {
				if v, ok := got[k]; ok {
					t.Errorf("%s: got %v, want none", k, v)
				}
			}
		})
	}
}

func TestEncodeIptunInfoData(t *testing.T) {
	type row struct {
		description string
		settings    []IptunSetting
		cur         *IptunInfo
		sit         bool
		expected    []byte
		err         error
	}
	u8 := func(atype uint16, v byte) []byte { return rtattr(atype, []byte{v}) }
	zero4, zero2 := []byte{0, 0, 0, 0}, []byte{0, 0}
	// tail is what follows the endpoints, sit's FLAGS included when sit.
	tail := func(ttl byte, sit bool, flags []byte) []byte {
		out := slices.Concat(u8(IflaIptunPMTUDisc, 1), u8(IflaIptunTOS, 0), u8(IflaIptunTTL, ttl),
			rtattr(IflaIptunLink, zero4), rtattr(IflaIptunFwMark, zero4), rtattr(IflaIptunEncapType, zero2),
			rtattr(IflaIptunEncapFlags, zero2), rtattr(IflaIptunEncapSport, zero2), rtattr(IflaIptunEncapDport, zero2))
		if sit {
			out = append(out, rtattr(IflaIptunFlags, flags)...)
		}
		return out
	}
	remote := rtattr(IflaIptunRemote, []byte{10, 0, 0, 1})
	args := func(words string) []IptunSetting { return iptunArgs(t, words, nil) }
	tests := []row{
		{
			description: "negative: ttl with nopmtudisc",
			settings:    args("ttl 64 nopmtudisc"),
			err:         ErrKindSettingConflict,
		},
		{
			description: "negative: ip6ip on ipip",
			settings:    args("mode ip6ip"),
			err:         ErrKindSettingRange,
		},
		{
			description: "negative: isatap on ipip",
			settings:    args("isatap"),
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
			description: "corner: PROTO before COLLECT_METADATA",
			settings:    args("external ttl 5"),
			expected:    slices.Concat(u8(IflaIptunProto, 0), rtattr(IflaIptunCollectMetadata, nil)),
		},
		{
			description: "corner: ipip drops the 6rd words",
			settings:    args("remote 10.0.0.1 6rd-prefix 2001:db8::/32 6rd-relay_prefix 10.0.0.0/8"),
			expected:    slices.Concat(u8(IflaIptunProto, 0), remote, tail(0, false, nil)),
		},
		{
			description: "corner: a set's seeded ttl with nopmtudisc",
			settings:    args("nopmtudisc"),
			cur:         ParseIptunInfoData(tunnelPrefetch(t, greRow(ipipKindCst), "set_nopmtudisc_ttl")),
			err:         ErrKindSettingConflict,
		},
		{
			description: "corner: a set keeps a prefetched isatap",
			settings:    args("ttl 0"),
			cur:         &IptunInfo{Flags: IptunFlagSitIsatap, HasFlags: true},
			sit:         true,
			expected:    slices.Concat(u8(IflaIptunProto, 0), tail(0, true, []byte{1, 0})),
		},
	}
	words := iptunCorpusWords()
	sent := 0
	for _, kind := range []string{ipipKindCst, sitKindCst} {
		k := greRow(kind)
		ents, err := os.ReadDir(tdLinkWrite_7_3_0 + "/kinds/" + kind)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range ents {
			slug := e.Name()
			data, _, ok := k.request(t, slug)
			w, listed := words[kind+"/"+slug]
			if ok != listed {
				t.Errorf("%s/%s: sent a request %v, listed %v", kind, slug, ok, listed)
			}
			if !ok || !listed {
				continue
			}
			sent++
			var cur *IptunInfo
			if strings.HasPrefix(slug, "set_") {
				cur = ParseIptunInfoData(tunnelPrefetch(t, k, slug))
			}
			tests = append(tests, row{
				description: fmt.Sprintf("positive: %s/%s's request", kind, slug),
				settings:    iptunArgs(t, w, greIfindex(t, k, slug)),
				cur:         cur,
				sit:         kind == sitKindCst,
				expected:    append([]byte{}, data...),
			})
		}
	}
	if sent != 60 {
		t.Errorf("%d ipip and sit rows sent a request, want 60", sent)
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := EncodeIptunInfoData(tt.settings, tt.cur, tt.sit)
			if err != tt.err {
				t.Fatalf("err: got %v, want %v", err, tt.err)
			}
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("got  % x\nwant % x", got, tt.expected)
			}
		})
	}
}

func TestLinkInfoIptunKinds(t *testing.T) {
	tests := []struct {
		description string
		row         string
		expected    bool
	}{
		{description: "positive: an ipip decodes Iptun", row: "ipip/add_default", expected: true},
		{description: "positive: a sit decodes Iptun", row: "sit/add_default", expected: true},
		{description: "negative: a gre does not", row: "gre/add_default"},
		{description: "corner: an ip6tnl, with its own parser, decodes Iptun", row: "ip6tnl/add_default", expected: true},
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

func BenchmarkParseIptunInfoData(b *testing.B) {
	nest := greRow(sitKindCst).dump(b, "add_6rd_prefix", "netlink_route_getlink_detail_json.pcap")
	b.ReportAllocs()
	for b.Loop() {
		if ParseIptunInfoData(nest) == nil {
			b.Fatal("nil")
		}
	}
}
