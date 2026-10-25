package xtcpnl

import (
	"golang.org/x/sys/unix"
)

// This file is the codec for IFLA_INFO_SLAVE_DATA on a link whose
// IFLA_INFO_SLAVE_KIND is "bridge": one port of a bridge (KP1-T02,
// docs/netlink/goip-link-kinds.md). It shares xtcpnl_bridge.go's row type,
// value type and encoder, and has its own table and decoder.
//
// The table is in bridge_slave_print_opt's order (ip/iplink_bridge_slave.c
// at v7.2.0). Its quirks:
//   - Four keywords name an attribute by another name: hairpin is
//     IFLA_BRPORT_MODE, root_block IFLA_BRPORT_PROTECT, flood
//     IFLA_BRPORT_UNICAST_FLOOD, and mcast_fast_leave is IFLA_BRPORT_FAST_LEAVE
//     again, which fastleave already sets. print_opt prints the attribute
//     under both words, the second in text only.
//   - nobackup_port is backup_port with ifindex 0; it has no row, as the
//     argv parser turns it into a backup_port setting of 0.
//   - The kernel sends six attributes 7.2.0 does not print: the two MRP
//     flags and the EHT host and multicast group counters. They are
//     read-only rows with no keyword, so the decoder keeps them and a
//     renderer skips them.

// IflaBrportNeighForwardGrat is IFLA_BRPORT_NEIGH_FORWARD_GRAT, which
// x/sys/unix v0.49.0 does not name (include/uapi/linux/if_link.h).
const IflaBrportNeighForwardGrat uint16 = 45

// BridgePortStates are the state words, indexed by BR_STATE_*. A state past
// the end prints as `state (N)`, "state_index" in JSON.
var BridgePortStates = []string{"disabled", "listening", bridgeKwLearningCst, "forwarding", "blocking"}

