package render

import (
	"fmt"
	"net/netip"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is amt_print_opt (ip/iplink_amt.c, iproute2 v7.2.0): the tokens
// `ip -d link show` prints after `    amt ` (KG3-T04,
// docs/netlink/goip-link-kinds.md).
//
//   - The mode prints as its name alone. ip indexes past its table for a
//     mode past relay; goip prints `unknown`, as gtp does for a role, and
//     amt_validate refuses such a mode, so no device carries one.
//   - Each address prints whenever sent, 0.0.0.0 included, and dev
//     whenever sent, through ll_index_to_name.

// AmtKindCst is the amt kind, exported for the `-d` gate.
const AmtKindCst = "amt"

// unknownCst is iproute2's name for a value past a table:
// gtp_role_to_string's, amt's mode, ila's, mroute's and format_encap_type's.
const unknownCst = "unknown"

// amtData is amt_print_opt over the decoded IFLA_INFO_DATA.
func amtData(a *xtcpnl.AmtInfo, names NameTab) *KindData {
	k := &KindData{}
	if a == nil {
		return k
	}
	if a.HasMode {
		mode := unknownCst
		if a.Mode < uint32(len(xtcpnl.AmtModeNames)) {
			mode = xtcpnl.AmtModeNames[a.Mode]
		}
		k.add("mode", mode+" ", mode)
	}
	if a.HasGatewayPort {
		k.add("gateway_port", fmt.Sprintf("gateway_port %d ", a.GatewayPort), a.GatewayPort)
	}
	if a.HasRelayPort {
		k.add("relay_port", fmt.Sprintf("relay_port %d ", a.RelayPort), a.RelayPort)
	}
	for _, addr := range [...]struct {
		key string
		a   netip.Addr
	}{{"local", a.Local}, {"remote", a.Remote}, {"discovery", a.Discovery}} {
		if addr.a.IsValid() {
			k.add(addr.key, addr.key+" "+addr.a.String()+" ", addr.a.String())
		}
	}
	if a.HasLink {
		name := kindLinkName(names, a.Link)
		k.add("link", "dev "+name+" ", name)
	}
	if a.HasMaxTunnels {
		k.add("max_tunnels", fmt.Sprintf("max_tunnels %d ", a.MaxTunnels), a.MaxTunnels)
	}
	return k
}
