package render

import (
	"encoding/json"
	"fmt"
	"math"
	"net/netip"
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// This file is bridge_print_opt (ip/iplink_bridge.c, iproute2 v7.2.0) and
// the bridge_print_mcast_querier_state it calls (lib/bridge.c): the tokens
// `ip -d link show` prints after `    bridge ` (KG1-T03,
// docs/netlink/goip-link-kinds.md).
//
// It walks xtcpnl.BridgeOptions, whose order is bridge_print_opt's, with
// each row's keyword for the text and its JSON key for `-j`; they differ
// for ten rows. How each wire prints:
//   - u8, u16, u32 and u64 are print_uint or print_lluint: a JSON number.
//   - stp_mode is its name, "(unknown)" past the table.
//   - vlan_protocol is ll_proto_n2a's name, "[N]" for an unnamed one.
//   - bridge_id and designated_root are br_dump_bridge_id's
//     "%.2x%.2x.<ether_ntoa>", whose address isn't zero-padded.
//   - group_fwd_mask is print_0xhex: "%#llx" in both renders, so 0 is "0".
//   - group_address is ll_addr_n2a's hex.
//   - a timer is _bridge_print_timer's seconds and hundredths, "%4i.%.2i"
//     to a terminal and "%i.%.2i" written raw to `-j`, a number no float
//     marshals the same: 0.00.
//   - the five boolopts print when their bit is in the mask, as 0 or 1.
//   - the querier state is two JSON objects, ipv4 and ipv6, opened whether
//     or not they hold anything.
//
// The timers go through C ints; goip keeps the u64, so the two would differ
// only past 2^31 seconds.

// BridgeKindCst is the bridge kind, exported for the `-d` gate.
const BridgeKindCst = "bridge"

// bridgeStpModeUnknownCst is stp_mode_to_str's answer past its table.
const bridgeStpModeUnknownCst = "(unknown)"

// bridgeData renders a bridge's IFLA_INFO_DATA. A nil BridgeInfo is
// print_opt's `if (!tb) return;`: an empty object.
func bridgeData(b *xtcpnl.BridgeInfo) *KindData {
	k := &KindData{}
	if b == nil {
		return k
	}
	for i := range xtcpnl.BridgeOptions {
		spec := &xtcpnl.BridgeOptions[i]
		switch spec.Wire {
		case xtcpnl.BridgeWireFlag:
			// fdb_flush: write-only.
		case xtcpnl.BridgeWireID:
			id := b.BridgeID
			if spec.ID == unix.IFLA_BR_ROOT_ID {
				id = b.RootID
			}
			if id != nil {
				s := bridgeIDString(id)
				k.add(spec.JSON, spec.Keyword+" "+s+" ", s)
			}
		case xtcpnl.BridgeWireMAC:
			if b.GroupAddr != nil {
				mac := xtcpnl.LLAddrN2A(b.GroupAddr, unix.ARPHRD_ETHER)
				k.add(spec.JSON, spec.Keyword+" "+mac+" ", mac)
			}
		case xtcpnl.BridgeWireBoolOpt:
			if on, ok := b.BoolOpts.Has(spec.Bit); ok {
				var v uint64
				if on {
					v = 1
				}
				k.add(spec.JSON, fmt.Sprintf("%s %d ", spec.Keyword, v), v)
			}
		case xtcpnl.BridgeWireQuerier:
			if b.QuerierState != nil {
				addBridgeQuerierState(k, b.QuerierState)
			}
		default:
			if v := b.Value(spec.ID); v.Present {
				addBridgeScalar(k, spec, v.U)
			}
		}
	}
	return k
}

// addBridgeScalar is one integer-valued row, printed by its wire.
func addBridgeScalar(k *KindData, spec *xtcpnl.BridgeOptionSpec, v uint64) {
	switch {
	case spec.Wire == xtcpnl.BridgeWireTimer:
		sec, cs := v/100, v%100 // __jiffies_to_tv: USER_HZ ticks
		k.add(spec.JSON, fmt.Sprintf("%s %4d.%02d ", spec.Keyword, sec, cs),
			json.RawMessage(fmt.Sprintf("%d.%02d", sec, cs)))
	case spec.Names != nil:
		name := bridgeStpModeUnknownCst
		if v < uint64(len(spec.Names)) {
			name = spec.Names[v]
		}
		k.add(spec.JSON, spec.Keyword+" "+name+" ", name)
	case spec.Wire == xtcpnl.BridgeWireBE16 && v <= math.MaxUint16:
		name := xtcpnl.LLProtoN2A(uint16(v))
		k.add(spec.JSON, spec.Keyword+" "+name+" ", name)
	case spec.Hex:
		h := cHex0x(v)
		k.add(spec.JSON, spec.Keyword+" "+h+" ", h)
	default:
		k.add(spec.JSON, spec.Keyword+" "+strconv.FormatUint(v, 10)+" ", v)
	}
}

// bridgeIDString is br_dump_bridge_id: the priority's two bytes in hex,
// then ether_ntoa's unpadded address.
func bridgeIDString(id *xtcpnl.BridgeID) string {
	a := id.Addr
	return fmt.Sprintf("%02x%02x.%x:%x:%x:%x:%x:%x", id.Prio[0], id.Prio[1], a[0], a[1], a[2], a[3], a[4], a[5])
}

// addBridgeQuerierState is bridge_print_mcast_querier_state: an ipv4 and
// an ipv6 object, each with the address, port and other timer the kernel
// sent.
func addBridgeQuerierState(k *KindData, q *xtcpnl.BridgeQuerierState) {
	k.add("mcast_querier_state_ipv4", "", bridgeQuerierFamily("ipv4", q.IPv4Addr, q.IPv4Port, q.IPv4OtherTimer))
	k.add("mcast_querier_state_ipv6", "", bridgeQuerierFamily("ipv6", q.IPv6Addr, q.IPv6Port, q.IPv6OtherTimer))
}

func bridgeQuerierFamily(fam string, addr netip.Addr, port xtcpnl.U32Attr, timer xtcpnl.BridgeValue) *KindData {
	o := &KindData{}
	prefix := "mcast_querier_" + fam + "_"
	if addr.IsValid() {
		s := addr.String()
		o.add(prefix+"addr", prefix+"addr "+s+" ", s)
	}
	if port.Present {
		o.add(prefix+"port", fmt.Sprintf("%sport %d ", prefix, port.Value), port.Value)
	}
	if timer.Present {
		s := sprintTime64(timer.U)
		o.add(prefix+"other_timer", prefix+"other_timer "+s+" ", s)
	}
	return o
}

// sprintTime64 is sprint_time64 (lib/utils.c): nanoseconds as "%.3g" of
// the largest unit they reach.
func sprintTime64(ns uint64) string {
	const (
		nsecPerUsec = 1000
		nsecPerMsec = 1000 * nsecPerUsec
		nsecPerSec  = 1000 * nsecPerMsec
	)
	f := float64(ns)
	switch {
	case ns >= nsecPerSec:
		return fmt.Sprintf("%.3gs", f/nsecPerSec)
	case ns >= nsecPerMsec:
		return fmt.Sprintf("%.3gms", f/nsecPerMsec)
	case ns >= nsecPerUsec:
		return fmt.Sprintf("%.3gus", f/nsecPerUsec)
	}
	return strconv.FormatUint(ns, 10) + "ns"
}
