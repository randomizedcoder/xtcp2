package xtcpnl

import (
	"encoding/binary"
	"net/netip"

	"golang.org/x/sys/unix"
)

// This file decodes the bonding driver's two per-kind blobs: IFLA_INFO_DATA
// on a link whose IFLA_INFO_KIND is "bond", and IFLA_INFO_SLAVE_DATA on a
// link whose IFLA_INFO_SLAVE_KIND is "bond" — every port enslaved to one.
// Phase 1 of docs/netlink/link-write-path.md.
//
// # One table, four users
//
// BondOptions is the single description of the bond's options: keyword,
// IFLA_BOND_* id, wire width, value names and iproute2's match rule. It is
// the decision recorded as link-write-path.md decision 2. The decoder here
// reads only the id and width; the -d renderer (phase 2) reads the names and
// the order, and the argv parser and encoder (phases 5-6) read the keyword
// and the match rule. Four hand-maintained copies of a 34-row table with four
// different wire widths would drift, and the drift would not look like an
// error: `arp_validate` decoded one byte wide reads correctly on every
// little-endian host until somebody sets it to 256.
//
// The table is in print_opt's order (ip/iplink_bond.c bond_print_opt), which
// is not id order: `arp_missed_max` (30) prints after `arp_interval` (7), and
// IFLA_BOND_AD_INFO is printed between `ad_select` and `ad_actor_sys_prio`.
// A renderer walks the table; nothing here depends on the order.

// BondWire is how one bond option is laid out on the wire.
type BondWire uint8

const (
	BondWireU8      BondWire = iota + 1 // nla_put_u8
	BondWireU16                         // nla_put_u16
	BondWireU32                         // nla_put_u32
	BondWireMAC                         // nla_put of ETH_ALEN bytes
	BondWireIPv4Set                     // nest of be32, sub-attribute type = position
	BondWireIPv6Set                     // nest of in6_addr, sub-attribute type = position
	BondWireNest                        // a structured nest with its own parser (AD_INFO)
	BondWireS32                         // nla_put_s32; only the port's prio
)

// BondMatch is how iproute2 matches the keyword on the command line: most
// bond keywords use matches(), a prefix test, and the newer ones use strcmp
// so they cannot steal a prefix an older keyword already answers to
// (iplink_bond.c:260,291,373,380,386).
type BondMatch uint8

const (
	BondMatchPrefix BondMatch = iota + 1
	BondMatchExact
)

// BondOptionSpec is one row of BondOptions.
type BondOptionSpec struct {
	Keyword string
	// Aliases are other keywords that set the same attribute:
	// `num_unsol_na` for `num_grat_arp`, `clear_active_slave` for
	// `active_slave`. The renderer prints Keyword; the parser accepts both.
	Aliases []string
	ID      uint16
	Wire    BondWire
	// Names maps a value to the word iproute2 prints and accepts for it,
	// indexed by value (`mode 4` is Names[4], "802.3ad"). nil for a numeric
	// option. A value past the end prints as its number, as get_name does.
	Names []string
	Match BondMatch
	// ReadOnly marks an attribute the kernel reports and no keyword sets.
	ReadOnly bool
}

// Bond attribute ids x/sys/unix v0.47.0 does not name yet. Values from
// include/uapi/linux/if_link.h, enum { IFLA_BOND_* } and
// enum { IFLA_BOND_SLAVE_* }.
const (
	IflaBondBroadcastNeigh                uint16 = 33
	IflaBondLacpStrict                    uint16 = 34
	IflaBondSlaveActorPortPrio            uint16 = 10
	IflaBondSlaveAdChurnActorState        uint16 = 11
	IflaBondSlaveAdChurnPartnerState      uint16 = 12
	bondKindCst                                  = "bond"
	bondMaxTargetsCst                            = 16 // BOND_MAX_ARP_TARGETS, include/net/bonding.h
	bondEtherAddrLenCst                          = 6
	bondSlavePartnerStateKernelWidthCst          = 2
	bondSlavePartnerStateIproute2WidthCst        = 1
	bondIPv6AddrLenCst                           = 16
	bondIPv4AddrLenCst                           = 4
	bondNoNamesCst                        string = ""
	bondNameNoneCst                              = "none"
)

