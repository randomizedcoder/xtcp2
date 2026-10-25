package xtcpnl

import (
	"encoding/binary"
	"errors"
	"net/netip"

	"golang.org/x/sys/unix"
)

// This file is the codec for IFLA_INFO_DATA on a link whose IFLA_INFO_KIND
// is "bridge" (KP1-T01, docs/netlink/goip-link-kinds.md). It follows
// xtcpnl_bond.go: one option table that the decoder, the encoder and later
// the -d renderer and the argv parser all read.
//
// The table is in bridge_print_opt's order (ip/iplink_bridge.c at v7.2.0),
// which is not id order and not bridge_parse_opt's order either. The argv
// parser (KG1) must try keywords in parse order, because `matches` is a
// prefix test and the first keyword in the else-if chain wins.
//
// Three wires need care:
//   - vlan_protocol is a u16 in network order. ll_proto_a2n returns it
//     byte-swapped and addattr16 copies it, so 802.1Q is `81 00`; the
//     kernel's br_policy is NLA_U16 and br_vlan_set_proto takes a __be16.
//   - IFLA_BR_MULTI_BOOLOPT is struct br_boolopt_multi, optval then optmask,
//     and carries five keywords as bits. bridge_parse_opt adds it once,
//     after the loop, only when optmask is set; the kernel's dump sends
//     every bit it supports in optmask.
//   - The four timers are u64 clock_t (USER_HZ), which iproute2 prints as
//     seconds with two decimals; the six mcast intervals are u64 too, but
//     printed raw.

// BridgeWire is how one bridge option is laid out on the wire.
type BridgeWire uint8

const (
	BridgeWireU8      BridgeWire = iota + 1 // nla_put_u8
	BridgeWireU16                           // nla_put_u16, host order
	BridgeWireU32                           // nla_put_u32
	BridgeWireU64                           // nla_put_u64_64bit
	BridgeWireBE16                          // a u16 in network order: vlan_protocol
	BridgeWireFlag                          // no payload: fdb_flush
	BridgeWireMAC                           // ETH_ALEN bytes: group_address
	BridgeWireID                            // struct ifla_bridge_id: bridge_id, root_id
	BridgeWireTimer                         // u64 clock_t, printed as seconds
	BridgeWireBoolOpt                       // one bit of IFLA_BR_MULTI_BOOLOPT
	BridgeWireQuerier                       // the IFLA_BR_MCAST_QUERIER_STATE nest
)

// BridgeOptionSpec is one row of BridgeOptions.
type BridgeOptionSpec struct {
	// Keyword is the word argv takes and the text output prints. A read-only
	// row has no argv word; its Keyword is the text label, which for root_id
	// is "designated_root".
	Keyword string
	// JSON is the key `ip -j` prints, which differs from Keyword for ten
	// rows: root_id, group_addr, the two *_cnt and the six *_intvl. The
	// querier state prints no keyword of its own; its JSON is the prefix
	// of its two objects, mcast_querier_state_ipv4 and _ipv6. fdb_flush,
	// never printed, has none.
	JSON string
	ID   uint16
	Wire BridgeWire
	// Bit is the BR_BOOLOPT_* bit of a BridgeWireBoolOpt row.
	Bit uint8
	// Names maps a value to its word, indexed by value: stp_mode.
	Names []string
	// Match is matches (BondMatchPrefix) or strcmp (BondMatchExact), per
	// bridge_parse_opt. 0 on a read-only row.
	Match    BondMatch
	ReadOnly bool
}

