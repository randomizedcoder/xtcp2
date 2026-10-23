package xtcpnl

import (
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// This file decodes RTM_NEWSTATS, the reply to the RTM_GETSTATS dump (and point
// get) that `ip stats show` issues. goip grounds the link group
// (IFLA_STATS_LINK_64, a rtnl_link_stats64), the xstats group
// (IFLA_STATS_LINK_XSTATS → bridge vlan + mcast in xtcpnl_bridge_xstats.go, bond
// 802.3ad in xtcpnl_bond_xstats.go) and the same bodies under xstats_slave
// (IFLA_STATS_LINK_XSTATS_SLAVE); the remaining leaves (bridge stp, offload,
// afstats mpls) are refused rather than decoded. See internal/goip/obj_stats.go for the
// transaction and render/stats.go + render/bridge_xstats.go for the output.

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

	// StatsFilterXstats is IFLA_STATS_FILTER_BIT(IFLA_STATS_LINK_XSTATS) = bit 1
	// = 0x2, the group `ip stats show group xstats` requests. The whole-group
	// request carries only this top-level bit, no IFLA_STATS_GET_FILTERS nest
	// (ipstats_req_add_filters, ip/ipstats.c:793-828).
	StatsFilterXstats uint32 = 1 << (unix.IFLA_STATS_LINK_XSTATS - 1)

	// StatsFilterXstatsSlave is IFLA_STATS_FILTER_BIT(IFLA_STATS_LINK_XSTATS_SLAVE)
	// = bit 2 = 0x4, the group `ip stats show group xstats_slave` requests, again
	// with no filters nest.
	StatsFilterXstatsSlave uint32 = 1 << (unix.IFLA_STATS_LINK_XSTATS_SLAVE - 1)

	// StatsFilterAfSpec is IFLA_STATS_FILTER_BIT(IFLA_STATS_AF_SPEC) = bit 4 =
	// 0x10, the group `ip link afstats` requests (ip/iplink.c:1670).
	StatsFilterAfSpec uint32 = 1 << (unix.IFLA_STATS_AF_SPEC - 1)
)

// ErrIfStatsMsgSmall is a reply body shorter than the fixed if_stats_msg header.
var ErrIfStatsMsgSmall = errors.New("data too small for IfStatsMsg")

// XstatsBodies is one xstats nest decoded: IFLA_STATS_LINK_XSTATS, or
// IFLA_STATS_LINK_XSTATS_SLAVE, which carries the same LINK_XSTATS_TYPE_* inner
// nests and which iproute2 prints with the same callbacks
// (ip/iplink_bridge.c, ip/iplink_bond.c ipstats_stat_desc_xstats_slave_*).
type XstatsBodies struct {
	// Bridge xstats (LINK_XSTATS_TYPE_BRIDGE). goip grounds the vlan and mcast
	// bodies; HasBridgeXstats records the type was present even when neither
	// body was (an empty nest still prints headers).
	HasBridgeXstats bool
	BridgeVlans     []BridgeVlanXstats
	HasBridgeVlan   bool
	BridgeMcast     BrMcastStats
	HasBridgeMcast  bool
	BridgeStp       BridgeStpXstats
	HasBridgeStp    bool

	// Bond3ad is LINK_XSTATS_TYPE_BOND → BOND_XSTATS_3AD, nil when absent.
	Bond3ad *Bond3adStats

	// HasUngroundedXstatsBody records a body goip does not ground: a second
	// BOND_XSTATS_3AD (the kernel puts one; iproute2 would print each).
	// obj_stats refuses such a record, and one with HasBridgeStp.
	HasUngroundedXstatsBody bool
}

