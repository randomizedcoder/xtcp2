package xtcpnl

import (
	"strings"
)

// The batadv IFLA_INFO_DATA encoder, as ip/iplink_batadv.c at v7.2.0 writes
// it (docs/netlink/goip-link-kinds.md, "KP5-T04 The batadv parser and the
// kinds with no parser"). The kernel has no fill_info for batadv, so
// nothing decodes.

// IflaBatadvAlgoName is IFLA_BATADV_ALGO_NAME, include/uapi/linux/batman_adv.h.
// x/sys/unix does not name it.
const IflaBatadvAlgoName uint16 = 1

// BatadvOption names what a BatadvSetting sets.
type BatadvOption uint8

const (
	BatadvOptionNone BatadvOption = iota
	// BatadvOptionRoutingAlgo is the ra word: the routing algorithm's name.
	BatadvOptionRoutingAlgo
)

// BatadvSetting is one ra word of `type batadv`.
type BatadvSetting struct {
	Option BatadvOption
	Name   string
}

// EncodeBatadvInfoData encodes the inside of IFLA_INFO_DATA for batadv as
// iplink_batadv.c's batadv_parse_opt does: one NUL-terminated ALGO_NAME per
// ra word, in order, repeats included, the name unchecked: looking it up is
// the kernel's. No settings encode to nothing. It refuses a name holding a
// NUL, which no argv word can (ErrKindSettingRange).
func EncodeBatadvInfoData(settings []BatadvSetting) ([]byte, error) {
	if len(settings) == 0 {
		return nil, nil
	}
	var raw [linkWriteBufCst]byte
	ab := NewAttrBuilder(raw[:])
	for _, s := range settings {
		if s.Option != BatadvOptionRoutingAlgo {
			return nil, ErrKindSettingOption
		}
		if strings.IndexByte(s.Name, 0) >= 0 {
			return nil, ErrKindSettingRange
		}
		if err := ab.PutBytes(IflaBatadvAlgoName, append([]byte(s.Name), 0)); err != nil {
			return nil, err
		}
	}
	return append([]byte(nil), ab.Bytes()...), nil
}
