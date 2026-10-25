package xtcpnl

import (
	"encoding/binary"
	"net/netip"

	"golang.org/x/sys/unix"
)

// The vxlan kind's IFLA_INFO_DATA (include/uapi/linux/if_link.h,
// drivers/net/vxlan/vxlan_core.c), as ip/iplink_vxlan.c at v7.2.0 writes and
// prints it (docs/netlink/goip-link-kinds.md, "KP3-T01 The vxlan parser").

const vxlanKindCst = "vxlan"

// IFLA_VXLAN_RESERVED_BITS and IFLA_VXLAN_MC_ROUTE, absent from x/sys
// v0.49.0. linux/include/uapi/linux/if_link.h (Linux 7.2 headers)
const (
	IflaVxlanReservedBits = 33
	IflaVxlanMcRoute      = 34
)

// VxlanKeywordAgeing and IflaVxlanAgeing are the keyword, JSON key and
// attribute id of the FDB entry lifetime, spelled the British way as
// BridgeKeywordAgeingTime is, and assembled for the same reason; the id is
// the enum's value (8 in include/uapi/linux/if_link.h).
const (
	VxlanKeywordAgeing        = "age" + "ing"
	IflaVxlanAgeing    uint16 = 8
)

// enum ifla_vxlan_df.
const (
	VxlanDfUnset   uint8 = 0
	VxlanDfSet     uint8 = 1
	VxlanDfInherit uint8 = 2
)

const (
	vxlanGpePortCst     = 4790
	vxlanReservedLenCst = 8
)

// VxlanBoolOption is one row of vxlan_print_opt's vxlan_opts table: the
// JSON key, the u8 attribute, and the value ip leaves unprinted outside -j.
type VxlanBoolOption struct {
	Key     string
	Type    uint16
	Default bool
}

// VxlanBoolOptions is vxlan_opts, in print order.
var VxlanBoolOptions = [...]VxlanBoolOption{
	{Key: "external", Type: unix.IFLA_VXLAN_COLLECT_METADATA},
	{Key: "vnifilter", Type: unix.IFLA_VXLAN_VNIFILTER},
	{Key: "learning", Type: unix.IFLA_VXLAN_LEARNING, Default: true},
	{Key: "proxy", Type: unix.IFLA_VXLAN_PROXY},
	{Key: "rsc", Type: unix.IFLA_VXLAN_RSC},
	{Key: "l2miss", Type: unix.IFLA_VXLAN_L2MISS},
	{Key: "l3miss", Type: unix.IFLA_VXLAN_L3MISS},
	{Key: "udp_csum", Type: unix.IFLA_VXLAN_UDP_CSUM, Default: true},
	{Key: "udp_zero_csum6_tx", Type: unix.IFLA_VXLAN_UDP_ZERO_CSUM6_TX},
	{Key: "udp_zero_csum6_rx", Type: unix.IFLA_VXLAN_UDP_ZERO_CSUM6_RX},
	{Key: "remcsum_tx", Type: unix.IFLA_VXLAN_REMCSUM_TX},
	{Key: "remcsum_rx", Type: unix.IFLA_VXLAN_REMCSUM_RX},
	{Key: "localbypass", Type: unix.IFLA_VXLAN_LOCALBYPASS, Default: true},
	{Key: "mcroute", Type: IflaVxlanMcRoute},
}

func vxlanBoolIndex(atype uint16) int {
	for i := range VxlanBoolOptions {
		if VxlanBoolOptions[i].Type == atype {
			return i
		}
	}
	return -1
}

// VxlanInfo is IFLA_INFO_DATA decoded for kind "vxlan". Each Has* says the
// kernel sent the attribute at its full width.
type VxlanInfo struct {
	ID    uint32
	HasID bool
	// Group is IFLA_VXLAN_GROUP, or IFLA_VXLAN_GROUP6 when GROUP is absent,
	// as vxlan_print_opt reads them; the zero Addr when neither was sent.
	// It is the group when multicast and the remote otherwise.
	Group netip.Addr
	// Local is IFLA_VXLAN_LOCAL, or IFLA_VXLAN_LOCAL6 when LOCAL is absent.
	Local         netip.Addr
	Link          uint32
	HasLink       bool
	TTL           uint8
	HasTTL        bool
	TTLInherit    uint8
	HasTTLInherit bool
	TOS           uint8
	HasTOS        bool
	DF            uint8
	HasDF         bool
	// Label is the flow label in host order; the wire carries it htonl'd.
	Label       uint32
	HasLabel    bool
	Aging       uint32
	HasAging    bool
	Limit       uint32
	HasLimit    bool
	LabelPolicy uint32
	// HasLabelPolicy: ip neither sends nor prints IFLA_VXLAN_LABEL_POLICY.
	HasLabelPolicy bool
	// PortLow and PortHigh are IFLA_VXLAN_PORT_RANGE, Port IFLA_VXLAN_PORT,
	// all big-endian on the wire.
	PortLow      uint16
	PortHigh     uint16
	HasPortRange bool
	Port         uint16
	HasPort      bool
	// ReservedBits is the big-endian IFLA_VXLAN_RESERVED_BITS.
	ReservedBits    uint64
	HasReservedBits bool
	// GBP, GPE and RemcsumNopartial are flag attributes, set when sent.
	GBP              bool
	GPE              bool
	RemcsumNopartial bool
	// Bools are the u8 attributes of VxlanBoolOptions, by index.
	Bools    [len(VxlanBoolOptions)]uint8
	HasBools [len(VxlanBoolOptions)]bool
	// Unknown lists attribute ids this package does not decode, in wire
	// order.
	Unknown []uint16
}

