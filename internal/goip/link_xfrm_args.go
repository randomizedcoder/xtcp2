package goip

import (
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is xfrm_parse_opt (ip/link_xfrm.c, iproute2 v7.2.0) (KG4-T06,
// docs/netlink/goip-link-kinds.md).
//
//   - dev and if_id are `matches`, external strcmp.
//   - Each if_id is sent as it comes; after the loop, external with an
//     if_id or a dev is exit(1), and no if_id is missarg (255).
//   - xfrm has no prefetch.

const kindXfrmCst = "xfrm"

const (
	xfrmIfID  = "if_id"
	xfrmBadID = "if_id value is invalid"
)

// xfrmConflictText is xfrm_parse_opt's external conflict.
const xfrmConflictText = "xfrmi: both 'external' and if_id/link cannot be specified"

// xfrmUsageLine is xfrm_print_help's first line.
const xfrmUsageLine = "Usage: ... xfrm dev [ PHYS_DEV ] [ if_id IF-ID ]"

// parseXfrmArgs is xfrm_parse_opt. resolve is ll_name_to_index, for dev.
func parseXfrmArgs(args []string, resolve linkResolver) ([]xtcpnl.XfrmLinkSetting, error) {
	var out []xtcpnl.XfrmLinkSetting
	var link, ifID uint64
	external := false
	a := &tunnelArgs{args: args}
	for ; a.more(); a.i++ {
		word := a.word()
		switch {
		case matchesPrefix(word, devKeywordCst):
			v, err := a.next()
			if err != nil {
				return nil, err
			}
			if link, err = resolveDev(v, resolve); err != nil {
				return nil, err
			}
			out = append(out, xtcpnl.XfrmLinkSetting{Option: xtcpnl.XfrmLinkOptionLink, Value: link})
		case matchesPrefix(word, xfrmIfID):
			v, err := a.next()
			if err != nil {
				return nil, err
			}
			n, ok := getU32(v, 0)
			if !ok || n == 0 {
				return nil, linkUsage(invargErr(xfrmBadID, v))
			}
			ifID = uint64(n)
			out = append(out, xtcpnl.XfrmLinkSetting{Option: xtcpnl.XfrmLinkOptionIfID, Value: ifID})
		case word == kindKeyExternal:
			external = true
			out = append(out, xtcpnl.XfrmLinkSetting{Option: xtcpnl.XfrmLinkOptionExternal})
		default:
			return nil, tunnelUsageErr(xfrmUsageLine)
		}
	}
	switch {
	case external && (ifID != 0 || link != 0):
		return nil, fmt.Errorf("%w: %s", ErrInvalidArgument, xfrmConflictText)
	case !external && ifID == 0:
		return nil, missargErr("IF_ID")
	}
	return out, nil
}

// xfrmData is xfrm's parse_opt: no prefetch.
func xfrmData(env *kindEnv, rest []string) ([]byte, error) {
	settings, err := parseXfrmArgs(rest, env.resolve)
	if err != nil {
		return nil, err
	}
	return xtcpnl.EncodeXfrmLinkInfoData(settings)
}
