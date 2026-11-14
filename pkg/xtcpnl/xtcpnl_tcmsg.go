package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
)

// This file decodes the two fixed headers of tc's rtnetlink messages and
// their top-level attributes: struct tcmsg, which RTM_*QDISC, RTM_*TCLASS,
// RTM_*TFILTER and RTM_*CHAIN carry, and struct tcamsg, which RTM_*ACTION
// carries with each action in a TCA_ROOT_TAB nest. TCA_OPTIONS and
// TCA_ACT_OPTIONS are per-kind and kept as raw bytes here; the workbench's
// tdc corpus (pkg/xtcpnl/testdata/<kernel>/ws/tdc/, docs/netlink/
// coverage-status.md "Workbench and generated rows") is what they are
// checked against.

// Tcmsg mirrors the kernel's `struct tcmsg`.
//
//	struct tcmsg {
//		unsigned char	tcm_family;
//		unsigned char	tcm__pad1;
//		unsigned short	tcm__pad2;
//		int		tcm_ifindex;
//		__u32		tcm_handle;
//		__u32		tcm_parent;  /* tcm_block_index when ifindex is TCM_IFINDEX_MAGIC_BLOCK */
//		__u32		tcm_info;
//	};
//
// Reference: linux/include/uapi/linux/rtnetlink.h. Ifindex is kept as the
// raw u32 because TCM_IFINDEX_MAGIC_BLOCK is 0xFFFFFFFF.
type Tcmsg struct {
	Family  uint8
	Pad1    uint8
	Pad2    uint16
	Ifindex uint32
	Handle  uint32
	Parent  uint32
	Info    uint32
}

const (
	// TcMsgSizeCst is sizeof(struct tcmsg).
	TcMsgSizeCst = 20
	// TcaMsgSizeCst is sizeof(struct tcamsg): a family byte and three pad bytes.
	TcaMsgSizeCst = 4

	// TcmIfindexMagicBlock is TCM_IFINDEX_MAGIC_BLOCK: tcm_parent holds a
	// shared block's index rather than a parent handle.
	TcmIfindexMagicBlock uint32 = 0xFFFFFFFF

	// TCA_* from include/uapi/linux/rtnetlink.h.
	TcaKind          uint16 = 1  // TCA_KIND: the qdisc, class or filter kind, a string
	TcaOptions       uint16 = 2  // TCA_OPTIONS: the kind's own nest
	TcaStats         uint16 = 3  // TCA_STATS: struct tc_stats
	TcaXstats        uint16 = 4  // TCA_XSTATS: the kind's own statistics
	TcaRate          uint16 = 5  // TCA_RATE: struct tc_estimator
	TcaFcnt          uint16 = 6  // TCA_FCNT
	TcaStats2        uint16 = 7  // TCA_STATS2: nested TCA_STATS_*
	TcaStab          uint16 = 8  // TCA_STAB: size table
	TcaPad           uint16 = 9  // TCA_PAD
	TcaDumpInvisible uint16 = 10 // TCA_DUMP_INVISIBLE: flag
	TcaChain         uint16 = 11 // TCA_CHAIN: u32 filter chain index
	TcaHwOffload     uint16 = 12 // TCA_HW_OFFLOAD: u8
	TcaIngressBlock  uint16 = 13 // TCA_INGRESS_BLOCK: u32 shared block index
	TcaEgressBlock   uint16 = 14 // TCA_EGRESS_BLOCK: u32 shared block index
	TcaDumpFlags     uint16 = 15 // TCA_DUMP_FLAGS: nla_bitfield32
	TcaExtWarnMsg    uint16 = 16 // TCA_EXT_WARN_MSG: string
	// TcaMaxKnown is the last TCA_* this file knows (TCA_MAX at 7.3.0).
	TcaMaxKnown = TcaExtWarnMsg

	// TCA_ROOT_* from include/uapi/linux/rtnetlink.h: the top level of an
	// RTM_*ACTION message.
	TcaRootTab        uint16 = 1 // TCA_ROOT_TAB (TCA_ACT_TAB): nest of actions by order
	TcaRootFlags      uint16 = 2 // TCA_ROOT_FLAGS: nla_bitfield32
	TcaRootCount      uint16 = 3 // TCA_ROOT_COUNT: u32, actions in a dump
	TcaRootTimeDelta  uint16 = 4 // TCA_ROOT_TIME_DELTA: u32 msecs
	TcaRootExtWarnMsg uint16 = 5 // TCA_ROOT_EXT_WARN_MSG: string
	TcaRootMaxKnown          = TcaRootExtWarnMsg

	// TCA_ACT_* from include/uapi/linux/pkt_cls.h: one action's nest.
	TcaActKind        uint16 = 1  // TCA_ACT_KIND: string
	TcaActOptions     uint16 = 2  // TCA_ACT_OPTIONS: the kind's own nest
	TcaActIndex       uint16 = 3  // TCA_ACT_INDEX: u32
	TcaActStats       uint16 = 4  // TCA_ACT_STATS: nested TCA_STATS_*
	TcaActPad         uint16 = 5  // TCA_ACT_PAD
	TcaActCookie      uint16 = 6  // TCA_ACT_COOKIE: up to 16 bytes
	TcaActFlags       uint16 = 7  // TCA_ACT_FLAGS: nla_bitfield32
	TcaActHwStats     uint16 = 8  // TCA_ACT_HW_STATS: nla_bitfield32
	TcaActUsedHwStats uint16 = 9  // TCA_ACT_USED_HW_STATS: nla_bitfield32
	TcaActInHwCount   uint16 = 10 // TCA_ACT_IN_HW_COUNT: u32
	TcaActMaxKnown           = TcaActInHwCount
)

