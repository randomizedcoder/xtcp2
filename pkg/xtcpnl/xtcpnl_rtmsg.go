package xtcpnl

import (
	"bytes"
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

func DeserializeRtMsgReflection(data []byte, m *RtMsg) (n int, err error) {
	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, m)
	if err != nil {
		return 0, err
	}

	return RtMsgReadCst, err
}

// RouteInfo is the subset of an RTM_NEWROUTE message xtcp2 keeps. DstLen and the
// header table/scope/type come from the rtmsg header; Dst/Gateway/PrefSrc hold
// raw network-order address bytes. Table is upgraded from RTA_TABLE when present
// (full table ids exceed the 8-bit header field). The connected-subnet test is
// Type==RTN_UNICAST && Gateway==nil && has a Dst prefix (NOT scope-gated: IPv4
// connected subnets are scope-link but IPv6 connected subnets are
// scope-universe); a locally-attached address is Type==RTN_LOCAL (typically in
// RT_TABLE_LOCAL, scope host).
type RouteInfo struct {
	Family   uint8
	DstLen   uint8
	Table    uint32 // header rtm_table, upgraded by RTA_TABLE
	Scope    uint8
	Type     uint8
	Protocol uint8
	Dst      []byte // RTA_DST
	Gateway  []byte // RTA_GATEWAY
	PrefSrc  []byte // RTA_PREFSRC
	Oif      uint32 // RTA_OIF
	Priority uint32 // RTA_PRIORITY
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
		Table:    uint32(m.Table),
		Scope:    m.Scope,
		Type:     m.Type,
		Protocol: m.Protocol,
	}
	err := walkRTAttrs(body[RtMsgSizeCst:], func(atype uint16, val []byte) {
		switch atype {
		case uint16(unix.RTA_DST):
			ri.Dst = copyBytes(val)
		case uint16(unix.RTA_GATEWAY):
			ri.Gateway = copyBytes(val)
		case uint16(unix.RTA_PREFSRC):
			ri.PrefSrc = copyBytes(val)
		case uint16(unix.RTA_OIF):
			if len(val) >= 4 {
				ri.Oif = binary.LittleEndian.Uint32(val[0:4])
			}
		case uint16(unix.RTA_PRIORITY):
			if len(val) >= 4 {
				ri.Priority = binary.LittleEndian.Uint32(val[0:4])
			}
		case uint16(unix.RTA_TABLE):
			if len(val) >= 4 {
				ri.Table = binary.LittleEndian.Uint32(val[0:4])
			}
		}
	})
	if err != nil {
		return RouteInfo{}, err
	}
	return ri, nil
}
