package xtcpnl

import (
	"golang.org/x/sys/unix"
)

// The hsr IFLA_INFO_DATA (include/uapi/linux/if_link.h,
// net/hsr/hsr_netlink.c), as ip/iplink_hsr.c at v7.2.0 writes and prints it
// (docs/netlink/goip-link-kinds.md, "KP5-T01 The hsr parser").

const hsrKindCst = "hsr"

// The IFLA_HSR_PROTOCOL values: HSR_PROTOCOL_HSR and HSR_PROTOCOL_PRP.
const (
	HsrProtocolHSR = 0
	HsrProtocolPRP = 1
)

const (
	// hsrSupervisionAddrLenCst is ETH_ALEN, the supervision address's
	// length.
	hsrSupervisionAddrLenCst = 6
	// hsrSeqNrLenCst is the sequence number's u16 width.
	hsrSeqNrLenCst = 2
	// hsrIfindexLenCst is a slave's or the interlink's u32 width.
	hsrIfindexLenCst = 4
)

// HsrInfo is IFLA_INFO_DATA decoded for hsr. Each Has* says the kernel sent
// the attribute at its full width.
type HsrInfo struct {
	Slave1       uint32
	HasSlave1    bool
	Slave2       uint32
	HasSlave2    bool
	Interlink    uint32
	HasInterlink bool
	// SupervisionAddr is the multicast address supervision frames go to.
	SupervisionAddr    [hsrSupervisionAddrLenCst]byte
	HasSupervisionAddr bool
	SeqNr              uint16
	HasSeqNr           bool
	// Version is absent for PRP, which the kernel sends as Protocol 1.
	Version     uint8
	HasVersion  bool
	Protocol    uint8
	HasProtocol bool
	// Short says a slave, the interlink, the sequence number or the
	// supervision address came shorter than its width, which makes
	// hsr_print_opt print nothing at all.
	Short bool
	// Unknown lists attribute ids this package does not decode, in wire
	// order.
	Unknown []uint16
}

// ParseHsrInfoData decodes IFLA_INFO_DATA for hsr. nil for a nil nest.
// Tolerant as ParseBridgeInfoData is.
func ParseHsrInfoData(nest []byte) *HsrInfo {
	if nest == nil {
		return nil
	}
	g := &HsrInfo{}
	walkNestTolerant(nest, func(atype uint16, val []byte) {
		switch atype {
		case unix.IFLA_HSR_SLAVE1:
			g.ifindex(val, &g.Slave1, &g.HasSlave1)
		case unix.IFLA_HSR_SLAVE2:
			g.ifindex(val, &g.Slave2, &g.HasSlave2)
		case unix.IFLA_HSR_INTERLINK:
			g.ifindex(val, &g.Interlink, &g.HasInterlink)
		case unix.IFLA_HSR_SUPERVISION_ADDR:
			if len(val) < hsrSupervisionAddrLenCst {
				g.Short = true
				return
			}
			g.SupervisionAddr, g.HasSupervisionAddr = [hsrSupervisionAddrLenCst]byte(val), true
		case unix.IFLA_HSR_SEQ_NR:
			g.Short = g.Short || len(val) < hsrSeqNrLenCst
			kindU16(val, &g.SeqNr, &g.HasSeqNr)
		case unix.IFLA_HSR_VERSION:
			kindU8(val, &g.Version, &g.HasVersion)
		case unix.IFLA_HSR_PROTOCOL:
			kindU8(val, &g.Protocol, &g.HasProtocol)
		default:
			g.Unknown = append(g.Unknown, atype)
		}
	})
	return g
}

// ifindex reads a slave's or the interlink's ifindex, marking a short one.
func (g *HsrInfo) ifindex(val []byte, dst *uint32, has *bool) {
	g.Short = g.Short || len(val) < hsrIfindexLenCst
	kindU32(val, dst, has)
}

// HsrOption names what an HsrSetting sets.
type HsrOption uint8

const (
	HsrOptionNone HsrOption = iota
	// HsrOptionSupervision sets the supervision address's last byte.
	HsrOptionSupervision
	HsrOptionVersion
	HsrOptionProto
	HsrOptionSlave1
	HsrOptionSlave2
	HsrOptionInterlink
)

// HsrSetting is one option word of `type hsr`. Value is the supervision
// byte, the version, the protocol, or a slave's or the interlink's
// ifindex.
type HsrSetting struct {
	Option HsrOption
	Value  uint64
}

// EncodeHsrInfoData encodes the inside of IFLA_INFO_DATA for hsr as
// iplink_hsr.c's hsr_parse_opt does: each word's attribute as it is read,
// repeats included. MULTICAST_SPEC, VERSION and PROTOCOL are u8s, and any
// u8 passes, as ip's checks test get_u8's return rather than the value;
// SLAVE1, SLAVE2 and INTERLINK are u32 ifindexes. No settings encode to
// nothing. It refuses a value out of range or an ifindex of 0
// (ErrKindSettingRange).
func EncodeHsrInfoData(settings []HsrSetting) ([]byte, error) {
	if len(settings) == 0 {
		return nil, nil
	}
	var raw [linkWriteBufCst]byte
	ab := NewAttrBuilder(raw[:])
	for _, s := range settings {
		if err := hsrPut(&ab, s); err != nil {
			return nil, err
		}
	}
	return append([]byte(nil), ab.Bytes()...), nil
}

// hsrPut sends one word's attribute.
func hsrPut(ab *AttrBuilder, s HsrSetting) error {
	switch s.Option {
	case HsrOptionSupervision:
		return kindPutU8(ab, unix.IFLA_HSR_MULTICAST_SPEC, s.Value, kindU8MaxCst)
	case HsrOptionVersion:
		return kindPutU8(ab, unix.IFLA_HSR_VERSION, s.Value, kindU8MaxCst)
	case HsrOptionProto:
		return kindPutU8(ab, unix.IFLA_HSR_PROTOCOL, s.Value, kindU8MaxCst)
	case HsrOptionSlave1, HsrOptionSlave2, HsrOptionInterlink:
		if s.Value == 0 {
			return ErrKindSettingRange
		}
		atype := uint16(unix.IFLA_HSR_SLAVE1)
		switch s.Option {
		case HsrOptionSlave2:
			atype = unix.IFLA_HSR_SLAVE2
		case HsrOptionInterlink:
			atype = unix.IFLA_HSR_INTERLINK
		default:
		}
		return kindPutU32(ab, atype, s.Value)
	default:
		return ErrKindSettingOption
	}
}
