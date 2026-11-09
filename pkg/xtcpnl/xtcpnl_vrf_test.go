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

// The vrf encoder's and the vrf and vrf_slave decoders' tests, against the
// vrf and vrf_slave rows of the 7.3.0 kind corpus
// (docs/netlink/goip-link-kinds.md, "KP5-T03 The vrf, vrf_slave and vxcan
// parsers").

var (
	vrfKind      = greRow(vrfKindCst)
	vrfSlaveKind = kindRows{kind: "vrf_slave", nest: unix.IFLA_INFO_SLAVE_DATA, key: "info_slave_data"}
)

// vrfCorpusWords are the option words after `type vrf` of every row that
// sent a request (kind-write-corpus.exp), keyed by slug: 8 accepted and 4
// refused by the kernel.
func vrfCorpusWords() map[string]string {
	return map[string]string{
		"add_table":         "table 10",
		"add_table_max":     "table 4294967295",
		"add_table_hex":     "table 0x10",
		"add_table_main":    "table main",
		"add_table_twice":   "table 10 table 20",
		"add_abbrev":        "t 10",
		"add_shared_table":  "table 10",
		"add_no_option":     "",
		"add_table_zero":    "table 0",
		"add_mcast_address": "table 10",
		"set_table":         "table 20",
		"set_no_option":     "",
	}
}

// vrfTableNames are rtnl_rttable_a2n's built-in names.
var vrfTableNames = map[string]uint64{"default": unix.RT_TABLE_DEFAULT, "main": unix.RT_TABLE_MAIN,
	"local": unix.RT_TABLE_LOCAL}

// vrfArgs parses option words as vrf_parse_opt does, for well-formed words.
func vrfArgs(t testing.TB, words string) []VrfSetting {
	t.Helper()
	argv := strings.Fields(words)
	var out []VrfSetting
	const table = "table"
	for i := 0; i < len(argv); i++ {
		// matches: a prefix of table.
		if kw := argv[i]; len(kw) > len(table) || table[:len(kw)] != kw {
			t.Fatalf("no vrf keyword for %q", kw)
		}
		i++
		v, ok := vrfTableNames[argv[i]]
		if !ok {
			var err error
			if v, err = strconv.ParseUint(argv[i], 0, 32); err != nil {
				t.Fatal(err)
			}
		}
		out = append(out, VrfSetting{Option: VrfOptionTable, Value: v})
	}
	return out
}

// vrfView renders a VRF nest's table as vrf_print_opt and
// vrf_slave_print_opt print it under -j.
func vrfView(a U32Attr) map[string]any {
	out := map[string]any{}
	if a.Present {
		out["table"] = float64(a.Value)
	}
	return out
}

func TestVrfDumpMatchesReadback(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	type row struct {
		k    kindRows
		slug string
		attr uint16
	}
	tests := make([]row, 0, 11)
	for _, slug := range vrfKind.accepted(t) {
		tests = append(tests, row{vrfKind, slug, IflaVrfTable})
	}
	// set_nomaster, the fourth accepted vrf_slave row, has no slave kind.
	for _, slug := range []string{"set_master", "set_vrf", "set_type"} {
		tests = append(tests, row{vrfSlaveKind, slug, IflaVrfPortTable})
	}
	for _, tt := range tests {
		t.Run("positive: "+tt.k.kind+"/"+tt.slug, func(t *testing.T) {
			got := vrfView(vrfTableAttr(tt.k.dump(t, tt.slug, pcap), tt.attr))
			want := tt.k.readback(t, tt.slug)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("dump renders %v, readback %v", got, want)
			}
		})
	}
	if len(tests) != 11 {
		t.Errorf("%d vrf and vrf_slave rows, want 11", len(tests))
	}
}

