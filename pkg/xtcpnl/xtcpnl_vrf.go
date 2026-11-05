package xtcpnl

// The vrf IFLA_INFO_DATA encoder, as ip/iplink_vrf.c at v7.2.0 writes it
// (docs/netlink/goip-link-kinds.md, "KP5-T03 The vrf, vrf_slave and vxcan
// parsers"). The decoder is vrfTableAttr, beside setLinkInfoNest.

// VrfOption names what a VrfSetting sets.
type VrfOption uint8

const (
	VrfOptionNone VrfOption = iota
	// VrfOptionTable takes the routing table id, its name already resolved
	// (main is 254).
	VrfOptionTable
)

// VrfSetting is one table word of `type vrf`.
type VrfSetting struct {
	Option VrfOption
	Value  uint64
}

// EncodeVrfInfoData encodes the inside of IFLA_INFO_DATA for vrf as
// iplink_vrf.c's vrf_parse_opt does: one TABLE u32 per table word, in order,
// repeats included. Table 0 is sent: refusing it is the kernel's. No
// settings encode to nothing. It refuses a table past 32 bits
// (ErrKindSettingRange).
func EncodeVrfInfoData(settings []VrfSetting) ([]byte, error) {
	if len(settings) == 0 {
		return nil, nil
	}
	var raw [linkWriteBufCst]byte
	ab := NewAttrBuilder(raw[:])
	for _, s := range settings {
		if s.Option != VrfOptionTable {
			return nil, ErrKindSettingOption
		}
		if err := kindPutU32(&ab, IflaVrfTable, s.Value); err != nil {
			return nil, err
		}
	}
	return append([]byte(nil), ab.Bytes()...), nil
}