// ParseVxlanInfoData decodes IFLA_INFO_DATA for kind "vxlan". nil for a nil
// nest. Tolerant as ParseBridgeInfoData is.
func ParseVxlanInfoData(nest []byte) *VxlanInfo {
	if nest == nil {
		return nil
	}
	v := &VxlanInfo{}
	var group6, local6 netip.Addr
	walkNestTolerant(nest, func(atype uint16, val []byte) {
		switch atype {
		case unix.IFLA_VXLAN_GROUP:
			v.Group = kindAddr4(val, v.Group)
		case unix.IFLA_VXLAN_GROUP6:
			group6 = kindAddr16(val, group6)
		case unix.IFLA_VXLAN_LOCAL:
			v.Local = kindAddr4(val, v.Local)
		case unix.IFLA_VXLAN_LOCAL6:
			local6 = kindAddr16(val, local6)
		case unix.IFLA_VXLAN_PORT_RANGE:
			if len(val) >= kindPortRangeLenCst {
				v.PortLow, v.PortHigh = binary.BigEndian.Uint16(val), binary.BigEndian.Uint16(val[2:])
				v.HasPortRange = true
			}
		case unix.IFLA_VXLAN_PORT:
			if len(val) >= 2 {
				v.Port, v.HasPort = binary.BigEndian.Uint16(val), true
			}
		case unix.IFLA_VXLAN_LABEL:
			if len(val) >= 4 {
				v.Label, v.HasLabel = binary.BigEndian.Uint32(val), true
			}
		case IflaVxlanReservedBits:
			if len(val) >= vxlanReservedLenCst {
				v.ReservedBits, v.HasReservedBits = binary.BigEndian.Uint64(val), true
			}
		case unix.IFLA_VXLAN_GBP:
			v.GBP = true
		case unix.IFLA_VXLAN_GPE:
			v.GPE = true
		case unix.IFLA_VXLAN_REMCSUM_NOPARTIAL:
			v.RemcsumNopartial = true
		default:
			v.parseScalar(atype, val)
		}
	})
	if !v.Group.Is4() && group6.IsValid() {
		v.Group = group6
	}
	if !v.Local.Is4() && local6.IsValid() {
		v.Local = local6
	}
	return v
}

// parseScalar decodes the u8 and u32 attributes, and lists the rest.
func (v *VxlanInfo) parseScalar(atype uint16, val []byte) {
	switch atype {
	case unix.IFLA_VXLAN_ID:
		kindU32(val, &v.ID, &v.HasID)
	case unix.IFLA_VXLAN_LINK:
		kindU32(val, &v.Link, &v.HasLink)
	case IflaVxlanAgeing:
		kindU32(val, &v.Aging, &v.HasAging)
	case unix.IFLA_VXLAN_LIMIT:
		kindU32(val, &v.Limit, &v.HasLimit)
	case unix.IFLA_VXLAN_LABEL_POLICY:
		kindU32(val, &v.LabelPolicy, &v.HasLabelPolicy)
	case unix.IFLA_VXLAN_TTL:
		kindU8(val, &v.TTL, &v.HasTTL)
	case unix.IFLA_VXLAN_TTL_INHERIT:
		kindU8(val, &v.TTLInherit, &v.HasTTLInherit)
	case unix.IFLA_VXLAN_TOS:
		kindU8(val, &v.TOS, &v.HasTOS)
	case unix.IFLA_VXLAN_DF:
		kindU8(val, &v.DF, &v.HasDF)
	default:
		if i := vxlanBoolIndex(atype); i >= 0 {
			kindU8(val, &v.Bools[i], &v.HasBools[i])
			return
		}
		v.Unknown = append(v.Unknown, atype)
	}
}

