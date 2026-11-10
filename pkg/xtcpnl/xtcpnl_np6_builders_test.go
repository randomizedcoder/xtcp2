package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// NP6: the stats-filter and multicast-dump request builders
// (goip-parity-next.md). Each captured row's bytes are the request ip sent
// in that row, nlmsg_seq aside.

// np6Requests is every request of type typ in a row's ip.pcap, with
// nlmsg_seq zeroed.
func np6Requests(t *testing.T, row string, typ uint16) [][]byte {
	t.Helper()
	var out [][]byte
	for _, m := range pcapNlMsgs(t, filepath.Join(tdBase, row, "ip.pcap")) {
		if m.h.Type != typ || m.h.Flags&unix.NLM_F_REQUEST == 0 {
			continue
		}
		b := make([]byte, NlMsgHdrSizeCst, NlMsgHdrSizeCst+len(m.body))
		binary.LittleEndian.PutUint32(b[0:], m.h.Len)
		binary.LittleEndian.PutUint16(b[4:], m.h.Type)
		binary.LittleEndian.PutUint16(b[6:], m.h.Flags)
		out = append(out, append(b, m.body...)) // seq and pid stay 0
	}
	return out
}

func TestBuildStatsFilterRequest(t *testing.T) {
	const offload, link, afstats = 1 << 3, 1 << 0, 1 << 4
	// The full sub-masks are written out, (1 << max) - 1 from the kernel's
	// IFLA_OFFLOAD_XSTATS_MAX (3) and LINK_XSTATS_TYPE_MAX (2), not taken
	// from the builder's constants.
	tests := []struct {
		description string
		row         string // the stats row whose request this is, under 7_3_1/ws/stats, or ""
		ifindex     uint32
		filter      StatsFilter
		expected    string // the request's hex, seq 0, when row is ""
	}{
		{description: "positive: group offload subgroup l3_stats, a nest {4: 0x6}", row: "l3-stats-text", filter: StatsFilter{Mask: offload, Offload: 0x6}},
		{description: "positive: the same for one device, a get", row: "l3-stats-dev-text", ifindex: 3, filter: StatsFilter{Mask: offload, Offload: 0x6}},
		{description: "positive: subgroup cpu_hit, a nest {4: 0x1}", row: "cpu-hit-text", filter: StatsFilter{Mask: offload, Offload: 0x1}},
		{description: "boundary: a full offload sub-mask sends no nest", row: "two-subgroups-text", filter: StatsFilter{Mask: offload, Offload: 0x7}},
		{description: "positive: the bare show, every group and no nest", row: "show-clean-text", filter: StatsFilter{Mask: 0x1f}},
		{description: "positive: the bare show of one device", row: "dev-text", ifindex: 3, filter: StatsFilter{Mask: 0x1f}},
		{description: "positive: two groups", row: "two-groups-text", filter: StatsFilter{Mask: link | afstats}},
		{description: "positive: link xstats type bond, the group alone", row: "xstats-bond-text", filter: StatsFilter{Mask: 1 << 1}},
		{
			description: "corner: the three sub-masks in group order",
			filter:      StatsFilter{Mask: 0xe, Xstats: 1, XstatsSlave: 2, Offload: 4},
			// nlmsghdr (len 56, RTM_GETSTATS, REQUEST|DUMP); if_stats_msg mask
			// 0xe; IFLA_STATS_GET_FILTERS|NESTED {2: 1, 3: 2, 4: 4}.
			expected: "38000000" + "5e000103" + "0000000000000000" + "00000000000000000e000000" +
				"1c000180" + "0800020001000000" + "0800030002000000" + "0800040004000000",
		},
		{
			description: "boundary: full xstats sub-masks send no nest, a partial offload one does",
			filter:      StatsFilter{Mask: 0xe, Xstats: 0x3, XstatsSlave: 0x3, Offload: 2},
			expected:    "28000000" + "5e000103" + "0000000000000000" + "00000000000000000e000000" + "0c000180" + "0800040002000000",
		},
		{
			description: "corner: a sub-mask past the full one is sent as given",
			filter:      StatsFilter{Mask: offload, Offload: 0x8},
			expected:    "28000000" + "5e000103" + "0000000000000000" + "000000000000000008000000" + "0c000180" + "0800040008000000",
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := BuildStatsRequest(tc.ifindex, tc.filter, 0)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			want := mustHex(t, tc.expected)
			if tc.row != "" {
				reqs := np6Requests(t, filepath.Join("7_3_1", "ws", "stats", tc.row), unix.RTM_GETSTATS)
				if len(reqs) != 1 {
					t.Fatalf("%d RTM_GETSTATS requests in stats/%s, want 1", len(reqs), tc.row)
				}
				want = reqs[0]
			}
			if !bytes.Equal(got, want) {
				t.Errorf("\n got  %x\n want %x", got, want)
			}
		})
	}
}

func TestBuildDumpMulticastRequest(t *testing.T) {
	tests := []struct {
		description string
		row         string // the v730maddr row, under 7_3_1/ws-ip730/v730maddr
		requests    []struct {
			family  uint8
			ifindex uint32
		}
	}{
		{
			description: "positive: a show of both families, AF_INET then AF_INET6",
			row:         "all",
			requests: []struct {
				family  uint8
				ifindex uint32
			}{{unix.AF_INET, 0}, {unix.AF_INET6, 0}},
		},
		{
			description: "positive: dev d0 puts its index in each",
			row:         "dev",
			requests: []struct {
				family  uint8
				ifindex uint32
			}{{unix.AF_INET, 3}, {unix.AF_INET6, 3}},
		},
		{
			description: "positive: -4 alone",
			row:         "inet",
			requests: []struct {
				family  uint8
				ifindex uint32
			}{{unix.AF_INET, 0}},
		},
		{
			description: "positive: -6 alone",
			row:         "inet6",
			requests: []struct {
				family  uint8
				ifindex uint32
			}{{unix.AF_INET6, 0}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want := np6Requests(t, filepath.Join("7_3_1", "ws-ip730", "v730maddr", tc.row), unix.RTM_GETMULTICAST)
			if len(want) != len(tc.requests) {
				t.Fatalf("%d captured requests, want %d", len(want), len(tc.requests))
			}
			for i, r := range tc.requests {
				got := BuildDumpMulticastRequest(r.family, r.ifindex, 0)
				if len(got) != NlMsgHdrSizeCst+IfAddrmsgSizeCst {
					t.Errorf("request %d is %d bytes, want 24", i, len(got))
				}
				if !bytes.Equal(got, want[i]) {
					t.Errorf("request %d:\n got  %x\n want %x", i, got, want[i])
				}
			}
		})
	}
}

// BenchmarkBuildStatsRequest builds the l3_stats dump, the one that carries
// a nest.
func BenchmarkBuildStatsRequest(b *testing.B) {
	f := StatsFilter{Mask: 1 << 3, Offload: 0x6}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := BuildStatsRequest(0, f, 1); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkBuildDumpMulticastRequest builds one family's dump.
func BenchmarkBuildDumpMulticastRequest(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		if BuildDumpMulticastRequest(unix.AF_INET6, 3, 1) == nil {
			b.Fatal("nil request")
		}
	}
}
