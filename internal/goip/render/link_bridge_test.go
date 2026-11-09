package render

import (
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The rows here cover what KC2's bridge readbacks cannot reach: values
// outside the name tables, an unnamed protocol, a timer past a second and
// the querier's other timer. The captured rows are compared end to end in
// internal/goip/link_bridge_detail_test.go.

func nlaU64(typ uint16, v uint64) []byte {
	return nla(typ, binary.LittleEndian.AppendUint64(nil, v))
}

func TestBridgeDataRender(t *testing.T) {
	querier := nla(unix.IFLA_BR_MCAST_QUERIER_STATE, cat(
		nla(xtcpnl.BridgeQuerierIPAddress, []byte{192, 0, 2, 1}),
		nlaU32(xtcpnl.BridgeQuerierIPPort, 4),
		nlaU64(xtcpnl.BridgeQuerierIPOtherTimer, 255_000_000),
	))
	tests := []struct {
		description string
		nest        []byte
		text        string
		json        string
	}{
		{"positive: a u32 and a u16", cat(nlaU32(unix.IFLA_BR_FORWARD_DELAY, 1500), nlaU16(unix.IFLA_BR_PRIORITY, 100)),
			"forward_delay 1500 priority 100 ", `{"forward_delay":1500,"priority":100}`},
		{"positive: a timer past a second", nlaU64(unix.IFLA_BR_GC_TIMER, 12345),
			"gc_timer  123.45 ", `{"gc_timer":123.45}`},
		{"positive: the querier's ipv4 object, the ipv6 one empty", querier,
			"mcast_querier_ipv4_addr 192.0.2.1 mcast_querier_ipv4_port 4 mcast_querier_ipv4_other_timer 255ms ",
			`{"mcast_querier_state_ipv4":{"mcast_querier_ipv4_addr":"192.0.2.1","mcast_querier_ipv4_port":4,` +
				`"mcast_querier_ipv4_other_timer":"255ms"},"mcast_querier_state_ipv6":{}}`},
		{"negative: no nest is an empty object", nil, "", `{}`},
		{"boundary: a timer under a hundredth", nlaU64(unix.IFLA_BR_HELLO_TIMER, 7),
			"hello_timer    0.07 ", `{"hello_timer":0.07}`},
		{"boundary: a nonzero mask is 0x-prefixed", nlaU16(unix.IFLA_BR_GROUP_FWD_MASK, 8),
			"group_fwd_mask 0x8 ", `{"group_fwd_mask":"0x8"}`},
		{"corner: an stp_mode past the table", nlaU32(xtcpnl.IflaBrStpMode, 7),
			"stp_mode (unknown) ", `{"stp_mode":"(unknown)"}`},
		{"corner: an unnamed vlan_protocol", nla(unix.IFLA_BR_VLAN_PROTOCOL, []byte{0x12, 0x34}),
			"vlan_protocol [4660] ", `{"vlan_protocol":"[4660]"}`},
		{"corner: only the boolopts in the mask print", nla(unix.IFLA_BR_MULTI_BOOLOPT,
			cat(binary.LittleEndian.AppendUint32(nil, 1<<xtcpnl.BrBoolOptMstEnable),
				binary.LittleEndian.AppendUint32(nil, 1<<xtcpnl.BrBoolOptMstEnable|1<<xtcpnl.BrBoolOptNoLLLearn))),
			"no_linklocal_learn 0 mst_enabled 1 ", `{"no_linklocal_learn":0,"mst_enabled":1}`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var b *xtcpnl.BridgeInfo
			if tc.nest != nil {
				b = xtcpnl.ParseBridgeInfoData(tc.nest)
			}
			k := bridgeData(b)
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

func TestSprintTime64(t *testing.T) {
	tests := []struct {
		description string
		ns          uint64
		expected    string
	}{
		{"positive: seconds", 2_500_000_000, "2.5s"},
		{"positive: milliseconds", 255_000_000, "255ms"},
		{"positive: microseconds", 1_500, "1.5us"},
		{"boundary: one second", 1_000_000_000, "1s"},
		{"boundary: under a microsecond", 999, "999ns"},
		{"corner: %.3g rounds to three digits", 123_456_789_000, "123s"},
		{"corner: %.3g turns exponential past three digits", 1_234_000_000_000, "1.23e+03s"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := sprintTime64(tc.ns); got != tc.expected {
				t.Errorf("sprintTime64(%d) = %q, want %q", tc.ns, got, tc.expected)
			}
		})
	}
}
