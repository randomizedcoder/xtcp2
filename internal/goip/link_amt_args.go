package goip

import (
	"fmt"
	"net/netip"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is amt_parse_opt (ip/iplink_amt.c, iproute2 v7.2.0): the argv
// after `type amt`, turned into xtcpnl.AmtSetting values in argv order,
// which xtcpnl.EncodeAmtInfoData writes as they come (KG3-T03,
// docs/netlink/goip-link-kinds.md).
//
//   - Every keyword is a strcmp, help included.
//   - Only local and discovery are check_duparg'd; a repeated mode or port
//     is sent twice.
//   - A bad mode prints the usage and returns -1 (1).
//   - local and discovery go through get_addr with the discovery's family
//     (AF_UNSPEC until one is given), whose failure is exit(1), and an
//     address that isn't is_addrtype_inet (any, all) is dropped.
//   - dev resolves as it is read, and a name that doesn't resolve is
//     exit(nodev), 255.

const kindAmtCst = "amt"

// amtAddrKeyword is amt's discovery keyword; local is addrLocalKeywordCst.
// mode is xfrmModeCst.
const (
	amtAddrKeyword    = "discovery"
	amtGatewayPortCst = "gateway_port"
)

// amtModes are mode's strcmp values.
var amtModes = map[string]uint64{"gateway": 0, "relay": 1}

// amtParse is amt_parse_opt's locals.
type amtParse struct {
	resolve linkResolver
	out     []xtcpnl.AmtSetting
	attrs   kindAttrs
	dfamily uint8
}

// parseAmtArgs is amt_parse_opt. resolve is ll_name_to_index, for dev.
func parseAmtArgs(args []string, resolve linkResolver) ([]xtcpnl.AmtSetting, error) {
	p := &amtParse{resolve: resolve}
	for rest := args; len(rest) > 0; rest = rest[2:] {
		word := rest[0]
		switch word {
		case xfrmModeCst, "relay_port", amtGatewayPortCst, "max_tunnels", devKeywordCst, addrLocalKeywordCst, amtAddrKeyword:
		case bondHelpKeywordCst:
			return nil, fmt.Errorf("%w: amt help is not implemented by goip", ErrNotImplemented)
		default:
			return nil, kindUnknownErr(kindAmtCst, word)
		}
		if len(rest) < 2 {
			return nil, linkUsage(incompleteErr())
		}
		if err := p.word(word, rest[1]); err != nil {
			return nil, err
		}
	}
	return p.out, nil
}

// word is one value word.
func (p *amtParse) word(word, v string) error {
	s := xtcpnl.AmtSetting{}
	switch word {
	case xfrmModeCst:
		m, ok := amtModes[v]
		if !ok {
			return fmt.Errorf("%w: Usage: ... amt", ErrInvalidArgument)
		}
		s = xtcpnl.AmtSetting{Option: xtcpnl.AmtOptionMode, Value: m}
	case "relay_port", amtGatewayPortCst:
		n, ok := getU16(v, 0)
		if !ok {
			return linkUsage(invargErr(word, v))
		}
		s = xtcpnl.AmtSetting{Option: xtcpnl.AmtOptionRelayPort, Value: uint64(n)}
		if word == amtGatewayPortCst {
			s.Option = xtcpnl.AmtOptionGatewayPort
		}
	case "max_tunnels":
		n, ok := getU32(v, 0)
		if !ok {
			return linkUsage(invargErr(word, v))
		}
		s = xtcpnl.AmtSetting{Option: xtcpnl.AmtOptionMaxTunnels, Value: uint64(n)}
	case devKeywordCst:
		idx, err := p.resolve(v)
		if err != nil {
			return err
		}
		if idx <= 0 {
			// As vxlan's dev: ll_name_to_index's 0, or an "if<n>" past
			// INT_MAX, which ip sends.
			return nodevExitErr(v)
		}
		s = xtcpnl.AmtSetting{Option: xtcpnl.AmtOptionLink, Value: uint64(idx)}
	default: // local, discovery
		return p.addr(word, v)
	}
	p.out = append(p.out, s)
	return nil
}

// addr is local or discovery: check_duparg, get_addr with the
// discovery's family, and the attribute only for an inet address.
func (p *amtParse) addr(word, v string) error {
	s := xtcpnl.AmtSetting{Option: xtcpnl.AmtOptionLocal}
	attr := uint16(xtcpnl.IflaAmtLocalIP)
	if word == amtAddrKeyword {
		s.Option, attr = xtcpnl.AmtOptionDiscovery, xtcpnl.IflaAmtDiscoveryIP
	}
	if err := p.attrs.dup(attr, word, v); err != nil {
		return err
	}
	pr, err := routeGetAddr(v, p.dfamily)
	if err != nil {
		return err
	}
	if s.Option == xtcpnl.AmtOptionDiscovery {
		p.dfamily = pr.family
	}
	if pr.family != unix.AF_INET && pr.family != unix.AF_INET6 {
		return nil
	}
	if len(pr.data) == 0 {
		// any or all after a discovery: ip sends the attribute empty,
		// which an xtcpnl.AmtSetting can't hold.
		return fmt.Errorf("amt: an empty address %q: %w", v, ErrNotImplemented)
	}
	s.Addr, _ = netip.AddrFromSlice(pr.data)
	p.out = append(p.out, s)
	return nil
}
