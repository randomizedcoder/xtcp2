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

// The vti and vti6 codec's tests, against the vti and vti6 rows of the
// 7.3.0 kind corpus (docs/netlink/goip-link-kinds.md, "KP4-T05 The vti,
// vti6 and xfrm parsers").

var (
	vtiKind  = greRow(vtiKindCst)
	vti6Kind = greRow(vti6KindCst)
)

// vtiCorpusWords are the option words after `type vti` or `type vti6` of
// every row that sent a request (kind-write-corpus.exp), keyed by kind and
// slug: vti's 11 accepted and 5 refused by the kernel, vti6's 7 and 2.
func vtiCorpusWords() map[string]map[string]string {
	const r4, r6 = "remote 10.0.0.1", "remote 2001:db8::1"
	return map[string]map[string]string{
		vtiKindCst: {
			"add_default":             r4 + " local 10.0.0.2",
			"add_no_option":           "",
			"add_ikey_okey":           r4 + " ikey 1.2.3.4 okey 0x10",
			"add_key_max":             r4 + " key 4294967295",
			"add_key_last_wins":       r4 + " key 5 ikey 6",
			"add_dev":                 r4 + " dev d0",
			"add_fwmark":              r4 + " fwmark 0x10",
			"add_abbrev":              "r 10.0.0.1 l 10.0.0.2 i 1 o 2 d d0",
			"add_second_other_dev":    r4 + " dev d0",
			"set_fwmark":              "fwmark 5",
			"set_no_option":           "",
			"add_duplicate_other_key": r4 + " key 7",
			"add_duplicate_any":       "",
			"set_remote_on_any":       r4,
			"set_remote_any":          "remote any",
			"set_collide":             r4,
		},
		vti6KindCst: {
			"add_default":             r6 + " local 2001:db8::2",
			"add_no_option":           "",
			"add_options":             r6 + " key 5 dev d0 fwmark 0x10",
			"set_fwmark":              "fwmark 5",
			"set_no_option":           "",
			"set_remote_on_any":       r6,
			"set_remote_any":          "remote any",
			"add_duplicate_other_dev": r6 + " dev d0",
			"set_collide":             r6,
		},
	}
}

// vtiKeywords are vti_parse_opt's matches keywords, in the order it tries
// them.
var vtiKeywords = []string{"key", "ikey", "okey", "remote", "local", "dev"}

// vtiArgs parses option words as vti_parse_opt does, for well-formed words.
func vtiArgs(t testing.TB, words string, ifindex map[string]uint64) []VtiSetting {
	t.Helper()
	argv := strings.Fields(words)
	var out []VtiSetting
	for i := 0; i < len(argv); i += 2 {
		kw, val := argv[i], argv[i+1]
		if kw != "fwmark" {
			for _, k := range vtiKeywords {
				if greMatchesWord(kw, k) {
					kw = k
					break
				}
			}
		}
		switch kw {
		case "key":
			out = append(out, VtiSetting{Option: VtiOptionKey, Value: greKey(t, val)})
		case "ikey":
			out = append(out, VtiSetting{Option: VtiOptionIKey, Value: greKey(t, val)})
		case "okey":
			out = append(out, VtiSetting{Option: VtiOptionOKey, Value: greKey(t, val)})
		case "remote", "local":
			var a netip.Addr
			if val != "any" {
				a = netip.MustParseAddr(val)
			}
			opt := VtiOptionRemote
			if kw == "local" {
				opt = VtiOptionLocal
			}
			out = append(out, VtiSetting{Option: opt, Addr: a})
		case "dev":
			idx, ok := ifindex[val]
			if !ok {
				t.Fatalf("no ifindex for %q", val)
			}
			out = append(out, VtiSetting{Option: VtiOptionLink, Value: idx})
		case "fwmark":
			v, err := strconv.ParseUint(val, 0, 32)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, VtiSetting{Option: VtiOptionFwMark, Value: v})
		default:
			t.Fatalf("no vti keyword for %q", argv[i])
		}
	}
	return out
}

// vtiKeyView is a key as vti_print_opt prints it: dotted, or nothing for
// 0.
func vtiKeyView(out map[string]any, name string, key uint32) {
	if key == 0 {
		return
	}
	out[name] = netip.AddrFrom4([4]byte(binary.BigEndian.AppendUint32(nil, key))).String()
}

// vtiView renders g as vti_print_opt (or vti6_print_opt, for v6) prints it
// under -j; names maps an ifindex to its device's name.
func vtiView(g *VtiInfo, names map[uint64]string, v6 bool) map[string]any {
	out := map[string]any{"remote": tunnelEndpoint(g.Remote, v6), "local": tunnelEndpoint(g.Local, v6)}
	if g.Link != 0 {
		name, ok := names[uint64(g.Link)]
		if !ok {
			name = fmt.Sprintf("if%d", g.Link)
		}
		out["link"] = name
	}
	vtiKeyView(out, "ikey", g.IKey)
	vtiKeyView(out, "okey", g.OKey)
	if g.FwMark != 0 {
		out["fwmark"] = bridgeHex(uint64(g.FwMark))
	}
	return out
}

