package xtcpnl

import (
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// This file decodes RTM_NEWSTATS, the reply to the RTM_GETSTATS dump (and point
// get) that `ip stats show` issues. goip grounds only the link group
// (IFLA_STATS_LINK_64, a rtnl_link_stats64); the other twelve leaves of
// ipstats's descriptor tree (bridge/bond xstats, offload, afstats mpls) are
// refused rather than decoded. See internal/goip/obj_stats.go for the
// transaction and render/stats.go for the output.

// IfStatsMsg mirrors the kernel's `struct if_stats_msg`, the fixed header of an
// RTM_*STATS message.
//
//	struct if_stats_msg {
//		__u8	family;
//		__u8	pad1;
//		__u16	pad2;
//		__u32	ifindex;
//		__u32	filter_mask;
//	};
//
// It is 12 bytes, already 4-aligned, so the attributes begin at
// IfStatsMsgSizeCst. Unlike the 4-byte ndtmsg/netconfmsg headers this one
// carries a filter_mask selecting which stat groups the kernel returns.
//
// Reference: linux/include/uapi/linux/if_link.h
type IfStatsMsg struct {
	Family     uint8
	Ifindex    uint32
	FilterMask uint32
}

const (
	// IfStatsMsgSizeCst is sizeof(struct if_stats_msg), already 4-aligned.
	IfStatsMsgSizeCst = 12

	// StatsFilterLink64 is IFLA_STATS_FILTER_BIT(IFLA_STATS_LINK_64) =
	// 1 << (IFLA_STATS_LINK_64 - 1) = bit 0 (if_link.h:1904). It selects the
	// link group alone; the full default `ip stats show` mask is 0x1F (all five
	// groups), which goip does not request — see obj_stats.go.
	StatsFilterLink64 uint32 = 1 << (unix.IFLA_STATS_LINK_64 - 1)
)

// ErrIfStatsMsgSmall is a reply body shorter than the fixed if_stats_msg header.
var ErrIfStatsMsgSmall = errors.New("data too small for IfStatsMsg")

// IfStatsInfo is a decoded RTM_NEWSTATS reply for one interface. goip renders
// only the link group, so Link64 (+HasLink64) is the one decoded payload.
// HasUnsupportedGroup records that the reply also carried a stat group goip does
// not ground (bridge/bond xstats, offload, afstats); obj_stats refuses such a
// record rather than silently under-rendering it, which is the path a bare
// `stats show` on a bridge takes.
type IfStatsInfo struct {
	Family     uint8
	Ifindex    uint32
	FilterMask uint32

	Link64    RtnlLinkStats64
	HasLink64 bool

	HasUnsupportedGroup bool
}

// DeserializeIfStatsMsg does an offset read of the fixed if_stats_msg header.
// The two pad bytes are ignored; the kernel leaves them zero.
func DeserializeIfStatsMsg(data []byte, h *IfStatsMsg) (n int, err error) {
	if len(data) < IfStatsMsgSizeCst {
		return 0, ErrIfStatsMsgSmall
	}
	h.Family = data[0]
	h.Ifindex = binary.LittleEndian.Uint32(data[4:8])
	h.FilterMask = binary.LittleEndian.Uint32(data[8:12])
	return IfStatsMsgSizeCst, nil
}

// ParseNewStats decodes an RTM_NEWSTATS reply body: the if_stats_msg header then
// its IFLA_STATS_* attributes. The link group (IFLA_STATS_LINK_64) is read via
// DeserializeRtnlLinkStats64, which tolerates a short or long payload exactly as
// get_rtnl_link_stats_rta does. Any other group attribute (2..5) sets
// HasUnsupportedGroup; unknown types are ignored.
func ParseNewStats(body []byte) (IfStatsInfo, error) {
	var si IfStatsInfo
	var h IfStatsMsg
	if _, err := DeserializeIfStatsMsg(body, &h); err != nil {
		return si, err
	}
	si.Family = h.Family
	si.Ifindex = h.Ifindex
	si.FilterMask = h.FilterMask

	err := WalkRTAttrs(body[IfStatsMsgSizeCst:], func(atype uint16, val []byte) {
		switch atype {
		case uint16(unix.IFLA_STATS_LINK_64):
			if _, derr := DeserializeRtnlLinkStats64(val, &si.Link64); derr == nil {
				si.HasLink64 = true
			}
		case uint16(unix.IFLA_STATS_LINK_XSTATS),
			uint16(unix.IFLA_STATS_LINK_XSTATS_SLAVE),
			uint16(unix.IFLA_STATS_LINK_OFFLOAD_XSTATS),
			uint16(unix.IFLA_STATS_AF_SPEC):
			si.HasUnsupportedGroup = true
		}
	})
	if err != nil {
		return si, err
	}
	return si, nil
}
