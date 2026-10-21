package xtcpnl

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/sys/unix"
)

// This file decodes the NETLINK_XFRM messages about SAs: NEWSA and UPDSA
// (struct xfrm_usersa_info), DELSA and GETSA (struct xfrm_usersa_id),
// FLUSHSA (struct xfrm_usersa_flush), and NEWAE and GETAE (struct
// xfrm_aevent_id), each followed by XFRMA_* attributes. The kernel's
// xfrm_msg_min (net/xfrm/xfrm_user.c) gives each type's struct; `ip xfrm
// state` and `ip xfrm monitor` (ip/xfrm_state.c, ip/xfrm_monitor.c) send
// and read them.

// XfrmID is a struct xfrm_id: an SA's destination, SPI and IPsec protocol
// (IPPROTO_ESP, _AH or _COMP). SPI is in host order here, converted from
// the network order it is sent in.
type XfrmID struct {
	Daddr XfrmAddress
	SPI   uint32
	Proto uint8
}

// XfrmStats is a struct xfrm_stats: the packets an SA dropped as outside
// its replay window, as replays, and as failing their integrity check.
type XfrmStats struct {
	ReplayWindow, Replay, IntegrityFailed uint32
}

// XfrmUsersaInfo is a struct xfrm_usersa_info: an SA.
type XfrmUsersaInfo struct {
	Sel          XfrmSelector
	ID           XfrmID
	Saddr        XfrmAddress
	Lft          XfrmLifetimeCfg
	Curlft       XfrmLifetimeCur
	Stats        XfrmStats
	Seq          uint32
	Reqid        uint32
	Family       uint16
	Mode         uint8 // XFRM_MODE_TRANSPORT (0), _TUNNEL (1) and the like
	ReplayWindow uint8
	Flags        uint8 // XFRM_STATE_*
}

func xfrmUsersaInfo(b []byte) XfrmUsersaInfo {
	if len(b) < XfrmUsersaInfoSizeCst {
		return XfrmUsersaInfo{}
	}
	return XfrmUsersaInfo{
		Sel:          xfrmSelector(b),
		ID:           XfrmID{xfrmAddress(b[56:]), binary.BigEndian.Uint32(b[72:]), b[76]},
		Saddr:        xfrmAddress(b[80:]),
		Lft:          xfrmLifetimeCfg(b[96:]),
		Curlft:       xfrmLifetimeCur(b[160:]),
		Stats:        XfrmStats{binary.LittleEndian.Uint32(b[192:]), binary.LittleEndian.Uint32(b[196:]), binary.LittleEndian.Uint32(b[200:])},
		Seq:          binary.LittleEndian.Uint32(b[204:]),
		Reqid:        binary.LittleEndian.Uint32(b[208:]),
		Family:       binary.LittleEndian.Uint16(b[212:]),
		Mode:         b[214],
		ReplayWindow: b[215],
		Flags:        b[216],
	}
}

// XfrmUsersaID is a struct xfrm_usersa_id: what names an SA to get or
// delete. SPI is in host order here.
type XfrmUsersaID struct {
	Daddr  XfrmAddress
	SPI    uint32
	Family uint16
	Proto  uint8
}

func xfrmUsersaID(b []byte) XfrmUsersaID {
	if len(b) < XfrmUsersaIDSizeCst {
		return XfrmUsersaID{}
	}
	return XfrmUsersaID{xfrmAddress(b), binary.BigEndian.Uint32(b[16:]), binary.LittleEndian.Uint16(b[20:]), b[22]}
}

// XfrmAeventID is a struct xfrm_aevent_id: the SA an async event, a
// replay or lifetime update, is about, and the XFRM_AE_* flags saying why
// it was sent.
type XfrmAeventID struct {
	SAID  XfrmUsersaID
	Saddr XfrmAddress
	Flags uint32
	Reqid uint32
}

// XfrmState is a decoded SA message. Which of Info, ID, FlushProto and
// AEvent is set follows Type: Info for NEWSA and UPDSA, ID for DELSA and
// for a GETSA that is not a dump, FlushProto for FLUSHSA, and AEvent for
// NEWAE and GETAE. A GETSA dump has no struct, only attributes.
//
// The kernel's DELSA event carries an xfrm_usersa_id too, and nests the
// deleted SA in XFRMA_SA, so Attrs.SA holds it.
type XfrmState struct {
	Type       uint16
	Dump       bool
	Info       *XfrmUsersaInfo
	ID         *XfrmUsersaID
	FlushProto *uint8
	AEvent     *XfrmAeventID
	Attrs      XfrmAttrs
}

// ParseXfrmState decodes an SA message's body: its struct, chosen by
// h.Type as the kernel's xfrm_msg_min chooses it, then its XFRMA_*
// attributes from the struct's NLMSG_ALIGN'd end. A GETSA with NLM_F_DUMP
// is a dump, whose attributes start at the body, as xfrm_dump_sa reads
// them. Byte slices are copied, so the result does not alias body.
func ParseXfrmState(h NlMsgHdr, body []byte) (XfrmState, error) {
	s := XfrmState{Type: h.Type}
	var n int
	switch h.Type {
	case XfrmMsgNewSA, XfrmMsgUpdSA:
		n = XfrmUsersaInfoSizeCst
	case XfrmMsgGetSA, XfrmMsgDelSA:
		n = XfrmUsersaIDSizeCst
		if h.Type == XfrmMsgGetSA && h.Flags&unix.NLM_F_DUMP == unix.NLM_F_DUMP {
			s.Dump = true
			n = 0
		}
	case XfrmMsgFlushSA:
		n = XfrmUsersaFlushSizeCst
	case XfrmMsgNewAE, XfrmMsgGetAE:
		n = XfrmAeventIDSizeCst
	default:
		return s, fmt.Errorf("%w: %d", ErrXfrmType, h.Type)
	}
	if len(body) < n {
		return s, fmt.Errorf("%w: type %d needs %d bytes, has %d", ErrXfrmSmall, h.Type, n, len(body))
	}
	switch {
	case s.Dump:
	case n == XfrmUsersaInfoSizeCst:
		info := xfrmUsersaInfo(body)
		s.Info = &info
	case n == XfrmUsersaIDSizeCst:
		id := xfrmUsersaID(body)
		s.ID = &id
	case n == XfrmUsersaFlushSizeCst:
		p := body[0]
		s.FlushProto = &p
	case n == XfrmAeventIDSizeCst:
		s.AEvent = &XfrmAeventID{xfrmUsersaID(body), xfrmAddress(body[24:]),
			binary.LittleEndian.Uint32(body[40:]), binary.LittleEndian.Uint32(body[44:])}
	}
	if off := nlmsgAlign(n); off < len(body) {
		return s, parseXfrmAttrs(body[off:], &s.Attrs)
	}
	return s, nil
}
