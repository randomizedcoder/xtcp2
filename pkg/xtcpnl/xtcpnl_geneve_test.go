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

// The geneve codec's tests, against the geneve rows of the 7.3.0 kind
// corpus (docs/netlink/goip-link-kinds.md, "KP3-T02 The geneve parser").

var geneveKind = kindRows{kind: geneveKindCst, nest: unix.IFLA_INFO_DATA, key: "info_data"}

// geneveCorpusWords are the option words after `type geneve` of every row
// that sent a request (kind-write-corpus.exp), keyed by slug: 22 accepted
// and 13 refused by the kernel.
func geneveCorpusWords() map[string]string {
	const base = "id 1 remote 10.0.0.1"
	return map[string]string{
		"add_default":            base,
		"add_remote6":            "id 1 remote 2001:db8::1",
		"add_external":           "external",
		"add_id_max":             "id 16777215 remote 10.0.0.1",
		"add_abbrev":             "i 1 r 10.0.0.1",
		"add_local":              base + " local 10.0.0.2",
		"add_local6":             "id 1 remote 2001:db8::1 local 2001:db8::2",
		"add_external_local":     "external local 192.168.0.1",
		"add_external_local6":    "external local 2001:db8::1",
		"add_ttl":                base + " ttl 64",
		"add_ttl_inherit":        base + " ttl inherit",
		"add_tos":                base + " tos 10",
		"add_df":                 base + " df set",
		"add_dstport":            base + " dstport 4789",
		"add_dstport_zero":       base + " dstport 0",
		"add_flowlabel":          "id 1 remote 2001:db8::1 flowlabel 0xfffff",
		"add_zero_csum6":         "id 1 remote 2001:db8::1 udp6zerocsumtx udp6zerocsumrx",
		"add_csum_cancel":        base + " udpcsum udp6zerocsumtx",
		"add_innerprotoinherit":  base + " innerprotoinherit",
		"set_changeable":         "id 1 ttl 5 tos 10 df set",
		"set_remote":             "id 1 remote 10.0.0.9",
		"set_local_same":         "id 1 local 10.0.0.2",
		"add_no_option":          "",
		"add_remote6_link_local": "id 1 remote fe80::1",
		"add_local6_link_local":  "id 1 remote 2001:db8::1 local fe80::2",
		"add_local_family_mix":   base + " local 2001:db8::2",
		"add_flowlabel_v4":       base + " label 5",
		"add_external_udpcsum":   "external udpcsum",
		"add_duplicate":          base,
		"add_after_external":     base,
		"set_id":                 "id 2",
		"set_dstport":            "id 1 dstport 6081",
		"set_family":             "id 1 remote 2001:db8::1",
		"set_local_new":          "id 1 local 10.0.0.2",
		"set_on_external":        "id 0",
	}
}

// geneveKeywords are geneve_parse_opt's keywords in its order; local and
// innerprotoinherit are compared with strcmp, the rest with matches.
var geneveKeywords = []struct {
	kw    string
	exact bool
}{
	{kw: "id"}, {kw: "vni"}, {kw: "remote"}, {kw: "local", exact: true}, {kw: "ttl"},
	{kw: "hoplimit"}, {kw: "tos"}, {kw: "dsfield"}, {kw: "df"}, {kw: "label"}, {kw: "flowlabel"},
	{kw: "dstport"}, {kw: "external"}, {kw: "noexternal"}, {kw: "udpcsum"}, {kw: "noudpcsum"},
	{kw: "udp6zerocsumtx"}, {kw: "noudp6zerocsumtx"}, {kw: "udp6zerocsumrx"},
	{kw: "noudp6zerocsumrx"}, {kw: "innerprotoinherit", exact: true},
}

var geneveCsumWords = map[string]uint16{
	"udpcsum":        unix.IFLA_GENEVE_UDP_CSUM,
	"udp6zerocsumtx": unix.IFLA_GENEVE_UDP_ZERO_CSUM6_TX,
	"udp6zerocsumrx": unix.IFLA_GENEVE_UDP_ZERO_CSUM6_RX,
}

