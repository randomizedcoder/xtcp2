package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// KP1-T01: the bridge codec against the KC2-T01 rows of the 7.3.0 kind
// corpus (docs/netlink/goip-link-kinds.md, "KP1-T01 The bridge parser").

// kindRows reads one kind's rows of the 7.3.0 kind corpus: its directory,
// the data nest its options go in, and its option table.
type kindRows struct {
	kind string
	nest uint16 // IFLA_INFO_DATA or IFLA_INFO_SLAVE_DATA
	key  string // the readback's JSON object for the nest
	opts []BridgeOptionSpec
}

var (
	bridgeKind      = kindRows{kind: bridgeKindCst, nest: unix.IFLA_INFO_DATA, key: "info_data", opts: BridgeOptions}
	bridgeSlaveKind = kindRows{kind: "bridge_slave", nest: unix.IFLA_INFO_SLAVE_DATA, key: "info_slave_data", opts: BridgeSlaveOptions}
)

// dir is the directory of row slug.
func (k kindRows) dir(slug string) string {
	return tdLinkWrite_7_3_0 + "/kinds/" + k.kind + "/" + slug
}

// data returns the kind's nest in an RTM_NEWLINK body, and whether the nest
// is there at all.
func (k kindRows) data(t testing.TB, body []byte) ([]byte, bool) {
	t.Helper()
	var data []byte
	var has bool
	if err := WalkRTAttrs(body[IfInfomsgSizeCst:], func(atype uint16, v []byte) {
		if atype != unix.IFLA_LINKINFO {
			return
		}
		walkNestTolerant(v, func(a uint16, in []byte) {
			if a == k.nest && !has {
				data, has = in, true
			}
		})
	}); err != nil {
		t.Fatal(err)
	}
	return data, has
}

// request returns the nest of the RTM_NEWLINK request in row slug's
// ip.pcap; sent is false when ip sent no request.
func (k kindRows) request(t testing.TB, slug string) (data []byte, has, sent bool) {
	t.Helper()
	for _, m := range pcapNlMsgs(t, k.dir(slug)+"/ip.pcap") {
		if m.h.Type == unix.RTM_NEWLINK && m.h.Flags&unix.NLM_F_REQUEST != 0 {
			data, has = k.data(t, m.body)
			return data, has, true
		}
	}
	return nil, false, false
}

// dump returns the nest of the RTM_NEWLINK answer in readback pcap of row
// slug, skipping the answers without it: the name lookup's, and the
// backup port's name.
func (k kindRows) dump(t testing.TB, slug, pcap string) []byte {
	t.Helper()
	for _, m := range pcapNlMsgs(t, k.dir(slug)+"/"+pcap) {
		if m.h.Type != unix.RTM_NEWLINK || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		if data, ok := k.data(t, m.body); ok {
			return data
		}
	}
	t.Fatalf("%s: no RTM_NEWLINK answer with nest %d in %s", slug, k.nest, pcap)
	return nil
}

// names maps every ifindex an RTM_NEWLINK answer in readback pcap of row
// slug names to its IFLA_IFNAME: ll_index_to_name's lookups.
func (k kindRows) names(t testing.TB, slug, pcap string) map[uint64]string {
	t.Helper()
	out := map[uint64]string{}
	for _, m := range pcapNlMsgs(t, k.dir(slug)+"/"+pcap) {
		if m.h.Type != unix.RTM_NEWLINK || m.h.Flags&unix.NLM_F_REQUEST != 0 {
			continue
		}
		idx := uint64(binary.LittleEndian.Uint32(m.body[4:8]))
		if err := WalkRTAttrs(m.body[IfInfomsgSizeCst:], func(atype uint16, v []byte) {
			if atype == unix.IFLA_IFNAME {
				out[idx] = string(bytes.TrimRight(v, "\x00"))
			}
		}); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

// accepted are the rows ip and the kernel accepted.
func (k kindRows) accepted(t testing.TB) []string {
	t.Helper()
	ents, err := os.ReadDir(tdLinkWrite_7_3_0 + "/kinds/" + k.kind)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		rc, err := os.ReadFile(k.dir(e.Name()) + "/ip.rc")
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(rc)) == "0" {
			out = append(out, e.Name())
		}
	}
	return out
}

