package xtcpnl

import (
	"encoding/binary"
	"errors"
	"net/netip"
)

// Small wire helpers the link kinds' codecs share: tolerant fixed-width
// reads that set a present bit, and range-checked writes built without a
// narrowing conversion.

const (
	kindU8MaxCst  = 0xff
	kindU16MaxCst = 0xffff
	kindU32MaxCst = 0xffffffff
	// kindLabelMaskCst is LABEL_MAX_MASK, an IPv6 flow label's 20 bits.
	kindLabelMaskCst = 0xfffff
	// kindVniMaxCst is the highest 24-bit VNI.
	kindVniMaxCst = 1<<24 - 1
	// kindPortRangeLenCst is struct ifla_vxlan_port_range's (and geneve's)
	// size: two big-endian u16s.
	kindPortRangeLenCst = 4
)

// ErrKindSettingConflict is a kind parser's refusal of a combination of
// option words, each of which is valid alone: vxlan's group and remote,
// say, or geneve's external with an id.
var ErrKindSettingConflict = errors.New("xtcpnl: link kind options conflict")

// ErrKindSettingMissing is a kind parser's refusal for want of a word it
// requires: vxlan's or geneve's id, geneve's remote on an add, bareudp's
// dstport or ethertype.
var ErrKindSettingMissing = errors.New("xtcpnl: link kind option missing")

func kindU32(val []byte, dst *uint32, has *bool) {
	if len(val) >= 4 {
		*dst, *has = binary.LittleEndian.Uint32(val), true
	}
}

func kindU8(val []byte, dst *uint8, has *bool) {
	if len(val) >= 1 {
		*dst, *has = val[0], true
	}
}

func kindAddr4(val []byte, old netip.Addr) netip.Addr {
	if len(val) < bridgeIPv4LenCst {
		return old
	}
	return netip.AddrFrom4([4]byte(val[:bridgeIPv4LenCst]))
}

func kindAddr16(val []byte, old netip.Addr) netip.Addr {
	if len(val) < bridgeIPv6LenCst {
		return old
	}
	return netip.AddrFrom16([16]byte(val[:bridgeIPv6LenCst]))
}

func kindPutAddr(ab *AttrBuilder, a netip.Addr, type4, type6 uint16) error {
	switch {
	case !a.IsValid():
		return nil
	case a.Is4():
		b := a.As4()
		return ab.PutBytes(type4, b[:])
	default:
		b := a.As16()
		return ab.PutBytes(type6, b[:])
	}
}

func kindMax(v, maxV uint64) error {
	if v > maxV {
		return ErrKindSettingRange
	}
	return nil
}

func kindPutU8(ab *AttrBuilder, atype uint16, v, maxV uint64) error {
	if v > maxV {
		return ErrKindSettingRange
	}
	return ab.PutBytes(atype, binary.LittleEndian.AppendUint64(nil, v)[:1])
}

func kindPutU32(ab *AttrBuilder, atype uint16, v uint64) error {
	if v > kindU32MaxCst {
		return ErrKindSettingRange
	}
	return ab.PutBytes(atype, binary.LittleEndian.AppendUint64(nil, v)[:4])
}

// kindBE is v's low w bytes, big-endian.
func kindBE(v uint64, w int) []byte {
	return binary.BigEndian.AppendUint64(nil, v)[8-w:]
}
