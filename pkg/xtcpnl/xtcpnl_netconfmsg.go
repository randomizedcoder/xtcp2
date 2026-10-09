package xtcpnl

import (
	"encoding/binary"
	"errors"
)

// This file decodes RTM_NEWNETCONF, the reply to the RTM_GETNETCONF dump (and
// point get) that `ip netconf show` issues. Each reply is one per-family,
// per-interface netconf record: the forwarding/rp_filter/… settings for one
// interface (or the `all`/`default` pseudo-interfaces). See
// internal/goip/obj_netconf.go for the transaction and render/netconf.go for the
// output.

// Netconfmsg mirrors the kernel's `struct netconfmsg`, the fixed header of an
// RTM_*NETCONF message.
//
//	struct netconfmsg {
//		__u8	ncm_family;
//	};
//
// It is one byte, which NLMSG_ALIGN rounds to four; the attributes begin at
// offset NetconfMsgSizeCst.
//
// Reference: linux/include/uapi/linux/netconf.h
type Netconfmsg struct {
	Family uint8
}

const (
	// NetconfMsgSizeCst is NLMSG_ALIGN(sizeof(struct netconfmsg)): a family byte
	// plus three bytes of alignment padding.
	NetconfMsgSizeCst = 4

	// NETCONFA_* attributes (include/uapi/linux/netconf.h). Declared here because
	// golang.org/x/sys/unix's pinned version carries RTM_*NETCONF but neither the
	// struct nor the NETCONFA_* enum, as xtcpnl_ndtmsg.go declares the ndtmsg
	// layout for the same reason. NETCONFA_BC_FORWARDING(8) and
	// NETCONFA_FORCE_FORWARDING(9) are parsed-but-not-printed by print_netconf, so
	// they are not decoded (the "decode only what renders" discipline).
	NetconfaIfindex                  uint16 = 1 // s32, -1 all / -2 default / else ifindex
	NetconfaForwarding               uint16 = 2 // s32 (0/1)
	NetconfaRpFilter                 uint16 = 3 // s32 (0 off / 1 strict / 2 loose)
	NetconfaMcForwarding             uint16 = 4 // s32 (0/1)
	NetconfaProxyNeigh               uint16 = 5 // s32 (0/1)
	NetconfaIgnoreRoutesWithLinkdown uint16 = 6 // s32 (0/1)
	NetconfaInput                    uint16 = 7 // s32 (0/1)
)

const (
	// NetconfIfindexAll and NetconfIfindexDefault are the two pseudo-interface
	// ifindex sentinels print_netconf renders as `all`/`default` (netconf.h).
	NetconfIfindexAll     int32 = -1
	NetconfIfindexDefault int32 = -2
)

// ErrNetconfmsgSmall is a reply body shorter than the fixed netconfmsg header.
var ErrNetconfmsgSmall = errors.New("data too small for Netconfmsg")

// NetconfInfo is a decoded RTM_NEWNETCONF reply: the netconfmsg family plus the
// NETCONFA_* attributes print_netconf renders. Each value carries a Has* bool
// because presence is not value: print_netconf guards every token on
// `if (tb[NETCONFA_X])`, so a present-but-zero setting prints `forwarding off`
// while an absent one prints no token at all (the RuleInfo.HasProtocol
// discipline). Values are s32: Ifindex is signed (the -1/-2 sentinels) and the
// rest are small non-negative ints, but all are read as a 4-byte little-endian
// word.
type NetconfInfo struct {
	Family uint8

	Ifindex    int32
	HasIfindex bool

	Forwarding    int32
	HasForwarding bool

	RpFilter    int32
	HasRpFilter bool

	McForwarding    int32
	HasMcForwarding bool

	ProxyNeigh    int32
	HasProxyNeigh bool

	IgnoreRoutesWithLinkdown    int32
	HasIgnoreRoutesWithLinkdown bool

	Input    int32
	HasInput bool
}

// DeserializeNetconfmsg does an offset read of the fixed netconfmsg header. The
// three alignment pad bytes are ignored, as the kernel leaves them zero.
func DeserializeNetconfmsg(data []byte, h *Netconfmsg) (n int, err error) {
	if len(data) < NetconfMsgSizeCst {
		return 0, ErrNetconfmsgSmall
	}
	h.Family = data[0]
	return NetconfMsgSizeCst, nil
}

// ParseNewNetconf decodes an RTM_NEWNETCONF reply body: the netconfmsg header
// then its NETCONFA_* attributes. Every value is an s32 taken at >= 4 bytes; a
// short payload leaves the field's Has bit clear, like a missing attribute (the
// width guard in ParseNewNeighTbl).
func ParseNewNetconf(body []byte) (NetconfInfo, error) {
	var ni NetconfInfo
	var h Netconfmsg
	if _, err := DeserializeNetconfmsg(body, &h); err != nil {
		return ni, err
	}
	ni.Family = h.Family

	s32 := func(dst *int32, has *bool, b []byte) {
		if len(b) >= 4 {
			*dst, *has = int32(binary.LittleEndian.Uint32(b[0:4])), true
		}
	}
	setters := map[uint16]func(b []byte){
		NetconfaIfindex:                  func(b []byte) { s32(&ni.Ifindex, &ni.HasIfindex, b) },
		NetconfaForwarding:               func(b []byte) { s32(&ni.Forwarding, &ni.HasForwarding, b) },
		NetconfaRpFilter:                 func(b []byte) { s32(&ni.RpFilter, &ni.HasRpFilter, b) },
		NetconfaMcForwarding:             func(b []byte) { s32(&ni.McForwarding, &ni.HasMcForwarding, b) },
		NetconfaProxyNeigh:               func(b []byte) { s32(&ni.ProxyNeigh, &ni.HasProxyNeigh, b) },
		NetconfaIgnoreRoutesWithLinkdown: func(b []byte) { s32(&ni.IgnoreRoutesWithLinkdown, &ni.HasIgnoreRoutesWithLinkdown, b) },
		NetconfaInput:                    func(b []byte) { s32(&ni.Input, &ni.HasInput, b) },
	}
	err := WalkRTAttrs(body[NetconfMsgSizeCst:], func(atype uint16, val []byte) {
		if set, ok := setters[atype]; ok {
			set(val)
		}
	})
	if err != nil {
		return ni, err
	}
	return ni, nil
}
