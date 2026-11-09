package goip

import (
	"fmt"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is bareudp_parse_opt (ip/iplink_bareudp.c, iproute2 v7.2.0):
// the argv after `type bareudp`, turned into xtcpnl.BareudpSetting values,
// which xtcpnl.EncodeBareudpInfoData writes in its own order (KG3-T03,
// docs/netlink/goip-link-kinds.md).
//
//   - Every keyword is a `matches`, so `d` is dstport, `m` multiproto and
//     `n` nomultiproto.
//   - Each word is check_duparg on its attribute; multiproto and
//     nomultiproto share one, and name the word itself.
//   - After the loop, a missing dstport and then a missing ethertype are
//     missarg, exit(-1).

const kindBareudpCst = "bareudp"

// The value words, in bareudp_parse_opt's order.
var bareudpValueWords = []struct {
	keyword string
	option  xtcpnl.BareudpOption
	attr    uint16
}{
	{kindKeyDstport, xtcpnl.BareudpOptionDstPort, unix.IFLA_BAREUDP_PORT},
	{"ethertype", xtcpnl.BareudpOptionEtherType, unix.IFLA_BAREUDP_ETHERTYPE},
	{"srcportmin", xtcpnl.BareudpOptionSrcPortMin, unix.IFLA_BAREUDP_SRCPORT_MIN},
}

// parseBareudpArgs is bareudp_parse_opt.
func parseBareudpArgs(args []string) ([]xtcpnl.BareudpSetting, error) {
	var attrs kindAttrs
	out := make([]xtcpnl.BareudpSetting, 0, len(args))
	for rest := args; len(rest) > 0; rest = rest[1:] {
		word := rest[0]
		if multi := matchesPrefix(word, "multiproto"); multi || matchesPrefix(word, "nomultiproto") {
			if err := attrs.dup(unix.IFLA_BAREUDP_MULTIPROTO_MODE, word, word); err != nil {
				return nil, err
			}
			s := xtcpnl.BareudpSetting{Option: xtcpnl.BareudpOptionMultiproto}
			if multi {
				s.Value = 1
			}
			out = append(out, s)
			continue
		}
		i := bareudpValueWord(word)
		if i < 0 {
			if matchesPrefix(word, bondHelpKeywordCst) {
				return nil, fmt.Errorf("%w: bareudp help is not implemented by goip", ErrNotImplemented)
			}
			return nil, kindUnknownErr(kindBareudpCst, word)
		}
		w := bareudpValueWords[i]
		if rest = rest[1:]; len(rest) == 0 {
			return nil, linkUsage(incompleteErr())
		}
		if err := attrs.dup(w.attr, w.keyword, rest[0]); err != nil {
			return nil, err
		}
		s, err := bareudpValue(w.option, w.keyword, rest[0])
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	switch {
	case !attrs.has(unix.IFLA_BAREUDP_PORT):
		return nil, missargErr(kindKeyDstport)
	case !attrs.has(unix.IFLA_BAREUDP_ETHERTYPE):
		return nil, missargErr("ethertype")
	}
	return out, nil
}

// bareudpValueWord is the index of the value word arg abbreviates, or -1.
func bareudpValueWord(arg string) int {
	for i, w := range bareudpValueWords {
		if matchesPrefix(arg, w.keyword) {
			return i
		}
	}
	return -1
}

// bareudpValue reads a value word's value: dstport is get_be16 and
// srcportmin get_u16, both base 0, and ethertype is ll_proto_a2n. Each
// invarg names its keyword.
func bareudpValue(option xtcpnl.BareudpOption, keyword, v string) (xtcpnl.BareudpSetting, error) {
	var n uint16
	var ok bool
	if option == xtcpnl.BareudpOptionEtherType {
		n, ok = llProtoA2N(v)
	} else {
		n, ok = getU16(v, 0)
	}
	if !ok {
		return xtcpnl.BareudpSetting{}, linkUsage(invargErr(keyword, v))
	}
	return xtcpnl.BareudpSetting{Option: option, Value: uint64(n)}, nil
}

// missargErr is missarg: a required argument not given, exit(-1).
func missargErr(key string) error {
	return linkUsage(fmt.Errorf("%w: Error: argument %q is required", ErrInvalidArgument, key))
}
