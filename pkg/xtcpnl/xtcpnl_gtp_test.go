package xtcpnl

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The gtp codec's tests, against the gtp rows of the 7.3.0 kind corpus
// (docs/netlink/goip-link-kinds.md, "KP3-T04 The gtp parser").

var gtpKind = kindRows{kind: gtpKindCst, nest: unix.IFLA_INFO_DATA, key: "info_data"}

// gtpCorpusWords are the option words after `type gtp` of every row that
// sent a request (kind-write-corpus.exp), keyed by slug: 7 accepted and 4
// refused by the kernel.
func gtpCorpusWords() map[string]string {
	return map[string]string{
		"add_ggsn":            "role ggsn",
		"add_sgsn":            "role sgsn",
		"add_hsize":           "role ggsn hsize 4096",
		"add_hsize_max":       "role ggsn hsize 131071",
		"add_hsize_zero":      "role ggsn hsize 0",
		"add_restart_count":   "role ggsn restart_count 255",
		"add_unknown_skipped": "role ggsn foo",
		"add_no_option":       "",
		"add_second":          "role sgsn",
		"set_same":            "role ggsn",
		"set_no_option":       "",
	}
}

// gtpArgs parses option words as gtp_parse_opt does, for well-formed words:
// strcmp keywords, and an unknown word skipped.
func gtpArgs(t testing.TB, words string) []GtpSetting {
	t.Helper()
	argv := strings.Fields(words)
	num := func(s string, base int) uint64 {
		v, err := strconv.ParseUint(s, base, 64)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	var out []GtpSetting
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "role":
			i++
			out = append(out, GtpSetting{Option: GtpOptionRole, Value: map[string]uint64{"sgsn": 1}[argv[i]]})
		case "hsize":
			i++
			out = append(out, GtpSetting{Option: GtpOptionHashSize, Value: num(argv[i], 0)})
		case "restart_count":
			i++
			out = append(out, GtpSetting{Option: GtpOptionRestartCount, Value: num(argv[i], 10)})
		}
	}
	return out
}

// gtpView renders g as gtp_print_opt prints it under -j.
func gtpView(g *GtpInfo) map[string]any {
	out := map[string]any{}
	if g.HasRole {
		out["role"] = "unknown"
		if g.Role < uint32(len(GtpRoleNames)) {
			out["role"] = GtpRoleNames[g.Role]
		}
	}
	if g.HasPDPHashSize {
		out["hsize"] = float64(g.PDPHashSize)
	}
	if g.HasRestartCount {
		out["restart_count"] = float64(g.RestartCount)
	}
	return out
}

func TestGtpDumpMatchesReadback(t *testing.T) {
	slugs := gtpKind.accepted(t)
	for _, slug := range slugs {
		t.Run("positive: "+slug, func(t *testing.T) {
			got := gtpView(ParseGtpInfoData(gtpKind.dump(t, slug, "netlink_route_getlink_detail_json.pcap")))
			want := gtpKind.readback(t, slug)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("dump renders\n %v\nreadback\n %v", got, want)
			}
		})
	}
	if len(slugs) != 7 {
		t.Errorf("%d accepted gtp rows, want 7", len(slugs))
	}
}

