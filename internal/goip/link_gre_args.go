//go:build goip_write

package goip

import (
	"fmt"
	"net/netip"
	"slices"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is gre_parse_opt (ip/link_gre.c, iproute2 v7.2.0): the argv
// after `type gre|gretap|erspan`, turned into xtcpnl.GreSetting values in
// argv order, which xtcpnl.EncodeGreInfoData writes over the prefetched
// state (KG4-T02, docs/netlink/goip-link-kinds.md).
//
//   - The words through ttl's and tos's aliases are `matches`, tried in the
//     chain's order, so `t` is ttl, `d` is dev and `noi` is noikey; the rest
//     are strcmp. The four erspan words are erspan's alone.
//   - remote and local go through get_addr(AF_INET), whose failure is
//     exit(1); dev resolves as it is read, and a name that doesn't resolve
//     is exit(nodev), 255.
//   - After the loop, a multicast remote, given or prefetched, needs a
//     local (1). This comes before external.

const (
	kindGreCst    = "gre"
	kindGretapCst = "gretap"
	kindErspanCst = "erspan"
)

// greKinds are the kinds gre_parse_opt serves, whose data its prefetch
// reads.
var greKinds = []string{kindGreCst, kindGretapCst, kindErspanCst}

// greWord is one test in gre_parse_opt's chain: the keyword, the word it
// stands for, a strcmp, and erspan's alone.
type greWord struct {
	keyword string
	canon   string
	exact   bool
	erspan  bool
}

// greCommonChain is the head of both gre_parse_opts' chains (link_gre.c
// and link_gre6.c): the key, seq and csum words, the endpoints, dev, ttl's
// and tos's aliases, all `matches`.
var greCommonChain = []greWord{
	{keyword: tnlKeyKey}, {keyword: "nokey"}, {keyword: tnlKeyIkey}, {keyword: "noikey"},
	{keyword: tnlKeyOkey}, {keyword: "nookey"},
	{keyword: greSeq}, {keyword: "noseq"}, {keyword: "iseq"}, {keyword: "noiseq"}, {keyword: "oseq"}, {keyword: "nooseq"},
	{keyword: "csum"}, {keyword: "nocsum"}, {keyword: "icsum"}, {keyword: "noicsum"}, {keyword: "ocsum"}, {keyword: "noocsum"},
	{keyword: kindKeyRemote}, {keyword: tnlKeyLocal}, {keyword: devKeywordCst},
	{keyword: kindKeyTTL}, {keyword: kindKeyHoplimit, canon: kindKeyTTL}, {keyword: tnlKeyHlim, canon: kindKeyTTL},
	{keyword: tosKeywordCst}, {keyword: tnlKeyTClass, canon: tosKeywordCst}, {keyword: kindKeyDsfield, canon: tosKeywordCst},
}

// greParseChain is link_gre.c's gre_parse_opt chain. ip tries
// [no]pmtudisc just before remote; after dsfield is the same, since no
// word between starts with n or p, so no abbreviation hits both.
var greParseChain = slices.Concat(greCommonChain, []greWord{
	{keyword: greNopmtudisc}, {keyword: grePmtudisc},
	{keyword: kindKeyExternal, exact: true}, {keyword: tnlKeyIgnoreDF, exact: true}, {keyword: "noignore-df", exact: true},
	{keyword: tnlKeyFwmark, exact: true},
	{keyword: kindErspanCst, exact: true, erspan: true}, {keyword: greErspanVer, exact: true, erspan: true},
	{keyword: greErspanDir, exact: true, erspan: true}, {keyword: greErspanHwid, exact: true, erspan: true},
})

const (
	greErspanVer  = "erspan_ver"
	greErspanDir  = "erspan_dir"
	greErspanHwid = "erspan_hwid"
	greSeq        = "seq"
	greNopmtudisc = "nopmtudisc"
	grePmtudisc   = "pmtudisc"
)

// greFlagWords are the seq and csum words: the side (i, o or both), the
// bit, and set or clear.
var greFlagWords = map[string]struct {
	in, out bool
	bit     uint16
	value   uint64
}{
	greSeq: {true, true, xtcpnl.GreFlagSeq, 1}, "noseq": {true, true, xtcpnl.GreFlagSeq, 0},
	"iseq": {true, false, xtcpnl.GreFlagSeq, 1}, "noiseq": {true, false, xtcpnl.GreFlagSeq, 0},
	"oseq": {false, true, xtcpnl.GreFlagSeq, 1}, "nooseq": {false, true, xtcpnl.GreFlagSeq, 0},
	"csum": {true, true, xtcpnl.GreFlagCsum, 1}, "nocsum": {true, true, xtcpnl.GreFlagCsum, 0},
	"icsum": {true, false, xtcpnl.GreFlagCsum, 1}, "noicsum": {true, false, xtcpnl.GreFlagCsum, 0},
	"ocsum": {false, true, xtcpnl.GreFlagCsum, 1}, "noocsum": {false, true, xtcpnl.GreFlagCsum, 0},
}

// lookupGreWord is the chain's first hit for arg, its canonical word, or
// "" for none. The strcmp encap words are tunnelEncapWord's.
func lookupGreWord(arg string, erspan bool) string {
	for i := range greParseChain {
		w := &greParseChain[i]
		if w.erspan && !erspan {
			continue
		}
		if w.exact && arg == w.keyword || !w.exact && matchesPrefix(arg, w.keyword) {
			if w.canon != "" {
				return w.canon
			}
			return w.keyword
		}
	}
	return ""
}

// greUsageLine is gre_print_help's first line: the kind padded to 9.
func greUsageLine(kind string) string {
	return fmt.Sprintf("Usage: ... %-9s\t[ remote ADDR ]", kind)
}

// greBroadcastText is gre's refusal of a multicast remote with no local.
const greBroadcastText = "A broadcast tunnel requires a source address."

// greParse is gre_parse_opt's locals.
type greParse struct {
	resolve      linkResolver
	out          []xtcpnl.GreSetting
	saddr, daddr netip.Addr
}

// parseGreArgs is gre_parse_opt for kind. cur is the prefetched state, nil
// on an add; its endpoints are what the multicast check starts from.
// resolve is ll_name_to_index, for dev.
func parseGreArgs(kind string, cur *xtcpnl.GreInfo, args []string, resolve linkResolver) ([]xtcpnl.GreSetting, error) {
	p := &greParse{resolve: resolve}
	if cur != nil {
		p.saddr, p.daddr = cur.Local, cur.Remote
	}
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
		canon := lookupGreWord(word, kind == kindErspanCst)
		if canon == "" {
			return nil, tunnelUsageErr(greUsageLine(kind))
		}
		if err := p.word(canon, a); err != nil {
			return nil, err
		}
	}
	if p.daddr.Is4() && p.daddr.IsMulticast() && (!p.saddr.IsValid() || p.saddr.IsUnspecified()) {
		return nil, fmt.Errorf("%w: %s", ErrInvalidArgument, greBroadcastText)
	}
	return p.out, nil
}