var (
	bondOnOff = []string{"off", "on"}

	// BondOptions is the bond option table, in bond_print_opt order. See the
	// file comment.
	BondOptions = []BondOptionSpec{
		{Keyword: "mode", ID: unix.IFLA_BOND_MODE, Wire: BondWireU8, Match: BondMatchPrefix,
			Names: []string{"balance-rr", "active-backup", "balance-xor", "broadcast", "802.3ad", "balance-tlb", "balance-alb"}},
		{Keyword: "active_slave", Aliases: []string{"clear_active_slave"}, ID: unix.IFLA_BOND_ACTIVE_SLAVE, Wire: BondWireU32, Match: BondMatchPrefix},
		{Keyword: "miimon", ID: unix.IFLA_BOND_MIIMON, Wire: BondWireU32, Match: BondMatchPrefix},
		{Keyword: "updelay", ID: unix.IFLA_BOND_UPDELAY, Wire: BondWireU32, Match: BondMatchPrefix},
		{Keyword: "downdelay", ID: unix.IFLA_BOND_DOWNDELAY, Wire: BondWireU32, Match: BondMatchPrefix},
		{Keyword: "peer_notify_delay", ID: unix.IFLA_BOND_PEER_NOTIF_DELAY, Wire: BondWireU32, Match: BondMatchPrefix},
		{Keyword: "use_carrier", ID: unix.IFLA_BOND_USE_CARRIER, Wire: BondWireU8, Match: BondMatchPrefix},
		{Keyword: "arp_interval", ID: unix.IFLA_BOND_ARP_INTERVAL, Wire: BondWireU32, Match: BondMatchPrefix},
		{Keyword: "arp_missed_max", ID: unix.IFLA_BOND_MISSED_MAX, Wire: BondWireU8, Match: BondMatchExact},
		{Keyword: "arp_ip_target", ID: unix.IFLA_BOND_ARP_IP_TARGET, Wire: BondWireIPv4Set, Match: BondMatchPrefix},
		{Keyword: "ns_ip6_target", ID: unix.IFLA_BOND_NS_IP6_TARGET, Wire: BondWireIPv6Set, Match: BondMatchExact},
		{Keyword: "arp_validate", ID: unix.IFLA_BOND_ARP_VALIDATE, Wire: BondWireU32, Match: BondMatchPrefix,
			Names: []string{bondNameNoneCst, "active", "backup", "all", "filter", "filter_active", "filter_backup"}},
		{Keyword: "arp_all_targets", ID: unix.IFLA_BOND_ARP_ALL_TARGETS, Wire: BondWireU32, Match: BondMatchPrefix,
			Names: []string{"any", "all"}},
		{Keyword: "primary", ID: unix.IFLA_BOND_PRIMARY, Wire: BondWireU32, Match: BondMatchPrefix},
		{Keyword: "primary_reselect", ID: unix.IFLA_BOND_PRIMARY_RESELECT, Wire: BondWireU8, Match: BondMatchPrefix,
			Names: []string{"always", "better", "failure"}},
		{Keyword: "fail_over_mac", ID: unix.IFLA_BOND_FAIL_OVER_MAC, Wire: BondWireU8, Match: BondMatchPrefix,
			Names: []string{bondNameNoneCst, "active", "follow"}},
		{Keyword: "xmit_hash_policy", ID: unix.IFLA_BOND_XMIT_HASH_POLICY, Wire: BondWireU8, Match: BondMatchPrefix,
			Names: []string{"layer2", "layer3+4", "layer2+3", "encap2+3", "encap3+4", "vlan+srcmac"}},
		{Keyword: "resend_igmp", ID: unix.IFLA_BOND_RESEND_IGMP, Wire: BondWireU32, Match: BondMatchPrefix},
		{Keyword: "num_grat_arp", Aliases: []string{"num_unsol_na"}, ID: unix.IFLA_BOND_NUM_PEER_NOTIF, Wire: BondWireU8, Match: BondMatchPrefix},
		{Keyword: "all_slaves_active", ID: unix.IFLA_BOND_ALL_SLAVES_ACTIVE, Wire: BondWireU8, Match: BondMatchPrefix},
		{Keyword: "lacp_strict", ID: IflaBondLacpStrict, Wire: BondWireU8, Match: BondMatchPrefix, Names: bondOnOff},
		{Keyword: "min_links", ID: unix.IFLA_BOND_MIN_LINKS, Wire: BondWireU32, Match: BondMatchPrefix},
		{Keyword: "lp_interval", ID: unix.IFLA_BOND_LP_INTERVAL, Wire: BondWireU32, Match: BondMatchPrefix},
		{Keyword: "packets_per_slave", ID: unix.IFLA_BOND_PACKETS_PER_SLAVE, Wire: BondWireU32, Match: BondMatchPrefix},
		{Keyword: "lacp_active", ID: unix.IFLA_BOND_AD_LACP_ACTIVE, Wire: BondWireU8, Match: BondMatchExact, Names: bondOnOff},
		{Keyword: "lacp_rate", ID: unix.IFLA_BOND_AD_LACP_RATE, Wire: BondWireU8, Match: BondMatchPrefix,
			Names: []string{"slow", "fast"}},
		{Keyword: "coupled_control", ID: unix.IFLA_BOND_COUPLED_CONTROL, Wire: BondWireU8, Match: BondMatchExact, Names: bondOnOff},
		{Keyword: "broadcast_neighbor", ID: IflaBondBroadcastNeigh, Wire: BondWireU8, Match: BondMatchExact, Names: bondOnOff},
		{Keyword: "ad_select", ID: unix.IFLA_BOND_AD_SELECT, Wire: BondWireU8, Match: BondMatchPrefix,
			Names: []string{"stable", "bandwidth", "count", "actor_port_prio"}},
		{Keyword: "ad_info", ID: unix.IFLA_BOND_AD_INFO, Wire: BondWireNest, ReadOnly: true},
		{Keyword: "ad_actor_sys_prio", ID: unix.IFLA_BOND_AD_ACTOR_SYS_PRIO, Wire: BondWireU16, Match: BondMatchPrefix},
		{Keyword: "ad_user_port_key", ID: unix.IFLA_BOND_AD_USER_PORT_KEY, Wire: BondWireU16, Match: BondMatchPrefix},
		{Keyword: "ad_actor_system", ID: unix.IFLA_BOND_AD_ACTOR_SYSTEM, Wire: BondWireMAC, Match: BondMatchPrefix},
		{Keyword: "tlb_dynamic_lb", ID: unix.IFLA_BOND_TLB_DYNAMIC_LB, Wire: BondWireU8, Match: BondMatchPrefix},
	}

	// BondSlaveOptions are the three port options a keyword sets, in
	// bond_slave_parse_opt order (ip/iplink_bond_slave.c). The port's other
	// nine attributes are read-only and decoded into BondSlaveInfo's named
	// fields; this table exists for the write path, so the encoder reads the
	// port's options the way it reads the bond's.
	BondSlaveOptions = []BondOptionSpec{
		{Keyword: "queue_id", ID: unix.IFLA_BOND_SLAVE_QUEUE_ID, Wire: BondWireU16, Match: BondMatchPrefix},
		{Keyword: "prio", ID: unix.IFLA_BOND_SLAVE_PRIO, Wire: BondWireS32, Match: BondMatchExact},
		{Keyword: "actor_port_prio", ID: IflaBondSlaveActorPortPrio, Wire: BondWireU16, Match: BondMatchExact},
	}

	// bondOptionByID indexes BondOptions for the decoder.
	bondOptionByID = indexBondOptions(BondOptions)
)

