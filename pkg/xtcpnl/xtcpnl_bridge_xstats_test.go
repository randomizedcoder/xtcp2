package xtcpnl

import (
	"encoding/binary"
	"testing"

	"golang.org/x/sys/unix"
)

// seqU64 builds n little-endian u64s with slot i holding value i+1, so decode
// tests can prove field order and the RX=slot0/TX=slot1 convention.
func seqU64(n int) []byte {
	b := make([]byte, n*8)
	for i := range n {
		binary.LittleEndian.PutUint64(b[i*8:i*8+8], uint64(i+1))
	}
	return b
}

// vlanBytes builds a bridge_vlan_xstats payload: four u64 then vid/flags u16 and
// a u32 pad.
func vlanBytes(rxBytes, rxPackets, txBytes, txPackets uint64, vid, flags uint16) []byte {
	b := make([]byte, BrVlanXstatsSizeCst)
	binary.LittleEndian.PutUint64(b[0:8], rxBytes)
	binary.LittleEndian.PutUint64(b[8:16], rxPackets)
	binary.LittleEndian.PutUint64(b[16:24], txBytes)
	binary.LittleEndian.PutUint64(b[24:32], txPackets)
	binary.LittleEndian.PutUint16(b[32:34], vid)
	binary.LittleEndian.PutUint16(b[34:36], flags)
	return b
}

// TestDeserializeBridgeVlanXstats covers the mixed-width bridge_vlan_xstats under
// the tolerant length rule.
//
// go test ./pkg/xtcpnl/ -run TestDeserializeBridgeVlanXstats
func TestDeserializeBridgeVlanXstats(t *testing.T) {
	tests := []struct {
		description string
		input       []byte
		want        BridgeVlanXstats
		wantN       int
	}{
		{
			description: "positive: full 40-byte entry decodes every field",
			input:       vlanBytes(10, 11, 20, 21, 1, BridgeVlanInfoPvid|BridgeVlanInfoUntagged),
			want:        BridgeVlanXstats{RxBytes: 10, RxPackets: 11, TxBytes: 20, TxPackets: 21, Vid: 1, Flags: BridgeVlanInfoPvid | BridgeVlanInfoUntagged},
			wantN:       BrVlanXstatsSizeCst,
		},
		{
			description: "boundary: 34 bytes reaches vid but not flags",
			input:       vlanBytes(10, 11, 20, 21, 7, 0xffff)[:34],
			want:        BridgeVlanXstats{RxBytes: 10, RxPackets: 11, TxBytes: 20, TxPackets: 21, Vid: 7},
			wantN:       34,
		},
		{
			description: "corner: 32 bytes is the u64 block only, vid/flags zero",
			input:       vlanBytes(10, 11, 20, 21, 7, 4)[:32],
			want:        BridgeVlanXstats{RxBytes: 10, RxPackets: 11, TxBytes: 20, TxPackets: 21},
			wantN:       32,
		},
		{
			description: "corner: long (44 bytes) truncates to the struct size",
			input:       append(vlanBytes(1, 2, 3, 4, 5, BridgeVlanInfoPvid), 0, 0, 0, 0),
			want:        BridgeVlanXstats{RxBytes: 1, RxPackets: 2, TxBytes: 3, TxPackets: 4, Vid: 5, Flags: BridgeVlanInfoPvid},
			wantN:       BrVlanXstatsSizeCst,
		},
		{
			description: "negative: empty slice is a zero struct, no error",
			input:       []byte{},
			want:        BridgeVlanXstats{},
			wantN:       0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var v BridgeVlanXstats
			n, err := DeserializeBridgeVlanXstats(tc.input, &v)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if n != tc.wantN {
				t.Errorf("n = %d, want %d", n, tc.wantN)
			}
			if v != tc.want {
				t.Errorf("struct = %+v, want %+v", v, tc.want)
			}
		})
	}
}

