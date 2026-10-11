package render

import (
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The rows here cover what the link-write fixtures cannot reach: options
// 802.3ad does not send, values outside every name table, and the JSON
// quirks of bond_print_opt and bond_slave_print_opt. The captured states are
// compared end to end in internal/goip/goip_bond_test.go.

// nla is one netlink attribute, padded to four bytes.
func nla(typ uint16, payload []byte) []byte {
	b := make([]byte, 4, 4+len(payload)+3)
	binary.LittleEndian.PutUint16(b[0:2], uint16(4+len(payload))) //nolint:gosec // test payloads are tiny
	binary.LittleEndian.PutUint16(b[2:4], typ)
	b = append(b, payload...)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

func nlaU8(typ uint16, v uint8) []byte { return nla(typ, []byte{v}) }

func nlaU16(typ uint16, v uint16) []byte {
	return nla(typ, binary.LittleEndian.AppendUint16(nil, v))
}

func nlaU32(typ uint16, v uint32) []byte {
	return nla(typ, binary.LittleEndian.AppendUint32(nil, v))
}

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// TestBondDataRender is bond_print_opt on hand-built nests.
//
// go test ./internal/goip/render/ -run TestBondDataRender
func TestBondDataRender(t *testing.T) {
	names := fakeNames{3: {name: "la1"}, 4: {name: "la2"}}
	tests := []struct {
		description  string
		nest         []byte
		nilInfo      bool
		expected     string // text
		expectedJSON string
	}{
		{
			description:  "boundary: no IFLA_INFO_DATA prints nothing and is an empty object",
			nilInfo:      true,
			expected:     "",
			expectedJSON: `{}`,
		},
		{
			description:  "positive: mode by name, miimon as a number",
			nest:         cat(nlaU8(unix.IFLA_BOND_MODE, 4), nlaU32(unix.IFLA_BOND_MIIMON, 100)),
			expected:     "mode 802.3ad miimon 100 ",
			expectedJSON: `{"mode":"802.3ad","miimon":100}`,
		},
		{
			description:  "negative: a mode past the table is get_name's UNKNOWN",
			nest:         nlaU8(unix.IFLA_BOND_MODE, 7),
			expected:     "mode UNKNOWN ",
			expectedJSON: `{"mode":"UNKNOWN"}`,
		},
		{
			description:  "corner: arp_validate 0 is \"none\" to a terminal and null to -j",
			nest:         nlaU32(unix.IFLA_BOND_ARP_VALIDATE, 0),
			expected:     "arp_validate none ",
			expectedJSON: `{"arp_validate":null}`,
		},
		{
			description:  "positive: a nonzero arp_validate is a string in both",
			nest:         nlaU32(unix.IFLA_BOND_ARP_VALIDATE, 3),
			expected:     "arp_validate all ",
			expectedJSON: `{"arp_validate":"all"}`,
		},
		{
			description:  "corner: coupled_control is print_on_off, so 2 is on and JSON is a bool",
			nest:         cat(nlaU8(unix.IFLA_BOND_COUPLED_CONTROL, 2), nlaU8(xtcpnl.IflaBondBroadcastNeigh, 0)),
			expected:     "coupled_control on broadcast_neighbor off ",
			expectedJSON: `{"coupled_control":true,"broadcast_neighbor":false}`,
		},
		{
			description:  "corner: lacp_active has the same names but goes through get_name, so 2 is UNKNOWN",
			nest:         nlaU8(unix.IFLA_BOND_AD_LACP_ACTIVE, 2),
			expected:     "lacp_active UNKNOWN ",
			expectedJSON: `{"ad_lacp_active":"UNKNOWN"}`,
		},
		{
			description:  "positive: three JSON keys that are not the keyword",
			nest:         cat(nlaU8(unix.IFLA_BOND_NUM_PEER_NOTIF, 1), nlaU8(unix.IFLA_BOND_AD_LACP_ACTIVE, 1), nlaU8(unix.IFLA_BOND_AD_LACP_RATE, 1)),
			expected:     "num_grat_arp 1 lacp_active on lacp_rate fast ",
			expectedJSON: `{"num_peer_notif":1,"ad_lacp_active":"on","ad_lacp_rate":"fast"}`,
		},
		{
			description:  "positive: active_slave and primary resolve through the name table",
			nest:         cat(nlaU32(unix.IFLA_BOND_ACTIVE_SLAVE, 3), nlaU32(unix.IFLA_BOND_PRIMARY, 4)),
			expected:     "active_slave la1 primary la2 ",
			expectedJSON: `{"active_slave":"la1","primary":"la2"}`,
		},
		{
			description:  "boundary: an index of 0 prints nothing at all",
			nest:         cat(nlaU32(unix.IFLA_BOND_ACTIVE_SLAVE, 0), nlaU32(unix.IFLA_BOND_PRIMARY, 0)),
			expected:     "",
			expectedJSON: `{}`,
		},
		{
			description:  "negative: an uncached index is ll_index_to_name's if%u",
			nest:         nlaU32(unix.IFLA_BOND_ACTIVE_SLAVE, 9),
			expected:     "active_slave if9 ",
			expectedJSON: `{"active_slave":"if9"}`,
		},
		{
			description: "positive: arp_ip_target joins with commas and is a JSON array",
			nest: nla(unix.IFLA_BOND_ARP_IP_TARGET, cat(
				nla(0, []byte{192, 0, 2, 1}), nla(1, []byte{192, 0, 2, 2}))),
			expected:     "arp_ip_target 192.0.2.1,192.0.2.2 ",
			expectedJSON: `{"arp_ip_target":["192.0.2.1","192.0.2.2"]}`,
		},
		{
			description:  "positive: one ns_ip6_target",
			nest:         nla(unix.IFLA_BOND_NS_IP6_TARGET, nla(0, []byte{0x20, 0x01, 0x0d, 0xb8, 15: 1})),
			expected:     "ns_ip6_target 2001:db8::1 ",
			expectedJSON: `{"ns_ip6_target":["2001:db8::1"]}`,
		},
		{
			description:  "corner: lacp_strict is not printed by the pinned iproute2",
			nest:         cat(nlaU8(unix.IFLA_BOND_ALL_SLAVES_ACTIVE, 0), nlaU8(xtcpnl.IflaBondLacpStrict, 1), nlaU32(unix.IFLA_BOND_MIN_LINKS, 0)),
			expected:     "all_slaves_active 0 min_links 0 ",
			expectedJSON: `{"all_slaves_active":0,"min_links":0}`,
		},
		{
			description: "positive: ad_info is a nested object whose keys are not its keywords",
			nest: nla(unix.IFLA_BOND_AD_INFO, cat(
				nlaU16(unix.IFLA_BOND_AD_INFO_AGGREGATOR, 1),
				nlaU16(unix.IFLA_BOND_AD_INFO_PARTNER_KEY, 15),
				nla(unix.IFLA_BOND_AD_INFO_PARTNER_MAC, []byte{2, 0, 0, 0, 0, 0x0b}))),
			expected:     "ad_aggregator 1 ad_partner_key 15 ad_partner_mac 02:00:00:00:00:0b ",
			expectedJSON: `{"ad_info":{"aggregator":1,"partner_key":15,"partner_mac":"02:00:00:00:00:0b"}}`,
		},
		{
			description:  "positive: ad_actor_system and the u16 priorities",
			nest:         cat(nlaU16(unix.IFLA_BOND_AD_ACTOR_SYS_PRIO, 65535), nla(unix.IFLA_BOND_AD_ACTOR_SYSTEM, []byte{2, 0, 0, 0, 0, 0x0a})),
			expected:     "ad_actor_sys_prio 65535 ad_actor_system 02:00:00:00:00:0a ",
			expectedJSON: `{"ad_actor_sys_prio":65535,"ad_actor_system":"02:00:00:00:00:0a"}`,
		},
		{
			description:  "corner: print order is bond_print_opt's, not the order on the wire",
			nest:         cat(nlaU8(unix.IFLA_BOND_TLB_DYNAMIC_LB, 1), nlaU8(unix.IFLA_BOND_MODE, 0)),
			expected:     "mode balance-rr tlb_dynamic_lb 1 ",
			expectedJSON: `{"mode":"balance-rr","tlb_dynamic_lb":1}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var b *xtcpnl.BondInfo
			if !tc.nilInfo {
				b = xtcpnl.ParseBondInfoData(tc.nest)
			}
			k := bondData(b, names)
			if got := k.Text(); got != tc.expected {
				t.Errorf("text = %q, want %q", got, tc.expected)
			}
			got, err := json.Marshal(k)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.expectedJSON {
				t.Errorf("json = %s, want %s", got, tc.expectedJSON)
			}
		})
	}
}

// TestBondSlaveDataRender is bond_slave_print_opt on hand-built values.
//
// go test ./internal/goip/render/ -run TestBondSlaveDataRender
func TestBondSlaveDataRender(t *testing.T) {
	u8 := func(v uint8) xtcpnl.U8Attr { return xtcpnl.U8Attr{Value: v, Present: true} }
	u16 := func(v uint16) xtcpnl.U16Attr { return xtcpnl.U16Attr{Value: v, Present: true} }
	tests := []struct {
		description  string
		slave        *xtcpnl.BondSlaveInfo
		expected     string
		expectedJSON string
	}{
		{
			description:  "boundary: no IFLA_INFO_SLAVE_DATA is an empty object",
			slave:        nil,
			expected:     "",
			expectedJSON: `{}`,
		},
		{
			description:  "positive: state and mii_status by name",
			slave:        &xtcpnl.BondSlaveInfo{State: u8(1), MiiStatus: u8(2)},
			expected:     "state BACKUP mii_status DOWN ",
			expectedJSON: `{"state":"BACKUP","mii_status":"DOWN"}`,
		},
		{
			description:  "negative: out-of-table state and mii_status move to the _index keys",
			slave:        &xtcpnl.BondSlaveInfo{State: u8(5), MiiStatus: u8(9)},
			expected:     "state 5 mii_status 9 ",
			expectedJSON: `{"state_index":5,"mii_status_index":9}`,
		},
		{
			description:  "corner: link_failure_count is print_int of a u32, so 2^32-1 prints -1",
			slave:        &xtcpnl.BondSlaveInfo{LinkFailureCount: xtcpnl.U32Attr{Value: 0xffffffff, Present: true}},
			expected:     "link_failure_count -1 ",
			expectedJSON: `{"link_failure_count":-1}`,
		},
		{
			description:  "corner: prio is an s32",
			slave:        &xtcpnl.BondSlaveInfo{Prio: xtcpnl.U32Attr{Value: 0xfffffffe, Present: true}},
			expected:     "prio -2 ",
			expectedJSON: `{"prio":-2}`,
		},
		{
			description:  "boundary: state 0 is an empty <> list and an empty JSON array, not null",
			slave:        &xtcpnl.BondSlaveInfo{AdPartnerOperState: u16(0)},
			expected:     "ad_partner_oper_port_state 0 ad_partner_oper_port_state_str <> ",
			expectedJSON: `{"ad_partner_oper_port_state":0,"ad_partner_oper_port_state_str":[]}`,
		},
		{
			description: "positive: 0x4f, the partnerless active port",
			slave:       &xtcpnl.BondSlaveInfo{AdActorOperState: u8(0x4f)},
			expected: "ad_actor_oper_port_state 79 ad_actor_oper_port_state_str " +
				"<active,short_timeout,aggregating,in_sync,defaulted> ",
			expectedJSON: `{"ad_actor_oper_port_state":79,"ad_actor_oper_port_state_str":` +
				`["active","short_timeout","aggregating","in_sync","defaulted"]}`,
		},
		{
			description:  "boundary: 0x80 alone, the last bit, has no comma",
			slave:        &xtcpnl.BondSlaveInfo{AdActorOperState: u8(0x80)},
			expected:     "ad_actor_oper_port_state 128 ad_actor_oper_port_state_str <expired> ",
			expectedJSON: `{"ad_actor_oper_port_state":128,"ad_actor_oper_port_state_str":["expired"]}`,
		},
		{
			description: "corner: a partner bit above 0xff leaves a trailing comma, as _PF does",
			slave:       &xtcpnl.BondSlaveInfo{AdPartnerOperState: u16(0x101)},
			expected:    "ad_partner_oper_port_state 257 ad_partner_oper_port_state_str <active,> ",
			expectedJSON: `{"ad_partner_oper_port_state":257,` +
				`"ad_partner_oper_port_state_str":["active"]}`,
		},
		{
			description:  "negative: an out-of-table churn state prints the number under the same key",
			slave:        &xtcpnl.BondSlaveInfo{AdChurnActorState: u8(2), AdChurnPartnerState: u8(7)},
			expected:     "churn_actor_state NO_CHURN churn_partner_state 7 ",
			expectedJSON: `{"churn_actor_state":"NO_CHURN","churn_partner_state":7}`,
		},
		{
			description:  "positive: perm_hwaddr, queue_id, aggregator id and port prio",
			slave:        &xtcpnl.BondSlaveInfo{PermHwaddr: []byte{2, 0, 0, 0, 0x0a, 1}, QueueID: u16(0), AdAggregatorID: u16(1), ActorPortPrio: u16(255)},
			expected:     "perm_hwaddr 02:00:00:00:0a:01 queue_id 0 ad_aggregator_id 1 actor_port_prio 255 ",
			expectedJSON: `{"perm_hwaddr":"02:00:00:00:0a:01","queue_id":0,"ad_aggregator_id":1,"actor_port_prio":255}`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			k := bondSlaveData(tc.slave)
			if got := k.Text(); got != tc.expected {
				t.Errorf("text = %q, want %q", got, tc.expected)
			}
			got, err := json.Marshal(k)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.expectedJSON {
				t.Errorf("json = %s, want %s", got, tc.expectedJSON)
			}
		})
	}
}

// TestBondRenderCoversOptionTable pins the render side of the shared option
// table: every row is printed, or is listed as not printed by the pinned
// iproute2. A row added to xtcpnl.BondOptions with no decision here fails.
//
// go test ./internal/goip/render/ -run TestBondRenderCoversOptionTable
func TestBondRenderCoversOptionTable(t *testing.T) {
	for i := range xtcpnl.BondOptions {
		spec := &xtcpnl.BondOptions[i]
		t.Run(spec.Keyword, func(t *testing.T) {
			var payload []byte
			switch spec.Wire {
			case xtcpnl.BondWireU8:
				payload = []byte{1}
			case xtcpnl.BondWireU16:
				payload = []byte{1, 0}
			case xtcpnl.BondWireU32:
				payload = []byte{1, 0, 0, 0}
			case xtcpnl.BondWireMAC:
				payload = []byte{2, 0, 0, 0, 0, 1}
			case xtcpnl.BondWireIPv4Set:
				payload = nla(0, []byte{192, 0, 2, 1})
			case xtcpnl.BondWireIPv6Set:
				payload = nla(0, []byte{0x20, 0x01, 0x0d, 0xb8, 15: 1})
			case xtcpnl.BondWireNest:
				payload = nlaU16(unix.IFLA_BOND_AD_INFO_AGGREGATOR, 1)
			default:
				t.Fatalf("wire %d has no payload here; BondOptions should not use it", spec.Wire)
			}
			k := bondData(xtcpnl.ParseBondInfoData(nla(spec.ID, payload)), fakeNames{1: {name: "p1"}})
			expected := !bondNotPrinted[spec.ID]
			if got := k.Text() != ""; got != expected {
				t.Errorf("printed = %v, want %v (text %q)", got, expected, k.Text())
			}
		})
	}
}

// TestKindDataJSON pins the JSON writer itself.
//
// go test ./internal/goip/render/ -run TestKindDataJSON
func TestKindDataJSON(t *testing.T) {
	nested := &KindData{}
	nested.add("x", "x 1 ", 1)
	tests := []struct {
		description string
		k           *KindData
		expected    string
	}{
		{description: "boundary: no calls is {}", k: &KindData{}, expected: `{}`},
		{
			description: "positive: call order is member order, not key order",
			k:           &KindData{calls: []printCall{{key: "z", value: 1}, {key: "a", value: "s"}}},
			expected:    `{"z":1,"a":"s"}`,
		},
		{
			description: "corner: a nil value is print_null",
			k:           &KindData{calls: []printCall{{key: "n", value: nil}}},
			expected:    `{"n":null}`,
		},
		{
			description: "positive: a nested object",
			k:           &KindData{calls: []printCall{{key: "o", value: nested}}},
			expected:    `{"o":{"x":1}}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := json.Marshal(tc.k)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.expected {
				t.Errorf("got %s, want %s", got, tc.expected)
			}
		})
	}
}
