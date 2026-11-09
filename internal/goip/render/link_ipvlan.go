package render

import "github.com/randomizedcoder/xtcp2/pkg/xtcpnl"

// This file is ipvlan_print_opt (ip/iplink_ipvlan.c, iproute2 v7.2.0),
// which ipvtap shares: the tokens `ip -d link show` prints after
// `    ipvlan ` or `    ipvtap ` (KG2-T04, docs/netlink/goip-link-kinds.md).
//
// The mode's format is " mode %s ", with a leading space, so the kind line
// reads `ipvlan  mode l3 `. Each attribute prints only at exactly 2 bytes,
// which the decoder's Has* already says. Exactly one flag word prints:
// private over vepa, and bridge for neither bit.

// The two kinds ipvlan_print_opt serves, exported for the `-d` gate.
const (
	IpvlanKindCst = "ipvlan"
	IpvtapKindCst = "ipvtap"
)

// ipvlanData is ipvlan_print_opt over the decoded IFLA_INFO_DATA.
func ipvlanData(v *xtcpnl.IpvlanInfo) *KindData {
	k := &KindData{}
	if v == nil {
		return k
	}
	if v.HasMode {
		mode := macvlanModeUnknownCst
		if int(v.Mode) < len(xtcpnl.IpvlanModes) {
			mode = xtcpnl.IpvlanModes[v.Mode]
		}
		k.add("mode", " mode "+mode+" ", mode)
	}
	if v.HasFlags {
		switch {
		case v.Flags&xtcpnl.IpvlanFlagPrivate != 0:
			k.add("private", "private ", true)
		case v.Flags&xtcpnl.IpvlanFlagVepa != 0:
			k.add("vepa", "vepa ", true)
		default:
			k.add(BridgeKindCst, "bridge ", true)
		}
	}
	return k
}
