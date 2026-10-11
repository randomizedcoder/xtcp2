package render

import (
	"fmt"
	"net/netip"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// This file is bond_print_opt and bond_slave_print_opt (ip/iplink_bond.c:442-781
// and ip/iplink_bond_slave.c, both at the v7.1.0 pin): the tokens `ip -d link
// show` prints after `    bond ` and `    bond_slave `.
//
// # Grounded on what fixtures
//
// pkg/xtcpnl/testdata/7_1_4/link-write holds three LACP states of one bond,
// each with its `ip -d link show` text and `-j` sidecars. Those are what
// goip_bond_test.go compares against, byte for byte. Paths those fixtures do
// not reach are transcribed from the C and covered by the hand-built rows in
// link_bond_test.go: active_slave and primary (802.3ad sends neither), the
// two target lists (none configured), and every out-of-table value.
//
// # One known divergence, deliberate
//
// iproute2 reads IFLA_BOND_SLAVE_AD_PARTNER_OPER_PORT_STATE with
// rta_getattr_u8 (iplink_bond_slave.c, bond_slave_print_opt). The kernel puts
// it as a u16 (bond_netlink.c, bond_fill_slave_info). goip renders the u16 it
// decoded. The two agree on every value the kernel can produce today, because
// the LACP port state is eight bits wide, so no fixture can show the
// difference. If the kernel ever sets a bit above 0xff, goip prints it and
// the pinned `ip` does not.

// Exported for the `-d` gate in internal/goip, which has to agree with this
// file on which kinds have a print_opt here.
const (
	BondKindCst = "bond"
	VrfKindCst  = "vrf"
)

// bondUnknownNameCst is get_name's answer for an index past the end of its
// table (ip/iplink_bond.c:98-107).
const bondUnknownNameCst = "UNKNOWN"

// bondJSONKey maps the options whose `-j` key is not their keyword. The text
// always prints the keyword, which is xtcpnl.BondOptionSpec.Keyword.
var bondJSONKey = map[uint16]string{
	unix.IFLA_BOND_NUM_PEER_NOTIF: "num_peer_notif",
	unix.IFLA_BOND_AD_LACP_ACTIVE: "ad_lacp_active",
	unix.IFLA_BOND_AD_LACP_RATE:   "ad_lacp_rate",
}

// bondOnOff is the two options bond_print_opt prints with print_on_off: a
// JSON bool, and "on"/"off" for any nonzero/zero value. lacp_active has the
// same two names in the option table and is NOT one of these: it goes
// through get_name, so it is a JSON string and a 2 prints "UNKNOWN".
var bondOnOff = map[uint16]bool{
	unix.IFLA_BOND_COUPLED_CONTROL: true,
	xtcpnl.IflaBondBroadcastNeigh:  true,
}

// bondNotPrinted is the options the pinned iproute2 has no print call for.
// lacp_strict is newer than 7.1.0, whose IFLA_BOND_MAX stops before it, so
// parse_rtattr_nested drops the attribute and nothing is printed even when a
// newer kernel sends it.
var bondNotPrinted = map[uint16]bool{
	xtcpnl.IflaBondLacpStrict: true,
}

// bondData renders a bond's IFLA_INFO_DATA.
//
// It walks xtcpnl.BondOptions, whose order is bond_print_opt's, so the text
// keyword, the wire width and the value names come from the same table the
// decoder and the encoder use. Only how each value is PRINTED is decided
// here. A nil BondInfo is print_opt's `if (!tb) return;`: an empty object.
//
// names resolves active_slave and primary, which bond_print_opt passes to
// ll_index_to_name.
func bondData(b *xtcpnl.BondInfo, names NameTab) *KindData {
	k := &KindData{}
	if b == nil {
		return k
	}
	for i := range xtcpnl.BondOptions {
		spec := &xtcpnl.BondOptions[i]
		if bondNotPrinted[spec.ID] {
			continue
		}
		switch spec.Wire {
		case xtcpnl.BondWireIPv4Set:
			addBondTargets(k, spec.Keyword, b.ArpIPTargets)
		case xtcpnl.BondWireIPv6Set:
			addBondTargets(k, spec.Keyword, b.NsIP6Targets)
		case xtcpnl.BondWireNest:
			if b.AdInfo != nil {
				k.add("ad_info", "", bondAdInfoData(b.AdInfo))
			}
		case xtcpnl.BondWireMAC:
			if v := b.Value(spec.ID); v.Present {
				// ll_addr_n2a(..., 1 /*ARPHRD_ETHER*/, ...), which has no
				// special case for ARPHRD_ETHER and falls through to hex.
				mac := xtcpnl.LLAddrN2A(v.Bytes, unix.ARPHRD_ETHER)
				k.add(spec.Keyword, spec.Keyword+" "+mac+" ", mac)
			}
		default:
			if v := b.Value(spec.ID); v.Present {
				addBondScalar(k, spec, v.U, names)
			}
		}
	}
	return k
}

// addBondScalar is one u8/u16/u32 option.
func addBondScalar(k *KindData, spec *xtcpnl.BondOptionSpec, v uint32, names NameTab) {
	key := spec.Keyword
	if j, ok := bondJSONKey[spec.ID]; ok {
		key = j
	}
	switch {
	case spec.ID == unix.IFLA_BOND_ACTIVE_SLAVE || spec.ID == unix.IFLA_BOND_PRIMARY:
		// `if (ifindex)`: a zero index prints nothing at all.
		if v == 0 {
			return
		}
		name := names.IndexToName(int32(v)) //nolint:gosec // an ifindex: the kernel's int, sent as u32
		k.add(key, spec.Keyword+" "+name+" ", name)
	case bondOnOff[spec.ID]:
		on := v != 0
		k.add(key, spec.Keyword+" "+onOff(on)+" ", on)
	case spec.ID == unix.IFLA_BOND_ARP_VALIDATE && v == 0:
		// `if (!arp_v && is_json_context()) print_null(...)`: "none" to a
		// terminal, null to -j.
		k.add(key, spec.Keyword+" "+spec.ValueName(v)+" ", nil)
	case len(spec.Names) > 0:
		name := spec.ValueName(v)
		if name == "" {
			name = bondUnknownNameCst
		}
		k.add(key, spec.Keyword+" "+name+" ", name)
	default:
		k.add(key, fmt.Sprintf("%s %d ", spec.Keyword, v), v)
	}
}

// addBondTargets is the arp_ip_target / ns_ip6_target block: the keyword,
// the addresses joined by commas, one space, and a JSON array of strings.
//
// iproute2 decides the comma by whether the NEXT slot is filled, and prints
// the keyword only when slot 0 is. The decoder has already dropped empty
// slots, so this joins what is left. The two agree because the kernel fills
// the slots from 0 with no gaps: removing a target shifts the rest down
// (bond_options.c, _bond_option_arp_ip_target_rem).
func addBondTargets(k *KindData, keyword string, addrs []netip.Addr) {
	if len(addrs) == 0 {
		return
	}
	strs := make([]string, len(addrs))
	for i, a := range addrs {
		strs[i] = a.String()
	}
	k.add(keyword, keyword+" "+strings.Join(strs, ",")+" ", strs)
}

// bondAdInfoData is the ad_info object. Its keys are not its text keywords,
// and each value is print_int of a u16.
func bondAdInfoData(ai *xtcpnl.BondAdInfo) *KindData {
	k := &KindData{}
	for _, f := range []struct {
		key, keyword string
		attr         xtcpnl.U16Attr
	}{
		{"aggregator", "ad_aggregator", ai.Aggregator},
		{"num_ports", "ad_num_ports", ai.NumPorts},
		{"actor_key", "ad_actor_key", ai.ActorKey},
		{"partner_key", "ad_partner_key", ai.PartnerKey},
	} {
		if f.attr.Present {
			k.add(f.key, fmt.Sprintf("%s %d ", f.keyword, f.attr.Value), f.attr.Value)
		}
	}
	if ai.PartnerMAC != nil {
		mac := xtcpnl.LLAddrN2A(ai.PartnerMAC, 0)
		k.add("partner_mac", "ad_partner_mac "+mac+" ", mac)
	}
	return k
}

// Slave state and mii_status names, by index (iplink_bond_slave.c). An index
// past the end is printed as a number under a different JSON key.
var (
	bondSlaveStates    = []string{"ACTIVE", "BACKUP"}
	bondSlaveMiiStatus = []string{"UP", "GOING_DOWN", "DOWN", "GOING_BACK"}
	bondChurnStates    = []string{"MONITOR", "CHURN", "NO_CHURN"}
)

// lacpStateBits is print_slave_oper_state's _PF list, in its order.
var lacpStateBits = []struct {
	bit  uint16
	name string
}{
	{0x01, "active"},
	{0x02, "short_timeout"},
	{0x04, "aggregating"},
	{0x08, "in_sync"},
	{0x10, "collecting"},
	{0x20, "distributing"},
	{0x40, "defaulted"},
	{0x80, "expired"},
}

// bondSlaveData renders a bond port's IFLA_INFO_SLAVE_DATA.
func bondSlaveData(s *xtcpnl.BondSlaveInfo) *KindData {
	k := &KindData{}
	if s == nil {
		return k
	}
	if s.State.Present {
		addIndexedName(k, "state", "state_index", bondSlaveStates, s.State.Value)
	}
	if s.MiiStatus.Present {
		addIndexedName(k, "mii_status", "mii_status_index", bondSlaveMiiStatus, s.MiiStatus.Value)
	}
	if s.LinkFailureCount.Present {
		// print_int of a u32: a count past 2^31 prints negative.
		n := int32(s.LinkFailureCount.Value) //nolint:gosec // transcribing %d of an unsigned value
		k.add("link_failure_count", fmt.Sprintf("link_failure_count %d ", n), n)
	}
	if s.PermHwaddr != nil {
		mac := xtcpnl.LLAddrN2A(s.PermHwaddr, 0)
		k.add("perm_hwaddr", "perm_hwaddr "+mac+" ", mac)
	}
	addU16Int(k, "queue_id", s.QueueID)
	if s.Prio.Present {
		p := s.PrioInt32()
		k.add("prio", fmt.Sprintf("prio %d ", p), p)
	}
	addU16Int(k, "ad_aggregator_id", s.AdAggregatorID)
	if s.AdActorOperState.Present {
		addOperState(k, "ad_actor_oper_port_state", uint16(s.AdActorOperState.Value))
	}
	if s.AdPartnerOperState.Present {
		addOperState(k, "ad_partner_oper_port_state", s.AdPartnerOperState.Value)
	}
	if s.AdChurnActorState.Present {
		addChurn(k, "churn_actor_state", s.AdChurnActorState.Value)
	}
	if s.AdChurnPartnerState.Present {
		addChurn(k, "churn_partner_state", s.AdChurnPartnerState.Value)
	}
	addU16Int(k, "actor_port_prio", s.ActorPortPrio)
	return k
}

// addIndexedName is print_slave_state and print_slave_mii_status: the name
// under key, or the raw index under indexKey when the table has no entry.
func addIndexedName(k *KindData, key, indexKey string, names []string, v uint8) {
	if int(v) < len(names) {
		k.add(key, key+" "+names[v]+" ", names[v])
		return
	}
	k.add(indexKey, fmt.Sprintf("%s %d ", key, v), v)
}

// addU16Int is print_int(PRINT_ANY, key, "key %d ", rta_getattr_u16(...)).
func addU16Int(k *KindData, key string, a xtcpnl.U16Attr) {
	if a.Present {
		k.add(key, fmt.Sprintf("%s %d ", key, a.Value), a.Value)
	}
}

// addChurn is the churn state print: a name, or %u of the raw value.
func addChurn(k *KindData, key string, v uint8) {
	if int(v) < len(bondChurnStates) {
		k.add(key, key+" "+bondChurnStates[v]+" ", bondChurnStates[v])
		return
	}
	k.add(key, fmt.Sprintf("%s %d ", key, v), v)
}

// addOperState is the number and then print_slave_oper_state's flag list.
//
// The list is transcribed including its one quirk: each name is followed by
// a comma if any bit is STILL set after clearing its own, and that test
// counts bits no name covers. So a state with a bit above 0x80 prints a
// trailing comma before the ">", and the JSON array is unaffected. Only the
// u16 partner state can carry such a bit; see the divergence note above.
func addOperState(k *KindData, key string, state uint16) {
	k.add(key, fmt.Sprintf("%s %d ", key, state), state)
	names := []string{}
	var t strings.Builder
	t.WriteString(key + "_str <")
	rest := state
	for _, f := range lacpStateBits {
		if rest&f.bit == 0 {
			continue
		}
		rest &^= f.bit
		names = append(names, f.name)
		t.WriteString(f.name)
		if rest != 0 {
			t.WriteByte(',')
		}
	}
	t.WriteString("> ")
	k.add(key+"_str", t.String(), names)
}