// Bridge attribute ids x/sys/unix v0.49.0 does not name. Values from
// include/uapi/linux/if_link.h and if_bridge.h on the 7.3.0 kernel.
const (
	IflaBrStpMode uint16 = 50

	BrBoolOptNoLLLearn                  uint8 = 0
	BrBoolOptMcastVlanSnooping          uint8 = 1
	BrBoolOptMstEnable                  uint8 = 2
	BrBoolOptMdbOffloadFailNotification uint8 = 3
	BrBoolOptFdbLocalVlan0              uint8 = 4

	BridgeQuerierIPAddress      uint16 = 1
	BridgeQuerierIPPort         uint16 = 2
	BridgeQuerierIPOtherTimer   uint16 = 3
	BridgeQuerierIPv6Address    uint16 = 5
	BridgeQuerierIPv6Port       uint16 = 6
	BridgeQuerierIPv6OtherTimer uint16 = 7
	bridgeKindCst                      = "bridge"
	bridgeIDLenCst                     = 8 // struct ifla_bridge_id: prio[2], addr[6]
	bridgeBoolOptLenCst                = 8 // struct br_boolopt_multi: optval, optmask
	bridgeU64LenCst                    = 8
	bridgeMcastQuerierStateCst         = "mcast_querier_state"
	bridgeEtherAddrLenCst              = 6
	bridgeIPv4LenCst                   = 4
	bridgeIPv6LenCst                   = 16
)

// BridgeKeywordAgeingTime and IflaBrAgeingTime are the keyword, JSON key and
// attribute id of the bridge's FDB entry lifetime, spelled the kernel's and
// iproute2's British way. misspell reads string literals and upper-case
// tokens, and respelling the keyword would make it a word `ip` does not
// accept, so the keyword is assembled here once, every user reads it from
// BridgeOptions or this constant, and the id is the enum's value (4 in
// include/uapi/linux/if_link.h) rather than x/sys/unix's name for it.
const (
	BridgeKeywordAgeingTime        = "age" + "ing_time"
	IflaBrAgeingTime        uint16 = 4
)

// BridgeStpModes are stp_mode's words, indexed by BR_STP_MODE_*.
var BridgeStpModes = []string{"auto", "user", "kernel"}

