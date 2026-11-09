package render

import (
	"encoding/json"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The rows here cover what KC2's bridge_slave readbacks cannot reach: a
// state past the table, a mask with named and unnamed bits, a backup port
// the cache lacks, and the attributes 7.2.0 doesn't print. The captured
// rows are compared end to end in
// internal/goip/link_bridge_slave_detail_test.go.

func TestBridgeSlaveDataRender(t *testing.T) {
	names := fakeNames{5: {name: "p1"}}
	tests := []struct {
		description string
		nest        []byte
		text        string
		json        string
	}{
		{"positive: a named state and an on/off flag", cat(nlaU8(unix.IFLA_BRPORT_STATE, 3), nlaU8(unix.IFLA_BRPORT_MODE, 1)),
			"state forwarding hairpin on ", `{"state":"forwarding","hairpin":true}`},
		{"positive: a backup port by name", nlaU32(unix.IFLA_BRPORT_BACKUP_PORT, 5),
			"backup_port p1 ", `{"backup_port":"p1"}`},
		{"positive: fast leave twice, the second text only", nlaU8(unix.IFLA_BRPORT_FAST_LEAVE, 1),
			"fastleave on mcast_fast_leave on ", `{"fastleave":true}`},
		{"negative: no nest is an empty object", nil, "", `{}`},
		{"boundary: blocking, the last named state", nlaU8(unix.IFLA_BRPORT_STATE, 4),
			"state blocking ", `{"state":"blocking"}`},
		{"boundary: one past it", nlaU8(unix.IFLA_BRPORT_STATE, 5),
			"state (5) ", `{"state_index":5}`},
		{"corner: a mask's named and unnamed bits", nlaU16(unix.IFLA_BRPORT_GROUP_FWD_MASK, 1|1<<2|1<<3|1<<14),
			"group_fwd_mask 0x400d group_fwd_mask_str stp,lacp,0x8,lldp ",
			`{"group_fwd_mask":"0x400d","group_fwd_mask_str":"stp,lacp,0x8,lldp"}`},
		{"corner: a backup port the cache lacks", nlaU32(unix.IFLA_BRPORT_BACKUP_PORT, 9),
			"backup_port if9 ", `{"backup_port":"if9"}`},
		{"corner: the attributes 7.2.0 doesn't print", cat(nlaU8(unix.IFLA_BRPORT_MRP_RING_OPEN, 1),
			nlaU32(unix.IFLA_BRPORT_MCAST_N_GROUPS, 3)), "", `{}`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var s *xtcpnl.BridgeSlaveInfo
			if tc.nest != nil {
				s = xtcpnl.ParseBridgeSlaveData(tc.nest)
			}
			k := bridgeSlaveData(s, names)
			if got := k.Text(); got != tc.text {
				t.Errorf("text %q, want %q", got, tc.text)
			}
			j, err := json.Marshal(k)
			if err != nil {
				t.Fatal(err)
			}
			if string(j) != tc.json {
				t.Errorf("json %s, want %s", j, tc.json)
			}
		})
	}
}