func TestVtiDumpMatchesReadback(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	n := 0
	for _, k := range []kindRows{vtiKind, vti6Kind} {
		for _, slug := range k.accepted(t) {
			n++
			t.Run("positive: "+k.kind+"/"+slug, func(t *testing.T) {
				got := vtiView(ParseVtiInfoData(k.dump(t, slug, pcap)), k.names(t, slug, pcap), k.kind == vti6KindCst)
				want := k.readback(t, slug)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("dump renders\n %v\nreadback\n %v", got, want)
				}
			})
		}
	}
	if n != 18 {
		t.Errorf("%d accepted vti and vti6 rows, want 18", n)
	}
}

func TestParseVtiInfoData(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	tests := []struct {
		description string
		nest        []byte
		v6          bool
		nilInfo     bool
		// expected are keys of the -j view; with exact, the whole view.
		expected map[string]any
		exact    bool
		// lens are the endpoints' recorded lengths, local then remote, -1
		// for not sent; nil to skip.
		lens []int
	}{
		{
			description: "positive: a vti's dump",
			nest:        vtiKind.dump(t, "add_default", pcap),
			expected:    map[string]any{"remote": "10.0.0.1", "local": "10.0.0.2"},
			exact:       true,
		},
		{
			description: "positive: split keys",
			nest:        vtiKind.dump(t, "add_ikey_okey", pcap),
			expected:    map[string]any{"local": "any", "ikey": "1.2.3.4", "okey": "0.0.0.16"},
		},
		{
			description: "negative: a nil nest",
			nest:        nil,
			nilInfo:     true,
		},
		{
			description: "negative: a 16-byte remote on vti",
			nest:        rtattr(IflaVtiRemote, netip.MustParseAddr("2001:db8::1").AsSlice()),
			expected:    map[string]any{"remote": "unknown"},
			lens:        []int{-1, 16},
		},
		{
			description: "boundary: the highest key",
			nest:        vtiKind.dump(t, "add_key_max", pcap),
			expected:    map[string]any{"ikey": "255.255.255.255", "okey": "255.255.255.255"},
		},
		{
			description: "corner: a zero key doesn't print",
			nest:        rtattr(IflaVtiIKey, []byte{0, 0, 0, 0}),
			expected:    map[string]any{"remote": "any", "local": "any"},
			exact:       true,
			lens:        []int{-1, -1},
		},
		{
			description: "corner: the endpoints' lengths, the last one kept",
			nest: slices.Concat(rtattr(IflaVtiLocal, []byte{10, 0, 0, 1}), rtattr(IflaVtiRemote, []byte{1}),
				rtattr(IflaVtiLocal, make([]byte, 16))),
			lens: []int{16, 1},
		},
		{
			description: "corner: vti6's dump order differs",
			nest:        vti6Kind.dump(t, "add_options", pcap),
			v6:          true,
			expected: map[string]any{"remote": "2001:db8::1", "local": "any", "link": "if3", "ikey": "0.0.0.5",
				"okey": "0.0.0.5", "fwmark": "0x10"},
			exact: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			g := ParseVtiInfoData(tt.nest)
			if (g == nil) != tt.nilInfo {
				t.Fatalf("nil info: got %v, want %v", g == nil, tt.nilInfo)
			}
			if g == nil {
				return
			}
			got := vtiView(g, nil, tt.v6)
			if tt.exact && !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got %v, want %v", got, tt.expected)
			}
			for _, k := range slices.Sorted(maps.Keys(tt.expected)) {
				if !reflect.DeepEqual(got[k], tt.expected[k]) {
					t.Errorf("%s: got %v, want %v", k, got[k], tt.expected[k])
				}
			}
			sent := func(has bool, n int) int {
				if !has {
					return -1
				}
				return n
			}
			if got := []int{sent(g.HasLocal, g.LocalLen), sent(g.HasRemote, g.RemoteLen)}; tt.lens != nil && !slices.Equal(got, tt.lens) {
				t.Errorf("lengths: got %v, want %v", got, tt.lens)
			}
		})
	}
}