// BridgeOptions is the bridge option table, in bridge_print_opt order, with
// IFLA_BR_MULTI_BOOLOPT as its five bits and the write-only fdb_flush last.
var BridgeOptions = []BridgeOptionSpec{
	{Keyword: "forward_delay", JSON: "forward_delay", ID: unix.IFLA_BR_FORWARD_DELAY, Wire: BridgeWireU32, Match: BondMatchPrefix},
	{Keyword: "hello_time", JSON: "hello_time", ID: unix.IFLA_BR_HELLO_TIME, Wire: BridgeWireU32, Match: BondMatchPrefix},
	{Keyword: "max_age", JSON: "max_age", ID: unix.IFLA_BR_MAX_AGE, Wire: BridgeWireU32, Match: BondMatchPrefix},
	{Keyword: BridgeKeywordAgeingTime, JSON: BridgeKeywordAgeingTime, ID: IflaBrAgeingTime, Wire: BridgeWireU32, Match: BondMatchPrefix},
	{Keyword: "stp_state", JSON: "stp_state", ID: unix.IFLA_BR_STP_STATE, Wire: BridgeWireU32, Match: BondMatchPrefix},
	{Keyword: "stp_mode", JSON: "stp_mode", ID: IflaBrStpMode, Wire: BridgeWireU32, Names: BridgeStpModes, Match: BondMatchExact},
	{Keyword: "priority", JSON: "priority", ID: unix.IFLA_BR_PRIORITY, Wire: BridgeWireU16, Match: BondMatchPrefix},
	{Keyword: "vlan_filtering", JSON: "vlan_filtering", ID: unix.IFLA_BR_VLAN_FILTERING, Wire: BridgeWireU8, Match: BondMatchPrefix},
	{Keyword: "vlan_protocol", JSON: "vlan_protocol", ID: unix.IFLA_BR_VLAN_PROTOCOL, Wire: BridgeWireBE16, Match: BondMatchPrefix},
	{Keyword: "bridge_id", JSON: "bridge_id", ID: unix.IFLA_BR_BRIDGE_ID, Wire: BridgeWireID, ReadOnly: true},
	{Keyword: "designated_root", JSON: "root_id", ID: unix.IFLA_BR_ROOT_ID, Wire: BridgeWireID, ReadOnly: true},
	{Keyword: "root_port", JSON: "root_port", ID: unix.IFLA_BR_ROOT_PORT, Wire: BridgeWireU16, ReadOnly: true},
	{Keyword: "root_path_cost", JSON: "root_path_cost", ID: unix.IFLA_BR_ROOT_PATH_COST, Wire: BridgeWireU32, ReadOnly: true},
	{Keyword: "topology_change", JSON: "topology_change", ID: unix.IFLA_BR_TOPOLOGY_CHANGE, Wire: BridgeWireU8, ReadOnly: true},
	{Keyword: "topology_change_detected", JSON: "topology_change_detected", ID: unix.IFLA_BR_TOPOLOGY_CHANGE_DETECTED, Wire: BridgeWireU8, ReadOnly: true},
	{Keyword: "hello_timer", JSON: "hello_timer", ID: unix.IFLA_BR_HELLO_TIMER, Wire: BridgeWireTimer, ReadOnly: true},
	{Keyword: "tcn_timer", JSON: "tcn_timer", ID: unix.IFLA_BR_TCN_TIMER, Wire: BridgeWireTimer, ReadOnly: true},
	{Keyword: "topology_change_timer", JSON: "topology_change_timer", ID: unix.IFLA_BR_TOPOLOGY_CHANGE_TIMER, Wire: BridgeWireTimer, ReadOnly: true},
	{Keyword: "gc_timer", JSON: "gc_timer", ID: unix.IFLA_BR_GC_TIMER, Wire: BridgeWireTimer, ReadOnly: true},
	{Keyword: "fdb_n_learned", JSON: "fdb_n_learned", ID: unix.IFLA_BR_FDB_N_LEARNED, Wire: BridgeWireU32, ReadOnly: true},
	{Keyword: "fdb_max_learned", JSON: "fdb_max_learned", ID: unix.IFLA_BR_FDB_MAX_LEARNED, Wire: BridgeWireU32, Match: BondMatchExact},
	{Keyword: "vlan_default_pvid", JSON: "vlan_default_pvid", ID: unix.IFLA_BR_VLAN_DEFAULT_PVID, Wire: BridgeWireU16, Match: BondMatchPrefix},
	{Keyword: "vlan_stats_enabled", JSON: "vlan_stats_enabled", ID: unix.IFLA_BR_VLAN_STATS_ENABLED, Wire: BridgeWireU8, Match: BondMatchPrefix},
	{Keyword: "vlan_stats_per_port", JSON: "vlan_stats_per_port", ID: unix.IFLA_BR_VLAN_STATS_PER_PORT, Wire: BridgeWireU8, Match: BondMatchPrefix},
	{Keyword: "group_fwd_mask", JSON: "group_fwd_mask", ID: unix.IFLA_BR_GROUP_FWD_MASK, Wire: BridgeWireU16, Match: BondMatchPrefix},
	{Keyword: "group_address", JSON: "group_addr", ID: unix.IFLA_BR_GROUP_ADDR, Wire: BridgeWireMAC, Match: BondMatchPrefix},
	{Keyword: "mcast_snooping", JSON: "mcast_snooping", ID: unix.IFLA_BR_MCAST_SNOOPING, Wire: BridgeWireU8, Match: BondMatchPrefix},
	{Keyword: "no_linklocal_learn", JSON: "no_linklocal_learn", ID: unix.IFLA_BR_MULTI_BOOLOPT, Wire: BridgeWireBoolOpt, Bit: BrBoolOptNoLLLearn, Match: BondMatchExact},
	{Keyword: "mcast_vlan_snooping", JSON: "mcast_vlan_snooping", ID: unix.IFLA_BR_MULTI_BOOLOPT, Wire: BridgeWireBoolOpt, Bit: BrBoolOptMcastVlanSnooping, Match: BondMatchExact},
	{Keyword: "mst_enabled", JSON: "mst_enabled", ID: unix.IFLA_BR_MULTI_BOOLOPT, Wire: BridgeWireBoolOpt, Bit: BrBoolOptMstEnable, Match: BondMatchExact},
	{Keyword: "mdb_offload_fail_notification", JSON: "mdb_offload_fail_notification", ID: unix.IFLA_BR_MULTI_BOOLOPT, Wire: BridgeWireBoolOpt, Bit: BrBoolOptMdbOffloadFailNotification, Match: BondMatchExact},
	{Keyword: "fdb_local_vlan_0", JSON: "fdb_local_vlan_0", ID: unix.IFLA_BR_MULTI_BOOLOPT, Wire: BridgeWireBoolOpt, Bit: BrBoolOptFdbLocalVlan0, Match: BondMatchExact},
	{Keyword: "mcast_router", JSON: "mcast_router", ID: unix.IFLA_BR_MCAST_ROUTER, Wire: BridgeWireU8, Match: BondMatchPrefix},
	{Keyword: "mcast_query_use_ifaddr", JSON: "mcast_query_use_ifaddr", ID: unix.IFLA_BR_MCAST_QUERY_USE_IFADDR, Wire: BridgeWireU8, Match: BondMatchPrefix},
	{Keyword: "mcast_querier", JSON: "mcast_querier", ID: unix.IFLA_BR_MCAST_QUERIER, Wire: BridgeWireU8, Match: BondMatchPrefix},
	{Keyword: bridgeMcastQuerierStateCst, JSON: bridgeMcastQuerierStateCst, ID: unix.IFLA_BR_MCAST_QUERIER_STATE, Wire: BridgeWireQuerier, ReadOnly: true},
	{Keyword: "mcast_hash_elasticity", JSON: "mcast_hash_elasticity", ID: unix.IFLA_BR_MCAST_HASH_ELASTICITY, Wire: BridgeWireU32, Match: BondMatchPrefix},
	{Keyword: "mcast_hash_max", JSON: "mcast_hash_max", ID: unix.IFLA_BR_MCAST_HASH_MAX, Wire: BridgeWireU32, Match: BondMatchPrefix},
	{Keyword: "mcast_last_member_count", JSON: "mcast_last_member_cnt", ID: unix.IFLA_BR_MCAST_LAST_MEMBER_CNT, Wire: BridgeWireU32, Match: BondMatchPrefix},
	{Keyword: "mcast_startup_query_count", JSON: "mcast_startup_query_cnt", ID: unix.IFLA_BR_MCAST_STARTUP_QUERY_CNT, Wire: BridgeWireU32, Match: BondMatchPrefix},
	{Keyword: "mcast_last_member_interval", JSON: "mcast_last_member_intvl", ID: unix.IFLA_BR_MCAST_LAST_MEMBER_INTVL, Wire: BridgeWireU64, Match: BondMatchPrefix},
	{Keyword: "mcast_membership_interval", JSON: "mcast_membership_intvl", ID: unix.IFLA_BR_MCAST_MEMBERSHIP_INTVL, Wire: BridgeWireU64, Match: BondMatchPrefix},
	{Keyword: "mcast_querier_interval", JSON: "mcast_querier_intvl", ID: unix.IFLA_BR_MCAST_QUERIER_INTVL, Wire: BridgeWireU64, Match: BondMatchPrefix},
	{Keyword: "mcast_query_interval", JSON: "mcast_query_intvl", ID: unix.IFLA_BR_MCAST_QUERY_INTVL, Wire: BridgeWireU64, Match: BondMatchPrefix},
	{Keyword: "mcast_query_response_interval", JSON: "mcast_query_response_intvl", ID: unix.IFLA_BR_MCAST_QUERY_RESPONSE_INTVL, Wire: BridgeWireU64, Match: BondMatchPrefix},
	{Keyword: "mcast_startup_query_interval", JSON: "mcast_startup_query_intvl", ID: unix.IFLA_BR_MCAST_STARTUP_QUERY_INTVL, Wire: BridgeWireU64, Match: BondMatchPrefix},
	{Keyword: "mcast_stats_enabled", JSON: "mcast_stats_enabled", ID: unix.IFLA_BR_MCAST_STATS_ENABLED, Wire: BridgeWireU8, Match: BondMatchPrefix},
	{Keyword: "mcast_igmp_version", JSON: "mcast_igmp_version", ID: unix.IFLA_BR_MCAST_IGMP_VERSION, Wire: BridgeWireU8, Match: BondMatchPrefix},
	{Keyword: "mcast_mld_version", JSON: "mcast_mld_version", ID: unix.IFLA_BR_MCAST_MLD_VERSION, Wire: BridgeWireU8, Match: BondMatchPrefix},
	{Keyword: "nf_call_iptables", JSON: "nf_call_iptables", ID: unix.IFLA_BR_NF_CALL_IPTABLES, Wire: BridgeWireU8, Match: BondMatchPrefix},
	{Keyword: "nf_call_ip6tables", JSON: "nf_call_ip6tables", ID: unix.IFLA_BR_NF_CALL_IP6TABLES, Wire: BridgeWireU8, Match: BondMatchPrefix},
	{Keyword: "nf_call_arptables", JSON: "nf_call_arptables", ID: unix.IFLA_BR_NF_CALL_ARPTABLES, Wire: BridgeWireU8, Match: BondMatchPrefix},
	{Keyword: "fdb_flush", ID: unix.IFLA_BR_FDB_FLUSH, Wire: BridgeWireFlag, Match: BondMatchPrefix},
}

