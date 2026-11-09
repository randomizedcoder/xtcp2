package goip

import (
	"errors"
	"fmt"
	"io"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is macvlan_parse_opt (ip/iplink_macvlan.c, iproute2 v7.2.0),
// the parser of both kinds it serves, macvlan and macvtap: the argv after
// `type macvlan`, turned into xtcpnl.MacvlanSetting values in argv order,
// which xtcpnl.EncodeMacvlanInfoData writes (KG2-T02,
// docs/netlink/goip-link-kinds.md).
//
// Every keyword but bclim is a `matches`, tried in the chain's order; the
// values are strcmp. Its refusals return -1, status 1, but for NEXT_ARG's
// incomplete_command, 255 (ErrLinkUsage). A refusal's text names the kind,
// lu->id.

const (
	kindMacvlanCst = "macvlan"
	kindMacvtapCst = "macvtap"
)

// macvlanAddrLenCst is ETH_ALEN, the length every macaddr must have.
const macvlanAddrLenCst = 6

// macvlanArm is one test in macvlan_parse_opt's chain.
type macvlanArm uint8

const (
	macvlanArmMode macvlanArm = iota
	macvlanArmFlag
	macvlanArmMacaddr
	macvlanArmNopromisc
	macvlanArmNodst
	macvlanArmBcQueueLen
	macvlanArmBclim
)

// macvlanParseChain is macvlan_parse_opt's if/else chain, in order; help
// and the unknown word follow it. bclim alone is a strcmp.
var macvlanParseChain = []struct {
	keyword string
	arm     macvlanArm
	exact   bool
}{
	{keyword: xfrmModeCst, arm: macvlanArmMode},
	{keyword: "flag", arm: macvlanArmFlag},
	{keyword: "macaddr", arm: macvlanArmMacaddr},
	{keyword: "nopromisc", arm: macvlanArmNopromisc},
	{keyword: "nodst", arm: macvlanArmNodst},
	{keyword: "bcqueuelen", arm: macvlanArmBcQueueLen},
	{keyword: "bclim", arm: macvlanArmBclim, exact: true},
}

// The values each arm compares with strcmp.
var (
	macvlanModeWords = map[string]uint32{
		"private":     xtcpnl.MacvlanModePrivate,
		"vepa":        xtcpnl.MacvlanModeVepa,
		kindBridgeCst: xtcpnl.MacvlanModeBridge,
		"passthru":    xtcpnl.MacvlanModePassthru,
		"source":      xtcpnl.MacvlanModeSource,
	}
	macvlanFlagWords = map[string]uint16{
		"nopromisc": xtcpnl.MacvlanFlagNopromisc,
		"nodst":     xtcpnl.MacvlanFlagNodst,
		"null":      0,
	}
	macvlanMacaddrOps = map[string]uint64{
		"add":   xtcpnl.MacvlanMacaddrAdd,
		"del":   xtcpnl.MacvlanMacaddrDel,
		"set":   xtcpnl.MacvlanMacaddrSet,
		"flush": xtcpnl.MacvlanMacaddrFlush,
	}
)

func lookupMacvlanArm(arg string) (macvlanArm, bool) {
	for _, a := range macvlanParseChain {
		if a.exact && arg == a.keyword || !a.exact && matchesPrefix(arg, a.keyword) {
			return a.arm, true
		}
	}
	return 0, false
}

// macvlanUsageErr is the first line of print_explain, which a bad macaddr
// op prints alone.
func macvlanUsageErr(kind string) error {
	return fmt.Errorf("%w: Usage: ... %s mode MODE [flag MODE_FLAG] MODE_OPTS [bcqueuelen BC_QUEUE_LEN] [bclim BCLIM]",
		ErrInvalidArgument, kind)
}

// parseMacvlanArgs is macvlan_parse_opt for kind. errOut takes the warning
// ll_addr_a2n prints for the word that ends a `macaddr set`, which ip
// prints and goes on.
func parseMacvlanArgs(errOut io.Writer, kind string, args []string) ([]xtcpnl.MacvlanSetting, error) {
	var out []xtcpnl.MacvlanSetting
	for rest := args; len(rest) > 0; {
		word := rest[0]
		rest = rest[1:]
		arm, ok := lookupMacvlanArm(word)
		if !ok {
			if matchesPrefix(word, bondHelpKeywordCst) {
				return nil, fmt.Errorf("%w: %s help is not implemented by goip", ErrNotImplemented, kind)
			}
			return nil, fmt.Errorf("%w: %s: unknown option %q?", ErrInvalidArgument, kind, word)
		}
		switch arm {
		case macvlanArmNopromisc:
			out = append(out, xtcpnl.MacvlanSetting{Option: xtcpnl.MacvlanOptionFlag, Value: uint64(xtcpnl.MacvlanFlagNopromisc)})
			continue
		case macvlanArmNodst:
			out = append(out, xtcpnl.MacvlanSetting{Option: xtcpnl.MacvlanOptionFlag, Value: uint64(xtcpnl.MacvlanFlagNodst)})
			continue
		default: // an arm with a value
		}
		if len(rest) == 0 {
			return nil, linkUsage(incompleteErr())
		}
		var s xtcpnl.MacvlanSetting
		var err error
		if arm == macvlanArmMacaddr {
			s, rest, err = readMacvlanMacaddr(errOut, kind, rest)
		} else {
			s, err = readMacvlanValue(arm, rest[0])
			rest = rest[1:]
		}
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// macvlanData parses and encodes kind's words. The encoder makes the
// nopromisc check after the loop, as macvlan_parse_opt does.
func macvlanData(errOut io.Writer, kind string, rest []string) ([]byte, error) {
	settings, err := parseMacvlanArgs(errOut, kind, rest)
	if err != nil {
		return nil, err
	}
	b, err := xtcpnl.EncodeMacvlanInfoData(settings)
	if errors.Is(err, xtcpnl.ErrMacvlanNopromisc) {
		return nil, fmt.Errorf("%w: %s: nopromisc flag only valid in passthru mode", ErrInvalidArgument, kind)
	}
	return b, err
}

// readMacvlanValue reads the value of a mode, flag, bcqueuelen or bclim arm.
func readMacvlanValue(arm macvlanArm, v string) (xtcpnl.MacvlanSetting, error) {
	switch arm {
	case macvlanArmMode:
		m, ok := macvlanModeWords[v]
		if !ok {
			return xtcpnl.MacvlanSetting{}, fmt.Errorf("%w: Error: argument of \"mode\" must be \"private\", \"vepa\", \"bridge\", \"passthru\" or \"source\", not %q",
				ErrInvalidArgument, v)
		}
		return xtcpnl.MacvlanSetting{Option: xtcpnl.MacvlanOptionMode, Value: uint64(m)}, nil
	case macvlanArmFlag:
		f, ok := macvlanFlagWords[v]
		if !ok {
			return xtcpnl.MacvlanSetting{}, fmt.Errorf("%w: Error: argument of \"flag\" must be \"nopromisc\", \"nodst\" or \"null\", not %q",
				ErrInvalidArgument, v)
		}
		return xtcpnl.MacvlanSetting{Option: xtcpnl.MacvlanOptionFlag, Value: uint64(f)}, nil
	case macvlanArmBcQueueLen:
		n, ok := getU32(v, 0)
		if !ok {
			return xtcpnl.MacvlanSetting{}, fmt.Errorf("%w: Error: argument of \"bcqueuelen\" must be a positive integer [0-4294967295], not %q",
				ErrInvalidArgument, v)
		}
		return xtcpnl.MacvlanSetting{Option: xtcpnl.MacvlanOptionBcQueueLen, Value: uint64(n)}, nil
	default: // bclim
		n, ok := getS32(v, 0)
		if !ok {
			return xtcpnl.MacvlanSetting{}, fmt.Errorf("%w: Error: illegal value for \"bclim\": %q", ErrInvalidArgument, v)
		}
		return xtcpnl.MacvlanSetting{Option: xtcpnl.MacvlanOptionBcCutoff, Cutoff: n}, nil
	}
}

// readMacvlanMacaddr reads `macaddr`'s op from the front of args and its
// addresses: one for add and del, none for flush, and for set every word
// ll_addr_a2n reads as ETH_ALEN bytes, until one it doesn't, which is left
// at the front of what it returns, cut as ll_addr_a2n left it.
func readMacvlanMacaddr(errOut io.Writer, kind string, args []string) (xtcpnl.MacvlanSetting, []string, error) {
	op, ok := macvlanMacaddrOps[args[0]]
	if !ok {
		return xtcpnl.MacvlanSetting{}, nil, macvlanUsageErr(kind)
	}
	rest := args[1:]
	s := xtcpnl.MacvlanSetting{Option: xtcpnl.MacvlanOptionMacaddr, Value: op}
	switch op {
	case xtcpnl.MacvlanMacaddrAdd, xtcpnl.MacvlanMacaddrDel:
		if len(rest) == 0 {
			return s, nil, linkUsage(incompleteErr())
		}
		b, n, bad := llAddrA2NPart(rest[0], macvlanAddrLenCst)
		switch {
		case bad != "":
			return s, nil, fmt.Errorf("%w: %q is invalid lladdr", ErrInvalidArgument, bad)
		case n != macvlanAddrLenCst:
			// ll_addr_a2n printed nothing, and neither does ip.
			return s, nil, fmt.Errorf("%w: %q is not a %d-byte lladdr", ErrInvalidArgument, rest[0], macvlanAddrLenCst)
		}
		s.Addrs = [][]byte{b}
		return s, rest[1:], nil
	case xtcpnl.MacvlanMacaddrSet:
		for len(rest) > 0 {
			b, n, bad := llAddrA2NPart(rest[0], macvlanAddrLenCst)
			if n == macvlanAddrLenCst {
				s.Addrs = append(s.Addrs, b)
				rest = rest[1:]
				continue
			}
			if bad != "" {
				if _, err := fmt.Fprintf(errOut, "\"%s\" is invalid lladdr.\n", bad); err != nil {
					return s, nil, err
				}
			}
			// PREV_ARG: the word goes back to the chain, as cut.
			rest = append([]string{llAddrCut(rest[0])}, rest[1:]...)
			break
		}
	}
	return s, rest, nil
}
