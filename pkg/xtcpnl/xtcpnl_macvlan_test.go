package xtcpnl

import (
	"bytes"
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

// The macvlan codec's tests, against the macvlan and macvtap rows of the
// 7.3.0 kind corpus (docs/netlink/goip-link-kinds.md, "KP2-T02 The macvlan
// and macvtap parser").

var (
	macvlanKind = kindRows{kind: macvlanKindCst, nest: unix.IFLA_INFO_DATA, key: "info_data"}
	macvtapKind = kindRows{kind: macvtapKindCst, nest: unix.IFLA_INFO_DATA, key: "info_data"}
)

const (
	macvlanMAC1 = "02:00:00:00:00:01"
	macvlanMAC2 = "02:00:00:00:00:02"
)

// macvlanCorpusWords are the option words after `type macvlan` (or
// macvtap) of every row that sent a request (kind-write-corpus.exp), keyed
// by kind and slug: macvlan's 20 accepted and 5 refused, macvtap's 5.
func macvlanCorpusWords() map[string]map[string]string {
	m := map[string]string{
		"add_default":            "",
		"add_flag_nodst":         "mode bridge flag nodst",
		"add_nodst":              "mode bridge nodst",
		"add_flag_null":          "mode bridge flag null",
		"add_passthru_nopromisc": "mode passthru nopromisc",
		"add_bcqueuelen":         "bcqueuelen 2000",
		"add_bclim":              "bclim 10",
		"add_bclim_negative":     "bclim -1",
		"add_macaddr_add":        "mode source macaddr add " + macvlanMAC1,
		"add_macaddr_set":        "mode source macaddr set " + macvlanMAC1 + " " + macvlanMAC2,
		"add_abbrev":             "mo bridge",
		"set_mode":               "mode private",
		"set_macaddr_del":        "macaddr del " + macvlanMAC1,
		"set_macaddr_flush":      "macaddr flush",
		"set_bcqueuelen":         "bcqueuelen 500",
		"set_passthru":           "mode passthru",
		"add_macaddr_not_source": "mode bridge macaddr add " + macvlanMAC1,
		"add_macaddr_multicast":  "mode source macaddr add 01:00:5e:00:00:01",
		"add_second_passthru":    "mode passthru",
		"add_on_loopback":        "",
	}
	for _, mode := range MacvlanModes {
		m["add_mode_"+mode] = "mode " + mode
	}
	return map[string]map[string]string{
		macvlanKindCst: m,
		macvtapKindCst: {
			"add_default":       "",
			"add_mode_bridge":   "mode bridge",
			"add_mode_passthru": "mode passthru",
			"add_bcqueuelen":    "bcqueuelen 2000",
			"set_mode":          "mode private",
		},
	}
}

// macvlanArgs parses option words as macvlan_parse_opt does, for
// well-formed words: keywords are prefixes in its order, bclim is strcmp,
// and `macaddr set` takes every address that follows.
func macvlanArgs(t testing.TB, words string) []MacvlanSetting {
	t.Helper()
	argv := strings.Fields(words)
	var out []MacvlanSetting
	mac := func(s string) []byte {
		a, err := net.ParseMAC(s)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	for i := 0; i < len(argv); i++ {
		kw := argv[i]
		for _, k := range []string{"mode", "flag", "macaddr", "nopromisc", "nodst", "bcqueuelen"} {
			if strings.HasPrefix(k, argv[i]) {
				kw = k
				break
			}
		}
		switch kw {
		case "mode":
			i++
			v := slices.IndexFunc(macvlanModeValues(), func(m uint32) bool { return MacvlanModes[m] == argv[i] })
			out = append(out, MacvlanSetting{Option: MacvlanOptionMode, Value: uint64(macvlanModeValues()[v])})
		case "flag":
			i++
			out = append(out, MacvlanSetting{Option: MacvlanOptionFlag, Value: map[string]uint64{"nopromisc": 1, "nodst": 2}[argv[i]]})
		case "nopromisc":
			out = append(out, MacvlanSetting{Option: MacvlanOptionFlag, Value: uint64(MacvlanFlagNopromisc)})
		case "nodst":
			out = append(out, MacvlanSetting{Option: MacvlanOptionFlag, Value: uint64(MacvlanFlagNodst)})
		case "macaddr":
			i++
			s := MacvlanSetting{Option: MacvlanOptionMacaddr, Value: map[string]uint64{
				"add": MacvlanMacaddrAdd, "del": MacvlanMacaddrDel, "flush": MacvlanMacaddrFlush, "set": MacvlanMacaddrSet,
			}[argv[i]]}
			switch argv[i] {
			case "add", "del":
				i++
				s.Addrs = [][]byte{mac(argv[i])}
			case "set":
				for ; i+1 < len(argv) && strings.Count(argv[i+1], ":") == 5; i++ {
					s.Addrs = append(s.Addrs, mac(argv[i+1]))
				}
			}
			out = append(out, s)
		case "bcqueuelen":
			i++
			n, err := strconv.ParseUint(argv[i], 0, 32)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, MacvlanSetting{Option: MacvlanOptionBcQueueLen, Value: n})
		case "bclim":
			i++
			n, err := strconv.ParseInt(argv[i], 0, 32)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, MacvlanSetting{Option: MacvlanOptionBcCutoff, Cutoff: int32(n)})
		default:
			t.Fatalf("no macvlan keyword for %q", argv[i])
		}
	}
	return out
}