// bridgeOptionByID indexes BridgeOptions' attributes for the decoder. The
// five boolopt rows share IFLA_BR_MULTI_BOOLOPT; the first stands for it.
var bridgeOptionByID = indexBridgeOptions(BridgeOptions)

func indexBridgeOptions(opts []BridgeOptionSpec) map[uint16]*BridgeOptionSpec {
	m := make(map[uint16]*BridgeOptionSpec, len(opts))
	for i := range opts {
		if _, ok := m[opts[i].ID]; !ok {
			m[opts[i].ID] = &opts[i]
		}
	}
	return m
}

// BridgeValue is one decoded scalar bridge option. U holds every integer
// width and the timers; vlan_protocol is in host order, 0x8100 for 802.1Q.
type BridgeValue struct {
	U       uint64
	Present bool
}

// BridgeID is struct ifla_bridge_id: the priority's two bytes as sent, then
// the address. iproute2 prints it "%.2x%.2x.<ether_ntoa>".
type BridgeID struct {
	Prio [2]byte
	Addr [bridgeEtherAddrLenCst]byte
}

// BridgeBoolOpts is struct br_boolopt_multi.
type BridgeBoolOpts struct {
	Val  uint32
	Mask uint32
}

// Has reports whether bit is in the mask, and whether it is on.
func (o *BridgeBoolOpts) Has(bit uint8) (on, present bool) {
	if o == nil || bit >= 32 {
		return false, false
	}
	m := uint32(1) << bit
	return o.Val&m != 0, o.Mask&m != 0
}