var (
	// ErrTcmsgSmall indicates a body shorter than struct tcmsg.
	ErrTcmsgSmall = errors.New("data too small for Tcmsg")
	// ErrTcamsgSmall indicates a body shorter than struct tcamsg.
	ErrTcamsgSmall = errors.New("data too small for Tcamsg")
)

// DeserializeTcmsg reads a struct tcmsg from the front of data.
func DeserializeTcmsg(data []byte, h *Tcmsg) (n int, err error) {
	if len(data) < TcMsgSizeCst {
		return 0, ErrTcmsgSmall
	}
	h.Family = data[0]
	h.Pad1 = data[1]
	h.Pad2 = binary.LittleEndian.Uint16(data[2:4])
	h.Ifindex = binary.LittleEndian.Uint32(data[4:8])
	h.Handle = binary.LittleEndian.Uint32(data[8:12])
	h.Parent = binary.LittleEndian.Uint32(data[12:16])
	h.Info = binary.LittleEndian.Uint32(data[16:20])
	return TcMsgSizeCst, nil
}

// TcInfo is a decoded RTM_*QDISC, RTM_*TCLASS, RTM_*TFILTER or RTM_*CHAIN
// body.
type TcInfo struct {
	Hdr Tcmsg
	// FilterPrio and FilterProtocol split tcm_info the way a filter message
	// packs it: TC_H_MAKE(prio << 16, htons(protocol)). They are meaningless
	// on a qdisc or class, where tcm_info is unused.
	FilterPrio     uint16
	FilterProtocol uint16

	Kind            string
	Options         []byte // TCA_OPTIONS payload, undecoded, copied
	HasOptions      bool
	Chain           uint32
	HasChain        bool
	IngressBlock    uint32
	HasIngressBlock bool
	EgressBlock     uint32
	HasEgressBlock  bool
	HwOffload       uint8
	HasHwOffload    bool
	ExtWarnMsg      string
	// Attrs is every top-level attribute type, in wire order.
	Attrs []uint16
}

// ParseTcMsg decodes a tcmsg body and its top-level TCA_* attributes.
func ParseTcMsg(body []byte) (TcInfo, error) {
	var ti TcInfo
	if _, err := DeserializeTcmsg(body, &ti.Hdr); err != nil {
		return ti, err
	}
	// tcm_info is a host-order u32 whose low half holds the protocol in
	// network order: read each half from its bytes, so nothing is narrowed.
	ti.FilterProtocol = binary.BigEndian.Uint16(body[16:18])
	ti.FilterPrio = binary.LittleEndian.Uint16(body[18:20])
	err := WalkRTAttrs(body[TcMsgSizeCst:], func(atype uint16, val []byte) {
		ti.Attrs = append(ti.Attrs, atype)
		switch atype {
		case TcaKind:
			ti.Kind = tcString(val)
		case TcaOptions:
			ti.Options, ti.HasOptions = CopyBytes(val), true
		case TcaChain:
			ti.Chain, ti.HasChain = tcU32(val)
		case TcaIngressBlock:
			ti.IngressBlock, ti.HasIngressBlock = tcU32(val)
		case TcaEgressBlock:
			ti.EgressBlock, ti.HasEgressBlock = tcU32(val)
		case TcaHwOffload:
			if len(val) >= 1 {
				ti.HwOffload, ti.HasHwOffload = val[0], true
			}
		case TcaExtWarnMsg:
			ti.ExtWarnMsg = tcString(val)
		}
	})
	return ti, err
}

