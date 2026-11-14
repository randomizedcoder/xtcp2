package xtcpnl

import (
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// The macvlan and macvtap kinds' IFLA_INFO_DATA (include/uapi/linux/
// if_link.h, drivers/net/macvlan.c), as ip/iplink_macvlan.c at v7.2.0 writes
// and prints it (docs/netlink/goip-link-kinds.md, "KP2-T02 The macvlan and
// macvtap parser"). macvtap shares macvlan's netlink ops and ip's parser.

const (
	macvlanKindCst = "macvlan"
	macvtapKindCst = "macvtap"
)

// enum macvlan_mode.
const (
	MacvlanModePrivate  uint32 = 1
	MacvlanModeVepa     uint32 = 2
	MacvlanModeBridge   uint32 = 4
	MacvlanModePassthru uint32 = 8
	MacvlanModeSource   uint32 = 16
)

// MACVLAN_FLAG_*.
const (
	MacvlanFlagNopromisc uint16 = 1
	MacvlanFlagNodst     uint16 = 2
)

// enum macvlan_macaddr_mode.
const (
	MacvlanMacaddrAdd   uint64 = 0
	MacvlanMacaddrDel   uint64 = 1
	MacvlanMacaddrFlush uint64 = 2
	MacvlanMacaddrSet   uint64 = 3
)

// MacvlanModes are the modes' names, macvlan_parse_opt's words and
// macvlan_print_opt's output, by value.
var MacvlanModes = map[uint32]string{
	MacvlanModePrivate:  "private",
	MacvlanModeVepa:     "vepa",
	MacvlanModeBridge:   "bridge",
	MacvlanModePassthru: "passthru",
	MacvlanModeSource:   "source",
}

// MacvlanInfo is IFLA_INFO_DATA decoded for kind "macvlan" or "macvtap".
// Each Has* says the kernel sent the attribute at its full width.
type MacvlanInfo struct {
	Mode              uint32
	HasMode           bool
	Flags             uint16
	HasFlags          bool
	BcQueueLen        uint32
	HasBcQueueLen     bool
	BcQueueLenUsed    uint32
	HasBcQueueLenUsed bool
	BcCutoff          int32
	HasBcCutoff       bool
	MacaddrCount      uint32
	HasMacaddrCount   bool
	// Macaddrs are the source addresses under IFLA_MACVLAN_MACADDR_DATA,
	// in the kernel's order; HasMacaddrData says the nest was sent.
	Macaddrs       [][6]byte
	HasMacaddrData bool
	// Unknown lists attribute ids past IFLA_MACVLAN_BC_CUTOFF, in wire
	// order. The request-only MACADDR_MODE and MACADDR are not listed.
	Unknown []uint16
}

// ParseMacvlanInfoData decodes IFLA_INFO_DATA for kind "macvlan" or
// "macvtap". nil for a nil nest. Tolerant as ParseBridgeInfoData is.
func ParseMacvlanInfoData(nest []byte) *MacvlanInfo {
	if nest == nil {
		return nil
	}
	m := &MacvlanInfo{}
	walkNestTolerant(nest, func(atype uint16, val []byte) {
		switch atype {
		case unix.IFLA_MACVLAN_MODE:
			m.Mode, m.HasMode = macvlanU32(val)
		case unix.IFLA_MACVLAN_FLAGS:
			if len(val) >= 2 {
				m.Flags, m.HasFlags = binary.LittleEndian.Uint16(val), true
			}
		case unix.IFLA_MACVLAN_MACADDR_MODE, unix.IFLA_MACVLAN_MACADDR:
		case unix.IFLA_MACVLAN_MACADDR_DATA:
			m.Macaddrs, m.HasMacaddrData = parseMacvlanMacaddrs(val), true
		case unix.IFLA_MACVLAN_MACADDR_COUNT:
			m.MacaddrCount, m.HasMacaddrCount = macvlanU32(val)
		case unix.IFLA_MACVLAN_BC_QUEUE_LEN:
			m.BcQueueLen, m.HasBcQueueLen = macvlanU32(val)
		case unix.IFLA_MACVLAN_BC_QUEUE_LEN_USED:
			m.BcQueueLenUsed, m.HasBcQueueLenUsed = macvlanU32(val)
		case unix.IFLA_MACVLAN_BC_CUTOFF:
			if _, err := binary.Decode(val, binary.LittleEndian, &m.BcCutoff); err == nil {
				m.HasBcCutoff = true
			}
		default:
			m.Unknown = append(m.Unknown, atype)
		}
	})
	return m
}

func macvlanU32(val []byte) (uint32, bool) {
	if len(val) < 4 {
		return 0, false
	}
	return binary.LittleEndian.Uint32(val), true
}

// parseMacvlanMacaddrs decodes the source list, skipping an entry that is
// not a whole IFLA_MACVLAN_MACADDR, as macvlan_print_opt does.
func parseMacvlanMacaddrs(nest []byte) [][6]byte {
	var out [][6]byte
	walkNestTolerant(nest, func(atype uint16, val []byte) {
		if atype != unix.IFLA_MACVLAN_MACADDR || len(val) < bridgeEtherAddrLenCst {
			return
		}
		out = append(out, [6]byte(val[:bridgeEtherAddrLenCst]))
	})
	return out
}

// MacvlanOption names what a MacvlanSetting sets.
type MacvlanOption uint8

const (
	MacvlanOptionNone MacvlanOption = iota
	MacvlanOptionMode
	MacvlanOptionFlag
	MacvlanOptionMacaddr
	MacvlanOptionBcQueueLen
	MacvlanOptionBcCutoff
)

// MacvlanSetting is one option word of `type macvlan`: Value is the mode, a
// flag's MACVLAN_FLAG_* (0 for `flag null`), the macaddr op, or the queue
// length; Cutoff is bclim's value; Addrs are the macaddr op's addresses.
type MacvlanSetting struct {
	Option MacvlanOption
	Value  uint64
	Cutoff int32
	Addrs  [][]byte
}

// ErrMacvlanNopromisc is macvlan_parse_opt's refusal of nopromisc outside
// passthru mode.
var ErrMacvlanNopromisc = errors.New("xtcpnl: macvlan nopromisc flag only valid in passthru mode")

// EncodeMacvlanInfoData encodes the inside of IFLA_INFO_DATA for kind
// "macvlan" or "macvtap" as macvlan_parse_opt does: the macaddr ops, the
// queue length and the cutoff in the order given, which is argv order; then
// IFLA_MACVLAN_MODE if a mode was given (the last one wins), then
// IFLA_MACVLAN_FLAGS if any flag word was.
func EncodeMacvlanInfoData(settings []MacvlanSetting) ([]byte, error) {
	var raw [linkWriteBufCst]byte
	ab := NewAttrBuilder(raw[:])
	var mode, flags uint64
	hasFlags := false
	for _, s := range settings {
		var err error
		switch s.Option {
		case MacvlanOptionMode:
			if !macvlanModeKnown(s.Value) {
				return nil, ErrKindSettingRange
			}
			mode = s.Value
		case MacvlanOptionFlag:
			if s.Value&^uint64(MacvlanFlagNopromisc|MacvlanFlagNodst) != 0 {
				return nil, ErrKindSettingRange
			}
			flags |= s.Value
			hasFlags = true
		case MacvlanOptionMacaddr:
			err = putMacvlanMacaddr(&ab, s)
		case MacvlanOptionBcQueueLen:
			err = putMacvlanU32(&ab, unix.IFLA_MACVLAN_BC_QUEUE_LEN, s.Value)
		case MacvlanOptionBcCutoff:
			err = ab.PutS32(unix.IFLA_MACVLAN_BC_CUTOFF, s.Cutoff)
		default:
			err = ErrKindSettingOption
		}
		if err != nil {
			return nil, err
		}
	}
	if mode != 0 {
		if err := putMacvlanU32(&ab, unix.IFLA_MACVLAN_MODE, mode); err != nil {
			return nil, err
		}
	}
	if hasFlags {
		if flags&uint64(MacvlanFlagNopromisc) != 0 && mode != uint64(MacvlanModePassthru) {
			return nil, ErrMacvlanNopromisc
		}
		le := binary.LittleEndian.AppendUint64(nil, flags)
		if err := ab.PutBytes(unix.IFLA_MACVLAN_FLAGS, le[:2]); err != nil {
			return nil, err
		}
	}
	return append([]byte(nil), ab.Bytes()...), nil
}

func macvlanModeKnown(v uint64) bool {
	for m := range MacvlanModes {
		if uint64(m) == v {
			return true
		}
	}
	return false
}

func putMacvlanU32(ab *AttrBuilder, atype uint16, v uint64) error {
	if v>>32 != 0 {
		return ErrKindSettingRange
	}
	return ab.PutBytes(atype, binary.LittleEndian.AppendUint64(nil, v)[:4])
}

// putMacvlanMacaddr writes MACADDR_MODE, then one MACADDR for add and del,
// or a MACADDR_DATA nest of every address for set, or nothing for flush.
func putMacvlanMacaddr(ab *AttrBuilder, s MacvlanSetting) error {
	for _, a := range s.Addrs {
		if len(a) != bridgeEtherAddrLenCst {
			return ErrKindSettingRange
		}
	}
	single := s.Value == MacvlanMacaddrAdd || s.Value == MacvlanMacaddrDel
	switch {
	case single && len(s.Addrs) != 1, s.Value == MacvlanMacaddrFlush && len(s.Addrs) != 0, s.Value > MacvlanMacaddrSet:
		return ErrKindSettingRange
	}
	if err := putMacvlanU32(ab, unix.IFLA_MACVLAN_MACADDR_MODE, s.Value); err != nil {
		return err
	}
	if single {
		return ab.PutBytes(unix.IFLA_MACVLAN_MACADDR, s.Addrs[0])
	}
	if s.Value != MacvlanMacaddrSet {
		return nil
	}
	m, err := ab.BeginNest(unix.IFLA_MACVLAN_MACADDR_DATA)
	if err != nil {
		return err
	}
	for _, a := range s.Addrs {
		if err := ab.PutBytes(unix.IFLA_MACVLAN_MACADDR, a); err != nil {
			return err
		}
	}
	return ab.EndNest(m)
}