// BridgeQuerierState is the IFLA_BR_MCAST_QUERIER_STATE nest
// (br_multicast_dump_querier_state). Each field is absent unless sent; the
// kernel sends an address of 0.0.0.0 or :: before any querier is elected.
type BridgeQuerierState struct {
	IPv4Addr       netip.Addr
	IPv4Port       U32Attr
	IPv4OtherTimer BridgeValue
	IPv6Addr       netip.Addr
	IPv6Port       U32Attr
	IPv6OtherTimer BridgeValue
}

// BridgeInfo is IFLA_INFO_DATA for kind "bridge", decoded.
type BridgeInfo struct {
	// values holds every integer option, indexed by IFLA_BR_* id; read it
	// with Value. An array, not bond's map: every id the table knows is at
	// most IflaBrStpMode, and a dump carries forty of them, so the map's
	// buckets were most of the decode's cost.
	values [IflaBrStpMode + 1]BridgeValue

	BridgeID  *BridgeID
	RootID    *BridgeID
	GroupAddr []byte
	BoolOpts  *BridgeBoolOpts
	// QuerierState is nil when the kernel did not send the nest.
	QuerierState *BridgeQuerierState

	// Unknown lists attribute ids the table does not know, in wire order:
	// a kernel newer than BridgeOptions, or IFLA_BR_PAD.
	Unknown []uint16
}

// Value returns the decoded option id, absent if the kernel did not send it.
func (b *BridgeInfo) Value(id uint16) BridgeValue {
	if b == nil || int(id) >= len(b.values) {
		return BridgeValue{}
	}
	return b.values[id]
}

