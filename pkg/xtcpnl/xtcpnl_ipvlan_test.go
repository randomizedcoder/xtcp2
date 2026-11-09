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

// The ipvlan codec's tests, against the ipvlan and ipvtap rows of the 7.3.0
// kind corpus (docs/netlink/goip-link-kinds.md, "KP2-T03 The ipvlan and
// ipvtap parser").

var (
	ipvlanKind = kindRows{kind: ipvlanKindCst, nest: unix.IFLA_INFO_DATA, key: "info_data"}
	ipvtapKind = kindRows{kind: ipvtapKindCst, nest: unix.IFLA_INFO_DATA, key: "info_data"}
)

// ipvlanCorpusWords are the option words after `type ipvlan` (or ipvtap)
// of every row that sent a request (kind-write-corpus.exp), keyed by kind
// and slug: ipvlan's 11 accepted and 3 refused, ipvtap's 3.
func ipvlanCorpusWords() map[string]map[string]string {
	m := map[string]string{
		"add_default":       "",
		"add_mode_and_flag": "mode l2 vepa",
		"add_abbrev":        "mo l2",
		"set_mode":          "mode l2",
		"set_flag":          "vepa",
		"add_no_link":       "",
		"add_on_loopback":   "",
		"add_on_busy_lower": "",
	}
	for _, mode := range IpvlanModes {
		m["add_mode_"+mode] = "mode " + mode
	}
	for _, flag := range []string{"private", "vepa", "bridge"} {
		m["add_"+flag] = flag
	}
	return map[string]map[string]string{
		ipvlanKindCst: m,
		ipvtapKindCst: {"add_default": "", "add_mode_l2": "mode l2", "set_mode": "mode l2"},
	}
}

// ipvlanArgs parses option words as ipvlan_parse_opt does, for well-formed
// words: keywords are prefixes, in its order.
func ipvlanArgs(t testing.TB, words string) []IpvlanSetting {
	t.Helper()
	argv := strings.Fields(words)
	flags := map[string]uint64{"private": uint64(IpvlanFlagPrivate), "vepa": uint64(IpvlanFlagVepa), "bridge": 0}
	var out []IpvlanSetting
	for i := 0; i < len(argv); i++ {
		kw := ""
		for _, k := range []string{"mode", "private", "vepa", "bridge"} {
			if strings.HasPrefix(k, argv[i]) {
				kw = k
				break
			}
		}
		switch kw {
		case "mode":
			i++
			out = append(out, IpvlanSetting{Option: IpvlanOptionMode, Value: uint64(slices.Index(IpvlanModes, argv[i]))})
		case "":
			t.Fatalf("no ipvlan keyword for %q", argv[i])
		default:
			out = append(out, IpvlanSetting{Option: IpvlanOptionFlag, Value: flags[kw]})
		}
	}
	return out
}

// ipvlanView renders v as ipvlan_print_opt prints it under -j: the mode by
// name, and exactly one of private, vepa or bridge, private first.
func ipvlanView(v *IpvlanInfo) map[string]any {
	out := map[string]any{}
	if v.HasMode {
		out["mode"] = "unknown"
		if int(v.Mode) < len(IpvlanModes) {
			out["mode"] = IpvlanModes[v.Mode]
		}
	}
	if v.HasFlags {
		switch {
		case v.Flags&IpvlanFlagPrivate != 0:
			out["private"] = true
		case v.Flags&IpvlanFlagVepa != 0:
			out["vepa"] = true
		default:
			out["bridge"] = true
		}
	}
	return out
}

func TestIpvlanDumpMatchesReadback(t *testing.T) {
	n := 0
	for _, k := range []kindRows{ipvlanKind, ipvtapKind} {
		for _, slug := range k.accepted(t) {
			n++
			t.Run("positive: "+k.kind+"/"+slug, func(t *testing.T) {
				got := ipvlanView(ParseIpvlanInfoData(k.dump(t, slug, "netlink_route_getlink_detail_json.pcap")))
				want := k.readback(t, slug)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("dump renders\n %v\nreadback\n %v", got, want)
				}
			})
		}
	}
	if n != 14 {
		t.Errorf("%d accepted ipvlan and ipvtap rows, want 14", n)
	}
}

func TestParseIpvlanInfoData(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	u16 := func(atype uint16, v byte) []byte { return rtattr(atype, []byte{v, 0}) }
	tests := []struct {
		description string
		nest        []byte
		nilInfo     bool
		// expected is the whole -j view.
		expected map[string]any
		unknown  []uint16
	}{
		{
			description: "positive: a bare ipvlan's dump",
			nest:        ipvlanKind.dump(t, "add_default", pcap),
			expected:    map[string]any{"mode": "l3", "bridge": true},
		},
		{
			description: "positive: mode and flag",
			nest:        ipvlanKind.dump(t, "add_mode_and_flag", pcap),
			expected:    map[string]any{"mode": "l2", "vepa": true},
		},
		{
			description: "positive: a change clears the flag",
			nest:        ipvlanKind.dump(t, "set_mode", pcap),
			expected:    map[string]any{"mode": "l2", "bridge": true},
		},
		{
			description: "positive: an ipvtap decodes the same",
			nest:        ipvtapKind.dump(t, "add_mode_l2", pcap),
			expected:    map[string]any{"mode": "l2", "bridge": true},
		},
		{
			description: "negative: a nil nest",
			nest:        nil,
			nilInfo:     true,
		},
		{
			description: "negative: a mode of 4 bytes",
			nest:        rtattr(unix.IFLA_IPVLAN_MODE, []byte{1, 0, 0, 0}),
			expected:    map[string]any{},
		},
		{
			description: "boundary: an empty nest",
			nest:        []byte{},
			expected:    map[string]any{},
		},
		{
			description: "boundary: the highest mode",
			nest:        u16(unix.IFLA_IPVLAN_MODE, 2),
			expected:    map[string]any{"mode": "l3s"},
		},
		{
			description: "corner: both flag bits render private alone",
			nest:        u16(unix.IFLA_IPVLAN_FLAGS, 3),
			expected:    map[string]any{"private": true},
		},
		{
			description: "corner: an unknown mode",
			nest:        u16(unix.IFLA_IPVLAN_MODE, 3),
			expected:    map[string]any{"mode": "unknown"},
		},
		{
			description: "corner: an id past IFLA_IPVLAN_FLAGS is listed",
			nest:        slices.Concat(u16(3, 1), u16(unix.IFLA_IPVLAN_FLAGS, 2)),
			expected:    map[string]any{"vepa": true},
			unknown:     []uint16{3},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			v := ParseIpvlanInfoData(tt.nest)
			if (v == nil) != tt.nilInfo {
				t.Fatalf("nil info: got %v, want %v", v == nil, tt.nilInfo)
			}
			if v == nil {
				return
			}
			if got := ipvlanView(v); !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got %v, want %v", got, tt.expected)
			}
			if !slices.Equal(v.Unknown, tt.unknown) {
				t.Errorf("unknown: got %v, want %v", v.Unknown, tt.unknown)
			}
		})
	}
}

