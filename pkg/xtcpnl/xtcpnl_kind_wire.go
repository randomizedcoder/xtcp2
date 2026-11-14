package xtcpnl

import (
	"encoding/binary"
	"errors"
	"math/bits"
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

func kindU16(val []byte, dst *uint16, has *bool) {
	if len(val) >= 2 {
		*dst, *has = binary.LittleEndian.Uint16(val), true
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

// kindMulticast is set_address_type's multicast test, IN_MULTICAST or
// IN6_IS_ADDR_MULTICAST on the bytes: unlike netip's IsMulticast, a
// v4-mapped 239.1.1.1 is not multicast.
func kindMulticast(a netip.Addr) bool {
	if a.Is4() {
		return a.As4()[0]&0xf0 == 0xe0
	}
	return a.As16()[0] == 0xff
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

// TUNNEL_ENCAP_* (include/uapi/linux/if_tunnel.h): the encap types ip
// parses, and the encap flag bits.
const (
	TunnelEncapNone        = 0
	TunnelEncapFOU         = 1
	TunnelEncapGUE         = 2
	TunnelEncapFlagCsum    = 1
	TunnelEncapFlagCsum6   = 2
	TunnelEncapFlagRemcsum = 4
)

// TunnelEncap is the encap block gre, gre6, ipip, sit and ip6tnl share:
// ENCAP_TYPE, ENCAP_FLAGS (host order) and the big-endian ENCAP_SPORT and
// ENCAP_DPORT, at consecutive ids from a per-space base. Sport and Dport are
// in host order. Each Has* says the kernel sent the attribute at its full
// width.
type TunnelEncap struct {
	Type     uint16
	HasType  bool
	Flags    uint16
	HasFlags bool
	Sport    uint16
	HasSport bool
	Dport    uint16
	HasDport bool
}

// parse decodes atype into e when it is one of the four ids from base, and
// says whether it was.
func (e *TunnelEncap) parse(base, atype uint16, val []byte) bool {
	if atype < base || atype > base+3 || len(val) < 2 {
		return atype >= base && atype <= base+3
	}
	switch atype - base {
	case 0:
		e.Type, e.HasType = binary.LittleEndian.Uint16(val), true
	case 1:
		e.Flags, e.HasFlags = binary.LittleEndian.Uint16(val), true
	case 2:
		e.Sport, e.HasSport = binary.BigEndian.Uint16(val), true
	default:
		e.Dport, e.HasDport = binary.BigEndian.Uint16(val), true
	}
	return true
}

// tunnelEncapEnc is the encap words' state in a tunnel parser.
type tunnelEncapEnc struct {
	typ, flags, sport, dport uint64
}

// seed copies a prefetched block as ip's parsers read it: the ports with
// rta_getattr_u16 from big-endian attributes, so they come back
// byte-swapped once put sends them through htons.
func (t *tunnelEncapEnc) seed(e TunnelEncap) {
	if e.HasType {
		t.typ = uint64(e.Type)
	}
	if e.HasFlags {
		t.flags = uint64(e.Flags)
	}
	if e.HasSport {
		t.sport = uint64(bits.ReverseBytes16(e.Sport))
	}
	if e.HasDport {
		t.dport = uint64(bits.ReverseBytes16(e.Dport))
	}
}

// setFlag sets or clears an encap flag bit.
func (t *tunnelEncapEnc) setFlag(bit, on uint64) error {
	switch bit {
	case TunnelEncapFlagCsum, TunnelEncapFlagCsum6, TunnelEncapFlagRemcsum:
	default:
		return ErrKindSettingRange
	}
	if on == 1 {
		t.flags |= bit
	} else {
		t.flags &^= bit
	}
	return kindMax(on, 1)
}

// put sends the four attributes from base, in ip's order: type, flags,
// sport, dport.
func (t *tunnelEncapEnc) put(ab *AttrBuilder, base uint16) error {
	if err := kindPutU16(ab, base, t.typ); err != nil {
		return err
	}
	if err := kindPutU16(ab, base+1, t.flags); err != nil {
		return err
	}
	if err := kindMax(t.sport, kindU16MaxCst); err != nil {
		return err
	}
	if err := ab.PutBytes(base+2, kindBE(t.sport, 2)); err != nil {
		return err
	}
	if err := kindMax(t.dport, kindU16MaxCst); err != nil {
		return err
	}
	return ab.PutBytes(base+3, kindBE(t.dport, 2))
}

func kindPutU16(ab *AttrBuilder, atype uint16, v uint64) error {
	if v > kindU16MaxCst {
		return ErrKindSettingRange
	}
	return ab.PutBytes(atype, binary.LittleEndian.AppendUint64(nil, v)[:2])
}

// kindAddrSet is ip's is_addrtype_inet_not_unspec for a parsed address:
// given, and not 0.0.0.0 or ::.
func kindAddrSet(a netip.Addr) bool {
	return a.IsValid() && !a.IsUnspecified()
}
