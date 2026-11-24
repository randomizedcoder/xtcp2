package render

import (
	"fmt"
	"net/netip"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// The renders of `ip tunnel`, `ip -6 tunnel` and `ip tuntap` at iproute2
// v7.2.0: ip/iptunnel.c and ip/ip6tunnel.c print_tunnel, ip/tunnel.c
// tnl_print_gre_flags and tnl_print_stats, and ip/iptuntap.c print_tuntap.
// Each writes to a Printer as the C writes to stdout, including its bare
// printf calls, which land inside a -j document (Printer.Raw).

// The tunnel protocol numbers tnl_strproto names.
const (
	tnlProtoIPIP = 4
	tnlProtoIPv6 = 41
	tnlProtoGRE  = 47
	tnlProtoESP  = 50
	tnlProtoMPLS = 137
)

// The mode words of ESP and MPLS.
const (
	tnlProtoESPCst  = "esp"
	tnlProtoMPLSCst = "mpls"
)

// ipDF is IP_DF in frag_off, host order.
const ipDF = 0x4000

// The ip6_tnl_parm2 flowinfo masks, host order (include/linux/ipv6.h
// IPV6_FLOWINFO_*).
const (
	ip6FlowinfoTclass    = 0x0FF00000
	ip6FlowinfoFlowlabel = 0x000FFFFF
)

// TnlStrproto is tnl_strproto: the protocol's mode word.
func TnlStrproto(proto uint8) string {
	switch proto {
	case tnlProtoIPIP:
		return "ip"
	case tnlProtoGRE:
		return "gre"
	case tnlProtoIPv6:
		return "ipv6"
	case tnlProtoESP:
		return tnlProtoESPCst
	case tnlProtoMPLS:
		return tnlProtoMPLSCst
	case 0:
		return tnlAnyCst
	}
	return "unknown"
}

// TunnelV4 is what iptunnel.c print_tunnel prints of one tunnel: the
// SIOCGETTUNNEL answer and what it asks on the way.
type TunnelV4 struct {
	Parm xtcpnl.IPTunnelParm
	// PRL is the SIOCGETPRL answer for an isatap tunnel, nil when it was
	// not asked or failed. Entry 0 is the request header, never printed.
	PRL []xtcpnl.TunnelPRL
	// Dev is ll_index_to_name(link) when link is set.
	Dev string
	// SixRD is the SIOCGET6RD answer for a sit tunnel, nil when it was
	// not asked or failed.
	SixRD *xtcpnl.Tunnel6RD
}

// PrintTunnelV4 is iptunnel.c print_tunnel. ds is the run's rt_dsfield
// table; sl is _SL_.
func PrintTunnelV4(p *Printer, t *TunnelV4, ds *Dsfield, sl string) {
	q := &t.Parm
	p.OpenObject("")
	p.String(PrintAny, "ifname", "%s: ", q.Name)
	p.String(PrintAny, "mode", "%s ", TnlStrproto(q.Protocol)+"/ip")
	p.FPf("remote ")
	remote := tnlAnyCst
	if !isZero4(q.Daddr) || p.JSON() {
		remote = addr4(q.Daddr)
	}
	p.String(PrintAny, "remote", "%s ", remote)
	p.FPf("local ")
	local := tnlAnyCst
	if !isZero4(q.Saddr) || p.JSON() {
		local = addr4(q.Saddr)
	}
	p.String(PrintAny, "local", "%s", local)

	if q.Protocol == tnlProtoIPv6 && q.IFlags&xtcpnl.SitIsatap != 0 {
		for i := 1; i < len(t.PRL); i++ {
			e := t.PRL[i]
			if isZero4(e.Addr) {
				continue
			}
			if e.Flags&xtcpnl.PRLDefault != 0 {
				p.String(PrintAny, "pdr", " pdr %s", addr4(e.Addr))
			} else {
				p.String(PrintAny, "pr", " pr %s", addr4(e.Addr))
			}
		}
	}
	if q.Link != 0 {
		p.String(PrintAny, "dev", " dev %s", t.Dev)
	}
	if q.TTL != 0 {
		p.U64(PrintAny, "ttl", " ttl %d", uint64(q.TTL))
	} else {
		p.FPf(" ttl inherit")
	}
	if q.TOS != 0 {
		if q.TOS != 1 {
			s := ds.N2A(q.TOS)
			if !p.JSON() && q.TOS&1 != 0 {
				s = "inherit/" + ds.N2A(q.TOS&^1)
			}
			p.String(PrintAny, "tos", " tos %s", s)
		} else {
			p.FPf(" tos inherit")
		}
	}
	if q.FragOff&ipDF == 0 {
		p.Null(PrintAny, "nopmtudisc", " nopmtudisc")
	}
	if q.Protocol == tnlProtoIPv6 && t.SixRD != nil && t.SixRD.PrefixLen != 0 {
		r := t.SixRD
		p.String(PrintAny, "6rd-prefix", " 6rd-prefix %s", inet6Ntop(r.Prefix.As16()))
		p.U64(PrintAny, "6rd-prefixlen", "/%d", uint64(r.PrefixLen))
		if !isZero4(r.RelayPrefix) {
			p.String(PrintAny, "6rd-relay_prefix", " 6rd-relay_prefix %s", addr4(r.RelayPrefix))
			p.U64(PrintAny, "6rd-relay_prefixlen", "/%d", uint64(r.RelayPrefixLen))
		}
	}
	printGreFlags(p, q.Protocol, q.IFlags, q.OFlags, q.IKey, q.OKey, sl)
	p.CloseObject()
}

// TunnelV6 is what ip6tunnel.c print_tunnel prints of one tunnel.
type TunnelV6 struct {
	Parm xtcpnl.IP6TnlParm2
	// Dev is ll_index_to_name(link) when link is set.
	Dev string
}

// PrintTunnelV6 is ip6tunnel.c print_tunnel; sl is _SL_.
func PrintTunnelV6(p *Printer, t *TunnelV6, sl string) {
	q := &t.Parm
	p.OpenObject("")
	p.String(PrintAny, "ifname", "%s: ", q.Name)
	p.String(PrintAny, "mode", "%s ", TnlStrproto(q.Proto)+"/ipv6")
	p.FPf("remote ")
	p.String(PrintAny, "remote", "%s ", addr16(q.Raddr))
	p.FPf("local ")
	p.String(PrintAny, "local", "%s", addr16(q.Laddr))
	if q.Link != 0 {
		p.String(PrintAny, "link", " dev %s", t.Dev)
	}
	if q.Flags&xtcpnl.IP6TnlFIgnEncapLimit != 0 {
		p.Null(PrintAny, "ip6_tnl_f_ign_encap_limit", " encaplimit none")
	} else {
		p.U64(PrintAny, "encap_limit", " encaplimit %d", uint64(q.EncapLimit))
	}
	if q.HopLimit != 0 {
		p.U64(PrintAny, "hoplimit", " hoplimit %d", uint64(q.HopLimit))
	} else {
		p.FPf(" hoplimit inherit")
	}
	if q.Flags&xtcpnl.IP6TnlFUseOrigTclass != 0 {
		p.Null(PrintAny, "ip6_tnl_f_use_orig_tclass", " tclass inherit")
	} else {
		p.String(PrintAny, "tclass", " tclass %s", fmt.Sprintf("0x%02x", (q.FlowInfo&ip6FlowinfoTclass)>>20))
	}
	if q.Flags&xtcpnl.IP6TnlFUseOrigFlowlabel != 0 {
		p.Null(PrintAny, "ip6_tnl_f_use_orig_flowlabel", " flowlabel inherit")
	} else {
		p.String(PrintAny, "flowlabel", " flowlabel %s", fmt.Sprintf("0x%05x", q.FlowInfo&ip6FlowinfoFlowlabel))
	}
	p.String(PrintAny, "flowinfo", " (flowinfo %s)", fmt.Sprintf("0x%08x", q.FlowInfo))
	if q.Flags&xtcpnl.IP6TnlFRcvDSCPCopy != 0 {
		p.Null(PrintAny, "ip6_tnl_f_rcv_dscp_copy", " dscp inherit")
	}
	if q.Flags&xtcpnl.IP6TnlFAllowLocalRemote != 0 {
		p.Null(PrintAny, "ip6_tnl_f_allow_local_remote", " allow-localremote")
	}
	printGreFlags(p, q.Proto, q.IFlags, q.OFlags, q.IKey, q.OKey, sl)
	p.CloseObject()
}

// printGreFlags is tnl_print_gre_flags: the keys, then for a GRE tunnel
// the flags array under -j, or one bare printf line each in text.
func printGreFlags(p *Printer, proto uint8, iflags, oflags uint16, ikey, okey uint32, sl string) {
	if iflags&xtcpnl.GreFlagKey != 0 && oflags&xtcpnl.GreFlagKey != 0 && ikey == okey {
		p.U64(PrintAny, "key", " key %d", uint64(ikey))
	} else {
		if iflags&xtcpnl.GreFlagKey != 0 {
			p.U64(PrintAny, "ikey", " ikey %d", uint64(ikey))
		}
		if oflags&xtcpnl.GreFlagKey != 0 {
			p.U64(PrintAny, "okey", " okey %d", uint64(okey))
		}
	}
	if proto != tnlProtoGRE {
		return
	}
	p.OpenArray("flags")
	for _, f := range []struct {
		set        bool
		json, text string
	}{
		{iflags&xtcpnl.GreFlagSeq != 0, "rx_drop_ooseq", "  Drop packets out of sequence."},
		{iflags&xtcpnl.GreFlagCsum != 0, "rx_csum", "  Checksum in received packet is required."},
		{oflags&xtcpnl.GreFlagSeq != 0, "tx_seq", "  Sequence packets on output."},
		{oflags&xtcpnl.GreFlagCsum != 0, "tx_csum", "  Checksum output packets."},
	} {
		if !f.set {
			continue
		}
		if p.JSON() {
			p.String(PrintJSON, "", "%s", f.json)
		} else {
			p.Raw(sl + f.text)
		}
	}
	p.CloseArray()
}

// TunnelStats is tnl_print_stats: bare printf, so under -j too. The
// counters are __u64 printed with %lld.
func TunnelStats(s *xtcpnl.RtnlLinkStats64, sl string) string {
	ll := cint.LongLong
	return sl +
		"RX: Packets    Bytes        Errors CsumErrs OutOfSeq Mcasts" + sl +
		fmt.Sprintf("    %-10d %-12d %-6d %-8d %-8d %-8d", ll(s.RxPackets), ll(s.RxBytes), ll(s.RxErrors),
			ll(s.RxFrameErrors), ll(s.RxFifoErrors), ll(s.Multicast)) + sl +
		"TX: Packets    Bytes        Errors DeadLoop NoRoute  NoBufs" + sl +
		fmt.Sprintf("    %-10d %-12d %-6d %-8d %-8d %-6d", ll(s.TxPackets), ll(s.TxBytes), ll(s.TxErrors),
			ll(s.Collisions), ll(s.TxCarrierErrors), ll(s.TxDropped))
}

// The tun flag words print_flags prints, in its order; pi and filter are
// printed when their bit is clear.
var tunFlagWords = []struct {
	bit   int64
	clear bool
	word  string
}{
	{xtcpnl.TunIffTun, false, "tun"},
	{xtcpnl.TunIffTap, false, "tap"},
	{xtcpnl.TunIffNoPI, true, "pi"},
	{xtcpnl.TunIffOneQueue, false, "one_queue"},
	{xtcpnl.TunIffMultiQueue, false, "multi_queue"},
	{xtcpnl.TunIffVnetHdr, false, "vnet_hdr"},
	{xtcpnl.TunIffPersist, false, "persist"},
	{xtcpnl.TunIffNoPI, true, "filter"}, // IFF_NOFILTER, the same bit
}

// TunProc is one process show_processes prints: its comm, or "<NULL>",
// and its pid.
type TunProc struct {
	Name string
	Pid  int
}

// Tuntap is what print_tuntap prints of one tun device.
type Tuntap struct {
	Name         string
	Flags        int64 // read_prop's long
	Owner, Group int64 // -1 for none
	// Details is show_details: Procs follow "Attached to processes:".
	Details bool
	Procs   []TunProc
}

// PrintTuntap is print_tuntap from its open_json_object; sl is _SL_.
func PrintTuntap(p *Printer, t *Tuntap, sl string) {
	p.OpenObject("")
	p.String(PrintAny, "ifname", "%s:", t.Name)
	p.OpenArray("flags")
	var known int64
	for _, w := range tunFlagWords {
		known |= w.bit
		if (t.Flags&w.bit != 0) != w.clear {
			p.String(PrintAny, "", " %s", w.word)
		}
	}
	if rest := t.Flags &^ known; rest != 0 {
		h := fmt.Sprintf("%#x", cint.U64Bits(rest))
		p.String(PrintAny, "", " %s", h)
	}
	p.CloseArray()
	printLongU64(p, "user", " user %d", t.Owner)
	printLongU64(p, "group", " group %d", t.Group)
	if t.Details {
		p.FPf("%s\tAttached to processes:", sl)
		p.OpenArray("processes")
		for _, pr := range t.Procs {
			p.OpenObject("")
			p.String(PrintAny, "name", "%s", pr.Name)
			p.Int(PrintAny, "pid", "(%d)", int64(pr.Pid))
			p.CloseObject()
		}
		p.CloseArray()
	}
	p.CloseObject()
	p.FPf("\n")
}

// printLongU64 is print_u64 of a long that is not -1: the JSON number is
// its u64 bits, the text its %ld.
func printLongU64(p *Printer, key, format string, v int64) {
	if v == -1 {
		return
	}
	if p.JSON() {
		p.U64(PrintJSON, key, "", cint.U64Bits(v))
		return
	}
	p.FPf(format, v)
}

// isZero4 is an address of all zero bytes, or no address.
func isZero4(a netip.Addr) bool {
	return !a.IsValid() || a.IsUnspecified()
}

// addr4 is format_host or rt_addr_n2a of a 4-byte address: dotted quad.
func addr4(a netip.Addr) string {
	if !a.IsValid() {
		return "0.0.0.0"
	}
	return a.String()
}

// addr16 is format_host or rt_addr_n2a of a 16-byte address: glibc's
// inet_ntop.
func addr16(a netip.Addr) string {
	if !a.Is6() {
		return "::"
	}
	return inet6Ntop(a.As16())
}
