package xtcpnl

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// NP8: `ip -s nexthop show`'s decode, NHA_GROUP_STATS and NHA_HW_STATS_*,
// and its requests' NHA_OP_FLAGS. The captured rows are the nhstats space
// (7_3_1/ws/nhstats/<row>).

var nhstatsReplyCaptured = map[string]string{
	// id 12 group 1/2 hw_stats on, under -s: NHA_HW_STATS_ENABLE 1 and no
	// NHA_HW_STATS_USED; two entries, each with ID and PACKETS
	"ec-003/dump_s": "0000000000000000080001000c0000000600030000000000140002000100000000000000020000000000000008001000010000002c000f801400018008000100010000000800020000000000140001800800010002000000080002000000000008000e0000000080",
	// id 13 group 1,3/2,7 under -s -s: PACKETS_HW too
	"ec-004/dump_ss": "0000000000000000080001000d0000000600030000000000140002000100000002000000020000000600000008001000000000003c000f801c0001800800010001000000080002000000000008000300000000001c00018008000100020000000800020000000000080003000000000008000e0000000080",
	// id 10 group 1/2 under -s -s after the ping: 5 packets through id 2
	"traffic/dump_ss": "0000000000000000080001000a0000000600030000000000140002000100000000000000020000000000000008001000000000003c000f801c0001800800010001000000080002000000000008000300000000001c00018008000100020000000800020005000000080003000000000008000e0000000080",
	// id 12 again, without -s: no stats at all
	"ec-003/dump": "0000000000000000080001000c0000000600030000000000140002000100000000000000020000000000000008000e0000000080",
}

func nhstatsReply(t testing.TB, row string) []byte {
	t.Helper()
	b, err := hex.DecodeString(nhstatsReplyCaptured[row])
	if err != nil || len(b) == 0 {
		t.Fatalf("%s: %v", row, err)
	}
	return b
}

func TestRtaGetattrUint(t *testing.T) {
	tests := []struct {
		description string
		val         []byte
		expected    uint64
	}{
		{description: "positive: u32", val: []byte{5, 0, 0, 0}, expected: 5},
		{description: "positive: u64", val: []byte{1, 0, 0, 0, 1, 0, 0, 0}, expected: 1<<32 | 1},
		{description: "positive: u8", val: []byte{0xff}, expected: 0xff},
		{description: "positive: u16", val: []byte{0x34, 0x12}, expected: 0x1234},
		{description: "boundary: the largest u64", val: bytes.Repeat([]byte{0xff}, 8), expected: ^uint64(0)},
		{description: "negative: empty is -1", val: nil, expected: ^uint64(0)},
		{description: "negative: 3 bytes is -1", val: []byte{1, 0, 0}, expected: ^uint64(0)},
		{description: "negative: 16 bytes is -1", val: make([]byte, 16), expected: ^uint64(0)},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := rtaGetattrUint(tc.val); got != tc.expected {
				t.Errorf("got %#x, expected %#x", got, tc.expected)
			}
		})
	}
}

func TestNexthopOpFlags(t *testing.T) {
	tests := []struct {
		description string
		showStats   int
		expected    uint32
	}{
		{description: "boundary: no -s sends none", showStats: 0, expected: 0},
		{description: "positive: -s", showStats: 1, expected: NhaOpFlagDumpStats},
		{description: "positive: -s -s", showStats: 2, expected: NhaOpFlagDumpStats | NhaOpFlagDumpHwStats},
		{description: "corner: -s -s -s is still both", showStats: 3, expected: 3},
		{description: "negative: a negative count sends none", showStats: -1, expected: 0},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := NexthopOpFlags(tc.showStats); got != tc.expected {
				t.Errorf("got %d, expected %d", got, tc.expected)
			}
		})
	}
}

func nhStatsEntry(id uint32, packets []byte, extra ...[]byte) []byte {
	parts := [][]byte{rtattr(NhaGroupStatsEntryID, le32(id))}
	if packets != nil {
		parts = append(parts, rtattr(NhaGroupStatsEntryPackets, packets))
	}
	return rtattr(NhaGroupStatsEntry|unix.NLA_F_NESTED, concat(append(parts, extra...)...))
}

