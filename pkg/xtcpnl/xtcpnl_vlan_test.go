package xtcpnl

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// The vlan codec's tests, against the vlan rows of the 7.3.0 kind corpus
// (docs/netlink/goip-link-kinds.md, "KP2-T01 The vlan parser").

var vlanKind = kindRows{kind: vlanKindCst, nest: unix.IFLA_INFO_DATA, key: "info_data"}

// vlanCorpusWords are the option words after `type vlan` of every vlan row
// that sent a request (kind-write-corpus.exp): the 15 the kernel accepted
// and the 6 it refused.
func vlanCorpusWords() map[string]string {
	w := map[string]string{
		"add_default":              "id 10",
		"add_protocol":             "id 10 protocol 802.1ad",
		"add_ingress_qos_map":      "id 10 ingress-qos-map 1:2 3:4",
		"add_egress_qos_map":       "id 10 egress-qos-map 2:1 4:3",
		"add_several":              "id 10 protocol 802.1ad gvrp on mvrp on",
		"add_id_max":               "id 4094",
		"add_abbrev":               "i 10",
		"set_flags":                "loose_binding on",
		"set_egress_qos_map":       "egress-qos-map 1:5",
		"set_id":                   "id 20",
		"add_no_id":                "",
		"add_no_link":              "id 10",
		"add_id_over":              "id 4095",
		"add_protocol_unsupported": "id 10 protocol ip",
		"add_duplicate_id":         "id 10",
		"add_on_loopback":          "id 10",
	}
	for _, f := range []string{"reorder_hdr off", "gvrp on", "mvrp on", "loose_binding on", "bridge_binding on"} {
		w["add_"+strings.Fields(f)[0]] = "id 10 " + f
	}
	return w
}

// vlanProtocols are the ll_proto names the corpus uses.
var vlanProtocols = map[string]uint16{bridgeProto8021Q: 0x8100, bridgeProto8021ad: 0x88a8, "ip": 0x0800}

// vlanArgs parses option words as vlan_parse_opt does, for well-formed
// words: keywords are matched as prefixes in its order, so "i" is id.
func vlanArgs(t testing.TB, words string) []VlanSetting {
	t.Helper()
	keywords := make([]string, 0, 2+len(VlanFlags)+2)
	keywords = append(keywords, "protocol", "id")
	for _, f := range VlanFlags {
		keywords = append(keywords, f.Keyword)
	}
	keywords = append(keywords, "ingress-qos-map", "egress-qos-map")
	argv := strings.Fields(words)
	var out []VlanSetting
	for i := 0; i < len(argv); i++ {
		kw := ""
		for _, k := range keywords {
			if strings.HasPrefix(k, argv[i]) {
				kw = k
				break
			}
		}
		i++
		switch kw {
		case "protocol":
			p, ok := vlanProtocols[argv[i]]
			if !ok {
				t.Fatalf("protocol %q", argv[i])
			}
			out = append(out, VlanSetting{Option: VlanOptionProtocol, Value: uint64(p)})
		case "id":
			n, err := strconv.ParseUint(argv[i], 0, 64)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, VlanSetting{Option: VlanOptionID, Value: n})
		case "ingress-qos-map", "egress-qos-map":
			s := VlanSetting{Option: VlanOptionIngressQos}
			if kw == "egress-qos-map" {
				s.Option = VlanOptionEgressQos
			}
			for ; i < len(argv) && strings.Contains(argv[i], ":"); i++ {
				var m VlanQosMapping
				if _, err := fmt.Sscanf(argv[i], "%d:%d", &m.From, &m.To); err != nil {
					t.Fatal(err)
				}
				s.Map = append(s.Map, m)
			}
			i--
			out = append(out, s)
		default:
			f := slices.IndexFunc(VlanFlags, func(f VlanFlag) bool { return f.Keyword == kw })
			if f < 0 {
				t.Fatalf("no vlan keyword for %q", argv[i-1])
			}
			out = append(out, VlanSetting{Option: VlanOptionFlag, Bit: VlanFlags[f].Bit, Value: map[string]uint64{"on": 1}[argv[i]]})
		}
	}
	return out
}

// vlanView renders v as vlan_print_opt prints it under -j: nothing without
// an id, the protocol 802.1Q when absent, the flags' names with any leftover
// bits in hex, and the qos maps.
func vlanView(v *VlanInfo) map[string]any {
	out := map[string]any{}
	if !v.HasID {
		return out
	}
	out["protocol"] = bridgeProto8021Q
	if v.HasProtocol {
		out["protocol"] = vlanProtocolName(v.Protocol)
	}
	out["id"] = float64(v.ID)
	if v.HasFlags {
		names := []any{}
		left := v.Flags
		for _, f := range VlanFlags {
			if left&f.Bit != 0 {
				left &^= f.Bit
				names = append(names, f.Name)
			}
		}
		if left != 0 {
			names = append(names, strconv.FormatUint(uint64(left), 16))
		}
		out["flags"] = names
	}
	for key, m := range map[string][]VlanQosMapping{"ingress_qos": v.Ingress, "egress_qos": v.Egress} {
		if (key == "ingress_qos" && !v.HasIngress) || (key == "egress_qos" && !v.HasEgress) {
			continue
		}
		list := []any{}
		for _, p := range m {
			list = append(list, map[string]any{"from": float64(p.From), "to": float64(p.To)})
		}
		out[key] = list
	}
	return out
}

