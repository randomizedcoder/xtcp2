package xtcpnl

import (
	"golang.org/x/sys/unix"
)

// The xfrm IFLA_INFO_DATA (include/uapi/linux/if_link.h,
// net/xfrm/xfrm_interface_core.c), as ip/link_xfrm.c at v7.2.0 writes and
// prints it (docs/netlink/goip-link-kinds.md, "KP4-T05 The vti, vti6 and
// xfrm parsers").

const xfrmKindCst = "xfrm"

// XfrmLinkInfo is IFLA_INFO_DATA decoded for xfrm. Each Has* says the kernel
// sent the attribute at its full width.
type XfrmLinkInfo struct {
	Link     uint32
	HasLink  bool
	IfID     uint32
	HasIfID  bool
	External bool
	// Unknown lists attribute ids this package does not decode, in wire
	// order.
	Unknown []uint16
}

// ParseXfrmLinkInfoData decodes IFLA_INFO_DATA for xfrm. nil for a nil nest.
// Tolerant as ParseBridgeInfoData is.
func ParseXfrmLinkInfoData(nest []byte) *XfrmLinkInfo {
	if nest == nil {
		return nil
	}
	g := &XfrmLinkInfo{}
	walkNestTolerant(nest, func(atype uint16, val []byte) {
		switch atype {
		case unix.IFLA_XFRM_LINK:
			kindU32(val, &g.Link, &g.HasLink)
		case unix.IFLA_XFRM_IF_ID:
			kindU32(val, &g.IfID, &g.HasIfID)
		case unix.IFLA_XFRM_COLLECT_METADATA:
			g.External = true
		default:
			g.Unknown = append(g.Unknown, atype)
		}
	})
	return g
}

// XfrmLinkOption names what an XfrmLinkSetting sets.
type XfrmLinkOption uint8

const (
	XfrmLinkOptionNone XfrmLinkOption = iota
	XfrmLinkOptionLink
	XfrmLinkOptionIfID
	XfrmLinkOptionExternal
)

// XfrmLinkSetting is one option word of `type xfrm`. Value is the ifindex or
// the if_id.
type XfrmLinkSetting struct {
	Option XfrmLinkOption
	Value  uint64
}

// EncodeXfrmLinkInfoData encodes the inside of IFLA_INFO_DATA for xfrm as
// link_xfrm.c's xfrm_parse_opt does; ip fetches nothing before a set. Each
// if_id word sends IF_ID as it comes; after the words, external sends
// COLLECT_METADATA, and otherwise LINK follows when nonzero. No settings
// encode to nothing. It refuses an if_id of 0 or a value out of range
// (ErrKindSettingRange), external with an if_id or a dev
// (ErrKindSettingConflict), and no if_id without external
// (ErrKindSettingMissing).
func EncodeXfrmLinkInfoData(settings []XfrmLinkSetting) ([]byte, error) {
	if len(settings) == 0 {
		return nil, nil
	}
	var raw [linkWriteBufCst]byte
	ab := NewAttrBuilder(raw[:])
	var link, ifID uint64
	external := false
	for _, s := range settings {
		switch s.Option {
		case XfrmLinkOptionLink:
			if err := kindMax(s.Value, kindU32MaxCst); err != nil {
				return nil, err
			}
			link = s.Value
		case XfrmLinkOptionIfID:
			if s.Value == 0 {
				return nil, ErrKindSettingRange
			}
			if err := kindPutU32(&ab, unix.IFLA_XFRM_IF_ID, s.Value); err != nil {
				return nil, err
			}
			ifID = s.Value
		case XfrmLinkOptionExternal:
			external = true
		default:
			return nil, ErrKindSettingOption
		}
	}
	switch {
	case external && (ifID != 0 || link != 0):
		return nil, ErrKindSettingConflict
	case external:
		if err := ab.PutBytes(unix.IFLA_XFRM_COLLECT_METADATA, nil); err != nil {
			return nil, err
		}
	case ifID == 0:
		return nil, ErrKindSettingMissing
	case link != 0:
		if err := kindPutU32(&ab, unix.IFLA_XFRM_LINK, link); err != nil {
			return nil, err
		}
	}
	return append([]byte(nil), ab.Bytes()...), nil
}
