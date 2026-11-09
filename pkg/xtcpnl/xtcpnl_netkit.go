package xtcpnl

import (
	"golang.org/x/sys/unix"
)

// The netkit IFLA_INFO_DATA (include/uapi/linux/if_link.h,
// drivers/net/netkit.c), as ip/iplink_netkit.c at v7.2.0 writes and prints
// it (docs/netlink/goip-link-kinds.md, "KP5-T02 The netkit parser").

const netkitKindCst = "netkit"

// IFLA_NETKIT_* past IFLA_NETKIT_MODE, absent from x/sys v0.49.0.
// linux/include/uapi/linux/if_link.h (Linux 7.2 headers)
const (
	IflaNetkitScrub     = 6
	IflaNetkitPeerScrub = 7
	IflaNetkitHeadroom  = 8
	IflaNetkitTailroom  = 9
	IflaNetkitPairing   = 10
)

// The netkit_scrub values, absent from x/sys v0.49.0.
const (
	NetkitScrubNone    = 0
	NetkitScrubDefault = 1
)

// NetkitModeNames, NetkitPolicyNames and NetkitScrubNames are
// netkit_print_opt's names; a value not here prints UNKNOWN.
var (
	NetkitModeNames   = map[uint32]string{unix.NETKIT_L2: "l2", unix.NETKIT_L3: "l3"}
	NetkitPolicyNames = map[uint32]string{unix.NETKIT_PASS: "forward", unix.NETKIT_DROP: "blackhole"}
	NetkitScrubNames  = map[uint32]string{NetkitScrubNone: "none", NetkitScrubDefault: "default"}
)

// NetkitInfo is IFLA_INFO_DATA decoded for netkit. Each Has* says the kernel
// sent the attribute at its full width.
type NetkitInfo struct {
	// Primary is 1 on the device the pair was created through, 0 on its
	// peer.
	Primary       uint8
	HasPrimary    bool
	Policy        uint32
	HasPolicy     bool
	PeerPolicy    uint32
	HasPeerPolicy bool
	Mode          uint32
	HasMode       bool
	// Scrub is absent on a single device.
	Scrub        uint32
	HasScrub     bool
	PeerScrub    uint32
	HasPeerScrub bool
	Headroom     uint16
	HasHeadroom  bool
	Tailroom     uint16
	HasTailroom  bool
	Pairing      uint32
	HasPairing   bool
	// Unknown lists attribute ids this package does not decode, in wire
	// order.
	Unknown []uint16
}

// ParseNetkitInfoData decodes IFLA_INFO_DATA for netkit. nil for a nil
// nest. Tolerant as ParseBridgeInfoData is.
func ParseNetkitInfoData(nest []byte) *NetkitInfo {
	if nest == nil {
		return nil
	}
	g := &NetkitInfo{}
	walkNestTolerant(nest, func(atype uint16, val []byte) {
		switch atype {
		case unix.IFLA_NETKIT_PRIMARY:
			kindU8(val, &g.Primary, &g.HasPrimary)
		case unix.IFLA_NETKIT_POLICY:
			kindU32(val, &g.Policy, &g.HasPolicy)
		case unix.IFLA_NETKIT_PEER_POLICY:
			kindU32(val, &g.PeerPolicy, &g.HasPeerPolicy)
		case unix.IFLA_NETKIT_MODE:
			kindU32(val, &g.Mode, &g.HasMode)
		case IflaNetkitScrub:
			kindU32(val, &g.Scrub, &g.HasScrub)
		case IflaNetkitPeerScrub:
			kindU32(val, &g.PeerScrub, &g.HasPeerScrub)
		case IflaNetkitHeadroom:
			kindU16(val, &g.Headroom, &g.HasHeadroom)
		case IflaNetkitTailroom:
			kindU16(val, &g.Tailroom, &g.HasTailroom)
		case IflaNetkitPairing:
			kindU32(val, &g.Pairing, &g.HasPairing)
		default:
			g.Unknown = append(g.Unknown, atype)
		}
	})
	return g
}

// NetkitOption names what a NetkitSetting sets.
type NetkitOption uint8

const (
	NetkitOptionNone NetkitOption = iota
	// NetkitOptionMode takes NETKIT_L2 or NETKIT_L3.
	NetkitOptionMode
	// NetkitOptionPolicy takes NETKIT_PASS (forward) or NETKIT_DROP
	// (blackhole), for the peer after NetkitOptionPeer.
	NetkitOptionPolicy
	// NetkitOptionScrub takes NetkitScrubNone or NetkitScrubDefault, for
	// the peer after NetkitOptionPeer.
	NetkitOptionScrub
	// NetkitOptionPeer is the peer word: the policy and scrub words after
	// it are the peer's.
	NetkitOptionPeer
)

// NetkitSetting is one of netkit's own words after `type netkit`.
type NetkitSetting struct {
	Option NetkitOption
	Value  uint64
}

// EncodeNetkitInfoData encodes the inside of IFLA_INFO_DATA for netkit as
// iplink_netkit.c's netkit_parse_opt does: MODE, POLICY (PEER_POLICY after
// peer) and SCRUB (PEER_SCRUB after peer) as each word is read, then, when
// peer is non-nil, PEER_INFO holding the peer's ifinfomsg and attributes
// (the words after peer that aren't netkit's, which iplink_parse takes).
// Settings that send nothing, peer alone, encode to an empty, non-nil
// slice; no settings to nil. It refuses a second mode or peer
// (ErrKindSettingConflict), a mode, policy or scrub outside its names
// (ErrKindSettingRange), and a peer half with no peer word
// (ErrKindSettingMissing).
func EncodeNetkitInfoData(settings []NetkitSetting, peer *VethPeer) ([]byte, error) {
	if len(settings) == 0 && peer == nil {
		return nil, nil
	}
	var raw [linkWriteBufCst]byte
	ab := NewAttrBuilder(raw[:])
	seenMode, seenPeer := false, false
	for _, s := range settings {
		var err error
		switch s.Option {
		case NetkitOptionMode:
			if seenMode {
				return nil, ErrKindSettingConflict
			}
			seenMode = true
			err = netkitPut(&ab, unix.IFLA_NETKIT_MODE, s.Value, NetkitModeNames)
		case NetkitOptionPolicy:
			atype := uint16(unix.IFLA_NETKIT_POLICY)
			if seenPeer {
				atype = unix.IFLA_NETKIT_PEER_POLICY
			}
			err = netkitPut(&ab, atype, s.Value, NetkitPolicyNames)
		case NetkitOptionScrub:
			atype := uint16(IflaNetkitScrub)
			if seenPeer {
				atype = IflaNetkitPeerScrub
			}
			err = netkitPut(&ab, atype, s.Value, NetkitScrubNames)
		case NetkitOptionPeer:
			if seenPeer {
				return nil, ErrKindSettingConflict
			}
			seenPeer = true
		default:
			return nil, ErrKindSettingOption
		}
		if err != nil {
			return nil, err
		}
	}
	if peer != nil {
		if !seenPeer {
			return nil, ErrKindSettingMissing
		}
		if err := putPeerNest(&ab, unix.IFLA_NETKIT_PEER_INFO, *peer); err != nil {
			return nil, err
		}
	}
	return append([]byte{}, ab.Bytes()...), nil
}

// netkitPut sends a u32 whose value must be one of names'.
func netkitPut(ab *AttrBuilder, atype uint16, v uint64, names map[uint32]string) error {
	for k := range names {
		if uint64(k) == v {
			return kindPutU32(ab, atype, v)
		}
	}
	return ErrKindSettingRange
}
