package xtcpnl

import (
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// RtMsg mirrors the kernel's `struct rtmsg` — the family header of an RTM_*ROUTE
// message.
//
//	struct rtmsg {
//		unsigned char		rtm_family;
//		unsigned char		rtm_dst_len;
//		unsigned char		rtm_src_len;
//		unsigned char		rtm_tos;
//		unsigned char		rtm_table;	/* Routing table id */
//		unsigned char		rtm_protocol;	/* Routing protocol; see below	*/
//		unsigned char		rtm_scope;	/* See below	*/
//		unsigned char		rtm_type;	/* See below	*/
//		unsigned		rtm_flags;
//	};
//
// Reference: https://github.com/torvalds/linux/blob/master/include/uapi/linux/rtnetlink.h
type RtMsg struct {
	Family   uint8  // 1
	DstLen   uint8  // 1
	SrcLen   uint8  // 1
	Tos      uint8  // 1
	Table    uint8  // 1
	Protocol uint8  // 1
	Scope    uint8  // 1
	Type     uint8  // 1
	Flags    uint32 // 4 = 12 ( 12 / 4 = 3 )
}

const (
	RtMsgSizeCst = 12
	RtMsgReadCst = RtMsgSizeCst

	// RtaNhID is RTA_NH_ID from include/uapi/linux/rtnetlink.h (Linux 5.3+):
	// the id of the nexthop object a route points at instead of carrying its
	// own RTA_GATEWAY / RTA_OIF. golang.org/x/sys/unix does not export it.
	RtaNhID uint16 = 30
)

var (
	ErrRtMsgSmall = errors.New("data too small for RtMsg")
)

// DeserializeRtMsg does a binary read of an RtMsg with a basic length check.
func DeserializeRtMsg(data []byte, m *RtMsg) (n int, err error) {
	if len(data) < RtMsgSizeCst {
		return 0, ErrRtMsgSmall
	}

	m.Family = data[0]
	m.DstLen = data[1]
	m.SrcLen = data[2]
	m.Tos = data[3]
	m.Table = data[4]
	m.Protocol = data[5]
	m.Scope = data[6]
	m.Type = data[7]
	m.Flags = binary.LittleEndian.Uint32(data[8:12])

	return RtMsgReadCst, nil
}

// RouteInfo is the subset of an RTM_NEWROUTE message xtcp2 keeps. DstLen and the
// header table/scope/type come from the rtmsg header; Dst/Gateway/PrefSrc hold
// raw network-order address bytes. Table is upgraded from RTA_TABLE when present
// (full table ids exceed the 8-bit header field). The local-subnet test
// (pkg/localnet) is Type==RTN_UNICAST && has a Dst prefix && no next hop, where
// "next hop" is any of Gateway, HasVia, HasMultipath or NhID (NOT scope-gated:
// IPv4 connected subnets are scope-link but IPv6 connected subnets are
// scope-universe); a locally-attached address is Type==RTN_LOCAL (typically in
// RT_TABLE_LOCAL, scope host).
//
// HasMultipath / HasVia / NhID record that the route is reached via a next hop
// expressed outside RTA_GATEWAY. HasMultipath and HasVia are kept as the
// presence predicates the local-subnet test reads, and are exactly
// len(Multipath) > 0 and Via != nil; the contents are in those two fields.
//
// HasPriority is not redundant with Priority != 0. `ip` prints the metric on
// attribute presence, not on value — `if (tb[RTA_PRIORITY] ...) print_uint(...
// "metric %u " ...)` (ip/iproute.c:941-943) — and the committed IPv6 dump
// contains routes that carry RTA_PRIORITY = 0 and are rendered `metric 0`,
// while the IPv4 dump's connected routes omit the attribute and print no metric
// token at all. Collapsing the two would emit `metric 0` on every v4 route.
type RouteInfo struct {
	Family       uint8
	DstLen       uint8
	SrcLen       uint8  // rtm_src_len; part of a route's identity
	Tos          uint8  // rtm_tos; part of a route's identity
	Table        uint32 // header rtm_table, upgraded by RTA_TABLE
	Scope        uint8
	Type         uint8
	Protocol     uint8
	Flags        uint32 // header rtm_flags — RTNH_F_* / RTM_F_*; `ip` renders linkdown, offload, trap
	Dst          []byte // RTA_DST
	Src          []byte // RTA_SRC
	Gateway      []byte // RTA_GATEWAY
	PrefSrc      []byte // RTA_PREFSRC
	Oif          uint32 // RTA_OIF
	Iif          uint32 // RTA_IIF, the input interface of a cloned or multicast route
	Priority     uint32 // RTA_PRIORITY
	HasPriority  bool   // RTA_PRIORITY present — see below, presence is what `ip` keys on
	Pref         uint8  // RTA_PREF, the ICMPv6 router preference (RFC 4191)
	HasPref      bool   // RTA_PREF present
	HasMultipath bool   // RTA_MULTIPATH present (ECMP nexthop list; gateways live inside it)
	HasVia       bool   // RTA_VIA present (gateway of a different address family)
	NhID         uint32 // RTA_NH_ID (nexthop object id; 0 = none)

	// Multipath is the decoded RTA_MULTIPATH nexthop list, nil when absent.
	// An ECMP route's gateways exist ONLY here — see xtcpnl_rtnexthop.go for
	// why not walking it produces a wrong answer rather than a partial one.
	Multipath []RouteNextHop

	// Via is the decoded RTA_VIA payload, nil when absent: a next hop in a
	// different address family from the route (RFC 5549).
	Via *RtVia

	// Metrics is the decoded RTA_METRICS nested stream, nil when absent.
	Metrics *RouteMetrics
}

// ParseNewRoute decodes an RTM_NEWROUTE message body (the bytes after the
// nlmsghdr): the rtmsg header followed by RTA_* attributes.
func ParseNewRoute(body []byte) (RouteInfo, error) {
	var m RtMsg
	if _, err := DeserializeRtMsg(body, &m); err != nil {
		return RouteInfo{}, err
	}

	ri := RouteInfo{
		Family:   m.Family,
		DstLen:   m.DstLen,
		SrcLen:   m.SrcLen,
		Tos:      m.Tos,
		Table:    uint32(m.Table),
		Scope:    m.Scope,
		Type:     m.Type,
		Protocol: m.Protocol,
		Flags:    m.Flags,
	}
	// The nested attributes are decoded by helpers that CAN fail, but
	// WalkRTAttrs's callback has no error return - so the first failure is
	// parked here and returned once the walk finishes. It is returned rather
	// than dropped because a malformed nexthop list changes the meaning of the
	// route, not just the completeness of the struct.
	var nestErr error
	var seen attrSeen
	err := WalkRTAttrs(body[RtMsgSizeCst:], func(atype uint16, val []byte) {
		if !seen.first(atype) {
			return
		}
		switch atype {
		case uint16(unix.RTA_DST):
			ri.Dst = CopyBytes(val)
		case uint16(unix.RTA_SRC):
			ri.Src = CopyBytes(val)
		case uint16(unix.RTA_GATEWAY):
			ri.Gateway = CopyBytes(val)
		case uint16(unix.RTA_PREFSRC):
			ri.PrefSrc = CopyBytes(val)
		case uint16(unix.RTA_OIF):
			if len(val) >= 4 {
				ri.Oif = binary.LittleEndian.Uint32(val[0:4])
			}
		case uint16(unix.RTA_IIF):
			if len(val) >= 4 {
				ri.Iif = binary.LittleEndian.Uint32(val[0:4])
			}
		case uint16(unix.RTA_PRIORITY):
			if len(val) >= 4 {
				ri.Priority = binary.LittleEndian.Uint32(val[0:4])
				ri.HasPriority = true
			}
		case uint16(unix.RTA_PREF):
			if len(val) >= 1 {
				ri.Pref = val[0]
				ri.HasPref = true
			}
		case uint16(unix.RTA_TABLE):
			if len(val) >= 4 {
				ri.Table = binary.LittleEndian.Uint32(val[0:4])
			}
		case uint16(unix.RTA_MULTIPATH):
			ri.HasMultipath = true
			if nestErr != nil {
				return
			}
			nestErr = WalkRouteNextHops(val, func(nh RouteNextHop) {
				ri.Multipath = append(ri.Multipath, nh)
			})
		case uint16(unix.RTA_VIA):
			ri.HasVia = true
			if nestErr != nil {
				return
			}
			var v RtVia
			if _, verr := DeserializeRtVia(val, &v); verr != nil {
				nestErr = verr
				return
			}
			ri.Via = &v
		case uint16(unix.RTA_METRICS):
			if nestErr != nil {
				return
			}
			mx, merr := ParseRouteMetrics(val)
			if merr != nil {
				nestErr = merr
				return
			}
			ri.Metrics = mx
		case RtaNhID:
			if len(val) >= 4 {
				ri.NhID = binary.LittleEndian.Uint32(val[0:4])
			}
		}
	})
	if err != nil {
		return RouteInfo{}, err
	}
	if nestErr != nil {
		return RouteInfo{}, nestErr
	}
	return ri, nil
}