// BridgeSlaveOptions is the bridge port option table, in
// bridge_slave_print_opt order, then the write-only fdb_flush and the
// attributes iproute2 does not print.
var BridgeSlaveOptions = []BridgeOptionSpec{
	{Keyword: "state", JSON: "state", ID: unix.IFLA_BRPORT_STATE, Wire: BridgeWireU8, Names: BridgePortStates, Match: BondMatchPrefix},
	{Keyword: bridgeKwPriorityCst, JSON: bridgeKwPriorityCst, ID: unix.IFLA_BRPORT_PRIORITY, Wire: BridgeWireU16, Match: BondMatchPrefix},
	{Keyword: "cost", JSON: "cost", ID: unix.IFLA_BRPORT_COST, Wire: BridgeWireU32, Match: BondMatchPrefix},
	{Keyword: "hairpin", JSON: "hairpin", ID: unix.IFLA_BRPORT_MODE, Wire: BridgeWireOnOff, Match: BondMatchPrefix},
	{Keyword: "guard", JSON: "guard", ID: unix.IFLA_BRPORT_GUARD, Wire: BridgeWireOnOff, Match: BondMatchPrefix},
	{Keyword: "root_block", JSON: "root_block", ID: unix.IFLA_BRPORT_PROTECT, Wire: BridgeWireOnOff, Match: BondMatchPrefix},
	{Keyword: "fastleave", JSON: "fastleave", ID: unix.IFLA_BRPORT_FAST_LEAVE, Wire: BridgeWireOnOff, Match: BondMatchPrefix},
	{Keyword: bridgeKwLearningCst, JSON: bridgeKwLearningCst, ID: unix.IFLA_BRPORT_LEARNING, Wire: BridgeWireOnOff, Match: BondMatchPrefix},
	{Keyword: "flood", JSON: "flood", ID: unix.IFLA_BRPORT_UNICAST_FLOOD, Wire: BridgeWireOnOff, Match: BondMatchPrefix},
	{Keyword: "port_id", JSON: "id", ID: unix.IFLA_BRPORT_ID, Wire: BridgeWireU16, Hex: true, ReadOnly: true},
	{Keyword: "port_no", JSON: "no", ID: unix.IFLA_BRPORT_NO, Wire: BridgeWireU16, Hex: true, ReadOnly: true},
	{Keyword: "designated_port", JSON: "designated_port", ID: unix.IFLA_BRPORT_DESIGNATED_PORT, Wire: BridgeWireU16, ReadOnly: true},
	{Keyword: "designated_cost", JSON: "designated_cost", ID: unix.IFLA_BRPORT_DESIGNATED_COST, Wire: BridgeWireU16, ReadOnly: true},
	{Keyword: "designated_bridge", JSON: bridgeKwBridgeIDCst, ID: unix.IFLA_BRPORT_BRIDGE_ID, Wire: BridgeWireID, ReadOnly: true},
	{Keyword: "designated_root", JSON: "root_id", ID: unix.IFLA_BRPORT_ROOT_ID, Wire: BridgeWireID, ReadOnly: true},
	{Keyword: "hold_timer", JSON: "hold_timer", ID: unix.IFLA_BRPORT_HOLD_TIMER, Wire: BridgeWireTimer, ReadOnly: true},
	{Keyword: "message_age_timer", JSON: "message_age_timer", ID: unix.IFLA_BRPORT_MESSAGE_AGE_TIMER, Wire: BridgeWireTimer, ReadOnly: true},
	{Keyword: "forward_delay_timer", JSON: "forward_delay_timer", ID: unix.IFLA_BRPORT_FORWARD_DELAY_TIMER, Wire: BridgeWireTimer, ReadOnly: true},
	{Keyword: "topology_change_ack", JSON: "topology_change_ack", ID: unix.IFLA_BRPORT_TOPOLOGY_CHANGE_ACK, Wire: BridgeWireU8, ReadOnly: true},
	{Keyword: "config_pending", JSON: "config_pending", ID: unix.IFLA_BRPORT_CONFIG_PENDING, Wire: BridgeWireU8, ReadOnly: true},
	{Keyword: devconfProxyArpCst, JSON: devconfProxyArpCst, ID: unix.IFLA_BRPORT_PROXYARP, Wire: BridgeWireOnOff, Match: BondMatchPrefix},
	{Keyword: "proxy_arp_wifi", JSON: "proxy_arp_wifi", ID: unix.IFLA_BRPORT_PROXYARP_WIFI, Wire: BridgeWireOnOff, Match: BondMatchPrefix},
	{Keyword: bridgeKwMcastRouterCst, JSON: "multicast_router", ID: unix.IFLA_BRPORT_MULTICAST_ROUTER, Wire: BridgeWireU8, Match: BondMatchPrefix},
	{Keyword: "mcast_fast_leave", ID: unix.IFLA_BRPORT_FAST_LEAVE, Wire: BridgeWireOnOff, Match: BondMatchPrefix},
	{Keyword: "mcast_flood", JSON: "mcast_flood", ID: unix.IFLA_BRPORT_MCAST_FLOOD, Wire: BridgeWireOnOff, Match: BondMatchPrefix},
	{Keyword: "bcast_flood", JSON: "bcast_flood", ID: unix.IFLA_BRPORT_BCAST_FLOOD, Wire: BridgeWireOnOff, Match: BondMatchPrefix},
	{Keyword: "mcast_to_unicast", JSON: "mcast_to_unicast", ID: unix.IFLA_BRPORT_MCAST_TO_UCAST, Wire: BridgeWireOnOff, Match: BondMatchPrefix},
	{Keyword: "neigh_suppress", JSON: "neigh_suppress", ID: unix.IFLA_BRPORT_NEIGH_SUPPRESS, Wire: BridgeWireOnOff, Match: BondMatchPrefix},
	{Keyword: "neigh_vlan_suppress", JSON: "neigh_vlan_suppress", ID: unix.IFLA_BRPORT_NEIGH_VLAN_SUPPRESS, Wire: BridgeWireOnOff, Match: BondMatchExact},
	{Keyword: "neigh_forward_grat", JSON: "neigh_forward_grat", ID: IflaBrportNeighForwardGrat, Wire: BridgeWireOnOff, Match: BondMatchExact},
	{Keyword: bridgeKwGroupFwdMaskCst, JSON: bridgeKwGroupFwdMaskCst, ID: unix.IFLA_BRPORT_GROUP_FWD_MASK, Wire: BridgeWireU16, Hex: true, Match: BondMatchPrefix},
	{Keyword: "vlan_tunnel", JSON: "vlan_tunnel", ID: unix.IFLA_BRPORT_VLAN_TUNNEL, Wire: BridgeWireOnOff, Match: BondMatchPrefix},
	{Keyword: "isolated", JSON: "isolated", ID: unix.IFLA_BRPORT_ISOLATED, Wire: BridgeWireOnOff, Match: BondMatchPrefix},
	{Keyword: "locked", JSON: "locked", ID: unix.IFLA_BRPORT_LOCKED, Wire: BridgeWireOnOff, Match: BondMatchPrefix},
	{Keyword: "mab", JSON: "mab", ID: unix.IFLA_BRPORT_MAB, Wire: BridgeWireOnOff, Match: BondMatchExact},
	{Keyword: "backup_port", JSON: "backup_port", ID: unix.IFLA_BRPORT_BACKUP_PORT, Wire: BridgeWireIfindex, Match: BondMatchPrefix},
	{Keyword: "backup_nhid", JSON: "backup_nhid", ID: unix.IFLA_BRPORT_BACKUP_NHID, Wire: BridgeWireU32, Match: BondMatchExact},
	{Keyword: "fdb_flush", ID: unix.IFLA_BRPORT_FLUSH, Wire: BridgeWireFlag, Match: BondMatchPrefix},
	{ID: unix.IFLA_BRPORT_MRP_RING_OPEN, Wire: BridgeWireU8, ReadOnly: true},
	{ID: unix.IFLA_BRPORT_MRP_IN_OPEN, Wire: BridgeWireU8, ReadOnly: true},
	{ID: unix.IFLA_BRPORT_MCAST_EHT_HOSTS_LIMIT, Wire: BridgeWireU32, ReadOnly: true},
	{ID: unix.IFLA_BRPORT_MCAST_EHT_HOSTS_CNT, Wire: BridgeWireU32, ReadOnly: true},
	{ID: unix.IFLA_BRPORT_MCAST_N_GROUPS, Wire: BridgeWireU32, ReadOnly: true},
	{ID: unix.IFLA_BRPORT_MCAST_MAX_GROUPS, Wire: BridgeWireU32, ReadOnly: true},
}

