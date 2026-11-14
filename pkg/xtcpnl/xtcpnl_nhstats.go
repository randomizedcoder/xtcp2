package xtcpnl

import "encoding/binary"

// The nexthop attributes `ip -s nexthop show` reads (include/uapi/linux/
// nexthop.h). NhaOpFlags and NhaHwStatsEnable are in xtcpnl_nhmsg.go.
const (
	// NhaGroupStats is NHA_GROUP_STATS, a nest of NHA_GROUP_STATS_ENTRY
	// nests, which the kernel adds when the request's NHA_OP_FLAGS has
	// NHA_OP_FLAG_DUMP_STATS.
	NhaGroupStats uint16 = 15
	// NhaHwStatsUsed is NHA_HW_STATS_USED, u32.
	NhaHwStatsUsed uint16 = 17

	NhaGroupStatsEntry uint16 = 1 // NHA_GROUP_STATS_ENTRY, a nest

	NhaGroupStatsEntryID        uint16 = 1 // u32
	NhaGroupStatsEntryPackets   uint16 = 2 // uint: u32 or u64
	NhaGroupStatsEntryPacketsHW uint16 = 3 // uint: u32 or u64

	// NhaOpFlagDumpStats and NhaOpFlagDumpHwStats are NHA_OP_FLAG_DUMP_STATS
	// and NHA_OP_FLAG_DUMP_HW_STATS: ipnh_get_op_flags sends the first for
	// -s and both for -s -s.
	NhaOpFlagDumpStats   uint32 = 1 << 0
	NhaOpFlagDumpHwStats uint32 = 1 << 1
)

// NhGroupStat is one NHA_GROUP_STATS_ENTRY, as parse_nh_group_stats_rta
// (ip/ipnexthop.c) reads it: a missing member is 0.
type NhGroupStat struct {
	ID        uint32
	Packets   uint64
	PacketsHW uint64
}

// NexthopOpFlags is ipnh_get_op_flags: the NHA_OP_FLAGS `ip nexthop` sends
// for its -s count.
func NexthopOpFlags(showStats int) uint32 {
	var f uint32
	if showStats > 0 {
		f |= NhaOpFlagDumpStats
		if showStats > 1 {
			f |= NhaOpFlagDumpHwStats
		}
	}
	return f
}

// rtaGetattrUint is libnetlink's rta_getattr_uint: a 1, 2, 4 or 8-byte
// payload read in host order, and -1 (all ones) for any other length.
func rtaGetattrUint(v []byte) uint64 {
	switch len(v) {
	case 1:
		return uint64(v[0])
	case 2:
		return uint64(binary.LittleEndian.Uint16(v))
	case 4:
		return uint64(binary.LittleEndian.Uint32(v))
	case 8:
		return binary.LittleEndian.Uint64(v)
	}
	return ^uint64(0)
}

// parseNhGroupStats decodes NHA_GROUP_STATS. As in parse_nh_group_stats_rta,
// every nest is an entry whatever its type, in order, and within an entry
// the first of a repeat wins; a short ID is dropped.
func parseNhGroupStats(val []byte) ([]NhGroupStat, error) {
	var (
		stats   []NhGroupStat
		nestErr error
	)
	err := WalkRTAttrsNested(val, func(_ uint16, entry []byte) {
		var (
			s    NhGroupStat
			seen attrSeen
		)
		err := WalkRTAttrsNested(entry, func(atype uint16, v []byte) {
			if !seen.first(atype) {
				return
			}
			switch atype {
			case NhaGroupStatsEntryID:
				if len(v) >= 4 {
					s.ID = binary.LittleEndian.Uint32(v)
				}
			case NhaGroupStatsEntryPackets:
				s.Packets = rtaGetattrUint(v)
			case NhaGroupStatsEntryPacketsHW:
				s.PacketsHW = rtaGetattrUint(v)
			}
		})
		if err != nil && nestErr == nil {
			nestErr = err
		}
		stats = append(stats, s)
	})
	if err == nil {
		err = nestErr
	}
	if err != nil {
		return nil, err
	}
	return stats, nil
}