// geneveArgs parses option words as geneve_parse_opt does, for well-formed
// words.
func geneveArgs(t testing.TB, words string) []GeneveSetting {
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
	var out []GeneveSetting
	for i := 0; i < len(argv); i++ {
		kw := ""
		for _, k := range geneveKeywords {
			if argv[i] == k.kw || !k.exact && strings.HasPrefix(k.kw, argv[i]) {
				kw = k.kw
				break
			}
		}
		var s GeneveSetting
		switch kw {
		case "id", "vni":
			s = GeneveSetting{Option: GeneveOptionID, Value: num(next(&i), 0)}
		case "remote":
			s = GeneveSetting{Option: GeneveOptionRemote, Addr: netip.MustParseAddr(next(&i))}
		case "local":
			s = GeneveSetting{Option: GeneveOptionLocal, Addr: netip.MustParseAddr(next(&i))}
		case "ttl", "hoplimit":
			switch v := next(&i); v {
			case "inherit":
				s = GeneveSetting{Option: GeneveOptionTTLInherit}
			case "auto":
				s = GeneveSetting{Option: GeneveOptionTTL}
			default:
				s = GeneveSetting{Option: GeneveOptionTTL, Value: num(v, 0)}
			}
		case "tos", "dsfield":
			// rtnl_dsfield_a2n reads hex.
			if s = (GeneveSetting{Option: GeneveOptionTOS, Value: 1}); next(&i) != "inherit" {
				s.Value = num(argv[i], 16)
			}
		case "df":
			s = GeneveSetting{Option: GeneveOptionDF, Value: map[string]uint64{"unset": 0, "set": 1, "inherit": 2}[next(&i)]}
		case "label", "flowlabel":
			s = GeneveSetting{Option: GeneveOptionLabel, Value: num(next(&i), 0)}
		case "dstport":
			s = GeneveSetting{Option: GeneveOptionDstPort, Value: num(next(&i), 0)}
		case "external", "noexternal":
			s = GeneveSetting{Option: GeneveOptionExternal, Value: vxlanOn(kw)}
		case "innerprotoinherit":
			s = GeneveSetting{Option: GeneveOptionInnerProtoInherit}
		case "":
			t.Fatalf("no geneve keyword for %q", argv[i])
		default:
			s = GeneveSetting{Option: GeneveOptionCsum, Attr: geneveCsumWords[strings.TrimPrefix(kw, "no")], Value: vxlanOn(kw)}
		}
		out = append(out, s)
	}
	return out
}

// geneveView renders g as geneve_print_opt prints it under -j.
func geneveView(g *GeneveInfo) map[string]any {
	out := map[string]any{}
	if g.CollectMetadata {
		out["external"] = true
	}
	if g.HasID {
		out["id"] = float64(g.ID)
	}
	switch r := g.Remote; {
	case r.Is4() && !r.IsUnspecified():
		out["remote"] = r.String()
	case r.Is6() && !r.IsUnspecified() && !r.IsMulticast():
		out["remote6"] = r.String()
	}
	switch l := g.Local; {
	case l.Is4():
		out["local"] = l.String()
	case l.Is6() && !l.IsUnspecified():
		out["local6"] = l.String()
	}
	if g.TTLInherit == 0 && g.HasTTL {
		out["ttl"] = float64(g.TTL)
	}
	if g.TOS != 0 {
		out["tos"] = bridgeHex(uint64(g.TOS))
	}
	if g.HasDF && g.DF <= geneveDfMaxCst {
		out["df"] = []string{"unset", "set", "inherit"}[g.DF]
	}
	if g.Label != 0 {
		out["label"] = bridgeHex(uint64(g.Label))
	}
	if g.HasPort {
		out["port"] = float64(g.Port)
	}
	if g.HasUDPCsum {
		out["udp_csum"] = g.UDPCsum != 0
	}
	if g.HasZeroCsum6Tx {
		out["udp_zero_csum6_tx"] = g.ZeroCsum6Tx != 0
	}
	if g.HasZeroCsum6Rx {
		out["udp_zero_csum6_rx"] = g.ZeroCsum6Rx != 0
	}
	if g.InnerProtoInherit {
		out["inner_proto_inherit"] = true
	}
	return out
}

func TestGeneveDumpMatchesReadback(t *testing.T) {
	slugs := geneveKind.accepted(t)
	for _, slug := range slugs {
		t.Run("positive: "+slug, func(t *testing.T) {
			got := geneveView(ParseGeneveInfoData(geneveKind.dump(t, slug, "netlink_route_getlink_detail_json.pcap")))
			want := geneveKind.readback(t, slug)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("dump renders\n %v\nreadback\n %v", got, want)
			}
		})
	}
	if len(slugs) != 22 {
		t.Errorf("%d accepted geneve rows, want 22", len(slugs))
	}
}

