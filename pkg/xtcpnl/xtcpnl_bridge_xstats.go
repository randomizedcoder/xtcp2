package xtcpnl

import (
	"encoding/binary"
)

// This file decodes the bridge payloads of IFLA_STATS_LINK_XSTATS, the group
// `ip stats show group xstats` requests (filter_mask 0x2). The group nests one
// LINK_XSTATS_TYPE_BRIDGE whose sub-attributes carry per-VLAN bridge_vlan_xstats
// (BRIDGE_XSTATS_VLAN, repeated) and a br_mcast_stats (BRIDGE_XSTATS_MCAST).
// goip grounds those two bodies; the bridge stp body and the bond type are
// refused (see ParseNewStats / internal/goip/obj_stats.go), and only these two
// structs are decoded. x/sys has none of these enums or structs, so they are
// declared here, the ifstatsmsg/netconfmsg precedent.

const (
	// LinkXstatsType* select the device family inside an xstats group
	// (if_link.h:1922). goip decodes only the bridge type.
	LinkXstatsTypeBridge = 1
	LinkXstatsTypeBond   = 2

	// BridgeXstats* are the sub-attributes inside LINK_XSTATS_TYPE_BRIDGE
	// (if_bridge.h:786-794). STP and PAD are not grounded.
	BridgeXstatsVlan  = 1
	BridgeXstatsMcast = 2
	BridgeXstatsPad   = 3
	BridgeXstatsStp   = 4

	// BridgeVlanInfo* are the bridge_vlan_xstats.flags bits goip renders
	// (if_bridge.h:130-132); other bits are decoded but have no name.
	BridgeVlanInfoPvid     = 1 << 1
	BridgeVlanInfoUntagged = 1 << 2

	// BrVlanXstatsSizeCst is sizeof(struct bridge_vlan_xstats): four u64 then
	// vid/flags u16 and a u32 pad (if_bridge.h:153-161).
	BrVlanXstatsSizeCst = 40

	// BrMcastStatsSizeCst is sizeof(struct br_mcast_stats), 30 u64s
	// (if_bridge.h:803-822). The trailing mcast_bytes/mcast_packets pairs are
	// decoded for a faithful transcription but never rendered.
	BrMcastStatsSizeCst = 240
)

// BridgeVlanXstats mirrors the kernel's struct bridge_vlan_xstats, one per VLAN.
// The u32 pad after flags is not kept.
type BridgeVlanXstats struct {
	RxBytes   uint64
	RxPackets uint64
	TxBytes   uint64
	TxPackets uint64
	Vid       uint16
	Flags     uint16
}

// BrMcastStats mirrors the kernel's struct br_mcast_stats. Each paired counter
// is [2]uint64 with index 0 = RX and index 1 = TX (BR_MCAST_DIR_RX/TX,
// if_bridge.h:796-800).
type BrMcastStats struct {
	IgmpV1queries   [2]uint64
	IgmpV2queries   [2]uint64
	IgmpV3queries   [2]uint64
	IgmpLeaves      [2]uint64
	IgmpV1reports   [2]uint64
	IgmpV2reports   [2]uint64
	IgmpV3reports   [2]uint64
	IgmpParseErrors uint64

	MldV1queries   [2]uint64
	MldV2queries   [2]uint64
	MldLeaves      [2]uint64
	MldV1reports   [2]uint64
	MldV2reports   [2]uint64
	MldParseErrors uint64

	// McastBytes/McastPackets are decoded but neither printed nor emitted to
	// JSON (bridge_print_stats_mcast stops at mld_parse_errors).
	McastBytes   [2]uint64
	McastPackets [2]uint64
}

// DeserializeBridgeVlanXstats decodes one bridge_vlan_xstats payload under the
// same tolerant length rule as the u64 decoders: a field the payload cannot
// reach stays zero, extra bytes are ignored, neither is an error.
func DeserializeBridgeVlanXstats(data []byte, v *BridgeVlanXstats) (n int, err error) {
	*v = BridgeVlanXstats{}
	rd64 := func(off int) uint64 {
		if off+8 > len(data) {
			return 0
		}
		return binary.LittleEndian.Uint64(data[off : off+8])
	}
	v.RxBytes, v.RxPackets = rd64(0), rd64(8)
	v.TxBytes, v.TxPackets = rd64(16), rd64(24)
	if len(data) >= 34 {
		v.Vid = binary.LittleEndian.Uint16(data[32:34])
	}
	if len(data) >= 36 {
		v.Flags = binary.LittleEndian.Uint16(data[34:36])
	}
	if len(data) >= BrVlanXstatsSizeCst {
		return BrVlanXstatsSizeCst, nil
	}
	return len(data), nil
}

// DeserializeBrMcastStats decodes a br_mcast_stats payload under the same length
// rule. The field order is the struct order; each [2]uint64 is two slots.
func DeserializeBrMcastStats(data []byte, s *BrMcastStats) (n int, err error) {
	*s = BrMcastStats{}
	fields := []*uint64{
		&s.IgmpV1queries[0], &s.IgmpV1queries[1],
		&s.IgmpV2queries[0], &s.IgmpV2queries[1],
		&s.IgmpV3queries[0], &s.IgmpV3queries[1],
		&s.IgmpLeaves[0], &s.IgmpLeaves[1],
		&s.IgmpV1reports[0], &s.IgmpV1reports[1],
		&s.IgmpV2reports[0], &s.IgmpV2reports[1],
		&s.IgmpV3reports[0], &s.IgmpV3reports[1],
		&s.IgmpParseErrors,
		&s.MldV1queries[0], &s.MldV1queries[1],
		&s.MldV2queries[0], &s.MldV2queries[1],
		&s.MldLeaves[0], &s.MldLeaves[1],
		&s.MldV1reports[0], &s.MldV1reports[1],
		&s.MldV2reports[0], &s.MldV2reports[1],
		&s.MldParseErrors,
		&s.McastBytes[0], &s.McastBytes[1],
		&s.McastPackets[0], &s.McastPackets[1],
	}
	return decodeU64Fields(data, fields, BrMcastStatsSizeCst)
}

// decodeU64Fields fills an ordered list of u64 pointers from a little-endian
// payload, zero-filling any field the payload is too short to reach and
// stopping at the struct size — the tolerant rule get_rtnl_link_stats_rta uses
// (see DecodeLinkStats).
func decodeU64Fields(data []byte, fields []*uint64, size int) (n int, err error) {
	for i := range fields {
		off := i * 8
		if off+8 > len(data) {
			return off, nil
		}
		*fields[i] = binary.LittleEndian.Uint64(data[off : off+8])
	}
	return size, nil
}