// readback is the nest's object in row slug's `ip -d -j link show`.
func (k kindRows) readback(t testing.TB, slug string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(k.dir(slug) + "/ip_getlink_detail_json")
	if err != nil {
		t.Fatal(err)
	}
	var links []struct {
		LinkInfo map[string]json.RawMessage `json:"linkinfo"`
	}
	if err := json.Unmarshal(raw, &links); err != nil {
		t.Fatal(err)
	}
	if len(links) != 1 {
		t.Fatalf("%s: %d links in the readback", slug, len(links))
	}
	var out map[string]any
	if err := json.Unmarshal(links[0].LinkInfo[k.key], &out); err != nil {
		t.Fatal(err)
	}
	return out
}

// spec is the table row whose Keyword is kw.
func (k kindRows) spec(t testing.TB, kw string) *BridgeOptionSpec {
	t.Helper()
	for i := range k.opts {
		if k.opts[i].Keyword == kw {
			return &k.opts[i]
		}
	}
	t.Fatalf("no %s option %q", k.kind, kw)
	return nil
}

// args turns a row's words into settings: full keywords only, since
// keyword matching is KG1's. Values are what ip's parsers produce; an
// ifindex is given as its number.
func (k kindRows) args(t testing.TB, words string) []BridgeSetting {
	t.Helper()
	f := strings.Fields(words)
	var out []BridgeSetting
	for i := 0; i < len(f); i++ {
		s := BridgeSetting{Spec: k.spec(t, f[i])}
		if s.Spec.Wire != BridgeWireFlag {
			i++
			s.Value, s.Bytes = bridgeArgValue(t, s.Spec, f[i])
		}
		out = append(out, s)
	}
	return out
}

func bridgeArgValue(t testing.TB, spec *BridgeOptionSpec, v string) (uint64, []byte) {
	t.Helper()
	if i := slices.Index(spec.Names, v); i >= 0 {
		return uint64(i), nil // stp_mode's names; a port's state is a number
	}
	switch spec.Wire {
	case BridgeWireOnOff:
		return map[string]uint64{"on": 1, "off": 0}[v], nil
	case BridgeWireBE16:
		return map[string]uint64{bridgeProto8021Q: 0x8100, bridgeProto8021ad: 0x88a8}[v], nil
	case BridgeWireMAC:
		mac, err := net.ParseMAC(v)
		if err != nil {
			t.Fatal(err)
		}
		return 0, mac
	default:
		n, err := strconv.ParseUint(v, 0, 64)
		if err != nil {
			t.Fatal(err)
		}
		return n, nil
	}
}

const (
	bridgeProto8021Q  = "802.1Q"
	bridgeProto8021ad = "802.1ad"
)

// bridgeCorpusOptions is BRIDGE_OPTIONS in kind-write-corpus.exp: each
// row add_<option> is `ip link add br0 type bridge <option> <value>`.
var bridgeCorpusOptions = [][2]string{
	{"forward_delay", "400"}, {"hello_time", "100"}, {"max_age", "1000"},
	{BridgeKeywordAgeingTime, "15000"}, {"stp_state", "1"}, {"priority", "100"},
	{"vlan_filtering", "1"}, {"vlan_protocol", bridgeProto8021ad},
	{"group_fwd_mask", "8"}, {"group_address", "01:80:c2:00:00:0e"},
	{"no_linklocal_learn", "1"}, {"mst_enabled", "1"},
	{"fdb_max_learned", "100"}, {"vlan_default_pvid", "10"},
	{"vlan_stats_enabled", "1"}, {"vlan_stats_per_port", "1"},
	{"mcast_router", "2"}, {"mcast_snooping", "0"},
	{"mcast_vlan_snooping", "1"}, {"mcast_query_use_ifaddr", "1"},
	{"mcast_querier", "1"}, {"mcast_hash_elasticity", "8"},
	{"mcast_hash_max", "1024"}, {"mcast_last_member_count", "3"},
	{"mcast_startup_query_count", "3"}, {"mcast_last_member_interval", "200"},
	{"mcast_membership_interval", "30000"}, {"mcast_querier_interval", "30000"},
	{"mcast_query_interval", "15000"}, {"mcast_query_response_interval", "500"},
	{"mcast_startup_query_interval", "1000"}, {"mcast_stats_enabled", "1"},
	{"mcast_igmp_version", "3"}, {"mcast_mld_version", "2"},
	{"nf_call_iptables", "1"}, {"nf_call_ip6tables", "1"},
	{"nf_call_arptables", "1"}, {"mdb_offload_fail_notification", "1"},
	{"fdb_local_vlan_0", "1"},
}

