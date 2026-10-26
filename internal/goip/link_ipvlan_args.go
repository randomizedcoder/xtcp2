package goip

import (
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is ipvlan_parse_opt (ip/iplink_ipvlan.c, iproute2 v7.2.0), the
// parser of both kinds it serves, ipvlan and ipvtap: the argv after `type
// ipvlan`, turned into xtcpnl.IpvlanSetting values in argv order, which
// xtcpnl.EncodeIpvlanInfoData writes (KG2-T03,
// docs/netlink/goip-link-kinds.md).
//
// Every keyword is a `matches`; the mode's values are strcmp. A flag word
// is tried only while none has been given, so a second one falls through
// to the unknown option. Its refusals return -1, status 1, but for
// NEXT_ARG's incomplete_command, 255 (ErrLinkUsage). The unknown option's
// text names the kind, lu->id.

const (
	kindIpvlanCst = "ipvlan"
	kindIpvtapCst = "ipvtap"
)

// ipvlanFlagWords are the flag arms, in the chain's order after mode, with
// their IPVLAN_F_*; bridge is neither bit.
var ipvlanFlagWords = []struct {
	keyword string
	flag    uint16
}{
	{keyword: "private", flag: xtcpnl.IpvlanFlagPrivate},
	{keyword: "vepa", flag: xtcpnl.IpvlanFlagVepa},
	{keyword: kindBridgeCst, flag: 0},
}

// parseIpvlanArgs is ipvlan_parse_opt for kind.
func parseIpvlanArgs(kind string, args []string) ([]xtcpnl.IpvlanSetting, error) {
	var out []xtcpnl.IpvlanSetting
	flagGiven := false
	for rest := args; len(rest) > 0; {
		word := rest[0]
		rest = rest[1:]
		if matchesPrefix(word, xfrmModeCst) {
			if len(rest) == 0 {
				return nil, linkUsage(incompleteErr())
			}
			mode, ok := ipvlanMode(rest[0])
			if !ok {
				return nil, fmt.Errorf("%w: Error: argument of \"mode\" must be either \"l2\", \"l3\" or \"l3s\"", ErrInvalidArgument)
			}
			rest = rest[1:]
			out = append(out, xtcpnl.IpvlanSetting{Option: xtcpnl.IpvlanOptionMode, Value: uint64(mode)})
			continue
		}
		if f, ok := lookupIpvlanFlag(word); ok && !flagGiven {
			flagGiven = true
			out = append(out, xtcpnl.IpvlanSetting{Option: xtcpnl.IpvlanOptionFlag, Value: uint64(f)})
			continue
		}
		if matchesPrefix(word, bondHelpKeywordCst) {
			return nil, fmt.Errorf("%w: %s help is not implemented by goip", ErrNotImplemented, kind)
		}
		return nil, fmt.Errorf("%w: %s: unknown option %q?", ErrInvalidArgument, kind, word)
	}
	return out, nil
}

// ipvlanMode is the mode value's strcmp, its index in xtcpnl.IpvlanModes.
func ipvlanMode(v string) (int, bool) {
	for i, m := range xtcpnl.IpvlanModes {
		if v == m {
			return i, true
		}
	}
	return 0, false
}

// lookupIpvlanFlag is the first flag arm word matches.
func lookupIpvlanFlag(word string) (uint16, bool) {
	for _, f := range ipvlanFlagWords {
		if matchesPrefix(word, f.keyword) {
			return f.flag, true
		}
	}
	return 0, false
}