func vlanProtocolName(p uint16) string {
	for name, v := range vlanProtocols {
		if v == p {
			return name
		}
	}
	return fmt.Sprintf("%#x", p)
}

func TestVlanDumpMatchesReadback(t *testing.T) {
	rows := vlanKind.accepted(t)
	if len(rows) != 15 {
		t.Fatalf("%d accepted vlan rows, want 15", len(rows))
	}
	for _, slug := range rows {
		t.Run("positive: "+slug, func(t *testing.T) {
			got := vlanView(ParseVlanInfoData(vlanKind.dump(t, slug, "netlink_route_getlink_detail_json.pcap")))
			want := vlanKind.readback(t, slug)
			if !reflect.DeepEqual(got, want) {
				t.Errorf("dump renders\n %v\nreadback\n %v", got, want)
			}
		})
	}
}

func TestParseVlanInfoData(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	tests := []struct {
		description string
		nest        []byte
		nilInfo     bool
		// expected holds the -j view's keys to check; nil means absent.
		expected map[string]any
		unknown  []uint16
	}{
		{
			description: "positive: a bare vlan's dump",
			nest:        vlanKind.dump(t, "add_default", pcap),
			expected:    map[string]any{"id": float64(10), "protocol": bridgeProto8021Q, "flags": []any{"REORDER_HDR"}},
		},
		{
			description: "positive: 802.1ad",
			nest:        vlanKind.dump(t, "add_protocol", pcap),
			expected:    map[string]any{"protocol": bridgeProto8021ad},
		},
		{
			description: "positive: the ingress map",
			nest:        vlanKind.dump(t, "add_ingress_qos_map", pcap),
			expected: map[string]any{"ingress_qos": []any{
				map[string]any{"from": float64(1), "to": float64(2)}, map[string]any{"from": float64(3), "to": float64(4)},
			}},
		},
		{
			description: "positive: the egress map",
			nest:        vlanKind.dump(t, "add_egress_qos_map", pcap),
			expected: map[string]any{"egress_qos": []any{
				map[string]any{"from": float64(2), "to": float64(1)}, map[string]any{"from": float64(4), "to": float64(3)},
			}, "ingress_qos": nil},
		},
		{
			description: "positive: a change keeps the id",
			nest:        vlanKind.dump(t, "set_id", pcap),
			expected:    map[string]any{"id": float64(10)},
		},
		{
			description: "negative: a nil nest",
			nest:        nil,
			nilInfo:     true,
		},
		{
			description: "negative: a short id",
			nest:        rtattr(unix.IFLA_VLAN_ID, []byte{10}),
			expected:    map[string]any{"id": nil, "protocol": nil},
		},
		{
			description: "boundary: an empty nest",
			nest:        []byte{},
			expected:    map[string]any{"id": nil, "flags": nil},
		},
		{
			description: "boundary: the highest id",
			nest:        vlanKind.dump(t, "add_id_max", pcap),
			expected:    map[string]any{"id": float64(4094)},
		},
		{
			description: "corner: protocol is big-endian",
			nest:        slices.Concat(rtattr(unix.IFLA_VLAN_ID, []byte{1, 0}), rtattr(unix.IFLA_VLAN_PROTOCOL, []byte{0x88, 0xa8})),
			expected:    map[string]any{"protocol": bridgeProto8021ad},
		},
		{
			description: "corner: a flag bit past BRIDGE_BINDING",
			nest:        slices.Concat(rtattr(unix.IFLA_VLAN_ID, []byte{1, 0}), rtattr(unix.IFLA_VLAN_FLAGS, []byte{0x21, 0, 0, 0, 0xff, 0xff, 0xff, 0xff})),
			expected:    map[string]any{"flags": []any{"REORDER_HDR", "20"}},
		},
		{
			description: "corner: an id past IFLA_VLAN_PROTOCOL",
			nest:        slices.Concat(rtattr(6, []byte{1}), rtattr(unix.IFLA_VLAN_ID, []byte{7, 0})),
			expected:    map[string]any{"id": float64(7)},
			unknown:     []uint16{6},
		},
		{
			description: "corner: a qos nest with a short entry",
			nest: slices.Concat(rtattr(unix.IFLA_VLAN_ID, []byte{1, 0}), rtattr(unix.IFLA_VLAN_INGRESS_QOS,
				slices.Concat(rtattr(unix.IFLA_VLAN_QOS_MAPPING, []byte{9, 0, 0, 0}), rtattr(unix.IFLA_VLAN_QOS_MAPPING, []byte{5, 0, 0, 0, 6, 0, 0, 0})))),
			expected: map[string]any{"ingress_qos": []any{map[string]any{"from": float64(5), "to": float64(6)}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			v := ParseVlanInfoData(tt.nest)
			if (v == nil) != tt.nilInfo {
				t.Fatalf("nil info: got %v, want %v", v == nil, tt.nilInfo)
			}
			if v == nil {
				return
			}
			view := vlanView(v)
			for k, want := range tt.expected {
				got, ok := view[k]
				if want == nil && ok {
					t.Errorf("%s: got %v, want absent", k, got)
				}
				if want != nil && !reflect.DeepEqual(got, want) {
					t.Errorf("%s: got %v, want %v", k, got, want)
				}
			}
			if !slices.Equal(v.Unknown, tt.unknown) {
				t.Errorf("unknown: got %v, want %v", v.Unknown, tt.unknown)
			}
		})
	}
}

func TestEncodeVlanInfoData(t *testing.T) {
	type row struct {
		description string
		settings    []VlanSetting
		expected    []byte
		err         error
	}
	id10 := rtattr(unix.IFLA_VLAN_ID, []byte{10, 0})
	flagsAttr := func(flags, mask byte) []byte {
		return rtattr(unix.IFLA_VLAN_FLAGS, []byte{flags, 0, 0, 0, mask, 0, 0, 0})
	}
	tests := []row{
		{
			description: "negative: an id too wide",
			settings:    []VlanSetting{{Option: VlanOptionID, Value: 65536}},
			err:         ErrKindSettingRange,
		},
		{
			description: "negative: a setting with no option",
			settings:    []VlanSetting{{}},
			err:         ErrKindSettingOption,
		},
		{
			description: "negative: a flag with no bit",
			settings:    []VlanSetting{{Option: VlanOptionFlag, Value: 1}},
			err:         ErrKindSettingRange,
		},
		{
			description: "boundary: an empty qos map is an empty nest",
			settings:    []VlanSetting{{Option: VlanOptionEgressQos}},
			expected:    []byte{4, 0, unix.IFLA_VLAN_EGRESS_QOS, 0},
		},
		{
			description: "boundary: no settings",
			settings:    nil,
			expected:    nil,
		},
		{
			description: "corner: flags go last",
			settings:    vlanArgs(t, "gvrp on id 10"),
			expected:    slices.Concat(id10, flagsAttr(0x2, 0x2)),
		},
		{
			description: "corner: two flags, one attribute",
			settings:    vlanArgs(t, "gvrp on mvrp on"),
			expected:    flagsAttr(0xa, 0xa),
		},
		{
			description: "corner: off sets the mask, not the flag",
			settings:    vlanArgs(t, "reorder_hdr off"),
			expected:    flagsAttr(0, 0x1),
		},
		{
			description: "corner: a later word for a bit wins",
			settings:    vlanArgs(t, "gvrp on gvrp off"),
			expected:    flagsAttr(0, 0x2),
		},
	}
	words := vlanCorpusWords()
	ents, err := os.ReadDir(tdLinkWrite_7_3_0 + "/kinds/vlan")
	if err != nil {
		t.Fatal(err)
	}
	sent := 0
	for _, e := range ents {
		data, _, ok := vlanKind.request(t, e.Name())
		w, listed := words[e.Name()]
		if ok != listed {
			t.Errorf("%s: sent a request %v, listed %v", e.Name(), ok, listed)
		}
		if !ok || !listed {
			continue
		}
		sent++
		tests = append(tests, row{
			description: "positive: " + e.Name() + "'s request",
			settings:    vlanArgs(t, w),
			expected:    append([]byte{}, data...),
		})
	}
	if sent != 21 {
		t.Errorf("%d vlan rows sent a request, want 21", sent)
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := EncodeVlanInfoData(tt.settings)
			if err != tt.err {
				t.Fatalf("err: got %v, want %v", err, tt.err)
			}
			if !bytes.Equal(got, tt.expected) {
				t.Errorf("got  % x\nwant % x", got, tt.expected)
			}
		})
	}
}

func BenchmarkParseVlanInfoData(b *testing.B) {
	nest := vlanKind.dump(b, "add_ingress_qos_map", "netlink_route_getlink_detail_json.pcap")
	b.ReportAllocs()
	for b.Loop() {
		if ParseVlanInfoData(nest) == nil {
			b.Fatal("nil")
		}
	}
}