// bridgeCorpusWords maps every kinds/bridge row that sends a request to its
// words, with keywords spelled out.
func bridgeCorpusWords() map[string]string {
	m := map[string]string{
		"add_default":                     "",
		"add_fdb_flush":                   "fdb_flush",
		"add_stp_mode_auto":               "stp_mode auto",
		"add_stp_mode_user":               "stp_mode user",
		"add_stp_mode_kernel":             "stp_mode kernel",
		"add_boolopts":                    "mst_enabled 1 mdb_offload_fail_notification 1 fdb_local_vlan_0 1",
		"add_several":                     "stp_state 1 priority 100 vlan_filtering 1 mcast_snooping 0",
		"set_" + BridgeKeywordAgeingTime:  BridgeKeywordAgeingTime + " 15000",
		"set_vlan_filtering":              "vlan_filtering 1",
		"set_stp_mode":                    "stp_mode user",
		"set_stp_mode_busy":               "stp_mode user",
		"add_abbrev":                      "forward_delay 400", // argv `forward_d 400`
		"add_group_address_bad":           "group_address 01:80:c2:00:00:01",
		"add_group_fwd_mask_bad":          "group_fwd_mask 1",
		"add_igmp_version_bad":            "mcast_igmp_version 4",
		"add_mcast_vlan_snooping_no_vlan": "mcast_vlan_snooping 1",
	}
	for _, o := range bridgeCorpusOptions {
		w := o[0] + " " + o[1]
		if o[0] == "mcast_vlan_snooping" {
			w = "vlan_filtering 1 " + w
		}
		m["add_"+o[0]] = w
	}
	return m
}

// bridgeView renders b as `ip -d -j link show` prints its info_data, with
// json.Unmarshal's types: numbers float64, strings, nested objects. It is
// the test's oracle against the captured readbacks, not goip's renderer.
func bridgeView(b *BridgeInfo) map[string]any {
	out := map[string]any{}
	for i := range BridgeOptions {
		s := &BridgeOptions[i]
		if v, ok := bridgeViewValue(b, s); ok {
			out[s.JSON] = v
		}
	}
	if q := b.QuerierState; q != nil {
		out[bridgeMcastQuerierStateCst+"_ipv4"] = bridgeQuerierView("ipv4", q.IPv4Addr.IsValid(), q.IPv4Addr.String(), q.IPv4Port, q.IPv4OtherTimer)
		out[bridgeMcastQuerierStateCst+"_ipv6"] = bridgeQuerierView("ipv6", q.IPv6Addr.IsValid(), q.IPv6Addr.String(), q.IPv6Port, q.IPv6OtherTimer)
	}
	return out
}

// bridgeQuerierView is one of bridge_print_mcast_querier_state's objects.
// No capture has an other_timer, whose sprint_time64 text is KG1's; it is
// rendered as its number so a capture that has one fails here.
func bridgeQuerierView(fam string, hasAddr bool, addr string, port U32Attr, timer BridgeValue) map[string]any {
	o := map[string]any{}
	if hasAddr {
		o["mcast_querier_"+fam+"_addr"] = addr
	}
	if port.Present {
		o["mcast_querier_"+fam+"_port"] = float64(port.Value)
	}
	if timer.Present {
		o["mcast_querier_"+fam+"_other_timer"] = float64(timer.U)
	}
	return o
}

