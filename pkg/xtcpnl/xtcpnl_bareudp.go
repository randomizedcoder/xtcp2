package xtcpnl

import (
	"encoding/binary"

	"golang.org/x/sys/unix"
)

// The bareudp kind's IFLA_INFO_DATA (include/uapi/linux/if_link.h,
// drivers/net/bareudp.c), as ip/iplink_bareudp.c at v7.2.0 writes and prints
// it (docs/netlink/goip-link-kinds.md, "KP3-T03 The bareudp and amt
// parsers").

const bareudpKindCst = "bareudp"

// BareudpInfo is IFLA_INFO_DATA decoded for kind "bareudp". Each Has* says
// the kernel sent the attribute at its full width.
type BareudpInfo struct {
	// Port is the big-endian IFLA_BAREUDP_PORT, in host order.
	Port    uint16
	HasPort bool
	// EtherType is the big-endian IFLA_BAREUDP_ETHERTYPE, in host order:
	// 0x8847 for mpls_uc. ll_proto_n2a prints it by name, or as "[%d]".
	EtherType    uint16
	HasEtherType bool
	// SrcPortMin is IFLA_BAREUDP_SRCPORT_MIN, a host-order u16.
	SrcPortMin    uint16
	HasSrcPortMin bool
	// Multiproto is the IFLA_BAREUDP_MULTIPROTO_MODE flag, set when sent;
	// ip prints it as a bool either way.
	Multiproto bool
	// Unknown lists attribute ids this package does not decode, in wire
	// order.
	Unknown []uint16
}

// ParseBareudpInfoData decodes IFLA_INFO_DATA for kind "bareudp". nil for a
// nil nest. Tolerant as ParseBridgeInfoData is.
func ParseBareudpInfoData(nest []byte) *BareudpInfo {
	if nest == nil {
		return nil
	}
	b := &BareudpInfo{}
	walkNestTolerant(nest, func(atype uint16, val []byte) {
		switch atype {
		case unix.IFLA_BAREUDP_PORT:
			if len(val) >= 2 {
				b.Port, b.HasPort = binary.BigEndian.Uint16(val), true
			}
		case unix.IFLA_BAREUDP_ETHERTYPE:
			if len(val) >= 2 {
				b.EtherType, b.HasEtherType = binary.BigEndian.Uint16(val), true
			}
		case unix.IFLA_BAREUDP_SRCPORT_MIN:
			if len(val) >= 2 {
				b.SrcPortMin, b.HasSrcPortMin = binary.LittleEndian.Uint16(val), true
			}
		case unix.IFLA_BAREUDP_MULTIPROTO_MODE:
			b.Multiproto = true
		default:
			b.Unknown = append(b.Unknown, atype)
		}
	})
	return b
}

// BareudpOption names what a BareudpSetting sets.
type BareudpOption uint8

const (
	BareudpOptionNone BareudpOption = iota
	BareudpOptionDstPort
	BareudpOptionEtherType
	BareudpOptionSrcPortMin
	BareudpOptionMultiproto
)

// BareudpSetting is one option word of `type bareudp`. Value is the dstport,
// the ethertype in host order (ll_proto_a2n's number for a name), the
// srcportmin, or 1 for multiproto and 0 for nomultiproto.
type BareudpSetting struct {
	Option BareudpOption
	Value  uint64
}

// EncodeBareudpInfoData encodes the inside of IFLA_INFO_DATA for kind
// "bareudp" as bareudp_parse_opt does. Nothing goes in the words' order:
// after the loop it sends PORT and ETHERTYPE big-endian, SRCPORT_MIN when
// given, and MULTIPROTO_MODE when the last of multiproto and nomultiproto
// was multiproto. An add and a set encode alike. No settings encode to
// nothing. It refuses a word given twice (multiproto and nomultiproto count
// as one) with ErrKindSettingConflict, and a missing dstport or ethertype
// with ErrKindSettingMissing.
func EncodeBareudpInfoData(settings []BareudpSetting) ([]byte, error) {
	if len(settings) == 0 {
		return nil, nil
	}
	var val [BareudpOptionMultiproto + 1]uint64
	var given [BareudpOptionMultiproto + 1]bool
	for _, s := range settings {
		if s.Option == BareudpOptionNone || s.Option > BareudpOptionMultiproto {
			return nil, ErrKindSettingOption
		}
		if given[s.Option] {
			return nil, ErrKindSettingConflict
		}
		maxV := uint64(kindU16MaxCst)
		if s.Option == BareudpOptionMultiproto {
			maxV = 1
		}
		if err := kindMax(s.Value, maxV); err != nil {
			return nil, err
		}
		val[s.Option], given[s.Option] = s.Value, true
	}
	if !given[BareudpOptionDstPort] || !given[BareudpOptionEtherType] {
		return nil, ErrKindSettingMissing
	}
	var raw [linkWriteBufCst]byte
	ab := NewAttrBuilder(raw[:])
	if err := ab.PutBytes(unix.IFLA_BAREUDP_PORT, kindBE(val[BareudpOptionDstPort], 2)); err != nil {
		return nil, err
	}
	if err := ab.PutBytes(unix.IFLA_BAREUDP_ETHERTYPE, kindBE(val[BareudpOptionEtherType], 2)); err != nil {
		return nil, err
	}
	if given[BareudpOptionSrcPortMin] {
		le := binary.LittleEndian.AppendUint64(nil, val[BareudpOptionSrcPortMin])[:2]
		if err := ab.PutBytes(unix.IFLA_BAREUDP_SRCPORT_MIN, le); err != nil {
			return nil, err
		}
	}
	if val[BareudpOptionMultiproto] == 1 {
		if err := ab.PutBytes(unix.IFLA_BAREUDP_MULTIPROTO_MODE, nil); err != nil {
			return nil, err
		}
	}
	return append([]byte(nil), ab.Bytes()...), nil
}
