package xtcpnl

import "encoding/binary"

// ILA_HOOK_* (include/uapi/linux/ila.h), ILA_ATTR_HOOK_TYPE's values.
const (
	ILAHookRouteOutput uint8 = 0
	ILAHookRouteInput  uint8 = 1
)

// IlaEncapInfo is an LWTUNNEL_ENCAP_ILA RTA_ENCAP payload on a read, as
// print_encap_ila (ip/iproute_lwtunnel.c) reads it. The first of a repeated
// attribute wins, as in parse_rtattr, and a scalar shorter than its width is
// dropped.
type IlaEncapInfo struct {
	// Locator is ILA_ATTR_LOCATOR's four 16-bit groups, read big-endian so
	// the first group is the high word, as addr64_n2a prints them.
	Locator   uint64
	CsumMode  uint8 // ILA_CSUM_*
	IdentType uint8 // ILA_ATYPE_*
	HookType  uint8 // ILA_HOOK_*

	HasLocator, HasCsumMode, HasIdentType, HasHookType bool
}

// ParseIlaEncap decodes an LWTUNNEL_ENCAP_ILA RTA_ENCAP payload.
func ParseIlaEncap(val []byte) (IlaEncapInfo, error) {
	var (
		e    IlaEncapInfo
		seen attrSeen
	)
	err := WalkRTAttrsNested(val, func(atype uint16, v []byte) {
		if !seen.first(atype) {
			return
		}
		switch atype {
		case ILAAttrLocator:
			if len(v) >= 8 {
				e.Locator, e.HasLocator = binary.BigEndian.Uint64(v), true
			}
		case ILAAttrCsumMode:
			if len(v) >= 1 {
				e.CsumMode, e.HasCsumMode = v[0], true
			}
		case ILAAttrIdentType:
			if len(v) >= 1 {
				e.IdentType, e.HasIdentType = v[0], true
			}
		case ILAAttrHookType:
			if len(v) >= 1 {
				e.HookType, e.HasHookType = v[0], true
			}
		}
	})
	if err != nil {
		return IlaEncapInfo{}, err
	}
	return e, nil
}

// XfrmEncapInfo is an LWTUNNEL_ENCAP_XFRM RTA_ENCAP payload on a read, as
// print_encap_xfrm reads it: both are host-order u32s. The first of a
// repeated attribute wins, and a short one is dropped.
type XfrmEncapInfo struct {
	IfID uint32 // LWT_XFRM_IF_ID
	Link uint32 // LWT_XFRM_LINK, an ifindex

	HasIfID, HasLink bool
}

// ParseXfrmEncap decodes an LWTUNNEL_ENCAP_XFRM RTA_ENCAP payload.
func ParseXfrmEncap(val []byte) (XfrmEncapInfo, error) {
	var (
		e    XfrmEncapInfo
		seen attrSeen
	)
	err := WalkRTAttrsNested(val, func(atype uint16, v []byte) {
		if !seen.first(atype) || len(v) < 4 {
			return
		}
		switch atype {
		case LwtXfrmIfID:
			e.IfID, e.HasIfID = binary.LittleEndian.Uint32(v), true
		case LwtXfrmLink:
			e.Link, e.HasLink = binary.LittleEndian.Uint32(v), true
		}
	})
	if err != nil {
		return XfrmEncapInfo{}, err
	}
	return e, nil
}
