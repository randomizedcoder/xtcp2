package render

import (
	"fmt"
	"math"
	"net/netip"
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is vxlan_print_opt (ip/iplink_vxlan.c, iproute2 v7.2.0): the
// tokens `ip -d link show` prints after `    vxlan ` (KG3-T04,
// docs/netlink/goip-link-kinds.md). The ttl, tos, df and flowlabel words
// and the address tests are geneve_print_opt's too, which link_geneve.go
// calls in its own order.
//
//   - group and local print only when not zero; a group that isn't
//     multicast is the remote. GROUP6 and LOCAL6 take the JSON keys
//     group6, remote6 and local6.
//   - tos 1 is `tos inherit` and ttl 0 `ttl auto` in text; JSON has the
//     numbers. A ttl inherit is text only.
//   - df unset is JSON only.
//   - Each bool of xtcpnl.VxlanBoolOptions prints, by its JSON key, when it
//     isn't at its default, or at any value under `-d -d`, the only site
//     at v7.2.0 that reads show_details past one. JSON has every bool the
//     kernel sent.

// VxlanKindCst is the vxlan kind, exported for the `-d` gate.
const VxlanKindCst = "vxlan"

// The DF values, VXLAN_DF_* and GENEVE_DF_*.
const (
	overlayDFUnset   = 0
	overlayDFSet     = 1
	overlayDFInherit = 2
)

// vxlanData is vxlan_print_opt over the decoded IFLA_INFO_DATA. details is
// show_details.
func vxlanData(v *xtcpnl.VxlanInfo, names NameTab, details int) *KindData {
	k := &KindData{}
	if v == nil {
		return k
	}
	if v.HasID {
		k.add("id", fmt.Sprintf("id %d ", v.ID), v.ID)
	}
	if g := v.Group; g.IsValid() && !g.IsUnspecified() {
		key := "remote"
		if overlayMulticast(g) {
			key = "group"
		}
		k.add(overlayKey(key, g), key+" "+g.String()+" ", g.String())
	}
	if l := v.Local; l.IsValid() && !l.IsUnspecified() {
		k.add(overlayKey("local", l), "local "+l.String()+" ", l.String())
	}
	if v.HasLink && v.Link != 0 {
		name := kindLinkName(names, v.Link)
		k.add("link", "dev "+name+" ", name)
	}
	if v.HasPortRange {
		r := &KindData{}
		r.add("low", "", v.PortLow)
		r.add("high", "", v.PortHigh)
		k.add("port_range", fmt.Sprintf("srcport %d %d ", v.PortLow, v.PortHigh), r)
	}
	if v.HasPort {
		k.add("port", fmt.Sprintf("dstport %d ", v.Port), v.Port)
	}
	overlayTOS(k, v.TOS)
	overlayTTL(k, v.HasTTLInherit && v.TTLInherit != 0, v.HasTTL, v.TTL)
	overlayDF(k, v.HasDF, v.DF)
	overlayLabel(k, v.HasLabel, v.Label)
	if v.HasAging {
		text := fmt.Sprintf("%s %d ", xtcpnl.VxlanKeywordAgeing, v.Aging)
		if v.Aging == 0 {
			text = xtcpnl.VxlanKeywordAgeing + " none "
		}
		k.add(xtcpnl.VxlanKeywordAgeing, text, v.Aging)
	}
	if v.HasLimit && v.Limit != 0 {
		k.add("limit", fmt.Sprintf("maxaddr %d ", v.Limit), v.Limit)
	}
	if v.HasReservedBits {
		h := cHex0x(v.ReservedBits)
		k.add("reserved_bits", "reserved_bits "+h+" ", h)
	}
	if v.GBP {
		k.add("gbp", "gbp ", nil)
	}
	if v.GPE {
		k.add("gpe", "gpe ", nil)
	}
	for i, opt := range xtcpnl.VxlanBoolOptions {
		if !v.HasBools[i] {
			continue
		}
		on := v.Bools[i] != 0
		text := ""
		if on != opt.Default || details > 1 {
			text = noPrefix(on) + opt.Key + " "
		}
		k.add(opt.Key, text, on)
	}
	return k
}

// overlayMulticast is IN_MULTICAST or IN6_IS_ADDR_MULTICAST on the bytes,
// so a v4-mapped address is not multicast, where netip's IsMulticast would
// unmap it.
func overlayMulticast(a netip.Addr) bool {
	if a.Is4() {
		return a.As4()[0]&0xf0 == 0xe0
	}
	return a.As16()[0] == 0xff
}

// overlayKey is an address's JSON key: key for the IPv4 attribute and
// key6 for the IPv6 one.
func overlayKey(key string, a netip.Addr) string {
	if a.Is4() {
		return key
	}
	return key + "6"
}

// kindLinkName is ll_index_to_name on a kind's u32 lower device. An index
// past INT_MAX is ll_idx_n2a's "if%u", which no cache holds.
func kindLinkName(names NameTab, idx uint32) string {
	if idx > math.MaxInt32 {
		return "if" + strconv.FormatUint(uint64(idx), 10)
	}
	return names.IndexToName(int32(idx))
}

// noPrefix is print_bool_opt's "no" for a false bool.
func noPrefix(on bool) string {
	if on {
		return ""
	}
	return "no"
}

// overlayTOS is the tos word: nothing for 0, `tos inherit` for 1 in text,
// and the value as print_0xhex otherwise and always in JSON.
func overlayTOS(k *KindData, tos uint8) {
	if tos == 0 {
		return
	}
	h := cHex0x(uint64(tos))
	text := "tos " + h + " "
	if tos == 1 {
		text = "tos inherit "
	}
	k.add("tos", text, h)
}

// overlayTTL is the ttl word: `ttl inherit` in text alone, or the ttl,
// which is `ttl auto` in text when 0.
func overlayTTL(k *KindData, inherit, has bool, ttl uint8) {
	switch {
	case inherit:
		k.addText("ttl inherit ")
	case has:
		text := fmt.Sprintf("ttl %d ", ttl)
		if ttl == 0 {
			text = "ttl auto "
		}
		k.add("ttl", text, ttl)
	}
}

// overlayDF is the df word: unset in JSON only, set and inherit in both,
// and any other value not at all.
func overlayDF(k *KindData, has bool, df uint8) {
	if !has {
		return
	}
	switch df {
	case overlayDFUnset:
		k.add("df", "", "unset")
	case overlayDFSet:
		k.add("df", "df set ", "set")
	case overlayDFInherit:
		k.add("df", "df inherit ", "inherit")
	}
}

// overlayLabel is the flowlabel word, printed when not zero.
func overlayLabel(k *KindData, has bool, label uint32) {
	if has && label != 0 {
		h := cHex0x(uint64(label))
		k.add("label", "flowlabel "+h+" ", h)
	}
}
