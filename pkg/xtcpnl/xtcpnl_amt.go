package xtcpnl

import (
	"encoding/binary"
	"net/netip"
)

// The amt kind's IFLA_INFO_DATA (include/uapi/linux/if_link.h,
// drivers/net/amt.c), as ip/iplink_amt.c at v7.2.0 writes and prints it
// (docs/netlink/goip-link-kinds.md, "KP3-T03 The bareudp and amt parsers").

const amtKindCst = "amt"

// IFLA_AMT_*, absent from x/sys v0.49.0.
// linux/include/uapi/linux/if_link.h (Linux 7.2 headers)
const (
	IflaAmtMode        = 1
	IflaAmtRelayPort   = 2
	IflaAmtGatewayPort = 3
	IflaAmtLink        = 4
	IflaAmtLocalIP     = 5
	IflaAmtRemoteIP    = 6
	IflaAmtDiscoveryIP = 7
	IflaAmtMaxTunnels  = 8
)

// AmtModeNames are amt_print_opt's modename, indexed by IFLA_AMT_MODE:
// AMT_MODE_GATEWAY and AMT_MODE_RELAY. ip indexes the array unchecked, so a
// higher mode has no name.
var AmtModeNames = [...]string{"gateway", "relay"}

// AmtInfo is IFLA_INFO_DATA decoded for kind "amt". Each Has* says the
// kernel sent the attribute at its full width.
type AmtInfo struct {
	Mode    uint32
	HasMode bool
	// RelayPort and GatewayPort are the big-endian ports, in host order.
	RelayPort      uint16
	HasRelayPort   bool
	GatewayPort    uint16
	HasGatewayPort bool
	// Link is the lower device's ifindex.
	Link    uint32
	HasLink bool
	// Local, Remote and Discovery are the IPv4 addresses amt_print_opt
	// reads; the zero Addr when not sent. The kernel sends Remote only
	// when it has one.
	Local     netip.Addr
	Remote    netip.Addr
	Discovery netip.Addr
	// MaxTunnels is IFLA_AMT_MAX_TUNNELS.
	MaxTunnels    uint32
	HasMaxTunnels bool
	// Unknown lists attribute ids this package does not decode, in wire
	// order.
	Unknown []uint16
}

// ParseAmtInfoData decodes IFLA_INFO_DATA for kind "amt". nil for a nil
// nest. Tolerant as ParseBridgeInfoData is.
func ParseAmtInfoData(nest []byte) *AmtInfo {
	if nest == nil {
		return nil
	}
	a := &AmtInfo{}
	walkNestTolerant(nest, func(atype uint16, val []byte) {
		switch atype {
		case IflaAmtMode:
			kindU32(val, &a.Mode, &a.HasMode)
		case IflaAmtRelayPort:
			if len(val) >= 2 {
				a.RelayPort, a.HasRelayPort = binary.BigEndian.Uint16(val), true
			}
		case IflaAmtGatewayPort:
			if len(val) >= 2 {
				a.GatewayPort, a.HasGatewayPort = binary.BigEndian.Uint16(val), true
			}
		case IflaAmtLink:
			kindU32(val, &a.Link, &a.HasLink)
		case IflaAmtLocalIP:
			a.Local = kindAddr4(val, a.Local)
		case IflaAmtRemoteIP:
			a.Remote = kindAddr4(val, a.Remote)
		case IflaAmtDiscoveryIP:
			a.Discovery = kindAddr4(val, a.Discovery)
		case IflaAmtMaxTunnels:
			kindU32(val, &a.MaxTunnels, &a.HasMaxTunnels)
		default:
			a.Unknown = append(a.Unknown, atype)
		}
	})
	return a
}

// AmtOption names what an AmtSetting sets.
type AmtOption uint8

const (
	AmtOptionNone AmtOption = iota
	AmtOptionMode
	AmtOptionRelayPort
	AmtOptionGatewayPort
	AmtOptionMaxTunnels
	AmtOptionLink
	AmtOptionLocal
	AmtOptionDiscovery
)

// AmtSetting is one option word of `type amt`. Value is the mode (0 for
// gateway, 1 for relay), a port, max_tunnels, or the dev's ifindex; Addr is
// the local or discovery address.
type AmtSetting struct {
	Option AmtOption
	Value  uint64
	Addr   netip.Addr
}

// EncodeAmtInfoData encodes the inside of IFLA_INFO_DATA for kind "amt" as
// amt_parse_opt does: every word's attribute in the words' order, a word
// given twice sent twice, and an address sent at its family's length. An add
// and a set encode alike. No settings encode to nothing. It refuses what
// amt_parse_opt refuses: ErrKindSettingConflict for a second local or
// discovery, and ErrKindSettingRange for a mode past relay, a port past
// 65535, a dev of ifindex 0 or an invalid address. The kernel's own checks
// (a link and a local on an add, a discovery for a gateway) are left to it.
func EncodeAmtInfoData(settings []AmtSetting) ([]byte, error) {
	if len(settings) == 0 {
		return nil, nil
	}
	var raw [linkWriteBufCst]byte
	ab := NewAttrBuilder(raw[:])
	var local, discovery bool
	for _, s := range settings {
		if err := amtPut(&ab, s, &local, &discovery); err != nil {
			return nil, err
		}
	}
	return append([]byte(nil), ab.Bytes()...), nil
}

// amtPut is one pass of amt_parse_opt's loop.
func amtPut(ab *AttrBuilder, s AmtSetting, local, discovery *bool) error {
	switch s.Option {
	case AmtOptionMode:
		if err := kindMax(s.Value, uint64(len(AmtModeNames)-1)); err != nil {
			return err
		}
		return kindPutU32(ab, IflaAmtMode, s.Value)
	case AmtOptionRelayPort, AmtOptionGatewayPort:
		if err := kindMax(s.Value, kindU16MaxCst); err != nil {
			return err
		}
		atype := uint16(IflaAmtRelayPort)
		if s.Option == AmtOptionGatewayPort {
			atype = IflaAmtGatewayPort
		}
		return ab.PutBytes(atype, kindBE(s.Value, 2))
	case AmtOptionMaxTunnels:
		return kindPutU32(ab, IflaAmtMaxTunnels, s.Value)
	case AmtOptionLink:
		if s.Value == 0 {
			return ErrKindSettingRange
		}
		return kindPutU32(ab, IflaAmtLink, s.Value)
	case AmtOptionLocal:
		return amtPutAddr(ab, IflaAmtLocalIP, s.Addr, local)
	case AmtOptionDiscovery:
		return amtPutAddr(ab, IflaAmtDiscoveryIP, s.Addr, discovery)
	default:
		return ErrKindSettingOption
	}
}

func amtPutAddr(ab *AttrBuilder, atype uint16, a netip.Addr, seen *bool) error {
	switch {
	case *seen:
		return ErrKindSettingConflict
	case !a.IsValid():
		return ErrKindSettingRange
	}
	*seen = true
	return ab.PutBytes(atype, a.AsSlice())
}