// greEncapSetting is an encap word as a GreSetting.
func greEncapSetting(e tunnelEncapSet) xtcpnl.GreSetting {
	switch e.op {
	case tunnelEncapType:
		return xtcpnl.GreSetting{Option: xtcpnl.GreOptionEncapType, Value: e.value}
	case tunnelEncapFlag:
		return xtcpnl.GreSetting{Option: xtcpnl.GreOptionEncapFlag, Value: e.value, Attr: e.bit}
	case tunnelEncapSport:
		return xtcpnl.GreSetting{Option: xtcpnl.GreOptionEncapSport, Value: e.value}
	default:
		return xtcpnl.GreSetting{Option: xtcpnl.GreOptionEncapDport, Value: e.value}
	}
}

// word is one chain word, canon its canonical spelling.
func (p *greParse) word(canon string, a *tunnelArgs) error {
	if f, ok := greFlagWords[canon]; ok {
		if f.in {
			p.out = append(p.out, xtcpnl.GreSetting{Option: xtcpnl.GreOptionIFlag, Value: f.value, Attr: f.bit})
		}
		if f.out {
			p.out = append(p.out, xtcpnl.GreSetting{Option: xtcpnl.GreOptionOFlag, Value: f.value, Attr: f.bit})
		}
		return nil
	}
	switch canon {
	case "nokey":
		p.add(xtcpnl.GreOptionNoIKey, 0)
		p.add(xtcpnl.GreOptionNoOKey, 0)
		return nil
	case "noikey":
		p.add(xtcpnl.GreOptionNoIKey, 0)
		return nil
	case "nookey":
		p.add(xtcpnl.GreOptionNoOKey, 0)
		return nil
	case greNopmtudisc, grePmtudisc:
		p.add(xtcpnl.GreOptionPMTUDisc, tunnelBool(canon == grePmtudisc))
		return nil
	case kindKeyExternal:
		p.add(xtcpnl.GreOptionExternal, 0)
		return nil
	case tnlKeyIgnoreDF, "noignore-df":
		p.add(xtcpnl.GreOptionIgnoreDF, tunnelBool(canon == tnlKeyIgnoreDF))
		return nil
	}
	v, err := a.next()
	if err != nil {
		return err
	}
	return p.value(canon, v)
}

