package render

import (
	"fmt"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// ilaNoneCst is "-" as print_string(PRINT_FP, NULL, "%-10s ", "-")
// writes it, for an absent device or checksum mode.
const ilaNoneCst = "-          "

// ilaCsumNames is ila_csum_mode2name (ip/ila_common.h).
var ilaCsumNames = map[uint8]string{
	xtcpnl.ILACsumAdjustTransport: "adj-transport",
	xtcpnl.ILACsumNeutralMap:      "neutral-map",
	xtcpnl.ILACsumNoAction:        "no-action",
	xtcpnl.ILACsumNeutralMapAuto:  "neutral-map-auto",
}

// ilaIdentNames is ila_ident_type2name.
var ilaIdentNames = map[uint8]string{
	xtcpnl.ILAAtypeIID:          "iid",
	xtcpnl.ILAAtypeLUID:         "luid",
	xtcpnl.ILAAtypeVirtV4:       "virt-v4",
	xtcpnl.ILAAtypeVirtUniV6:    "virt-uni-v6",
	xtcpnl.ILAAtypeVirtMultiV6:  "virt-multi-v6",
	xtcpnl.ILAAtypeNonlocalAddr: "nonlocal-addr",
	xtcpnl.ILAAtypeUseFormat:    "use-format",
}

// ilaName is a name from one of the tables, "unknown" for a value it does
// not hold.
func ilaName(names map[uint8]string, v uint8) string {
	if n, ok := names[v]; ok {
		return n
	}
	return "unknown"
}

// IlaMapping is print_ila_mapping's line and object (ip/ipila.c): the
// locator to match and the locator, each print_addr64's four hex words or
// "-", padded to 20; the device, through dev, padded to 16, or "-" padded
// to 10 and a space in text alone; the checksum mode's name, or the same
// "-"; then the identifier type's name, or "-". Each name an attribute's
// value has no entry for is "unknown".
func IlaMapping(m *xtcpnl.ILA, dev func(int32) string) (string, []byte) {
	var o genlOut
	ilaLocid(&o, "locator_match", m.LocatorMatch)
	ilaLocid(&o, "locator", m.Locator)
	if m.Ifindex != nil {
		name := dev(*m.Ifindex)
		o.str("interface", fmt.Sprintf("%-16s", name), name)
	} else {
		o.text.WriteString(ilaNoneCst)
	}
	if m.CsumMode != nil {
		name := ilaName(ilaCsumNames, *m.CsumMode)
		o.str("csum_mode", name, name)
	} else {
		o.text.WriteString(ilaNoneCst)
	}
	if m.IdentType != nil {
		name := ilaName(ilaIdentNames, *m.IdentType)
		o.str("ident_type", name, name)
	} else {
		o.text.WriteString("-")
	}
	return o.done()
}

// ilaLocid is print_ila_locid: print_addr64 of the attribute, its four
// 16-bit words in network order each in hex, or "-" when it is absent.
func ilaLocid(o *genlOut, key string, v *uint64) {
	s := "-"
	if v != nil {
		s = fmt.Sprintf("%x:%x:%x:%x", *v>>48, *v>>32&0xffff, *v>>16&0xffff, *v&0xffff)
	}
	o.str(key, fmt.Sprintf("%-20s", s), s)
}
