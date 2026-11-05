package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The xfrm interface codec's tests, against the xfrm rows of the 7.3.0
// kind corpus (docs/netlink/goip-link-kinds.md, "KP4-T05 The vti, vti6 and
// xfrm parsers").

var xfrmLinkKind = greRow(xfrmKindCst)

// xfrmLinkCorpusWords are the option words after `type xfrm` of every row
// that sent a request (kind-write-corpus.exp), keyed by slug: 9 accepted
// and 8 refused by the kernel.
func xfrmLinkCorpusWords() map[string]string {
	return map[string]string{
		"add_default":             "if_id 1",
		"add_dev":                 "dev d0 if_id 1",
		"add_if_id_max":           "if_id 0xffffffff",
		"add_external":            "external",
		"add_abbrev":              "d d0 i 5",
		"add_if_id_twice":         "if_id 1 if_id 2",
		"set_if_id":               "if_id 2",
		"set_if_id_dev":           "dev d0 if_id 2",
		"set_no_option":           "",
		"add_no_option":           "",
		"add_duplicate_other_dev": "dev d0 if_id 1",
		"add_second_external":     "external",
		"set_drop_dev":            "if_id 2",
		"set_add_dev":             "dev d0 if_id 1",
		"set_collide":             "if_id 1",
		"set_external":            "external",
		"set_on_external":         "if_id 5",
	}
}

// xfrmLinkArgs parses option words as xfrm_parse_opt does, for well-formed
// words.
func xfrmLinkArgs(t testing.TB, words string, ifindex map[string]uint64) []XfrmLinkSetting {
	t.Helper()
	argv := strings.Fields(words)
	var out []XfrmLinkSetting
	for i := 0; i < len(argv); i++ {
		kw := argv[i]
		switch {
		case kw == "external":
			out = append(out, XfrmLinkSetting{Option: XfrmLinkOptionExternal})
		case greMatchesWord(kw, "dev"):
			i++
			idx, ok := ifindex[argv[i]]
			if !ok {
				t.Fatalf("no ifindex for %q", argv[i])
			}
			out = append(out, XfrmLinkSetting{Option: XfrmLinkOptionLink, Value: idx})
		case greMatchesWord(kw, "if_id"):
			i++
			v, err := strconv.ParseUint(argv[i], 0, 32)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, XfrmLinkSetting{Option: XfrmLinkOptionIfID, Value: v})
		default:
			t.Fatalf("no xfrm keyword for %q", kw)
		}
	}
	return out
}

// xfrmLinkView renders g as xfrm_print_opt prints it under -j: external
// alone, or the if_id; never the link.
func xfrmLinkView(g *XfrmLinkInfo) map[string]any {
	out := map[string]any{}
	switch {
	case g.External:
		out["external"] = true
	case g.HasIfID:
		out["if_id"] = bridgeHex(uint64(g.IfID))
	}
	return out
}

func TestXfrmLinkDumpMatchesReadback(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	slugs := xfrmLinkKind.accepted(t)
	for _, slug := range slugs {
		t.Run("positive: "+slug, func(t *testing.T) {
			got := xfrmLinkView(ParseXfrmLinkInfoData(xfrmLinkKind.dump(t, slug, pcap)))
			want := xfrmLinkKind.readback(t, slug)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("dump renders\n %v\nreadback\n %v", got, want)
			}
		})
	}
	if len(slugs) != 9 {
		t.Errorf("%d accepted xfrm rows, want 9", len(slugs))
	}
}

