package xtcpnl

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/sys/unix"
)

// This file decodes the NETLINK_XFRM messages about policies: NEWPOLICY
// and UPDPOLICY (struct xfrm_userpolicy_info), DELPOLICY and GETPOLICY
// (struct xfrm_userpolicy_id), FLUSHPOLICY (no struct), POLEXPIRE (struct
// xfrm_user_polexpire), and SETDEFAULT and GETDEFAULT (struct
// xfrm_userpolicy_default), each followed by XFRMA_* attributes. The
// kernel's xfrm_msg_min (net/xfrm/xfrm_user.c) gives each type's struct;
// `ip xfrm policy` and `ip xfrm monitor` (ip/xfrm_policy.c,
// ip/xfrm_monitor.c) send and read them.

// Sizes on x86_64 of the policy structs.
const (
	XfrmUserpolicyInfoSizeCst    = 168 // struct xfrm_userpolicy_info
	XfrmUserpolicyIDSizeCst      = 64  // struct xfrm_userpolicy_id
	XfrmUserTmplSizeCst          = 64  // struct xfrm_user_tmpl
	XfrmUserPolexpireSizeCst     = 176 // struct xfrm_user_polexpire, its u8 padded to 8
	XfrmUserpolicyDefaultSizeCst = 3   // struct xfrm_userpolicy_default
	XfrmUserpolicyTypeSizeCst    = 6   // struct xfrm_userpolicy_type: u8, u16, u8, 2-aligned
)

// XfrmUserpolicyInfo is a struct xfrm_userpolicy_info: a policy.
type XfrmUserpolicyInfo struct {
	Sel      XfrmSelector
	Lft      XfrmLifetimeCfg
	Curlft   XfrmLifetimeCur
	Priority uint32
	Index    uint32
	Dir      uint8 // XFRM_POLICY_IN (0), _OUT (1) or _FWD (2)
	Action   uint8 // XFRM_POLICY_ALLOW (0) or _BLOCK (1)
	Flags    uint8 // XFRM_POLICY_LOCALOK and the like
	Share    uint8
}

func xfrmUserpolicyInfo(b []byte) XfrmUserpolicyInfo {
	if len(b) < XfrmUserpolicyInfoSizeCst {
		return XfrmUserpolicyInfo{}
	}
	return XfrmUserpolicyInfo{
		Sel:      xfrmSelector(b),
		Lft:      xfrmLifetimeCfg(b[56:]),
		Curlft:   xfrmLifetimeCur(b[120:]),
		Priority: binary.LittleEndian.Uint32(b[152:]),
		Index:    binary.LittleEndian.Uint32(b[156:]),
		Dir:      b[160],
		Action:   b[161],
		Flags:    b[162],
		Share:    b[163],
	}
}

// XfrmUserpolicyID is a struct xfrm_userpolicy_id: what names a policy to
// get or delete, its selector or, where it is not 0, its index.
type XfrmUserpolicyID struct {
	Sel   XfrmSelector
	Index uint32
	Dir   uint8
}

func xfrmUserpolicyID(b []byte) XfrmUserpolicyID {
	if len(b) < XfrmUserpolicyIDSizeCst {
		return XfrmUserpolicyID{}
	}
	return XfrmUserpolicyID{xfrmSelector(b), binary.LittleEndian.Uint32(b[56:]), b[60]}
}

// XfrmUserTmpl is a struct xfrm_user_tmpl: one SA a policy needs, by its
// id, source and reqid, and the algorithms it may use, as masks.
type XfrmUserTmpl struct {
	ID       XfrmID
	Family   uint16
	Saddr    XfrmAddress
	Reqid    uint32
	Mode     uint8
	Share    uint8
	Optional uint8
	AAlgos   uint32
	EAlgos   uint32
	CAlgos   uint32
}

func xfrmUserTmpl(b []byte) XfrmUserTmpl {
	if len(b) < XfrmUserTmplSizeCst {
		return XfrmUserTmpl{}
	}
	return XfrmUserTmpl{
		ID:       XfrmID{xfrmAddress(b), binary.BigEndian.Uint32(b[16:]), b[20]},
		Family:   binary.LittleEndian.Uint16(b[24:]),
		Saddr:    xfrmAddress(b[28:]),
		Reqid:    binary.LittleEndian.Uint32(b[44:]),
		Mode:     b[48],
		Share:    b[49],
		Optional: b[50],
		AAlgos:   binary.LittleEndian.Uint32(b[52:]),
		EAlgos:   binary.LittleEndian.Uint32(b[56:]),
		CAlgos:   binary.LittleEndian.Uint32(b[60:]),
	}
}

