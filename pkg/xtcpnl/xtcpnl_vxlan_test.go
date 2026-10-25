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

// The vxlan codec's tests, against the vxlan rows of the 7.3.0 kind corpus
// (docs/netlink/goip-link-kinds.md, "KP3-T01 The vxlan parser").

var vxlanKind = kindRows{kind: vxlanKindCst, nest: unix.IFLA_INFO_DATA, key: "info_data"}

// vxlanCorpusWords are the option words after `type vxlan` of every row
// that sent a request (kind-write-corpus.exp), keyed by slug: 46 accepted
// and 12 refused by the kernel.
func vxlanCorpusWords() map[string]string {
	const base = "id 42 dstport 4789"
	m := map[string]string{
		"add_default":               "id 42",
		"add_dstport_zero":          "id 42 dstport 0",
		"add_id_zero":               "id 0 dstport 4789",
		"add_id_max":                "id 16777215 dstport 4789",
		"add_vni_abbrev":            "v 42 dstport 4789",
		"add_external":              "external",
		"add_vnifilter":             "external vnifilter dstport 4789",
		"add_gpe":                   "external gpe",
		"set_changeable":            "ttl 32 tos inherit df set " + VxlanKeywordAgeing + " 600 nolearning remote 10.0.0.2",
		"set_dev":                   "dev d0",
		"set_id_same":               "id 42",
		"add_no_option":             "",
		"add_duplicate_vni":         base,
		"add_link_local_no_dev":     "id 42 local fe80::1",
		"add_scope_mix":             "id 42 local fe80::1 remote 2001:db8::1",
		"add_flowlabel_v4":          "id 42 flowlabel 5",
		"add_srcport_inverted":      "id 42 srcport 2000 1000",
		"add_gpe_no_external":       "id 42 gpe",
		"add_reserved_bits_overlap": "id 42 reserved_bits 0x0800000000000000",
		"set_id":                    "id 43",
		"set_dstport":               "dstport 4789",
		"set_proxy":                 "proxy",
		"set_family":                "remote 2001:db8::1",
		"add_dstport":               base,
		"add_remote":                base + " remote 10.0.0.1",
		"add_remote6":               base + " remote 2001:db8::1",
		"add_group":                 base + " group 239.1.1.1 dev d0",
		"add_group6":                base + " group ff05::1 dev d0",
		"add_local":                 base + " local 10.0.0.1",
		"add_local6_link_local":     base + " local fe80::1 dev d0",
		"add_ttl":                   base + " ttl 64",
		"add_ttl_auto":              base + " ttl auto",
		"add_ttl_inherit":           base + " ttl inherit",
		"add_tos":                   base + " tos 10",
		"add_tos_inherit":           base + " tos inherit",
		"add_flowlabel":             base + " remote 2001:db8::1 flowlabel 0x12345",
		"add_maxaddress":            base + " maxaddress 100",
		"add_srcport":               base + " srcport 1000 2000",
		"add_reserved_bits":         base + " reserved_bits 0",
		"add_no_abbrev":             base + " no",
	}
	m["add_"+VxlanKeywordAgeing] = base + " " + VxlanKeywordAgeing + " 600"
	m["add_"+VxlanKeywordAgeing+"_none"] = base + " " + VxlanKeywordAgeing + " none"
	for _, df := range []string{"set", "unset", "inherit"} {
		m["add_df_"+df] = base + " df " + df
	}
	for _, flag := range strings.Fields("proxy rsc l2miss l3miss noudpcsum udp6zerocsumtx udp6zerocsumrx remcsumtx remcsumrx nolearning nolocalbypass gbp mcroute") {
		m["add_"+flag] = base + " " + flag
	}
	return m
}

