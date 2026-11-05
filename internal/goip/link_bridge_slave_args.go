package goip

import (
	"fmt"
	"slices"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is bridge_slave_parse_opt (ip/iplink_bridge_slave.c, iproute2
// v7.2.0): the argv after `type bridge_slave`, turned into
// xtcpnl.BridgeSetting values in argv order, which
// xtcpnl.EncodeBridgeSlaveData writes (KG1-T02,
// docs/netlink/goip-link-kinds.md).
//
// Its differences from bridge_parse_opt:
//   - state is a get_u8 number; BridgePortStates' names are print-only.
//   - on/off words go through parse_on_off, which compares with strcmp: no
//     prefix, no warning, and a refusal exits 1.
//   - backup_port is a name, resolved by ll_name_to_index (a get unless
//     the command already fetched it); an unknown one is invarg.
//   - nobackup_port, which has no row, sends backup_port 0 and takes no
//     value. It is tried after the chain, which is the same as its place in
//     it: no word before it in the chain matches a prefix of it longer
//     than "n", and "n" is neigh_suppress.
//   - invarg's text is "<keyword> is invalid" for four words and "invalid
//     <keyword>" for the rest.

// bridgeSlaveParseKeywords is bridge_slave_parse_opt's if/else chain, in
// order, less nobackup_port.
var bridgeSlaveParseKeywords = []string{
	"fdb_flush", bridgeSlaveStateCst, bridgeKwPriorityCst, "cost", "hairpin", "guard", "root_block",
	"fastleave", "learning", "flood", "mcast_flood", "bcast_flood", "mcast_to_unicast",
	"proxy_arp", "proxy_arp_wifi", "mcast_router", "mcast_fast_leave",
	"neigh_suppress", "neigh_vlan_suppress", "neigh_forward_grat", "group_fwd_mask",
	"vlan_tunnel", "isolated", "locked", "mab", bridgeSlaveBackupPortCst, "backup_nhid",
}

const (
	bridgeSlaveStateCst        = "state"
	bridgeKwPriorityCst        = "priority"
	bridgeSlaveBackupPortCst   = "backup_port"
	bridgeSlaveNoBackupPortCst = "nobackup_port"
)

// bridgeSlaveIsInvalid are the words whose invarg text is "<keyword> is
// invalid".
var bridgeSlaveIsInvalid = []string{bridgeSlaveStateCst, bridgeKwPriorityCst, "cost", "backup_nhid"}

// bridgeSlaveParseChain is bridgeSlaveParseKeywords' rows, looked up once.
var bridgeSlaveParseChain = bridgeChain(bridgeSlaveParseKeywords, xtcpnl.BridgeSlaveOptions)

// bridgeSlaveBackupPort is backup_port's row, which nobackup_port sets to 0.
var bridgeSlaveBackupPort = lookupBridgeKeyword(bridgeSlaveParseChain, bridgeSlaveBackupPortCst)

// parseBridgeSlaveArgs is bridge_slave_parse_opt. resolve is
// ll_name_to_index, for backup_port.
func parseBridgeSlaveArgs(args []string, resolve linkResolver) ([]xtcpnl.BridgeSetting, error) {
	var out []xtcpnl.BridgeSetting
	for rest := args; len(rest) > 0; {
		word := rest[0]
		rest = rest[1:]
		spec := lookupBridgeKeyword(bridgeSlaveParseChain, word)
		if spec == nil {
			switch {
			case matchesPrefix(word, bridgeSlaveNoBackupPortCst):
				out = append(out, xtcpnl.BridgeSetting{Spec: bridgeSlaveBackupPort})
				continue
			case matchesPrefix(word, bondHelpKeywordCst):
				return nil, fmt.Errorf("%w: bridge_slave help is not implemented by goip", ErrNotImplemented)
			}
			return nil, fmt.Errorf("%w: bridge_slave: unknown option %q?", ErrInvalidArgument, word)
		}
		s := xtcpnl.BridgeSetting{Spec: spec}
		if spec.Wire == xtcpnl.BridgeWireFlag {
			out = append(out, s)
			continue
		}
		if len(rest) == 0 {
			return nil, linkUsage(incompleteErr())
		}
		v := rest[0]
		rest = rest[1:]
		n, err := readBridgeSlaveValue(spec, v, resolve)
		if err != nil {
			return nil, err
		}
		s.Value = n
		out = append(out, s)
	}
	return out, nil
}

// readBridgeSlaveValue reads one arm's value by its row's wire.
func readBridgeSlaveValue(spec *xtcpnl.BridgeOptionSpec, v string, resolve linkResolver) (uint64, error) {
	switch spec.Wire {
	case xtcpnl.BridgeWireOnOff:
		switch v {
		case offKeywordCst:
			return 0, nil
		case onKeywordCst:
			return 1, nil
		default:
			return 0, fmt.Errorf("%w: Error: argument of %q must be one of \"off\", \"on\", not %q",
				ErrInvalidArgument, spec.Keyword, v)
		}
	case xtcpnl.BridgeWireIfindex:
		idx, err := resolve(v)
		if err != nil {
			return 0, err
		}
		if idx <= 0 {
			return 0, linkUsage(invargErr("Device does not exist\n", v))
		}
		return uint64(cint.Bits(idx)), nil
	default:
		n, ok := readBridgeUint(v, spec.Wire)
		if !ok {
			msg := "invalid " + spec.Keyword
			if slices.Contains(bridgeSlaveIsInvalid, spec.Keyword) {
				msg = spec.Keyword + " is invalid"
			}
			return 0, linkUsage(invargErr(msg, v))
		}
		return n, nil
	}
}
