//go:build goip_write

package goip

import (
	"slices"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is gre_parse_opt (ip/link_gre6.c, iproute2 v7.2.0): the argv
// after `type ip6gre|ip6gretap|ip6erspan`, turned into xtcpnl.GreSetting
// values in argv order, which xtcpnl.EncodeGre6InfoData writes over the
// prefetched state (KG4-T03, docs/netlink/goip-link-kinds.md).
//
//   - The words through tos's aliases are `matches`, tried in the chain's
//     order, so `t` is ttl and `tc` is tclass; the rest are strcmp. The
//     erspan words are every kind's.
//   - tos, tclass and dsfield are a hex u8 into the flow info's class, and
//     flowlabel a hex 20-bit label; each takes inherit, as do dscp (only
//     inherit) and fwmark. encaplimit takes none.
//   - gre's pmtudisc and ignore-df words aren't here, and there is no
//     multicast check.

const (
	kindIP6GreCst    = "ip6gre"
	kindIP6GretapCst = "ip6gretap"
	kindIP6ErspanCst = "ip6erspan"
)

// gre6Kinds are the kinds link_gre6.c's gre_parse_opt serves, whose data
// its prefetch reads.
var gre6Kinds = []string{kindIP6GreCst, kindIP6GretapCst, kindIP6ErspanCst}

const (
	gre6Flowlabel   = "flowlabel"
	gre6Dscp        = "dscp"
	gre6AllowLR     = "allow-localremote"
	gre6NoAllowLR   = "noallow-localremote"
	gre6Encaplimit  = "encaplimit"
	gre6EncapNone   = "none"
	gre6TClassText  = "invalid TClass"
	gre6FlowlblText = "invalid Flowlabel"
)

// gre6ParseChain is link_gre6.c's gre_parse_opt chain, in order. The encap
// words are tunnelEncapWord's.
var gre6ParseChain = slices.Concat(greCommonChain, []greWord{
	{keyword: gre6Flowlabel, exact: true}, {keyword: "fl", canon: gre6Flowlabel, exact: true},
	{keyword: gre6Dscp, exact: true}, {keyword: kindKeyExternal, exact: true}, {keyword: tnlKeyFwmark, exact: true},
	{keyword: gre6AllowLR, exact: true}, {keyword: gre6NoAllowLR, exact: true}, {keyword: gre6Encaplimit, exact: true},
	{keyword: kindErspanCst, exact: true}, {keyword: greErspanVer, exact: true},
	{keyword: greErspanDir, exact: true}, {keyword: greErspanHwid, exact: true},
})

// lookupGre6Word is the chain's first hit for arg, its canonical word, or
// "" for none.
func lookupGre6Word(arg string) string {
	for i := range gre6ParseChain {
		w := &gre6ParseChain[i]
		if w.exact && arg == w.keyword || !w.exact && matchesPrefix(arg, w.keyword) {
			if w.canon != "" {
				return w.canon
			}
			return w.keyword
		}
	}
	return ""
}

// parseGre6Args is link_gre6.c's gre_parse_opt for kind. resolve is
// ll_name_to_index, for dev.
func parseGre6Args(kind string, args []string, resolve linkResolver) ([]xtcpnl.GreSetting, error) {
	p := &greParse{resolve: resolve}
	a := &tunnelArgs{args: args}
	for ; a.more(); a.i++ {
		word := a.word()
		e, isEncap, err := tunnelEncapWord(word, a.next)
		if err != nil {
			return nil, err
		}
		if isEncap {
			p.out = append(p.out, greEncapSetting(e))
			continue
		}
		canon := lookupGre6Word(word)
		if canon == "" {
			return nil, tunnelUsageErr(greUsageLine(kind))
		}
		if err := p.word6(canon, a); err != nil {
			return nil, err
		}
	}
	return p.out, nil
}

// tnlFlag appends an IP6_TNL_F_* bit's set or clear.
func (p *greParse) tnlFlag(bit uint16, on bool) {
	p.out = append(p.out, xtcpnl.GreSetting{Option: xtcpnl.GreOptionTnlFlag, Value: tunnelBool(on), Attr: bit})
}

// word6 is one gre6 chain word, canon its canonical spelling.
func (p *greParse) word6(canon string, a *tunnelArgs) error {
	switch canon {
	case kindKeyExternal:
		p.add(xtcpnl.GreOptionExternal, 0)
		return nil
	case gre6AllowLR, gre6NoAllowLR:
		p.tnlFlag(xtcpnl.Ip6TnlFlagAllowLocalRemote, canon == gre6AllowLR)
		return nil
	case kindKeyRemote, tnlKeyLocal, kindKeyTTL, tosKeywordCst, gre6Flowlabel, gre6Dscp, tnlKeyFwmark, gre6Encaplimit:
	default:
		// The key, seq and csum words and dev and erspan's are gre's.
		return p.word(canon, a)
	}
	v, err := a.next()
	if err != nil {
		return err
	}
	return p.value6(canon, v)
}

// value6 is a gre6 word that takes a value and isn't gre's.
func (p *greParse) value6(canon, v string) error {
	switch canon {
	case kindKeyRemote, tnlKeyLocal:
		addr, err := tunnelAddr(v, unix.AF_INET6)
		if err != nil {
			return err
		}
		o := xtcpnl.GreOptionRemote
		if canon == tnlKeyLocal {
			o = xtcpnl.GreOptionLocal
		}
		p.out = append(p.out, xtcpnl.GreSetting{Option: o, Addr: addr})
	case kindKeyTTL:
		n := uint64(0)
		if v != kindInheritCst {
			var err error
			if n, err = tunnelU8(v, "invalid HLIM\n"); err != nil {
				return err
			}
		}
		p.add(xtcpnl.GreOptionTTL, n)
	case tosKeywordCst:
		if v == kindInheritCst {
			p.tnlFlag(xtcpnl.Ip6TnlFlagUseOrigTclass, true)
			return nil
		}
		n, ok := getU8(v, 16)
		if !ok {
			return linkUsage(invargErr(gre6TClassText, v))
		}
		p.add(xtcpnl.GreOptionTClass, uint64(n))
	case gre6Flowlabel:
		if v == kindInheritCst {
			p.tnlFlag(xtcpnl.Ip6TnlFlagUseOrigFlowlabel, true)
			return nil
		}
		n, ok := getU32(v, 16)
		if !ok || n > xtcpnl.Ip6FlowInfoFlowlabel {
			return linkUsage(invargErr(gre6FlowlblText, v))
		}
		p.add(xtcpnl.GreOptionFlowLabel, uint64(n))
	case gre6Dscp:
		if v != kindInheritCst {
			return linkUsage(invargErr("not inherit", v))
		}
		p.tnlFlag(xtcpnl.Ip6TnlFlagRcvDscpCopy, true)
	case tnlKeyFwmark:
		if v == kindInheritCst {
			p.tnlFlag(xtcpnl.Ip6TnlFlagUseOrigFwmark, true)
			return nil
		}
		n, ok := getU32(v, 0)
		if !ok {
			return linkUsage(invargErr("invalid fwmark\n", v))
		}
		p.add(xtcpnl.GreOptionFwMark, uint64(n))
	default:
		if v == gre6EncapNone {
			p.tnlFlag(xtcpnl.Ip6TnlFlagIgnEncapLimit, true)
			return nil
		}
		n, err := tunnelU8(v, "invalid ELIM")
		if err != nil {
			return err
		}
		p.add(xtcpnl.GreOptionEncapLimit, n)
	}
	return nil
}

// gre6Data is the ip6 gre kinds' parse_opt: the prefetch on a set, the
// words, then the encoder over the prefetched state.
func gre6Data(env *kindEnv, kind string, rest []string) ([]byte, error) {
	raw, err := env.tunnelPrefetch(gre6Kinds, unix.AF_INET6, xtcpnl.IflaGreLocal, xtcpnl.IflaGreRemote)
	if err != nil {
		return nil, err
	}
	settings, err := parseGre6Args(kind, rest, env.resolve)
	if err != nil {
		return nil, err
	}
	return xtcpnl.EncodeGre6InfoData(settings, xtcpnl.ParseGreInfoData(raw))
}
