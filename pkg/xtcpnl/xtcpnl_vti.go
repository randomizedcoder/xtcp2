package xtcpnl

import (
	"net/netip"
)

// The vti and vti6 IFLA_INFO_DATA (include/uapi/linux/if_tunnel.h,
// net/ipv4/ip_vti.c, net/ipv6/ip6_vti.c), as ip/link_vti.c and
// ip/link_vti6.c at v7.2.0 write and print them
// (docs/netlink/goip-link-kinds.md, "KP4-T05 The vti, vti6 and xfrm
// parsers").

const (
	vtiKindCst  = "vti"
	vti6KindCst = "vti6"
)

// IFLA_VTI_*, absent from x/sys v0.49.0.
// linux/include/uapi/linux/if_tunnel.h (Linux 7.2 headers)
const (
	IflaVtiLink   = 1
	IflaVtiIKey   = 2
	IflaVtiOKey   = 3
	IflaVtiLocal  = 4
	IflaVtiRemote = 5
	IflaVtiFwMark = 6
)

// vtiPrefetchLinkMaskCst is what rta_getattr_u8 keeps of a prefetched
// IFLA_VTI_LINK: its low byte.
const vtiPrefetchLinkMaskCst = 0xff

// VtiInfo is IFLA_INFO_DATA decoded for vti and vti6. Each Has* says the
// kernel sent the attribute at its full width.
type VtiInfo struct {
	Link    uint32
	HasLink bool
	// IKey and OKey are big-endian on the wire, here in host order.
	IKey    uint32
	HasIKey bool
	OKey    uint32
	HasOKey bool
	// Local and Remote are 4 bytes for vti and 16 for vti6; the zero Addr
	// when shorter or not sent.
	Local     netip.Addr
	Remote    netip.Addr
	FwMark    uint32
	HasFwMark bool
	// Unknown lists attribute ids this package does not decode, in wire
	// order.
	Unknown []uint16
}

// ParseVtiInfoData decodes IFLA_INFO_DATA for vti and vti6, in whichever
// order the kernel sent it. nil for a nil nest. Tolerant as
// ParseBridgeInfoData is.
func ParseVtiInfoData(nest []byte) *VtiInfo {
	if nest == nil {
		return nil
	}
	g := &VtiInfo{}
	walkNestTolerant(nest, func(atype uint16, val []byte) {
		switch atype {
		case IflaVtiLink:
			kindU32(val, &g.Link, &g.HasLink)
		case IflaVtiIKey:
			kindBE32(val, &g.IKey, &g.HasIKey)
		case IflaVtiOKey:
			kindBE32(val, &g.OKey, &g.HasOKey)
		case IflaVtiLocal:
			g.Local = kindAddr(val, g.Local)
		case IflaVtiRemote:
			g.Remote = kindAddr(val, g.Remote)
		case IflaVtiFwMark:
			kindU32(val, &g.FwMark, &g.HasFwMark)
		default:
			g.Unknown = append(g.Unknown, atype)
		}
	})
	return g
}

// VtiOption names what a VtiSetting sets.
type VtiOption uint8

const (
	VtiOptionNone VtiOption = iota
	// VtiOptionKey sets both keys, as the key word does.
	VtiOptionKey
	VtiOptionIKey
	VtiOptionOKey
	VtiOptionRemote
	VtiOptionLocal
	VtiOptionLink
	VtiOptionFwMark
)

// VtiSetting is one option word of `type vti` or `vti6`. Value is a key
// (in host order: a dotted key's address as a number), the ifindex or the
// fwmark; Addr is the remote or local, the zero Addr for any.
type VtiSetting struct {
	Option VtiOption
	Value  uint64
	Addr   netip.Addr
}

// vtiEnc is vti_parse_opt's state across the words.
type vtiEnc struct {
	ikey, okey, link, fwmark uint64
	local, remote            netip.Addr
}

// EncodeVtiInfoData encodes the inside of IFLA_INFO_DATA for vti or vti6
// (v6 says which) as link_vti.c's vti_parse_opt and link_vti6.c's
// vti6_parse_opt do. cur is the device's state, which ip fetches before a
// set and the words then change; nil for an add. IKEY and OKEY
// (big-endian) always, LOCAL and REMOTE when set and not zero, FWMARK
// always, and LINK when nonzero. The prefetch seeds the endpoints, keys
// and fwmark, and only the low byte of the link, as ip's rta_getattr_u8
// does. No settings encode to nothing. It refuses a value out of range or
// an endpoint of the other family (ErrKindSettingRange).
func EncodeVtiInfoData(settings []VtiSetting, cur *VtiInfo, v6 bool) ([]byte, error) {
	if len(settings) == 0 {
		return nil, nil
	}
	e := &vtiEnc{}
	e.seed(cur)
	for _, s := range settings {
		if err := e.put(s, v6); err != nil {
			return nil, err
		}
	}
	var raw [linkWriteBufCst]byte
	ab := NewAttrBuilder(raw[:])
	if err := e.finish(&ab); err != nil {
		return nil, err
	}
	return append([]byte(nil), ab.Bytes()...), nil
}

// seed copies what vti_parse_opt reads from the prefetch.
func (e *vtiEnc) seed(cur *VtiInfo) {
	if cur == nil {
		return
	}
	e.local, e.remote = cur.Local, cur.Remote
	e.ikey, e.okey = uint64(cur.IKey), uint64(cur.OKey)
	e.link = uint64(cur.Link & vtiPrefetchLinkMaskCst)
	e.fwmark = uint64(cur.FwMark)
}

// put is one pass of vti_parse_opt's loop.
func (e *vtiEnc) put(s VtiSetting, v6 bool) error {
	switch s.Option {
	case VtiOptionKey:
		e.ikey, e.okey = s.Value, s.Value
	case VtiOptionIKey:
		e.ikey = s.Value
	case VtiOptionOKey:
		e.okey = s.Value
	case VtiOptionRemote, VtiOptionLocal:
		if s.Addr.IsValid() && s.Addr.Is6() != v6 {
			return ErrKindSettingRange
		}
		if s.Option == VtiOptionRemote {
			e.remote = s.Addr
		} else {
			e.local = s.Addr
		}
		return nil
	case VtiOptionLink:
		e.link = s.Value
	case VtiOptionFwMark:
		e.fwmark = s.Value
	default:
		return ErrKindSettingOption
	}
	return kindMax(s.Value, kindU32MaxCst)
}

// finish sends what vti_parse_opt sends after its loop.
func (e *vtiEnc) finish(ab *AttrBuilder) error {
	for _, kv := range [2]struct {
		atype uint16
		v     uint64
	}{{IflaVtiIKey, e.ikey}, {IflaVtiOKey, e.okey}} {
		if err := ab.PutBytes(kv.atype, kindBE(kv.v, 4)); err != nil {
			return err
		}
	}
	for _, ep := range [2]struct {
		atype uint16
		a     netip.Addr
	}{{IflaVtiLocal, e.local}, {IflaVtiRemote, e.remote}} {
		if kindAddrSet(ep.a) {
			if err := ab.PutBytes(ep.atype, ep.a.AsSlice()); err != nil {
				return err
			}
		}
	}
	if err := kindPutU32(ab, IflaVtiFwMark, e.fwmark); err != nil {
		return err
	}
	if e.link == 0 {
		return nil
	}
	return kindPutU32(ab, IflaVtiLink, e.link)
}