func TestParseGeneveInfoData(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	tests := []struct {
		description string
		nest        []byte
		nilInfo     bool
		// expected are keys of the -j view; with exact, the whole view.
		expected map[string]any
		exact    bool
		unknown  []uint16
	}{
		{
			description: "positive: a bare geneve's dump",
			nest:        geneveKind.dump(t, "add_default", pcap),
			expected:    map[string]any{"id": float64(1), "remote": "10.0.0.1", "port": float64(6081)},
		},
		{
			description: "positive: 7.2.0's local, IPv4",
			nest:        geneveKind.dump(t, "add_local", pcap),
			expected:    map[string]any{"local": "10.0.0.2"},
		},
		{
			description: "positive: 7.2.0's local, IPv6",
			nest:        geneveKind.dump(t, "add_local6", pcap),
			expected:    map[string]any{"local6": "2001:db8::2", "remote6": "2001:db8::1"},
		},
		{
			description: "positive: a metadata device",
			nest:        geneveKind.dump(t, "add_external", pcap),
			expected:    map[string]any{"external": true, "id": float64(0)},
		},
		{
			description: "positive: the label's top",
			nest:        geneveKind.dump(t, "add_flowlabel", pcap),
			expected:    map[string]any{"label": "0xfffff"},
		},
		{
			description: "negative: a nil nest",
			nest:        nil,
			nilInfo:     true,
		},
		{
			description: "negative: a short remote",
			nest:        rtattr(unix.IFLA_GENEVE_REMOTE, []byte{10, 0}),
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
			nest:        geneveKind.dump(t, "add_id_max", pcap),
			expected:    map[string]any{"id": float64(16777215)},
		},
		{
			description: "corner: the inner-proto flag has no payload",
			nest:        geneveKind.dump(t, "add_innerprotoinherit", pcap),
			expected:    map[string]any{"inner_proto_inherit": true},
		},
		{
			description: "corner: IPv4 and IPv6 remotes both present, REMOTE wins",
			nest: slices.Concat(rtattr(unix.IFLA_GENEVE_REMOTE6, netip.MustParseAddr("2001:db8::1").AsSlice()),
				rtattr(unix.IFLA_GENEVE_REMOTE, []byte{10, 0, 0, 1})),
			expected: map[string]any{"remote": "10.0.0.1"},
			exact:    true,
		},
		{
			description: "corner: an IPv4 local of 0.0.0.0 still prints",
			nest:        rtattr(IflaGeneveLocal, []byte{0, 0, 0, 0}),
			expected:    map[string]any{"local": "0.0.0.0"},
			exact:       true,
		},
		{
			description: "corner: a multicast IPv6 remote prints nothing",
			nest:        rtattr(unix.IFLA_GENEVE_REMOTE6, netip.MustParseAddr("ff05::1").AsSlice()),
			expected:    map[string]any{},
			exact:       true,
		},
		{
			description: "corner: an id past the last IFLA_GENEVE_* is listed",
			nest:        slices.Concat(rtattr(40, []byte{1}), rtattr(unix.IFLA_GENEVE_PORT, []byte{0x17, 0xc1})),
			expected:    map[string]any{"port": float64(6081)},
			exact:       true,
			unknown:     []uint16{40},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			g := ParseGeneveInfoData(tt.nest)
			if (g == nil) != tt.nilInfo {
				t.Fatalf("nil info: got %v, want %v", g == nil, tt.nilInfo)
			}
			if g == nil {
				return
			}
			got := geneveView(g)
			if tt.exact && !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got %v, want %v", got, tt.expected)
			}
			for _, k := range slices.Sorted(maps.Keys(tt.expected)) {
				if !reflect.DeepEqual(got[k], tt.expected[k]) {
					t.Errorf("%s: got %v, want %v", k, got[k], tt.expected[k])
				}
			}
			if !slices.Equal(g.Unknown, tt.unknown) {
				t.Errorf("unknown: got %v, want %v", g.Unknown, tt.unknown)
			}
		})
	}
}