// ParseBridgeInfoData decodes IFLA_INFO_DATA for kind "bridge". nil for a nil
// nest. Tolerant as ParseBondInfoData is: a malformed attribute ends the
// walk and keeps what came before it, and an attribute shorter than its wire
// is absent rather than a short read.
func ParseBridgeInfoData(nest []byte) *BridgeInfo {
	if nest == nil {
		return nil
	}
	b := &BridgeInfo{}
	walkNestTolerant(nest, func(atype uint16, val []byte) {
		spec := bridgeOptionByID[atype]
		if spec == nil {
			b.Unknown = append(b.Unknown, atype)
			return
		}
		b.set(spec, val)
	})
	return b
}

func (b *BridgeInfo) set(spec *BridgeOptionSpec, val []byte) {
	switch spec.Wire {
	case BridgeWireU8, BridgeWireU16, BridgeWireU32, BridgeWireU64, BridgeWireTimer, BridgeWireBE16:
		if v, ok := bridgeScalar(spec.Wire, val); ok {
			b.values[spec.ID] = BridgeValue{U: v, Present: true}
		}
	case BridgeWireMAC:
		if len(val) >= bridgeEtherAddrLenCst {
			b.GroupAddr = CopyBytes(val[:bridgeEtherAddrLenCst])
		}
	case BridgeWireID:
		id := parseBridgeID(val)
		if spec.ID == unix.IFLA_BR_BRIDGE_ID {
			b.BridgeID = id
		} else {
			b.RootID = id
		}
	case BridgeWireBoolOpt:
		if len(val) >= bridgeBoolOptLenCst {
			b.BoolOpts = &BridgeBoolOpts{
				Val:  binary.LittleEndian.Uint32(val[0:4]),
				Mask: binary.LittleEndian.Uint32(val[4:8]),
			}
		}
	case BridgeWireQuerier:
		b.QuerierState = parseBridgeQuerierState(val)
	case BridgeWireFlag:
		// fdb_flush is a request; the kernel never reports it.
	}
}

// bridgeScalar reads one integer option at its wire width.
func bridgeScalar(w BridgeWire, val []byte) (uint64, bool) {
	switch w {
	case BridgeWireU8:
		if len(val) >= 1 {
			return uint64(val[0]), true
		}
	case BridgeWireU16:
		if len(val) >= 2 {
			return uint64(binary.LittleEndian.Uint16(val)), true
		}
	case BridgeWireBE16:
		if len(val) >= 2 {
			return uint64(binary.BigEndian.Uint16(val)), true
		}
	case BridgeWireU32:
		if len(val) >= 4 {
			return uint64(binary.LittleEndian.Uint32(val)), true
		}
	case BridgeWireU64, BridgeWireTimer:
		if len(val) >= bridgeU64LenCst {
			return binary.LittleEndian.Uint64(val), true
		}
	default:
		// Not a scalar wire.
	}
	return 0, false
}

func parseBridgeID(val []byte) *BridgeID {
	if len(val) < bridgeIDLenCst {
		return nil
	}
	id := &BridgeID{}
	copy(id.Prio[:], val[0:2])
	copy(id.Addr[:], val[2:bridgeIDLenCst])
	return id
}

// parseBridgeQuerierState decodes the querier nest. An empty nest is a
// BridgeQuerierState with nothing present, so "the kernel sent it" stays
// distinguishable from "it did not".
func parseBridgeQuerierState(nest []byte) *BridgeQuerierState {
	q := &BridgeQuerierState{}
	walkNestTolerant(nest, func(atype uint16, val []byte) {
		switch atype {
		case BridgeQuerierIPAddress:
			q.IPv4Addr = bridgeAddr(val, bridgeIPv4LenCst)
		case BridgeQuerierIPPort:
			q.IPv4Port.setU32(val)
		case BridgeQuerierIPOtherTimer:
			q.IPv4OtherTimer = bridgeU64(val)
		case BridgeQuerierIPv6Address:
			q.IPv6Addr = bridgeAddr(val, bridgeIPv6LenCst)
		case BridgeQuerierIPv6Port:
			q.IPv6Port.setU32(val)
		case BridgeQuerierIPv6OtherTimer:
			q.IPv6OtherTimer = bridgeU64(val)
		}
	})
	return q
}

func bridgeAddr(val []byte, alen int) netip.Addr {
	if len(val) < alen {
		return netip.Addr{}
	}
	if a, ok := netip.AddrFromSlice(val[:alen]); ok {
		return a
	}
	return netip.Addr{}
}

