package render

import (
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is gtp_print_opt (ip/iplink_gtp.c, iproute2 v7.2.0): the tokens
// `ip -d link show` prints after `    gtp ` (KG3-T04,
// docs/netlink/goip-link-kinds.md). A role past sgsn is
// gtp_role_to_string's `unknown`.

// GtpKindCst is the gtp kind, exported for the `-d` gate.
const GtpKindCst = "gtp"

// gtpData is gtp_print_opt over the decoded IFLA_INFO_DATA.
func gtpData(g *xtcpnl.GtpInfo) *KindData {
	k := &KindData{}
	if g == nil {
		return k
	}
	if g.HasRole {
		role := unknownCst
		if g.Role < uint32(len(xtcpnl.GtpRoleNames)) {
			role = xtcpnl.GtpRoleNames[g.Role]
		}
		k.add("role", "role "+role+" ", role)
	}
	if g.HasPDPHashSize {
		k.add("hsize", fmt.Sprintf("hsize %d ", g.PDPHashSize), g.PDPHashSize)
	}
	if g.HasRestartCount {
		k.add("restart_count", fmt.Sprintf("restart_count %d ", g.RestartCount), g.RestartCount)
	}
	return k
}
