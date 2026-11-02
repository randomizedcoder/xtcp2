package goip

import (
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is hsr_parse_opt (ip/iplink_hsr.c, iproute2 v7.2.0) (KG5-T02,
// docs/netlink/goip-link-kinds.md).
//
//   - supervision, version, proto, slave1 and slave2 are `matches`, in
//     that order, so `s` is supervision and `sl` slave1; interlink is
//     strcmp; help is `matches`, after them.
//   - Each word sends its attribute as it is read, so a repeat sends twice.
//   - version and proto test get_u8's return rather than the value, so any
//     u8 passes; a value past a u8 is invarg, 255.
//   - The slaves and the interlink resolve as they are read; a name with
//     no device is invarg("No such interface"), 255.

const kindHsrCst = "hsr"

// hsrUsageLine is hsr's print_usage's first line, with its tab.
const hsrUsageLine = "Usage:\tip link add name NAME type hsr slave1 SLAVE1-IF slave2 SLAVE2-IF"

// hsrWord is one word of hsr_parse_opt's chain.
type hsrWord struct {
	keyword string
	prefix  bool // matches(); otherwise strcmp
	option  xtcpnl.HsrOption
	// invalid is the invarg text for a value that isn't a u8; empty for
	// the words that name a device.
	invalid string
}

// hsrChain is hsr_parse_opt's if/else chain, in order, help aside.
var hsrChain = []hsrWord{
	{"supervision", true, xtcpnl.HsrOptionSupervision, "ADDR-BYTE is invalid"},
	{"version", true, xtcpnl.HsrOptionVersion, "version is invalid"},
	{xfrmProtoCst, true, xtcpnl.HsrOptionProto, "protocol is invalid"},
	{"slave1", true, xtcpnl.HsrOptionSlave1, ""},
	{"slave2", true, xtcpnl.HsrOptionSlave2, ""},
	{"interlink", false, xtcpnl.HsrOptionInterlink, ""},
}

func lookupHsrWord(arg string) *hsrWord {
	for i := range hsrChain {
		w := &hsrChain[i]
		if (w.prefix && matchesPrefix(arg, w.keyword)) || (!w.prefix && arg == w.keyword) {
			return w
		}
	}
	return nil
}

// parseHsrArgs is hsr_parse_opt. resolve is ll_name_to_index, for the
// slaves and the interlink.
func parseHsrArgs(args []string, resolve linkResolver) ([]xtcpnl.HsrSetting, error) {
	var out []xtcpnl.HsrSetting
	a := &tunnelArgs{args: args}
	for ; a.more(); a.i++ {
		word := a.word()
		w := lookupHsrWord(word)
		if w == nil {
			if matchesPrefix(word, bondHelpKeywordCst) {
				return nil, tunnelUsageErr(hsrUsageLine)
			}
			return nil, fmt.Errorf("%w: hsr: what is %q?", ErrInvalidArgument, word)
		}
		v, err := a.next()
		if err != nil {
			return nil, err
		}
		n, err := hsrValue(w, v, resolve)
		if err != nil {
			return nil, err
		}
		out = append(out, xtcpnl.HsrSetting{Option: w.option, Value: n})
	}
	return out, nil
}

// hsrValue is a word's value: get_u8 in base 0, or a device's ifindex.
func hsrValue(w *hsrWord, v string, resolve linkResolver) (uint64, error) {
	if w.invalid != "" {
		n, ok := getU8(v, 0)
		if !ok {
			return 0, linkUsage(invargErr(w.invalid, v))
		}
		return uint64(n), nil
	}
	idx, err := resolve(v)
	if err != nil {
		return 0, err
	}
	if idx == 0 {
		return 0, linkUsage(invargErr("No such interface", v))
	}
	return uint64(s32Bits(idx)), nil
}

// hsrData is hsr's parse_opt.
func hsrData(env *kindEnv, rest []string) ([]byte, error) {
	settings, err := parseHsrArgs(rest, env.resolve)
	if err != nil {
		return nil, err
	}
	return xtcpnl.EncodeHsrInfoData(settings)
}