// bridgeSlaveOptionByID indexes BridgeSlaveOptions for the decoder. The
// first row for an id stands for it: fastleave, not mcast_fast_leave.
var bridgeSlaveOptionByID = indexBridgeOptions(BridgeSlaveOptions)

// BridgeSlaveInfo is IFLA_INFO_SLAVE_DATA for slave kind "bridge", decoded.
type BridgeSlaveInfo struct {
	// values holds every integer option, indexed by IFLA_BRPORT_* id; read
	// it with Value.
	values [IflaBrportNeighForwardGrat + 1]BridgeValue

	// BridgeID and RootID are the designated bridge and root.
	BridgeID *BridgeID
	RootID   *BridgeID

	// Unknown lists attribute ids the table does not know, in wire order.
	Unknown []uint16
}

// Value returns the decoded option id, absent if the kernel did not send it.
func (s *BridgeSlaveInfo) Value(id uint16) BridgeValue {
	if s == nil || int(id) >= len(s.values) {
		return BridgeValue{}
	}
	return s.values[id]
}

// ParseBridgeSlaveData decodes IFLA_INFO_SLAVE_DATA for slave kind
// "bridge". nil for a nil nest; tolerant as ParseBridgeInfoData is.
func ParseBridgeSlaveData(nest []byte) *BridgeSlaveInfo {
	if nest == nil {
		return nil
	}
	s := &BridgeSlaveInfo{}
	walkNestTolerant(nest, func(atype uint16, val []byte) {
		spec := bridgeSlaveOptionByID[atype]
		if spec == nil {
			s.Unknown = append(s.Unknown, atype)
			return
		}
		if spec.Wire == BridgeWireID {
			if spec.ID == unix.IFLA_BRPORT_BRIDGE_ID {
				s.BridgeID = parseBridgeID(val)
			} else {
				s.RootID = parseBridgeID(val)
			}
			return
		}
		if v, ok := bridgeScalar(spec.Wire, val); ok {
			s.values[atype] = BridgeValue{U: v, Present: true}
		}
	})
	return s
}

// EncodeBridgeSlaveData encodes the inside of IFLA_INFO_SLAVE_DATA for kind
// "bridge_slave": one attribute per setting, in argv order, over
// BridgeSlaveOptions rows. Two keywords for one attribute send it twice, as
// bridge_slave_parse_opt does.
func EncodeBridgeSlaveData(settings []BridgeSetting) ([]byte, error) {
	var raw [linkWriteBufCst]byte
	ab := NewAttrBuilder(raw[:])
	for _, s := range settings {
		if err := putBridgeSetting(&ab, s); err != nil {
			return nil, err
		}
	}
	return append([]byte(nil), ab.Bytes()...), nil
}