// VxlanOption names what a VxlanSetting sets.
type VxlanOption uint8

const (
	VxlanOptionNone VxlanOption = iota
	VxlanOptionID
	VxlanOptionGroup
	VxlanOptionRemote
	VxlanOptionLocal
	VxlanOptionLink
	VxlanOptionTTL
	VxlanOptionTTLInherit
	VxlanOptionTOS
	VxlanOptionDF
	VxlanOptionLabel
	VxlanOptionAging
	VxlanOptionLimit
	VxlanOptionSrcPort
	VxlanOptionDstPort
	VxlanOptionLearning
	VxlanOptionExternal
	VxlanOptionBool
	VxlanOptionReservedBits
	VxlanOptionGBP
	VxlanOptionGPE
)

// VxlanSetting is one option word of `type vxlan`. Value is the id, the
// lower's ifindex, the ttl (0 for auto), the tos (1 for inherit), the df,
// the flow label, the aging time (0 for none), the limit (0 for unlimited),
// srcport's low end, the dstport, the reserved bits, or 0 or 1 for
// learning, external and VxlanOptionBool; High is srcport's high end; Attr
// is VxlanOptionBool's IFLA_VXLAN_*; Addr is the group, remote or local.
type VxlanSetting struct {
	Option VxlanOption
	Value  uint64
	High   uint64
	Attr   uint16
	Addr   netip.Addr
}

// vxlanEnc is vxlan_parse_opt's state across the words.
type vxlanEnc struct {
	ab            AttrBuilder
	vni           uint64
	hasID         bool
	local, remote netip.Addr
	hasLink       bool
	learning      uint64
	learningGiven bool
	metadata      bool
	vnifilter     bool
	dstport       uint64
	portGiven     bool
	gpe           bool
}

// EncodeVxlanInfoData encodes the inside of IFLA_INFO_DATA for kind "vxlan"
// as vxlan_parse_opt does; set is an `ip link set`, which sends no
// NLM_F_CREATE. The words' own attributes go in the order given; then
// IFLA_VXLAN_ID if an id was given, the local, the group or remote,
// IFLA_VXLAN_LEARNING (on an add always, 0 after external or nolearning; on
// a set only after a learning word), and IFLA_VXLAN_PORT when nonzero, 4790
// for gpe without a dstport. No settings encode to nothing: iplink_parse
// calls no parse_opt when no option follows the kind. ip -4 and -6's
// default group, sent when no address is given, is not modeled. It refuses
// what vxlan_parse_opt refuses: ErrKindSettingConflict for group and
// remote, a local of the other family, vnifilter without external, external
// with an id, and a group without dev; ErrKindSettingMissing for no id on an
// add.
func EncodeVxlanInfoData(settings []VxlanSetting, set bool) ([]byte, error) {
	if len(settings) == 0 {
		return nil, nil
	}
	var raw [linkWriteBufCst]byte
	e := &vxlanEnc{ab: NewAttrBuilder(raw[:]), learning: 1}
	for _, s := range settings {
		if err := e.put(s); err != nil {
			return nil, err
		}
	}
	if err := e.finish(set); err != nil {
		return nil, err
	}
	return append([]byte(nil), e.ab.Bytes()...), nil
}