// value is a word that takes a value.
func (p *greParse) value(canon, v string) error {
	switch canon {
	case tnlKeyKey, tnlKeyIkey, tnlKeyOkey:
		k, err := tnlParseKey(canon, v)
		if err != nil {
			return err
		}
		if canon != tnlKeyOkey {
			p.add(xtcpnl.GreOptionIKey, k)
		}
		if canon != tnlKeyIkey {
			p.add(xtcpnl.GreOptionOKey, k)
		}
		return nil
	case kindKeyRemote, tnlKeyLocal:
		addr, err := tunnelAddr(v, unix.AF_INET)
		if err != nil {
			return err
		}
		if canon == kindKeyRemote {
			p.daddr = addr
			p.out = append(p.out, xtcpnl.GreSetting{Option: xtcpnl.GreOptionRemote, Addr: addr})
		} else {
			p.saddr = addr
			p.out = append(p.out, xtcpnl.GreSetting{Option: xtcpnl.GreOptionLocal, Addr: addr})
		}
		return nil
	case devKeywordCst:
		idx, err := p.resolve(v)
		if err != nil {
			return err
		}
		if idx <= 0 {
			return nodevExitErr(v)
		}
		p.add(xtcpnl.GreOptionLink, uint64(cint.Bits(idx)))
		return nil
	case kindKeyTTL:
		n := uint64(0)
		if v != kindInheritCst {
			var err error
			if n, err = tunnelU8(v, "invalid TTL\n"); err != nil {
				return err
			}
		}
		p.add(xtcpnl.GreOptionTTL, n)
		return nil
	case tosKeywordCst:
		n, err := kindTOS(v)
		if err != nil {
			return err
		}
		p.add(xtcpnl.GreOptionTOS, n)
		return nil
	case tnlKeyFwmark:
		n, ok := getU32(v, 0)
		if !ok {
			return linkUsage(invargErr("invalid fwmark\n", v))
		}
		p.add(xtcpnl.GreOptionFwMark, uint64(n))
		return nil
	default:
		return p.erspanValue(canon, v)
	}
}

// erspanValue is one of erspan's four words.
func (p *greParse) erspanValue(canon, v string) error {
	switch canon {
	case kindErspanCst:
		n, ok := getU32(v, 0)
		if !ok {
			return linkUsage(invargErr("invalid erspan index\n", v))
		}
		if n == 0 || n&^0xfffff != 0 {
			return linkUsage(invargErr("erspan index must be > 0 and <= 20-bit\n", v))
		}
		p.add(xtcpnl.GreOptionErspanIndex, uint64(n))
	case greErspanVer:
		n, err := tunnelU8(v, "invalid erspan version\n")
		if err != nil {
			return err
		}
		if n > 2 {
			return linkUsage(invargErr("erspan version must be 0/1/2\n", v))
		}
		p.add(xtcpnl.GreOptionErspanVer, n)
	case greErspanDir:
		switch {
		case matchesPrefix(v, "ingress"):
			p.add(xtcpnl.GreOptionErspanDir, 0)
		case matchesPrefix(v, "egress"):
			p.add(xtcpnl.GreOptionErspanDir, 1)
		default:
			return linkUsage(invargErr("Invalid erspan direction.", v))
		}
	default:
		n, ok := getU16(v, 0)
		if !ok {
			return linkUsage(invargErr("invalid erspan hwid\n", v))
		}
		p.add(xtcpnl.GreOptionErspanHwid, uint64(n))
	}
	return nil
}

func (p *greParse) add(o xtcpnl.GreOption, v uint64) {
	p.out = append(p.out, xtcpnl.GreSetting{Option: o, Value: v})
}

// greData is the gre kinds' parse_opt: the prefetch on a set, the words,
// then the encoder over the prefetched state.
func greData(env *kindEnv, kind string, rest []string) ([]byte, error) {
	raw, err := env.tunnelPrefetch(greKinds, unix.AF_INET, xtcpnl.IflaGreLocal, xtcpnl.IflaGreRemote)
	if err != nil {
		return nil, err
	}
	cur := xtcpnl.ParseGreInfoData(raw)
	settings, err := parseGreArgs(kind, cur, rest, env.resolve)
	if err != nil {
		return nil, err
	}
	return xtcpnl.EncodeGreInfoData(settings, cur, kind == kindErspanCst)
}
