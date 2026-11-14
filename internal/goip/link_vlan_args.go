//go:build goip_write

package goip

import (
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is vlan_parse_opt (ip/iplink_vlan.c, iproute2 v7.2.0): the
// argv after `type vlan`, turned into xtcpnl.VlanSetting values in argv
// order, which xtcpnl.EncodeVlanInfoData writes (KG2-T01,
// docs/netlink/goip-link-kinds.md).
//
// Every keyword is a `matches`, tried in the chain's order. The statuses
// are ip's: invarg and NEXT_ARG's incomplete_command exit 255
// (ErrLinkUsage); a bad on/off value and an unknown word are returned, 1.

const kindVlanCst = "vlan"

// The qos map keywords, each an IFLA_VLAN_*_QOS nest.
const (
	vlanIngressQosCst = "ingress-qos-map"
	vlanEgressQosCst  = "egress-qos-map"
)

// vlanArm is one test in vlan_parse_opt's chain.
type vlanArm struct {
	keyword string
	option  xtcpnl.VlanOption
	bit     uint32 // a flag's VLAN_FLAG_*
}

// vlanParseChain is vlan_parse_opt's if/else chain, in order; help and the
// unknown word follow it.
var vlanParseChain = vlanChain()

func vlanChain() []vlanArm {
	chain := []vlanArm{
		{keyword: "protocol", option: xtcpnl.VlanOptionProtocol},
		{keyword: "id", option: xtcpnl.VlanOptionID},
	}
	for _, f := range xtcpnl.VlanFlags {
		chain = append(chain, vlanArm{keyword: f.Keyword, option: xtcpnl.VlanOptionFlag, bit: f.Bit})
	}
	return append(chain,
		vlanArm{keyword: vlanIngressQosCst, option: xtcpnl.VlanOptionIngressQos},
		vlanArm{keyword: vlanEgressQosCst, option: xtcpnl.VlanOptionEgressQos})
}

func lookupVlanArm(arg string) *vlanArm {
	for i := range vlanParseChain {
		if matchesPrefix(arg, vlanParseChain[i].keyword) {
			return &vlanParseChain[i]
		}
	}
	return nil
}

// parseVlanArgs is vlan_parse_opt.
func parseVlanArgs(args []string) ([]xtcpnl.VlanSetting, error) {
	var out []xtcpnl.VlanSetting
	for rest := args; len(rest) > 0; {
		word := rest[0]
		rest = rest[1:]
		arm := lookupVlanArm(word)
		if arm == nil {
			if matchesPrefix(word, bondHelpKeywordCst) {
				return nil, fmt.Errorf("%w: vlan help is not implemented by goip", ErrNotImplemented)
			}
			return nil, fmt.Errorf("%w: vlan: unknown command %q?", ErrInvalidArgument, word)
		}
		if len(rest) == 0 {
			return nil, linkUsage(incompleteErr())
		}
		s := xtcpnl.VlanSetting{Option: arm.option, Bit: arm.bit}
		if arm.option == xtcpnl.VlanOptionIngressQos || arm.option == xtcpnl.VlanOptionEgressQos {
			// The map leaves rest at the word that stopped it.
			m, left, err := parseQosMapping(rest, arm.keyword)
			if err != nil {
				return nil, err
			}
			s.Map, rest = m, left
			out = append(out, s)
			continue
		}
		v := rest[0]
		rest = rest[1:]
		if err := readVlanValue(arm, v, &s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// readVlanValue reads the value of a protocol, id or flag arm.
func readVlanValue(arm *vlanArm, v string, s *xtcpnl.VlanSetting) error {
	switch arm.option {
	case xtcpnl.VlanOptionProtocol:
		p, ok := llProtoA2N(v)
		if !ok {
			return linkUsage(invargErr("protocol is invalid", v))
		}
		s.Value = uint64(p)
	case xtcpnl.VlanOptionID:
		id, ok := getU16(v, 0)
		if !ok {
			return linkUsage(invargErr("id is invalid", v))
		}
		s.Value = uint64(id)
	default: // a flag
		switch v {
		case onKeywordCst:
			s.Value = 1
		case offKeywordCst:
		default:
			return fmt.Errorf("%w: Error: argument of %q must be \"on\" or \"off\", not %q",
				ErrInvalidArgument, arm.keyword, v)
		}
	}
	return nil
}

// parseQosMapping is parse_mapping (lib/utils.c) with vlan's callback: the
// FROM:TO words at the front of args, each half a get_u32 base 0, until a
// word with no colon, which it leaves at the front of what it returns. A
// bad half is vlan_parse_qos_map's invarg, which names the word as
// parse_mapping left it, cut at its colon.
func parseQosMapping(args []string, keyword string) ([]xtcpnl.VlanQosMapping, []string, error) {
	var m []xtcpnl.VlanQosMapping
	for len(args) > 0 {
		from, to, ok := strings.Cut(args[0], ":")
		if !ok {
			break
		}
		f, okF := getU32(from, 0)
		t, okT := getU32(to, 0)
		if !okF || !okT {
			return nil, args, linkUsage(invargErr("invalid "+keyword, from))
		}
		m = append(m, xtcpnl.VlanQosMapping{From: f, To: t})
		args = args[1:]
	}
	return m, args, nil
}
