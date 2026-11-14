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

// The amt codec's tests, against the amt rows of the 7.3.0 kind corpus
// (docs/netlink/goip-link-kinds.md, "KP3-T03 The bareudp and amt parsers").

var amtKind = kindRows{kind: amtKindCst, nest: unix.IFLA_INFO_DATA, key: "info_data"}

// amtCorpusWords are the option words after `type amt` of every row that
// sent a request (kind-write-corpus.exp), keyed by slug: 5 accepted and 8
// refused by the kernel.
func amtCorpusWords() map[string]string {
	const relay = "mode relay dev d0 local 10.0.0.1"
	const gateway = "mode gateway dev d0 local 10.0.0.1 discovery 10.0.0.2"
	return map[string]string{
		"add_gateway":              gateway,
		"add_relay":                relay,
		"add_ports":                gateway + " relay_port 3000 gateway_port 3001 max_tunnels 10",
		"add_max_tunnels_zero":     relay + " max_tunnels 0",
		"add_mode_twice":           "mode gateway mode relay dev d0 local 10.0.0.1",
		"add_no_option":            "",
		"add_no_link":              "mode relay local 10.0.0.1",
		"add_gateway_no_discovery": "mode gateway dev d0 local 10.0.0.1",
		"add_on_loopback":          "mode relay dev lo local 10.0.0.1",
		"add_local_loopback":       "mode relay dev d0 local 127.0.0.1",
		"add_relay_port_zero":      relay + " relay_port 0",
		"add_discovery_multicast":  "mode gateway dev d0 local 10.0.0.1 discovery 224.0.0.1",
		"set_same":                 relay,
	}
}

// amtArgs parses option words as amt_parse_opt does, for well-formed words;
// ifindex maps a dev's name to its index.
func amtArgs(t testing.TB, words string, ifindex map[string]uint64) []AmtSetting {
	t.Helper()
	argv := strings.Fields(words)
	num := func(s string) uint64 {
		v, err := strconv.ParseUint(s, 0, 64)
		if err != nil {
			t.Fatal(err)
		}
		return v
	}
	opts := map[string]AmtOption{
		"relay_port": AmtOptionRelayPort, "gateway_port": AmtOptionGatewayPort, "max_tunnels": AmtOptionMaxTunnels,
	}
	var out []AmtSetting
	for i := 0; i+1 < len(argv); i += 2 {
		kw, v := argv[i], argv[i+1]
		var s AmtSetting
		switch kw {
		case "mode":
			s = AmtSetting{Option: AmtOptionMode, Value: map[string]uint64{"relay": 1}[v]}
		case "dev":
			idx, ok := ifindex[v]
			if !ok {
				t.Fatalf("no ifindex for %q", v)
			}
			s = AmtSetting{Option: AmtOptionLink, Value: idx}
		case "local":
			s = AmtSetting{Option: AmtOptionLocal, Addr: netip.MustParseAddr(v)}
		case "discovery":
			s = AmtSetting{Option: AmtOptionDiscovery, Addr: netip.MustParseAddr(v)}
		default:
			opt, ok := opts[kw]
			if !ok {
				t.Fatalf("no amt keyword %q", kw)
			}
			s = AmtSetting{Option: opt, Value: num(v)}
		}
		out = append(out, s)
	}
	return out
}

// amtIfindex maps the names a row's captures carry to their ifindexes; lo
// is always 1.
func amtIfindex(t testing.TB, slug string) map[string]uint64 {
	t.Helper()
	out := map[string]uint64{"lo": 1}
	for _, pcap := range []string{"ip.pcap", "netlink_route_getlink_detail_json.pcap"} {
		for idx, name := range amtKind.names(t, slug, pcap) {
			out[name] = idx
		}
	}
	return out
}

