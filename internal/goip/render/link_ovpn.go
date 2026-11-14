package render

import "github.com/randomizedcoder/xtcp2/pkg/xtcpnl"

// OvpnKindCst is the ovpn kind, whose print_opt is ovpn_print_opt
// (ip/iplink_ovpn.c), new in iproute2 7.3.0 (92f89d0e).
const OvpnKindCst = "ovpn"

// RenderOvpnMode is whether ovpn's `-d` prints IFLA_OVPN_MODE. 7.3.0's
// ovpn_print_opt prints `mode p2p|mp`; before it ip has no ovpn link_util,
// so the kind prints and nothing after it.
//
// true, the default and the parity target, is 7.3.0's; false reproduces
// 7.2.0 and earlier.
var RenderOvpnMode = true

// ovpnUnknownCst is ovpn_mode_to_str's name for a mode past its table.
const ovpnUnknownCst = "(unknown)"

// ovpnModeNames is ovpn_mode_strings.
var ovpnModeNames = []string{"p2p", "mp"}

// ovpnData is ovpn_print_opt over the decoded IFLA_INFO_DATA: `mode %s `,
// or nothing with no mode of at least a u8. Before 7.3.0 there is no
// print_opt, so nil: no info_data object at all.
func ovpnData(o *xtcpnl.OvpnInfo) *KindData {
	if !RenderOvpnMode {
		return nil
	}
	k := &KindData{}
	if o == nil || !o.HasMode {
		return k
	}
	name := ovpnUnknownCst
	if int(o.Mode) < len(ovpnModeNames) {
		name = ovpnModeNames[o.Mode]
	}
	k.add("mode", "mode "+name+" ", name)
	return k
}
