package goip

import (
	"errors"
	"fmt"
	"net/netip"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// This file is bond_parse_opt and bond_slave_parse_opt (ip/iplink_bond.c,
// ip/iplink_bond_slave.c, iproute2 7.1.0): the argv after `type bond` or
// `type bond_slave`, turned into xtcpnl.BondSetting values in argv order.
// xtcpnl.EncodeBondInfoData and EncodeBondSlaveData then write them.
//
// # Why there is a keyword list here and not just xtcpnl.BondOptions
//
// BondOptions is in bond_print_opt order, and that is the order the
// renderer needs. The parser needs bond_parse_opt's order, which differs,
// and the order decides what a prefix means: matches() is tried keyword by
// keyword, so `p` is `peer_notify_delay` (tried seventh) and not `primary`
// (fifteenth), and `a` is `active_slave`. bondParseKeywords is that chain.
// The rows point back at BondOptions by id, so the wire width and value
// names still come from the one table.
//
// Two rows of BondOptions have no keyword here. `ad_info` is read-only, and
// `lacp_strict` is newer than the pinned 7.1.0, whose parser does not know
// it: `ip link add … type bond lacp_strict on` is "unknown command".

// ErrInvalidArgument is an argument `ip` refuses before it sends anything:
// invarg, nodev, an unknown keyword, a missing value. The message after it
// is iproute2's.
var ErrInvalidArgument = errors.New("invalid argument")

// bondArgKind is how one keyword reads its value.
type bondArgKind uint8

const (
	bondArgIndex   bondArgKind = iota + 1 // get_index over the row's Names
	bondArgUint                           // get_u8/u16/u32 by the row's width, base 0
	bondArgIfindex                        // ll_name_to_index, nodev on 0
	bondArgClear                          // no value; the attribute is 0
	bondArgTargets                        // optional comma list; an empty nest without one
	bondArgOnOff                          // parse_on_off: strcmp "off"/"on", no numbers
	bondArgLLAddr                         // ll_addr_a2n into a 32-byte buffer
	bondArgS32                            // get_s32, base 0
	bondArgHelp                           // explain() and fail
)

// bondKeyword is one arm of the parse chain.
type bondKeyword struct {
	keyword string
	match   xtcpnl.BondMatch
	id      uint16
	arg     bondArgKind
	// invarg is iproute2's message for a value it refuses.
	invarg string
}

// bondHelpKeywordCst is the help arm both chains end with.
const bondHelpKeywordCst = "help"

// bondLLAddrBufCst is `char abuf[32]` in bond_parse_opt's ad_actor_system arm.
const bondLLAddrBufCst = 32

// bondParseKeywords is bond_parse_opt's if/else chain, in order.
var bondParseKeywords = []bondKeyword{
	{keyword: "mode", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_MODE, arg: bondArgIndex, invarg: "invalid mode"},
	{keyword: "active_slave", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_ACTIVE_SLAVE, arg: bondArgIfindex},
	{keyword: "clear_active_slave", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_ACTIVE_SLAVE, arg: bondArgClear},
	{keyword: "miimon", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_MIIMON, arg: bondArgUint, invarg: "invalid miimon"},
	{keyword: "updelay", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_UPDELAY, arg: bondArgUint, invarg: "invalid updelay"},
	{keyword: "downdelay", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_DOWNDELAY, arg: bondArgUint, invarg: "invalid downdelay"},
	{keyword: "peer_notify_delay", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_PEER_NOTIF_DELAY, arg: bondArgUint, invarg: "invalid peer_notify_delay"},
	{keyword: "use_carrier", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_USE_CARRIER, arg: bondArgUint, invarg: "invalid use_carrier"},
	{keyword: "arp_interval", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_ARP_INTERVAL, arg: bondArgUint, invarg: "invalid arp_interval"},
	{keyword: "arp_ip_target", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_ARP_IP_TARGET, arg: bondArgTargets},
	{keyword: "ns_ip6_target", match: xtcpnl.BondMatchExact, id: unix.IFLA_BOND_NS_IP6_TARGET, arg: bondArgTargets},
	{keyword: "arp_validate", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_ARP_VALIDATE, arg: bondArgIndex, invarg: "invalid arp_validate"},
	{keyword: "arp_all_targets", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_ARP_ALL_TARGETS, arg: bondArgIndex, invarg: "invalid arp_all_targets"},
	{keyword: "arp_missed_max", match: xtcpnl.BondMatchExact, id: unix.IFLA_BOND_MISSED_MAX, arg: bondArgUint, invarg: "invalid arp_missed_max"},
	{keyword: "primary", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_PRIMARY, arg: bondArgIfindex},
	{keyword: "primary_reselect", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_PRIMARY_RESELECT, arg: bondArgIndex, invarg: "invalid primary_reselect"},
	{keyword: "fail_over_mac", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_FAIL_OVER_MAC, arg: bondArgIndex, invarg: "invalid fail_over_mac"},
	{keyword: "xmit_hash_policy", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_XMIT_HASH_POLICY, arg: bondArgIndex, invarg: "invalid xmit_hash_policy"},
	{keyword: "resend_igmp", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_RESEND_IGMP, arg: bondArgUint, invarg: "invalid resend_igmp"},
	{keyword: "num_grat_arp", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_NUM_PEER_NOTIF, arg: bondArgUint, invarg: "invalid num_grat_arp|num_unsol_na"},
	{keyword: "num_unsol_na", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_NUM_PEER_NOTIF, arg: bondArgUint, invarg: "invalid num_grat_arp|num_unsol_na"},
	{keyword: "all_slaves_active", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_ALL_SLAVES_ACTIVE, arg: bondArgUint, invarg: "invalid all_slaves_active"},
	{keyword: "min_links", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_MIN_LINKS, arg: bondArgUint, invarg: "invalid min_links"},
	{keyword: "lp_interval", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_LP_INTERVAL, arg: bondArgUint, invarg: "invalid lp_interval"},
	{keyword: "packets_per_slave", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_PACKETS_PER_SLAVE, arg: bondArgUint, invarg: "invalid packets_per_slave"},
	{keyword: "lacp_rate", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_AD_LACP_RATE, arg: bondArgIndex, invarg: "invalid lacp_rate"},
	{keyword: "lacp_active", match: xtcpnl.BondMatchExact, id: unix.IFLA_BOND_AD_LACP_ACTIVE, arg: bondArgIndex, invarg: "invalid lacp_active"},
	{keyword: "coupled_control", match: xtcpnl.BondMatchExact, id: unix.IFLA_BOND_COUPLED_CONTROL, arg: bondArgOnOff},
	{keyword: "broadcast_neighbor", match: xtcpnl.BondMatchExact, id: xtcpnl.IflaBondBroadcastNeigh, arg: bondArgOnOff},
	{keyword: "ad_select", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_AD_SELECT, arg: bondArgIndex, invarg: "invalid ad_select"},
	{keyword: "ad_user_port_key", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_AD_USER_PORT_KEY, arg: bondArgUint, invarg: "invalid ad_user_port_key"},
	{keyword: "ad_actor_sys_prio", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_AD_ACTOR_SYS_PRIO, arg: bondArgUint, invarg: "invalid ad_actor_sys_prio"},
	{keyword: "ad_actor_system", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_AD_ACTOR_SYSTEM, arg: bondArgLLAddr},
	{keyword: "tlb_dynamic_lb", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_TLB_DYNAMIC_LB, arg: bondArgUint, invarg: "invalid tlb_dynamic_lb"},
	{keyword: bondHelpKeywordCst, match: xtcpnl.BondMatchPrefix, arg: bondArgHelp},
}

// bondSlaveParseKeywords is bond_slave_parse_opt's chain. Its fallback arm
// tests `help` with matches() before printing "unknown option", which is the
// same as a last help row.
var bondSlaveParseKeywords = []bondKeyword{
	{keyword: "queue_id", match: xtcpnl.BondMatchPrefix, id: unix.IFLA_BOND_SLAVE_QUEUE_ID, arg: bondArgUint, invarg: "queue_id is invalid"},
	{keyword: "prio", match: xtcpnl.BondMatchExact, id: unix.IFLA_BOND_SLAVE_PRIO, arg: bondArgS32, invarg: "prio is invalid"},
	{keyword: "actor_port_prio", match: xtcpnl.BondMatchExact, id: xtcpnl.IflaBondSlaveActorPortPrio, arg: bondArgUint, invarg: "actor prio is invalid"},
	{keyword: bondHelpKeywordCst, match: xtcpnl.BondMatchPrefix, arg: bondArgHelp},
}

// bondResolver is ll_name_to_index for active_slave and primary: 0 means no
// such device. It is a read transaction ahead of the write, so the caller
// decides how it is answered.
type bondResolver func(name string) (uint32, error)

func (k *bondKeyword) matches(arg string) bool {
	if k.match == xtcpnl.BondMatchExact {
		return arg == k.keyword
	}
	return matchesPrefix(arg, k.keyword)
}

func lookupBondKeyword(chain []bondKeyword, arg string) *bondKeyword {
	for i := range chain {
		if chain[i].matches(arg) {
			return &chain[i]
		}
	}
	return nil
}

func specByID(table []xtcpnl.BondOptionSpec, id uint16) *xtcpnl.BondOptionSpec {
	for i := range table {
		if table[i].ID == id {
			return &table[i]
		}
	}
	return nil
}

// parseBondArgs is bond_parse_opt.
func parseBondArgs(args []string, resolve bondResolver) ([]xtcpnl.BondSetting, error) {
	return parseBondChain(args, bondParseKeywords, xtcpnl.BondOptions, resolve, "bond: unknown command")
}

// parseBondSlaveArgs is bond_slave_parse_opt. It resolves no names.
func parseBondSlaveArgs(args []string) ([]xtcpnl.BondSetting, error) {
	return parseBondChain(args, bondSlaveParseKeywords, xtcpnl.BondSlaveOptions, nil, "bond_slave: unknown option")
}

func parseBondChain(args []string, chain []bondKeyword, table []xtcpnl.BondOptionSpec,
	resolve bondResolver, unknown string) ([]xtcpnl.BondSetting, error) {
	var out []xtcpnl.BondSetting
	for i := 0; i < len(args); i++ {
		kw := lookupBondKeyword(chain, args[i])
		if kw == nil {
			return nil, fmt.Errorf("%w: %s %q?", ErrInvalidArgument, unknown, args[i])
		}
		if kw.arg == bondArgHelp {
			return nil, fmt.Errorf("%w: bond help is not implemented by goip", ErrNotImplemented)
		}
		spec := specByID(table, kw.id)
		if spec == nil {
			return nil, fmt.Errorf("goip: bond keyword %q has no table row", kw.keyword)
		}
		s := xtcpnl.BondSetting{Spec: spec}
		var err error
		i, err = readBondValue(args, i, kw, &s, resolve)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, nil
}

// readBondValue fills s from the argument(s) after args[i] and returns the
// index of the last argument consumed.
func readBondValue(args []string, i int, kw *bondKeyword, s *xtcpnl.BondSetting, resolve bondResolver) (int, error) {
	switch kw.arg {
	case bondArgClear:
		return i, nil
	case bondArgTargets:
		// NEXT_ARG_OK: any following argument is the list, even one that
		// looks like a keyword. With none, the nest is empty.
		if i+1 >= len(args) {
			s.Targets = []netip.Addr{}
			return i, nil
		}
		i++
		t, err := parseBondTargets(args[i], s.Spec.Wire)
		s.Targets = t
		return i, err
	default:
		// Every other arm reads one value, below.
	}

	// Every other arm is NEXT_ARG: a missing value ends the command.
	if i+1 >= len(args) {
		return i, fmt.Errorf("%w: Command line is not complete. Try option \"help\"", ErrInvalidArgument)
	}
	i++
	v := args[i]
	invarg := func() error {
		return fmt.Errorf("%w: argument %q is wrong: %s", ErrInvalidArgument, v, kw.invarg)
	}
	switch kw.arg {
	case bondArgIndex:
		n := getIndex(s.Spec.Names, v)
		if n < 0 {
			return i, invarg()
		}
		s.Value = uint32(n)
	case bondArgUint:
		n, ok := readBondUint(v, s.Spec.Wire)
		if !ok {
			return i, invarg()
		}
		s.Value = n
	case bondArgS32:
		n, ok := getS32(v, 0)
		if !ok {
			return i, invarg()
		}
		s.Value = uint32(n) //nolint:gosec // the wire s32, reinterpreted
	case bondArgIfindex:
		idx, err := resolve(v)
		if err != nil {
			return i, err
		}
		if idx == 0 {
			return i, fmt.Errorf("%w: Cannot find device %q", ErrInvalidArgument, v)
		}
		s.Value = idx
	case bondArgOnOff:
		switch v {
		case "off":
			s.Value = 0
		case "on":
			s.Value = 1
		default:
			return i, fmt.Errorf("%w: argument of %q must be \"on\" or \"off\", not %q", ErrInvalidArgument, kw.keyword, v)
		}
	case bondArgLLAddr:
		b, ok := llAddrA2N(v, bondLLAddrBufCst)
		if !ok {
			return i, fmt.Errorf("%w: %q is invalid lladdr", ErrInvalidArgument, v)
		}
		s.Bytes = b
	default:
		return i, fmt.Errorf("goip: bond keyword %q has no value reader", kw.keyword)
	}
	return i, nil
}

func readBondUint(v string, w xtcpnl.BondWire) (uint32, bool) {
	switch w {
	case xtcpnl.BondWireU8:
		n, ok := getU8(v, 0)
		return uint32(n), ok
	case xtcpnl.BondWireU16:
		n, ok := getU16(v, 0)
		return uint32(n), ok
	default:
		return getU32(v, 0)
	}
}

// parseBondTargets is the strtok(targets, ",") loop: empty fields vanish,
// and the loop stops after BOND_MAX_ARP_TARGETS, dropping the rest without
// a word. A bad address is get_addr32's (or get_addr's) exit(1).
func parseBondTargets(list string, w xtcpnl.BondWire) ([]netip.Addr, error) {
	out := []netip.Addr{}
	for _, f := range strings.Split(list, ",") {
		if f == "" {
			continue
		}
		if len(out) == bondMaxTargetsCst {
			break
		}
		var a netip.Addr
		var ok bool
		if w == xtcpnl.BondWireIPv4Set {
			a, ok = getAddrIPv4(f)
			if !ok {
				return nil, fmt.Errorf("%w: an IP address is expected rather than %q", ErrInvalidArgument, f)
			}
		} else {
			a, ok = getAddrIPv6(f)
			if !ok {
				return nil, fmt.Errorf("%w: inet6 address is expected rather than %q", ErrInvalidArgument, f)
			}
		}
		out = append(out, a)
	}
	return out, nil
}

// bondMaxTargetsCst is BOND_MAX_ARP_TARGETS and BOND_MAX_NS_TARGETS, both 16
// (include/net/bonding.h).
const bondMaxTargetsCst = 16