func indexBondOptions(opts []BondOptionSpec) map[uint16]*BondOptionSpec {
	m := make(map[uint16]*BondOptionSpec, len(opts))
	for i := range opts {
		m[opts[i].ID] = &opts[i]
	}
	return m
}

// BondOptionSpecByID returns the table row for one IFLA_BOND_* id, or nil for
// an id the table does not know — an attribute newer than this file.
func BondOptionSpecByID(id uint16) *BondOptionSpec {
	return bondOptionByID[id]
}

// ValueName is the word iproute2 uses for v, or "" when the option is
// numeric or v is past the end of the name table (get_name returns NULL and
// the caller prints the number).
func (s *BondOptionSpec) ValueName(v uint32) string {
	if s == nil || uint64(v) >= uint64(len(s.Names)) {
		return bondNoNamesCst
	}
	return s.Names[v]
}

// U8Attr and U16Attr are U32Attr's narrower siblings: a value plus whether
// the attribute was there at all. The bond port attributes need the
// distinction because `state 0` is ACTIVE and `mii_status 0` is UP — the
// commonest values are zero, and absent is not the same answer.
type U8Attr struct {
	Value   uint8
	Present bool
}

func (a *U8Attr) setU8(val []byte) {
	if len(val) < 1 {
		return
	}
	a.Value = val[0]
	a.Present = true
}

