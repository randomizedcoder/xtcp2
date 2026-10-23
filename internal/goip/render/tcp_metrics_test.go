package render

import (
	"encoding/json"
	"net/netip"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// TestTCPMetricsView checks process_msg's line and JSON object for entries
// the captures do not hold: the msecs RTT alone, the usecs one winning
// either way, zero not printed, the TIME-WAIT pair, the Fast Open members
// and a cookie longer than 16 bytes, and an IPv6 address glibc prints in
// dotted quad.
//
// go test ./internal/goip/render/ -run TestTCPMetricsView
func TestTCPMetricsView(t *testing.T) {
	u32 := func(v uint32) *uint32 { return &v }
	u64 := func(v uint64) *uint64 { return &v }
	u16 := func(v uint16) *uint16 { return &v }
	s32 := func(v int32) *int32 { return &v }
	v4 := netip.MustParseAddr("192.0.2.1")
	src := netip.MustParseAddr("192.0.2.2")
	metrics := func(set map[int]uint32) (m [xtcpnl.TCPMetricCountCst]*uint32) {
		for i, v := range set {
			m[i] = u32(v)
		}
		return m
	}
	tests := []struct {
		description string
		m           xtcpnl.TCPMetrics
		text        string
		json        string
	}{
		{
			description: "positive: a captured entry, usecs RTT and cwnd",
			m: xtcpnl.TCPMetrics{Addr: v4, Saddr: src, Age: u64(692), HasVals: true,
				Metrics: metrics(map[int]uint32{xtcpnl.TCPMetricCwnd: 10, xtcpnl.TCPMetricRTTUs: 232, xtcpnl.TCPMetricRTTVarUs: 116})},
			text: "192.0.2.1 age 0.692sec cwnd 10 rtt 29us rttvar 29us source 192.0.2.2\n",
			json: `{"dst":"192.0.2.1","age":0.692,"cwnd":10,"rtt":2.9e-05,"rttvar":2.9e-05,"source":"192.0.2.2"}`,
		},
		{
			description: "positive: msecs alone, scaled by 1000 then shifted",
			m: xtcpnl.TCPMetrics{Addr: v4, HasVals: true,
				Metrics: metrics(map[int]uint32{xtcpnl.TCPMetricRTT: 16, xtcpnl.TCPMetricRTTVar: 8})},
			text: "192.0.2.1 rtt 2000us rttvar 2000us\n",
			json: `{"dst":"192.0.2.1","rtt":0.002,"rttvar":0.002}`,
		},
		{
			description: "positive: the three named metrics in index order",
			m: xtcpnl.TCPMetrics{Addr: v4, HasVals: true, Metrics: metrics(map[int]uint32{
				xtcpnl.TCPMetricReordering: 3, xtcpnl.TCPMetricSsthresh: 7, xtcpnl.TCPMetricCwnd: 10})},
			text: "192.0.2.1 ssthresh 7 cwnd 10 reordering 3\n",
			json: `{"dst":"192.0.2.1","ssthresh":7,"cwnd":10,"reordering":3}`,
		},
		{
			description: "boundary: usecs zero overrides msecs, and zero is not printed",
			m: xtcpnl.TCPMetrics{Addr: v4, HasVals: true,
				Metrics: metrics(map[int]uint32{xtcpnl.TCPMetricRTT: 16, xtcpnl.TCPMetricRTTUs: 0})},
			text: "192.0.2.1\n",
			json: `{"dst":"192.0.2.1"}`,
		},
		{
			description: "boundary: usecs 7, shifted to 0, is not printed",
			m:           xtcpnl.TCPMetrics{Addr: v4, HasVals: true, Metrics: metrics(map[int]uint32{xtcpnl.TCPMetricRTTUs: 7})},
			text:        "192.0.2.1\n",
			json:        `{"dst":"192.0.2.1"}`,
		},
		{
			description: "corner: metrics outside TCP_METRICS_ATTR_VALS are not read",
			m:           xtcpnl.TCPMetrics{Addr: v4, Metrics: metrics(map[int]uint32{xtcpnl.TCPMetricCwnd: 10})},
			text:        "192.0.2.1\n",
			json:        `{"dst":"192.0.2.1"}`,
		},
		{
			description: "positive: the TIME-WAIT pair, tsval 0 when absent",
			m:           xtcpnl.TCPMetrics{Addr: v4, TwTsStamp: s32(-5)},
			text:        "192.0.2.1 tw_ts 0/-5 ago\n",
			json:        `{"dst":"192.0.2.1","tw_ts_stamp":"0/-5"}`,
		},
		{
			description: "positive: Fast Open, ip's fopen_miss key, and a cookie cut at 16 bytes",
			m: xtcpnl.TCPMetrics{Addr: v4, FopenMSS: u16(1460), FopenSynDrops: u16(2), FopenSynDropTS: u64(1500000),
				FopenCookie: []byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}},
			text: "192.0.2.1 fo_mss 1460 fo_syn_drops 2/1.500usec ago fo_cookie 000102030405060708090a0b0c0d0e0f\n",
			json: `{"dst":"192.0.2.1","fopen_miss":1460,"fopen_syn_drops":2,"fopen_syn_drop_ts":1.5,` +
				`"fo_cookie":"000102030405060708090a0b0c0d0e0f"}`,
		},
		{
			description: "corner: an empty cookie prints an empty value, SYN drops with no stamp 0",
			m:           xtcpnl.TCPMetrics{Addr: v4, FopenSynDrops: u16(1), FopenCookie: []byte{}},
			text:        "192.0.2.1 fo_syn_drops 1/0.000usec ago fo_cookie \n",
			json:        `{"dst":"192.0.2.1","fopen_syn_drops":1,"fopen_syn_drop_ts":0,"fo_cookie":""}`,
		},
		{
			description: "corner: IPv6, a seventh-word address in glibc's dotted quad",
			m:           xtcpnl.TCPMetrics{Addr: netip.MustParseAddr("::a:b"), Saddr: netip.MustParseAddr("2001:db8::1")},
			text:        "::0.10.0.11 source 2001:db8::1\n",
			json:        `{"dst":"::0.10.0.11","source":"2001:db8::1"}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v := TCPMetricsViewOf(&tc.m)
			if got := v.Text(); got != tc.text {
				t.Errorf("text\n got %q\nwant %q", got, tc.text)
			}
			got, err := json.Marshal(v)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.json {
				t.Errorf("json\n got %s\nwant %s", got, tc.json)
			}
		})
	}
}
