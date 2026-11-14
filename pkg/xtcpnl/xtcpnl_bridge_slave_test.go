package xtcpnl

import (
	"bytes"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// KP1-T02: the bridge port codec against the KC2-T02 rows of the 7.3.0 kind
// corpus (docs/netlink/goip-link-kinds.md, "KP1-T02 The bridge_slave
// parser").

// bridgeSlaveCorpusOptions is SLAVE_OPTIONS in kind-write-corpus.exp: each
// row set_<option> is `ip link set dev p0 type bridge_slave <option>
// <value>`.
var bridgeSlaveCorpusOptions = [][2]string{
	{"state", "4"}, {"priority", "7"}, {"cost", "10"}, {"hairpin", "on"},
	{"guard", "on"}, {"root_block", "on"}, {"fastleave", "on"},
	{"learning", "off"}, {"flood", "off"}, {"mcast_flood", "off"},
	{"bcast_flood", "off"}, {"mcast_to_unicast", "on"}, {"proxy_arp", "on"},
	{"proxy_arp_wifi", "on"}, {"mcast_router", "2"},
	{"mcast_fast_leave", "on"}, {"neigh_suppress", "on"},
	{"neigh_vlan_suppress", "on"}, {"group_fwd_mask", "8"},
	{"vlan_tunnel", "on"}, {"isolated", "on"}, {"locked", "on"},
	{"mab", "on"}, {"backup_nhid", "5"},
}

// bridgeSlaveCorpusWords maps every kinds/bridge_slave row that sends a
// request to its words, keywords spelled out and ports by ifindex: p0 is 4
// and p1 is 5 in every row's namespace.
func bridgeSlaveCorpusWords() map[string]string {
	m := map[string]string{
		"set_fdb_flush":          "fdb_flush",
		"set_nobackup_port":      "backup_port 0", // argv `nobackup_port`
		"set_neigh_forward_grat": "neigh_forward_grat on",
		"set_backup_port":        "backup_port 5", // argv `backup_port p1`
		"set_several":            "cost 10 priority 7 learning off",
		"set_abbrev":             "cost 10", // argv `co 10`
		"set_state_bad":          "state 5",
		"set_priority_over":      "priority 64",
		"set_cost_zero":          "cost 0",
		"set_group_fwd_mask_bad": "group_fwd_mask 2",
		"set_mab_unlocked":       "mab on",
		"set_backup_port_self":   "backup_port 4", // argv `backup_port p0`
		"set_not_enslaved":       "cost 10",
	}
	for _, o := range bridgeSlaveCorpusOptions {
		w := o[0] + " " + o[1]
		if o[0] == "mab" {
			w = "locked on " + w
		}
		m["set_"+o[0]] = w
	}
	return m
}

// bridgeFwdMaskNames is fwd_mask_tbl in iplink_bridge_slave.c.
var bridgeFwdMaskNames = map[int]string{0: "stp", 2: "lacp", 14: "lldp"}

// bridgeFwdMaskString is _bitmask2str over fwd_mask_tbl: the set bits'
// names or 0x values, comma-separated, or 0x0.
func bridgeFwdMaskString(v uint64) string {
	var parts []string
	for i := 0; i < 16; i++ {
		if v&(1<<i) == 0 {
			continue
		}
		if n, ok := bridgeFwdMaskNames[i]; ok {
			parts = append(parts, n)
		} else {
			parts = append(parts, bridgeHex(1<<i))
		}
	}
	if parts == nil {
		return "0x0"
	}
	return strings.Join(parts, ",")
}

// bridgeSlaveView renders s as `ip -d -j link show` prints its
// info_slave_data, with json.Unmarshal's types. names resolves backup_port
// as ll_index_to_name does; an ifindex it lacks renders as its number.
func bridgeSlaveView(s *BridgeSlaveInfo, names map[uint64]string) map[string]any {
	out := map[string]any{}
	for i := range BridgeSlaveOptions {
		spec := &BridgeSlaveOptions[i]
		if spec.JSON == "" {
			continue
		}
		if spec.Wire == BridgeWireID {
			id := s.RootID
			if spec.ID == unix.IFLA_BRPORT_BRIDGE_ID {
				id = s.BridgeID
			}
			if id != nil {
				out[spec.JSON] = bridgeIDString(id)
			}
			continue
		}
		v := s.Value(spec.ID)
		if !v.Present {
			continue
		}
		key, val := bridgeSlaveViewValue(spec, v.U, names)
		out[key] = val
		if spec.ID == unix.IFLA_BRPORT_GROUP_FWD_MASK {
			out["group_fwd_mask_str"] = bridgeFwdMaskString(v.U)
		}
	}
	return out
}

func bridgeSlaveViewValue(spec *BridgeOptionSpec, v uint64, names map[uint64]string) (string, any) {
	switch {
	case spec.Names != nil:
		if v < uint64(len(spec.Names)) {
			return spec.JSON, spec.Names[v]
		}
		return "state_index", float64(v)
	case spec.Hex:
		return spec.JSON, bridgeHex(v)
	case spec.Wire == BridgeWireOnOff:
		return spec.JSON, v != 0
	case spec.Wire == BridgeWireTimer:
		return spec.JSON, float64(v) / 100 // USER_HZ
	case spec.Wire == BridgeWireIfindex:
		if n, ok := names[v]; ok {
			return spec.JSON, n
		}
	}
	return spec.JSON, float64(v)
}

// TestBridgeSlaveDumpMatchesReadback is TestParseBridgeSlaveData's
// every-row row, one subtest per row, against the JSON readback's own pcap.
func TestBridgeSlaveDumpMatchesReadback(t *testing.T) {
	const pcap = "netlink_route_getlink_detail_json.pcap"
	rows := bridgeSlaveKind.accepted(t)
	if len(rows) != 30 {
		t.Fatalf("%d accepted bridge_slave rows, want 30", len(rows))
	}
	for _, slug := range rows {
		t.Run("positive: "+slug, func(t *testing.T) {
			got := bridgeSlaveView(ParseBridgeSlaveData(bridgeSlaveKind.dump(t, slug, pcap)), bridgeSlaveKind.names(t, slug, pcap))
			expected := bridgeSlaveKind.readback(t, slug)
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

func TestParseBridgeSlaveData(t *testing.T) {
	const pcap = "netlink_route_getlink_detail.pcap"
	several := bridgeSlaveKind.dump(t, "set_several", pcap)
	tests := []struct {
		description string
		nest        []byte
		names       map[uint64]string
		// expected holds the bridgeSlaveView keys to check; a nil value is
		// a key that must be absent. nilInfo expects ParseBridgeSlaveData's
		// nil.
		expected map[string]any
		unknown  []uint16
		nilInfo  bool
	}{
		{
			description: "positive: a port's dump",
			nest:        several,
			expected:    map[string]any{"cost": float64(10), "priority": float64(7), "learning": false, "flood": true, "state": "disabled"},
		},
		{
			description: "positive: the 7.2.0 attribute",
			nest:        bridgeSlaveKind.dump(t, "set_neigh_forward_grat", pcap),
			expected:    map[string]any{"neigh_forward_grat": true},
		},
		{
			description: "positive: a backup port is p1's ifindex",
			nest:        bridgeSlaveKind.dump(t, "set_backup_port", pcap),
			expected:    map[string]any{"backup_port": float64(5)},
		},
		{
			description: "positive: a backup port resolves to p1",
			nest:        bridgeSlaveKind.dump(t, "set_backup_port", pcap),
			names:       bridgeSlaveKind.names(t, "set_backup_port", pcap),
			expected:    map[string]any{"backup_port": "p1"},
		},
		{
			description: "negative: a nil nest",
			nest:        nil,
			nilInfo:     true,
		},
		{
			description: "negative: a u8 attribute of no bytes",
			nest:        rtattr(unix.IFLA_BRPORT_STATE, nil),
			expected:    map[string]any{"state": nil, "state_index": nil},
		},
		{
			description: "boundary: an empty nest",
			nest:        []byte{},
			expected:    map[string]any{"state": nil, "cost": nil, "bridge_id": nil},
		},
		{
			description: "boundary: the highest id",
			nest:        rtattr(IflaBrportNeighForwardGrat, []byte{1}),
			expected:    map[string]any{"neigh_forward_grat": true},
		},
		{
			description: "corner: the port id and number",
			nest:        several,
			expected:    map[string]any{"id": "0x1c01", "no": "0x1"},
		},
		{
			description: "corner: an id past IFLA_BRPORT_NEIGH_FORWARD_GRAT is listed and the rest decoded",
			nest:        append(rtattr(46, []byte{1}), rtattr(unix.IFLA_BRPORT_GUARD, []byte{1})...),
			expected:    map[string]any{"guard": true},
			unknown:     []uint16{46},
		},
		{
			description: "corner: a truncated nest keeps what came before the cut",
			nest:        several[:10], // state's 8 bytes, then half of priority
			expected:    map[string]any{"state": "disabled", "priority": nil},
		},
		{
			description: "corner: a state past blocking is its index",
			nest:        rtattr(unix.IFLA_BRPORT_STATE, []byte{5}),
			expected:    map[string]any{"state": nil, "state_index": float64(5)},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			s := ParseBridgeSlaveData(tt.nest)
			if (s == nil) != tt.nilInfo {
				t.Fatalf("nil info: got %v, want %v", s == nil, tt.nilInfo)
			}
			if s == nil {
				return
			}
			view := bridgeSlaveView(s, tt.names)
			for k, want := range tt.expected {
				got, ok := view[k]
				if want == nil && ok {
					t.Errorf("%s: got %v, want absent", k, got)
				}
				if want != nil && !reflect.DeepEqual(got, want) {
					t.Errorf("%s: got %v, want %v", k, got, want)
				}
			}
			if !slices.Equal(s.Unknown, tt.unknown) {
				t.Errorf("unknown: got %v, want %v", s.Unknown, tt.unknown)
			}
		})
	}
}

func TestEncodeBridgeSlaveData(t *testing.T) {
	type row struct {
		description string
		settings    []BridgeSetting
		expected    []byte
		err         error
	}
	k := bridgeSlaveKind
	tests := []row{
		{
			description: "positive: several in argument order",
			settings:    k.args(t, "cost 10 priority 7 learning off"),
			expected: slices.Concat(rtattr(unix.IFLA_BRPORT_COST, []byte{10, 0, 0, 0}),
				rtattr(unix.IFLA_BRPORT_PRIORITY, []byte{7, 0}), rtattr(unix.IFLA_BRPORT_LEARNING, []byte{0})),
		},
		{
			description: "negative: a value too wide for its wire",
			settings:    []BridgeSetting{{Spec: k.spec(t, "state"), Value: 256}},
			err:         ErrBridgeSettingRange,
		},
		{
			description: "negative: a read-only option",
			settings:    []BridgeSetting{{Spec: k.spec(t, "port_id")}},
			err:         ErrBridgeSettingWire,
		},
		{
			description: "negative: a bridge boolopt in a port write",
			settings:    bridgeKind.args(t, "mst_enabled 1"),
			err:         ErrBridgeSettingWire,
		},
		{
			description: "boundary: nobackup_port is backup_port 0",
			settings:    k.args(t, "backup_port 0"),
			expected:    rtattr(unix.IFLA_BRPORT_BACKUP_PORT, []byte{0, 0, 0, 0}),
		},
		{
			description: "boundary: fdb_flush is a bare header",
			settings:    k.args(t, "fdb_flush"),
			expected:    []byte{4, 0, unix.IFLA_BRPORT_FLUSH, 0},
		},
		{
			description: "corner: two keywords, one attribute",
			settings:    k.args(t, "fastleave on mcast_fast_leave off"),
			expected:    append(rtattr(unix.IFLA_BRPORT_FAST_LEAVE, []byte{1}), rtattr(unix.IFLA_BRPORT_FAST_LEAVE, []byte{0})...),
		},
	}
	words := bridgeSlaveCorpusWords()
	ents, err := os.ReadDir(tdLinkWrite_7_3_0 + "/kinds/bridge_slave")
	if err != nil {
		t.Fatal(err)
	}
	sent := 0
	for _, e := range ents {
		data, _, ok := k.request(t, e.Name())
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
			settings:    k.args(t, w),
			expected:    append([]byte{}, data...),
		})
	}
	if sent != 37 {
		t.Errorf("%d bridge_slave rows sent a request, want 37", sent)
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := EncodeBridgeSlaveData(tt.settings)
			if err != tt.err {
				t.Fatalf("err: got %v, want %v", err, tt.err)
			}
			if err == nil && !bytes.Equal(got, tt.expected) {
				t.Errorf("got % x, want % x", got, tt.expected)
			}
		})
	}
}

