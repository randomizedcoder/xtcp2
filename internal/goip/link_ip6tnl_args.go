//go:build goip_write

package goip

import (
	"fmt"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is ip6tunnel_parse_opt (ip/link_ip6tnl.c, iproute2 v7.2.0):
// the argv after `type ip6tnl`, turned into xtcpnl.IptunSetting values in
// argv order, which xtcpnl.EncodeIp6tnlInfoData writes over the prefetched
// state (KG4-T05, docs/netlink/goip-link-kinds.md).
//
//   - dev and dsfield are `matches`, dev first; every other word is strcmp,
//     so `dsc` is no word.
//   - tos, tclass and dsfield are a hex u8 with no dsfield names, and
//     flowlabel a hex 20-bit label; each takes inherit, as do dscp (only
//     inherit) and fwmark (which zeroes the mark). encaplimit takes none.
//   - An encaplimit that isn't a u8 or none is never refused: ip sends an
//     uninitialized byte, so goip implements none.
//   - The prefetch seeds all but the encap, which a set resets.

const kindIp6tnlCst = "ip6tnl"

// ip6tnlKinds is the kind ip6tunnel_parse_opt serves, whose data its
// prefetch reads.
var ip6tnlKinds = []string{kindIp6tnlCst}

// ip6tnlModes are ip6tnl's mode words, by protocol.
var ip6tnlModes = map[string]uint64{
	"ipv6/ipv6": xtcpnl.IptunProtoIPv6, "ip6ip6": xtcpnl.IptunProtoIPv6,
	"ip/ipv6": xtcpnl.IptunProtoIPIP, "ipv4/ipv6": xtcpnl.IptunProtoIPIP,
	"ipip6": xtcpnl.IptunProtoIPIP, "ip4ip6": xtcpnl.IptunProtoIPIP,
	"any/ipv6": xtcpnl.IptunProtoAny, addrNameAnyCst: xtcpnl.IptunProtoAny,
}

// ip6tnlCanon is the chain's canonical word for arg: dev and dsfield by
// prefix, hoplimit's and tclass's aliases folded; "" for an unknown word.
func ip6tnlCanon(arg string) string {
	switch {
	case matchesPrefix(arg, devKeywordCst):
		return devKeywordCst
	case tunnelTTLTOSCanon(arg) != "":
		return tunnelTTLTOSCanon(arg)
	case arg == "fl":
		return gre6Flowlabel
	}
	switch arg {
	case iptnlMode, kindKeyRemote, tnlKeyLocal, gre6Encaplimit, gre6Flowlabel, gre6Dscp, tnlKeyFwmark,
		gre6AllowLR, gre6NoAllowLR, kindKeyExternal:
		return arg
	}
	return ""
}

// ip6tnlParse is ip6tunnel_parse_opt's settings so far.
type ip6tnlParse struct {
	resolve linkResolver
	out     []xtcpnl.IptunSetting
}

func (p *ip6tnlParse) add(o xtcpnl.IptunOption, v uint64) {
	p.out = append(p.out, xtcpnl.IptunSetting{Option: o, Value: v})
}

// flag sets an IP6_TNL_F_* bit.
func (p *ip6tnlParse) flag(bit uint16, on bool) {
	p.out = append(p.out, xtcpnl.IptunSetting{Option: xtcpnl.IptunOptionTnlFlag, Value: tunnelBool(on), Attr: bit})
}

// parseIp6tnlArgs is ip6tunnel_parse_opt. resolve is ll_name_to_index,
// for dev.
func parseIp6tnlArgs(args []string, resolve linkResolver) ([]xtcpnl.IptunSetting, error) {
	p := &ip6tnlParse{resolve: resolve}
	a := &tunnelArgs{args: args}
	for ; a.more(); a.i++ {
		word := a.word()
		e, isEncap, err := tunnelEncapWord(word, a.next)
		if err != nil {
			return nil, err
		}
		if isEncap {
			p.out = append(p.out, iptnlEncapSetting(e))
			continue
		}
		if err := p.word(ip6tnlCanon(word), a); err != nil {
			return nil, err
		}
	}
	return p.out, nil
}

// word is one chain word, canon its canonical spelling.
func (p *ip6tnlParse) word(canon string, a *tunnelArgs) error {
	switch canon {
	case "":
		return tunnelUsageErr(iptnlUsageLine(kindIp6tnlCst))
	case kindKeyExternal:
		p.add(xtcpnl.IptunOptionExternal, 0)
		return nil
	case gre6AllowLR, gre6NoAllowLR:
		p.flag(xtcpnl.Ip6TnlFlagAllowLocalRemote, canon == gre6AllowLR)
		return nil
	}
	v, err := a.next()
	if err != nil {
		return err
	}
	if v == kindInheritCst {
		if bit, ok := ip6tnlInherit[canon]; ok {
			p.flag(bit, true)
			return nil
		}
	}
	return p.value(canon, v)
}

// ip6tnlInherit are the words whose inherit is an IP6_TNL_F_* flag.
var ip6tnlInherit = map[string]uint16{
	tosKeywordCst: xtcpnl.Ip6TnlFlagUseOrigTclass,
	gre6Flowlabel: xtcpnl.Ip6TnlFlagUseOrigFlowlabel,
	gre6Dscp:      xtcpnl.Ip6TnlFlagRcvDscpCopy,
	tnlKeyFwmark:  xtcpnl.Ip6TnlFlagUseOrigFwmark,
}

// value is a word that takes a value, inherit already taken.
func (p *ip6tnlParse) value(canon, v string) error {
	switch canon {
	case iptnlMode:
		proto, ok := ip6tnlModes[v]
		if !ok {
			return linkUsage(invargErr("Cannot guess tunnel mode.", v))
		}
		p.add(xtcpnl.IptunOptionMode, proto)
	case kindKeyRemote, tnlKeyLocal:
		addr, err := tunnelAddr(v, unix.AF_INET6)
		if err != nil {
			return err
		}
		o := xtcpnl.IptunOptionRemote
		if canon == tnlKeyLocal {
			o = xtcpnl.IptunOptionLocal
		}
		p.out = append(p.out, xtcpnl.IptunSetting{Option: o, Addr: addr})
	case devKeywordCst:
		idx, err := p.resolve(v)
		if err != nil {
			return err
		}
		if idx <= 0 {
			return nodevExitErr(v)
		}
		p.add(xtcpnl.IptunOptionLink, uint64(cint.Bits(idx)))
	case kindKeyTTL:
		n := uint64(0)
		if v != kindInheritCst {
			var err error
			if n, err = tunnelU8(v, "invalid HLIM\n"); err != nil {
				return err
			}
		}
		p.add(xtcpnl.IptunOptionTTL, n)
	case gre6Encaplimit:
		return p.encaplimit(v)
	default:
		return p.flowValue(canon, v)
	}
	return nil
}

// encaplimit is encaplimit's value: none, or a u8.
func (p *ip6tnlParse) encaplimit(v string) error {
	if v == gre6EncapNone {
		p.flag(xtcpnl.Ip6TnlFlagIgnEncapLimit, true)
		return nil
	}
	n, ok := getU8(v, 0)
	if !ok {
		return fmt.Errorf("encaplimit %s, for which ip sends an uninitialized byte: %w", v, ErrNotImplemented)
	}
	p.add(xtcpnl.IptunOptionEncapLimit, uint64(n))
	return nil
}

// flowValue is tclass's, flowlabel's, dscp's or fwmark's number.
func (p *ip6tnlParse) flowValue(canon, v string) error {
	switch canon {
	case tosKeywordCst:
		n, ok := getU8(v, 16)
		if !ok {
			return linkUsage(invargErr(gre6TClassText, v))
		}
		p.add(xtcpnl.IptunOptionTClass, uint64(n))
	case gre6Flowlabel:
		n, ok := getU32(v, 16)
		if !ok || n > xtcpnl.Ip6FlowInfoFlowlabel {
			return linkUsage(invargErr(gre6FlowlblText, v))
		}
		p.add(xtcpnl.IptunOptionFlowLabel, uint64(n))
	case gre6Dscp:
		return linkUsage(invargErr("not inherit", v))
	default:
		n, ok := getU32(v, 0)
		if !ok {
			return linkUsage(invargErr("invalid fwmark\n", v))
		}
		p.add(xtcpnl.IptunOptionFwMark, uint64(n))
	}
	return nil
}

// ip6tnlData is ip6tnl's parse_opt: the prefetch on a set, the words,
// then the encoder over the prefetched state.
func ip6tnlData(env *kindEnv, rest []string) ([]byte, error) {
	raw, err := env.tunnelPrefetch(ip6tnlKinds, unix.AF_INET6, xtcpnl.IflaIptunLocal, xtcpnl.IflaIptunRemote)
	if err != nil {
		return nil, err
	}
	settings, err := parseIp6tnlArgs(rest, env.resolve)
	if err != nil {
		return nil, err
	}
	return xtcpnl.EncodeIp6tnlInfoData(settings, xtcpnl.ParseIptunInfoData(raw))
}