// macvlanModeValues are MacvlanModes' keys in value order.
func macvlanModeValues() []uint32 {
	return slices.Sorted(maps.Keys(MacvlanModes))
}

// macvlanView renders m as macvlan_print_opt prints it under -j: nothing
// without a mode, the flags as true, the queue lengths and cutoff, and in
// source mode only the count and the list.
func macvlanView(m *MacvlanInfo) map[string]any {
	out := map[string]any{}
	if !m.HasMode {
		return out
	}
	name, ok := MacvlanModes[m.Mode]
	if !ok {
		name = "unknown"
	}
	out["mode"] = name
	if m.Flags&MacvlanFlagNopromisc != 0 {
		out["nopromisc"] = true
	}
	if m.Flags&MacvlanFlagNodst != 0 {
		out["nodst"] = true
	}
	if m.HasBcQueueLen {
		out["bcqueuelen"] = float64(m.BcQueueLen)
	}
	if m.HasBcQueueLenUsed {
		out["usedbcqueuelen"] = float64(m.BcQueueLenUsed)
	}
	if m.HasBcCutoff {
		out["bclim"] = float64(m.BcCutoff)
	}
	if m.Mode != MacvlanModeSource || !m.HasMacaddrCount {
		return out
	}
	out["macaddr_count"] = float64(m.MacaddrCount)
	if !m.HasMacaddrData {
		return out
	}
	list := []any{}
	for _, a := range m.Macaddrs {
		list = append(list, net.HardwareAddr(a[:]).String())
	}
	out["macaddr_data"] = list
	return out
}

func TestMacvlanDumpMatchesReadback(t *testing.T) {
	n := 0
	for _, k := range []kindRows{macvlanKind, macvtapKind} {
		for _, slug := range k.accepted(t) {
			n++
			t.Run("positive: "+k.kind+"/"+slug, func(t *testing.T) {
				got := macvlanView(ParseMacvlanInfoData(k.dump(t, slug, "netlink_route_getlink_detail_json.pcap")))
				want := k.readback(t, slug)
				if !reflect.DeepEqual(got, want) {
					t.Errorf("dump renders\n %v\nreadback\n %v", got, want)
				}
			})
		}
	}
	if n != 25 {
		t.Errorf("%d accepted macvlan and macvtap rows, want 25", n)
	}
}

