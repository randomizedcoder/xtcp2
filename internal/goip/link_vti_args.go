package goip

import (
	"fmt"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is vti_parse_opt and vti6_parse_opt (ip/link_vti.c and
// ip/link_vti6.c, iproute2 v7.2.0), one chain for both kinds (KG4-T06,
// docs/netlink/goip-link-kinds.md).
//
//   - vti's key, ikey, okey, remote, local and dev are `matches`, in that
//     order; fwmark is strcmp. The endpoints are get_addr in the kind's
//     family, and there is no after-loop check.

const (
	kindVtiCst  = "vti"
	kindVti6Cst = "vti6"
)

// vtiKinds are the kinds the vti prefetch reads.
var vtiKinds = []string{kindVtiCst, kindVti6Cst}

const (
	vtiKey  = "key"
	vtiIKey = "ikey"
	vtiOKey = "okey"
)

// vtiChain is vti_parse_opt's matches chain, in order.
var vtiChain = []string{vtiKey, vtiIKey, vtiOKey, kindKeyRemote, tnlKeyLocal, devKeywordCst}

// vtiUsageLine is vti_print_help's first line: the kind padded to 4.
func vtiUsageLine(kind string) string {
	return fmt.Sprintf("Usage: ... %-4s\t[ remote ADDR ]", kind)
}

// vtiCanon is the chain's first hit for arg, or "" for none.
func vtiCanon(arg string) string {
	for _, w := range vtiChain {
		if matchesPrefix(arg, w) {
			return w
		}
	}
	if arg == tnlKeyFwmark {
		return arg
	}
	return ""
}

// parseVtiArgs is vti_parse_opt, or vti6_parse_opt when v6. resolve is
// ll_name_to_index, for dev.
func parseVtiArgs(v6 bool, args []string, resolve linkResolver) ([]xtcpnl.VtiSetting, error) {
	kind, family := kindVtiCst, uint8(unix.AF_INET)
	if v6 {
		kind, family = kindVti6Cst, unix.AF_INET6
	}
	var out []xtcpnl.VtiSetting
	a := &tunnelArgs{args: args}
	for ; a.more(); a.i++ {
		canon := vtiCanon(a.word())
		if canon == "" {
			return nil, tunnelUsageErr(vtiUsageLine(kind))
		}
		v, err := a.next()
		if err != nil {
			return nil, err
		}
		s, err := vtiValue(canon, v, family, resolve)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// vtiKeyOptions are the key words' options.
var vtiKeyOptions = map[string]xtcpnl.VtiOption{
	vtiKey: xtcpnl.VtiOptionKey, vtiIKey: xtcpnl.VtiOptionIKey, vtiOKey: xtcpnl.VtiOptionOKey,
}

// vtiValue is one word's value as a setting.
func vtiValue(canon, v string, family uint8, resolve linkResolver) (xtcpnl.VtiSetting, error) {
	switch canon {
	case kindKeyRemote, tnlKeyLocal:
		addr, err := tunnelAddr(v, family)
		if err != nil {
			return xtcpnl.VtiSetting{}, err
		}
		o := xtcpnl.VtiOptionRemote
		if canon == tnlKeyLocal {
			o = xtcpnl.VtiOptionLocal
		}
		return xtcpnl.VtiSetting{Option: o, Addr: addr}, nil
	case devKeywordCst:
		idx, err := resolveDev(v, resolve)
		if err != nil {
			return xtcpnl.VtiSetting{}, err
		}
		return xtcpnl.VtiSetting{Option: xtcpnl.VtiOptionLink, Value: idx}, nil
	case tnlKeyFwmark:
		n, ok := getU32(v, 0)
		if !ok {
			return xtcpnl.VtiSetting{}, linkUsage(invargErr("invalid fwmark\n", v))
		}
		return xtcpnl.VtiSetting{Option: xtcpnl.VtiOptionFwMark, Value: uint64(n)}, nil
	}
	n, err := tnlParseKey(canon, v)
	if err != nil {
		return xtcpnl.VtiSetting{}, err
	}
	return xtcpnl.VtiSetting{Option: vtiKeyOptions[canon], Value: n}, nil
}

// resolveDev is dev's ll_name_to_index, whose zero is nodev's exit(255).
func resolveDev(v string, resolve linkResolver) (uint64, error) {
	idx, err := resolve(v)
	if err != nil {
		return 0, err
	}
	if idx <= 0 {
		return 0, nodevExitErr(v)
	}
	return uint64(cint.Bits(idx)), nil
}

// vtiData is vti's and vti6's parse_opt: the prefetch on a set, the
// words, then the encoder over the prefetched state.
func vtiData(env *kindEnv, kind string, rest []string) ([]byte, error) {
	v6 := kind == kindVti6Cst
	family := uint8(unix.AF_INET)
	if v6 {
		family = unix.AF_INET6
	}
	raw, err := env.tunnelPrefetch(vtiKinds, family, xtcpnl.IflaVtiLocal, xtcpnl.IflaVtiRemote)
	if err != nil {
		return nil, err
	}
	settings, err := parseVtiArgs(v6, rest, env.resolve)
	if err != nil {
		return nil, err
	}
	return xtcpnl.EncodeVtiInfoData(settings, xtcpnl.ParseVtiInfoData(raw), v6)
}