func TestEncodeVrfInfoData(t *testing.T) {
	type row struct {
		description string
		settings    []VrfSetting
		expected    []byte
		err         error
	}
	table := func(v uint32) []byte { return rtattr(IflaVrfTable, binary.LittleEndian.AppendUint32(nil, v)) }
	tests := []row{
		{
			description: "negative: a table past 32 bits",
			settings:    []VrfSetting{{Option: VrfOptionTable, Value: 1 << 32}},
			err:         ErrKindSettingRange,
		},
		{
			description: "negative: a setting with no option",
			settings:    []VrfSetting{{Value: 10}},
			err:         ErrKindSettingOption,
		},
		{
			description: "boundary: no settings",
			settings:    nil,
			expected:    nil,
		},
		{
			description: "boundary: the highest table",
			settings:    vrfArgs(t, "table 4294967295"),
			expected:    table(0xffffffff),
		},
		{
			description: "corner: two tables send two attributes, in order",
			settings:    vrfArgs(t, "table 20 table 10"),
			expected:    slices.Concat(table(20), table(10)),
		},
		{
			description: "corner: table 0 is sent",
			settings:    vrfArgs(t, "table 0"),
			expected:    table(0),
		},
	}
	words := vrfCorpusWords()
	ents, err := os.ReadDir(tdLinkWrite_7_3_0 + "/kinds/" + vrfKindCst)
	if err != nil {
		t.Fatal(err)
	}
	sent := 0
	for _, e := range ents {
		slug := e.Name()
		data, has, ok := vrfKind.request(t, slug)
		w, listed := words[slug]
		if ok != listed {
			t.Errorf("%s: sent a request %v, listed %v", slug, ok, listed)
		}
		if !ok || !listed {
			continue
		}
		sent++
		var want []byte
		if has {
			want = append([]byte(nil), data...)
		}
		tests = append(tests, row{
			description: fmt.Sprintf("positive: %s's request", slug),
			settings:    vrfArgs(t, w),
			expected:    want,
		})
	}
	if sent != 12 {
		t.Errorf("%d vrf rows sent a request, want 12", sent)
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := EncodeVrfInfoData(tt.settings)
			if err != tt.err {
				t.Fatalf("err: got %v, want %v", err, tt.err)
			}
			if (got == nil) != (tt.expected == nil) || !bytes.Equal(got, tt.expected) {
				t.Errorf("got  % x\nwant % x", got, tt.expected)
			}
		})
	}
}

// TestKindSlaveDataAbsent is vrf_slave's half: ip has no vrf_slave parser,
// so no request carries INFO_SLAVE_DATA, and the kernel's enslaved device
// reads back VrfPortTable.
func TestKindSlaveDataAbsent(t *testing.T) {
	ents, err := os.ReadDir(tdLinkWrite_7_3_0 + "/kinds/vrf_slave")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range ents {
		t.Run("negative: "+e.Name()+"'s request has no INFO_SLAVE_DATA", func(t *testing.T) {
			if _, has, _ := vrfSlaveKind.request(t, e.Name()); has {
				t.Error("INFO_SLAVE_DATA sent")
			}
		})
	}
	tests := []struct {
		description string
		slug        string
		expected    U32Attr
	}{
		{description: "positive: set_master's readback decodes VrfPortTable", slug: "set_master",
			expected: U32Attr{Value: 10, Present: true}},
		{description: "positive: set_type's readback decodes VrfPortTable", slug: "set_type",
			expected: U32Attr{Value: 10, Present: true}},
		{description: "corner: set_nomaster's has no slave kind", slug: "set_nomaster"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			var body []byte
			for _, m := range pcapNlMsgs(t, vrfSlaveKind.dir(tt.slug)+"/netlink_route_getlink_detail.pcap") {
				if m.h.Type == unix.RTM_NEWLINK && m.h.Flags&unix.NLM_F_REQUEST == 0 && len(m.body) > len(body) {
					body = m.body
				}
			}
			li, err := ParseNewLink(body)
			if err != nil {
				t.Fatal(err)
			}
			if li.VrfPortTable != tt.expected {
				t.Errorf("VrfPortTable %+v (slave kind %q), want %+v", li.VrfPortTable, li.SlaveKind, tt.expected)
			}
			if !tt.expected.Present && li.SlaveKind != "" {
				t.Errorf("slave kind %q, want none", li.SlaveKind)
			}
		})
	}
}

func BenchmarkParseVrfInfoData(b *testing.B) {
	nest := vrfKind.dump(b, "add_table", "netlink_route_getlink_detail_json.pcap")
	b.ReportAllocs()
	for b.Loop() {
		if !vrfTableAttr(nest, IflaVrfTable).Present {
			b.Fatal("absent")
		}
	}
}