func TestParseMacvlanInfoData(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	mode := func(m uint32) []byte { return rtattr(unix.IFLA_MACVLAN_MODE, []byte{byte(m), 0, 0, 0}) }
	addr := func(last byte) []byte { return rtattr(unix.IFLA_MACVLAN_MACADDR, []byte{2, 0, 0, 0, 0, last}) }
	tests := []struct {
		description string
		nest        []byte
		nilInfo     bool
		// expected holds the -j view's keys to check; nil means absent.
		expected map[string]any
		unknown  []uint16
	}{
		{
			description: "positive: a bare macvlan's dump",
			nest:        macvlanKind.dump(t, "add_default", pcap),
			expected:    map[string]any{"mode": "vepa", "bcqueuelen": float64(1000), "usedbcqueuelen": float64(1000), "nodst": nil, "nopromisc": nil},
		},
		{
			description: "positive: the source list",
			nest:        macvlanKind.dump(t, "add_macaddr_set", pcap),
			expected:    map[string]any{"mode": "source", "macaddr_count": float64(2), "macaddr_data": []any{macvlanMAC2, macvlanMAC1}},
		},
		{
			description: "positive: a negative cutoff",
			nest:        macvlanKind.dump(t, "add_bclim_negative", pcap),
			expected:    map[string]any{"bclim": float64(-1)},
		},
		{
			description: "positive: a macvtap decodes the same",
			nest:        macvtapKind.dump(t, "add_mode_passthru", pcap),
			expected:    map[string]any{"mode": "passthru"},
		},
		{
			description: "negative: a nil nest",
			nest:        nil,
			nilInfo:     true,
		},
		{
			description: "negative: a short mode",
			nest:        slices.Concat(rtattr(unix.IFLA_MACVLAN_MODE, []byte{4, 0}), rtattr(unix.IFLA_MACVLAN_BC_QUEUE_LEN, []byte{1, 0, 0, 0})),
			expected:    map[string]any{"mode": nil, "bcqueuelen": nil},
		},
		{
			description: "boundary: an empty nest",
			nest:        []byte{},
			expected:    map[string]any{"mode": nil},
		},
		{
			description: "boundary: both flags",
			nest:        slices.Concat(mode(MacvlanModePassthru), rtattr(unix.IFLA_MACVLAN_FLAGS, []byte{3, 0})),
			expected:    map[string]any{"nopromisc": true, "nodst": true},
		},
		{
			description: "corner: a source list outside source mode is decoded, not rendered",
			nest: slices.Concat(mode(MacvlanModeBridge), rtattr(unix.IFLA_MACVLAN_MACADDR_COUNT, []byte{1, 0, 0, 0}),
				rtattr(unix.IFLA_MACVLAN_MACADDR_DATA, addr(1))),
			expected: map[string]any{"mode": "bridge", "macaddr_count": nil, "macaddr_data": nil},
		},
		{
			description: "corner: a non-address entry in the list is skipped",
			nest: slices.Concat(mode(MacvlanModeSource), rtattr(unix.IFLA_MACVLAN_MACADDR_COUNT, []byte{1, 0, 0, 0}),
				rtattr(unix.IFLA_MACVLAN_MACADDR_DATA, slices.Concat(rtattr(1, []byte{9, 9, 9, 9}), addr(7)))),
			expected: map[string]any{"macaddr_data": []any{"02:00:00:00:00:07"}},
		},
		{
			description: "corner: an id past IFLA_MACVLAN_BC_CUTOFF is listed",
			nest:        slices.Concat(rtattr(10, []byte{1}), mode(MacvlanModePrivate)),
			expected:    map[string]any{"mode": "private"},
			unknown:     []uint16{10},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			m := ParseMacvlanInfoData(tt.nest)
			if (m == nil) != tt.nilInfo {
				t.Fatalf("nil info: got %v, want %v", m == nil, tt.nilInfo)
			}
			if m == nil {
				return
			}
			view := macvlanView(m)
			for k, want := range tt.expected {
				got, ok := view[k]
				if want == nil && ok {
					t.Errorf("%s: got %v, want absent", k, got)
				}
				if want != nil && !reflect.DeepEqual(got, want) {
					t.Errorf("%s: got %v, want %v", k, got, want)
				}
			}
			if !slices.Equal(m.Unknown, tt.unknown) {
				t.Errorf("unknown: got %v, want %v", m.Unknown, tt.unknown)
			}
		})
	}
}

