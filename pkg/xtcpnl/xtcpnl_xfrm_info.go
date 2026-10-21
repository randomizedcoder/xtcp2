package xtcpnl

import (
	"encoding/binary"
	"fmt"
)

// This file decodes the NETLINK_XFRM messages about the SA and policy
// databases: GETSADINFO and NEWSADINFO, the SA count and hash table, and
// GETSPDINFO and NEWSPDINFO, the policy counts, hash table and hash
// thresholds. Each is a u32 of flags, then attributes numbered in their
// own space, XFRMA_SAD_* or XFRMA_SPD_*, not XFRMA_*. The kernel's
// xfrm_get_sadinfo, xfrm_get_spdinfo and xfrm_set_spdinfo
// (net/xfrm/xfrm_user.c) answer and take them; `ip xfrm state count` and
// `ip xfrm policy count` and `set` (ip/xfrm_state.c, ip/xfrm_policy.c)
// send them.

// XFRMA_SAD_* attributes (enum xfrm_sadattr_type_t).
const (
	XfrmSADAttrCnt   uint16 = 1 // u32
	XfrmSADAttrHInfo uint16 = 2 // struct xfrmu_sadhinfo
)

// XFRMA_SPD_* attributes (enum xfrm_spdattr_type_t).
const (
	XfrmSPDAttrInfo        uint16 = 1 // struct xfrmu_spdinfo
	XfrmSPDAttrHInfo       uint16 = 2 // struct xfrmu_spdhinfo
	XfrmSPDAttrIPv4HThresh uint16 = 3 // struct xfrmu_spdhthresh
	XfrmSPDAttrIPv6HThresh uint16 = 4 // struct xfrmu_spdhthresh
)

// Sizes of the database structs.
const (
	XfrmInfoFlagsSizeCst  = 4  // the u32 of flags each message starts with
	XfrmSADHInfoSizeCst   = 8  // struct xfrmu_sadhinfo
	XfrmSPDInfoSizeCst    = 24 // struct xfrmu_spdinfo
	XfrmSPDHInfoSizeCst   = 8  // struct xfrmu_spdhinfo
	XfrmSPDHThreshSizeCst = 2  // struct xfrmu_spdhthresh
)

// XfrmHInfo is a struct xfrmu_sadhinfo or xfrmu_spdhinfo: a hash table's
// bucket count and the most it may grow to.
type XfrmHInfo struct {
	Count, MaxCount uint32
}

// XfrmSPDInfo is a struct xfrmu_spdinfo: the policies in each direction,
// then the socket policies in each.
type XfrmSPDInfo struct {
	In, Out, Fwd    uint32
	InS, OutS, FwdS uint32
}

// XfrmSPDHThresh is a struct xfrmu_spdhthresh: the prefix lengths, local
// and remote, at or over which a policy goes in the policy hash table.
type XfrmSPDHThresh struct {
	LBits, RBits uint8
}

// XfrmInfo is a decoded SAD or SPD info message. SADCnt and SADHInfo are
// for GETSADINFO and NEWSADINFO, the rest for GETSPDINFO and NEWSPDINFO;
// each is nil when absent. A repeated attribute keeps the last, and one
// this does not know is skipped.
type XfrmInfo struct {
	Type  uint16
	Flags uint32

	SADCnt   *uint32
	SADHInfo *XfrmHInfo

	SPDInfo     *XfrmSPDInfo
	SPDHInfo    *XfrmHInfo
	IPv4HThresh *XfrmSPDHThresh
	IPv6HThresh *XfrmSPDHThresh
}

// The least length of each XFRMA_SAD_* and XFRMA_SPD_* struct attribute;
// XFRMA_SAD_CNT, a u32, must be exactly 4.
var (
	xfrmSADAttrMin = map[uint16]int{XfrmSADAttrHInfo: XfrmSADHInfoSizeCst}
	xfrmSPDAttrMin = map[uint16]int{
		XfrmSPDAttrInfo: XfrmSPDInfoSizeCst, XfrmSPDAttrHInfo: XfrmSPDHInfoSizeCst,
		XfrmSPDAttrIPv4HThresh: XfrmSPDHThreshSizeCst, XfrmSPDAttrIPv6HThresh: XfrmSPDHThreshSizeCst,
	}
)

// set decodes one attribute, in the SAD's numbering when sad is true,
// else the SPD's, after checking its length.
func (x *XfrmInfo) set(sad bool, id uint16, d []byte) error {
	if sad && id == XfrmSADAttrCnt {
		if len(d) != 4 {
			return fmt.Errorf("%w: %d needs 4 bytes, has %d", ErrXfrmAttr, id, len(d))
		}
		v := binary.LittleEndian.Uint32(d)
		x.SADCnt = &v
		return nil
	}
	want := xfrmSPDAttrMin
	if sad {
		want = xfrmSADAttrMin
	}
	n, known := want[id]
	if !known {
		return nil
	}
	if len(d) < n {
		return fmt.Errorf("%w: %d is %d bytes, under %d", ErrXfrmAttr, id, len(d), n)
	}
	u := func(i int) uint32 { return binary.LittleEndian.Uint32(d[4*i:]) }
	switch {
	case sad:
		x.SADHInfo = &XfrmHInfo{u(0), u(1)}
	case id == XfrmSPDAttrInfo:
		x.SPDInfo = &XfrmSPDInfo{u(0), u(1), u(2), u(3), u(4), u(5)}
	case id == XfrmSPDAttrHInfo:
		x.SPDHInfo = &XfrmHInfo{u(0), u(1)}
	case id == XfrmSPDAttrIPv4HThresh:
		x.IPv4HThresh = &XfrmSPDHThresh{d[0], d[1]}
	default:
		x.IPv6HThresh = &XfrmSPDHThresh{d[0], d[1]}
	}
	return nil
}

// ParseXfrmInfo decodes a SAD or SPD info message's body: its u32 of
// flags, then its attributes from offset 4, in the SAD's numbering for
// GETSADINFO and NEWSADINFO and the SPD's for GETSPDINFO and NEWSPDINFO.
// It stops at the first malformed attribute, keeping those before.
func ParseXfrmInfo(h NlMsgHdr, body []byte) (XfrmInfo, error) {
	x := XfrmInfo{Type: h.Type}
	var sad bool
	switch h.Type {
	case XfrmMsgNewSADInfo, XfrmMsgGetSADInfo:
		sad = true
	case XfrmMsgNewSPDInfo, XfrmMsgGetSPDInfo:
	default:
		return x, fmt.Errorf("%w: %d", ErrXfrmType, h.Type)
	}
	if len(body) < XfrmInfoFlagsSizeCst {
		return x, fmt.Errorf("%w: type %d needs %d bytes, has %d", ErrXfrmSmall, h.Type, XfrmInfoFlagsSizeCst, len(body))
	}
	x.Flags = binary.LittleEndian.Uint32(body)
	var err error
	walkErr := WalkRTAttrs(body[XfrmInfoFlagsSizeCst:], func(id uint16, d []byte) {
		if err == nil {
			err = x.set(sad, id, d)
		}
	})
	if walkErr != nil {
		return x, walkErr
	}
	return x, err
}
