package xtcpnl

// The ovpn IFLA_INFO_DATA (include/uapi/linux/if_link.h,
// drivers/net/ovpn/main.c), as ip/iplink_ovpn.c at v7.3.0 prints it
// (docs/netlink/goip-parity-next.md, "NP3 IFLA_OVPN_MODE").

const ovpnKindCst = "ovpn"

// IflaOvpnMode is IFLA_OVPN_MODE, u8: OVPN_MODE_P2P or OVPN_MODE_MP. The
// kernel always sends it, so a link made with no mode reports p2p.
const IflaOvpnMode uint16 = 1

// The IFLA_OVPN_MODE values.
const (
	OvpnModeP2P = 0
	OvpnModeMP  = 1
)

// OvpnInfo is IFLA_INFO_DATA decoded for ovpn.
type OvpnInfo struct {
	// Mode is the first byte of IFLA_OVPN_MODE, as rta_getattr_u8 reads
	// it, when HasMode. An empty one is absent: ovpn_print_opt prints
	// nothing for a payload shorter than a u8.
	Mode    uint8
	HasMode bool
}

// ParseOvpnInfoData decodes IFLA_INFO_DATA for ovpn. nil for a nil nest.
// Tolerant as ParseBridgeInfoData is.
func ParseOvpnInfoData(nest []byte) *OvpnInfo {
	if nest == nil {
		return nil
	}
	o := &OvpnInfo{}
	walkNestTolerant(nest, func(atype uint16, val []byte) {
		if atype == IflaOvpnMode && !o.HasMode && len(val) >= 1 {
			o.Mode, o.HasMode = val[0], true
		}
	})
	return o
}