// vxlanKeywords are vxlan_parse_opt's keywords in its order; an exact one
// is compared with strcmp, the rest with matches, so a prefix picks the
// first keyword it begins.
var vxlanKeywords = []struct {
	kw    string
	exact bool
}{
	{kw: "id"}, {kw: "vni"}, {kw: "group"}, {kw: "remote"}, {kw: "local"}, {kw: "dev"},
	{kw: "ttl"}, {kw: "hoplimit"}, {kw: "tos"}, {kw: "dsfield"}, {kw: "df"}, {kw: "label"},
	{kw: "flowlabel"}, {kw: VxlanKeywordAgeing}, {kw: "maxaddress"}, {kw: "port"}, {kw: "srcport"},
	{kw: "dstport"}, {kw: "nolearning"}, {kw: "learning"}, {kw: "noproxy"}, {kw: "proxy"},
	{kw: "norsc"}, {kw: "rsc"}, {kw: "nol2miss"}, {kw: "l2miss"}, {kw: "nol3miss"},
	{kw: "l3miss"}, {kw: "udpcsum"}, {kw: "noudpcsum"}, {kw: "udp6zerocsumtx"},
	{kw: "noudp6zerocsumtx"}, {kw: "udp6zerocsumrx"}, {kw: "noudp6zerocsumrx"},
	{kw: "remcsumtx"}, {kw: "noremcsumtx"}, {kw: "remcsumrx"}, {kw: "noremcsumrx"},
	{kw: "localbypass", exact: true}, {kw: "nolocalbypass", exact: true},
	{kw: "reserved_bits", exact: true}, {kw: "external"}, {kw: "noexternal"}, {kw: "gbp"},
	{kw: "gpe"}, {kw: "vnifilter", exact: true}, {kw: "novnifilter", exact: true},
	{kw: "mcroute", exact: true}, {kw: "nomcroute", exact: true},
}

// vxlanBoolWords are the u8 words VxlanOptionBool carries, without "no".
var vxlanBoolWords = map[string]uint16{
	"proxy": unix.IFLA_VXLAN_PROXY, "rsc": unix.IFLA_VXLAN_RSC,
	"l2miss": unix.IFLA_VXLAN_L2MISS, "l3miss": unix.IFLA_VXLAN_L3MISS,
	"udpcsum": unix.IFLA_VXLAN_UDP_CSUM, "udp6zerocsumtx": unix.IFLA_VXLAN_UDP_ZERO_CSUM6_TX,
	"udp6zerocsumrx": unix.IFLA_VXLAN_UDP_ZERO_CSUM6_RX, "remcsumtx": unix.IFLA_VXLAN_REMCSUM_TX,
	"remcsumrx": unix.IFLA_VXLAN_REMCSUM_RX, "localbypass": unix.IFLA_VXLAN_LOCALBYPASS,
	"vnifilter": unix.IFLA_VXLAN_VNIFILTER, "mcroute": IflaVxlanMcRoute,
}

func vxlanKeyword(t testing.TB, word string) string {
	t.Helper()
	for _, k := range vxlanKeywords {
		if word == k.kw || !k.exact && strings.HasPrefix(k.kw, word) {
			return k.kw
		}
	}
	t.Fatalf("no vxlan keyword for %q", word)
	return ""
}

