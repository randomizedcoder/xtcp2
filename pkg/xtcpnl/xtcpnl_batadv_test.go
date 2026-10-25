package xtcpnl

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The batadv encoder's tests and the checks that the kinds with no parser
// carry no kind data, against the 7.3.0 kind corpus
// (docs/netlink/goip-link-kinds.md, "KP5-T04 The batadv parser and the
// kinds with no parser").

var batadvKind = greRow("batadv")

// batadvCorpusWords are the option words after `type batadv` of every row
// that sent a request (kind-write-corpus.exp), keyed by slug: 6 accepted
// and 6 refused by the kernel.
func batadvCorpusWords() map[string]string {
	return map[string]string{
		"add_no_option":    "",
		"add_ra_v":         "ra BATMAN_V",
		"add_ra_iv":        "ra BATMAN_IV",
		"add_abbrev":       "r BATMAN_V",
		"add_ra_twice":     "ra BATMAN_IV ra BATMAN_V",
		"add_duplicate":    "",
		"add_ra_bogus":     "ra BOGUS",
		"add_ra_lowercase": "ra batman_v",
		"add_mtu_low":      "",
		"set_ra":           "ra BATMAN_V",
		"set_ra_bogus":     "ra BOGUS",
		"set_no_option":    "",
	}
}

// batadvArgs parses option words as batadv_parse_opt does, for well-formed
// words.
func batadvArgs(t testing.TB, words string) []BatadvSetting {
	t.Helper()
	const ra = "ra"
	argv := strings.Fields(words)
	var out []BatadvSetting
	for i := 0; i < len(argv); i++ {
		// matches: a prefix of ra.
		if kw := argv[i]; len(kw) > len(ra) || ra[:len(kw)] != kw {
			t.Fatalf("no batadv keyword for %q", kw)
		}
		i++
		out = append(out, BatadvSetting{Option: BatadvOptionRoutingAlgo, Name: argv[i]})
	}
	return out
}

func TestEncodeBatadvInfoData(t *testing.T) {
	type row struct {
		description string
		settings    []BatadvSetting
		expected    []byte
		err         error
	}
	algo := func(name string) []byte { return rtattr(IflaBatadvAlgoName, append([]byte(name), 0)) }
	tests := []row{
		{
			description: "negative: a name with a NUL",
			settings:    []BatadvSetting{{Option: BatadvOptionRoutingAlgo, Name: "BATMAN\x00V"}},
			err:         ErrKindSettingRange,
		},
		{
			description: "negative: a setting with no option",
			settings:    []BatadvSetting{{Name: "BATMAN_V"}},
			err:         ErrKindSettingOption,
		},
		{
			description: "boundary: no settings",
			settings:    nil,
			expected:    nil,
		},
		{
			description: "boundary: an empty name",
			settings:    []BatadvSetting{{Option: BatadvOptionRoutingAlgo}},
			expected:    algo(""),
		},
		{
			description: "corner: two names send two attributes, in order",
			settings:    batadvArgs(t, "ra BATMAN_V ra BATMAN_IV"),
			expected:    slices.Concat(algo("BATMAN_V"), algo("BATMAN_IV")),
		},
		{
			description: "corner: the case is kept",
			settings:    batadvArgs(t, "ra batman_v"),
			expected:    algo("batman_v"),
		},
	}
	words := batadvCorpusWords()
	ents, err := os.ReadDir(tdLinkWrite_7_3_0 + "/kinds/batadv")
	if err != nil {
		t.Fatal(err)
	}
	sent := 0
	for _, e := range ents {
		slug := e.Name()
		data, has, ok := batadvKind.request(t, slug)
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
			settings:    batadvArgs(t, w),
			expected:    want,
		})
	}
	if sent != 12 {
		t.Errorf("%d batadv rows sent a request, want 12", sent)
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := EncodeBatadvInfoData(tt.settings)
			if err != tt.err {
				t.Fatalf("err: got %v, want %v", err, tt.err)
			}
			if (got == nil) != (tt.expected == nil) || !bytes.Equal(got, tt.expected) {
				t.Errorf("got  % x\nwant % x", got, tt.expected)
			}
		})
	}
}

// parserlessKinds are the kinds ip has no parser for.
var parserlessKinds = []string{"ifb", "nlmon", "vcan", "team", "team_slave"}

