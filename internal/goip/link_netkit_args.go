package goip

import (
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// This file is netkit_parse_opt (ip/iplink_netkit.c, iproute2 v7.2.0)
// (KG5-T03, docs/netlink/goip-link-kinds.md).
//
//   - Every word is strcmp. mode takes l3 or l2, and a second mode is
//     duparg, 255; forward and blackhole are POLICY, or PEER_POLICY after
//     peer; scrub is SCRUB, or PEER_SCRUB after peer.
//   - A second peer is duparg("peer", the next word), which is "(null)" at
//     the end of the line.
//   - After peer, the first word that isn't netkit's starts iplink_parse
//     over the rest of the line, into PEER_INFO; a type in it is
//     duparg("type", the word after the kind's value). Before peer, that
//     word, help among them, is netkit's unknown option, 1.
//   - Parsing the peer into a fresh linkParsed is the save, zero and
//     restore the C does around the shared ifinfomsg, as in vethData.

const kindNetkitCst = "netkit"

// The netkit words.
const (
	netkitForward   = "forward"
	netkitBlackhole = "blackhole"
	netkitScrub     = "scrub"
)

// cNullCst is what glibc's printf prints for a NULL %s, as duparg does at
// the end of argv.
const cNullCst = "(null)"

// netkitModes are mode's values.
var netkitModes = map[string]uint64{"l3": unix.NETKIT_L3, "l2": unix.NETKIT_L2}

// netkitScrubs are scrub's values.
var netkitScrubs = map[string]uint64{"none": xtcpnl.NetkitScrubNone, addrDefaultGroupCst: xtcpnl.NetkitScrubDefault}

// netkitData is netkit's parse_opt.
func netkitData(env *kindEnv, rest []string) ([]byte, error) {
	var out []xtcpnl.NetkitSetting
	var peer *xtcpnl.VethPeer
	seenMode, seenPeer := false, false
	a := &tunnelArgs{args: rest}
loop:
	for ; a.more(); a.i++ {
		word := a.word()
		switch word {
		case iptnlMode:
			v, err := a.next()
			if err != nil {
				return nil, err
			}
			if seenMode {
				return nil, linkUsage(dupargErr(iptnlMode, v))
			}
			seenMode = true
			m, ok := netkitModes[v]
			if !ok {
				return nil, fmt.Errorf("%w: Error: argument of \"mode\" must be either \"l3\" or \"l2\"", ErrInvalidArgument)
			}
			out = append(out, xtcpnl.NetkitSetting{Option: xtcpnl.NetkitOptionMode, Value: m})
		case netkitForward:
			out = append(out, xtcpnl.NetkitSetting{Option: xtcpnl.NetkitOptionPolicy, Value: unix.NETKIT_PASS})
		case netkitBlackhole:
			out = append(out, xtcpnl.NetkitSetting{Option: xtcpnl.NetkitOptionPolicy, Value: unix.NETKIT_DROP})
		case vethPeerCst:
			if seenPeer {
				return nil, linkUsage(dupargErr(vethPeerCst, argOrNull(rest, a.i+1)))
			}
			seenPeer = true
			out = append(out, xtcpnl.NetkitSetting{Option: xtcpnl.NetkitOptionPeer})
		case netkitScrub:
			v, err := a.next()
			if err != nil {
				return nil, err
			}
			s, ok := netkitScrubs[v]
			if !ok {
				return nil, fmt.Errorf("%w: Error: scrub must be either \"none\" or \"default\"", ErrInvalidArgument)
			}
			out = append(out, xtcpnl.NetkitSetting{Option: xtcpnl.NetkitOptionScrub, Value: s})
		default:
			if !seenPeer {
				return nil, fmt.Errorf("%w: netkit: unknown option %q?", ErrInvalidArgument, word)
			}
			var err error
			if peer, err = netkitPeer(env, rest[a.i:]); err != nil {
				return nil, err
			}
			break loop
		}
	}
	return xtcpnl.EncodeNetkitInfoData(out, peer)
}

// netkitPeer is iplink_parse over the peer's words.
func netkitPeer(env *kindEnv, args []string) (*xtcpnl.VethPeer, error) {
	p, err := parseLinkArgs(args, env.verb.create(), env.family, env.resolve, env.isVRF, env.fetch)
	if err != nil {
		return nil, err
	}
	if p.hasKind {
		// duparg("type", argv[err]): argv starts at the peer's first
		// word, so index consumed is the word after the kind's value.
		return nil, linkUsage(dupargErr(linkTypeKeywordCst, argOrNull(args, p.consumed)))
	}
	return &xtcpnl.VethPeer{Index: p.index, IfFlags: p.ifFlags, IfChange: p.ifChange, Attrs: p.attrs}, nil
}

// argOrNull is argv[i], or "(null)" past the end, as printf prints
// argv's terminating NULL.
func argOrNull(args []string, i int) string {
	if i < len(args) {
		return args[i]
	}
	return cNullCst
}
