//go:build goip_write

package goip

import (
	"fmt"
	"math"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is vrf_parse_opt (ip/iplink_vrf.c) and batadv_parse_opt
// (ip/iplink_batadv.c), iproute2 v7.2.0 (KG5-T04,
// docs/netlink/goip-link-kinds.md).
//
//   - vrf's table is `matches`, so `t` is table. Its value goes through
//     rtnl_rttable_a2n, a name from rt_tables or get_u32 in base 0; a
//     failure is invarg, 255. Each table word sends IFLA_VRF_TABLE.
//   - batadv's ra is `matches`, so `r` is ra, and sends its value with its
//     NUL, unchecked. Each ra word sends IFLA_BATADV_ALGO_NAME.
//   - help, also `matches`, prints the usage, and an unknown word its
//     line and the usage; both return -1, 1.

// The kinds this file parses.
const (
	kindVrfCst    = "vrf"
	kindBatadvCst = "batadv"
)

// The usage lines explain prints first.
const (
	vrfUsageLine    = "Usage: ... vrf table TABLEID"
	batadvUsageLine = "Usage: ... batadv [ ra ROUTING_ALG ]"
)

// parseVrfArgs is vrf_parse_opt.
func parseVrfArgs(args []string) ([]xtcpnl.VrfSetting, error) {
	var out []xtcpnl.VrfSetting
	a := &tunnelArgs{args: args}
	for ; a.more(); a.i++ {
		word := a.word()
		switch {
		case matchesPrefix(word, mrouteTableKeyCst):
			v, err := a.next()
			if err != nil {
				return nil, err
			}
			id, ok := rtnlNameA2N(routeTableNames, v, math.MaxUint32)
			if !ok {
				return nil, linkUsage(invargErr("invalid table ID\n", v))
			}
			out = append(out, xtcpnl.VrfSetting{Option: xtcpnl.VrfOptionTable, Value: uint64(id)})
		case matchesPrefix(word, bondHelpKeywordCst):
			return nil, tunnelUsageErr(vrfUsageLine)
		default:
			return nil, fmt.Errorf("%w: vrf: unknown option %q?", ErrInvalidArgument, word)
		}
	}
	return out, nil
}

// parseBatadvArgs is batadv_parse_opt.
func parseBatadvArgs(args []string) ([]xtcpnl.BatadvSetting, error) {
	var out []xtcpnl.BatadvSetting
	a := &tunnelArgs{args: args}
	for ; a.more(); a.i++ {
		word := a.word()
		switch {
		case matchesPrefix(word, "ra"):
			v, err := a.next()
			if err != nil {
				return nil, err
			}
			out = append(out, xtcpnl.BatadvSetting{Option: xtcpnl.BatadvOptionRoutingAlgo, Name: v})
		case matchesPrefix(word, bondHelpKeywordCst):
			return nil, tunnelUsageErr(batadvUsageLine)
		default:
			return nil, kindUnknownErr(kindBatadvCst, word)
		}
	}
	return out, nil
}
