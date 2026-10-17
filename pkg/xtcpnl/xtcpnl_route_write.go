package xtcpnl

import (
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// This file encodes the requests `ip route add/change/replace/prepend/
// append/test/delete` sends: the rtmsg, the RTA_* attributes, and the
// RTA_METRICS nest of RTAX_* metrics. Phases 2 and 3 of
// docs/netlink/route-write-path.md.
//
// It encodes; it does not parse argv. iproute_modify (ip/iproute.c:1158-1645)
// writes each attribute as its keyword is read, so the order is argv order and
// that is internal/goip's business. A RouteWrite carries the attributes as an
// ordered list, RTA_OIF included where iproute_modify appends it after the
// loop, and this file writes them as given. What this file owns is the layout
// iproute_modify imposes after parsing: the attributes, then one RTA_METRICS
// holding the metrics in the order they were read and a trailing RTAX_LOCK.
//
// Nothing here sends. BuildRouteWriteRequest goes through BuildWriteRequest,
// and goip writes the result to a file (link-write-path.md, decision 5).

const (
	// routeWriteBufCst is iproute_modify's char buf[4096], the room after the
	// nlmsghdr and rtmsg that addattr_l is given as maxlen.
	routeWriteBufCst = 4096
	// routeMetricsBufCst is iproute_modify's char mxbuf[256], which holds the
	// RTA_METRICS header as well as the metrics, so the payload has 4 bytes
	// less.
	routeMetricsBufCst = 256 - RTAttrSizeCst
)

// ErrRouteWriteType is a RouteWrite whose MsgType is not RTM_NEWROUTE or
// RTM_DELROUTE. BuildWriteRequest would admit link and address types too; a
// route write must not carry one.
var ErrRouteWriteType = errors.New("xtcpnl: a route write is RTM_NEWROUTE or RTM_DELROUTE")

// RouteAttr is one RTA_* attribute of a route write, payload already encoded.
type RouteAttr struct {
	Type    uint16
	Payload []byte
}

// RouteAttrU32 is addattr32: RTA_OIF, RTA_PRIORITY, RTA_TABLE.
func RouteAttrU32(atype uint16, v uint32) RouteAttr {
	p := make([]byte, 4)
	binary.LittleEndian.PutUint32(p, v)
	return RouteAttr{Type: atype, Payload: p}
}

// RouteAttrBytes is addattr_l of raw bytes: RTA_DST, RTA_SRC, RTA_GATEWAY,
// RTA_PREFSRC (the address bytes), and RTA_VIA (a u16 family, then the
// address).
func RouteAttrBytes(atype uint16, b []byte) RouteAttr {
	return RouteAttr{Type: atype, Payload: b}
}

// RouteMetric is one RTAX_* sub-attribute of RTA_METRICS. Every metric is a
// u32 except RTAX_CC_ALGO, which is Str.
type RouteMetric struct {
	Type uint16
	U32  uint32
	Str  string
}

// RouteWrite is one route write as iproute_modify assembles it.
type RouteWrite struct {
	// MsgType is RTM_NEWROUTE (every verb but delete) or RTM_DELROUTE.
	MsgType uint16
	// Flags are the NLM_F_* bits besides NLM_F_REQUEST: the verb's, from
	// do_iproute, and NLM_F_ACK, which rtnl_talk adds.
	Flags uint16

	// The struct rtmsg, field for field (include/uapi/linux/rtnetlink.h).
	Family   uint8
	DstLen   uint8
	SrcLen   uint8
	Tos      uint8
	Table    uint8
	Protocol uint8
	Scope    uint8
	Type     uint8
	RtmFlags uint32

	// Attrs are the plain attributes in wire order, RTA_OIF included.
	Attrs []RouteAttr

	// Metrics are the RTAX_* sub-attributes in the order they were read.
	// A repeated keyword is two entries: rta_addattr32 appends, it does
	// not replace.
	Metrics []RouteMetric
	// MetricLock is the RTAX_LOCK bitmask, 1<<RTAX_* per locked metric. It
	// is written after the metrics, and only when non-zero.
	MetricLock uint32
}

// EncodeRtMsg lays out a struct rtmsg.
func EncodeRtMsg(w *RouteWrite) []byte {
	b := make([]byte, 0, RtMsgSizeCst)
	b = append(b, w.Family, w.DstLen, w.SrcLen, w.Tos,
		w.Table, w.Protocol, w.Scope, w.Type)
	return binary.LittleEndian.AppendUint32(b, w.RtmFlags)
}

// EncodeRouteMetrics is the payload of RTA_METRICS: each metric in order,
// then RTAX_LOCK when lock is non-zero. With neither it is nil, because
// iproute_modify adds RTA_METRICS only when mxbuf holds something
// (ip/iproute.c:1595-1599), never as an empty nest.
//
// The sub-attributes have no NLA_F_NESTED on the nest and are plain RTAX_*
// types: rta_addattr32 and rta_addattr_l write the type as given.
// RTAX_CC_ALGO is rta_addattr_l(…, strlen), so the name has no NUL and
// rta_len is 4+len; the padding after it is zero and not counted.
//
// The lock-with-no-metrics case has no argv that produces it (every metric
// keyword that sets a lock bit also writes its metric), so it is encoded as
// asked rather than refused.
func EncodeRouteMetrics(ms []RouteMetric, lock uint32) ([]byte, error) {
	if len(ms) == 0 && lock == 0 {
		return nil, nil
	}
	var raw [routeMetricsBufCst]byte
	ab := NewAttrBuilder(raw[:])
	for _, m := range ms {
		var err error
		if m.Type == uint16(unix.RTAX_CC_ALGO) {
			err = ab.PutBytes(m.Type, []byte(m.Str))
		} else {
			err = ab.PutU32(m.Type, m.U32)
		}
		if err != nil {
			// rta_addattr32 prints and drops a metric past mxbuf; `ip`
			// then sends the request without it. Refusing is the one
			// divergence, and it takes over thirty metrics to reach.
			return nil, err
		}
	}
	if lock != 0 {
		if err := ab.PutU32(uint16(unix.RTAX_LOCK), lock); err != nil {
			return nil, err
		}
	}
	return append([]byte(nil), ab.Bytes()...), nil
}

// BuildRouteWriteRequest lays out w: the rtmsg, w.Attrs in order, then
// RTA_METRICS when there are metrics.
func BuildRouteWriteRequest(w RouteWrite, seq uint32) ([]byte, error) {
	if w.MsgType != uint16(unix.RTM_NEWROUTE) && w.MsgType != uint16(unix.RTM_DELROUTE) {
		return nil, ErrRouteWriteType
	}
	mx, err := EncodeRouteMetrics(w.Metrics, w.MetricLock)
	if err != nil {
		return nil, err
	}

	var raw [routeWriteBufCst]byte
	ab := NewAttrBuilder(raw[:])
	for _, a := range w.Attrs {
		if err := ab.PutBytes(a.Type, a.Payload); err != nil {
			return nil, err
		}
	}
	if mx != nil {
		if err := ab.PutBytes(uint16(unix.RTA_METRICS), mx); err != nil {
			return nil, err
		}
	}
	return BuildWriteRequest(w.MsgType, w.Flags, seq, EncodeRtMsg(&w), ab.Bytes())
}
