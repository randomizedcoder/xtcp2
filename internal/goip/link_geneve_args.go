//go:build goip_write

package goip

import (
	"fmt"
	"net/netip"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is geneve_parse_opt (ip/iplink_geneve.c, iproute2 v7.2.0): the
// argv after `type geneve`, turned into xtcpnl.GeneveSetting values in argv
// order, which xtcpnl.EncodeGeneveInfoData writes (KG3-T02,
// docs/netlink/goip-link-kinds.md).
//
//   - Every keyword is a `matches`, tried in the chain's order, except
//     local and innerprotoinherit, which are strcmp. So `l` is label, `d`
//     is dsfield (before df) and `n` is noexternal; 7.2.0's local6 is no
//     keyword at all.
//   - Each value word is NEXT_ARG, then check_duparg on its attribute,
//     whose duparg2 (255) names the keyword ip passes; a flag and its `no`
//     form share an attribute and name the word itself.
//   - remote and local go through get_addr with AF_UNSPEC, whose failure is
//     exit(1), then is_addrtype_inet_not_multi.
//   - After the loop come three returned checks (1). A set needs no remote.

const kindGeneveCst = "geneve"

// geneveWord is one test in geneve_parse_opt's chain. option is what the
// word sets, attr its check_duparg bit (and a GeneveOptionCsum's
// attribute), key duparg2's name for a value word, and value a flag's 0
// or 1.
type geneveWord struct {
	keyword string
	option  xtcpnl.GeneveOption
	attr    uint16
	key     string
	value   uint64
	flag    bool
	exact   bool
}

// geneveParseChain is geneve_parse_opt's if/else chain, in order; help and
// the unknown word follow it.
var geneveParseChain = []geneveWord{
	{keyword: kindKeyID, option: xtcpnl.GeneveOptionID, attr: unix.IFLA_GENEVE_ID, key: kindKeyID},
	{keyword: "vni", option: xtcpnl.GeneveOptionID, attr: unix.IFLA_GENEVE_ID, key: kindKeyID},
	{keyword: kindKeyRemote, option: xtcpnl.GeneveOptionRemote, attr: unix.IFLA_GENEVE_REMOTE, key: kindKeyRemote},
	{keyword: addrLocalKeywordCst, option: xtcpnl.GeneveOptionLocal, attr: xtcpnl.IflaGeneveLocal, key: addrLocalKeywordCst, exact: true},
	{keyword: kindKeyTTL, option: xtcpnl.GeneveOptionTTL, attr: unix.IFLA_GENEVE_TTL, key: kindKeyTTL},
	{keyword: kindKeyHoplimit, option: xtcpnl.GeneveOptionTTL, attr: unix.IFLA_GENEVE_TTL, key: kindKeyTTL},
	{keyword: tosKeywordCst, option: xtcpnl.GeneveOptionTOS, attr: unix.IFLA_GENEVE_TOS, key: tosKeywordCst},
	{keyword: kindKeyDsfield, option: xtcpnl.GeneveOptionTOS, attr: unix.IFLA_GENEVE_TOS, key: tosKeywordCst},
	{keyword: kindKeyDF, option: xtcpnl.GeneveOptionDF, attr: unix.IFLA_GENEVE_DF, key: kindKeyDF},
	{keyword: kindKeyLabel, option: xtcpnl.GeneveOptionLabel, attr: unix.IFLA_GENEVE_LABEL, key: kindKeyFlowlabel},
	{keyword: kindKeyFlowlabel, option: xtcpnl.GeneveOptionLabel, attr: unix.IFLA_GENEVE_LABEL, key: kindKeyFlowlabel},
	{keyword: kindKeyDstport, option: xtcpnl.GeneveOptionDstPort, attr: unix.IFLA_GENEVE_PORT, key: kindKeyDstport},
	{keyword: kindKeyExternal, option: xtcpnl.GeneveOptionExternal, attr: unix.IFLA_GENEVE_COLLECT_METADATA, value: 1, flag: true},
	{keyword: kindKeyNoexternal, option: xtcpnl.GeneveOptionExternal, attr: unix.IFLA_GENEVE_COLLECT_METADATA, flag: true},
	geneveCsum("udpcsum", unix.IFLA_GENEVE_UDP_CSUM, 1),
	geneveCsum("noudpcsum", unix.IFLA_GENEVE_UDP_CSUM, 0),
	geneveCsum("udp6zerocsumtx", unix.IFLA_GENEVE_UDP_ZERO_CSUM6_TX, 1),
	geneveCsum("noudp6zerocsumtx", unix.IFLA_GENEVE_UDP_ZERO_CSUM6_TX, 0),
	geneveCsum("udp6zerocsumrx", unix.IFLA_GENEVE_UDP_ZERO_CSUM6_RX, 1),
	geneveCsum("noudp6zerocsumrx", unix.IFLA_GENEVE_UDP_ZERO_CSUM6_RX, 0),
	{
		keyword: "innerprotoinherit", option: xtcpnl.GeneveOptionInnerProtoInherit,
		attr: unix.IFLA_GENEVE_INNER_PROTO_INHERIT, flag: true, exact: true,
	},
}

// geneveCsum is a chain row for one of the checksum words.
func geneveCsum(keyword string, attr uint16, value uint64) geneveWord {
	return geneveWord{keyword: keyword, option: xtcpnl.GeneveOptionCsum, attr: attr, value: value, flag: true}
}

func lookupGeneveWord(arg string) *geneveWord {
	for i := range geneveParseChain {
		w := &geneveParseChain[i]
		if w.exact && arg == w.keyword || !w.exact && matchesPrefix(arg, w.keyword) {
			return w
		}
	}
	return nil
}

// geneveParse is geneve_parse_opt's locals.
type geneveParse struct {
	out      []xtcpnl.GeneveSetting
	attrs    kindAttrs
	remote   bool
	metadata bool
}

// parseGeneveArgs is geneve_parse_opt; set is set_op, an RTM_NEWLINK
// without NLM_F_CREATE.
func parseGeneveArgs(set bool, args []string) ([]xtcpnl.GeneveSetting, error) {
	p := &geneveParse{}
	for rest := args; len(rest) > 0; {
		w := lookupGeneveWord(rest[0])
		if w == nil {
			if matchesPrefix(rest[0], bondHelpKeywordCst) {
				return nil, fmt.Errorf("%w: geneve help is not implemented by goip", ErrNotImplemented)
			}
			return nil, kindUnknownErr(kindGeneveCst, rest[0])
		}
		var err error
		if rest, err = p.word(w, rest); err != nil {
			return nil, err
		}
	}
	if err := p.finish(set); err != nil {
		return nil, err
	}
	return p.out, nil
}

// word is one pass of the loop: rest starts at the keyword, and what is
// returned starts after its value.
func (p *geneveParse) word(w *geneveWord, rest []string) ([]string, error) {
	word := rest[0]
	rest = rest[1:]
	if w.flag {
		if err := p.attrs.dup(w.attr, word, word); err != nil {
			return nil, err
		}
		if w.option == xtcpnl.GeneveOptionExternal {
			p.metadata = w.value == 1
		}
		p.out = append(p.out, xtcpnl.GeneveSetting{Option: w.option, Value: w.value, Attr: w.attr})
		return rest, nil
	}
	if len(rest) == 0 {
		return nil, linkUsage(incompleteErr())
	}
	v := rest[0]
	if err := p.attrs.dup(w.attr, w.key, v); err != nil {
		return nil, err
	}
	s, err := p.value(w.option, v)
	if err != nil {
		return nil, err
	}
	p.out = append(p.out, s)
	return rest[1:], nil
}

// value reads a value word's value.
func (p *geneveParse) value(option xtcpnl.GeneveOption, v string) (xtcpnl.GeneveSetting, error) {
	s := xtcpnl.GeneveSetting{Option: option}
	var err error
	switch option {
	case xtcpnl.GeneveOptionID:
		s.Value, err = kindID(v)
	case xtcpnl.GeneveOptionRemote, xtcpnl.GeneveOptionLocal:
		return p.addr(option, v)
	case xtcpnl.GeneveOptionTTL:
		var inherit bool
		s.Value, inherit, err = kindTTL(v)
		if inherit {
			s.Option = xtcpnl.GeneveOptionTTLInherit
		}
	case xtcpnl.GeneveOptionTOS:
		s.Value, err = kindTOS(v)
	case xtcpnl.GeneveOptionDF:
		s.Value, err = kindDF(v)
	case xtcpnl.GeneveOptionLabel:
		s.Value, err = kindLabel(v)
	default: // dstport
		n, ok := getU16(v, 0)
		if !ok {
			err = linkUsage(invargErr(kindKeyDstport, v))
		}
		s.Value = uint64(n)
	}
	return s, err
}

// addr is remote or local: get_addr with AF_UNSPEC, then
// is_addrtype_inet_not_multi. AF_UNSPEC's any and all have no family, so
// they fail the type check, and an address that passes has its bytes.
func (p *geneveParse) addr(option xtcpnl.GeneveOption, v string) (xtcpnl.GeneveSetting, error) {
	pr, err := routeGetAddr(v, unix.AF_UNSPEC)
	if err != nil {
		return xtcpnl.GeneveSetting{}, err
	}
	if a := newKindAddr(pr); !a.inet || a.multi {
		msg := "invalid remote address"
		if option == xtcpnl.GeneveOptionLocal {
			msg = "invalid local address"
		}
		return xtcpnl.GeneveSetting{}, linkUsage(invargErr(msg, v))
	}
	if option == xtcpnl.GeneveOptionRemote {
		p.remote = true
	}
	addr, _ := netip.AddrFromSlice(pr.data)
	return xtcpnl.GeneveSetting{Option: option, Addr: addr}, nil
}

// finish is geneve_parse_opt's checks after the loop, in its order.
func (p *geneveParse) finish(set bool) error {
	hasID := p.attrs.has(unix.IFLA_GENEVE_ID)
	switch {
	case p.metadata && hasID:
		return kindOptErr(kindGeneveCst, "both 'external' and vni cannot be specified")
	case p.metadata:
		return nil
	case !hasID:
		return kindOptErr(kindGeneveCst, "missing virtual network identifier")
	case !set && !p.remote:
		return kindOptErr(kindGeneveCst, "remote link partner not specified")
	}
	return nil
}