// TestDeserializeBrMcastStats covers the 30-u64 br_mcast_stats, including the
// RX=slot0/TX=slot1 pairing and the two scalars among the arrays.
//
// go test ./pkg/xtcpnl/ -run TestDeserializeBrMcastStats
func TestDeserializeBrMcastStats(t *testing.T) {
	full := BrMcastStats{
		IgmpV1queries: [2]uint64{1, 2}, IgmpV2queries: [2]uint64{3, 4}, IgmpV3queries: [2]uint64{5, 6},
		IgmpLeaves: [2]uint64{7, 8}, IgmpV1reports: [2]uint64{9, 10}, IgmpV2reports: [2]uint64{11, 12},
		IgmpV3reports: [2]uint64{13, 14}, IgmpParseErrors: 15,
		MldV1queries: [2]uint64{16, 17}, MldV2queries: [2]uint64{18, 19}, MldLeaves: [2]uint64{20, 21},
		MldV1reports: [2]uint64{22, 23}, MldV2reports: [2]uint64{24, 25}, MldParseErrors: 26,
		McastBytes: [2]uint64{27, 28}, McastPackets: [2]uint64{29, 30},
	}
	tests := []struct {
		description string
		input       []byte
		want        BrMcastStats
		wantN       int
	}{
		{
			description: "positive: full 240-byte sentinel; pairs keep RX=slot0/TX=slot1, scalars at slots 14 and 25",
			input:       seqU64(30),
			want:        full,
			wantN:       BrMcastStatsSizeCst,
		},
		{
			description: "boundary: first 15 slots fill the igmp block and its scalar, mld/mcast zero",
			input:       seqU64(15),
			want: BrMcastStats{
				IgmpV1queries: [2]uint64{1, 2}, IgmpV2queries: [2]uint64{3, 4}, IgmpV3queries: [2]uint64{5, 6},
				IgmpLeaves: [2]uint64{7, 8}, IgmpV1reports: [2]uint64{9, 10}, IgmpV2reports: [2]uint64{11, 12},
				IgmpV3reports: [2]uint64{13, 14}, IgmpParseErrors: 15,
			},
			wantN: 120,
		},
		{
			description: "corner: long (32 slots) truncates to the struct size",
			input:       seqU64(32),
			want:        full,
			wantN:       BrMcastStatsSizeCst,
		},
		{
			description: "negative: empty slice is a zero struct, no error",
			input:       []byte{},
			want:        BrMcastStats{},
			wantN:       0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var s BrMcastStats
			n, err := DeserializeBrMcastStats(tc.input, &s)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if n != tc.wantN {
				t.Errorf("n = %d, want %d", n, tc.wantN)
			}
			if s != tc.want {
				t.Errorf("struct = %+v, want %+v", s, tc.want)
			}
		})
	}
}

// bridgeXstatsBody wraps bridge sub-attrs in the LINK_XSTATS → TYPE_BRIDGE nest
// and prepends a 12-byte if_stats_msg header (ifindex, filter_mask 0x2).
func bridgeXstatsBody(ifindex uint32, inner []byte) []byte {
	nest := rtattr(LinkXstatsTypeBridge, inner)
	return cat(ifsmHdr(0, ifindex, StatsFilterXstats), rtattr(uint16(unix.IFLA_STATS_LINK_XSTATS), nest))
}

