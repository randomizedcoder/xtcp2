//go:build goip_write

package goip

import (
	"fmt"
	"net/netip"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is iptunnel_parse_opt (ip/link_iptnl.c, iproute2 v7.2.0): the
// argv after `type ipip|sit`, turned into xtcpnl.IptunSetting values in
// argv order, which xtcpnl.EncodeIptunInfoData writes over the prefetched
// state (KG4-T04, docs/netlink/goip-link-kinds.md).
//
//   - dev and dsfield are `matches`, dev first; every other word is strcmp.
//   - isatap and mode ip6ip are sit's. The 6rd words are parsed on both
//     kinds and sent on sit alone, so a bad prefix exits 1 on ipip too.
//   - remote, local and the 6rd prefixes go through get_addr and
//     get_prefix, whose failures are exit(1).
//   - After the loop, a nonzero ttl with pmtudisc off, seeded or given, is
//     exit(-1). This comes before external.
//   - 6rd-reset segfaults ip 7.2.0, and `6rd-prefix any` sets a prefix ip
//     then doesn't send; goip implements neither.

const (
	kindIpipCst = "ipip"
	kindSitCst  = "sit"
)

// iptnlKinds are the kinds iptunnel_parse_opt serves, whose data its
// prefetch reads.
var iptnlKinds = []string{kindIpipCst, kindSitCst}

const (
	iptnlNopmtudisc = "nopmtudisc"
	iptnlPmtudisc   = "pmtudisc"
	iptnl6rdPrefix  = "6rd-prefix"
	iptnl6rdRelay   = "6rd-relay_prefix"
	iptnl6rdReset   = "6rd-reset"
	iptnlIsatap     = "isatap"
	iptnlMode       = "mode"
)

// iptnlTTLText is the check after iptunnel_parse_opt's loop.
const iptnlTTLText = "ttl != 0 and nopmtudisc are incompatible"

// iptnlUsageLine is iptunnel_print_help's first line: the kind padded to 6.
func iptnlUsageLine(kind string) string {
	return fmt.Sprintf("Usage: ... %-6s\t[ remote ADDR ]", kind)
}

// iptnlModes are the mode words both kinds take, by protocol.
var iptnlModes = map[string]uint64{
	"ipv4/ipv4": xtcpnl.IptunProtoIPIP, kindIpipCst: xtcpnl.IptunProtoIPIP, "ip4ip4": xtcpnl.IptunProtoIPIP,
	"mpls/ipv4": xtcpnl.IptunProtoMPLS, "mplsip": xtcpnl.IptunProtoMPLS,
	"any/ipv4": xtcpnl.IptunProtoAny, addrNameAnyCst: xtcpnl.IptunProtoAny,
}

// iptnlParse is iptunnel_parse_opt's locals.
type iptnlParse struct {
	sit           bool
	resolve       linkResolver
	out           []xtcpnl.IptunSetting
	ttl, pmtudisc uint64
}

// parseIptnlArgs is iptunnel_parse_opt for kind. cur is the prefetched
// state, nil on an add; its ttl and pmtudisc are what the ttl check starts
// from. resolve is ll_name_to_index, for dev.
func parseIptnlArgs(kind string, cur *xtcpnl.IptunInfo, args []string, resolve linkResolver) ([]xtcpnl.IptunSetting, error) {
	p := &iptnlParse{sit: kind == kindSitCst, resolve: resolve, pmtudisc: 1}
	if cur != nil {
		if cur.HasTTL {
			p.ttl = uint64(cur.TTL)
		}
		if cur.HasPMTUDisc {
			p.pmtudisc = uint64(cur.PMTUDisc)
		}
	}
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
		if err := p.word(kind, word, a); err != nil {
			return nil, err
		}
	}
	if p.ttl != 0 && p.pmtudisc == 0 {
		return nil, linkUsage(fmt.Errorf("%w: %s", ErrInvalidArgument, iptnlTTLText))
	}
	return p.out, nil
}

// iptnlEncapSetting is an encap word as an IptunSetting.
func iptnlEncapSetting(e tunnelEncapSet) xtcpnl.IptunSetting {
	switch e.op {
	case tunnelEncapType:
		return xtcpnl.IptunSetting{Option: xtcpnl.IptunOptionEncapType, Value: e.value}
	case tunnelEncapFlag:
		return xtcpnl.IptunSetting{Option: xtcpnl.IptunOptionEncapFlag, Value: e.value, Attr: e.bit}
	case tunnelEncapSport:
		return xtcpnl.IptunSetting{Option: xtcpnl.IptunOptionEncapSport, Value: e.value}
	default:
		return xtcpnl.IptunSetting{Option: xtcpnl.IptunOptionEncapDport, Value: e.value}
	}
}

func (p *iptnlParse) add(o xtcpnl.IptunOption, v uint64) {
	p.out = append(p.out, xtcpnl.IptunSetting{Option: o, Value: v})
}

// iptnlCanon is the chain's canonical word for arg: dev and dsfield by
// prefix, ttl's and tos's aliases folded; "" for an unknown word.
func iptnlCanon(arg string, sit bool) string {
	switch {
	case matchesPrefix(arg, devKeywordCst):
		return devKeywordCst
	case tunnelTTLTOSCanon(arg) != "":
		return tunnelTTLTOSCanon(arg)
	case arg == iptnlIsatap:
		if sit {
			return arg
		}
		return ""
	}
	switch arg {
	case iptnlMode, kindKeyRemote, tnlKeyLocal, iptnlNopmtudisc, iptnlPmtudisc, kindKeyExternal,
		iptnl6rdPrefix, iptnl6rdRelay, iptnl6rdReset, tnlKeyFwmark:
		return arg
	}
	return ""
}

