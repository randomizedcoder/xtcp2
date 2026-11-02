package render

import (
	"encoding/binary"
	"fmt"
	"net/netip"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is gre_print_opt (ip/link_gre.c) and its ip6 twin
// (ip/link_gre6.c), iproute2 v7.2.0: the tokens `ip -d link show` prints
// after `    gre ` and the five other gre kinds (KG4-T07,
// docs/netlink/goip-link-kinds.md).
//
//   - The endpoints always print, `any` when not sent.
//   - ttl (gre6's text says hoplimit) prints `inherit` in text for 0 and
//     the number in JSON; gre's tos is `inherit` in text for 1. gre6 has
//     no tos word and no mip6.
//   - pmtudisc prints only off in text, and as a bool in JSON.
//   - A key prints dotted, and only under its KEY flag.
//   - gre6's flag words hide the value they stand for; the CAP bits print
//     nothing.

// The gre kinds, exported for the `-d` gate.
const (
	GreKindCst       = "gre"
	GretapKindCst    = "gretap"
	ErspanKindCst    = "erspan"
	IP6GreKindCst    = "ip6gre"
	IP6GretapKindCst = "ip6gretap"
	IP6ErspanKindCst = "ip6erspan"
)

// gre6Flags are gre6's IP6_TNL_F_* words, each with the JSON bool it is.
var gre6Flags = map[uint32]struct{ key, text string }{
	xtcpnl.Ip6TnlFlagIgnEncapLimit:    {"ip6_tnl_f_ign_encap_limit", "encaplimit none "},
	xtcpnl.Ip6TnlFlagUseOrigTclass:    {"ip6_tnl_f_use_orig_tclass", "tclass inherit "},
	xtcpnl.Ip6TnlFlagUseOrigFlowlabel: {"ip6_tnl_f_use_orig_flowlabel", "flowlabel inherit "},
	xtcpnl.Ip6TnlFlagRcvDscpCopy:      {"ip6_tnl_f_rcv_dscp_copy", "dscp inherit "},
	xtcpnl.Ip6TnlFlagAllowLocalRemote: {"ip6_tnl_f_allow_local_remote", "allow-localremote "},
	xtcpnl.Ip6TnlFlagUseOrigFwmark:    {"ip6_tnl_f_use_orig_fwmark", "fwmark inherit "},
}

// gre6Flag prints bit's word and says whether it was set.
func gre6Flag(k *KindData, flags, bit uint32) bool {
	if flags&bit == 0 {
		return false
	}
	f := gre6Flags[bit]
	k.add(f.key, f.text, true)
	return true
}

// greData is gre_print_opt over the decoded IFLA_INFO_DATA: gre6's when v6.
func greData(g *xtcpnl.GreInfo, names NameTab, v6 bool) *KindData {
	k := &KindData{}
	if g == nil {
		return k
	}
	if g.CollectMetadata {
		k.add("external", "external ", true)
	}
	tnlEndpoint(k, "remote", g.Remote, g.HasRemote, g.RemoteLen, v6)
	tnlEndpoint(k, "local", g.Local, g.HasLocal, g.LocalLen, v6)
	tnlLink(k, names, g.HasLink, g.Link)
	if v6 {
		tnlTTL(k, "hoplimit", g.TTL)
		gre6Flow(k, g)
	} else {
		tnlTTL(k, "ttl", g.TTL)
		greTOS(k, g)
	}
	greKeys(k, g)
	if v6 {
		gre6Flag(k, g.Flags, xtcpnl.Ip6TnlFlagAllowLocalRemote)
		if !gre6Flag(k, g.Flags, xtcpnl.Ip6TnlFlagUseOrigFwmark) {
			greFwmark(k, g)
		}
	} else {
		greFwmark(k, g)
	}
	greErspan(k, g)
	tnlEncap(k, g.Encap)
	return k
}

// greTOS is gre's tos, pmtudisc and ignore-df words.
func greTOS(k *KindData, g *xtcpnl.GreInfo) {
	if g.TOS != 0 {
		h := cHex0x(uint64(g.TOS))
		text := "tos " + h + " "
		if g.TOS == 1 {
			text = "tos inherit "
		}
		k.add("tos", text, h)
	}
	if g.HasPMTUDisc {
		text := ""
		if g.PMTUDisc == 0 {
			text = "nopmtudisc "
		}
		k.add("pmtudisc", text, g.PMTUDisc != 0)
	}
	if g.HasIgnoreDF && g.IgnoreDF != 0 {
		k.add("ignore_df", "ignore-df ", true)
	}
}

// gre6Flow is gre6's encaplimit, tclass, flowlabel and dscp words.
func gre6Flow(k *KindData, g *xtcpnl.GreInfo) {
	if !gre6Flag(k, g.Flags, xtcpnl.Ip6TnlFlagIgnEncapLimit) && g.HasEncapLimit {
		k.add("encap_limit", fmt.Sprintf("encaplimit %d ", g.EncapLimit), g.EncapLimit)
	}
	if !gre6Flag(k, g.Flags, xtcpnl.Ip6TnlFlagUseOrigTclass) && g.HasFlowInfo {
		v := fmt.Sprintf("0x%02x", (g.FlowInfo&xtcpnl.Ip6FlowInfoTclass)>>20)
		k.add("tclass", "tclass "+v+" ", v)
	}
	if !gre6Flag(k, g.Flags, xtcpnl.Ip6TnlFlagUseOrigFlowlabel) && g.HasFlowInfo {
		v := fmt.Sprintf("0x%05x", g.FlowInfo&xtcpnl.Ip6FlowInfoFlowlabel)
		k.add("flowlabel", "flowlabel "+v+" ", v)
	}
	gre6Flag(k, g.Flags, xtcpnl.Ip6TnlFlagRcvDscpCopy)
}

// greKeyText is a key as inet_ntop prints its four bytes.
func greKeyText(key uint32) string {
	return netip.AddrFrom4([4]byte(binary.BigEndian.AppendUint32(nil, key))).String()
}

// greKeys is the keys under their KEY flags, then the seq and csum flags.
func greKeys(k *KindData, g *xtcpnl.GreInfo) {
	if g.IFlags&xtcpnl.GreFlagKey != 0 && g.HasIKey {
		v := greKeyText(g.IKey)
		k.add("ikey", "ikey "+v+" ", v)
	}
	if g.OFlags&xtcpnl.GreFlagKey != 0 && g.HasOKey {
		v := greKeyText(g.OKey)
		k.add("okey", "okey "+v+" ", v)
	}
	for _, f := range [...]struct {
		key string
		on  bool
	}{
		{"iseq", g.IFlags&xtcpnl.GreFlagSeq != 0}, {"oseq", g.OFlags&xtcpnl.GreFlagSeq != 0},
		{"icsum", g.IFlags&xtcpnl.GreFlagCsum != 0}, {"ocsum", g.OFlags&xtcpnl.GreFlagCsum != 0},
	} {
		if f.on {
			k.add(f.key, f.key+" ", true)
		}
	}
}

// greFwmark is the fwmark word, printed when not zero.
func greFwmark(k *KindData, g *xtcpnl.GreInfo) {
	if g.HasFwMark && g.FwMark != 0 {
		h := cHex0x(uint64(g.FwMark))
		k.add("fwmark", "fwmark "+h+" ", h)
	}
}

// greErspan is the erspan words, each whenever sent.
func greErspan(k *KindData, g *xtcpnl.GreInfo) {
	if g.HasErspanIndex {
		k.add("erspan_index", fmt.Sprintf("erspan_index %d ", g.ErspanIndex), g.ErspanIndex)
	}
	if g.HasErspanVer {
		k.add("erspan_ver", fmt.Sprintf("erspan_ver %d ", g.ErspanVer), g.ErspanVer)
	}
	if g.HasErspanDir {
		dir := "egress"
		if g.ErspanDir == 0 {
			dir = "ingress"
		}
		k.add("erspan_dir", "erspan_dir "+dir+" ", dir)
	}
	if g.HasErspanHwid {
		h := cHex0x(uint64(g.ErspanHwid))
		k.add("erspan_hwid", "erspan_hwid "+h+" ", h)
	}
}