func TestEncodeGeneveInfoData(t *testing.T) {
	type row struct {
		description string
		settings    []GeneveSetting
		set         bool
		expected    []byte
		err         error
	}
	u8 := func(atype uint16, v byte) []byte { return rtattr(atype, []byte{v}) }
	id1 := rtattr(unix.IFLA_GENEVE_ID, []byte{1, 0, 0, 0})
	remote := rtattr(unix.IFLA_GENEVE_REMOTE, []byte{10, 0, 0, 1})
	zeros := slices.Concat(rtattr(unix.IFLA_GENEVE_LABEL, []byte{0, 0, 0, 0}), u8(unix.IFLA_GENEVE_TTL, 0), u8(unix.IFLA_GENEVE_TOS, 0))
	args := func(words string) []GeneveSetting { return geneveArgs(t, words) }
	tests := []row{
		{
			description: "negative: a label past 20 bits",
			settings:    args("id 1 remote 10.0.0.1 flowlabel 0x100000"),
			err:         ErrKindSettingRange,
		},
		{
			description: "negative: a setting with no option",
			settings:    []GeneveSetting{{}},
			err:         ErrKindSettingOption,
		},
		{
			description: "negative: external with an id",
			settings:    args("external id 5"),
			err:         ErrKindSettingConflict,
		},
		{
			description: "negative: an add with no remote",
			settings:    args("id 1"),
			err:         ErrKindSettingConflict,
		},
		{
			description: "boundary: dstport 0 sends no PORT",
			settings:    args("id 1 remote 10.0.0.1 dstport 0"),
			expected:    slices.Concat(id1, remote, zeros),
		},
		{
			description: "corner: a local of the other family is sent",
			settings:    args("id 1 remote 2001:db8::1 local 10.0.0.2"),
			expected: slices.Concat(id1, rtattr(unix.IFLA_GENEVE_REMOTE6, netip.MustParseAddr("2001:db8::1").AsSlice()),
				rtattr(IflaGeneveLocal, []byte{10, 0, 0, 2}), zeros),
		},
		{
			description: "corner: a set sends only what it was given",
			settings:    args("id 1 ttl 5"),
			set:         true,
			expected:    slices.Concat(id1, u8(unix.IFLA_GENEVE_TTL, 5)),
		},
		{
			description: "corner: an add always sends the zeros",
			settings:    args("id 1 remote 10.0.0.1"),
			expected:    slices.Concat(id1, remote, zeros),
		},
		{
			description: "corner: a later checksum word wins",
			settings:    args("id 1 remote 10.0.0.1 udpcsum noudpcsum"),
			expected:    slices.Concat(id1, remote, zeros, u8(unix.IFLA_GENEVE_UDP_CSUM, 0)),
		},
		{
			description: "corner: TTL_INHERIT and DF go in the words' order, before ID",
			settings:    args("id 1 remote 10.0.0.1 ttl inherit df set"),
			expected:    slices.Concat(u8(unix.IFLA_GENEVE_TTL_INHERIT, 1), u8(unix.IFLA_GENEVE_DF, 1), id1, remote, zeros),
		},
	}
	words := geneveCorpusWords()
	ents, err := os.ReadDir(tdLinkWrite_7_3_0 + "/kinds/" + geneveKindCst)
	if err != nil {
		t.Fatal(err)
	}
	sent := 0
	for _, e := range ents {
		slug := e.Name()
		data, _, ok := geneveKind.request(t, slug)
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
			settings:    geneveArgs(t, w),
			set:         strings.HasPrefix(slug, "set_"),
			expected:    append([]byte{}, data...),
		})
	}
	if sent != 35 {
		t.Errorf("%d geneve rows sent a request, want 35", sent)
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := EncodeGeneveInfoData(tt.settings, tt.set)
			if err != tt.err {
				t.Fatalf("err: got %v, want %v", err, tt.err)
			}
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("got  % x\nwant % x", got, tt.expected)
			}
		})
	}
}

func TestLinkInfoGeneveKinds(t *testing.T) {
	tests := []struct {
		description string
		row         string
		expected    bool
	}{
		{description: "positive: a geneve decodes Geneve", row: "geneve/add_default", expected: true},
		{description: "positive: an external geneve decodes Geneve", row: "geneve/add_external", expected: true},
		{description: "negative: a vxlan does not", row: "vxlan/add_default"},
		{description: "corner: a bareudp, another UDP overlay, does not", row: "bareudp/add_default"},
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
			if got := li.Geneve != nil; got != tt.expected {
				t.Errorf("Geneve %v for kind %q, want %v", got, li.Kind, tt.expected)
			}
		})
	}
}

func BenchmarkParseGeneveInfoData(b *testing.B) {
	nest := geneveKind.dump(b, "add_local6", "netlink_route_getlink_detail_json.pcap")
	b.ReportAllocs()
	for b.Loop() {
		if ParseGeneveInfoData(nest) == nil {
			b.Fatal("nil")
		}
	}
}