func TestEncodeMacvlanInfoData(t *testing.T) {
	type row struct {
		description string
		settings    []MacvlanSetting
		expected    []byte
		err         error
	}
	u32 := func(atype uint16, v byte) []byte { return rtattr(atype, []byte{v, 0, 0, 0}) }
	tests := []row{
		{
			description: "negative: nopromisc outside passthru",
			settings:    macvlanArgs(t, "mode bridge nopromisc"),
			err:         ErrMacvlanNopromisc,
		},
		{
			description: "negative: an address of 5 bytes",
			settings:    []MacvlanSetting{{Option: MacvlanOptionMacaddr, Value: MacvlanMacaddrAdd, Addrs: [][]byte{{2, 0, 0, 0, 1}}}},
			err:         ErrKindSettingRange,
		},
		{
			description: "negative: a mode no kernel defines",
			settings:    []MacvlanSetting{{Option: MacvlanOptionMode, Value: 3}},
			err:         ErrKindSettingRange,
		},
		{
			description: "negative: a setting with no option",
			settings:    []MacvlanSetting{{}},
			err:         ErrKindSettingOption,
		},
		{
			description: "boundary: macaddr flush",
			settings:    macvlanArgs(t, "macaddr flush"),
			expected:    u32(unix.IFLA_MACVLAN_MACADDR_MODE, byte(MacvlanMacaddrFlush)),
		},
		{
			description: "boundary: flag null",
			settings:    macvlanArgs(t, "flag null"),
			expected:    rtattr(unix.IFLA_MACVLAN_FLAGS, []byte{0, 0}),
		},
		{
			description: "boundary: the widest queue length",
			settings:    macvlanArgs(t, "bcqueuelen 4294967295"),
			expected:    rtattr(unix.IFLA_MACVLAN_BC_QUEUE_LEN, []byte{0xff, 0xff, 0xff, 0xff}),
		},
		{
			description: "corner: mode and flags go last",
			settings:    macvlanArgs(t, "nodst mode bridge bcqueuelen 5"),
			expected: slices.Concat(u32(unix.IFLA_MACVLAN_BC_QUEUE_LEN, 5), u32(unix.IFLA_MACVLAN_MODE, byte(MacvlanModeBridge)),
				rtattr(unix.IFLA_MACVLAN_FLAGS, []byte{byte(MacvlanFlagNodst), 0})),
		},
		{
			description: "corner: macaddr set with no address",
			settings:    macvlanArgs(t, "macaddr set"),
			expected:    slices.Concat(u32(unix.IFLA_MACVLAN_MACADDR_MODE, byte(MacvlanMacaddrSet)), []byte{4, 0, unix.IFLA_MACVLAN_MACADDR_DATA, 0}),
		},
		{
			description: "corner: the last mode wins",
			settings:    macvlanArgs(t, "mode bridge mode private"),
			expected:    u32(unix.IFLA_MACVLAN_MODE, byte(MacvlanModePrivate)),
		},
	}
	words := macvlanCorpusWords()
	sent := 0
	for _, k := range []kindRows{macvlanKind, macvtapKind} {
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
				settings:    macvlanArgs(t, w),
				expected:    append([]byte{}, data...),
			})
		}
	}
	if sent != 30 {
		t.Errorf("%d macvlan and macvtap rows sent a request, want 30", sent)
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := EncodeMacvlanInfoData(tt.settings)
			if err != tt.err {
				t.Fatalf("err: got %v, want %v", err, tt.err)
			}
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("got  % x\nwant % x", got, tt.expected)
			}
		})
	}
}

func TestLinkInfoMacvlanKinds(t *testing.T) {
	tests := []struct {
		description string
		row         string
		expected    bool
	}{
		{description: "positive: a macvlan decodes Macvlan", row: "macvlan/add_mode_bridge", expected: true},
		{description: "positive: a macvtap decodes Macvlan", row: "macvtap/add_mode_bridge", expected: true},
		{description: "negative: a vlan does not", row: "vlan/add_default"},
		{description: "corner: an ipvlan, another lower-stacked kind, does not", row: "ipvlan/add_default"},
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
			if got := li.Macvlan != nil; got != tt.expected {
				t.Errorf("Macvlan %v for kind %q, want %v", got, li.Kind, tt.expected)
			}
		})
	}
}

func BenchmarkParseMacvlanInfoData(b *testing.B) {
	nest := macvlanKind.dump(b, "add_macaddr_set", "netlink_route_getlink_detail_json.pcap")
	b.ReportAllocs()
	for b.Loop() {
		if ParseMacvlanInfoData(nest) == nil {
			b.Fatal("nil")
		}
	}
}