type U16Attr struct {
	Value   uint16
	Present bool
}

func (a *U16Attr) setU16(val []byte) {
	if len(val) < 2 {
		return
	}
	a.Value = binary.LittleEndian.Uint16(val[0:2])
	a.Present = true
}

// BondValue is one decoded scalar bond option. U holds every integer width
// (the table says which it was on the wire); Bytes holds a MAC.
type BondValue struct {
	U       uint32
	Bytes   []byte
	Present bool
}

// BondInfo is IFLA_INFO_DATA for kind "bond", decoded.
type BondInfo struct {
	// values holds every scalar and MAC option, by IFLA_BOND_* id. A map
	// rather than 32 named fields because the table is what gives each id
	// its meaning, and a second, named copy of that meaning is exactly the
	// drift BondOptions exists to prevent. Read it with Value.
	values map[uint16]BondValue

	// ArpIPTargets and NsIP6Targets are the two target lists, in position
	// order. bond_fill_info emits a list only when it is non-empty, so nil
	// is both "absent" and "empty" - the kernel does not distinguish them
	// either.
	ArpIPTargets []netip.Addr
	NsIP6Targets []netip.Addr

	// AdInfo is IFLA_BOND_AD_INFO, which bond_fill_info emits only in
	// 802.3ad mode with an active aggregator (bond_netlink.c:884-909). nil
	// when absent. A partnerless bond still has one: the 7_1_4
	// lacp_no_partner capture carries AD_INFO with partner MAC zero and
	// partner key 1, so "no partner" is a value here, not a nil.
	AdInfo *BondAdInfo

	// Unknown lists attribute ids the table does not know, in wire order,
	// so a kernel newer than BondOptions is visible rather than silent.
	Unknown []uint16
}

// Value returns the decoded option id, absent if the kernel did not send it.
func (b *BondInfo) Value(id uint16) BondValue {
	if b == nil {
		return BondValue{}
	}
	return b.values[id]
}

// BondAdInfo is the IFLA_BOND_AD_INFO nest: the bond's active aggregator.
type BondAdInfo struct {
	Aggregator U16Attr
	NumPorts   U16Attr
	ActorKey   U16Attr
	PartnerKey U16Attr
	PartnerMAC []byte
}

