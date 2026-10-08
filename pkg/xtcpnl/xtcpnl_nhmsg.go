package xtcpnl

import (
	"encoding/binary"
	"errors"
)

// This file decodes RTM_NEWNEXTHOP, the reply to the RTM_GETNEXTHOP single-get
// that `ip -d route show` issues for a route carrying RTA_NH_ID. The route dump
// prints `nhid %u` from the attribute alone; -d follows it into the nexthop
// object the id names and renders that object on its own `nh_info` line
// (ip/iproute.c:1002, print_cache_nexthop_id -> ipnh_cache_add). See
// internal/goip/obj_route.go for the transaction and render/route.go for the
// line.

// Nhmsg mirrors the kernel's `struct nhmsg`, the fixed header of an
// RTM_*NEXTHOP message.
//
//	struct nhmsg {
//		unsigned char	nh_family;
//		unsigned char	nh_scope;    /* return only */
//		unsigned char	nh_protocol; /* Routing protocol that installed nh */
//		unsigned char	resvd;
//		unsigned int	nh_flags;    /* RTNH_F flags */
//	};
//
// Reference: linux/include/uapi/linux/nexthop.h
type Nhmsg struct {
	Family   uint8
	Scope    uint8
	Protocol uint8
	Resvd    uint8
	Flags    uint32
}

const (
	// NhMsgSizeCst is sizeof(struct nhmsg): four bytes plus a u32.
	NhMsgSizeCst = 8

	// NHA_* from include/uapi/linux/nexthop.h. Declared here because
	// golang.org/x/sys/unix's pinned version stops at NHA_MASTER and does not
	// carry NhaOpFlags, as RtaNhID is declared in xtcpnl_rtmsg.go.
	NhaID        uint16 = 1  // NHA_ID: the nexthop's own id
	NhaGroup     uint16 = 2  // NHA_GROUP: a group of other nexthops
	NhaGroupType uint16 = 3  // NHA_GROUP_TYPE: u16, NEXTHOP_GRP_TYPE_*
	NhaBlackhole uint16 = 4  // NHA_BLACKHOLE: a flag attribute, no payload
	NhaOIF       uint16 = 5  // NHA_OIF: the outgoing interface index
	NhaGateway   uint16 = 6  // NHA_GATEWAY: the next hop, network order
	NhaResGroup  uint16 = 12 // NHA_RES_GROUP: nested resilient-group args
	NhaOpFlags   uint16 = 14 // NHA_OP_FLAGS: op filter (request) / resvd (reply)

	// NexthopGrpTypeMpath is NEXTHOP_GRP_TYPE_MPATH, the default group type that
	// iproute2 prints no `type` token for.
	NexthopGrpTypeMpath uint16 = 0

	// NhaOpFlagRespGrpResvd0 is NHA_OP_FLAG_RESP_GRP_RESVD_0: when the reply sets
	// it in NHA_OP_FLAGS, a group entry's weight_high byte is meaningful.
	NhaOpFlagRespGrpResvd0 uint32 = 0x80000000

	// NexthopGrpSizeCst is sizeof(struct nexthop_grp): u32 id, u8 weight,
	// u8 weight_high, u16 resvd.
	NexthopGrpSizeCst = 8
)

var (
	// ErrNhmsgSmall is a reply body shorter than the fixed nhmsg header.
	ErrNhmsgSmall = errors.New("data too small for Nhmsg")

	// ErrNhGroupBad is an NHA_GROUP payload that is not a whole number of
	// nexthop_grp entries.
	ErrNhGroupBad = errors.New("NHA_GROUP payload not a multiple of nexthop_grp")
)

// GroupMember is one entry of an NHA_GROUP array: a member nexthop id and its
// decoded weight (1-based, weight_high folded in per nhgrp_weight).
type GroupMember struct {
	ID     uint32
	Weight uint16
}