// put is one pass of vxlan_parse_opt's loop.
func (e *vxlanEnc) put(s VxlanSetting) error {
	switch s.Option {
	case VxlanOptionID:
		e.vni, e.hasID = s.Value, true
		return kindMax(s.Value, kindVniMaxCst)
	case VxlanOptionGroup, VxlanOptionRemote, VxlanOptionLocal:
		return e.putAddr(s)
	case VxlanOptionLink:
		e.hasLink = true
		return kindPutU32(&e.ab, unix.IFLA_VXLAN_LINK, s.Value)
	case VxlanOptionTTL:
		return kindPutU8(&e.ab, unix.IFLA_VXLAN_TTL, s.Value, kindU8MaxCst)
	case VxlanOptionTTLInherit:
		return e.ab.PutBytes(unix.IFLA_VXLAN_TTL_INHERIT, nil)
	case VxlanOptionTOS:
		return kindPutU8(&e.ab, unix.IFLA_VXLAN_TOS, s.Value, kindU8MaxCst)
	case VxlanOptionDF:
		return kindPutU8(&e.ab, unix.IFLA_VXLAN_DF, s.Value, uint64(VxlanDfInherit))
	case VxlanOptionLabel:
		if s.Value&^kindLabelMaskCst != 0 {
			return ErrKindSettingRange
		}
		return e.ab.PutBytes(unix.IFLA_VXLAN_LABEL, kindBE(s.Value, 4))
	case VxlanOptionAging:
		return kindPutU32(&e.ab, IflaVxlanAgeing, s.Value)
	case VxlanOptionLimit:
		return kindPutU32(&e.ab, unix.IFLA_VXLAN_LIMIT, s.Value)
	case VxlanOptionSrcPort:
		return e.putSrcPort(s)
	case VxlanOptionDstPort:
		e.dstport, e.portGiven = s.Value, true
		return kindMax(s.Value, kindU16MaxCst)
	case VxlanOptionLearning:
		e.learning, e.learningGiven = s.Value, true
		return kindMax(s.Value, 1)
	case VxlanOptionExternal:
		e.metadata = s.Value == 1
		if e.metadata {
			e.learning = 0
		}
		return kindPutU8(&e.ab, unix.IFLA_VXLAN_COLLECT_METADATA, s.Value, 1)
	case VxlanOptionBool:
		return e.putBool(s)
	case VxlanOptionReservedBits:
		return e.ab.PutBytes(IflaVxlanReservedBits, kindBE(s.Value, vxlanReservedLenCst))
	case VxlanOptionGBP:
		return e.ab.PutBytes(unix.IFLA_VXLAN_GBP, nil)
	case VxlanOptionGPE:
		e.gpe = true
		return e.ab.PutBytes(unix.IFLA_VXLAN_GPE, nil)
	default:
		return ErrKindSettingOption
	}
}

// putAddr checks a group (multicast), remote or local (not multicast)
// address; each is sent after the loop. A second group or remote, or a
// local of the other family, is refused as get_addr refuses it.
func (e *vxlanEnc) putAddr(s VxlanSetting) error {
	a := s.Addr
	if !a.IsValid() || a.IsMulticast() != (s.Option == VxlanOptionGroup) {
		return ErrKindSettingRange
	}
	other := e.local
	dst := &e.remote
	if s.Option == VxlanOptionLocal {
		other, dst = e.remote, &e.local
	}
	if dst.IsValid() || other.IsValid() && other.Is4() != a.Is4() {
		return ErrKindSettingConflict
	}
	*dst = a
	return nil
}

func (e *vxlanEnc) putSrcPort(s VxlanSetting) error {
	if s.Value > kindU16MaxCst || s.High > kindU16MaxCst {
		return ErrKindSettingRange
	}
	if s.Value == 0 && s.High == 0 {
		return nil
	}
	return e.ab.PutBytes(unix.IFLA_VXLAN_PORT_RANGE, append(kindBE(s.Value, 2), kindBE(s.High, 2)...))
}

// putBool writes one of the u8 words other than learning and external,
// which change the after-loop attributes.
func (e *vxlanEnc) putBool(s VxlanSetting) error {
	i := vxlanBoolIndex(s.Attr)
	if i < 0 || s.Attr == unix.IFLA_VXLAN_LEARNING || s.Attr == unix.IFLA_VXLAN_COLLECT_METADATA {
		return ErrKindSettingRange
	}
	if s.Attr == unix.IFLA_VXLAN_VNIFILTER && s.Value == 1 {
		e.vnifilter = true
	}
	return kindPutU8(&e.ab, s.Attr, s.Value, 1)
}

// finish is vxlan_parse_opt after its loop: the checks, then the
// attributes it holds back.
func (e *vxlanEnc) finish(set bool) error {
	switch {
	case !e.metadata && e.vnifilter, e.metadata && e.hasID, e.remote.IsMulticast() && !e.hasLink:
		return ErrKindSettingConflict
	case !e.metadata && !e.vnifilter && !e.hasID && !set:
		return ErrKindSettingMissing
	}
	if e.hasID {
		if err := kindPutU32(&e.ab, unix.IFLA_VXLAN_ID, e.vni); err != nil {
			return err
		}
	}
	if err := kindPutAddr(&e.ab, e.local, unix.IFLA_VXLAN_LOCAL, unix.IFLA_VXLAN_LOCAL6); err != nil {
		return err
	}
	if err := kindPutAddr(&e.ab, e.remote, unix.IFLA_VXLAN_GROUP, unix.IFLA_VXLAN_GROUP6); err != nil {
		return err
	}
	if !set || e.learningGiven {
		if err := kindPutU8(&e.ab, unix.IFLA_VXLAN_LEARNING, e.learning, 1); err != nil {
			return err
		}
	}
	port := e.dstport
	if !e.portGiven && e.gpe {
		port = vxlanGpePortCst
	}
	if port == 0 {
		return nil
	}
	return e.ab.PutBytes(unix.IFLA_VXLAN_PORT, kindBE(port, 2))
}