func BenchmarkParseBridgeSlaveData(b *testing.B) {
	nest := bridgeSlaveKind.dump(b, "set_several", "netlink_route_getlink_detail.pcap")
	b.ReportAllocs()
	for b.Loop() {
		if ParseBridgeSlaveData(nest) == nil {
			b.Fatal("nil")
		}
	}
}

// TestLinkInfoBridgeKinds decodes whole corpus answers through ParseNewLink,
// so the setLinkInfoNest branches are keyed on the right kind: Bridge on
// IFLA_INFO_KIND, BridgeSlave on IFLA_INFO_SLAVE_KIND.
func TestLinkInfoBridgeKinds(t *testing.T) {
	// link parses the longest RTM_NEWLINK answer in a row's -d readback:
	// the show's, not a name lookup's.
	link := func(t *testing.T, row string) LinkInfo {
		t.Helper()
		var body []byte
		for _, m := range pcapNlMsgs(t, tdLinkWrite_7_3_0+"/kinds/"+row+"/netlink_route_getlink_detail.pcap") {
			if m.h.Type == unix.RTM_NEWLINK && m.h.Flags&unix.NLM_F_REQUEST == 0 && len(m.body) > len(body) {
				body = m.body
			}
		}
		li, err := ParseNewLink(body)
		if err != nil {
			t.Fatal(err)
		}
		return li
	}
	tests := []struct {
		description string
		row         string
		// expected is the port's cost and the bridge's forward_delay, 0
		// for a nil decode.
		expected [2]uint64
		slave    bool
		bridge   bool
	}{
		{description: "positive: a port decodes BridgeSlave", row: "bridge_slave/set_several", expected: [2]uint64{10, 0}, slave: true},
		{description: "positive: a bridge decodes Bridge", row: "bridge/add_default", expected: [2]uint64{0, 1500}, bridge: true},
		{description: "negative: a dummy that is no port decodes neither", row: "bridge_slave/set_not_enslaved"},
		{description: "corner: a vrf port's slave data is not a bridge port's", row: "vrf_slave/set_master"},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			li := link(t, tt.row)
			if li.Name != "p0" && li.Name != "br0" && li.Name != "d0" {
				t.Fatalf("decoded %q, not the row's device", li.Name)
			}
			if (li.BridgeSlave != nil) != tt.slave || (li.Bridge != nil) != tt.bridge {
				t.Fatalf("BridgeSlave %v, Bridge %v; want %v, %v", li.BridgeSlave != nil, li.Bridge != nil, tt.slave, tt.bridge)
			}
			got := [2]uint64{li.BridgeSlave.Value(unix.IFLA_BRPORT_COST).U, li.Bridge.Value(unix.IFLA_BR_FORWARD_DELAY).U}
			if got != tt.expected {
				t.Errorf("got %v, want %v", got, tt.expected)
			}
		})
	}
}
