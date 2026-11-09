package render

import (
	"fmt"
	"net/netip"
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file is iptunnel_print_opt (ip/link_iptnl.c, ipip and sit) and
// ip6tunnel_print_opt (ip/link_ip6tnl.c), iproute2 v7.2.0 (KG4-T08,
// docs/netlink/goip-link-kinds.md).
//
//   - pmtudisc always prints on ipip and sit: `nopmtudisc` when off or
//     not sent. ip6tnl has no pmtudisc.
//   - ip6tnl's text says hoplimit; its JSON key is ttl, as gre6's.
//   - The 6rd words print only when the prefix length is not zero, and
//     the relay only when its prefix is.

// The ipip-family kinds, exported for the `-d` gate.
const (
	IpipKindCst   = "ipip"
	SitKindCst    = "sit"
	IP6TnlKindCst = "ip6tnl"
)

// iptunProtoNames are ipip's and sit's proto words; ip6tunProtoNames are
// ip6tnl's. A protocol in neither prints nothing.
var (
	iptunProtoNames = map[uint8]string{
		xtcpnl.IptunProtoAny: tnlAnyCst, xtcpnl.IptunProtoIPIP: "ipip",
		xtcpnl.IptunProtoIPv6: "ip6ip", xtcpnl.IptunProtoMPLS: "mplsip",
	}
	ip6tunProtoNames = map[uint8]string{
		xtcpnl.IptunProtoAny: tnlAnyCst, xtcpnl.IptunProtoIPIP: "ipip6", xtcpnl.IptunProtoIPv6: "ip6ip6",
	}
)

// iptunData is iptnl's print_opt over the decoded IFLA_INFO_DATA, or
// ip6tnl's when v6.
func iptunData(g *xtcpnl.IptunInfo, names NameTab, v6 bool) *KindData {
	k := &KindData{}
	if g == nil {
		return k
	}
	if g.CollectMetadata {
		k.add("external", "external ", true)
	}
	protos := iptunProtoNames
	if v6 {
		protos = ip6tunProtoNames
	}
	if p, ok := protos[g.Proto]; ok && g.HasProto {
		k.add("proto", p+" ", p)
	}
	tnlEndpoint(k, "remote", g.Remote, g.HasRemote, g.RemoteLen, v6)
	tnlEndpoint(k, "local", g.Local, g.HasLocal, g.LocalLen, v6)
	tnlLink(k, names, g.HasLink, g.Link)
	if v6 {
		ip6tunTail(k, g)
	} else {
		iptunTail(k, g)
	}
	tnlEncap(k, g.Encap)
	return k
}

// iptunTail is ipip's and sit's words after the lower: ttl, tos,
// pmtudisc, isatap, 6rd and fwmark.
func iptunTail(k *KindData, g *xtcpnl.IptunInfo) {
	tnlTTL(k, "ttl", g.TTL)
	overlayTOS(k, g.TOS)
	on := g.HasPMTUDisc && g.PMTUDisc != 0
	k.add("pmtudisc", noPrefix(on)+"pmtudisc ", on)
	if g.HasFlags && g.Flags&xtcpnl.IptunFlagSitIsatap != 0 {
		k.add("isatap", "isatap ", true)
	}
	iptun6rd(k, g)
	tnlFwmark(k, g.HasFwMark, g.FwMark)
}

// iptun6rd is sit's 6rd prefix and relay prefix.
func iptun6rd(k *KindData, g *xtcpnl.IptunInfo) {
	if !g.HasRdPrefixLen || g.RdPrefixLen == 0 {
		return
	}
	prefix := netip.IPv6Unspecified().String()
	if g.RdPrefix.IsValid() {
		prefix = g.RdPrefix.String()
	}
	n := strconv.FormatUint(uint64(g.RdPrefixLen), 10)
	k.add("prefix", "6rd-prefix "+prefix+"/"+n+" ", prefix)
	k.add("prefixlen", "", g.RdPrefixLen)
	if g.RdRelayPrefix.IsValid() && !g.RdRelayPrefix.IsUnspecified() {
		relay := g.RdRelayPrefix.String()
		k.add("relay_prefix", fmt.Sprintf("6rd-relay_prefix %s/%d ", relay, g.RdRelayPrefixLen), relay)
		k.add("relay_prefixlen", "", g.RdRelayPrefixLen)
	}
}

// ip6tunTail is ip6tnl's words after the lower: hoplimit, the flow words,
// mip6, allow-localremote and fwmark.
func ip6tunTail(k *KindData, g *xtcpnl.IptunInfo) {
	tnlTTL(k, "hoplimit", g.TTL)
	tnl6Flow(k, g.Flags, g.HasEncapLimit, g.EncapLimit, g.HasFlowInfo, g.FlowInfo)
	if g.Flags&xtcpnl.Ip6TnlFlagMIP6Dev != 0 {
		k.add("ip6_tnl_f_mip6_dev", "mip6 ", true)
	}
	gre6Flag(k, g.Flags, xtcpnl.Ip6TnlFlagAllowLocalRemote)
	if !gre6Flag(k, g.Flags, xtcpnl.Ip6TnlFlagUseOrigFwmark) {
		tnlFwmark(k, g.HasFwMark, g.FwMark)
	}
}
