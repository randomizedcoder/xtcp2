package render

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestPrefixText is print_prefix's line: no space between the prefix and
// dev, the flags in their order, and the cache info's times.
//
// go test ./internal/goip/render/ -run TestPrefixText
func TestPrefixText(t *testing.T) {
	names := fakeNames{5: {name: "r0"}}
	addr := []byte{0x20, 0x01, 0x0d, 0xb8, 0, 5, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}
	tests := []struct {
		description string
		pi          xtcpnl.PrefixInfo
		expected    string
	}{
		{"positive: the captured RA's prefix", xtcpnl.PrefixInfo{Ifindex: 5, Len: 64, Flags: 3, Address: addr,
			HasCacheinfo: true, PreferredTime: 300, ValidTime: 600},
			"prefix 2001:db8:5::/64dev r0 onlink autoconf valid 600 preferred 300 \n"},
		{"positive: onlink alone, no cache info", xtcpnl.PrefixInfo{Ifindex: 5, Len: 48, Flags: 1, Address: addr},
			"prefix 2001:db8:5::/48dev r0 onlink \n"},
		{"boundary: autoconf alone, prefix length 0", xtcpnl.PrefixInfo{Ifindex: 5, Flags: 2, Address: addr},
			"prefix 2001:db8:5::/0dev r0 autoconf \n"},
		{"negative: no PREFIX_ADDRESS is no prefix", xtcpnl.PrefixInfo{Ifindex: 5, Len: 64},
			"dev r0 \n"},
		{"corner: an uncached index is ifN, other flag bits print nothing", xtcpnl.PrefixInfo{Ifindex: 9,
			Len: 64, Flags: 0xfc, Address: addr}, "prefix 2001:db8:5::/64dev if9 \n"},
		{"boundary: the most a u32 time holds", xtcpnl.PrefixInfo{Ifindex: 5, Len: 128, Address: addr,
			HasCacheinfo: true, PreferredTime: 0xffffffff, ValidTime: 0xffffffff},
			"prefix 2001:db8:5::/128dev r0 valid 4294967295 preferred 4294967295 \n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := PrefixText(&tc.pi, names); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestIfStatsHwSInfoText is ipstats_print's record with the hw_s_info
// subgroup alone, as the monitor prints it: the header, and l3_stats'
// request and used, each 0 or 1, or refused on stderr.
//
// go test ./internal/goip/render/ -run TestIfStatsHwSInfoText
func TestIfStatsHwSInfoText(t *testing.T) {
	names := fakeNames{4: {name: "d0"}}
	l3 := func(req, used uint8, hasReq, hasUsed bool) xtcpnl.IfStatsInfo {
		return xtcpnl.IfStatsInfo{Ifindex: 4, HasHwSInfo: true, HwSInfo: xtcpnl.HwSInfo{HasL3: true,
			L3Request: req, HasL3Request: hasReq, L3Used: used, HasL3Used: hasUsed}}
	}
	type result struct{ out, errOut string }
	tests := []struct {
		description string
		info        xtcpnl.IfStatsInfo
		expected    result
	}{
		{"positive: request on, used off", l3(1, 0, true, true), result{out: "4: d0:\n    l3_stats on used off\n"}},
		{"positive: request off, used on", l3(0, 1, true, true), result{out: "4: d0:\n    l3_stats off used on\n"}},
		{"boundary: no hw_s_info is the header", xtcpnl.IfStatsInfo{Ifindex: 4}, result{out: "4: d0:\n"}},
		{"boundary: hw_s_info with no l3_stats is a blank line", xtcpnl.IfStatsInfo{Ifindex: 4, HasHwSInfo: true},
			result{out: "4: d0:\n\n"}},
		{"corner: l3_stats with neither byte", l3(0, 0, false, false), result{out: "4: d0:\n    l3_stats\n"}},
		{"negative: a request of 2 stops the record after the header", l3(2, 0, true, true),
			result{out: "4: d0:\n", errOut: "Invalid value for request: expected 0 or 1, got 2.\n"}},
		{"negative: a used of 7 prints the request alone", l3(1, 7, true, true),
			result{out: "4: d0:\n    l3_stats on\n", errOut: "Invalid value for used: expected 0 or 1, got 7.\n"}},
		{"corner: an uncached index is ifN", xtcpnl.IfStatsInfo{Ifindex: 8}, result{out: "8: if8:\n"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			out, errOut := IfStatsHwSInfoText(&tc.info, names)
			if got := (result{out, errOut}); got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
		})
	}
}

// TestLinkEventToken is print_link_event: a name for a known IFLA_EVENT_*,
// "%d" past the table, nothing for NONE.
//
// go test ./internal/goip/render/ -run TestLinkEventToken
func TestLinkEventToken(t *testing.T) {
	tests := []struct {
		description string
		event       uint32
		expected    string
	}{
		{"positive: REBOOT", 1, "event REBOOT "},
		{"positive: FEATURE CHANGE, its name holding a space", 2, "event FEATURE CHANGE "},
		{"boundary: BONDING OPTION, the table's last", 6, "event BONDING OPTION "},
		{"boundary: one past the table is its number", 7, "event 7 "},
		{"negative: NONE prints nothing", 0, ""},
		{"corner: the top bit set prints negative, as %d does", 0xffffffff, "event -1 "},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := linkEventToken(tc.event); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}

// TestLinkViewMonitorTokens is the link line the monitor prints: the event
// after the group, and WithoutQlen's dropping of qlen.
//
// go test ./internal/goip/render/ -run TestLinkViewMonitorTokens
func TestLinkViewMonitorTokens(t *testing.T) {
	q := uint32(1000)
	li := func(event uint32, has bool) xtcpnl.LinkInfo {
		return xtcpnl.LinkInfo{Index: 4, Name: "d0", MTU: 1500, HasMTU: true, TxQLen: q, HasTxQLen: true,
			Event: event, HasEvent: has}
	}
	tests := []struct {
		description string
		li          xtcpnl.LinkInfo
		withoutQlen bool
		expected    string
	}{
		{"positive: qlen kept", li(0, false), false, "4: d0: <> mtu 1500 state UNKNOWN qlen 1000\n"},
		{"positive: WithoutQlen drops it", li(0, false), true, "4: d0: <> mtu 1500 state UNKNOWN \n"},
		{"positive: an event follows", li(1, true), true, "4: d0: <> mtu 1500 state UNKNOWN event REBOOT \n"},
		{"corner: an event after qlen, with no space between, as ip prints it", li(1, true), false,
			"4: d0: <> mtu 1500 state UNKNOWN qlen 1000event REBOOT \n"},
		{"negative: NONE prints nothing", li(0, true), true, "4: d0: <> mtu 1500 state UNKNOWN \n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v := LinkViewForAddr(tc.li, fakeNames{}, 2, false)
			if tc.withoutQlen {
				v = v.WithoutQlen()
			}
			if got := v.Text(); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}