// word is one chain word.
func (p *iptnlParse) word(kind, word string, a *tunnelArgs) error {
	canon := iptnlCanon(word, p.sit)
	switch canon {
	case "":
		return tunnelUsageErr(iptnlUsageLine(kind))
	case iptnlNopmtudisc, iptnlPmtudisc:
		p.pmtudisc = tunnelBool(canon == iptnlPmtudisc)
		p.add(xtcpnl.IptunOptionPMTUDisc, p.pmtudisc)
		return nil
	case iptnlIsatap:
		p.add(xtcpnl.IptunOptionIsatap, 1)
		return nil
	case kindKeyExternal:
		p.add(xtcpnl.IptunOptionExternal, 0)
		return nil
	case iptnl6rdReset:
		return fmt.Errorf("sit 6rd-reset, which crashes ip 7.2.0: %w", ErrNotImplemented)
	}
	v, err := a.next()
	if err != nil {
		return err
	}
	return p.value(canon, v)
}

// value is a word that takes a value.
func (p *iptnlParse) value(canon, v string) error {
	switch canon {
	case iptnlMode:
		proto, ok := iptnlModes[v]
		if p.sit && (v == "ipv6/ipv4" || v == "ip6ip") {
			proto, ok = xtcpnl.IptunProtoIPv6, true
		}
		if !ok {
			return linkUsage(invargErr("Cannot guess tunnel mode.", v))
		}
		p.add(xtcpnl.IptunOptionMode, proto)
	case kindKeyRemote, tnlKeyLocal:
		addr, err := tunnelAddr(v, unix.AF_INET)
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
			if n, err = tunnelU8(v, "invalid TTL\n"); err != nil {
				return err
			}
		}
		p.ttl = n
		p.add(xtcpnl.IptunOptionTTL, n)
	case tosKeywordCst:
		n, err := kindTOS(v)
		if err != nil {
			return err
		}
		p.add(xtcpnl.IptunOptionTOS, n)
	case tnlKeyFwmark:
		n, ok := getU32(v, 0)
		if !ok {
			return linkUsage(invargErr("invalid fwmark\n", v))
		}
		p.add(xtcpnl.IptunOptionFwMark, uint64(n))
	default:
		return p.prefix(canon, v)
	}
	return nil
}

// prefix is 6rd-prefix or 6rd-relay_prefix: get_prefix in the word's
// family.
func (p *iptnlParse) prefix(canon, v string) error {
	family, o := uint8(unix.AF_INET6), xtcpnl.IptunOption6rdPrefix
	if canon == iptnl6rdRelay {
		family, o = unix.AF_INET, xtcpnl.IptunOption6rdRelayPrefix
	}
	if addr, _, slash := strings.Cut(v, "/"); !slash {
		if q, ok := getAddr1(addr, family); ok && q.bitlen == -2 {
			return fmt.Errorf("%s %s, which ip doesn't send: %w", canon, v, ErrNotImplemented)
		}
	}
	pfx, err := routeGetPrefix(v, family)
	if err != nil {
		return err
	}
	a, ok := netip.AddrFromSlice(pfx.data)
	if !ok || pfx.bitlen < 0 || pfx.bitlen > a.BitLen() {
		return fmt.Errorf("%s %s: %w", canon, v, ErrNotImplemented)
	}
	p.out = append(p.out, xtcpnl.IptunSetting{Option: o, Addr: a, Value: uint64(pfx.bitlen)})
	return nil
}

// iptnlData is ipip's and sit's parse_opt: the prefetch on a set, the
// words, then the encoder over the prefetched state.
func iptnlData(env *kindEnv, kind string, rest []string) ([]byte, error) {
	raw, err := env.tunnelPrefetch(iptnlKinds, unix.AF_INET,
		xtcpnl.IflaIptunLocal, xtcpnl.IflaIptunRemote, xtcpnl.IflaIptun6rdRelayPrefix)
	if err != nil {
		return nil, err
	}
	if err := iptnl6rdPrefixLen(raw); err != nil {
		return nil, err
	}
	cur := xtcpnl.ParseIptunInfoData(raw)
	settings, err := parseIptnlArgs(kind, cur, rest, env.resolve)
	if err != nil {
		return nil, err
	}
	return xtcpnl.EncodeIptunInfoData(settings, cur, kind == kindSitCst)
}

// iptnl6rdPrefixLen is get_failed's check of a prefetched 6rd prefix,
// which get_addr_rta reads as AF_INET6: 16 bytes.
func iptnl6rdPrefixLen(raw []byte) error {
	bad := false
	err := xtcpnl.WalkRTAttrsNested(raw, func(atype uint16, val []byte) {
		if atype&^unix.NLA_F_NESTED == xtcpnl.IflaIptun6rdPrefix && len(val) != 16 {
			bad = true
		}
	})
	if err != nil {
		return fmt.Errorf("goip: decode IFLA_INFO_DATA: %w", err)
	}
	if bad {
		return errTunnelGetFailed
	}
	return nil
}