// TestParseNewStatsXstats covers the nested xstats walk with synthetic bodies —
// no capture produces these exact shapes, so they are contract rows. The
// captured-bytes row lives in TestParseNewStatsXstatsCaptured.
//
// go test ./pkg/xtcpnl/ -run TestParseNewStatsXstats
func TestParseNewStatsXstats(t *testing.T) {
	vlan := rtattr(BridgeXstatsVlan, vlanBytes(0, 0, 0, 0, 1, BridgeVlanInfoPvid|BridgeVlanInfoUntagged))
	mcast := rtattr(BridgeXstatsMcast, seqU64(30))

	tests := []struct {
		description string
		body        []byte
		check       func(t *testing.T, si IfStatsInfo)
	}{
		{
			description: "positive: TYPE_BRIDGE with vlan and mcast sets both bodies",
			body:        bridgeXstatsBody(5, cat(vlan, mcast)),
			check: func(t *testing.T, si IfStatsInfo) {
				if !si.HasBridgeXstats || !si.HasBridgeVlan || !si.HasBridgeMcast {
					t.Errorf("has flags = %+v, want all true", si)
				}
				if len(si.BridgeVlans) != 1 || si.BridgeVlans[0].Vid != 1 || si.BridgeMcast.IgmpV1queries != [2]uint64{1, 2} {
					t.Errorf("bodies = %+v / %+v", si.BridgeVlans, si.BridgeMcast)
				}
				if si.HasUngroundedXstatsBody || si.HasUnsupportedGroup {
					t.Errorf("unexpected refusal flag: %+v", si)
				}
			},
		},
		{
			description: "positive: two vlan attrs append in order",
			body: bridgeXstatsBody(5, cat(
				rtattr(BridgeXstatsVlan, vlanBytes(0, 0, 0, 0, 1, BridgeVlanInfoPvid)),
				rtattr(BridgeXstatsVlan, vlanBytes(0, 0, 0, 0, 10, BridgeVlanInfoUntagged)),
			)),
			check: func(t *testing.T, si IfStatsInfo) {
				if len(si.BridgeVlans) != 2 || si.BridgeVlans[0].Vid != 1 || si.BridgeVlans[1].Vid != 10 {
					t.Errorf("vlans = %+v, want vids [1 10]", si.BridgeVlans)
				}
			},
		},
		{
			description: "boundary: empty TYPE_BRIDGE nest marks the group present, no bodies",
			body:        bridgeXstatsBody(5, nil),
			check: func(t *testing.T, si IfStatsInfo) {
				if !si.HasBridgeXstats || si.HasBridgeVlan || si.HasBridgeMcast || si.HasUngroundedXstatsBody {
					t.Errorf("flags = %+v, want HasBridgeXstats only", si)
				}
			},
		},
		{
			description: "negative: a bridge stp body is ungrounded",
			body:        bridgeXstatsBody(5, rtattr(BridgeXstatsStp, seqU64(6))),
			check: func(t *testing.T, si IfStatsInfo) {
				if !si.HasUngroundedXstatsBody {
					t.Errorf("HasUngroundedXstatsBody = false, want true")
				}
			},
		},
		{
			description: "negative: a bond xstats type is ungrounded",
			body:        cat(ifsmHdr(0, 5, StatsFilterXstats), rtattr(uint16(unix.IFLA_STATS_LINK_XSTATS), rtattr(LinkXstatsTypeBond, seqU64(9)))),
			check: func(t *testing.T, si IfStatsInfo) {
				if !si.HasUngroundedXstatsBody {
					t.Errorf("HasUngroundedXstatsBody = false, want true")
				}
			},
		},
		{
			description: "corner: an unknown inner type is ignored, vlan still read",
			body:        bridgeXstatsBody(5, cat(rtattr(99, seqU64(1)), vlan)),
			check: func(t *testing.T, si IfStatsInfo) {
				if !si.HasBridgeVlan || si.HasUngroundedXstatsBody {
					t.Errorf("flags = %+v, want vlan read and no refusal", si)
				}
			},
		},
		{
			description: "negative: the xstats_slave group stays unsupported",
			body:        cat(ifsmHdr(0, 5, StatsFilterXstats), rtattr(uint16(unix.IFLA_STATS_LINK_XSTATS_SLAVE), nil)),
			check: func(t *testing.T, si IfStatsInfo) {
				if !si.HasUnsupportedGroup {
					t.Errorf("HasUnsupportedGroup = false, want true")
				}
			},
		},
		{
			description: "corner: LINK_64 and LINK_XSTATS in one record both decode",
			body:        cat(ifsmHdr(0, 5, StatsFilterLink64|StatsFilterXstats), rtattr(uint16(unix.IFLA_STATS_LINK_64), link64Payload(10, 20)), rtattr(uint16(unix.IFLA_STATS_LINK_XSTATS), rtattr(LinkXstatsTypeBridge, vlan))),
			check: func(t *testing.T, si IfStatsInfo) {
				if !si.HasLink64 || !si.HasBridgeXstats || !si.HasBridgeVlan {
					t.Errorf("flags = %+v, want link64 + bridge vlan", si)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			si, err := ParseNewStats(tc.body)
			if err != nil {
				t.Fatalf("ParseNewStats: %v", err)
			}
			tc.check(t, si)
		})
	}
}

// xstatsPcapMesh is the committed `ip stats show group xstats` capture: the
// ll_init_map link dump then the RTM_GETSTATS xstats dump. br0 (ifindex 3) is
// the only device carrying a bridge xstats attr; its counters are frozen at zero
// on the quiescent bridge, which is the ground truth the golden was rendered from.
const xstatsPcapMesh = "testdata/7_1_4/dumps/mesh/netlink_route_getstats_xstats.pcap"

// TestParseNewStatsXstatsCaptured decodes the real RTM_NEWSTATS bodies from the
// committed mesh xstats pcap: five records (ifindex 1..5), only br0 (ifindex 3)
// carrying LINK_XSTATS → TYPE_BRIDGE → {VLAN, MCAST}, all counters zero, and no
// record tripping the ungrounded or unsupported refusal flags.
//
// go test ./pkg/xtcpnl/ -run TestParseNewStatsXstatsCaptured
func TestParseNewStatsXstatsCaptured(t *testing.T) {
	bodies := ifStatsBodies(t, xstatsPcapMesh)
	if got := len(bodies); got != 5 {
		t.Fatalf("RTM_NEWSTATS message count = %d, want 5", got)
	}
	for i, body := range bodies {
		si, err := ParseNewStats(body)
		if err != nil {
			t.Fatalf("ParseNewStats(body %d): %v", i, err)
		}
		if want := uint32(i + 1); si.Ifindex != want {
			t.Errorf("body %d ifindex = %d, want %d", i, si.Ifindex, want)
		}
		if si.HasUngroundedXstatsBody || si.HasUnsupportedGroup {
			t.Errorf("body %d trips a refusal flag: %+v", i, si)
		}
		isBr0 := si.Ifindex == 3
		if si.HasBridgeVlan != isBr0 || si.HasBridgeMcast != isBr0 {
			t.Errorf("body %d (ifindex %d) vlan/mcast = %v/%v, want %v", i, si.Ifindex, si.HasBridgeVlan, si.HasBridgeMcast, isBr0)
		}
	}
	// br0 carries exactly one VID (PVID + Egress Untagged) with zero counters and
	// an all-zero mcast block.
	br0, _ := ParseNewStats(bodies[2])
	if len(br0.BridgeVlans) != 1 {
		t.Fatalf("br0 vlan count = %d, want 1", len(br0.BridgeVlans))
	}
	// The kernel's raw flags are 0x27 = MASTER|PVID|UNTAGGED|BRENTRY; iproute2
	// names only PVID and UNTAGGED, so the renderer masks to those two (the mesh
	// golden proves the text/JSON output). Here the whole wire value is pinned.
	v := br0.BridgeVlans[0]
	if v.Vid != 1 || v.Flags != 0x27 {
		t.Errorf("br0 vid/flags = %d/%#x, want 1/0x27", v.Vid, v.Flags)
	}
	if v.Flags&BridgeVlanInfoPvid == 0 || v.Flags&BridgeVlanInfoUntagged == 0 {
		t.Errorf("br0 flags %#x missing PVID or UNTAGGED", v.Flags)
	}
	if v.RxBytes|v.RxPackets|v.TxBytes|v.TxPackets != 0 {
		t.Errorf("br0 vlan counters = %+v, want all zero", v)
	}
	if br0.BridgeMcast.IgmpV1queries != [2]uint64{0, 0} || br0.BridgeMcast.MldParseErrors != 0 {
		t.Errorf("br0 mcast = %+v, want all zero", br0.BridgeMcast)
	}
}