// vxlanArgs parses option words as vxlan_parse_opt does, for well-formed
// words; dev names resolve through ifindex.
func vxlanArgs(t testing.TB, words string, ifindex map[string]uint64) []VxlanSetting {
	t.Helper()
	argv := strings.Fields(words)
	num := func(s string, base int) uint64 {
		v, err := strconv.ParseUint(s, base, 64)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	next := func(i *int) string { *i++; return argv[*i] }
	named := map[string]uint64{"none": 0, "unlimited": 0, "auto": 0, "unset": 0, "set": 1, "inherit": 2}
	var out []VxlanSetting
	for i := 0; i < len(argv); i++ {
		kw := vxlanKeyword(t, argv[i])
		var s VxlanSetting
		switch kw {
		case "id", "vni":
			s = VxlanSetting{Option: VxlanOptionID, Value: num(next(&i), 0)}
		case "group", "remote", "local":
			opt := map[string]VxlanOption{"group": VxlanOptionGroup, "remote": VxlanOptionRemote, "local": VxlanOptionLocal}[kw]
			s = VxlanSetting{Option: opt, Addr: netip.MustParseAddr(next(&i))}
		case "dev":
			s = VxlanSetting{Option: VxlanOptionLink, Value: ifindex[next(&i)]}
		case "ttl", "hoplimit":
			if s = (VxlanSetting{Option: VxlanOptionTTLInherit}); next(&i) != "inherit" {
				s = VxlanSetting{Option: VxlanOptionTTL, Value: named[argv[i]]}
				if argv[i] != "auto" {
					s.Value = num(argv[i], 0)
				}
			}
		case "tos", "dsfield":
			// rtnl_dsfield_a2n reads hex.
			if s = (VxlanSetting{Option: VxlanOptionTOS, Value: 1}); next(&i) != "inherit" {
				s.Value = num(argv[i], 16)
			}
		case "df":
			s = VxlanSetting{Option: VxlanOptionDF, Value: named[next(&i)]}
		case "label", "flowlabel":
			s = VxlanSetting{Option: VxlanOptionLabel, Value: num(next(&i), 0)}
		case VxlanKeywordAgeing, "maxaddress":
			opt := map[string]VxlanOption{VxlanKeywordAgeing: VxlanOptionAging, "maxaddress": VxlanOptionLimit}[kw]
			if v, ok := named[next(&i)]; ok {
				s = VxlanSetting{Option: opt, Value: v}
			} else {
				s = VxlanSetting{Option: opt, Value: num(argv[i], 0)}
			}
		case "port", "srcport":
			s = VxlanSetting{Option: VxlanOptionSrcPort, Value: num(next(&i), 0)}
			s.High = num(next(&i), 0)
		case "dstport":
			s = VxlanSetting{Option: VxlanOptionDstPort, Value: num(next(&i), 0)}
		case "learning", "nolearning":
			s = VxlanSetting{Option: VxlanOptionLearning, Value: vxlanOn(kw)}
		case "external", "noexternal":
			s = VxlanSetting{Option: VxlanOptionExternal, Value: vxlanOn(kw)}
		case "reserved_bits":
			s = VxlanSetting{Option: VxlanOptionReservedBits, Value: num(next(&i), 0)}
		case "gbp":
			s = VxlanSetting{Option: VxlanOptionGBP}
		case "gpe":
			s = VxlanSetting{Option: VxlanOptionGPE}
		default:
			s = VxlanSetting{Option: VxlanOptionBool, Attr: vxlanBoolWords[strings.TrimPrefix(kw, "no")], Value: vxlanOn(kw)}
		}
		out = append(out, s)
	}
	return out
}

func vxlanOn(kw string) uint64 {
	if strings.HasPrefix(kw, "no") {
		return 0
	}
	return 1
}

// vxlanView renders v as vxlan_print_opt prints it under -j, naming the
// lower through names.
func vxlanView(v *VxlanInfo, names map[uint64]string) map[string]any {
	out := map[string]any{}
	if v.HasID {
		out["id"] = float64(v.ID)
	}
	if v.Group.IsValid() && !v.Group.IsUnspecified() {
		key := map[bool]string{true: "remote", false: "group"}[!v.Group.IsMulticast()]
		if v.Group.Is6() {
			key += "6"
		}
		out[key] = v.Group.String()
	}
	if v.Local.IsValid() && !v.Local.IsUnspecified() {
		out[map[bool]string{true: "local", false: "local6"}[v.Local.Is4()]] = v.Local.String()
	}
	if v.HasLink && v.Link != 0 {
		name, ok := names[uint64(v.Link)]
		if !ok {
			name = fmt.Sprintf("if%d", v.Link)
		}
		out["link"] = name
	}
	if v.HasPortRange {
		out["port_range"] = map[string]any{"low": float64(v.PortLow), "high": float64(v.PortHigh)}
	}
	if v.HasPort {
		out["port"] = float64(v.Port)
	}
	if v.TOS != 0 {
		out["tos"] = bridgeHex(uint64(v.TOS))
	}
	if v.TTLInherit == 0 && v.HasTTL {
		out["ttl"] = float64(v.TTL)
	}
	if v.HasDF && v.DF <= VxlanDfInherit {
		out["df"] = []string{"unset", "set", "inherit"}[v.DF]
	}
	if v.Label != 0 {
		out["label"] = bridgeHex(uint64(v.Label))
	}
	if v.HasAging {
		out[VxlanKeywordAgeing] = float64(v.Aging)
	}
	if v.Limit != 0 {
		out["limit"] = float64(v.Limit)
	}
	if v.HasReservedBits {
		out["reserved_bits"] = bridgeHex(v.ReservedBits)
	}
	if v.GBP {
		out["gbp"] = nil
	}
	if v.GPE {
		out["gpe"] = nil
	}
	for i, o := range VxlanBoolOptions {
		if v.HasBools[i] {
			out[o.Key] = v.Bools[i] != 0
		}
	}
	return out
}

// vxlanIfindex maps the names row slug's ip.pcap and readback resolve to
// their ifindex: ll_name_to_index's lookups.
func vxlanIfindex(t testing.TB, slug string) map[string]uint64 {
	t.Helper()
	out := map[string]uint64{}
	for _, pcap := range []string{"ip.pcap", "netlink_route_getlink_detail_json.pcap"} {
		for idx, name := range vxlanKind.names(t, slug, pcap) {
			out[name] = idx
		}
	}
	return out
}

func TestVxlanDumpMatchesReadback(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	slugs := vxlanKind.accepted(t)
	for _, slug := range slugs {
		t.Run("positive: "+slug, func(t *testing.T) {
			got := vxlanView(ParseVxlanInfoData(vxlanKind.dump(t, slug, pcap)), vxlanKind.names(t, slug, pcap))
			want := vxlanKind.readback(t, slug)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("dump renders\n %v\nreadback\n %v", got, want)
			}
		})
	}
	if len(slugs) != 46 {
		t.Errorf("%d accepted vxlan rows, want 46", len(slugs))
	}
}