func TestParseGtpInfoData(t *testing.T) {
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
			description: "positive: a ggsn's dump",
			nest:        gtpKind.dump(t, "add_ggsn", pcap),
			expected:    map[string]any{"role": "ggsn", "hsize": float64(1024), "restart_count": float64(0)},
			exact:       true,
		},
		{
			description: "positive: a restart count",
			nest:        gtpKind.dump(t, "add_restart_count", pcap),
			expected:    map[string]any{"restart_count": float64(255)},
		},
		{
			description: "positive: hsize 0 reads back the kernel's 1024",
			nest:        gtpKind.dump(t, "add_hsize_zero", pcap),
			expected:    map[string]any{"hsize": float64(1024)},
		},
		{
			description: "negative: a nil nest",
			nest:        nil,
			nilInfo:     true,
		},
		{
			description: "negative: a short role",
			nest:        rtattr(unix.IFLA_GTP_ROLE, []byte{1, 0}),
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
			description: "boundary: the hash size's top",
			nest:        gtpKind.dump(t, "add_hsize_max", pcap),
			expected:    map[string]any{"hsize": float64(131071)},
		},
		{
			description: "corner: a role past sgsn prints unknown",
			nest:        rtattr(unix.IFLA_GTP_ROLE, []byte{2, 0, 0, 0}),
			expected:    map[string]any{"role": "unknown"},
			exact:       true,
		},
		{
			description: "corner: the request-only attributes are skipped, not listed",
			nest: slices.Concat(rtattr(unix.IFLA_GTP_CREATE_SOCKETS, []byte{1}),
				rtattr(unix.IFLA_GTP_FD0, []byte{3, 0, 0, 0}), rtattr(10, nil)),
			expected: map[string]any{},
			exact:    true,
			unknown:  []uint16{10},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			g := ParseGtpInfoData(tt.nest)
			if (g == nil) != tt.nilInfo {
				t.Fatalf("nil info: got %v, want %v", g == nil, tt.nilInfo)
			}
			if g == nil {
				return
			}
			got := gtpView(g)
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

func TestEncodeGtpInfoData(t *testing.T) {
	type row struct {
		description string
		settings    []GtpSetting
		expected    []byte
		err         error
	}
	u32 := func(atype uint16, v uint32) []byte {
		return rtattr(atype, []byte{byte(v), byte(v >> 8), byte(v >> 16), 0})
	}
	create := rtattr(unix.IFLA_GTP_CREATE_SOCKETS, []byte{1})
	args := func(words string) []GtpSetting { return gtpArgs(t, words) }
	tests := []row{
		{
			description: "negative: a restart count too wide",
			settings:    []GtpSetting{{Option: GtpOptionRole}, {Option: GtpOptionRestartCount, Value: 256}},
			err:         ErrKindSettingRange,
		},
		{
			description: "negative: an hsize of 1<<17",
			settings:    args("role ggsn hsize 131072"),
			err:         ErrKindSettingRange,
		},
		{
			description: "negative: no role",
			settings:    args("hsize 10"),
			err:         ErrKindSettingMissing,
		},
		{
			description: "negative: role twice",
			settings:    args("role ggsn role sgsn"),
			err:         ErrKindSettingConflict,
		},
		{
			description: "negative: a setting with no option",
			settings:    []GtpSetting{{}},
			err:         ErrKindSettingOption,
		},
		{
			description: "boundary: no settings",
			settings:    nil,
			expected:    nil,
		},
		{
			description: "corner: the default hash size follows the words",
			settings:    args("restart_count 1 role ggsn"),
			expected: slices.Concat(create, rtattr(unix.IFLA_GTP_RESTART_COUNT, []byte{1}),
				u32(unix.IFLA_GTP_ROLE, 0), u32(unix.IFLA_GTP_PDP_HASHSIZE, 1024)),
		},
		{
			description: "corner: an explicit hash size is not repeated",
			settings:    args("role ggsn hsize 4096"),
			expected:    slices.Concat(create, u32(unix.IFLA_GTP_ROLE, 0), u32(unix.IFLA_GTP_PDP_HASHSIZE, 4096)),
		},
	}
	words := gtpCorpusWords()
	ents, err := os.ReadDir(tdLinkWrite_7_3_0 + "/kinds/" + gtpKindCst)
	if err != nil {
		t.Fatal(err)
	}
	sent := 0
	for _, e := range ents {
		slug := e.Name()
		data, _, ok := gtpKind.request(t, slug)
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
			settings:    gtpArgs(t, w),
			expected:    append([]byte{}, data...),
		})
	}
	if sent != 11 {
		t.Errorf("%d gtp rows sent a request, want 11", sent)
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := EncodeGtpInfoData(tt.settings)
			if err != tt.err {
				t.Fatalf("err: got %v, want %v", err, tt.err)
			}
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("got  % x\nwant % x", got, tt.expected)
			}
		})
	}
}

func TestLinkInfoGtpKinds(t *testing.T) {
	tests := []struct {
		description string
		row         string
		expected    bool
	}{
		{description: "positive: a ggsn decodes Gtp", row: "gtp/add_ggsn", expected: true},
		{description: "positive: an sgsn decodes Gtp", row: "gtp/add_sgsn", expected: true},
		{description: "negative: an amt does not", row: "amt/add_relay"},
		{description: "corner: a gtp made past an unknown word decodes Gtp", row: "gtp/add_unknown_skipped", expected: true},
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
			if got := li.Gtp != nil; got != tt.expected {
				t.Errorf("Gtp %v for kind %q, want %v", got, li.Kind, tt.expected)
			}
		})
	}
}

func BenchmarkParseGtpInfoData(b *testing.B) {
	nest := gtpKind.dump(b, "add_restart_count", "netlink_route_getlink_detail_json.pcap")
	b.ReportAllocs()
	for b.Loop() {
		if ParseGtpInfoData(nest) == nil {
			b.Fatal("nil")
		}
	}
}