// IfStatsInfo is a decoded RTM_NEWSTATS reply for one interface: the link group
// (Link64 + HasLink64), the xstats group (the embedded XstatsBodies) and the
// xstats_slave group (Slave, HasXstatsSlave). HasUnsupportedGroup records that
// the reply also carried a stat group goip does not ground (offload, afstats);
// obj_stats refuses such a record rather than silently under-rendering it, which
// is the path a bare `stats show` on a bridge takes.
type IfStatsInfo struct {
	Family     uint8
	Ifindex    uint32
	FilterMask uint32

	Link64    RtnlLinkStats64
	HasLink64 bool

	XstatsBodies

	Slave          XstatsBodies
	HasXstatsSlave bool

	HasUnsupportedGroup bool

	// AfSpec is the member types of IFLA_STATS_AF_SPEC, in wire order: one
	// address family each (AF_MPLS the only one iproute2 prints). HasAfSpec
	// records the nest even when it is empty, as it is for every device
	// without MPLS. The group still sets HasUnsupportedGroup, for obj_stats.
	AfSpec    []uint16
	HasAfSpec bool
	// AfSpecMalformed records a nest whose walk stopped at a malformed member.
	AfSpecMalformed bool

	// HwSInfo is IFLA_STATS_LINK_OFFLOAD_XSTATS's HW_S_INFO nest, the one
	// offload body `ip monitor stats` prints (ipstats_print, ip/ipstats.c);
	// HasHwSInfo records the nest. The group still sets HasUnsupportedGroup,
	// for obj_stats.
	HwSInfo    HwSInfo
	HasHwSInfo bool
	// HwSInfoMalformed records an offload nest whose walk stopped at a
	// malformed attribute.
	HwSInfoMalformed bool
}