// BondSlaveInfo is IFLA_INFO_SLAVE_DATA for slave kind "bond": one port.
// bond_fill_slave_info (bond_netlink.c:37-104) sends the first six always
// and the AD_* five only for an 802.3ad port that has an aggregator; ACTOR_PORT_PRIO
// is sent for every 802.3ad port.
//
// These are named fields, not a table: the port has twelve attributes, only
// three of which a keyword can set (queue_id, prio, actor_port_prio), and
// the decode is the only user of the other nine.
type BondSlaveInfo struct {
	State            U8Attr // BOND_STATE_ACTIVE 0, BOND_STATE_BACKUP 1
	MiiStatus        U8Attr // BOND_LINK_UP 0, _FAIL 1, _DOWN 2, _BACK 3
	LinkFailureCount U32Attr
	PermHwaddr       []byte
	QueueID          U16Attr
	Prio             U32Attr // s32 on the wire; PrioInt32 for the signed value
	AdAggregatorID   U16Attr
	AdActorOperState U8Attr

	// AdPartnerOperState is decoded at the KERNEL's width. The kernel puts
	// IFLA_BOND_SLAVE_AD_PARTNER_OPER_PORT_STATE as a u16 (bond_netlink.c:79)
	// and iproute2 reads it as a u8 (iplink_bond_slave.c:148), which on a
	// little-endian host prints the right number by luck and on a big-endian
	// one prints 0 (docs/netlink/upstream/iproute2-bond-partner-state-width.md).
	// The LACP port state is an octet, so the high byte is zero on every
	// kernel seen; a renderer matching `ip` prints the low byte, and the
	// decode keeps both so a test can tell the two apart.
	//
	// A one-byte attribute is accepted too, as the octet itself - nothing
	// sends one today, but the field means the same either way.
	AdPartnerOperState U16Attr

	AdChurnActorState   U8Attr // AD_CHURN_MONITOR 0, AD_CHURN 1, AD_NO_CHURN 2
	AdChurnPartnerState U8Attr
	ActorPortPrio       U16Attr
}

// PrioInt32 is Prio as the s32 the kernel sent.
func (s *BondSlaveInfo) PrioInt32() int32 {
	return int32(s.Prio.Value) //nolint:gosec // reinterpreting the wire s32, not converting a magnitude
}

// ParseBondInfoData decodes IFLA_INFO_DATA for kind "bond". Tolerant in the
// way walkNestTolerant documents: a malformed attribute ends the walk and
// keeps what came before it.
func ParseBondInfoData(nest []byte) *BondInfo {
	if nest == nil {
		return nil
	}
	b := &BondInfo{values: make(map[uint16]BondValue)}
	walkNestTolerant(nest, func(atype uint16, val []byte) {
		spec := bondOptionByID[atype]
		if spec == nil {
			b.Unknown = append(b.Unknown, atype)
			return
		}
		switch spec.Wire {
		case BondWireU8, BondWireU16, BondWireU32:
			if v, ok := bondScalar(spec.Wire, val); ok {
				b.values[atype] = BondValue{U: v, Present: true}
			}
		case BondWireMAC:
			if len(val) >= bondEtherAddrLenCst {
				b.values[atype] = BondValue{Bytes: CopyBytes(val[:bondEtherAddrLenCst]), Present: true}
			}
		case BondWireIPv4Set:
			b.ArpIPTargets = bondTargets(val, bondIPv4AddrLenCst)
		case BondWireIPv6Set:
			b.NsIP6Targets = bondTargets(val, bondIPv6AddrLenCst)
		case BondWireNest:
			b.AdInfo = ParseBondAdInfo(val)
		default:
			// BondWireS32 is a port option; BondOptions has no such row.
		}
	})
	return b
}

// bondScalar reads one integer option at the width the table gives it. A
// short attribute is absent, the U32Attr rule.
func bondScalar(w BondWire, val []byte) (uint32, bool) {
	switch w {
	case BondWireU8:
		if len(val) >= 1 {
			return uint32(val[0]), true
		}
	case BondWireU16:
		if len(val) >= 2 {
			return uint32(binary.LittleEndian.Uint16(val)), true
		}
	case BondWireU32:
		if len(val) >= 4 {
			return binary.LittleEndian.Uint32(val), true
		}
	default:
		// Not a scalar the bond's own options use.
	}
	return 0, false
}

// bondTargets decodes a target-list nest. Each entry's sub-attribute TYPE is
// its position (bond_netlink.c:745-755, nla_put_be32(skb, i, t)), so the
// list is rebuilt in position order rather than wire order; the two agree on
// every kernel, but the position is what the type means. Entries of the
// wrong size, and positions past BOND_MAX_ARP_TARGETS, are skipped.
func bondTargets(nest []byte, alen int) []netip.Addr {
	var slots [bondMaxTargetsCst]netip.Addr
	n := 0
	walkNestTolerant(nest, func(pos uint16, val []byte) {
		if int(pos) >= bondMaxTargetsCst || len(val) != alen {
			return
		}
		a, ok := netip.AddrFromSlice(val)
		if !ok {
			return
		}
		slots[pos] = a
		if int(pos)+1 > n {
			n = int(pos) + 1
		}
	})
	var out []netip.Addr
	for i := 0; i < n; i++ {
		if slots[i].IsValid() {
			out = append(out, slots[i])
		}
	}
	return out
}