func TestParseNewNexthopStats(t *testing.T) {
	hdr := make([]byte, NhMsgSizeCst)
	nh := func(attrs ...[]byte) []byte { return concat(append([][]byte{hdr}, attrs...)...) }
	type stats struct {
		Supported, Enabled, Used, HasGroupStats bool
		GroupStats                              []NhGroupStat
	}
	tests := []struct {
		description string
		body        []byte
		expected    stats
		err         error
	}{
		{description: "positive: captured, hw_stats on under -s", body: nhstatsReply(t, "ec-003/dump_s"),
			expected: stats{Supported: true, Enabled: true, HasGroupStats: true,
				GroupStats: []NhGroupStat{{ID: 1}, {ID: 2}}}},
		{description: "positive: captured, -s -s", body: nhstatsReply(t, "ec-004/dump_ss"),
			expected: stats{Supported: true, HasGroupStats: true, GroupStats: []NhGroupStat{{ID: 1}, {ID: 2}}}},
		{description: "positive: captured, counted traffic", body: nhstatsReply(t, "traffic/dump_ss"),
			expected: stats{Supported: true, HasGroupStats: true, GroupStats: []NhGroupStat{{ID: 1}, {ID: 2, Packets: 5}}}},
		{description: "positive: captured, no -s has no stats", body: nhstatsReply(t, "ec-003/dump"), expected: stats{}},
		{description: "positive: hw_stats used", body: nh(rtattr(NhaHwStatsEnable, le32(1)), rtattr(NhaHwStatsUsed, le32(1))),
			expected: stats{Supported: true, Enabled: true, Used: true}},
		{description: "positive: a u64 packet count", body: nh(rtattr(NhaGroupStats,
			nhStatsEntry(1, []byte{0, 0, 0, 0, 1, 0, 0, 0}))),
			expected: stats{HasGroupStats: true, GroupStats: []NhGroupStat{{ID: 1, Packets: 1 << 32}}}},
		{description: "boundary: an empty NHA_GROUP_STATS is present with no entries", body: nh(rtattr(NhaGroupStats, nil)),
			expected: stats{HasGroupStats: true}},
		{description: "boundary: a short hw_stats enable is supported and off", body: nh(rtattr(NhaHwStatsEnable, []byte{1})),
			expected: stats{Supported: true}},
		{description: "boundary: hw_stats enable 2 is on", body: nh(rtattr(NhaHwStatsEnable, le32(2))),
			expected: stats{Supported: true, Enabled: true}},
		{description: "corner: a 3-byte packet count is -1, as rta_getattr_uint reads it",
			body:     nh(rtattr(NhaGroupStats, nhStatsEntry(7, []byte{1, 2, 3}))),
			expected: stats{HasGroupStats: true, GroupStats: []NhGroupStat{{ID: 7, Packets: ^uint64(0)}}}},
		{description: "corner: an entry of any type counts, in order",
			body: nh(rtattr(NhaGroupStats, concat(rtattr(9, rtattr(NhaGroupStatsEntryID, le32(4))),
				nhStatsEntry(5, le32(6))))),
			expected: stats{HasGroupStats: true, GroupStats: []NhGroupStat{{ID: 4}, {ID: 5, Packets: 6}}}},
		{description: "corner: within an entry the first repeat wins, and packets_hw is read",
			body: nh(rtattr(NhaGroupStats, nhStatsEntry(1, le32(2), rtattr(NhaGroupStatsEntryPackets, le32(9)),
				rtattr(NhaGroupStatsEntryPacketsHW, le32(3)), rtattr(NhaGroupStatsEntryID, le32(8))))),
			expected: stats{HasGroupStats: true, GroupStats: []NhGroupStat{{ID: 1, Packets: 2, PacketsHW: 3}}}},
		{description: "corner: a short entry id is 0", body: nh(rtattr(NhaGroupStats,
			rtattr(NhaGroupStatsEntry, rtattr(NhaGroupStatsEntryID, []byte{1, 0})))),
			expected: stats{HasGroupStats: true, GroupStats: []NhGroupStat{{}}}},
		{description: "corner: the first of a repeated hw_stats enable wins",
			body:     nh(rtattr(NhaHwStatsEnable, le32(0)), rtattr(NhaHwStatsEnable, le32(1))),
			expected: stats{Supported: true}},
		{description: "corner: the first of a repeated NHA_GROUP_STATS wins",
			body:     nh(rtattr(NhaGroupStats, nhStatsEntry(1, nil)), rtattr(NhaGroupStats, nhStatsEntry(2, nil))),
			expected: stats{HasGroupStats: true, GroupStats: []NhGroupStat{{ID: 1}}}},
		{description: "negative: a malformed entry", body: nh(rtattr(NhaGroupStats, rtattr(NhaGroupStatsEntry, []byte{12, 0, 1, 0}))),
			err: ErrRTAttrSmall},
		{description: "negative: a malformed NHA_GROUP_STATS", body: nh(rtattr(NhaGroupStats, []byte{12, 0, 1, 0})),
			err: ErrRTAttrSmall},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseNewNexthop(tc.body)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("err %v, expected %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			s := stats{got.HwStatsSupported, got.HwStatsEnabled, got.HwStatsUsed, got.HasGroupStats, got.GroupStats}
			if !reflect.DeepEqual(s, tc.expected) {
				t.Errorf("got %+v, expected %+v", s, tc.expected)
			}
		})
	}
}