func bridgeViewValue(b *BridgeInfo, s *BridgeOptionSpec) (any, bool) {
	switch s.Wire {
	case BridgeWireBoolOpt:
		on, present := b.BoolOpts.Has(s.Bit)
		if on {
			return float64(1), present
		}
		return float64(0), present
	case BridgeWireMAC:
		return net.HardwareAddr(b.GroupAddr).String(), b.GroupAddr != nil
	case BridgeWireID:
		id := b.RootID
		if s.ID == unix.IFLA_BR_BRIDGE_ID {
			id = b.BridgeID
		}
		if id == nil {
			return nil, false
		}
		return bridgeIDString(id), true
	case BridgeWireQuerier, BridgeWireFlag:
		return nil, false
	default:
		return bridgeViewScalar(b, s)
	}
}

func bridgeViewScalar(b *BridgeInfo, s *BridgeOptionSpec) (any, bool) {
	v := b.Value(s.ID)
	if !v.Present {
		return nil, false
	}
	switch {
	case s.Names != nil:
		if v.U < uint64(len(s.Names)) {
			return s.Names[v.U], true
		}
		return "(unknown)", true
	case s.Wire == BridgeWireBE16:
		return map[uint64]string{0x8100: bridgeProto8021Q, 0x88a8: bridgeProto8021ad}[v.U], true
	case s.Wire == BridgeWireTimer:
		return float64(v.U) / 100, true // USER_HZ
	case s.Hex:
		return bridgeHex(v.U), true
	}
	return float64(v.U), true
}

// TestBridgeDumpMatchesReadback is TestParseBridgeInfoData's every-row row,
// one subtest per row: the decoded dump, rendered, is the readback. The
// dump is the one the JSON readback printed, from its own pcap, since the
// timers move between the text and JSON runs.
func TestBridgeDumpMatchesReadback(t *testing.T) {
	rows := bridgeKind.accepted(t)
	if len(rows) != 50 {
		t.Fatalf("%d accepted bridge rows, want 50", len(rows))
	}
	for _, slug := range rows {
		t.Run("positive: "+slug, func(t *testing.T) {
			got := bridgeView(ParseBridgeInfoData(bridgeKind.dump(t, slug, "netlink_route_getlink_detail_json.pcap")))
			expected := bridgeKind.readback(t, slug)
			if !reflect.DeepEqual(got, expected) {
				for k := range expected {
					if !reflect.DeepEqual(got[k], expected[k]) {
						t.Errorf("%s: got %v, want %v", k, got[k], expected[k])
					}
				}
				for k := range got {
					if _, ok := expected[k]; !ok {
						t.Errorf("%s: got %v, not in the readback", k, got[k])
					}
				}
			}
		})
	}
}

// bridgeAttr is one built attribute.
func bridgeAttr(atype uint16, payload ...byte) []byte {
	return rtattr(atype, payload)
}

