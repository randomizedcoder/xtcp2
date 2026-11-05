package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"maps"
	"net"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The hsr codec's tests, against the hsr rows of the 7.3.0 kind corpus
// (docs/netlink/goip-link-kinds.md, "KP5-T01 The hsr parser").

var hsrKind = greRow(hsrKindCst)

// hsrCorpusWords are the option words after `type hsr` of every row that
// sent a request (kind-write-corpus.exp), keyed by slug: 10 accepted and
// 10 refused by the kernel.
func hsrCorpusWords() map[string]string {
	const base = "slave1 d0 slave2 d1"
	return map[string]string{
		"add_default":           base,
		"add_supervision":       base + " supervision 42",
		"add_supervision_max":   base + " supervision 255",
		"add_supervision_hex":   base + " supervision 0xff",
		"add_supervision_twice": base + " supervision 1 supervision 2",
		"add_version_1":         base + " version 1",
		"add_proto_prp":         base + " proto 1",
		"add_interlink":         base + " interlink d2",
		"add_abbrev":            "sl d0 slave2 d1 s 7 v 1 p 0",
		"add_no_option":         "",
		"add_slave1_only":       "slave1 d0",
		"add_slave2_only":       "slave2 d1",
		"add_same_slaves":       "slave1 d0 slave2 d0",
		"add_version_2":         base + " version 2",
		"add_proto_2":           base + " proto 2",
		"add_prp_version":       base + " proto 1 version 0",
		"add_interlink_slave":   base + " interlink d0",
		"add_slave_lo":          "slave1 lo slave2 d1",
		"set_option":            "supervision 5",
		"set_no_option":         "",
	}
}

// hsrKeywords are hsr_parse_opt's matches keywords, in the order it tries
// them, and their options; interlink is strcmp.
var hsrKeywords = []struct {
	kw  string
	opt HsrOption
}{
	{"supervision", HsrOptionSupervision}, {"version", HsrOptionVersion}, {"proto", HsrOptionProto},
	{"slave1", HsrOptionSlave1}, {"slave2", HsrOptionSlave2},
}

// hsrArgs parses option words as hsr_parse_opt does, for well-formed words.
func hsrArgs(t testing.TB, words string, ifindex map[string]uint64) []HsrSetting {
	t.Helper()
	argv := strings.Fields(words)
	var out []HsrSetting
	for i := 0; i < len(argv); i += 2 {
		kw, val := argv[i], argv[i+1]
		opt := HsrOptionNone
		if kw == "interlink" {
			opt = HsrOptionInterlink
		}
		for _, k := range hsrKeywords {
			if opt == HsrOptionNone && greMatchesWord(kw, k.kw) {
				opt = k.opt
			}
		}
		switch opt {
		case HsrOptionSlave1, HsrOptionSlave2, HsrOptionInterlink:
			idx, ok := ifindex[val]
			if !ok {
				t.Fatalf("no ifindex for %q", val)
			}
			out = append(out, HsrSetting{Option: opt, Value: idx})
		case HsrOptionSupervision, HsrOptionVersion, HsrOptionProto:
			v, err := strconv.ParseUint(val, 0, 8)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, HsrSetting{Option: opt, Value: v})
		default:
			t.Fatalf("no hsr keyword for %q", kw)
		}
	}
	return out
}

// hsrView renders g as hsr_print_opt prints it under -j; names maps an
// ifindex to its device's name.
func hsrView(g *HsrInfo, names map[uint64]string) map[string]any {
	out := map[string]any{}
	if g.Short {
		return out
	}
	name := func(idx uint32) string {
		if n, ok := names[uint64(idx)]; ok {
			return n
		}
		return fmt.Sprintf("if%d", idx)
	}
	out["slave1"], out["slave2"] = nil, nil
	if g.HasSlave1 {
		out["slave1"] = name(g.Slave1)
	}
	if g.HasSlave2 {
		out["slave2"] = name(g.Slave2)
	}
	if g.HasInterlink {
		out["interlink"] = name(g.Interlink)
	}
	if g.HasSeqNr {
		out["seq_nr"] = float64(g.SeqNr)
	}
	if g.HasSupervisionAddr {
		out["supervision_addr"] = net.HardwareAddr(g.SupervisionAddr[:]).String()
	}
	if g.HasProtocol {
		out["proto"] = float64(g.Protocol)
	}
	if g.HasVersion {
		out["version"] = float64(g.Version)
	}
	return out
}