// nhstatsShowStats is each text readback's -s count; the JSON ones are
// left to NX8.
var nhstatsShowStats = map[string]int{"dump": 0, "dump_s": 1, "dump_ss": 2, "get_s": 1, "show_id_s": 1}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// nhstatsBlock is the text ip prints after a nexthop's line for its stats,
// as print_nh_grp_stats writes it: one line per NHA_GROUP member, from the
// stats in order, a member past their end printing zeros.
func nhstatsBlock(nh *NexthopInfo, showStats int) string {
	var b strings.Builder
	b.WriteString("  stats:\n")
	for i := range nh.Group {
		var s NhGroupStat
		if i < len(nh.GroupStats) {
			s = nh.GroupStats[i]
		}
		fmt.Fprintf(&b, "    id %d packets %d", s.ID, s.Packets)
		if showStats > 1 {
			fmt.Fprintf(&b, " packets_hw %d", s.PacketsHW)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// TestWsNhstatsReplies decodes every nexthop in the nhstats space's text
// readbacks and finds what ip printed for its stats: `hw_stats … used …` on
// its line under -s, and the stats block after it; none of either without.
func TestWsNhstatsReplies(t *testing.T) {
	var nexthops, blocks int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "nhstats/") || row.Result.Status != "OK" {
			continue
		}
		for name, showStats := range nhstatsShowStats {
			raw, err := os.ReadFile(filepath.Join(dir, name+".out"))
			if err != nil {
				t.Fatal(err)
			}
			out := string(raw)
			for _, m := range pcapNlMsgs(t, filepath.Join(dir, "netlink_route_"+name+".pcap")) {
				if m.h.Type != unix.RTM_NEWNEXTHOP {
					continue
				}
				nh, err := ParseNewNexthop(m.body)
				if err != nil {
					t.Fatalf("%s %s: %v", row.ID, name, err)
				}
				nexthops++
				prefix := "id " + strconv.FormatUint(uint64(nh.ID), 10) + " "
				i := strings.Index(out, prefix)
				if i < 0 || (i > 0 && out[i-1] != '\n') {
					t.Fatalf("%s %s: no line for id %d:\n%s", row.ID, name, nh.ID, out)
				}
				line, rest, _ := strings.Cut(out[i:], "\n")
				hw := fmt.Sprintf("hw_stats %s used %s ", onOff(nh.HwStatsEnabled), onOff(nh.HwStatsUsed))
				if want := showStats > 0 && nh.HwStatsSupported; want != strings.HasSuffix(line, hw) {
					t.Errorf("%s %s: line %q, %q expected %v", row.ID, name, line, hw, want)
				}
				if showStats == 0 && (nh.HasGroupStats || nh.HwStatsSupported) {
					t.Errorf("%s %s: stats without -s: %+v", row.ID, name, nh)
				}
				if !nh.HasGroupStats {
					if strings.HasPrefix(rest, "  stats:") {
						t.Errorf("%s %s: id %d printed stats ip has none for", row.ID, name, nh.ID)
					}
					continue
				}
				blocks++
				if len(nh.GroupStats) != len(nh.Group) {
					t.Errorf("%s %s: %d stats for %d members", row.ID, name, len(nh.GroupStats), len(nh.Group))
				}
				if want := nhstatsBlock(&nh, showStats); !strings.HasPrefix(rest, want) {
					t.Errorf("%s %s: after %q:\n%s\nexpected:\n%s", row.ID, name, line, rest, want)
				}
			}
		}
	}
	t.Logf("%d nexthops, %d stats blocks", nexthops, blocks)
	if nexthops != 106 || blocks != 36 {
		t.Errorf("%d nexthops and %d stats blocks, want 106 and 36", nexthops, blocks)
	}
}

// TestWsNhstatsRequests builds each readback's RTM_GETNEXTHOP and compares
// it with the captured one, past the seq and pid.
func TestWsNhstatsRequests(t *testing.T) {
	var checked int
	for _, dir := range wsRows(t) {
		row := wsLoadRow(t, dir)
		if !strings.HasPrefix(row.ID, "nhstats/") || row.Result.Status != "OK" {
			continue
		}
		id, _ := strconv.ParseUint(row.Axes["id"], 10, 32)
		for name, op := range nhstatsOpFlags {
			reqs := wsRouteRequests(pcapNlMsgs(t, filepath.Join(dir, "netlink_route_"+name+".pcap")), unix.RTM_GETNEXTHOP)
			if len(reqs) != 1 {
				t.Fatalf("%s %s: %d requests", row.ID, name, len(reqs))
			}
			var (
				msg []byte
				err error
			)
			switch {
			case op.get:
				msg, err = BuildGetNexthopByIDOpRequest(0, uint32(id), uint32(op.flags), 0)
			case op.flags < 0:
				msg, err = BuildGetNexthopDumpRequest(0, NexthopDumpFilter{}, 0)
			default:
				msg, err = BuildGetNexthopDumpRequest(0, NexthopDumpFilter{OpFlags: uint32(op.flags)}, 0)
			}
			if err != nil {
				t.Fatal(err)
			}
			var h NlMsgHdr
			if _, err := DeserializeNlMsgHdr(msg, &h); err != nil {
				t.Fatal(err)
			}
			got := reqs[0]
			if h.Type != got.h.Type || h.Flags != got.h.Flags || !bytes.Equal(msg[NlMsgHdrSizeCst:], got.body) {
				t.Errorf("%s %s: built %d %#x %x, captured %d %#x %x", row.ID, name,
					h.Type, h.Flags, msg[NlMsgHdrSizeCst:], got.h.Type, got.h.Flags, got.body)
			}
			checked++
		}
	}
	if checked != 77 {
		t.Errorf("%d requests checked, want 77", checked)
	}
}

func FuzzParseNhGroupStats(f *testing.F) {
	for _, h := range nhstatsReplyCaptured {
		b, _ := hex.DecodeString(h)
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		nh, err := ParseNewNexthop(b)
		if err != nil {
			return
		}
		if !nh.HasGroupStats && nh.GroupStats != nil {
			t.Fatalf("stats %v without NHA_GROUP_STATS", nh.GroupStats)
		}
		if !nh.HwStatsSupported && nh.HwStatsEnabled {
			t.Fatal("hw_stats enabled without NHA_HW_STATS_ENABLE")
		}
	})
}

func BenchmarkParseNewNexthopStats(b *testing.B) {
	body := nhstatsReply(b, "ec-004/dump_ss")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseNewNexthop(body); err != nil {
			b.Fatal(err)
		}
	}
}
