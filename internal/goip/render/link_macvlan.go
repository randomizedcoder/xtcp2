package render

import (
	"fmt"
	"math"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is macvlan_print_opt (ip/iplink_macvlan.c, iproute2 v7.2.0),
// which macvtap shares: the tokens `ip -d link show` prints after
// `    macvlan ` or `    macvtap ` (KG2-T04,
// docs/netlink/goip-link-kinds.md).
//
// Without a mode nothing prints. A mode past the five is "unknown". The
// flags print as JSON bools, the queue lengths as numbers and bclim
// signed. Only source mode goes on to `remotes (<count>) ` and the
// addresses, the count through print_int's %d, and the addresses as a
// `-j` array of their own.

// The two kinds macvlan_print_opt serves, exported for the `-d` gate.
const (
	MacvlanKindCst = "macvlan"
	MacvtapKindCst = "macvtap"
)

// macvlanModeUnknownCst is macvlan_print_opt's name for a mode past the
// five; ipvlan_print_opt's is the same word.
const macvlanModeUnknownCst = "unknown"

// macvlanData is macvlan_print_opt over the decoded IFLA_INFO_DATA.
func macvlanData(m *xtcpnl.MacvlanInfo) *KindData {
	k := &KindData{}
	if m == nil || !m.HasMode {
		return k
	}
	mode, ok := xtcpnl.MacvlanModes[m.Mode]
	if !ok {
		mode = macvlanModeUnknownCst
	}
	k.add("mode", "mode "+mode+" ", mode)
	if m.Flags&xtcpnl.MacvlanFlagNopromisc != 0 {
		k.add("nopromisc", "nopromisc ", true)
	}
	if m.Flags&xtcpnl.MacvlanFlagNodst != 0 {
		k.add("nodst", "nodst ", true)
	}
	if m.HasBcQueueLen {
		k.add("bcqueuelen", fmt.Sprintf("bcqueuelen %d ", m.BcQueueLen), m.BcQueueLen)
	}
	if m.HasBcQueueLenUsed {
		k.add("usedbcqueuelen", fmt.Sprintf("usedbcqueuelen %d ", m.BcQueueLenUsed), m.BcQueueLenUsed)
	}
	if m.HasBcCutoff {
		k.add("bclim", fmt.Sprintf("bclim %d ", m.BcCutoff), m.BcCutoff)
	}
	if m.Mode != xtcpnl.MacvlanModeSource || !m.HasMacaddrCount {
		return k
	}
	// print_int takes an int, so a count past MaxInt32 wraps negative.
	count := int64(m.MacaddrCount)
	if count > math.MaxInt32 {
		count -= 1 << 32
	}
	k.add("macaddr_count", fmt.Sprintf("remotes (%d) ", count), count)
	if !m.HasMacaddrData {
		return k
	}
	var text string
	addrs := make([]string, 0, len(m.Macaddrs))
	for _, a := range m.Macaddrs {
		s := fmt.Sprintf("%.2x:%.2x:%.2x:%.2x:%.2x:%.2x", a[0], a[1], a[2], a[3], a[4], a[5])
		text += s + " "
		addrs = append(addrs, s)
	}
	k.add("macaddr_data", text, addrs)
	return k
}
