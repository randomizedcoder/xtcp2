package xtcpnl

import (
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// TestDecodeRouteWriteCorpus runs the read path's decoder over the route
// writes `ip` 7.1.0 sent in the captured corpus
// (testdata/7_1_4/link-write/route/), so the encoder's wire format and the
// decoder's agree on bytes neither of them produced, and checks the kernel's
// answer with ParseNlMsgErr.
//
// metrics is keyed by RTAX_*; a metric not listed must be absent.
func TestDecodeRouteWriteCorpus(t *testing.T) {
	type result struct {
		msgType uint16
		table   uint32
		via     bool
		metrics map[uint16]uint32
		ccAlgo  string
		errno   syscall.Errno
	}
	tests := []struct {
		description string
		slug        string
		expected    result
	}{
		{description: "positive: the NFS metrics decode, lock included", slug: "replace_nfs",
			expected: result{msgType: unix.RTM_NEWROUTE, table: unix.RT_TABLE_MAIN, ccAlgo: "cubic",
				metrics: map[uint16]uint32{
					unix.RTAX_RTO_MIN: 3, unix.RTAX_INITCWND: 32, unix.RTAX_INITRWND: 32,
					unix.RTAX_QUICKACK: 1, unix.RTAX_LOCK: 0x12000,
				}}},
		{description: "positive: every u32 metric as typed, rtt and rttvar scaled", slug: "metrics_all",
			expected: result{msgType: unix.RTM_NEWROUTE, table: unix.RT_TABLE_MAIN,
				metrics: map[uint16]uint32{
					unix.RTAX_MTU: 1400, unix.RTAX_ADVMSS: 1360, unix.RTAX_WINDOW: 65535,
					unix.RTAX_RTT: 24, unix.RTAX_RTTVAR: 40, unix.RTAX_SSTHRESH: 100,
					unix.RTAX_CWND: 10, unix.RTAX_REORDERING: 5, unix.RTAX_HOPLIMIT: 64,
					unix.RTAX_INITCWND: 10, unix.RTAX_INITRWND: 20, unix.RTAX_FEATURES: 0x11,
					unix.RTAX_FASTOPEN_NO_COOKIE: 1, unix.RTAX_LOCK: 1 << unix.RTAX_MTU,
				}}},
		{description: "boundary: raw rtt is unscaled and rto_min always locks", slug: "metrics_raw",
			expected: result{msgType: unix.RTM_NEWROUTE, table: unix.RT_TABLE_MAIN,
				metrics: map[uint16]uint32{
					unix.RTAX_RTT: 24, unix.RTAX_RTTVAR: 40, unix.RTAX_RTO_MIN: 200,
					unix.RTAX_LOCK: 1 << unix.RTAX_RTO_MIN,
				}}},
		{description: "boundary: table 300 spills into RTA_TABLE", slug: "add_table_300",
			expected: result{msgType: unix.RTM_NEWROUTE, table: 300}},
		{description: "boundary: table 100 fits the header", slug: "add_table_100",
			expected: result{msgType: unix.RTM_NEWROUTE, table: 100}},
		{description: "positive: a cross-family next hop is RTA_VIA", slug: "add_via_inet6",
			expected: result{msgType: unix.RTM_NEWROUTE, table: unix.RT_TABLE_MAIN, via: true}},
		{description: "negative: deleting a missing route is ESRCH", slug: "del_missing",
			expected: result{msgType: unix.RTM_DELROUTE, table: unix.RT_TABLE_MAIN, errno: unix.ESRCH}},
		// change is NLM_F_REPLACE without CREATE, and metric is part of a
		// route's identity, so metric 100 names a route that is not there.
		{description: "negative: change of a route with another metric is ENOENT", slug: "change_metric",
			expected: result{msgType: unix.RTM_NEWROUTE, table: unix.RT_TABLE_MAIN, errno: unix.ENOENT}},
		{description: "negative: test of an existing route is EEXIST", slug: "test_dev",
			expected: result{msgType: unix.RTM_NEWROUTE, table: unix.RT_TABLE_MAIN, errno: unix.EEXIST}},
		// Both sub-attributes are on the wire (TestRouteWriteMatchesIpBytes
		// pins that); the decoder indexes by RTAX_*, so the last one wins,
		// as it does in the kernel's ip_metrics_convert.
		{description: "corner: a repeated metric decodes to the last value", slug: "metric_repeated",
			expected: result{msgType: unix.RTM_NEWROUTE, table: unix.RT_TABLE_MAIN,
				metrics: map[uint16]uint32{unix.RTAX_INITCWND: 20}}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			flags, body, request := lastNlMsgErr(t, tdLinkWrite_7_1_4+"/route/"+tc.slug+"/ip.pcap")
			var got result
			got.msgType = uint16(request[4]) | uint16(request[5])<<8
			ri, err := ParseNewRoute(request[NlMsgHdrSizeCst:])
			if err != nil {
				t.Fatal(err)
			}
			got.table, got.via = ri.Table, ri.Via != nil
			if ri.Metrics != nil {
				got.metrics = map[uint16]uint32{}
				for rtax := uint16(0); rtax <= RouteMetricMaxCst; rtax++ {
					if v, ok := ri.Metrics.Get(rtax); ok && rtax != unix.RTAX_CC_ALGO {
						got.metrics[rtax] = v
					}
				}
				got.ccAlgo = ri.Metrics.CcAlgo
			}
			e, err := ParseNlMsgErr(flags, body)
			if err != nil {
				t.Fatal(err)
			}
			if e.Errno != 0 {
				got.errno = syscall.Errno(-e.Errno)
			}
			if e.RequestType != got.msgType {
				t.Errorf("the answer echoes type %d, the write was %d", e.RequestType, got.msgType)
			}
			if !equalMetricMap(got.metrics, tc.expected.metrics) ||
				got.msgType != tc.expected.msgType || got.table != tc.expected.table ||
				got.via != tc.expected.via || got.ccAlgo != tc.expected.ccAlgo ||
				got.errno != tc.expected.errno {
				t.Errorf("got %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}

func equalMetricMap(a, b map[uint16]uint32) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}
