package xtcpnl

import (
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// The vlan kind's IFLA_INFO_DATA (include/uapi/linux/if_link.h,
// net/8021q/vlan_netlink.c), as ip/iplink_vlan.c at v7.2.0 writes and
// prints it (docs/netlink/goip-link-kinds.md, "KP2-T01 The vlan parser").

const (
	vlanKindCst        = "vlan"
	vlanFlagsLenCst    = 8 // struct ifla_vlan_flags: flags, mask
	vlanQosMapLenCst   = 8 // struct ifla_vlan_qos_mapping: from, to
	vlanIDLenCst       = 2
	vlanProtocolLenCst = 2
)

// VlanFlag is one VLAN_FLAG_* bit and the words ip spells it with.
type VlanFlag struct {
	Keyword string // vlan_parse_opt's on/off keyword
	Name    string // vlan_print_flags' name
	Bit     uint32
}

// VlanFlags are the flags in vlan_print_flags' order, which is not bit
// order: MVRP (8) prints before LOOSE_BINDING (4).
var VlanFlags = []VlanFlag{
	{Keyword: "reorder_hdr", Name: "REORDER_HDR", Bit: 0x1},
	{Keyword: "gvrp", Name: "GVRP", Bit: 0x2},
	{Keyword: "mvrp", Name: "MVRP", Bit: 0x8},
	{Keyword: "loose_binding", Name: "LOOSE_BINDING", Bit: 0x4},
	{Keyword: "bridge_binding", Name: "BRIDGE_BINDING", Bit: 0x10},
}

// VlanQosMapping is struct ifla_vlan_qos_mapping.
type VlanQosMapping struct {
	From, To uint32
}

// VlanInfo is IFLA_INFO_DATA decoded for kind "vlan". Each Has* says the
// kernel sent the attribute at its full width.
type VlanInfo struct {
	ID          uint16
	HasID       bool
	Protocol    uint16 // host order; big-endian on the wire
	HasProtocol bool
	Flags, Mask uint32
	HasFlags    bool
	Ingress     []VlanQosMapping
	HasIngress  bool
	Egress      []VlanQosMapping
	HasEgress   bool
	// Unknown lists attribute ids past IFLA_VLAN_PROTOCOL, in wire order.
	Unknown []uint16
}

// ParseVlanInfoData decodes IFLA_INFO_DATA for kind "vlan". nil for a nil
// nest. Tolerant as ParseBridgeInfoData is: a malformed attribute ends the
// walk, and an attribute shorter than its struct is absent.
func ParseVlanInfoData(nest []byte) *VlanInfo {
	if nest == nil {
		return nil
	}
	v := &VlanInfo{}
	walkNestTolerant(nest, func(atype uint16, val []byte) {
		switch atype {
		case unix.IFLA_VLAN_ID:
			if len(val) >= vlanIDLenCst {
				v.ID, v.HasID = binary.LittleEndian.Uint16(val), true
			}
		case unix.IFLA_VLAN_FLAGS:
			if len(val) >= vlanFlagsLenCst {
				v.Flags = binary.LittleEndian.Uint32(val)
				v.Mask = binary.LittleEndian.Uint32(val[4:])
				v.HasFlags = true
			}
		case unix.IFLA_VLAN_EGRESS_QOS:
			v.Egress, v.HasEgress = parseVlanQosMap(val), true
		case unix.IFLA_VLAN_INGRESS_QOS:
			v.Ingress, v.HasIngress = parseVlanQosMap(val), true
		case unix.IFLA_VLAN_PROTOCOL:
			if len(val) >= vlanProtocolLenCst {
				v.Protocol, v.HasProtocol = binary.BigEndian.Uint16(val), true
			}
		default:
			v.Unknown = append(v.Unknown, atype)
		}
	})
	return v
}

// parseVlanQosMap decodes a qos nest's IFLA_VLAN_QOS_MAPPING entries,
// skipping one shorter than its struct.
func parseVlanQosMap(nest []byte) []VlanQosMapping {
	var out []VlanQosMapping
	walkNestTolerant(nest, func(atype uint16, val []byte) {
		if atype != unix.IFLA_VLAN_QOS_MAPPING || len(val) < vlanQosMapLenCst {
			return
		}
		out = append(out, VlanQosMapping{
			From: binary.LittleEndian.Uint32(val),
			To:   binary.LittleEndian.Uint32(val[4:]),
		})
	})
	return out
}

// VlanOption names what a VlanSetting sets.
type VlanOption uint8

const (
	VlanOptionNone VlanOption = iota
	VlanOptionID
	VlanOptionProtocol
	VlanOptionFlag
	VlanOptionIngressQos
	VlanOptionEgressQos
)

// VlanSetting is one option word of `type vlan`: Value is the id, the
// protocol in host order, or a flag's 0 or 1 (with Bit its VLAN_FLAG_*);
// Map is a qos map's pairs.
type VlanSetting struct {
	Option VlanOption
	Value  uint64
	Bit    uint32
	Map    []VlanQosMapping
}

// The kind encoders' errors, shared by every kind with a typed setting.
var (
	ErrKindSettingOption = errors.New("xtcpnl: link kind setting names no option")
	ErrKindSettingRange  = errors.New("xtcpnl: link kind setting value out of range")
)

// EncodeVlanInfoData encodes the inside of IFLA_INFO_DATA for kind "vlan"
// as vlan_parse_opt does: the id, the protocol and the qos maps in the
// order given, which is argv order, then one IFLA_VLAN_FLAGS for every flag
// word, last, when any was given. A later word for a bit overrides an
// earlier one; off sets the mask bit and clears the flag.
func EncodeVlanInfoData(settings []VlanSetting) ([]byte, error) {
	var raw [linkWriteBufCst]byte
	ab := NewAttrBuilder(raw[:])
	var flags, mask uint32
	for _, s := range settings {
		var err error
		switch s.Option {
		case VlanOptionID, VlanOptionProtocol:
			err = putVlanU16(&ab, s)
		case VlanOptionFlag:
			if s.Bit == 0 || s.Value > 1 {
				return nil, ErrKindSettingRange
			}
			mask |= s.Bit
			flags &^= s.Bit
			if s.Value == 1 {
				flags |= s.Bit
			}
		case VlanOptionIngressQos:
			err = putVlanQosMap(&ab, unix.IFLA_VLAN_INGRESS_QOS, s.Map)
		case VlanOptionEgressQos:
			err = putVlanQosMap(&ab, unix.IFLA_VLAN_EGRESS_QOS, s.Map)
		default:
			err = ErrKindSettingOption
		}
		if err != nil {
			return nil, err
		}
	}
	if mask != 0 {
		p := binary.LittleEndian.AppendUint32(make([]byte, 0, vlanFlagsLenCst), flags)
		p = binary.LittleEndian.AppendUint32(p, mask)
		if err := ab.PutBytes(unix.IFLA_VLAN_FLAGS, p); err != nil {
			return nil, err
		}
	}
	return append([]byte(nil), ab.Bytes()...), nil
}

func putVlanU16(ab *AttrBuilder, s VlanSetting) error {
	if s.Value > 0xffff {
		return ErrKindSettingRange
	}
	le := binary.LittleEndian.AppendUint64(make([]byte, 0, bridgeU64LenCst), s.Value)
	if s.Option == VlanOptionProtocol {
		return ab.PutBytes(unix.IFLA_VLAN_PROTOCOL, []byte{le[1], le[0]})
	}
	return ab.PutBytes(unix.IFLA_VLAN_ID, le[:vlanIDLenCst])
}

// putVlanQosMap opens the nest before its first pair, as
// vlan_parse_qos_map does, so an empty map is an empty nest.
func putVlanQosMap(ab *AttrBuilder, atype uint16, pairs []VlanQosMapping) error {
	m, err := ab.BeginNest(atype)
	if err != nil {
		return err
	}
	for _, p := range pairs {
		b := binary.LittleEndian.AppendUint32(make([]byte, 0, vlanQosMapLenCst), p.From)
		b = binary.LittleEndian.AppendUint32(b, p.To)
		if err := ab.PutBytes(unix.IFLA_VLAN_QOS_MAPPING, b); err != nil {
			return err
		}
	}
	return ab.EndNest(m)
}