func TestParseBridgeInfoData(t *testing.T) {
	def := bridgeKind.dump(t, "add_default", "netlink_route_getlink_detail.pcap")
	querier := bridgeKind.dump(t, "add_mcast_querier", "netlink_route_getlink_detail.pcap")
	tests := []struct {
		description string
		nest        []byte
		// expected holds the bridgeView keys to check; a nil value is a key
		// that must be absent. nilInfo expects ParseBridgeInfoData's nil.
		expected map[string]any
		unknown  []uint16
		nilInfo  bool
	}{
		{
			description: "positive: a bare bridge's dump",
			nest:        def,
			expected: map[string]any{
				"forward_delay": float64(1500), "hello_time": float64(200), "max_age": float64(2000),
				BridgeKeywordAgeingTime: float64(30000), "priority": float64(32768), "stp_mode": "auto",
				"vlan_protocol": bridgeProto8021Q, "group_addr": "01:80:c2:00:00:00",
				"mcast_hash_max": float64(4096), "bridge_id": "8000.0:0:0:0:0:0",
				"mst_enabled": float64(0), "fdb_local_vlan_0": float64(0),
			},
			unknown: nil,
		},
		{
			description: "positive: several options",
			nest:        bridgeKind.dump(t, "add_several", "netlink_route_getlink_detail.pcap"),
			expected: map[string]any{
				"stp_state": float64(1), "priority": float64(100), "vlan_filtering": float64(1),
				"mcast_snooping": float64(0), "bridge_id": "0064.0:0:0:0:0:0",
			},
		},
		{
			description: "positive: the boolopts",
			nest:        bridgeKind.dump(t, "add_boolopts", "netlink_route_getlink_detail.pcap"),
			expected: map[string]any{
				"mst_enabled": float64(1), "mdb_offload_fail_notification": float64(1),
				"fdb_local_vlan_0": float64(1), "no_linklocal_learn": float64(0), "mcast_vlan_snooping": float64(0),
			},
		},
		{
			description: "positive: 802.1ad",
			nest:        bridgeKind.dump(t, "add_vlan_protocol", "netlink_route_getlink_detail.pcap"),
			expected:    map[string]any{"vlan_protocol": bridgeProto8021ad},
		},
		{
			description: "negative: a nil nest",
			nest:        nil,
			nilInfo:     true,
		},
		{
			description: "negative: a u32 attribute of 2 bytes",
			nest:        bridgeAttr(unix.IFLA_BR_FORWARD_DELAY, 0xdc, 0x05),
			expected:    map[string]any{"forward_delay": nil},
		},
		{
			description: "boundary: an empty nest",
			nest:        []byte{},
			expected:    map[string]any{"forward_delay": nil, "stp_mode": nil, "bridge_id": nil},
		},
		{
			description: "boundary: the highest id",
			nest:        bridgeAttr(IflaBrStpMode, 2, 0, 0, 0),
			expected:    map[string]any{"stp_mode": "kernel"},
		},
		{
			description: "corner: vlan_protocol is big-endian",
			nest:        bridgeAttr(unix.IFLA_BR_VLAN_PROTOCOL, 0x81, 0x00),
			expected:    map[string]any{"vlan_protocol": bridgeProto8021Q},
		},
		{
			description: "corner: an id past IFLA_BR_STP_MODE is listed and the rest decoded",
			nest:        append(bridgeAttr(51, 1, 0, 0, 0), bridgeAttr(unix.IFLA_BR_STP_STATE, 1, 0, 0, 0)...),
			expected:    map[string]any{"stp_state": float64(1)},
			unknown:     []uint16{51},
		},
		{
			description: "corner: a truncated nest keeps the attributes before the cut",
			nest:        def[:50], // four 12-byte timers, then half of forward_delay
			expected:    map[string]any{"hello_timer": float64(0), "gc_timer": float64(0), "forward_delay": nil},
		},
		{
			description: "corner: a querier state nest",
			nest:        querier,
			expected: map[string]any{
				"mcast_querier_state_ipv4": map[string]any{"mcast_querier_ipv4_addr": "0.0.0.0"},
				"mcast_querier_state_ipv6": map[string]any{"mcast_querier_ipv6_addr": "::"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			b := ParseBridgeInfoData(tt.nest)
			if (b == nil) != tt.nilInfo {
				t.Fatalf("nil info: got %v, want %v", b == nil, tt.nilInfo)
			}
			if b == nil {
				return
			}
			view := bridgeView(b)
			for k, want := range tt.expected {
				got, ok := view[k]
				if want == nil && ok {
					t.Errorf("%s: got %v, want absent", k, got)
				}
				if want != nil && !reflect.DeepEqual(got, want) {
					t.Errorf("%s: got %v, want %v", k, got, want)
				}
			}
			if !slices.Equal(b.Unknown, tt.unknown) {
				t.Errorf("unknown: got %v, want %v", b.Unknown, tt.unknown)
			}
		})
	}
}

func TestEncodeBridgeInfoData(t *testing.T) {
	type row struct {
		description string
		settings    []BridgeSetting
		expected    []byte
		err         error
	}
	be := binary.LittleEndian
	tests := []row{
		{
			description: "positive: a bare type bridge has no attributes",
			settings:    nil,
			expected:    []byte{},
		},
		{
			description: "negative: a setting with no option row",
			settings:    []BridgeSetting{{}},
			err:         ErrBridgeSettingWire,
		},
		{
			description: "negative: a read-only option",
			settings:    []BridgeSetting{{Spec: bridgeKind.spec(t, "bridge_id")}},
			err:         ErrBridgeSettingWire,
		},
		{
			description: "negative: a value too wide for its wire",
			settings:    []BridgeSetting{{Spec: bridgeKind.spec(t, "priority"), Value: 65536}},
			err:         ErrBridgeSettingRange,
		},
		{
			description: "boundary: fdb_flush is a bare header",
			settings:    bridgeKind.args(t, "fdb_flush"),
			expected:    []byte{4, 0, unix.IFLA_BR_FDB_FLUSH, 0},
		},
		{
			description: "boundary: a u64 takes its full width",
			settings:    []BridgeSetting{{Spec: bridgeKind.spec(t, "mcast_query_interval"), Value: 1 << 63}},
			expected:    rtattr(unix.IFLA_BR_MCAST_QUERY_INTVL, be.AppendUint64(nil, 1<<63)),
		},
		{
			description: "corner: the boolopt goes last",
			settings:    bridgeKind.args(t, "mst_enabled 1 priority 100"),
			expected: append(rtattr(unix.IFLA_BR_PRIORITY, []byte{100, 0}),
				rtattr(unix.IFLA_BR_MULTI_BOOLOPT, []byte{4, 0, 0, 0, 4, 0, 0, 0})...),
		},
		{
			description: "corner: a later boolopt word clears the bit and keeps the mask",
			settings:    bridgeKind.args(t, "mst_enabled 1 mst_enabled 0"),
			expected:    rtattr(unix.IFLA_BR_MULTI_BOOLOPT, []byte{0, 0, 0, 0, 4, 0, 0, 0}),
		},
		{
			description: "corner: vlan_protocol in network order",
			settings:    bridgeKind.args(t, "vlan_protocol "+bridgeProto8021ad),
			expected:    rtattr(unix.IFLA_BR_VLAN_PROTOCOL, []byte{0x88, 0xa8}),
		},
	}
	words := bridgeCorpusWords()
	ents, err := os.ReadDir(tdLinkWrite_7_3_0 + "/kinds/bridge")
	if err != nil {
		t.Fatal(err)
	}
	sent := 0
	for _, e := range ents {
		data, _, ok := bridgeKind.request(t, e.Name())
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
			settings:    bridgeKind.args(t, w),
			expected:    append([]byte{}, data...),
		})
	}
	if sent != 55 {
		t.Errorf("%d bridge rows sent a request, want 55", sent)
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := EncodeBridgeInfoData(tt.settings)
			if err != tt.err {
				t.Fatalf("err: got %v, want %v", err, tt.err)
			}
			if err == nil && !bytes.Equal(got, tt.expected) {
				t.Errorf("got % x, want % x", got, tt.expected)
			}
		})
	}
}

func BenchmarkParseBridgeInfoData(b *testing.B) {
	nest := bridgeKind.dump(b, "add_default", "netlink_route_getlink_detail.pcap")
	b.ReportAllocs()
	for b.Loop() {
		if ParseBridgeInfoData(nest) == nil {
			b.Fatal("nil")
		}
	}
}

// bridgeHex is C's %#llx, which prints 0 without the 0x.
func bridgeHex(v uint64) string {
	if v == 0 {
		return "0"
	}
	return fmt.Sprintf("%#x", v)
}

// bridgeIDString is br_dump_bridge_id: the priority's two bytes, then
// ether_ntoa's unpadded address.
func bridgeIDString(id *BridgeID) string {
	a := id.Addr
	return fmt.Sprintf("%02x%02x.%x:%x:%x:%x:%x:%x", id.Prio[0], id.Prio[1], a[0], a[1], a[2], a[3], a[4], a[5])
}
