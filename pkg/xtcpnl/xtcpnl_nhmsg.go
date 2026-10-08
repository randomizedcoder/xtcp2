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
	NhaBlackhole uint16 = 4  // NHA_BLACKHOLE: a flag attribute, no payload
	NhaOIF       uint16 = 5  // NHA_OIF: the outgoing interface index
	NhaGateway   uint16 = 6  // NHA_GATEWAY: the next hop, network order
	NhaOpFlags   uint16 = 14 // NHA_OP_FLAGS: request-only operation filter
)

var (
	// ErrNhmsgSmall is a reply body shorter than the fixed nhmsg header.
	ErrNhmsgSmall = errors.New("data too small for Nhmsg")
)

// NexthopInfo is a decoded RTM_NEWNEXTHOP reply: the nhmsg header fields worth
// keeping plus the attributes a single (non-group) nexthop carries.
//
// A group nexthop sets HasGroup and references OTHER ids that iproute2 fetches
// and renders recursively; goip keeps the flag so the renderer can decline a
// shape it has no fixture for rather than emit a wrong line. See
// render.NexthopInfoText.
type NexthopInfo struct {
	Family    uint8
	Scope     uint8
	Protocol  uint8
	Flags     uint32
	ID        uint32
	OIF       int32  // 0 is absent: index 0 is not a device
	Gateway   []byte // network order, nil when absent
	Blackhole bool
	HasGroup  bool
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
		}
	})
	if err != nil {
		return nh, err
	}
	return nh, nil
}