// HwSInfo is IFLA_OFFLOAD_XSTATS_HW_S_INFO: its L3_STATS member's REQUEST
// and USED bytes, each with its presence, as ipstats_dissect_hw_s_info_one
// reads them. A later duplicate replaces an earlier one, as parse_rtattr's
// table does.
type HwSInfo struct {
	HasL3        bool
	L3Request    uint8
	HasL3Request bool
	L3Used       uint8
	HasL3Used    bool
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
// get_rtnl_link_stats_rta does. The xstats and xstats_slave groups are descended
// by parseXstats into the embedded XstatsBodies and Slave. The remaining groups
// (offload, afstats) set HasUnsupportedGroup; unknown types are ignored.
func ParseNewStats(body []byte) (IfStatsInfo, error) {
	var si IfStatsInfo
	var h IfStatsMsg
	if _, err := DeserializeIfStatsMsg(body, &h); err != nil {
		return si, err
	}
	si.Family = h.Family
	si.Ifindex = h.Ifindex
	si.FilterMask = h.FilterMask

	var nestErr error
	err := WalkRTAttrs(body[IfStatsMsgSizeCst:], func(atype uint16, val []byte) {
		switch atype {
		case uint16(unix.IFLA_STATS_LINK_64):
			if _, derr := DeserializeRtnlLinkStats64(val, &si.Link64); derr == nil {
				si.HasLink64 = true
			}
		case uint16(unix.IFLA_STATS_LINK_XSTATS):
			if e := si.parseXstats(val); e != nil && nestErr == nil {
				nestErr = e
			}
		case uint16(unix.IFLA_STATS_LINK_XSTATS_SLAVE):
			si.HasXstatsSlave = true
			if e := si.Slave.parseXstats(val); e != nil && nestErr == nil {
				nestErr = e
			}
		case uint16(unix.IFLA_STATS_AF_SPEC):
			// print_af_stats_attr's RTA_OK loop stops at a malformed member
			// rather than failing the record, so the walk's error is dropped
			// and the members before it are kept.
			si.HasUnsupportedGroup, si.HasAfSpec = true, true
			if e := WalkRTAttrsNested(val, func(af uint16, _ []byte) {
				si.AfSpec = append(si.AfSpec, af)
			}); e != nil {
				si.AfSpecMalformed = true
			}
		case uint16(unix.IFLA_STATS_LINK_OFFLOAD_XSTATS):
			si.HasUnsupportedGroup = true
			si.parseOffload(val)
		}
	})
	if err != nil {
		return si, err
	}
	return si, nestErr
}

// parseOffload descends the offload group to HW_S_INFO's L3_STATS. iproute2
// reads these nests with parse_rtattr, which stops quietly at a malformed
// attribute, so a walk that fails keeps what came before it and records the
// failure in HwSInfoMalformed.
func (si *IfStatsInfo) parseOffload(val []byte) {
	var inner error
	outer := WalkRTAttrsNested(val, func(otype uint16, hw []byte) {
		if otype != unix.IFLA_OFFLOAD_XSTATS_HW_S_INFO {
			return
		}
		si.HasHwSInfo = true
		inner = errors.Join(inner, WalkRTAttrsNested(hw, func(htype uint16, l3 []byte) {
			if htype != unix.IFLA_OFFLOAD_XSTATS_L3_STATS {
				return
			}
			si.HwSInfo = HwSInfo{HasL3: true}
			inner = errors.Join(inner, si.HwSInfo.parseL3(l3))
		}))
	})
	si.HwSInfoMalformed = outer != nil || inner != nil
}

// parseL3 is ipstats_dissect_hw_s_info_one's two rta_getattr_u8 reads; an
// empty attribute reads as absent, since no byte is there to read.
func (h *HwSInfo) parseL3(l3 []byte) error {
	return WalkRTAttrsNested(l3, func(atype uint16, v []byte) {
		if len(v) == 0 {
			return
		}
		switch atype {
		case unix.IFLA_OFFLOAD_XSTATS_HW_S_INFO_REQUEST:
			h.L3Request, h.HasL3Request = v[0], true
		case unix.IFLA_OFFLOAD_XSTATS_HW_S_INFO_USED:
			h.L3Used, h.HasL3Used = v[0], true
		}
	})
}

// parseXstats descends one xstats nest. LINK_XSTATS_TYPE_BRIDGE yields the vlan
// and mcast bodies (each BRIDGE_XSTATS_VLAN attr is one VLAN and appends to
// BridgeVlans) and the stp body;
// LINK_XSTATS_TYPE_BOND yields BOND_XSTATS_3AD. HasBridgeXstats marks the bridge
// type present even when it carried no body goip renders. A malformed nest
// returns the rtattr error, as the top-level walk does.
func (x *XstatsBodies) parseXstats(val []byte) error {
	var innerErr error
	keep := func(e error) {
		if e != nil && innerErr == nil {
			innerErr = e
		}
	}
	outerErr := WalkRTAttrsNested(val, func(ltype uint16, inner []byte) {
		switch ltype {
		case LinkXstatsTypeBridge:
			x.HasBridgeXstats = true
			keep(WalkRTAttrsNested(inner, func(btype uint16, body []byte) {
				switch btype {
				case BridgeXstatsVlan:
					var v BridgeVlanXstats
					if _, derr := DeserializeBridgeVlanXstats(body, &v); derr == nil {
						x.BridgeVlans = append(x.BridgeVlans, v)
						x.HasBridgeVlan = true
					}
				case BridgeXstatsMcast:
					if _, derr := DeserializeBrMcastStats(body, &x.BridgeMcast); derr == nil {
						x.HasBridgeMcast = true
					}
				case BridgeXstatsStp:
					if _, derr := DeserializeBridgeStpXstats(body, &x.BridgeStp); derr == nil {
						x.HasBridgeStp = true
					}
				}
			}))
		case LinkXstatsTypeBond:
			keep(WalkRTAttrsNested(inner, func(btype uint16, body []byte) {
				if btype != BondXstats3ad {
					return
				}
				if x.Bond3ad != nil {
					x.HasUngroundedXstatsBody = true
					return
				}
				s, e := ParseBond3adStats(body)
				keep(e)
				x.Bond3ad = s
			}))
		}
	})
	if outerErr != nil {
		return outerErr
	}
	return innerErr
}
