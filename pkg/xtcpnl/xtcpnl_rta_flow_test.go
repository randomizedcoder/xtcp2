package xtcpnl

import (
	"testing"

	"golang.org/x/sys/unix"
)

// TestParseNewRouteFlow checks RTA_FLOW, `realms FROM/TO`, on a route and on
// a multipath path, as print_rta_flow reads it.
func TestParseNewRouteFlow(t *testing.T) {
	hdr := rtmsgHdr(unix.AF_INET, 24, 0, unix.RT_TABLE_MAIN, unix.RTPROT_BOOT, unix.RT_SCOPE_LINK, unix.RTN_UNICAST, 0)
	type flow struct {
		v   uint32
		has bool
	}
	tests := []struct {
		description string
		attrs       [][]byte
		expected    flow // the route's
		hop         *flow
	}{
		{description: "positive: realm 5", attrs: [][]byte{rtattr(unix.RTA_FLOW, le32(5))}, expected: flow{5, true}},
		{description: "positive: realms 1/2", attrs: [][]byte{rtattr(unix.RTA_FLOW, le32(1<<16|2))}, expected: flow{1<<16 | 2, true}},
		{description: "negative: no RTA_FLOW", attrs: nil},
		{description: "boundary: realm 0 is present", attrs: [][]byte{rtattr(unix.RTA_FLOW, le32(0))}, expected: flow{0, true}},
		{description: "corner: a short RTA_FLOW is dropped", attrs: [][]byte{rtattr(unix.RTA_FLOW, []byte{5, 0})}},
		{description: "corner: the first of a repeat wins",
			attrs: [][]byte{rtattr(unix.RTA_FLOW, le32(3)), rtattr(unix.RTA_FLOW, le32(4))}, expected: flow{3, true}},
		{description: "positive: a path's realm 3",
			attrs: [][]byte{rtattr(unix.RTA_MULTIPATH, concat(
				rtnh(t, 2, rtattr(unix.RTA_GATEWAY, v4b(192, 0, 2, 1)), rtattr(unix.RTA_FLOW, le32(3)))))},
			hop: &flow{3, true}},
		{description: "corner: a path's short RTA_FLOW is dropped",
			attrs: [][]byte{rtattr(unix.RTA_MULTIPATH, concat(
				rtnh(t, 2, rtattr(unix.RTA_GATEWAY, v4b(192, 0, 2, 1)), rtattr(unix.RTA_FLOW, []byte{3}))))},
			hop: &flow{}},
		{description: "corner: a path's first RTA_FLOW wins, as parse_rtattr keeps it",
			attrs: [][]byte{rtattr(unix.RTA_MULTIPATH, concat(
				rtnh(t, 2, rtattr(unix.RTA_FLOW, le32(3)), rtattr(unix.RTA_FLOW, le32(4)))))},
			hop: &flow{3, true}},
		{description: "negative: a path with none",
			attrs: [][]byte{rtattr(unix.RTA_MULTIPATH, concat(rtnh(t, 2, rtattr(unix.RTA_GATEWAY, v4b(192, 0, 2, 2)))))},
			hop:   &flow{}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ri, err := ParseNewRoute(concat(append([][]byte{hdr}, tc.attrs...)...))
			if err != nil {
				t.Fatal(err)
			}
			if got := (flow{ri.Flow, ri.HasFlow}); got != tc.expected {
				t.Errorf("route flow %+v, expected %+v", got, tc.expected)
			}
			if tc.hop == nil {
				return
			}
			if len(ri.Multipath) != 1 {
				t.Fatalf("%d paths, expected 1", len(ri.Multipath))
			}
			if got := (flow{ri.Multipath[0].Flow, ri.Multipath[0].HasFlow}); got != *tc.hop {
				t.Errorf("path flow %+v, expected %+v", got, *tc.hop)
			}
		})
	}
}