// ParseBondAdInfo decodes the IFLA_BOND_AD_INFO nest. nil for a nil nest;
// an empty or malformed one yields a BondAdInfo whose fields are absent, so
// "the kernel sent AD_INFO" stays distinguishable from "it did not".
func ParseBondAdInfo(nest []byte) *BondAdInfo {
	if nest == nil {
		return nil
	}
	ai := &BondAdInfo{}
	walkNestTolerant(nest, func(atype uint16, val []byte) {
		switch atype {
		case unix.IFLA_BOND_AD_INFO_AGGREGATOR:
			ai.Aggregator.setU16(val)
		case unix.IFLA_BOND_AD_INFO_NUM_PORTS:
			ai.NumPorts.setU16(val)
		case unix.IFLA_BOND_AD_INFO_ACTOR_KEY:
			ai.ActorKey.setU16(val)
		case unix.IFLA_BOND_AD_INFO_PARTNER_KEY:
			ai.PartnerKey.setU16(val)
		case unix.IFLA_BOND_AD_INFO_PARTNER_MAC:
			if len(val) >= bondEtherAddrLenCst {
				ai.PartnerMAC = CopyBytes(val[:bondEtherAddrLenCst])
			}
		}
	})
	return ai
}

// ParseBondSlaveData decodes IFLA_INFO_SLAVE_DATA for slave kind "bond".
func ParseBondSlaveData(nest []byte) *BondSlaveInfo {
	if nest == nil {
		return nil
	}
	s := &BondSlaveInfo{}
	walkNestTolerant(nest, func(atype uint16, val []byte) {
		switch atype {
		case unix.IFLA_BOND_SLAVE_STATE:
			s.State.setU8(val)
		case unix.IFLA_BOND_SLAVE_MII_STATUS:
			s.MiiStatus.setU8(val)
		case unix.IFLA_BOND_SLAVE_LINK_FAILURE_COUNT:
			s.LinkFailureCount.setU32(val)
		case unix.IFLA_BOND_SLAVE_PERM_HWADDR:
			s.PermHwaddr = CopyBytes(val)
		case unix.IFLA_BOND_SLAVE_QUEUE_ID:
			s.QueueID.setU16(val)
		case unix.IFLA_BOND_SLAVE_PRIO:
			s.Prio.setU32(val)
		case unix.IFLA_BOND_SLAVE_AD_AGGREGATOR_ID:
			s.AdAggregatorID.setU16(val)
		case unix.IFLA_BOND_SLAVE_AD_ACTOR_OPER_PORT_STATE:
			s.AdActorOperState.setU8(val)
		case unix.IFLA_BOND_SLAVE_AD_PARTNER_OPER_PORT_STATE:
			setPartnerOperState(&s.AdPartnerOperState, val)
		case IflaBondSlaveAdChurnActorState:
			s.AdChurnActorState.setU8(val)
		case IflaBondSlaveAdChurnPartnerState:
			s.AdChurnPartnerState.setU8(val)
		case IflaBondSlaveActorPortPrio:
			s.ActorPortPrio.setU16(val)
		}
	})
	return s
}

// setPartnerOperState reads the partner port state at the kernel's u16 width,
// or as the bare octet from a one-byte attribute. See
// BondSlaveInfo.AdPartnerOperState.
func setPartnerOperState(a *U16Attr, val []byte) {
	switch {
	case len(val) >= bondSlavePartnerStateKernelWidthCst:
		a.setU16(val)
	case len(val) == bondSlavePartnerStateIproute2WidthCst:
		a.Value = uint16(val[0])
		a.Present = true
	}
}