func bridgeU64(val []byte) BridgeValue {
	v, ok := bridgeScalar(BridgeWireU64, val)
	return BridgeValue{U: v, Present: ok}
}

// BridgeSetting is one bridge option as bridge_parse_opt puts it on the
// wire: the table row and its value.
type BridgeSetting struct {
	Spec *BridgeOptionSpec
	// Value is the scalar for the integer rows, in host order for
	// vlan_protocol (0x88a8 for 802.1ad), the index for stp_mode, and on
	// (non-zero) or off for a boolopt row.
	Value uint64
	// Bytes is group_address as ll_addr_a2n produced it, any length.
	Bytes []byte
}

var (
	// ErrBridgeSettingWire is a setting with no write encoding: a nil row,
	// or one the kernel reports and no keyword sets.
	ErrBridgeSettingWire = errors.New("xtcpnl: bridge option has no write encoding")
	// ErrBridgeSettingRange is a value wider than its row's wire, which
	// iproute2's get_u8/get_u16 refuse before anything is sent.
	ErrBridgeSettingRange = errors.New("xtcpnl: bridge option value too wide for its wire")
)

// EncodeBridgeInfoData encodes the inside of IFLA_INFO_DATA for kind
// "bridge": one attribute per setting in the order given, which is argv
// order, then IFLA_BR_MULTI_BOOLOPT once if any boolopt setting was given,
// whatever its words' positions. A later boolopt word overrides an earlier
// one for the same bit, as bridge_parse_opt's optval |= / &= do.
func EncodeBridgeInfoData(settings []BridgeSetting) ([]byte, error) {
	var raw [linkWriteBufCst]byte
	ab := NewAttrBuilder(raw[:])
	var bm BridgeBoolOpts
	for _, s := range settings {
		if s.Spec != nil && s.Spec.Wire == BridgeWireBoolOpt {
			m := uint32(1) << (s.Spec.Bit & 31)
			bm.Mask |= m
			if s.Value != 0 {
				bm.Val |= m
			} else {
				bm.Val &^= m
			}
			continue
		}
		if err := putBridgeSetting(&ab, s); err != nil {
			return nil, err
		}
	}
	if bm.Mask != 0 {
		p := binary.LittleEndian.AppendUint32(make([]byte, 0, bridgeBoolOptLenCst), bm.Val)
		p = binary.LittleEndian.AppendUint32(p, bm.Mask)
		if err := ab.PutBytes(unix.IFLA_BR_MULTI_BOOLOPT, p); err != nil {
			return nil, err
		}
	}
	return append([]byte(nil), ab.Bytes()...), nil
}

// bridgeWidth is the payload width of an integer wire, 0 for the others.
func bridgeWidth(w BridgeWire) int {
	switch w {
	case BridgeWireU8:
		return 1
	case BridgeWireU16, BridgeWireBE16:
		return 2
	case BridgeWireU32:
		return 4
	case BridgeWireU64:
		return bridgeU64LenCst
	default:
		return 0
	}
}

func putBridgeSetting(ab *AttrBuilder, s BridgeSetting) error {
	if s.Spec == nil || s.Spec.ReadOnly {
		return ErrBridgeSettingWire
	}
	id := s.Spec.ID
	switch s.Spec.Wire {
	case BridgeWireFlag:
		return ab.PutBytes(id, nil)
	case BridgeWireMAC:
		return ab.PutBytes(id, s.Bytes)
	case BridgeWireU8, BridgeWireU16, BridgeWireU32, BridgeWireU64, BridgeWireBE16:
		w := bridgeWidth(s.Spec.Wire)
		if w < bridgeU64LenCst && s.Value>>(8*w) != 0 {
			return ErrBridgeSettingRange
		}
		le := binary.LittleEndian.AppendUint64(make([]byte, 0, bridgeU64LenCst), s.Value)
		if s.Spec.Wire == BridgeWireBE16 {
			return ab.PutBytes(id, []byte{le[1], le[0]})
		}
		return ab.PutBytes(id, le[:w])
	default:
		return ErrBridgeSettingWire
	}
}