func TestParseVxlanInfoData(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	tests := []struct {
		description string
		nest        []byte
		names       map[uint64]string
		nilInfo     bool
		// expected are keys of the -j view; with exact, the whole view.
		expected map[string]any
		exact    bool
		absent   []string
		unknown  []uint16
	}{
		{
			description: "positive: a bare vxlan's dump",
			nest:        vxlanKind.dump(t, "add_default", pcap),
			expected: map[string]any{
				"id": float64(42), "port": float64(8472), VxlanKeywordAgeing: float64(300),
				"learning": true, "udp_csum": true, "localbypass": true,
			},
		},
		{
			description: "positive: a multicast group and its lower",
			nest:        vxlanKind.dump(t, "add_group", pcap),
			names:       vxlanKind.names(t, "add_group", pcap),
			expected:    map[string]any{"group": "239.1.1.1", "link": "d0"},
			absent:      []string{"remote"},
		},
		{
			description: "positive: an IPv6 remote",
			nest:        vxlanKind.dump(t, "add_remote6", pcap),
			expected:    map[string]any{"remote6": "2001:db8::1"},
		},
		{
			description: "positive: the source port range",
			nest:        vxlanKind.dump(t, "add_srcport", pcap),
			expected:    map[string]any{"port_range": map[string]any{"low": float64(1000), "high": float64(2000)}},
		},
		{
			description: "positive: ttl inherit",
			nest:        vxlanKind.dump(t, "add_ttl_inherit", pcap),
			expected:    map[string]any{},
			absent:      []string{"ttl"},
		},
		{
			description: "negative: a nil nest",
			nest:        nil,
			nilInfo:     true,
		},
		{
			description: "negative: a short id",
			nest:        rtattr(unix.IFLA_VXLAN_ID, []byte{42, 0}),
			expected:    map[string]any{},
			exact:       true,
		},
		{
			description: "boundary: an empty nest",
			nest:        []byte{},
			expected:    map[string]any{},
			exact:       true,
		},
		{
			description: "boundary: the highest VNI",
			nest:        vxlanKind.dump(t, "add_id_max", pcap),
			expected:    map[string]any{"id": float64(16777215)},
		},
		{
			description: "corner: the port is big-endian",
			nest:        rtattr(unix.IFLA_VXLAN_PORT, []byte{0x12, 0xb5}),
			expected:    map[string]any{"port": float64(4789)},
			exact:       true,
		},
		{
			description: "corner: reserved bits are big-endian",
			nest:        rtattr(IflaVxlanReservedBits, []byte{0xf7, 0xff, 0xff, 0xff, 0, 0, 0, 0xff}),
			expected:    map[string]any{"reserved_bits": "0xf7ffffff000000ff"},
			exact:       true,
		},
		{
			description: "corner: a group of 0.0.0.0 prints nothing",
			nest:        rtattr(unix.IFLA_VXLAN_GROUP, []byte{0, 0, 0, 0}),
			expected:    map[string]any{},
			exact:       true,
		},
		{
			description: "corner: GROUP hides GROUP6, whatever the order",
			nest: slices.Concat(rtattr(unix.IFLA_VXLAN_GROUP6, netip.MustParseAddr("ff05::1").AsSlice()),
				rtattr(unix.IFLA_VXLAN_GROUP, []byte{10, 0, 0, 1})),
			expected: map[string]any{"remote": "10.0.0.1"},
			exact:    true,
		},
		{
			description: "corner: an id past the last IFLA_VXLAN_* is listed",
			nest:        slices.Concat(rtattr(40, []byte{1}), rtattr(unix.IFLA_VXLAN_PORT, []byte{0x12, 0xb5})),
			expected:    map[string]any{"port": float64(4789)},
			exact:       true,
			unknown:     []uint16{40},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			v := ParseVxlanInfoData(tt.nest)
			if (v == nil) != tt.nilInfo {
				t.Fatalf("nil info: got %v, want %v", v == nil, tt.nilInfo)
			}
			if v == nil {
				return
			}
			got := vxlanView(v, tt.names)
			if tt.exact && !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got %v, want %v", got, tt.expected)
			}
			for _, k := range slices.Sorted(maps.Keys(tt.expected)) {
				if !reflect.DeepEqual(got[k], tt.expected[k]) {
					t.Errorf("%s: got %v, want %v", k, got[k], tt.expected[k])
				}
			}
			for _, k := range tt.absent {
				if _, ok := got[k]; ok {
					t.Errorf("%s present: %v", k, got[k])
				}
			}
			if !slices.Equal(v.Unknown, tt.unknown) {
				t.Errorf("unknown: got %v, want %v", v.Unknown, tt.unknown)
			}
		})
	}
}

