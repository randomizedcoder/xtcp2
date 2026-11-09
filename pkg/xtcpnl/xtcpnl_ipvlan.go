package xtcpnl

import (
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// The ipvlan and ipvtap kinds' IFLA_INFO_DATA (include/uapi/linux/
// if_link.h, drivers/net/ipvlan/ipvlan_main.c), as ip/iplink_ipvlan.c at
// v7.2.0 writes and prints it (docs/netlink/goip-link-kinds.md, "KP2-T03
// The ipvlan and ipvtap parser"). ipvtap shares ipvlan's attributes and
// ip's parser.

const (
	ipvlanKindCst    = "ipvlan"
	ipvtapKindCst    = "ipvtap"
	ipvlanAttrLenCst = 2
)

// IpvlanModes are enum ipvlan_mode's names, by value: IpvlanModes[mode].
var IpvlanModes = []string{"l2", "l3", "l3s"}

// IPVLAN_F_*; bridge is neither bit.
const (
	IpvlanFlagPrivate uint16 = 1
	IpvlanFlagVepa    uint16 = 2
)

// IpvlanInfo is IFLA_INFO_DATA decoded for kind "ipvlan" or "ipvtap". Each
// Has* says the attribute was exactly 2 bytes, the only width
// ipvlan_print_opt reads.
type IpvlanInfo struct {
	Mode     uint16
	HasMode  bool
	Flags    uint16
	HasFlags bool
	// Unknown lists attribute ids past IFLA_IPVLAN_FLAGS, in wire order.
	Unknown []uint16
}

// ParseIpvlanInfoData decodes IFLA_INFO_DATA for kind "ipvlan" or
// "ipvtap". nil for a nil nest. Tolerant as ParseBridgeInfoData is.
func ParseIpvlanInfoData(nest []byte) *IpvlanInfo {
	if nest == nil {
		return nil
	}
	v := &IpvlanInfo{}
	walkNestTolerant(nest, func(atype uint16, val []byte) {
		switch atype {
		case unix.IFLA_IPVLAN_MODE:
			if len(val) == ipvlanAttrLenCst {
				v.Mode, v.HasMode = binary.LittleEndian.Uint16(val), true
			}
		case unix.IFLA_IPVLAN_FLAGS:
			if len(val) == ipvlanAttrLenCst {
				v.Flags, v.HasFlags = binary.LittleEndian.Uint16(val), true
			}
		default:
			v.Unknown = append(v.Unknown, atype)
		}
	})
	return v
}

// IpvlanOption names what an IpvlanSetting sets.
type IpvlanOption uint8

const (
	IpvlanOptionNone IpvlanOption = iota
	IpvlanOptionMode
	IpvlanOptionFlag
)

// IpvlanSetting is one option word of `type ipvlan`: Value is the mode, or
// the flag word's IPVLAN_F_* (0 for bridge).
type IpvlanSetting struct {
	Option IpvlanOption
	Value  uint64
}

// ErrIpvlanSecondFlag is ipvlan_parse_opt's refusal of a second flag word,
// which it reports as an unknown option.
var ErrIpvlanSecondFlag = errors.New("xtcpnl: ipvlan takes one flag word")

// EncodeIpvlanInfoData encodes the inside of IFLA_INFO_DATA for kind
// "ipvlan" or "ipvtap" as ipvlan_parse_opt does: one IFLA_IPVLAN_MODE per
// mode word, in the order given, then IFLA_IPVLAN_FLAGS always, 0 without a
// flag word. No settings encode to nothing: iplink_parse calls no parse_opt
// when no option follows the kind.
func EncodeIpvlanInfoData(settings []IpvlanSetting) ([]byte, error) {
	if len(settings) == 0 {
		return nil, nil
	}
	var raw [linkWriteBufCst]byte
	ab := NewAttrBuilder(raw[:])
	var flags uint64
	flagGiven := false
	for _, s := range settings {
		switch s.Option {
		case IpvlanOptionMode:
			if s.Value >= uint64(len(IpvlanModes)) {
				return nil, ErrKindSettingRange
			}
			if err := putIpvlanU16(&ab, unix.IFLA_IPVLAN_MODE, s.Value); err != nil {
				return nil, err
			}
		case IpvlanOptionFlag:
			if flagGiven {
				return nil, ErrIpvlanSecondFlag
			}
			if s.Value != 0 && s.Value != uint64(IpvlanFlagPrivate) && s.Value != uint64(IpvlanFlagVepa) {
				return nil, ErrKindSettingRange
			}
			flags, flagGiven = s.Value, true
		default:
			return nil, ErrKindSettingOption
		}
	}
	if err := putIpvlanU16(&ab, unix.IFLA_IPVLAN_FLAGS, flags); err != nil {
		return nil, err
	}
	return append([]byte(nil), ab.Bytes()...), nil
}

func putIpvlanU16(ab *AttrBuilder, atype uint16, v uint64) error {
	return ab.PutBytes(atype, binary.LittleEndian.AppendUint64(nil, v)[:ipvlanAttrLenCst])
}