// amtView renders a as amt_print_opt prints it under -j; names maps an
// ifindex to its device's name.
func amtView(a *AmtInfo, names map[uint64]string) map[string]any {
	out := map[string]any{}
	if a.HasMode && a.Mode < uint32(len(AmtModeNames)) {
		out["mode"] = AmtModeNames[a.Mode]
	}
	if a.HasGatewayPort {
		out["gateway_port"] = float64(a.GatewayPort)
	}
	if a.HasRelayPort {
		out["relay_port"] = float64(a.RelayPort)
	}
	for key, addr := range map[string]netip.Addr{"local": a.Local, "remote": a.Remote, "discovery": a.Discovery} {
		if addr.IsValid() {
			out[key] = addr.String()
		}
	}
	if a.HasLink {
		name, ok := names[uint64(a.Link)]
		if !ok {
			name = fmt.Sprintf("if%d", a.Link)
		}
		out["link"] = name
	}
	if a.HasMaxTunnels {
		out["max_tunnels"] = float64(a.MaxTunnels)
	}
	return out
}

func TestAmtDumpMatchesReadback(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	slugs := amtKind.accepted(t)
	for _, slug := range slugs {
		t.Run("positive: "+slug, func(t *testing.T) {
			got := amtView(ParseAmtInfoData(amtKind.dump(t, slug, pcap)), amtKind.names(t, slug, pcap))
			want := amtKind.readback(t, slug)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("dump renders\n %v\nreadback\n %v", got, want)
			}
		})
	}
	if len(slugs) != 5 {
		t.Errorf("%d accepted amt rows, want 5", len(slugs))
	}
}

func TestParseAmtInfoData(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	names := map[uint64]string{}
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
			description: "positive: a gateway's dump",
			nest:        amtKind.dump(t, "add_gateway", pcap),
			expected: map[string]any{"mode": "gateway", "local": "10.0.0.1", "discovery": "10.0.0.2",
				"relay_port": float64(2268), "gateway_port": float64(2268)},
		},
		{
			description: "positive: the ports and max_tunnels",
			nest:        amtKind.dump(t, "add_ports", pcap),
			expected:    map[string]any{"relay_port": float64(3000), "gateway_port": float64(3001), "max_tunnels": float64(10)},
		},
		{
			description: "negative: a nil nest",
			nest:        nil,
			nilInfo:     true,
		},
		{
			description: "negative: a short local",
			nest:        rtattr(IflaAmtLocalIP, []byte{10, 0}),
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
			description: "boundary: mode 1 is relay",
			nest:        rtattr(IflaAmtMode, []byte{1, 0, 0, 0}),
			expected:    map[string]any{"mode": "relay"},
			exact:       true,
		},
		{
			description: "corner: mode 2 has no name",
			nest:        rtattr(IflaAmtMode, []byte{2, 0, 0, 0}),
			expected:    map[string]any{},
			exact:       true,
		},
		{
			description: "corner: a remote of 0.0.0.0 still prints",
			nest:        rtattr(IflaAmtRemoteIP, []byte{0, 0, 0, 0}),
			expected:    map[string]any{"remote": "0.0.0.0"},
			exact:       true,
		},
		{
			description: "corner: an unnamed lower prints as if<n>",
			nest:        rtattr(IflaAmtLink, []byte{7, 0, 0, 0}),
			expected:    map[string]any{"link": "if7"},
			exact:       true,
		},
		{
			description: "corner: an id past IFLA_AMT_MAX_TUNNELS is listed",
			nest:        slices.Concat(rtattr(9, []byte{1}), rtattr(IflaAmtMaxTunnels, []byte{0, 1, 0, 0})),
			expected:    map[string]any{"max_tunnels": float64(256)},
			exact:       true,
			unknown:     []uint16{9},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			a := ParseAmtInfoData(tt.nest)
			if (a == nil) != tt.nilInfo {
				t.Fatalf("nil info: got %v, want %v", a == nil, tt.nilInfo)
			}
			if a == nil {
				return
			}
			got := amtView(a, names)
			if tt.exact && !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got %v, want %v", got, tt.expected)
			}
			for _, k := range slices.Sorted(maps.Keys(tt.expected)) {
				if !reflect.DeepEqual(got[k], tt.expected[k]) {
					t.Errorf("%s: got %v, want %v", k, got[k], tt.expected[k])
				}
			}
			if !slices.Equal(a.Unknown, tt.unknown) {
				t.Errorf("unknown: got %v, want %v", a.Unknown, tt.unknown)
			}
		})
	}
}