func TestEncodeVxlanInfoData(t *testing.T) {
	type row struct {
		description string
		settings    []VxlanSetting
		set         bool
		expected    []byte
		err         error
	}
	u8 := func(atype uint16, v byte) []byte { return rtattr(atype, []byte{v}) }
	id42 := rtattr(unix.IFLA_VXLAN_ID, []byte{42, 0, 0, 0})
	args := func(words string) []VxlanSetting { return vxlanArgs(t, words, nil) }
	tests := []row{
		{
			description: "negative: a VNI too wide",
			settings:    []VxlanSetting{{Option: VxlanOptionID, Value: 1 << 24}},
			err:         ErrKindSettingRange,
		},
		{
			description: "negative: a setting with no option",
			settings:    []VxlanSetting{{}},
			err:         ErrKindSettingOption,
		},
		{
			description: "negative: external with an id",
			settings:    args("external id 42"),
			err:         ErrVxlanConflict,
		},
		{
			description: "negative: a group without dev",
			settings:    args("id 42 group 239.1.1.1"),
			err:         ErrVxlanConflict,
		},
		{
			description: "negative: a multicast remote",
			settings:    args("id 42 remote 239.1.1.1"),
			err:         ErrKindSettingRange,
		},
		{
			description: "boundary: dstport 0 sends no PORT",
			settings:    args("id 42 dstport 0"),
			expected:    slices.Concat(id42, u8(unix.IFLA_VXLAN_LEARNING, 1)),
		},
		{
			description: "boundary: a zero source range sends nothing",
			settings:    args("srcport 0 0"),
			set:         true,
			expected:    nil,
		},
		{
			description: "corner: the id goes after the loop",
			settings:    args("ttl 5 id 42"),
			expected:    slices.Concat(u8(unix.IFLA_VXLAN_TTL, 5), id42, u8(unix.IFLA_VXLAN_LEARNING, 1)),
		},
		{
			description: "corner: a set sends no learning by default",
			settings:    args("ttl 5"),
			set:         true,
			expected:    u8(unix.IFLA_VXLAN_TTL, 5),
		},
		{
			description: "corner: external turns learning off",
			settings:    args("external"),
			expected:    slices.Concat(u8(unix.IFLA_VXLAN_COLLECT_METADATA, 1), u8(unix.IFLA_VXLAN_LEARNING, 0)),
		},
		{
			description: "corner: gpe picks 4790",
			settings:    args("external gpe"),
			expected: slices.Concat(u8(unix.IFLA_VXLAN_COLLECT_METADATA, 1), rtattr(unix.IFLA_VXLAN_GPE, nil),
				u8(unix.IFLA_VXLAN_LEARNING, 0), rtattr(unix.IFLA_VXLAN_PORT, []byte{0x12, 0xb6})),
		},
	}
	words := vxlanCorpusWords()
	ents, err := os.ReadDir(tdLinkWrite_7_3_0 + "/kinds/" + vxlanKindCst)
	if err != nil {
		t.Fatal(err)
	}
	sent := 0
	for _, e := range ents {
		slug := e.Name()
		data, _, ok := vxlanKind.request(t, slug)
		w, listed := words[slug]
		if ok != listed {
			t.Errorf("%s: sent a request %v, listed %v", slug, ok, listed)
		}
		if !ok || !listed {
			continue
		}
		sent++
		tests = append(tests, row{
			description: fmt.Sprintf("positive: %s's request", slug),
			settings:    vxlanArgs(t, w, vxlanIfindex(t, slug)),
			set:         strings.HasPrefix(slug, "set_"),
			expected:    append([]byte{}, data...),
		})
	}
	if sent != 58 {
		t.Errorf("%d vxlan rows sent a request, want 58", sent)
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := EncodeVxlanInfoData(tt.settings, tt.set)
			if err != tt.err {
				t.Fatalf("err: got %v, want %v", err, tt.err)
			}
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("got  % x\nwant % x", got, tt.expected)
			}
		})
	}
}

func TestLinkInfoVxlanKinds(t *testing.T) {
	tests := []struct {
		description string
		row         string
		expected    bool
	}{
		{description: "positive: a vxlan decodes Vxlan", row: "vxlan/add_group", expected: true},
		{description: "positive: an external vxlan decodes Vxlan", row: "vxlan/add_external", expected: true},
		{description: "negative: an ipvlan does not", row: "ipvlan/add_default"},
		{description: "corner: a geneve, another UDP overlay, does not", row: "geneve/add_default"},
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
			if got := li.Vxlan != nil; got != tt.expected {
				t.Errorf("Vxlan %v for kind %q, want %v", got, li.Kind, tt.expected)
			}
		})
	}
}

func BenchmarkParseVxlanInfoData(b *testing.B) {
	nest := vxlanKind.dump(b, "add_group", "netlink_route_getlink_detail_json.pcap")
	b.ReportAllocs()
	for b.Loop() {
		if ParseVxlanInfoData(nest) == nil {
			b.Fatal("nil")
		}
	}
}