func TestParseXfrmLinkInfoData(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	dump := func(slug string) []byte { return xfrmLinkKind.dump(t, slug, pcap) }
	tests := []struct {
		description string
		nest        []byte
		nilInfo     bool
		expected    map[string]any
	}{
		{
			description: "positive: an xfrm's dump",
			nest:        dump("add_default"),
			expected:    map[string]any{"if_id": "0x1"},
		},
		{
			description: "positive: the lower is decoded but not printed",
			nest:        dump("add_dev"),
			expected:    map[string]any{"if_id": "0x1"},
		},
		{
			description: "negative: a nil nest",
			nest:        nil,
			nilInfo:     true,
		},
		{
			description: "negative: a short IF_ID",
			nest:        rtattr(unix.IFLA_XFRM_IF_ID, []byte{1, 0}),
			expected:    map[string]any{},
		},
		{
			description: "boundary: the highest if_id",
			nest:        dump("add_if_id_max"),
			expected:    map[string]any{"if_id": "0xffffffff"},
		},
		{
			description: "corner: an external xfrm prints only external",
			nest:        dump("add_external"),
			expected:    map[string]any{"external": true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			g := ParseXfrmLinkInfoData(tt.nest)
			if (g == nil) != tt.nilInfo {
				t.Fatalf("nil info: got %v, want %v", g == nil, tt.nilInfo)
			}
			if g == nil {
				return
			}
			if got := xfrmLinkView(g); !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestEncodeXfrmLinkInfoData(t *testing.T) {
	type row struct {
		description string
		settings    []XfrmLinkSetting
		expected    []byte
		err         error
	}
	le := func(atype uint16, v uint32) []byte { return rtattr(atype, binary.LittleEndian.AppendUint32(nil, v)) }
	ifindex := map[string]uint64{"d0": 2}
	args := func(words string) []XfrmLinkSetting { return xfrmLinkArgs(t, words, ifindex) }
	tests := []row{
		{
			description: "negative: external with an if_id",
			settings:    args("if_id 5 external"),
			err:         ErrKindSettingConflict,
		},
		{
			description: "negative: external with a dev",
			settings:    args("external dev d0"),
			err:         ErrKindSettingConflict,
		},
		{
			description: "negative: no if_id",
			settings:    args("dev d0"),
			err:         ErrKindSettingMissing,
		},
		{
			description: "negative: an if_id of 0",
			settings:    args("if_id 0"),
			err:         ErrKindSettingRange,
		},
		{
			description: "negative: a setting with no option",
			settings:    []XfrmLinkSetting{{}},
			err:         ErrKindSettingOption,
		},
		{
			description: "boundary: no settings",
			settings:    nil,
			expected:    nil,
		},
		{
			description: "corner: two if_ids send two IF_IDs",
			settings:    args("if_id 1 if_id 2"),
			expected:    slices.Concat(le(unix.IFLA_XFRM_IF_ID, 1), le(unix.IFLA_XFRM_IF_ID, 2)),
		},
		{
			description: "corner: the link follows the if_id",
			settings:    args("dev d0 if_id 3"),
			expected:    slices.Concat(le(unix.IFLA_XFRM_IF_ID, 3), le(unix.IFLA_XFRM_LINK, 2)),
		},
	}
	words := xfrmLinkCorpusWords()
	ents, err := os.ReadDir(tdLinkWrite_7_3_0 + "/kinds/" + xfrmKindCst)
	if err != nil {
		t.Fatal(err)
	}
	sent := 0
	for _, e := range ents {
		slug := e.Name()
		data, _, ok := xfrmLinkKind.request(t, slug)
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
			settings:    xfrmLinkArgs(t, w, greIfindex(t, xfrmLinkKind, slug)),
			expected:    append([]byte{}, data...),
		})
	}
	if sent != 17 {
		t.Errorf("%d xfrm rows sent a request, want 17", sent)
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := EncodeXfrmLinkInfoData(tt.settings)
			if err != tt.err {
				t.Fatalf("err: got %v, want %v", err, tt.err)
			}
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("got  % x\nwant % x", got, tt.expected)
			}
		})
	}
}

func BenchmarkParseXfrmLinkInfoData(b *testing.B) {
	nest := xfrmLinkKind.dump(b, "add_dev", "netlink_route_getlink_detail_json.pcap")
	b.ReportAllocs()
	for b.Loop() {
		if ParseXfrmLinkInfoData(nest) == nil {
			b.Fatal("nil")
		}
	}
}