// kindDump is the largest RTM_NEWLINK answer in row's -d readback: the
// device's own, not a name lookup's.
func kindDump(t testing.TB, row string) []byte {
	t.Helper()
	var body []byte
	for _, m := range pcapNlMsgs(t, tdLinkWrite_7_3_0+"/kinds/"+row+"/netlink_route_getlink_detail.pcap") {
		if m.h.Type == unix.RTM_NEWLINK && m.h.Flags&unix.NLM_F_REQUEST == 0 && len(m.body) > len(body) {
			body = m.body
		}
	}
	if body == nil {
		t.Fatalf("%s: no RTM_NEWLINK answer", row)
	}
	return body
}

// decodedKindFields names li's non-nil kind fields and present VRF tables.
func decodedKindFields(li *LinkInfo) []string {
	var out []string
	v := reflect.ValueOf(li).Elem()
	for i := range v.NumField() {
		if f := v.Field(i); f.Kind() == reflect.Pointer && !f.IsNil() {
			out = append(out, v.Type().Field(i).Name)
		}
	}
	if li.VrfTable.Present {
		out = append(out, "VrfTable")
	}
	if li.VrfPortTable.Present {
		out = append(out, "VrfPortTable")
	}
	return out
}

func TestKindsWithoutData(t *testing.T) {
	for _, kind := range parserlessKinds {
		ents, err := os.ReadDir(tdLinkWrite_7_3_0 + "/kinds/" + kind)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range ents {
			t.Run("negative: "+kind+"/"+e.Name()+"'s request carries no kind data", func(t *testing.T) {
				for _, nest := range []uint16{unix.IFLA_INFO_DATA, unix.IFLA_INFO_SLAVE_DATA} {
					k := kindRows{kind: kind, nest: nest}
					if _, has, _ := k.request(t, e.Name()); has {
						t.Errorf("nest %d sent", nest)
					}
				}
			})
		}
	}
	tests := []struct {
		description string
		row         string
		kind        string
		slaveKind   string
	}{
		{description: "positive: an ifb's dump decodes no kind data", row: "ifb/add_no_option", kind: "ifb"},
		{description: "positive: a team port's dump has a slave kind and no data", row: "team/set_enslave",
			kind: "dummy", slaveKind: "team"},
		{description: "positive: a vcan's dump decodes no kind data", row: "vcan/add_mtu_16", kind: "vcan"},
		{description: "corner: batadv's dump has no INFO_DATA", row: "batadv/add_ra_v", kind: "batadv"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			li, err := ParseNewLink(kindDump(t, tt.row))
			if err != nil {
				t.Fatal(err)
			}
			if li.Kind != tt.kind || li.SlaveKind != tt.slaveKind {
				t.Errorf("kind %q slave %q, want %q %q", li.Kind, li.SlaveKind, tt.kind, tt.slaveKind)
			}
			if li.HasInfoData || li.HasInfoSlaveData {
				t.Errorf("INFO_DATA %v, INFO_SLAVE_DATA %v, want neither", li.HasInfoData, li.HasInfoSlaveData)
			}
			if got := decodedKindFields(&li); len(got) != 0 {
				t.Errorf("decoded %v, want nothing", got)
			}
		})
	}
	t.Run("boundary: a bare set of another kind sends INFO_KIND alone", func(t *testing.T) {
		var got []uint16
		var kind string
		for _, m := range pcapNlMsgs(t, tdLinkWrite_7_3_0+"/kinds/ifb/set_kind_mismatch/ip.pcap") {
			if m.h.Type != unix.RTM_NEWLINK || m.h.Flags&unix.NLM_F_REQUEST == 0 {
				continue
			}
			if err := WalkRTAttrs(m.body[IfInfomsgSizeCst:], func(atype uint16, v []byte) {
				if atype != unix.IFLA_LINKINFO {
					return
				}
				walkNestTolerant(v, func(a uint16, in []byte) {
					got = append(got, a)
					if a == unix.IFLA_INFO_KIND {
						kind = string(bytes.TrimRight(in, "\x00"))
					}
				})
			}); err != nil {
				t.Fatal(err)
			}
		}
		if !slices.Equal(got, []uint16{unix.IFLA_INFO_KIND}) || kind != "vcan" {
			t.Errorf("LINKINFO holds %v, kind %q; want INFO_KIND vcan alone", got, kind)
		}
	})
}

func BenchmarkEncodeBatadvInfoData(b *testing.B) {
	settings := []BatadvSetting{{Option: BatadvOptionRoutingAlgo, Name: "BATMAN_V"}}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := EncodeBatadvInfoData(settings); err != nil {
			b.Fatal(err)
		}
	}
}