func TestEncodeAmtInfoData(t *testing.T) {
	type row struct {
		description string
		settings    []AmtSetting
		expected    []byte
		err         error
	}
	u32 := func(atype uint16, v byte) []byte { return rtattr(atype, []byte{v, 0, 0, 0}) }
	local := rtattr(IflaAmtLocalIP, []byte{10, 0, 0, 1})
	d0 := map[string]uint64{"d0": 2}
	args := func(words string) []AmtSetting { return amtArgs(t, words, d0) }
	tests := []row{
		{
			description: "negative: a relay_port past 65535",
			settings:    args("relay_port 70000"),
			err:         ErrKindSettingRange,
		},
		{
			description: "negative: a setting with no option",
			settings:    []AmtSetting{{}},
			err:         ErrKindSettingOption,
		},
		{
			description: "negative: local twice",
			settings:    args("local 10.0.0.1 local 10.0.0.3"),
			err:         ErrKindSettingConflict,
		},
		{
			description: "negative: a mode past relay",
			settings:    []AmtSetting{{Option: AmtOptionMode, Value: 2}},
			err:         ErrKindSettingRange,
		},
		{
			description: "boundary: a relay with max_tunnels 0 sends MAX_TUNNELS 0",
			settings:    args("mode relay max_tunnels 0"),
			expected:    slices.Concat(u32(IflaAmtMode, 1), u32(IflaAmtMaxTunnels, 0)),
		},
		{
			description: "corner: mode gateway mode relay sends two MODEs",
			settings:    args("mode gateway mode relay dev d0 local 10.0.0.1"),
			expected:    slices.Concat(u32(IflaAmtMode, 0), u32(IflaAmtMode, 1), u32(IflaAmtLink, 2), local),
		},
		{
			description: "corner: the ports go big-endian, in the words' order",
			settings:    args("gateway_port 3001 relay_port 3000"),
			expected: slices.Concat(rtattr(IflaAmtGatewayPort, []byte{0x0b, 0xb9}),
				rtattr(IflaAmtRelayPort, []byte{0x0b, 0xb8})),
		},
		{
			description: "corner: an IPv6 local is sent at its length",
			settings:    args("local 2001:db8::1"),
			expected:    rtattr(IflaAmtLocalIP, netip.MustParseAddr("2001:db8::1").AsSlice()),
		},
	}
	words := amtCorpusWords()
	ents, err := os.ReadDir(tdLinkWrite_7_3_0 + "/kinds/" + amtKindCst)
	if err != nil {
		t.Fatal(err)
	}
	sent := 0
	for _, e := range ents {
		slug := e.Name()
		data, _, ok := amtKind.request(t, slug)
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
			settings:    amtArgs(t, w, amtIfindex(t, slug)),
			expected:    append([]byte{}, data...),
		})
	}
	if sent != 13 {
		t.Errorf("%d amt rows sent a request, want 13", sent)
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := EncodeAmtInfoData(tt.settings)
			if err != tt.err {
				t.Fatalf("err: got %v, want %v", err, tt.err)
			}
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("got  % x\nwant % x", got, tt.expected)
			}
		})
	}
}

func BenchmarkParseAmtInfoData(b *testing.B) {
	nest := amtKind.dump(b, "add_ports", "netlink_route_getlink_detail_json.pcap")
	b.ReportAllocs()
	for b.Loop() {
		if ParseAmtInfoData(nest) == nil {
			b.Fatal("nil")
		}
	}
}