// NexthopInfo is a decoded RTM_NEWNEXTHOP reply: the nhmsg header fields worth
// keeping plus the attributes a nexthop carries.
//
// A group nexthop sets HasGroup and lists its member ids in Group; GroupType
// distinguishes mpath from resilient. The render layer renders mpath groups and
// declines resilient/unknown ones (HasResGroup or a non-mpath GroupType), which
// carry live-ticking args no fixture grounds. See render.NexthopText.
type NexthopInfo struct {
	Family      uint8
	Scope       uint8
	Protocol    uint8
	Flags       uint32
	ID          uint32
	OIF         int32  // 0 is absent: index 0 is not a device
	Gateway     []byte // network order, nil when absent
	Blackhole   bool
	HasGroup    bool
	Group       []GroupMember // NHA_GROUP members, nil when not a group
	GroupType   uint16        // NHA_GROUP_TYPE, NEXTHOP_GRP_TYPE_*
	HasResGroup bool          // NHA_RES_GROUP present (resilient)
	RespOpFlags uint32        // NHA_OP_FLAGS from the reply (weight gate)
}

// DeserializeNhmsg does an offset read of the fixed nhmsg header.
func DeserializeNhmsg(data []byte, h *Nhmsg) (n int, err error) {
	if len(data) < NhMsgSizeCst {
		return 0, ErrNhmsgSmall
	}
	h.Family = data[0]
	h.Scope = data[1]
	h.Protocol = data[2]
	h.Resvd = data[3]
	h.Flags = binary.LittleEndian.Uint32(data[4:8])
	return NhMsgSizeCst, nil
}

// ParseNewNexthop decodes an RTM_NEWNEXTHOP reply body: the nhmsg header then
// its NHA_* attributes. Gateway is copied, so it does not alias the buffer.
func ParseNewNexthop(body []byte) (NexthopInfo, error) {
	var nh NexthopInfo
	var h Nhmsg
	if _, err := DeserializeNhmsg(body, &h); err != nil {
		return nh, err
	}
	nh.Family = h.Family
	nh.Scope = h.Scope
	nh.Protocol = h.Protocol
	nh.Flags = h.Flags

	var groupRaw []byte // NHA_GROUP bytes, resolved to members after the walk
	err := WalkRTAttrs(body[NhMsgSizeCst:], func(atype uint16, val []byte) {
		switch atype {
		case NhaID:
			if len(val) >= 4 {
				nh.ID = binary.LittleEndian.Uint32(val[0:4])
			}
		case NhaOIF:
			if len(val) >= 4 {
				nh.OIF = int32(binary.LittleEndian.Uint32(val[0:4]))
			}
		case NhaGateway:
			nh.Gateway = CopyBytes(val)
		case NhaBlackhole:
			nh.Blackhole = true
		case NhaGroup:
			nh.HasGroup = true
			groupRaw = CopyBytes(val)
		case NhaGroupType:
			if len(val) >= 2 {
				nh.GroupType = binary.LittleEndian.Uint16(val[0:2])
			}
		case NhaResGroup:
			nh.HasResGroup = true
		case NhaOpFlags:
			if len(val) >= 4 {
				nh.RespOpFlags = binary.LittleEndian.Uint32(val[0:4])
			}
		}
	})
	if err != nil {
		return nh, err
	}

	// Resolve group members now that RespOpFlags (whichever order it arrived in)
	// is known: nhgrp_weight gates weight_high on NHA_OP_FLAG_RESP_GRP_RESVD_0.
	if groupRaw != nil {
		if len(groupRaw) == 0 || len(groupRaw)%NexthopGrpSizeCst != 0 {
			return nh, ErrNhGroupBad
		}
		highOK := nh.RespOpFlags&NhaOpFlagRespGrpResvd0 != 0
		nh.Group = make([]GroupMember, 0, len(groupRaw)/NexthopGrpSizeCst)
		for off := 0; off < len(groupRaw); off += NexthopGrpSizeCst {
			id := binary.LittleEndian.Uint32(groupRaw[off : off+4])
			low := groupRaw[off+4]
			high := groupRaw[off+5]
			var w uint16
			if highOK {
				w = uint16(high)
			}
			nh.Group = append(nh.Group, GroupMember{ID: id, Weight: (w<<8 | uint16(low)) + 1})
		}
	}
	return nh, nil
}
