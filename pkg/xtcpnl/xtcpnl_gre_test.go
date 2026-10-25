package xtcpnl

import (
	"bytes"
	"encoding/binary"
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

// The gre family codec's tests, against the gre, gretap and erspan rows of
// the 7.3.0 kind corpus (docs/netlink/goip-link-kinds.md, "KP4-T01 The gre,
// gretap and erspan parser").

// greCorpusWords are the option words after `type <kind>` of every row that
// sent a request (kind-write-corpus.exp), keyed by kind and slug: 44
// accepted and 16 refused by the kernel.
func greCorpusWords() map[string]string {
	const r1 = "remote 10.0.0.1"
	const es = "remote 10.0.0.1 seq key 10"
	return map[string]string{
		"gre/add_default":              r1,
		"gre/add_no_option":            "",
		"gre/add_local":                "remote any local 10.0.0.2",
		"gre/add_dev":                  r1 + " dev d0",
		"gre/add_key":                  r1 + " key 42",
		"gre/add_key_split":            r1 + " ikey 1.2.3.4 okey 5",
		"gre/add_seq_csum":             r1 + " seq csum noocsum",
		"gre/add_ttl_max":              r1 + " hoplimit 255",
		"gre/add_tos_max":              r1 + " tclass ff",
		"gre/add_tos_inherit":          r1 + " tos inherit",
		"gre/add_nopmtudisc_ignore_df": r1 + " nopmtudisc ignore-df",
		"gre/add_fwmark":               r1 + " fwmark 0x10",
		"gre/add_encap_none":           r1 + " encap none",
		"gre/add_encap_fou": r1 + " encap fou encap-sport auto encap-dport 5555 encap-csum noencap-csum" +
			" encap-udp6-csum encap-remcsum",
		"gre/add_external":             "external",
		"gre/add_external_encap":       "external encap fou",
		"gre/add_multicast":            "remote 239.1.1.1 local 10.0.0.1",
		"gre/add_abbrev":               "r 10.0.0.1 l 10.0.0.2 t 64 k 5 s c",
		"gre/add_no_abbrev":            r1 + " key 5 no",
		"gre/add_second_keyed":         r1 + " key 5",
		"gre/set_changeable":           "ttl 32 tos 10 key 5 seq csum fwmark 7 local 10.0.0.2 remote 10.0.0.2",
		"gre/set_dev":                  "dev d0",
		"gre/set_no_option":            "",
		"gre/set_on_external":          "ttl 64",
		"gre/add_ignore_df":            r1 + " ignore-df",
		"gre/add_duplicate":            r1,
		"gre/add_second_external":      "external",
		"gre/set_remote_any":           "remote any",
		"gre/set_collide":              r1,
		"gre/set_external":             "external",
		"gre/set_pmtudisc_ignore_df":   "pmtudisc noignore-df",
		"gretap/add_default":           r1,
		"gretap/add_no_option":         "",
		"gretap/add_external":          "external",
		"gretap/add_multicast":         "remote 239.1.1.1 local 10.0.0.1",
		"gretap/add_address":           r1,
		"gretap/set_remote_any":        "remote any",
		"gretap/add_address_multicast": r1,
		"gretap/add_second_external":   "external",
		"gretap/set_external":          "external",
		"gretap/set_on_gre":            "ttl 5",
		"erspan/add_default":           es,
		"erspan/add_no_option":         "",
		"erspan/add_index":             es + " erspan 123",
		"erspan/add_index_max":         es + " erspan 1048575",
		"erspan/add_flags_split":       r1 + " iseq okey 10",
		"erspan/add_key_max":           r1 + " seq key 1023",
		"erspan/add_ver0":              r1 + " erspan_ver 0 csum",
		"erspan/add_ver2":              es + " erspan_ver 2 erspan_dir egress erspan_hwid 7",
		"erspan/add_hwid_max":          es + " erspan_ver 2 erspan_hwid 0x3f",
		"erspan/add_dir_abbrev":        es + " erspan_ver 2 erspan_dir e",
		"erspan/add_dir_ver1":          es + " erspan_dir egress erspan_hwid 7",
		"erspan/add_external":          "external",
		"erspan/set_index":             "erspan 200",
		"erspan/set_ver0_noseq":        "erspan_ver 0 noseq",
		"erspan/add_remote_only":       r1,
		"erspan/add_csum":              es + " csum",
		"erspan/add_key_over":          r1 + " seq key 1024",
		"erspan/add_hwid_over":         es + " erspan_ver 2 erspan_hwid 0x40",
		"erspan/set_noseq":             "noseq",
	}
}

// greMatches are gre_parse_opt's matches keywords in its order; every other
// keyword is compared with strcmp.
var greMatches = []string{
	"key", "nokey", "ikey", "noikey", "okey", "nookey", "seq", "noseq", "iseq", "noiseq", "oseq", "nooseq",
	"csum", "nocsum", "icsum", "noicsum", "ocsum", "noocsum", "nopmtudisc", "pmtudisc", "remote", "local",
	"dev", "ttl", "hoplimit", "hlim", "tos", "tclass", "dsfield",
}

// greFlagWords are the seq and csum words: the bit, whether they touch the
// input and output flags, and the value they set.
var greFlagWords = map[string]struct {
	bit    uint16
	in, on bool
	out    bool
}{
	"seq": {GreFlagSeq, true, true, true}, "noseq": {GreFlagSeq, true, false, true},
	"iseq": {GreFlagSeq, true, true, false}, "noiseq": {GreFlagSeq, true, false, false},
	"oseq": {GreFlagSeq, false, true, true}, "nooseq": {GreFlagSeq, false, false, true},
	"csum": {GreFlagCsum, true, true, true}, "nocsum": {GreFlagCsum, true, false, true},
	"icsum": {GreFlagCsum, true, true, false}, "noicsum": {GreFlagCsum, true, false, false},
	"ocsum": {GreFlagCsum, false, true, true}, "noocsum": {GreFlagCsum, false, false, true},
}

var greEncapFlagWords = map[string]uint16{
	"encap-csum": TunnelEncapFlagCsum, "encap-udp6-csum": TunnelEncapFlagCsum6, "encap-remcsum": TunnelEncapFlagRemcsum,
}

// greMatchesWord is iproute2's matches: word a nonempty prefix of kw.
func greMatchesWord(word, kw string) bool {
	return word != "" && strings.HasPrefix(kw, word)
}

// greKey parses a key as tnl_parse_key does: dotted through get_addr32,
// else a number.
func greKey(t testing.TB, s string) uint64 {
	t.Helper()
	if strings.Contains(s, ".") {
		a := netip.MustParseAddr(s)
		return uint64(binary.BigEndian.Uint32(a.AsSlice()))
	}
	v, err := strconv.ParseUint(s, 0, 32)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

// greArgs parses option words as link_gre.c's gre_parse_opt does, for
// well-formed words; ifindex maps a dev's name to its index.
func greArgs(t testing.TB, words string, ifindex map[string]uint64) []GreSetting {
	t.Helper()
	argv := strings.Fields(words)
	num := func(s string, base int) uint64 {
		v, err := strconv.ParseUint(s, base, 64)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	var out []GreSetting
	for i := 0; i < len(argv); i++ {
		kw := argv[i]
		for _, k := range greMatches {
			if strings.HasPrefix(k, argv[i]) {
				kw = k
				break
			}
		}
		next := func() string { i++; return argv[i] }
		addr := func() netip.Addr {
			if v := next(); v != "any" {
				return netip.MustParseAddr(v)
			}
			return netip.Addr{}
		}
		if f, ok := greFlagWords[kw]; ok {
			v := map[bool]uint64{true: 1}[f.on]
			if f.in {
				out = append(out, GreSetting{Option: GreOptionIFlag, Attr: f.bit, Value: v})
			}
			if f.out {
				out = append(out, GreSetting{Option: GreOptionOFlag, Attr: f.bit, Value: v})
			}
			continue
		}
		if bit, ok := greEncapFlagWords[strings.TrimPrefix(kw, "no")]; ok {
			out = append(out, GreSetting{Option: GreOptionEncapFlag, Attr: bit, Value: vxlanOn(kw)})
			continue
		}
		switch kw {
		case "key":
			k := greKey(t, next())
			out = append(out, GreSetting{Option: GreOptionIKey, Value: k}, GreSetting{Option: GreOptionOKey, Value: k})
		case "ikey":
			out = append(out, GreSetting{Option: GreOptionIKey, Value: greKey(t, next())})
		case "okey":
			out = append(out, GreSetting{Option: GreOptionOKey, Value: greKey(t, next())})
		case "nokey":
			out = append(out, GreSetting{Option: GreOptionNoIKey}, GreSetting{Option: GreOptionNoOKey})
		case "noikey":
			out = append(out, GreSetting{Option: GreOptionNoIKey})
		case "nookey":
			out = append(out, GreSetting{Option: GreOptionNoOKey})
		case "pmtudisc", "nopmtudisc":
			out = append(out, GreSetting{Option: GreOptionPMTUDisc, Value: vxlanOn(kw)})
		case "remote":
			out = append(out, GreSetting{Option: GreOptionRemote, Addr: addr()})
		case "local":
			out = append(out, GreSetting{Option: GreOptionLocal, Addr: addr()})
		case "dev":
			out = append(out, GreSetting{Option: GreOptionLink, Value: ifindex[next()]})
		case "ttl", "hoplimit", "hlim":
			s := GreSetting{Option: GreOptionTTL}
			if v := next(); v != "inherit" {
				s.Value = num(v, 0)
			}
			out = append(out, s)
		case "tos", "tclass", "dsfield":
			s := GreSetting{Option: GreOptionTOS, Value: 1}
			if v := next(); v != "inherit" {
				s.Value = num(v, 16)
			}
			out = append(out, s)
		case "noencap":
			out = append(out, GreSetting{Option: GreOptionEncapType})
		case "encap":
			out = append(out, GreSetting{Option: GreOptionEncapType,
				Value: map[string]uint64{"fou": TunnelEncapFOU, "gue": TunnelEncapGUE}[next()]})
		case "encap-sport":
			s := GreSetting{Option: GreOptionEncapSport}
			if v := next(); v != "auto" {
				s.Value = num(v, 0)
			}
			out = append(out, s)
		case "encap-dport":
			out = append(out, GreSetting{Option: GreOptionEncapDport, Value: num(next(), 0)})
		case "external":
			out = append(out, GreSetting{Option: GreOptionExternal})
		case "ignore-df", "noignore-df":
			out = append(out, GreSetting{Option: GreOptionIgnoreDF, Value: vxlanOn(kw)})
		case "fwmark":
			out = append(out, GreSetting{Option: GreOptionFwMark, Value: num(next(), 0)})
		case "erspan":
			out = append(out, GreSetting{Option: GreOptionErspanIndex, Value: num(next(), 0)})
		case "erspan_ver":
			out = append(out, GreSetting{Option: GreOptionErspanVer, Value: num(next(), 0)})
		case "erspan_dir":
			out = append(out, GreSetting{Option: GreOptionErspanDir, Value: map[bool]uint64{true: 1}[greMatchesWord(next(), "egress")]})
		case "erspan_hwid":
			out = append(out, GreSetting{Option: GreOptionErspanHwid, Value: num(next(), 0)})
		default:
			t.Fatalf("no gre keyword for %q", argv[i])
		}
	}
	return out
}

// greRow is the corpus harness for one gre kind.
func greRow(kind string) kindRows {
	return kindRows{kind: kind, nest: unix.IFLA_INFO_DATA, key: "info_data"}
}

// greIfindex maps the names a row's captures carry to their ifindexes.
func greIfindex(t testing.TB, k kindRows, slug string) map[string]uint64 {
	t.Helper()
	out := map[string]uint64{}
	for _, pcap := range []string{"ip.pcap", "netlink_route_getlink_detail_json.pcap"} {
		for idx, name := range k.names(t, slug, pcap) {
			out[name] = idx
		}
	}
	return out
}

// tunnelPrefetch is the INFO_DATA of the device ip fetched before a set: the
// answer to its RTM_GETLINK in the row's ip.pcap, or nil for none.
func tunnelPrefetch(t testing.TB, k kindRows, slug string) []byte {
	t.Helper()
	for _, m := range pcapNlMsgs(t, k.dir(slug)+"/ip.pcap") {
		if m.h.Type != unix.RTM_NEWLINK || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		if data, ok := k.data(t, m.body); ok {
			return data
		}
	}
	return nil
}

// tunnelEndpoint is tnl_print_endpoint: any for none or zero, unknown for
// the other family.
func tunnelEndpoint(a netip.Addr, v6 bool) string {
	switch {
	case !a.IsValid() || a.IsUnspecified():
		return "any"
	case a.Is6() != v6:
		return "unknown"
	}
	return a.String()
}

// tunnelEncapView is tnl_print_encap's object, or nil for type 0.
func tunnelEncapView(e TunnelEncap) any {
	if !e.HasType || e.Type == TunnelEncapNone {
		return nil
	}
	var typ any
	switch e.Type {
	case TunnelEncapFOU:
		typ = "fou"
	case TunnelEncapGUE:
		typ = "gue"
	}
	return map[string]any{
		"type": typ, "sport": float64(e.Sport), "dport": float64(e.Dport),
		"csum": e.Flags&TunnelEncapFlagCsum != 0, "csum6": e.Flags&TunnelEncapFlagCsum6 != 0,
		"remcsum": e.Flags&TunnelEncapFlagRemcsum != 0,
	}
}

// greKeyView renders a key as inet_ntop prints its four bytes.
func greKeyView(k uint32) string {
	return netip.AddrFrom4([4]byte(binary.BigEndian.AppendUint32(nil, k))).String()
}

// greView renders g as link_gre.c's gre_print_opt prints it under -j; names
// maps an ifindex to its device's name.
func greView(g *GreInfo, names map[uint64]string) map[string]any {
	out := map[string]any{
		"remote": tunnelEndpoint(g.Remote, false), "local": tunnelEndpoint(g.Local, false), "ttl": float64(g.TTL),
	}
	if g.CollectMetadata {
		out["external"] = true
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
	if g.HasPMTUDisc {
		out["pmtudisc"] = g.PMTUDisc != 0
	}
	if g.IgnoreDF != 0 {
		out["ignore_df"] = true
	}
	greFlagsView(out, g)
	if g.FwMark != 0 {
		out["fwmark"] = bridgeHex(uint64(g.FwMark))
	}
	if g.HasErspanIndex {
		out["erspan_index"] = float64(g.ErspanIndex)
	}
	if g.HasErspanVer {
		out["erspan_ver"] = float64(g.ErspanVer)
	}
	if g.HasErspanDir {
		out["erspan_dir"] = map[bool]string{true: "ingress", false: "egress"}[g.ErspanDir == 0]
	}
	if g.HasErspanHwid {
		out["erspan_hwid"] = bridgeHex(uint64(g.ErspanHwid))
	}
	if e := tunnelEncapView(g.Encap); e != nil {
		out["encap"] = e
	}
	return out
}

// greFlagsView adds the keys and the seq and csum flags.
func greFlagsView(out map[string]any, g *GreInfo) {
	if g.IFlags&GreFlagKey != 0 && g.HasIKey {
		out["ikey"] = greKeyView(g.IKey)
	}
	if g.OFlags&GreFlagKey != 0 && g.HasOKey {
		out["okey"] = greKeyView(g.OKey)
	}
	for key, on := range map[string]bool{
		"iseq": g.IFlags&GreFlagSeq != 0, "oseq": g.OFlags&GreFlagSeq != 0,
		"icsum": g.IFlags&GreFlagCsum != 0, "ocsum": g.OFlags&GreFlagCsum != 0,
	} {
		if on {
			out[key] = true
		}
	}
}

func TestGreDumpMatchesReadback(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	n := 0
	for _, kind := range []string{greKindCst, gretapKindCst, erspanKindCst} {
		k := greRow(kind)
		for _, slug := range k.accepted(t) {
			n++
			t.Run("positive: "+kind+"/"+slug, func(t *testing.T) {
				got := greView(ParseGreInfoData(k.dump(t, slug, pcap)), k.names(t, slug, pcap))
				want := k.readback(t, slug)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("dump renders\n %v\nreadback\n %v", got, want)
				}
			})
		}
	}
	if n != 44 {
		t.Errorf("%d accepted gre, gretap and erspan rows, want 44", n)
	}
}

func TestParseGreInfoData(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	gre, erspan := greRow(greKindCst), greRow(erspanKindCst)
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
			description: "positive: a gre's dump",
			nest:        gre.dump(t, "add_default", pcap),
			expected:    map[string]any{"remote": "10.0.0.1", "local": "any", "ttl": float64(0), "pmtudisc": true},
			exact:       true,
		},
		{
			description: "positive: split keys",
			nest:        gre.dump(t, "add_key_split", pcap),
			expected:    map[string]any{"ikey": "1.2.3.4", "okey": "0.0.0.5"},
		},
		{
			description: "positive: the fou encap",
			nest:        gre.dump(t, "add_encap_fou", pcap),
			expected: map[string]any{"encap": map[string]any{"type": "fou", "sport": float64(0), "dport": float64(5555),
				"csum": false, "csum6": true, "remcsum": true}},
		},
		{
			description: "positive: erspan version 2",
			nest:        erspan.dump(t, "add_ver2", pcap),
			expected:    map[string]any{"erspan_ver": float64(2), "erspan_dir": "egress", "erspan_hwid": "0x7"},
		},
		{
			description: "negative: a nil nest",
			nest:        nil,
			nilInfo:     true,
		},
		{
			description: "negative: a short remote",
			nest:        rtattr(IflaGreRemote, []byte{10, 0}),
			expected:    map[string]any{"remote": "any"},
		},
		{
			description: "boundary: an empty nest",
			nest:        []byte{},
			expected:    map[string]any{"remote": "any", "local": "any", "ttl": float64(0)},
			exact:       true,
		},
		{
			description: "boundary: the 20-bit index's top",
			nest:        erspan.dump(t, "add_index_max", pcap),
			expected:    map[string]any{"erspan_index": float64(1048575)},
		},
		{
			description: "corner: erspan's dumped OFLAGS carries KEY",
			nest:        erspan.dump(t, "add_no_option", pcap),
			expected:    map[string]any{"okey": "0.0.0.0"},
		},
		{
			description: "corner: a key without its flag doesn't print",
			nest:        slices.Concat(rtattr(IflaGreIKey, []byte{0, 0, 0, 5}), rtattr(IflaGreIFlags, []byte{0, 0})),
			expected:    map[string]any{"remote": "any", "local": "any", "ttl": float64(0)},
			exact:       true,
		},
		{
			description: "corner: hwid 0 prints without 0x",
			nest:        rtattr(IflaGreErspanHwid, []byte{0, 0}),
			expected:    map[string]any{"erspan_hwid": "0"},
		},
		{
			description: "corner: an encap type with no name",
			nest: slices.Concat(rtattr(IflaGreEncapType, []byte{3, 0}), rtattr(IflaGreEncapFlags, []byte{0, 0}),
				rtattr(IflaGreEncapSport, []byte{0, 0}), rtattr(IflaGreEncapDport, []byte{0, 0})),
			expected: map[string]any{"encap": map[string]any{"type": nil, "sport": float64(0), "dport": float64(0),
				"csum": false, "csum6": false, "remcsum": false}},
		},
		{
			description: "corner: an id past ERSPAN_HWID is listed",
			nest:        rtattr(25, []byte{1}),
			expected:    map[string]any{"remote": "any"},
			unknown:     []uint16{25},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			g := ParseGreInfoData(tt.nest)
			if (g == nil) != tt.nilInfo {
				t.Fatalf("nil info: got %v, want %v", g == nil, tt.nilInfo)
			}
			if g == nil {
				return
			}
			got := greView(g, nil)
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

func TestEncodeGreInfoData(t *testing.T) {
	type row struct {
		description string
		settings    []GreSetting
		cur         *GreInfo
		erspan      bool
		expected    []byte
		err         error
	}
	u8 := func(atype uint16, v byte) []byte { return rtattr(atype, []byte{v}) }
	keys := func(ik, ok []byte, ifl, ofl []byte) []byte {
		return slices.Concat(rtattr(IflaGreIKey, ik), rtattr(IflaGreOKey, ok), rtattr(IflaGreIFlags, ifl),
			rtattr(IflaGreOFlags, ofl))
	}
	zero4, zero2 := []byte{0, 0, 0, 0}, []byte{0, 0}
	tail := func(dport []byte) []byte {
		return slices.Concat(u8(IflaGreTOS, 0), u8(IflaGreTTL, 0), rtattr(IflaGreFwMark, zero4),
			rtattr(IflaGreEncapType, zero2), rtattr(IflaGreEncapFlags, zero2), rtattr(IflaGreEncapSport, zero2),
			rtattr(IflaGreEncapDport, dport))
	}
	remote := rtattr(IflaGreRemote, []byte{10, 0, 0, 1})
	args := func(words string) []GreSetting { return greArgs(t, words, nil) }
	tests := []row{
		{
			description: "negative: a multicast remote with no local",
			settings:    args("remote 239.1.1.1"),
			err:         ErrKindSettingMissing,
		},
		{
			description: "negative: an erspan index of 0",
			settings:    args("erspan 0"),
			erspan:      true,
			err:         ErrKindSettingRange,
		},
		{
			description: "negative: an erspan word on gre",
			settings:    args("erspan_ver 1"),
			err:         ErrKindSettingOption,
		},
		{
			description: "negative: a setting with no option",
			settings:    []GreSetting{{}},
			err:         ErrKindSettingOption,
		},
		{
			description: "boundary: no settings",
			settings:    nil,
			expected:    nil,
		},
		{
			description: "corner: external drops every other word",
			settings:    args("external encap fou ttl 5"),
			expected:    rtattr(IflaGreCollectMetadata, nil),
		},
		{
			description: "corner: the multicast keys",
			settings:    args("remote 239.1.1.1 local 10.0.0.1"),
			expected: slices.Concat(keys([]byte{239, 1, 1, 1}, []byte{239, 1, 1, 1}, []byte{0x20, 0}, []byte{0x20, 0}),
				rtattr(IflaGreLocal, []byte{10, 0, 0, 1}), rtattr(IflaGreRemote, []byte{239, 1, 1, 1}),
				u8(IflaGrePMTUDisc, 1), tail(zero2)),
		},
		{
			description: "corner: noignore-df sends 0",
			settings:    args("remote 10.0.0.1 noignore-df"),
			expected: slices.Concat(keys(zero4, zero4, zero2, zero2), remote, u8(IflaGrePMTUDisc, 1),
				u8(IflaGreIgnoreDF, 0), tail(zero2)),
		},
		{
			description: "corner: a set re-sends the prefetched port swapped",
			settings:    args("ttl 0"),
			cur:         &GreInfo{Remote: netip.MustParseAddr("10.0.0.1"), Encap: TunnelEncap{Dport: 5555, HasDport: true}},
			expected:    slices.Concat(keys(zero4, zero4, zero2, zero2), remote, u8(IflaGrePMTUDisc, 1), tail([]byte{0xb3, 0x15})),
		},
		{
			description: "corner: an index at version 2 is not sent",
			settings:    args("remote 10.0.0.1 erspan 5 erspan_ver 2"),
			erspan:      true,
			expected: slices.Concat(keys(zero4, zero4, zero2, zero2), remote, u8(IflaGrePMTUDisc, 1),
				u8(IflaGreTOS, 0), u8(IflaGreTTL, 0), rtattr(IflaGreFwMark, zero4), u8(IflaGreErspanVer, 2),
				u8(IflaGreErspanDir, 0), rtattr(IflaGreErspanHwid, zero2), rtattr(IflaGreEncapType, zero2),
				rtattr(IflaGreEncapFlags, zero2), rtattr(IflaGreEncapSport, zero2), rtattr(IflaGreEncapDport, zero2)),
		},
		{
			description: "corner: a set on an external device",
			settings:    args("ttl 64"),
			cur:         ParseGreInfoData(tunnelPrefetch(t, greRow(greKindCst), "set_on_external")),
			expected:    rtattr(IflaGreCollectMetadata, nil),
		},
	}
	words := greCorpusWords()
	sent := 0
	for _, kind := range []string{greKindCst, gretapKindCst, erspanKindCst} {
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
			var cur *GreInfo
			if strings.HasPrefix(slug, "set_") {
				cur = ParseGreInfoData(tunnelPrefetch(t, k, slug))
			}
			tests = append(tests, row{
				description: fmt.Sprintf("positive: %s/%s's request", kind, slug),
				settings:    greArgs(t, w, greIfindex(t, k, slug)),
				cur:         cur,
				erspan:      kind == erspanKindCst,
				expected:    append([]byte{}, data...),
			})
		}
	}
	if sent != 60 {
		t.Errorf("%d gre, gretap and erspan rows sent a request, want 60", sent)
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := EncodeGreInfoData(tt.settings, tt.cur, tt.erspan)
			if err != tt.err {
				t.Fatalf("err: got %v, want %v", err, tt.err)
			}
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("got  % x\nwant % x", got, tt.expected)
			}
		})
	}
}

func TestLinkInfoGreKinds(t *testing.T) {
	tests := []struct {
		description string
		row         string
		expected    bool
	}{
		{description: "positive: a gre decodes Gre", row: "gre/add_default", expected: true},
		{description: "positive: a gretap decodes Gre", row: "gretap/add_default", expected: true},
		{description: "negative: a vti does not", row: "vti/add_default"},
		{description: "corner: an erspan, with its own fill_info, decodes Gre", row: "erspan/add_default", expected: true},
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
			if got := li.Gre != nil; got != tt.expected {
				t.Errorf("Gre %v for kind %q, want %v", got, li.Kind, tt.expected)
			}
		})
	}
}

func BenchmarkParseGreInfoData(b *testing.B) {
	nest := greRow(erspanKindCst).dump(b, "add_ver2", "netlink_route_getlink_detail_json.pcap")
	b.ReportAllocs()
	for b.Loop() {
		if ParseGreInfoData(nest) == nil {
			b.Fatal("nil")
		}
	}
}
