package xtcpnl

import (
	"encoding/binary"
	"fmt"
)

// This file decodes the NETLINK_XFRM events the kernel sends to the
// XFRMNLGRP_ACQUIRE, _EXPIRE and _MAPPING groups: ACQUIRE (struct
// xfrm_user_acquire), asking a key manager for an SA a policy needs;
// EXPIRE (struct xfrm_user_expire), an SA's lifetime running out; and
// MAPPING (struct xfrm_user_mapping), a NAT-T peer's address or port
// changing. Each is followed by XFRMA_* attributes. The kernel's
// build_acquire, build_expire and build_mapping (net/xfrm/xfrm_user.c)
// send them; `ip xfrm monitor` (ip/xfrm_monitor.c) reads them.

// Sizes on x86_64 of the event structs.
const (
	XfrmUserAcquireSizeCst = 280 // struct xfrm_user_acquire
	XfrmUserExpireSizeCst  = 232 // struct xfrm_user_expire, its u8 padded to 8
	XfrmUserMappingSizeCst = 64  // struct xfrm_user_mapping
)

// XfrmUserAcquire is a struct xfrm_user_acquire: the SA wanted, by its id
// and source, the traffic that wants it, the policy that asked, the
// algorithms it may use, as masks, and the kernel's sequence number for
// the request, which the key manager's ALLOCSPI and NEWSA echo.
type XfrmUserAcquire struct {
	ID     XfrmID
	Saddr  XfrmAddress
	Sel    XfrmSelector
	Policy XfrmUserpolicyInfo
	AAlgos uint32
	EAlgos uint32
	CAlgos uint32
	Seq    uint32
}

func xfrmUserAcquire(b []byte) XfrmUserAcquire {
	if len(b) < XfrmUserAcquireSizeCst {
		return XfrmUserAcquire{}
	}
	return XfrmUserAcquire{
		ID:     XfrmID{xfrmAddress(b), binary.BigEndian.Uint32(b[16:]), b[20]},
		Saddr:  xfrmAddress(b[24:]),
		Sel:    xfrmSelector(b[40:]),
		Policy: xfrmUserpolicyInfo(b[96:]),
		AAlgos: binary.LittleEndian.Uint32(b[264:]),
		EAlgos: binary.LittleEndian.Uint32(b[268:]),
		CAlgos: binary.LittleEndian.Uint32(b[272:]),
		Seq:    binary.LittleEndian.Uint32(b[276:]),
	}
}

// XfrmUserExpire is a struct xfrm_user_expire: an SA whose soft (Hard 0)
// or hard (Hard 1) lifetime has run out.
type XfrmUserExpire struct {
	State XfrmUsersaInfo
	Hard  uint8
}

// XfrmUserMapping is a struct xfrm_user_mapping: a NAT-T SA, and its
// peer's address and port as the SA had them and as the packet came.
// Ports are in host order here.
type XfrmUserMapping struct {
	ID                 XfrmUsersaID
	Reqid              uint32
	OldSaddr, NewSaddr XfrmAddress
	OldSport, NewSport uint16
}

func xfrmUserMapping(b []byte) XfrmUserMapping {
	if len(b) < XfrmUserMappingSizeCst {
		return XfrmUserMapping{}
	}
	return XfrmUserMapping{
		ID:       xfrmUsersaID(b),
		Reqid:    binary.LittleEndian.Uint32(b[24:]),
		OldSaddr: xfrmAddress(b[28:]),
		NewSaddr: xfrmAddress(b[44:]),
		OldSport: binary.BigEndian.Uint16(b[60:]),
		NewSport: binary.BigEndian.Uint16(b[62:]),
	}
}

// XfrmEvent is a decoded ACQUIRE, EXPIRE or MAPPING. Which of Acquire,
// Expire and Mapping is set follows Type.
type XfrmEvent struct {
	Type    uint16
	Acquire *XfrmUserAcquire
	Expire  *XfrmUserExpire
	Mapping *XfrmUserMapping
	Attrs   XfrmAttrs
}

// ParseXfrmEvent decodes an event's body: its struct, chosen by h.Type as
// the kernel's xfrm_msg_min chooses it, then its XFRMA_* attributes from
// the struct's NLMSG_ALIGN'd end. Byte slices are copied, so the result
// does not alias body.
func ParseXfrmEvent(h NlMsgHdr, body []byte) (XfrmEvent, error) {
	e := XfrmEvent{Type: h.Type}
	var n int
	switch h.Type {
	case XfrmMsgAcquire:
		n = XfrmUserAcquireSizeCst
	case XfrmMsgExpire:
		n = XfrmUserExpireSizeCst
	case XfrmMsgMapping:
		n = XfrmUserMappingSizeCst
	default:
		return e, fmt.Errorf("%w: %d", ErrXfrmType, h.Type)
	}
	if len(body) < n {
		return e, fmt.Errorf("%w: type %d needs %d bytes, has %d", ErrXfrmSmall, h.Type, n, len(body))
	}
	switch h.Type {
	case XfrmMsgAcquire:
		a := xfrmUserAcquire(body)
		e.Acquire = &a
	case XfrmMsgExpire:
		e.Expire = &XfrmUserExpire{xfrmUsersaInfo(body), body[XfrmUsersaInfoSizeCst]}
	case XfrmMsgMapping:
		m := xfrmUserMapping(body)
		e.Mapping = &m
	}
	if off := nlmsgAlign(n); off < len(body) {
		return e, parseXfrmAttrs(body[off:], &e.Attrs)
	}
	return e, nil
}