// xfrmUserTmpls decodes an XFRMA_TMPL: as many templates as fit, as the
// kernel's copy_from_user_tmpl counts them, bytes past the last left out.
func xfrmUserTmpls(d []byte) []XfrmUserTmpl {
	t := make([]XfrmUserTmpl, len(d)/XfrmUserTmplSizeCst)
	for i := range t {
		t[i] = xfrmUserTmpl(d[i*XfrmUserTmplSizeCst:])
	}
	return t
}

// XfrmUserPolexpire is a struct xfrm_user_polexpire: a policy whose soft
// (Hard 0) or hard (Hard 1) lifetime has run out.
type XfrmUserPolexpire struct {
	Pol  XfrmUserpolicyInfo
	Hard uint8
}

// XfrmUserpolicyDefault is a struct xfrm_userpolicy_default: what traffic
// no policy matches meets in each direction, XFRM_USERPOLICY_UNSPEC (0),
// _BLOCK (1) or _ACCEPT (2).
type XfrmUserpolicyDefault struct {
	In, Fwd, Out uint8
}

// XfrmPolicy is a decoded policy message. Which of Info, ID, Expire and
// Default is set follows Type: Info for NEWPOLICY and UPDPOLICY, ID for
// DELPOLICY and for a GETPOLICY that is not a dump, Expire for POLEXPIRE,
// and Default for SETDEFAULT and GETDEFAULT. FLUSHPOLICY and a GETPOLICY
// dump have no struct, only attributes.
//
// The kernel's DELPOLICY event carries an xfrm_userpolicy_id too, and
// nests the deleted policy in XFRMA_POLICY, so Attrs.Policy holds it.
type XfrmPolicy struct {
	Type    uint16
	Dump    bool
	Info    *XfrmUserpolicyInfo
	ID      *XfrmUserpolicyID
	Expire  *XfrmUserPolexpire
	Default *XfrmUserpolicyDefault
	Attrs   XfrmAttrs
}

// ParseXfrmPolicy decodes a policy message's body: its struct, chosen by
// h.Type as the kernel's xfrm_msg_min chooses it, then its XFRMA_*
// attributes from the struct's NLMSG_ALIGN'd end. A GETPOLICY with
// NLM_F_DUMP is a dump, whose attributes start at the body; the kernel's
// xfrm_dump_policy reads none. Byte slices are copied, so the result does
// not alias body.
func ParseXfrmPolicy(h NlMsgHdr, body []byte) (XfrmPolicy, error) {
	p := XfrmPolicy{Type: h.Type}
	var n int
	switch h.Type {
	case XfrmMsgNewPolicy, XfrmMsgUpdPolicy:
		n = XfrmUserpolicyInfoSizeCst
	case XfrmMsgGetPolicy, XfrmMsgDelPolicy:
		n = XfrmUserpolicyIDSizeCst
		if h.Type == XfrmMsgGetPolicy && h.Flags&unix.NLM_F_DUMP == unix.NLM_F_DUMP {
			p.Dump = true
			n = 0
		}
	case XfrmMsgFlushPolicy:
	case XfrmMsgPolExpire:
		n = XfrmUserPolexpireSizeCst
	case XfrmMsgSetDefault, XfrmMsgGetDefault:
		n = XfrmUserpolicyDefaultSizeCst
	default:
		return p, fmt.Errorf("%w: %d", ErrXfrmType, h.Type)
	}
	if len(body) < n {
		return p, fmt.Errorf("%w: type %d needs %d bytes, has %d", ErrXfrmSmall, h.Type, n, len(body))
	}
	switch n {
	case XfrmUserpolicyInfoSizeCst:
		info := xfrmUserpolicyInfo(body)
		p.Info = &info
	case XfrmUserpolicyIDSizeCst:
		id := xfrmUserpolicyID(body)
		p.ID = &id
	case XfrmUserPolexpireSizeCst:
		p.Expire = &XfrmUserPolexpire{xfrmUserpolicyInfo(body), body[XfrmUserpolicyInfoSizeCst]}
	case XfrmUserpolicyDefaultSizeCst:
		p.Default = &XfrmUserpolicyDefault{body[0], body[1], body[2]}
	}
	if off := nlmsgAlign(n); off < len(body) {
		return p, parseXfrmAttrs(body[off:], &p.Attrs)
	}
	return p, nil
}