func TestHsrDumpMatchesReadback(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	slugs := hsrKind.accepted(t)
	for _, slug := range slugs {
		t.Run("positive: "+slug, func(t *testing.T) {
			got := hsrView(ParseHsrInfoData(hsrKind.dump(t, slug, pcap)), hsrKind.names(t, slug, pcap))
			want := hsrKind.readback(t, slug)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("dump renders\n %v\nreadback\n %v", got, want)
			}
		})
	}
	if len(slugs) != 10 {
		t.Errorf("%d accepted hsr rows, want 10", len(slugs))
	}
}

func TestParseHsrInfoData(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	dump := func(slug string) []byte { return hsrKind.dump(t, slug, pcap) }
	names := func(slug string) map[uint64]string { return hsrKind.names(t, slug, pcap) }
	tests := []struct {
		description string
		nest        []byte
		names       map[uint64]string
		nilInfo     bool
		// expected are keys of the -j view; with exact, the whole view.
		expected map[string]any
		exact    bool
	}{
		{
			description: "positive: an hsr's dump",
			nest:        dump("add_default"),
			names:       names("add_default"),
			expected: map[string]any{"slave1": "d0", "slave2": "d1", "seq_nr": float64(64511),
				"supervision_addr": "01:15:4e:00:01:00", "proto": float64(0), "version": float64(0)},
			exact: true,
		},
		{
			description: "positive: a PRP device",
			nest:        dump("add_proto_prp"),
			expected:    map[string]any{"proto": float64(1), "version": nil},
		},
		{
			description: "negative: a nil nest",
			nest:        nil,
			nilInfo:     true,
		},
		{
			description: "negative: a short SLAVE1 hides everything",
			nest:        slices.Concat(rtattr(unix.IFLA_HSR_SLAVE1, []byte{2, 0}), rtattr(unix.IFLA_HSR_PROTOCOL, []byte{0})),
			expected:    map[string]any{},
			exact:       true,
		},
		{
			description: "boundary: an empty nest",
			nest:        []byte{},
			expected:    map[string]any{"slave1": nil, "slave2": nil},
			exact:       true,
		},
		{
			description: "corner: the interlink",
			nest:        dump("add_interlink"),
			names:       names("add_interlink"),
			expected:    map[string]any{"interlink": "d2"},
		},
		{
			description: "corner: the supervision address's last byte",
			nest:        dump("add_supervision_max"),
			expected:    map[string]any{"supervision_addr": "01:15:4e:00:01:ff"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			g := ParseHsrInfoData(tt.nest)
			if (g == nil) != tt.nilInfo {
				t.Fatalf("nil info: got %v, want %v", g == nil, tt.nilInfo)
			}
			if g == nil {
				return
			}
			got := hsrView(g, tt.names)
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

func TestEncodeHsrInfoData(t *testing.T) {
	type row struct {
		description string
		settings    []HsrSetting
		expected    []byte
		err         error
	}
	u8 := func(atype uint16, v byte) []byte { return rtattr(atype, []byte{v}) }
	le := func(atype uint16, v uint32) []byte { return rtattr(atype, binary.LittleEndian.AppendUint32(nil, v)) }
	ifindex := map[string]uint64{"d0": 2, "d1": 3}
	args := func(words string) []HsrSetting { return hsrArgs(t, words, ifindex) }
	tests := []row{
		{
			description: "negative: a slave of ifindex 0",
			settings:    []HsrSetting{{Option: HsrOptionSlave1, Value: 0}},
			err:         ErrKindSettingRange,
		},
		{
			description: "negative: a supervision byte past 255",
			settings:    []HsrSetting{{Option: HsrOptionSupervision, Value: 256}},
			err:         ErrKindSettingRange,
		},
		{
			description: "negative: a setting with no option",
			settings:    []HsrSetting{{}},
			err:         ErrKindSettingOption,
		},
		{
			description: "boundary: no settings",
			settings:    nil,
			expected:    nil,
		},
		{
			description: "boundary: the supervision byte's top",
			settings:    args("supervision 255"),
			expected:    u8(unix.IFLA_HSR_MULTICAST_SPEC, 0xff),
		},
		{
			description: "corner: two supervisions send two attributes",
			settings:    args("supervision 1 supervision 2"),
			expected:    slices.Concat(u8(unix.IFLA_HSR_MULTICAST_SPEC, 1), u8(unix.IFLA_HSR_MULTICAST_SPEC, 2)),
		},
		{
			description: "corner: version 2 passes ip",
			settings:    args("version 2"),
			expected:    u8(unix.IFLA_HSR_VERSION, 2),
		},
		{
			description: "corner: the words' order is the attributes'",
			settings:    args("s 7 sl d0 p 0 v 1 slave2 d1"),
			expected: slices.Concat(u8(unix.IFLA_HSR_MULTICAST_SPEC, 7), le(unix.IFLA_HSR_SLAVE1, 2),
				u8(unix.IFLA_HSR_PROTOCOL, 0), u8(unix.IFLA_HSR_VERSION, 1), le(unix.IFLA_HSR_SLAVE2, 3)),
		},
	}
	words := hsrCorpusWords()
	ents, err := os.ReadDir(tdLinkWrite_7_3_0 + "/kinds/" + hsrKindCst)
	if err != nil {
		t.Fatal(err)
	}
	sent := 0
	for _, e := range ents {
		slug := e.Name()
		data, _, ok := hsrKind.request(t, slug)
		w, listed := words[slug]
		if ok != listed {
			t.Errorf("%s: sent a request %v, listed %v", slug, ok, listed)
		}
		if !ok || !listed {
			continue
		}
		sent++
		idx := greIfindex(t, hsrKind, slug)
		if _, ok := idx["lo"]; !ok {
			idx["lo"] = 1
		}
		tests = append(tests, row{
			description: fmt.Sprintf("positive: %s's request", slug),
			settings:    hsrArgs(t, w, idx),
			expected:    append([]byte{}, data...),
		})
	}
	if sent != 20 {
		t.Errorf("%d hsr rows sent a request, want 20", sent)
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := EncodeHsrInfoData(tt.settings)
			if err != tt.err {
				t.Fatalf("err: got %v, want %v", err, tt.err)
			}
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("got  % x\nwant % x", got, tt.expected)
			}
		})
	}
}

func TestLinkInfoHsrKinds(t *testing.T) {
	tests := []struct {
		description string
		row         string
		expected    bool
	}{
		{description: "positive: an hsr decodes Hsr", row: "hsr/add_default", expected: true},
		{description: "positive: a PRP hsr decodes Hsr", row: "hsr/add_proto_prp", expected: true},
		{description: "negative: an ifb does not", row: "ifb/add_no_option"},
		{description: "corner: an hsr with an interlink decodes Hsr", row: "hsr/add_interlink", expected: true},
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
			if got := li.Hsr != nil; got != tt.expected {
				t.Errorf("Hsr %v for kind %q, want %v", got, li.Kind, tt.expected)
			}
		})
	}
}

func BenchmarkParseHsrInfoData(b *testing.B) {
	nest := hsrKind.dump(b, "add_interlink", "netlink_route_getlink_detail_json.pcap")
	b.ReportAllocs()
	for b.Loop() {
		if ParseHsrInfoData(nest) == nil {
			b.Fatal("nil")
		}
	}
}
