package render

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// This file is bridge_slave_print_opt (ip/iplink_bridge_slave.c, iproute2
// v7.2.0): the tokens `ip -d link show` prints after `    bridge_slave `
// for a port of a bridge (KG1-T04, docs/netlink/goip-link-kinds.md).
//
// It walks xtcpnl.BridgeSlaveOptions, in print order, as bridgeData walks
// the bridge's table. What differs from the bridge:
//   - state is its name, or "state (N)" under the JSON key "state_index"
//     past BR_STATE_BLOCKING.
//   - the port's flags are print_on_off: on/off text, a JSON bool.
//   - port_id and port_no are print_0xhex, as group_fwd_mask is, and
//     group_fwd_mask adds group_fwd_mask_str, _bitmask2str's names.
//   - mcast_fast_leave is IFLA_BRPORT_FAST_LEAVE again, text only: its row
//     has no JSON key.
//   - backup_port is ll_index_to_name's name, which internal/goip fetches
//     under -d, as it does a bond's active_slave.
//   - the six attributes 7.2.0 doesn't print have no keyword and are
//     skipped.
//
// priority and cost are print_int of the u16 and u32; the kernel holds
// them to 63 and 65535 (br_stp_set_port_priority, br_stp_set_path_cost),
// so the unsigned print is the same.

// bridgeSlaveStateBlockingCst is BR_STATE_BLOCKING, the last named state.
const bridgeSlaveStateBlockingCst = 4

// bridgeFwdMaskNames is fwd_mask_tbl: the group address bits with names.
var bridgeFwdMaskNames = map[int]string{0: "stp", 2: "lacp", 14: "lldp"}

// bridgeSlaveData renders a bridge port's IFLA_INFO_SLAVE_DATA. A nil
// BridgeSlaveInfo is print_opt's `if (!tb) return;`: an empty object.
// names resolves backup_port.
func bridgeSlaveData(s *xtcpnl.BridgeSlaveInfo, names NameTab) *KindData {
	k := &KindData{}
	if s == nil {
		return k
	}
	for i := range xtcpnl.BridgeSlaveOptions {
		spec := &xtcpnl.BridgeSlaveOptions[i]
		switch {
		case spec.Keyword == "" || spec.Wire == xtcpnl.BridgeWireFlag:
			// not printed by 7.2.0, or write-only
		case spec.Wire == xtcpnl.BridgeWireID:
			id := s.RootID
			if spec.ID == unix.IFLA_BRPORT_BRIDGE_ID {
				id = s.BridgeID
			}
			if id != nil {
				str := bridgeIDString(id)
				k.add(spec.JSON, spec.Keyword+" "+str+" ", str)
			}
		default:
			if v := s.Value(spec.ID); v.Present {
				addBridgeSlaveScalar(k, spec, v.U, names)
			}
		}
	}
	return k
}

// addBridgeSlaveScalar is one integer-valued port row, printed by its wire.
func addBridgeSlaveScalar(k *KindData, spec *xtcpnl.BridgeOptionSpec, v uint64, names NameTab) {
	switch {
	case spec.Names != nil:
		if v <= bridgeSlaveStateBlockingCst {
			k.add(spec.JSON, spec.Keyword+" "+spec.Names[v]+" ", spec.Names[v])
			return
		}
		k.add("state_index", fmt.Sprintf("state (%d) ", v), v)
	case spec.Wire == xtcpnl.BridgeWireOnOff && spec.JSON == "":
		// mcast_fast_leave: print_string(PRINT_FP, ...), no JSON.
		k.addText(spec.Keyword + " " + onOff(v != 0) + " ")
	case spec.Wire == xtcpnl.BridgeWireOnOff:
		k.add(spec.JSON, spec.Keyword+" "+onOff(v != 0)+" ", v != 0)
	case spec.Wire == xtcpnl.BridgeWireIfindex && v <= math.MaxInt32:
		name := names.IndexToName(int32(v))
		k.add(spec.JSON, spec.Keyword+" "+name+" ", name)
	case spec.Hex:
		h := cHex0x(v)
		k.add(spec.JSON, spec.Keyword+" "+h+" ", h)
		if spec.ID == unix.IFLA_BRPORT_GROUP_FWD_MASK {
			str := bridgeFwdMaskString(v)
			k.add("group_fwd_mask_str", "group_fwd_mask_str "+str+" ", str)
		}
	default:
		addBridgeScalar(k, spec, v)
	}
}

// bridgeFwdMaskString is _bitmask2str over fwd_mask_tbl: the set bits'
// names or "0x%x" values, comma-separated, or "0x0" for none.
func bridgeFwdMaskString(v uint64) string {
	var parts []string
	for i := range 16 {
		if v&(1<<i) == 0 {
			continue
		}
		if n, ok := bridgeFwdMaskNames[i]; ok {
			parts = append(parts, n)
		} else {
			parts = append(parts, "0x"+strconv.FormatUint(1<<i, 16))
		}
	}
	if parts == nil {
		return "0x0"
	}
	return strings.Join(parts, ",")
}