func TestEncodeIpvlanInfoData(t *testing.T) {
	type row struct {
		description string
		settings    []IpvlanSetting
		expected    []byte
		err         error
	}
	u16 := func(atype uint16, v byte) []byte { return rtattr(atype, []byte{v, 0}) }
	tests := []row{
		{
			description: "positive: a mode alone sends flags 0",
			settings:    ipvlanArgs(t, "mode l2"),
			expected:    slices.Concat(u16(unix.IFLA_IPVLAN_MODE, 0), u16(unix.IFLA_IPVLAN_FLAGS, 0)),
		},
		{
			description: "negative: two flag words",
			settings:    ipvlanArgs(t, "private vepa"),
			err:         ErrIpvlanSecondFlag,
		},
		{
			description: "negative: a mode out of range",
			settings:    []IpvlanSetting{{Option: IpvlanOptionMode, Value: 3}},
			err:         ErrKindSettingRange,
		},
		{
			description: "negative: a setting with no option",
			settings:    []IpvlanSetting{{}},
			err:         ErrKindSettingOption,
		},
		{
			description: "boundary: no settings",
			settings:    nil,
			expected:    nil,
		},
		{
			description: "corner: bridge is flags 0",
			settings:    ipvlanArgs(t, "bridge"),
			expected:    u16(unix.IFLA_IPVLAN_FLAGS, 0),
		},
		{
			description: "corner: the flags follow a later mode",
			settings:    ipvlanArgs(t, "vepa mode l2"),
			expected:    slices.Concat(u16(unix.IFLA_IPVLAN_MODE, 0), u16(unix.IFLA_IPVLAN_FLAGS, 2)),
		},
		{
			description: "corner: each mode word is its own attribute",
			settings:    ipvlanArgs(t, "mode l2 mode l3s"),
			expected:    slices.Concat(u16(unix.IFLA_IPVLAN_MODE, 0), u16(unix.IFLA_IPVLAN_MODE, 2), u16(unix.IFLA_IPVLAN_FLAGS, 0)),
		},
	}
	words := ipvlanCorpusWords()
	sent := 0
	for _, k := range []kindRows{ipvlanKind, ipvtapKind} {
		ents, err := os.ReadDir(tdLinkWrite_7_3_0 + "/kinds/" + k.kind)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range ents {
			data, _, ok := k.request(t, e.Name())
			w, listed := words[k.kind][e.Name()]
			if ok != listed {
				t.Errorf("%s/%s: sent a request %v, listed %v", k.kind, e.Name(), ok, listed)
			}
			if !ok || !listed {
				continue
			}
			sent++
			tests = append(tests, row{
				description: fmt.Sprintf("positive: %s/%s's request", k.kind, e.Name()),
				settings:    ipvlanArgs(t, w),
				expected:    append([]byte{}, data...),
			})
		}
	}
	if sent != 17 {
		t.Errorf("%d ipvlan and ipvtap rows sent a request, want 17", sent)
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := EncodeIpvlanInfoData(tt.settings)
			if err != tt.err {
				t.Fatalf("err: got %v, want %v", err, tt.err)
			}
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("got  % x\nwant % x", got, tt.expected)
			}
		})
	}
}

func TestLinkInfoIpvlanKinds(t *testing.T) {
	tests := []struct {
		description string
		row         string
		expected    bool
	}{
		{description: "positive: an ipvlan decodes Ipvlan", row: "ipvlan/add_mode_l2", expected: true},
		{description: "positive: an ipvtap decodes Ipvlan", row: "ipvtap/add_mode_l2", expected: true},
		{description: "negative: a macvlan does not", row: "macvlan/add_default"},
		{description: "corner: a vlan, another lower-stacked kind, does not", row: "vlan/add_default"},
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
			if got := li.Ipvlan != nil; got != tt.expected {
				t.Errorf("Ipvlan %v for kind %q, want %v", got, li.Kind, tt.expected)
			}
		})
	}
}

func BenchmarkParseIpvlanInfoData(b *testing.B) {
	nest := ipvlanKind.dump(b, "add_mode_and_flag", "netlink_route_getlink_detail_json.pcap")
	b.ReportAllocs()
	for b.Loop() {
		if ParseIpvlanInfoData(nest) == nil {
			b.Fatal("nil")
		}
	}
}
