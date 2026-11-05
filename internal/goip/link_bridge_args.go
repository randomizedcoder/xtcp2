package goip

import (
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is bridge_parse_opt (ip/iplink_bridge.c, iproute2 v7.2.0): the
// argv after `type bridge`, turned into xtcpnl.BridgeSetting values in argv
// order, which xtcpnl.EncodeBridgeInfoData writes (KG1-T01,
// docs/netlink/goip-link-kinds.md).
//
// As for bond, the chain is in parse order, not BridgeOptions' print order,
// since `matches` is a prefix test and the first arm wins: `mcast_q` is
// mcast_query_use_ifaddr, not mcast_querier. Each arm names its
// BridgeOptions row by keyword; the row's Match says matches or strcmp, and
// its Wire how the value is read.
//
// The statuses are ip's: invarg and NEXT_ARG's incomplete_command exit 255
// (ErrLinkUsage), and the refusals parse_opt returns, an unknown word, a
// bad stp_mode and a bad group_address, exit 1.

// bridgeParseKeywords is bridge_parse_opt's if/else chain, in order.
var bridgeParseKeywords = []string{
	"forward_delay", "hello_time", "max_age", xtcpnl.BridgeKeywordAgeingTime, "stp_state", "stp_mode",
	"priority", "vlan_filtering", "vlan_protocol", "group_fwd_mask", "group_address",
	"no_linklocal_learn", "mst_enabled", "fdb_max_learned", "fdb_flush", "vlan_default_pvid",
	"vlan_stats_enabled", "vlan_stats_per_port", "mcast_router", "mcast_snooping", "mcast_vlan_snooping",
	"mcast_query_use_ifaddr", "mcast_querier", "mcast_hash_elasticity", "mcast_hash_max",
	"mcast_last_member_count", "mcast_startup_query_count", "mcast_last_member_interval",
	"mcast_membership_interval", "mcast_querier_interval", "mcast_query_interval",
	"mcast_query_response_interval", "mcast_startup_query_interval", "mcast_stats_enabled",
	"mcast_igmp_version", "mcast_mld_version", "nf_call_iptables", "nf_call_ip6tables",
	"nf_call_arptables", "mdb_offload_fail_notification", "fdb_local_vlan_0",
}

// bridgeLLAddrBufCst is `char llabuf[32]` in the group_address arm.
const bridgeLLAddrBufCst = 32

// bridgeParseChain is bridgeParseKeywords' rows, looked up once.
var bridgeParseChain = bridgeChain(bridgeParseKeywords, xtcpnl.BridgeOptions)

// bridgeChain finds each keyword's row in table. A keyword with no row is a
// programming error, which TestBridgeParseChain catches.
func bridgeChain(keywords []string, table []xtcpnl.BridgeOptionSpec) []*xtcpnl.BridgeOptionSpec {
	out := make([]*xtcpnl.BridgeOptionSpec, 0, len(keywords))
	for _, kw := range keywords {
		for i := range table {
			if table[i].Keyword == kw && !table[i].ReadOnly {
				out = append(out, &table[i])
				break
			}
		}
	}
	return out
}

// bridgeKeywordMatches is the arm's test: strcmp or matches.
func bridgeKeywordMatches(s *xtcpnl.BridgeOptionSpec, arg string) bool {
	if s.Match == xtcpnl.BondMatchExact {
		return arg == s.Keyword
	}
	return matchesPrefix(arg, s.Keyword)
}

// parseBridgeArgs is bridge_parse_opt. errOut takes stp_mode's prefix
// warning, which ip prints and goes on.
func parseBridgeArgs(errOut io.Writer, args []string) ([]xtcpnl.BridgeSetting, error) {
	var out []xtcpnl.BridgeSetting
	for rest := args; len(rest) > 0; {
		word := rest[0]
		rest = rest[1:]
		spec := lookupBridgeKeyword(bridgeParseChain, word)
		if spec == nil {
			if matchesPrefix(word, bondHelpKeywordCst) {
				return nil, fmt.Errorf("%w: bridge help is not implemented by goip", ErrNotImplemented)
			}
			return nil, fmt.Errorf("%w: bridge: unknown command %q?", ErrInvalidArgument, word)
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
		if err := readBridgeValue(errOut, v, &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

func lookupBridgeKeyword(chain []*xtcpnl.BridgeOptionSpec, arg string) *xtcpnl.BridgeOptionSpec {
	for _, s := range chain {
		if bridgeKeywordMatches(s, arg) {
			return s
		}
	}
	return nil
}

// readBridgeValue reads one arm's value by its row's wire.
func readBridgeValue(errOut io.Writer, v string, s *xtcpnl.BridgeSetting) error {
	spec := s.Spec
	invarg := func() error { return linkUsage(invargErr("invalid "+spec.Keyword, v)) }
	switch {
	case spec.Names != nil:
		n, err := parseOneOfWarn(errOut, spec.Keyword, v, spec.Names)
		if err != nil {
			return err
		}
		s.Value = n
		return nil
	case spec.Wire == xtcpnl.BridgeWireBE16:
		p, ok := llProtoA2N(v)
		if !ok {
			return invarg()
		}
		s.Value = uint64(p)
		return nil
	case spec.Wire == xtcpnl.BridgeWireMAC:
		b, ok := llAddrA2N(v, bridgeLLAddrBufCst)
		if !ok {
			return fmt.Errorf("%w: %q is invalid lladdr", ErrInvalidArgument, v)
		}
		s.Bytes = b
		return nil
	}
	n, ok := readBridgeUint(v, spec.Wire)
	if !ok {
		return invarg()
	}
	s.Value = n
	return nil
}

// readBridgeUint is get_u8, get_u16, get_u32 or get_u64, base 0, by the
// row's wire; a boolopt word is a get_u8.
func readBridgeUint(v string, w xtcpnl.BridgeWire) (uint64, bool) {
	switch w {
	case xtcpnl.BridgeWireU8, xtcpnl.BridgeWireBoolOpt, xtcpnl.BridgeWireOnOff:
		n, ok := getU8(v, 0)
		return uint64(n), ok
	case xtcpnl.BridgeWireU16:
		n, ok := getU16(v, 0)
		return uint64(n), ok
	case xtcpnl.BridgeWireU64:
		return getU64(v, 0)
	default:
		n, ok := getU32(v, 0)
		return uint64(n), ok
	}
}

// parseOneOfWarn is parse_one_of (lib/utils.c) with matches_warn: the first
// name v is a prefix of, warning on errOut when v is shorter than it; for
// none, __parse_one_of's message, which parse_opt returns (exit 1).
func parseOneOfWarn(errOut io.Writer, msg, v string, names []string) (uint64, error) {
	var i uint64
	for _, name := range names {
		if !matchesPrefix(v, name) {
			i++
			continue
		}
		if len(v) != len(name) {
			if _, err := fmt.Fprintf(errOut, "WARNING: '%s' matches '%s' by prefix.\n"+
				"Matching by prefix is deprecated in this context, please use the full string.\n", v, name); err != nil {
				return 0, err
			}
		}
		return i, nil
	}
	var b strings.Builder
	for _, name := range names {
		b.WriteString(strconv.Quote(name) + ", ")
	}
	return 0, fmt.Errorf("%w: Error: argument of %q must be one of %snot %q", ErrInvalidArgument, msg, b.String(), v)
}
