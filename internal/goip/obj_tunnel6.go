package goip

import (
	"fmt"
	"net/netip"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/tnlsys"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip -6 tunnel show` is ip/ip6tunnel.c do_show at v7.2.0: parse_args over
// a zeroed ip6_tnl_parm2 of protocol 0 (any), then one SIOCGETTUNNEL on
// the named device, printed with no final newline (T9), or, with no name
// or under -s, do_tunnels_list over the AF_INET6 link dump.

// The ip6_tnl_parm2 flowinfo masks, host order.
const (
	ip6FlowinfoTclassCst    = 0x0FF00000
	ip6FlowinfoFlowlabelCst = 0x000FFFFF
)

// tnlModeV6 is ip6tunnel.c parse_args' mode word.
func tnlModeV6(p *xtcpnl.IP6TnlParm2, mode string) error {
	switch mode {
	case tnlModeIPv6v6Cst, tnlModeIP6IP6Cst:
		p.Proto = tnlProtoIPv6
	case kindVti6Cst:
		p.Proto = tnlProtoIPv6
		p.IFlags |= xtcpnl.VtiIsVti
	case tnlModeIPIP6Cst, tnlModeIPv4v6Cst, tnlModeIPIP6wCst, tnlModeIP4IP6Cst:
		p.Proto = tnlProtoIPIP
	case kindIP6GreCst, tnlModeGREv6Cst:
		p.Proto = tnlProtoGRE
	case tnlModeAnyV6Cst, addrNameAnyCst:
		p.Proto = 0
	default:
		return fmt.Errorf("%w: Unknown tunnel mode %q", ErrTunnelUsage, mode)
	}
	return nil
}

// tnlAddr6 is get_addr(AF_INET6), whose failure is exit(1): any, all and
// default are all zero.
func tnlAddr6(v string) (netip.Addr, error) {
	a, err := routeGetAddr(v, unix.AF_INET6)
	if err != nil {
		return netip.Addr{}, err
	}
	var b [16]byte
	copy(b[:], a.data)
	return netip.AddrFrom16(b), nil
}

// tnl6Value is parse_args' arms that take a value: false for a word that
// is not one of them.
func tnl6Value(p *xtcpnl.IP6TnlParm2, w, v string) (bool, error) {
	switch w {
	case kindKeyRemote:
		a, err := tnlAddr6(v)
		p.Raddr = a
		return true, err
	case addrLocalKeywordCst:
		a, err := tnlAddr6(v)
		p.Laddr = a
		return true, err
	case "encaplimit":
		if v == noneKeywordCst {
			p.Flags |= xtcpnl.IP6TnlFIgnEncapLimit
			return true, nil
		}
		// get_u8's -1 is never < -1 (T20): a bad value leaves uval
		// unset, which the corpus shows as 0.
		u, ok := getU8(v, 0)
		if !ok {
			u = 0
		}
		p.EncapLimit = u
		p.Flags &^= xtcpnl.IP6TnlFIgnEncapLimit
		return true, nil
	case kindKeyHoplimit, kindKeyTTL, tnlKeyHlim:
		u, ok := getU8(v, 0)
		if !ok {
			return true, tnlUsage(invargErr("invalid TTL", v))
		}
		p.HopLimit = u
		return true, nil
	case gre6Flowlabel, "fl":
		p.FlowInfo &^= ip6FlowinfoFlowlabelCst
		if v == tnlInheritCst {
			p.Flags |= xtcpnl.IP6TnlFUseOrigFlowlabel
			return true, nil
		}
		u, ok := getU32(v, 16)
		if !ok || u > ip6FlowinfoFlowlabelCst {
			return true, tnlUsage(invargErr("invalid Flowlabel", v))
		}
		p.FlowInfo |= u
		p.Flags &^= xtcpnl.IP6TnlFUseOrigFlowlabel
		return true, nil
	case gre6Dscp:
		if v != tnlInheritCst {
			return true, tnlUsage(invargErr("not inherit", v))
		}
		p.Flags |= xtcpnl.IP6TnlFRcvDSCPCopy
		return true, nil
	}
	if w == tnlTclassCst || w == "tc" || w == tosKeywordCst || matchesPrefix(w, tnlDsfieldCst) {
		p.FlowInfo &^= ip6FlowinfoTclassCst
		if v == tnlInheritCst {
			p.Flags |= xtcpnl.IP6TnlFUseOrigTclass
			return true, nil
		}
		u, ok := getU8(v, 16)
		if !ok {
			return true, tnlUsage(invargErr("invalid TClass", v))
		}
		p.FlowInfo |= uint32(u) << 20
		p.Flags &^= xtcpnl.IP6TnlFUseOrigTclass
		return true, nil
	}
	return false, nil
}

// tnl6TakesValue is whether parse_args' word w takes NEXT_ARG.
func tnl6TakesValue(w string) bool {
	switch w {
	case tnlModeCst, kindKeyRemote, addrLocalKeywordCst, devKeywordCst, "encaplimit", kindKeyHoplimit, kindKeyTTL, tnlKeyHlim, gre6Flowlabel, "fl", gre6Dscp,
		tnlTclassCst, "tc", tosKeywordCst:
		return true
	}
	return matchesPrefix(w, tnlDsfieldCst)
}

// parseTunnelV6 is ip6tunnel.c parse_args for SIOCGETTUNNEL over do_show's
// starting struct.
func parseTunnelV6(c *runCtx, args []string) (xtcpnl.IP6TnlParm2, error) {
	var p xtcpnl.IP6TnlParm2
	var medium string
	var err error
	for i := 0; i < len(args); i++ {
		w := args[i]
		switch {
		case w == tnlKeyKey || w == tnlKeyIkey || w == tnlKeyOkey:
			if i, err = tnlKeyWord(args, i, &p.IFlags, &p.OFlags, &p.IKey, &p.OKey); err != nil {
				return p, err
			}
		case tnlFlagWord(w, &p.IFlags, &p.OFlags):
		case w == "allow-localremote":
			p.Flags |= xtcpnl.IP6TnlFAllowLocalRemote
		case w == "noallow-localremote":
			p.Flags &^= xtcpnl.IP6TnlFAllowLocalRemote
		case tnl6TakesValue(w):
			if i, err = tnlNext(args, i); err != nil {
				return p, err
			}
			switch w {
			case tnlModeCst:
				err = tnlModeV6(&p, args[i])
			case devKeywordCst:
				medium = args[i]
			default:
				_, err = tnl6Value(&p, w, args[i])
			}
			if err != nil {
				return p, err
			}
		default:
			if p.Name, i, err = tnlParseName(args, i, p.Name, ip6TunnelUsageCst); err != nil {
				return p, err
			}
		}
	}
	if medium != "" {
		idx, err := tnlDev(c, medium)
		if err != nil {
			return p, err
		}
		p.Link = idx
	}
	return p, nil
}

// tunnelShowV6 is ip6tunnel.c do_show.
func tunnelShowV6(c *runCtx, args []string) error {
	p, err := parseTunnelV6(c, args)
	if err != nil {
		return err
	}
	if p.Name == "" || c.showStats > 0 {
		return tunnelsList(c, unix.AF_INET6, p.Name, func(pr *render.Printer, li *xtcpnl.LinkInfo) (bool, error) {
			return tunnelListV6(c, pr, &p, li)
		})
	}
	buf := p.Marshal()
	ok, err := tnlIoctl(c, unix.AF_INET6, tnlsys.SIOCGETTUNNEL, p.Name, buf, tnlGetMsg(p.Name))
	if err != nil {
		return err
	}
	if !ok {
		return ErrTunnelRefused
	}
	q, err := xtcpnl.ParseIP6TnlParm2(buf)
	if err != nil {
		return err
	}
	// Text under -j too, as on the v4 named path, and no newline (T9).
	pr := render.NewPrinter(false)
	render.PrintTunnelV6(pr, &render.TunnelV6{Parm: q, Dev: tnlIndexName(c, q.Link)}, tnlSL(c))
	_, err = c.out.Write(pr.Bytes())
	return err
}

// tunnelListV6 is print_nlmsg_tunnel's per-tunnel part for v6:
// ip6_tnl_parm_initialize (protocol by ARPHRD, the device's index and
// p1's name), the ioctl, ip6_tnl_parm_match and print_tunnel.
func tunnelListV6(c *runCtx, pr *render.Printer, p1 *xtcpnl.IP6TnlParm2, li *xtcpnl.LinkInfo) (bool, error) {
	p2 := xtcpnl.IP6TnlParm2{TunnelName: xtcpnl.TunnelName{Name: p1.Name}, Link: li.Index, Proto: tnlProtoIPv6}
	if li.Type == unix.ARPHRD_IP6GRE {
		p2.Proto = tnlProtoGRE
	}
	buf := p2.Marshal()
	ok, err := tnlIoctl(c, unix.AF_INET6, tnlsys.SIOCGETTUNNEL, li.Name, buf, tnlGetMsg(li.Name))
	if err != nil || !ok {
		return false, err
	}
	q, err := xtcpnl.ParseIP6TnlParm2(buf)
	if err != nil {
		return false, err
	}
	if !tnlMatchV6(p1, &q) {
		return false, nil
	}
	render.PrintTunnelV6(pr, &render.TunnelV6{Parm: q, Dev: tnlIndexName(c, q.Link)}, tnlSL(c))
	return true, nil
}

// tnlMatchV6 is ip6_tnl_parm_match.
func tnlMatchV6(p1, p2 *xtcpnl.IP6TnlParm2) bool {
	const tc, fl = ip6FlowinfoTclassCst, ip6FlowinfoFlowlabelCst
	return (p1.Link == 0 || p1.Link == p2.Link) &&
		(p1.Name == "" || p1.Name == p2.Name) &&
		(!addrSet(p1.Laddr) || p1.Laddr == p2.Laddr) &&
		(!addrSet(p1.Raddr) || p1.Raddr == p2.Raddr) &&
		(p1.Proto == 0 || p2.Proto == 0 || p1.Proto == p2.Proto) &&
		(p1.EncapLimit == 0 || p1.EncapLimit == p2.EncapLimit) &&
		(p1.HopLimit == 0 || p1.HopLimit == p2.HopLimit) &&
		(p1.FlowInfo&tc == 0 || (p1.FlowInfo^p2.FlowInfo)&tc == 0) &&
		(p1.FlowInfo&fl == 0 || (p1.FlowInfo^p2.FlowInfo)&fl == 0) &&
		(p1.Flags == 0 || p1.Flags&p2.Flags != 0)
}