// TcAction is one action of an RTM_*ACTION message's TCA_ROOT_TAB nest.
type TcAction struct {
	// Order is the nest's attribute type. tc numbers a request's actions
	// from 1, but the kernel numbers a dump's from 0, and past
	// TCA_ACT_MAX_PRIO (32) when the dump asks for TCA_ACT_FLAG_LARGE_DUMP_ON
	// (net/sched/act_api.c, tcf_dump_walker). A flush's reply is one nest 0
	// holding TCA_ACT_KIND and TCA_FCNT, the count of actions flushed, whose
	// value 7 reads as TCA_ACT_FLAGS here (tcf_del_walker). So no order is
	// refused.
	Order    uint16
	Kind     string
	Index    uint32
	HasIndex bool
	Options  []byte // TCA_ACT_OPTIONS payload, undecoded, copied
	Cookie   []byte
	// Attrs is every attribute type in the action's nest, in wire order.
	Attrs []uint16
}

// TcActionInfo is a decoded RTM_*ACTION body.
type TcActionInfo struct {
	Family     uint8
	Actions    []TcAction
	Count      uint32
	HasCount   bool
	ExtWarnMsg string
	// Attrs is every top-level attribute type, in wire order.
	Attrs []uint16
}

// ParseTcAction decodes a tcamsg body: the TCA_ROOT_* attributes and each
// action in TCA_ROOT_TAB.
func ParseTcAction(body []byte) (TcActionInfo, error) {
	var ai TcActionInfo
	if len(body) < TcaMsgSizeCst {
		return ai, ErrTcamsgSmall
	}
	ai.Family = body[0]
	var tabs [][]byte
	err := WalkRTAttrs(body[TcaMsgSizeCst:], func(atype uint16, val []byte) {
		ai.Attrs = append(ai.Attrs, atype)
		switch atype {
		case TcaRootTab:
			tabs = append(tabs, val)
		case TcaRootCount:
			ai.Count, ai.HasCount = tcU32(val)
		case TcaRootExtWarnMsg:
			ai.ExtWarnMsg = tcString(val)
		}
	})
	if err != nil {
		return ai, err
	}
	for _, tab := range tabs {
		var nestErr error
		if err := WalkRTAttrsNested(tab, func(order uint16, val []byte) {
			a, err := parseTcActionNest(order, val)
			ai.Actions = append(ai.Actions, a)
			nestErr = errors.Join(nestErr, err)
		}); err != nil {
			return ai, err
		}
		if nestErr != nil {
			return ai, nestErr
		}
	}
	return ai, nil
}

func parseTcActionNest(order uint16, nest []byte) (TcAction, error) {
	a := TcAction{Order: order}
	err := WalkRTAttrsNested(nest, func(atype uint16, val []byte) {
		a.Attrs = append(a.Attrs, atype)
		switch atype {
		case TcaActKind:
			a.Kind = tcString(val)
		case TcaActIndex:
			a.Index, a.HasIndex = tcU32(val)
		case TcaActOptions:
			a.Options = CopyBytes(val)
		case TcaActCookie:
			a.Cookie = CopyBytes(val)
		}
	})
	return a, err
}

// tcString reads a NUL-terminated string attribute.
func tcString(val []byte) string {
	if i := bytes.IndexByte(val, 0); i >= 0 {
		val = val[:i]
	}
	return string(val)
}

func tcU32(val []byte) (uint32, bool) {
	if len(val) < 4 {
		return 0, false
	}
	return binary.LittleEndian.Uint32(val[:4]), true
}
