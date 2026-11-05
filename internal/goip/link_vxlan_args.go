package goip

import (
	"fmt"
	"io"
	"net/netip"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is vxlan_parse_opt (ip/iplink_vxlan.c, iproute2 v7.2.0): the
// argv after `type vxlan`, turned into xtcpnl.VxlanSetting values in argv
// order, which xtcpnl.EncodeVxlanInfoData writes (KG3-T01,
// docs/netlink/goip-link-kinds.md).
//
//   - Every keyword is a `matches`, tried in the chain's order, except the
//     seven strcmp words. So `d` is dev, `l` is local, `p` is port (the
//     source range) and `n` is nolearning.
//   - check_duparg keys each word on its attribute: group and remote share
//     IFLA_VXLAN_GROUP, and a flag and its `no` form share theirs. A second
//     one is duparg2 (255), whose key is the keyword ip names, the flag
//     itself, or reserved_bits' value.
//   - group and remote refuse each other before NEXT_ARG.
//   - The addresses go through get_addr with the other address's family,
//     whose failure is exit(1).
//   - dev resolves as it is read, and a name that doesn't resolve is
//     exit(nodev), 255.
//   - After the loop come four returned checks (1), then the dstport
//     warning, which ip prints and goes on from.

const (
	kindVxlanCst    = "vxlan"
	vxlanKeySrcport = "srcport"
)

// vxlanWord is one test in vxlan_parse_opt's chain. option is what the
// word sets, attr its check_duparg bit (and a VxlanOptionBool's
// attribute), key duparg2's name for a value word, and value a flag's
// 0 or 1.
type vxlanWord struct {
	keyword string
	option  xtcpnl.VxlanOption
	attr    uint16
	key     string
	value   uint64
	flag    bool
	exact   bool
}

// vxlanParseChain is vxlan_parse_opt's if/else chain, in order; help and
// the unknown word follow it.
var vxlanParseChain = []vxlanWord{
	{keyword: kindKeyID, option: xtcpnl.VxlanOptionID, attr: unix.IFLA_VXLAN_ID, key: kindKeyID},
	{keyword: "vni", option: xtcpnl.VxlanOptionID, attr: unix.IFLA_VXLAN_ID, key: kindKeyID},
	{keyword: groupKeywordCst, option: xtcpnl.VxlanOptionGroup, attr: unix.IFLA_VXLAN_GROUP, key: groupKeywordCst},
	{keyword: kindKeyRemote, option: xtcpnl.VxlanOptionRemote, attr: unix.IFLA_VXLAN_GROUP, key: kindKeyRemote},
	{keyword: addrLocalKeywordCst, option: xtcpnl.VxlanOptionLocal, attr: unix.IFLA_VXLAN_LOCAL, key: addrLocalKeywordCst},
	{keyword: devKeywordCst, option: xtcpnl.VxlanOptionLink, attr: unix.IFLA_VXLAN_LINK, key: devKeywordCst},
	{keyword: kindKeyTTL, option: xtcpnl.VxlanOptionTTL, attr: unix.IFLA_VXLAN_TTL, key: kindKeyTTL},
	{keyword: kindKeyHoplimit, option: xtcpnl.VxlanOptionTTL, attr: unix.IFLA_VXLAN_TTL, key: kindKeyTTL},
	{keyword: tosKeywordCst, option: xtcpnl.VxlanOptionTOS, attr: unix.IFLA_VXLAN_TOS, key: tosKeywordCst},
	{keyword: kindKeyDsfield, option: xtcpnl.VxlanOptionTOS, attr: unix.IFLA_VXLAN_TOS, key: tosKeywordCst},
	{keyword: kindKeyDF, option: xtcpnl.VxlanOptionDF, attr: unix.IFLA_VXLAN_DF, key: kindKeyDF},
	{keyword: kindKeyLabel, option: xtcpnl.VxlanOptionLabel, attr: unix.IFLA_VXLAN_LABEL, key: kindKeyFlowlabel},
	{keyword: kindKeyFlowlabel, option: xtcpnl.VxlanOptionLabel, attr: unix.IFLA_VXLAN_LABEL, key: kindKeyFlowlabel},
	{keyword: xtcpnl.VxlanKeywordAgeing, option: xtcpnl.VxlanOptionAging, attr: xtcpnl.IflaVxlanAgeing, key: xtcpnl.VxlanKeywordAgeing},
	{keyword: "maxaddress", option: xtcpnl.VxlanOptionLimit, attr: unix.IFLA_VXLAN_LIMIT, key: "maxaddress"},
	{keyword: "port", option: xtcpnl.VxlanOptionSrcPort, attr: unix.IFLA_VXLAN_PORT_RANGE, key: vxlanKeySrcport},
	{keyword: vxlanKeySrcport, option: xtcpnl.VxlanOptionSrcPort, attr: unix.IFLA_VXLAN_PORT_RANGE, key: vxlanKeySrcport},
	{keyword: kindKeyDstport, option: xtcpnl.VxlanOptionDstPort, attr: unix.IFLA_VXLAN_PORT, key: kindKeyDstport},
	{keyword: "nolearning", option: xtcpnl.VxlanOptionLearning, attr: unix.IFLA_VXLAN_LEARNING, flag: true},
	{keyword: "learning", option: xtcpnl.VxlanOptionLearning, attr: unix.IFLA_VXLAN_LEARNING, value: 1, flag: true},
	vxlanBool("noproxy", unix.IFLA_VXLAN_PROXY, 0, false),
	vxlanBool("proxy", unix.IFLA_VXLAN_PROXY, 1, false),
	vxlanBool("norsc", unix.IFLA_VXLAN_RSC, 0, false),
	vxlanBool("rsc", unix.IFLA_VXLAN_RSC, 1, false),
	vxlanBool("nol2miss", unix.IFLA_VXLAN_L2MISS, 0, false),
	vxlanBool("l2miss", unix.IFLA_VXLAN_L2MISS, 1, false),
	vxlanBool("nol3miss", unix.IFLA_VXLAN_L3MISS, 0, false),
	vxlanBool("l3miss", unix.IFLA_VXLAN_L3MISS, 1, false),
	vxlanBool("udpcsum", unix.IFLA_VXLAN_UDP_CSUM, 1, false),
	vxlanBool("noudpcsum", unix.IFLA_VXLAN_UDP_CSUM, 0, false),
	vxlanBool("udp6zerocsumtx", unix.IFLA_VXLAN_UDP_ZERO_CSUM6_TX, 1, false),
	vxlanBool("noudp6zerocsumtx", unix.IFLA_VXLAN_UDP_ZERO_CSUM6_TX, 0, false),
	vxlanBool("udp6zerocsumrx", unix.IFLA_VXLAN_UDP_ZERO_CSUM6_RX, 1, false),
	vxlanBool("noudp6zerocsumrx", unix.IFLA_VXLAN_UDP_ZERO_CSUM6_RX, 0, false),
	vxlanBool("remcsumtx", unix.IFLA_VXLAN_REMCSUM_TX, 1, false),
	vxlanBool("noremcsumtx", unix.IFLA_VXLAN_REMCSUM_TX, 0, false),
	vxlanBool("remcsumrx", unix.IFLA_VXLAN_REMCSUM_RX, 1, false),
	vxlanBool("noremcsumrx", unix.IFLA_VXLAN_REMCSUM_RX, 0, false),
	vxlanBool("localbypass", unix.IFLA_VXLAN_LOCALBYPASS, 1, true),
	vxlanBool("nolocalbypass", unix.IFLA_VXLAN_LOCALBYPASS, 0, true),
	{keyword: "reserved_bits", option: xtcpnl.VxlanOptionReservedBits, attr: xtcpnl.IflaVxlanReservedBits, exact: true},
	{keyword: kindKeyExternal, option: xtcpnl.VxlanOptionExternal, attr: unix.IFLA_VXLAN_COLLECT_METADATA, value: 1, flag: true},
	{keyword: kindKeyNoexternal, option: xtcpnl.VxlanOptionExternal, attr: unix.IFLA_VXLAN_COLLECT_METADATA, flag: true},
	{keyword: "gbp", option: xtcpnl.VxlanOptionGBP, attr: unix.IFLA_VXLAN_GBP, flag: true},
	{keyword: "gpe", option: xtcpnl.VxlanOptionGPE, attr: unix.IFLA_VXLAN_GPE, flag: true},
	vxlanBool("vnifilter", unix.IFLA_VXLAN_VNIFILTER, 1, true),
	vxlanBool("novnifilter", unix.IFLA_VXLAN_VNIFILTER, 0, true),
	vxlanBool("mcroute", xtcpnl.IflaVxlanMcRoute, 1, true),
	vxlanBool("nomcroute", xtcpnl.IflaVxlanMcRoute, 0, true),
}

// vxlanBool is a chain row for one of the u8 words EncodeVxlanInfoData
// writes as VxlanOptionBool.
func vxlanBool(keyword string, attr uint16, value uint64, exact bool) vxlanWord {
	return vxlanWord{keyword: keyword, option: xtcpnl.VxlanOptionBool, attr: attr, value: value, flag: true, exact: exact}
}

func lookupVxlanWord(arg string) *vxlanWord {
	for i := range vxlanParseChain {
		w := &vxlanParseChain[i]
		if w.exact && arg == w.keyword || !w.exact && matchesPrefix(arg, w.keyword) {
			return w
		}
	}
	return nil
}

// vxlanPortWarning is what ip prints for an add with neither dstport nor
// gpe, and then sends the request.
const vxlanPortWarning = "vxlan: destination port not specified\n" +
	"Will use Linux kernel default (non-standard value)\n" +
	"Use 'dstport 4789' to get the IANA assigned value\n" +
	"Use 'dstport 0' to get default and quiet this message\n"

// vxlanParse is vxlan_parse_opt's locals.
type vxlanParse struct {
	resolve   linkResolver
	out       []xtcpnl.VxlanSetting
	attrs     kindAttrs
	saddr     kindAddr
	daddr     kindAddr
	metadata  bool
	vnifilter bool
}

// parseVxlanArgs is vxlan_parse_opt. family is ip's -4 or -6, which adds
// a default group when no address is given; set is set_op, an RTM_NEWLINK
// without NLM_F_CREATE. errOut takes the dstport warning. resolve is
// ll_name_to_index, for dev.
func parseVxlanArgs(errOut io.Writer, family uint8, set bool, args []string, resolve linkResolver) ([]xtcpnl.VxlanSetting, error) {
	p := &vxlanParse{resolve: resolve}
	for rest := args; len(rest) > 0; {
		w := lookupVxlanWord(rest[0])
		if w == nil {
			if matchesPrefix(rest[0], bondHelpKeywordCst) {
				return nil, fmt.Errorf("%w: vxlan help is not implemented by goip", ErrNotImplemented)
			}
			return nil, kindUnknownErr(kindVxlanCst, rest[0])
		}
		var err error
		if rest, err = p.word(w, rest); err != nil {
			return nil, err
		}
	}
	if err := p.finish(errOut, set); err != nil {
		return nil, err
	}
	if family == unix.AF_INET || family == unix.AF_INET6 {
		p.out = append(p.out, xtcpnl.VxlanSetting{Option: xtcpnl.VxlanOptionDefaultGroup, Value: uint64(family)})
	}
	return p.out, nil
}

// word is one pass of the loop: rest starts at the keyword, and what is
// returned starts after its values.
func (p *vxlanParse) word(w *vxlanWord, rest []string) ([]string, error) {
	word := rest[0]
	rest = rest[1:]
	if w.flag {
		if err := p.attrs.dup(w.attr, word, word); err != nil {
			return nil, err
		}
		p.flag(w)
		return rest, nil
	}
	if w.option == xtcpnl.VxlanOptionGroup && p.daddr.inet && !p.daddr.multi ||
		w.option == xtcpnl.VxlanOptionRemote && p.daddr.multi {
		return nil, vxlanErr("both group and remote cannot be specified")
	}
	if len(rest) == 0 {
		return nil, linkUsage(incompleteErr())
	}
	v := rest[0]
	rest = rest[1:]
	key := w.key
	if w.option == xtcpnl.VxlanOptionReservedBits {
		key = v
	}
	if err := p.attrs.dup(w.attr, key, v); err != nil {
		return nil, err
	}
	switch w.option {
	case xtcpnl.VxlanOptionSrcPort:
		return p.srcport(v, rest)
	case xtcpnl.VxlanOptionGroup, xtcpnl.VxlanOptionRemote, xtcpnl.VxlanOptionLocal:
		return rest, p.addr(w.option, v)
	case xtcpnl.VxlanOptionLink:
		return rest, p.dev(v)
	default:
		s, err := vxlanValue(w.option, v)
		if err != nil {
			return nil, err
		}
		p.out = append(p.out, s)
		return rest, nil
	}
}

// flag is a word with no value: learning and external set the locals the
// after-loop checks read, and vnifilter sets its own.
func (p *vxlanParse) flag(w *vxlanWord) {
	switch {
	case w.option == xtcpnl.VxlanOptionExternal:
		p.metadata = w.value == 1
	case w.attr == unix.IFLA_VXLAN_VNIFILTER && w.value == 1:
		p.vnifilter = true
	}
	p.out = append(p.out, xtcpnl.VxlanSetting{Option: w.option, Value: w.value, Attr: w.attr})
}

// addr is group, remote or local: get_addr with the other address's
// family, then the type check.
func (p *vxlanParse) addr(option xtcpnl.VxlanOption, v string) error {
	other := p.saddr.p.family
	if option == xtcpnl.VxlanOptionLocal {
		other = p.daddr.p.family
	}
	pr, err := routeGetAddr(v, other)
	if err != nil {
		return err
	}
	a := newKindAddr(pr)
	ok, msg := a.inet && !a.multi, "invalid remote address"
	switch option {
	case xtcpnl.VxlanOptionGroup:
		ok, msg = a.multi, "invalid group address"
	case xtcpnl.VxlanOptionLocal:
		msg = "invalid local address"
	default: // remote
	}
	if !ok {
		return linkUsage(invargErr(msg, v))
	}
	if len(pr.data) == 0 {
		// any or all with the other address's family: ip sends the
		// attribute empty, which an xtcpnl.VxlanSetting can't hold.
		return fmt.Errorf("vxlan: an empty address %q: %w", v, ErrNotImplemented)
	}
	if option == xtcpnl.VxlanOptionLocal {
		p.saddr = a
	} else {
		p.daddr = a
	}
	addr, _ := netip.AddrFromSlice(pr.data)
	p.out = append(p.out, xtcpnl.VxlanSetting{Option: option, Addr: addr})
	return nil
}

// dev is ll_name_to_index as the word is read; IFLA_VXLAN_LINK goes out in
// word order.
func (p *vxlanParse) dev(v string) error {
	idx, err := p.resolve(v)
	if err != nil {
		return err
	}
	if idx <= 0 {
		// ll_name_to_index's 0. A negative one, an "if<n>" name past
		// INT_MAX, is refused here too, where ip sends it.
		return nodevExitErr(v)
	}
	p.out = append(p.out, xtcpnl.VxlanSetting{Option: xtcpnl.VxlanOptionLink, Value: uint64(cint.Bits(idx))})
	return nil
}

// srcport reads the range's two values; the second is NEXT_ARG'd too.
func (p *vxlanParse) srcport(low string, rest []string) ([]string, error) {
	lo, ok := getU16(low, 0)
	if !ok {
		return nil, linkUsage(invargErr("min port", low))
	}
	if len(rest) == 0 {
		return nil, linkUsage(incompleteErr())
	}
	hi, ok := getU16(rest[0], 0)
	if !ok {
		return nil, linkUsage(invargErr("max port", rest[0]))
	}
	p.out = append(p.out, xtcpnl.VxlanSetting{Option: xtcpnl.VxlanOptionSrcPort, Value: uint64(lo), High: uint64(hi)})
	return rest[1:], nil
}

// vxlanValue reads the value of a word whose value is a number or a name.
func vxlanValue(option xtcpnl.VxlanOption, v string) (xtcpnl.VxlanSetting, error) {
	s := xtcpnl.VxlanSetting{Option: option}
	var err error
	ok := true
	var msg string
	switch option {
	case xtcpnl.VxlanOptionID:
		s.Value, err = kindID(v)
	case xtcpnl.VxlanOptionTTL:
		return vxlanTTL(v)
	case xtcpnl.VxlanOptionTOS:
		s.Value, err = kindTOS(v)
	case xtcpnl.VxlanOptionDF:
		s.Value, err = kindDF(v)
	case xtcpnl.VxlanOptionLabel:
		s.Value, err = kindLabel(v)
	case xtcpnl.VxlanOptionAging:
		s.Value, ok = vxlanU32(v, noneKeywordCst)
		msg = xtcpnl.VxlanKeywordAgeing + " timer"
	case xtcpnl.VxlanOptionLimit:
		s.Value, ok = vxlanU32(v, "unlimited")
		msg = "max addresses"
	case xtcpnl.VxlanOptionDstPort:
		var n uint16
		n, ok = getU16(v, 0)
		msg, s.Value = "dst port", uint64(n)
	default: // reserved_bits
		s.Value, ok = getU64(v, 0)
		msg = "reserved_bits"
	}
	if err != nil {
		return xtcpnl.VxlanSetting{}, err
	}
	if !ok {
		return xtcpnl.VxlanSetting{}, linkUsage(invargErr(msg, v))
	}
	return s, nil
}

// vxlanU32 is get_u32 base 0, or 0 for zero, the word that means none.
func vxlanU32(v, zero string) (uint64, bool) {
	if v == zero {
		return 0, true
	}
	n, ok := getU32(v, 0)
	return uint64(n), ok
}

// vxlanTTL is ttl's value: inherit is IFLA_VXLAN_TTL_INHERIT, and auto is
// 0.
func vxlanTTL(v string) (xtcpnl.VxlanSetting, error) {
	n, inherit, err := kindTTL(v)
	switch {
	case err != nil:
		return xtcpnl.VxlanSetting{}, err
	case inherit:
		return xtcpnl.VxlanSetting{Option: xtcpnl.VxlanOptionTTLInherit}, nil
	}
	return xtcpnl.VxlanSetting{Option: xtcpnl.VxlanOptionTTL, Value: n}, nil
}

// finish is vxlan_parse_opt's checks after the loop, in its order, then
// the warning.
func (p *vxlanParse) finish(errOut io.Writer, set bool) error {
	hasID := p.attrs.has(unix.IFLA_VXLAN_ID)
	switch {
	case !p.metadata && p.vnifilter:
		return vxlanErr("vnifilter is valid only when 'external' is set")
	case p.metadata && hasID:
		return vxlanErr("both 'external' and vni cannot be specified")
	case !p.metadata && !p.vnifilter && !hasID && !set:
		return vxlanErr("missing virtual network identifier")
	case p.daddr.multi && !p.attrs.has(unix.IFLA_VXLAN_LINK):
		return vxlanErr("'group' requires 'dev' to be specified")
	}
	if !p.attrs.has(unix.IFLA_VXLAN_PORT) && !p.attrs.has(unix.IFLA_VXLAN_GPE) && !set {
		if _, err := io.WriteString(errOut, vxlanPortWarning); err != nil {
			return fmt.Errorf("vxlan: %w", err)
		}
	}
	return nil
}

// vxlanErr is a refusal vxlan_parse_opt prints and returns -1 from.
func vxlanErr(msg string) error {
	return kindOptErr(kindVxlanCst, msg)
}