func TestEncodeVtiInfoData(t *testing.T) {
	type row struct {
		description string
		settings    []VtiSetting
		cur         *VtiInfo
		v6          bool
		expected    []byte
		err         error
	}
	be := func(atype uint16, v uint32) []byte { return rtattr(atype, binary.BigEndian.AppendUint32(nil, v)) }
	le := func(atype uint16, v uint32) []byte { return rtattr(atype, binary.LittleEndian.AppendUint32(nil, v)) }
	remote := rtattr(IflaVtiRemote, netip.MustParseAddr("10.0.0.1").AsSlice())
	tests := []row{
		{
			description: "negative: a vti with an IPv6 remote",
			settings:    []VtiSetting{{Option: VtiOptionRemote, Addr: netip.MustParseAddr("2001:db8::1")}},
			err:         ErrKindSettingRange,
		},
		{
			description: "negative: a key past 32 bits",
			settings:    []VtiSetting{{Option: VtiOptionKey, Value: 1 << 32}},
			err:         ErrKindSettingRange,
		},
		{
			description: "negative: a setting with no option",
			settings:    []VtiSetting{{}},
			err:         ErrKindSettingOption,
		},
		{
			description: "boundary: no settings",
			settings:    nil,
			expected:    nil,
		},
		{
			description: "boundary: the last vti key wins",
			settings:    vtiArgs(t, "remote 10.0.0.1 key 5 ikey 6", nil),
			expected:    slices.Concat(be(IflaVtiIKey, 6), be(IflaVtiOKey, 5), remote, le(IflaVtiFwMark, 0)),
		},
		{
			description: "corner: the prefetched link keeps its low byte",
			settings:    vtiArgs(t, "fwmark 1", nil),
			cur:         &VtiInfo{Remote: netip.MustParseAddr("10.0.0.1"), Link: 258, HasLink: true},
			expected: slices.Concat(be(IflaVtiIKey, 0), be(IflaVtiOKey, 0), remote, le(IflaVtiFwMark, 1),
				le(IflaVtiLink, 2)),
		},
		{
			description: "corner: remote any drops a prefetched remote",
			settings:    vtiArgs(t, "remote any", nil),
			cur:         &VtiInfo{Remote: netip.MustParseAddr("10.0.0.1"), FwMark: 7, HasFwMark: true},
			expected:    slices.Concat(be(IflaVtiIKey, 0), be(IflaVtiOKey, 0), le(IflaVtiFwMark, 7)),
		},
	}
	sent := 0
	for kind, words := range vtiCorpusWords() {
		k := greRow(kind)
		ents, err := os.ReadDir(tdLinkWrite_7_3_0 + "/kinds/" + kind)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range ents {
			slug := e.Name()
			data, _, ok := k.request(t, slug)
			w, listed := words[slug]
			if ok != listed {
				t.Errorf("%s/%s: sent a request %v, listed %v", kind, slug, ok, listed)
			}
			if !ok || !listed {
				continue
			}
			sent++
			var cur *VtiInfo
			if strings.HasPrefix(slug, "set_") {
				cur = ParseVtiInfoData(tunnelPrefetch(t, k, slug))
			}
			tests = append(tests, row{
				description: fmt.Sprintf("positive: %s/%s's request", kind, slug),
				settings:    vtiArgs(t, w, greIfindex(t, k, slug)),
				cur:         cur,
				v6:          kind == vti6KindCst,
				expected:    append([]byte{}, data...),
			})
		}
	}
	if sent != 25 {
		t.Errorf("%d vti and vti6 rows sent a request, want 25", sent)
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := EncodeVtiInfoData(tt.settings, tt.cur, tt.v6)
			if err != tt.err {
				t.Fatalf("err: got %v, want %v", err, tt.err)
			}
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("got  % x\nwant % x", got, tt.expected)
			}
		})
	}
}

func TestLinkInfoTunnelKinds(t *testing.T) {
	tests := []struct {
		description string
		row         string
		vti, xfrm   bool
	}{
		{description: "positive: a vti decodes Vti", row: "vti/add_default", vti: true},
		{description: "positive: an xfrm decodes Xfrm", row: "xfrm/add_default", xfrm: true},
		{description: "negative: a gre decodes neither", row: "gre/add_default"},
		{description: "corner: a vti6 decodes Vti", row: "vti6/add_default", vti: true},
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
			if got := li.Vti != nil; got != tt.vti {
				t.Errorf("Vti %v for kind %q, want %v", got, li.Kind, tt.vti)
			}
			if got := li.Xfrm != nil; got != tt.xfrm {
				t.Errorf("Xfrm %v for kind %q, want %v", got, li.Kind, tt.xfrm)
			}
		})
	}
}

func BenchmarkParseVtiInfoData(b *testing.B) {
	nest := vtiKind.dump(b, "add_ikey_okey", "netlink_route_getlink_detail_json.pcap")
	b.ReportAllocs()
	for b.Loop() {
		if ParseVtiInfoData(nest) == nil {
			b.Fatal("nil")
		}
	}
}
