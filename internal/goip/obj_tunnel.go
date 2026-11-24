package goip

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/internal/goip/service"
	"github.com/randomizedcoder/xtcp2/internal/tnlsys"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip tunnel` is do_iptunnel (ip/iptunnel.c, iproute2 v7.2.0), and `ip -6
// tunnel` do_ip6tunnel (ip/ip6tunnel.c), over ip/tunnel.c:
//
//   - a pre-scan of the arguments: a "mode" whose word is an IPv6 mode
//     switches to do_ip6tunnel, whatever -4 said (T10);
//   - the verb by prefix; no verb is show;
//   - show: parse_args, then either one SIOCGETTUNNEL on the named or
//     fallback device, printed as text even under -j (no new_json_obj), or
//     do_tunnels_list: a link dump of the family, one SIOCGETTUNNEL per
//     tunnel of the family's ARPHRDs, the match against the arguments and
//     print_tunnel, each followed by a bare newline, under -j too.
//
// The ioctls go through c.sys (internal/tnlsys): the kernel live, or the
// row's recorded sidecar under replay. The writes, add, change, delete,
// prl and 6rd, are ioctls goip does not make.

var (
	// ErrTunnelUsage is a refusal iptunnel.c, ip6tunnel.c or iptuntap.c
	// ends with exit(-1), 255: usage, an unknown command or mode, invarg,
	// duparg2 and incomplete_command.
	ErrTunnelUsage = errors.New(objTunnelCst)
	// ErrTunnelRefused is a show that returns -1, exit 1: a refused
	// named ioctl, keys with ipip or sit, a broadcast tunnel with no
	// source, and a dump it cannot finish ("Dump terminated").
	ErrTunnelRefused = errors.New("tunnel refused")
)

const (
	tnlModeCst = "mode"
	tnlNameCst = "name"
	tnlHelpCst = "help"
	// tnlInheritCst is the word ttl, tos, tclass, flowlabel and dscp take.
	tnlInheritCst = "inherit"
	tnlTclassCst  = "tclass"
	tnlDsfieldCst = "dsfield"

	// The IPv6 mode words both parse_args and the pre-scan read.
	tnlModeIP6IP6Cst = "ip6ip6"
	tnlModeIPv6v6Cst = "ipv6/ipv6"
	tnlModeIP4IP6Cst = "ip4ip6"
	tnlModeIPIP6wCst = "ipip6"
	tnlModeIPv4v6Cst = "ipv4/ipv6"
	tnlModeGREv6Cst  = "gre/ipv6"
	tnlModeIPIP6Cst  = "ip/ipv6"
	tnlModeAnyV6Cst  = "any/ipv6"

	// The one-sided seq and csum words.
	tnlISeqCst  = "iseq"
	tnlOSeqCst  = "oseq"
	tnlICsumCst = "icsum"
	tnlOCsumCst = "ocsum"

	tunnelUsageCst = "Usage: ip tunnel { add | change | del | show | prl | 6rd } [ NAME ]\n" +
		"        [ mode { gre | ipip | isatap | sit | vti } ]\n" +
		"        [ remote ADDR ] [ local ADDR ]\n" +
		"        [ [i|o]seq ] [ [i|o]key KEY ] [ [i|o]csum ]\n" +
		"        [ prl-default ADDR ] [ prl-nodefault ADDR ] [ prl-delete ADDR ]\n" +
		"        [ 6rd-prefix ADDR ] [ 6rd-relay_prefix ADDR ] [ 6rd-reset ]\n" +
		"        [ ttl TTL ] [ tos TOS ] [ [no]pmtudisc ] [ dev PHYS_DEV ]\n" +
		"\n" +
		"Where: NAME := STRING\n" +
		"       ADDR := { IP_ADDRESS | any }\n" +
		"       TOS  := { STRING | 00..ff | inherit | inherit/STRING | inherit/00..ff }\n" +
		"       TTL  := { 1..255 | inherit }\n" +
		"       KEY  := { DOTTED_QUAD | NUMBER }"

	ip6TunnelUsageCst = "Usage: ip -f inet6 tunnel { add | change | del | show } [ NAME ]\n" +
		"          [ mode { ip6ip6 | ipip6 | ip6gre | vti6 | any } ]\n" +
		"          [ remote ADDR local ADDR ] [ dev PHYS_DEV ]\n" +
		"          [ encaplimit ELIM ]\n" +
		"          [ hoplimit TTL ] [ tclass TCLASS ] [ flowlabel FLOWLABEL ]\n" +
		"          [ dscp inherit ]\n" +
		"          [ [no]allow-localremote ]\n" +
		"          [ [i|o]seq ] [ [i|o]key KEY ] [ [i|o]csum ]\n" +
		"\n" +
		"Where: NAME      := STRING\n" +
		"       ADDR      := IPV6_ADDRESS\n" +
		"       ELIM      := { none | 0..255 }(default=4)\n" +
		"       TTL       := 0..255 (default=64)\n" +
		"       TCLASS    := { 0x0..0xff | inherit }\n" +
		"       FLOWLABEL := { 0x0..0xfffff | inherit }\n" +
		"       KEY       := { DOTTED_QUAD | NUMBER }"
)

// The protocols a tunnel struct carries.
const (
	tnlProtoIPIP uint8 = unix.IPPROTO_IPIP
	tnlProtoGRE  uint8 = unix.IPPROTO_GRE
	tnlProtoIPv6 uint8 = unix.IPPROTO_IPV6
)

// ipDFCst is IP_DF, frag_off's don't-fragment bit, host order.
const ipDFCst = 0x4000

// tnlMulticast is IN_MULTICAST(ntohl(a)).
func tnlMulticast(a netip.Addr) bool {
	return a.Is4() && a.As4()[0]&0xf0 == 0xe0
}

// tnlUsage wraps a refusal as ErrTunnelUsage's exit(-1).
func tnlUsage(err error) error {
	return fmt.Errorf("%w: %w", ErrTunnelUsage, err)
}

// tnlUsageMsg is ErrTunnelUsage with ip's line, which may end in a period.
func tnlUsageMsg(line string) error {
	return fmt.Errorf("%w: %s", ErrTunnelUsage, line)
}

// tnlNext is NEXT_ARG, whose incomplete_command exits -1.
func tnlNext(args []string, i int) (int, error) {
	j, err := nextArg(args, i)
	if err != nil {
		return i, tnlUsage(err)
	}
	return j, nil
}

// tnlSL is _SL_: a backslash under -o, else a newline.
func tnlSL(c *runCtx) string {
	if c.oneline {
		return "\\"
	}
	return "\n"
}

// tunnelModeIsIPv6 is tunnel_mode_is_ipv6.
func tunnelModeIsIPv6(mode string) bool {
	switch mode {
	case tnlModeIPv6v6Cst, tnlModeIP6IP6Cst, kindVti6Cst, tnlModeIPIP6Cst, tnlModeIPv4v6Cst, tnlModeIPIP6wCst, tnlModeIP4IP6Cst, kindIP6GreCst,
		tnlModeGREv6Cst, tnlModeAnyV6Cst, addrNameAnyCst:
		return true
	}
	return false
}

// runTunnel is do_iptunnel.
func runTunnel(c *runCtx, args []string) error {
	fam := c.family
	for i := 0; i < len(args)-1; i++ {
		if args[i] == tnlModeCst {
			if tunnelModeIsIPv6(args[i+1]) {
				fam = unix.AF_INET6
			}
			break
		}
	}
	switch fam {
	case unix.AF_UNSPEC, unix.AF_INET:
	case unix.AF_INET6:
		return runIP6Tunnel(c, args)
	default:
		return fmt.Errorf("%w: Unsupported protocol family: %d", ErrTunnelUsage, fam)
	}
	if len(args) == 0 {
		return tunnelShowV4(c, nil)
	}
	verb, rest := args[0], args[1:]
	switch {
	case matchesPrefix(verb, addrVerbAddCst), matchesPrefix(verb, addrVerbChangeCst),
		matchesPrefix(verb, addrVerbDeleteCst):
		return fmt.Errorf("tunnel %q: an ioctl write: %w", verb, ErrNotImplemented)
	case matchesPrefix(verb, showVerbCst), matchesPrefix(verb, lstVerbCst), matchesPrefix(verb, listVerbCst):
		return tunnelShowV4(c, rest)
	case matchesPrefix(verb, "prl"), matchesPrefix(verb, "6rd"):
		return fmt.Errorf("tunnel %q: an ioctl write: %w", verb, ErrNotImplemented)
	case matchesPrefix(verb, tnlHelpCst):
		return fmt.Errorf("%w: %s", ErrTunnelUsage, tunnelUsageCst)
	}
	return fmt.Errorf("%w: Command %q is unknown, try \"ip tunnel help\"", ErrTunnelUsage, verb)
}

// runIP6Tunnel is do_ip6tunnel, reached through do_iptunnel.
func runIP6Tunnel(c *runCtx, args []string) error {
	if len(args) == 0 {
		return tunnelShowV6(c, nil)
	}
	verb, rest := args[0], args[1:]
	switch {
	case matchesPrefix(verb, addrVerbAddCst), matchesPrefix(verb, addrVerbChangeCst),
		matchesPrefix(verb, addrVerbDeleteCst):
		return fmt.Errorf("tunnel %q: an ioctl write: %w", verb, ErrNotImplemented)
	case matchesPrefix(verb, showVerbCst), matchesPrefix(verb, lstVerbCst), matchesPrefix(verb, listVerbCst):
		return tunnelShowV6(c, rest)
	case matchesPrefix(verb, tnlHelpCst):
		return fmt.Errorf("%w: %s", ErrTunnelUsage, ip6TunnelUsageCst)
	}
	return tnlUsageMsg(fmt.Sprintf("Command %q is unknown, try \"ip -f inet6 tunnel help\".", verb))
}

// tnlParseKey is tnl_parse_key (ip/tunnel.c:164-178): a value with a '.' is
// get_addr32's address, read big-endian, whose failure is exit(1);
// anything else is get_unsigned base 0, whose failure is exit(-1) naming
// the keyword.
func tnlParseKey(name, v string) (uint64, error) {
	if strings.IndexByte(v, '.') >= 0 {
		a, ok := getAddrIPv4Parts(v)
		if !ok {
			return 0, fmt.Errorf("%w: Error: an IP address is expected rather than %q", ErrInvalidArgument, v)
		}
		b := a.As4()
		return uint64(binary.BigEndian.Uint32(b[:])), nil
	}
	n, ok := getU32(v, 0)
	if !ok {
		return 0, linkUsage(fmt.Errorf("%w: invalid value for %q: %q; it should be an unsigned integer",
			ErrInvalidArgument, name, v))
	}
	return uint64(n), nil
}

// tnlParseName is parse_args' last arm: name takes the next word, a
// prefix of help is usage, then duparg2 and get_ifname.
func tnlParseName(args []string, i int, have, usage string) (string, int, error) {
	var err error
	if args[i] == tnlNameCst {
		if i, err = tnlNext(args, i); err != nil {
			return "", i, err
		}
	} else if matchesPrefix(args[i], tnlHelpCst) {
		return "", i, fmt.Errorf("%w: %s", ErrTunnelUsage, usage)
	}
	if have != "" {
		return "", i, tnlUsage(duparg2Err(tnlNameCst, args[i]))
	}
	if !checkIfname(args[i]) {
		return "", i, tnlUsage(invargErr("\"name\" not a valid ifname", args[i]))
	}
	return args[i], i, nil
}

// tnlKeyWord is parse_args' key, ikey and okey: the flags it sets and the
// key, which tnl_parse_key reads.
func tnlKeyWord(args []string, i int, iflags, oflags *uint16, ikey, okey *uint32) (int, error) {
	w := args[i]
	i, err := tnlNext(args, i)
	if err != nil {
		return i, err
	}
	k, err := tnlParseKey(w, args[i])
	if err != nil {
		return i, err
	}
	v := uint32(k)
	if w != tnlKeyOkey {
		*iflags |= xtcpnl.GreFlagKey
		*ikey = v
	}
	if w != tnlKeyIkey {
		*oflags |= xtcpnl.GreFlagKey
		*okey = v
	}
	return i, nil
}

// tnlFlagWords are parse_args' seq and csum words, which take no value:
// the GRE bit and whether each sets it in i_flags and o_flags.
var tnlFlagWords = map[string]struct {
	bit     uint16
	in, out bool
}{
	"seq":       {xtcpnl.GreFlagSeq, true, true},
	tnlISeqCst:  {xtcpnl.GreFlagSeq, true, false},
	tnlOSeqCst:  {xtcpnl.GreFlagSeq, false, true},
	lwtKeyCsum:  {xtcpnl.GreFlagCsum, true, true},
	tnlICsumCst: {xtcpnl.GreFlagCsum, true, false},
	tnlOCsumCst: {xtcpnl.GreFlagCsum, false, true},
}

// tnlFlagWord sets a seq or csum word's bits; false for any other word.
func tnlFlagWord(w string, iflags, oflags *uint16) bool {
	f, ok := tnlFlagWords[w]
	if !ok {
		return false
	}
	if f.in {
		*iflags |= f.bit
	}
	if f.out {
		*oflags |= f.bit
	}
	return true
}

// tnlDev resolves parse_args' dev: ll_name_to_index, whose miss is nodev's
// returned -1. A replay has no single-get; its dump fallback's miss is the
// kernel's ENODEV.
func tnlDev(c *runCtx, dev string) (int32, error) {
	idx, err := genlNameToIndex(c, dev)
	if service.IsLinkNotFound(err) {
		idx, err = 0, nil
	}
	if err != nil {
		return 0, err
	}
	if idx == 0 {
		return 0, nodevErr(dev)
	}
	return idx, nil
}

// tnlSetProto is set_tunnel_proto.
func tnlSetProto(p *xtcpnl.IPTunnelParm, proto uint8) error {
	if p.Protocol != 0 && p.Protocol != proto {
		return tnlUsageMsg("You managed to ask for more than one tunnel mode.")
	}
	p.Protocol = proto
	return nil
}

// tnlModeV4 is parse_args' mode word.
func tnlModeV4(p *xtcpnl.IPTunnelParm, mode string, isatap *bool) error {
	switch mode {
	case "ipip", "ip/ip":
		return tnlSetProto(p, tnlProtoIPIP)
	case "gre", "gre/ip":
		return tnlSetProto(p, tnlProtoGRE)
	case "sit", "ipv6/ip":
		return tnlSetProto(p, tnlProtoIPv6)
	case "isatap":
		*isatap = true
		return tnlSetProto(p, tnlProtoIPv6)
	case "vti":
		if err := tnlSetProto(p, tnlProtoIPIP); err != nil {
			return err
		}
		p.IFlags |= xtcpnl.VtiIsVti
		return nil
	}
	return fmt.Errorf("%w: Unknown tunnel mode %q", ErrTunnelUsage, mode)
}

// tnlAddr32 is get_addr32, whose failure is exit(1).
func tnlAddr32(v string) (netip.Addr, error) {
	a, ok := getAddr1(v, unix.AF_INET)
	if !ok {
		return netip.Addr{}, fmt.Errorf("%w: Error: an IP address is expected rather than \"%s\"", ErrInvalidArgument, v)
	}
	var b [4]byte
	copy(b[:], a.data)
	return netip.AddrFrom4(b), nil
}

// tnlTOS is parse_args' tos value: strsep at the first '/' cuts the word;
// "inherit" sets bit 0 and reads what followed the slash, anything else
// reads the cut word and drops what followed.
func tnlTOS(arg string) (uint8, error) {
	head, tail, slash := arg, "", false
	for i := 0; i < len(arg); i++ {
		if arg[i] == '/' {
			head, tail, slash = arg[:i], arg[i+1:], true
			break
		}
	}
	var tos uint8
	ds, have := head, true
	if head == tnlInheritCst {
		tos, ds, have = 1, tail, slash
	}
	if have {
		v, ok := rtnlDsfieldA2N(ds)
		if !ok {
			return 0, tnlUsage(invargErr("bad TOS value", head))
		}
		tos |= v
	}
	return tos, nil
}

// tnlV4TakesValue is whether iptunnel.c parse_args' word w takes NEXT_ARG.
func tnlV4TakesValue(w string) bool {
	switch w {
	case tnlModeCst, kindKeyRemote, addrLocalKeywordCst, devKeywordCst, kindKeyTTL, kindKeyHoplimit, tnlKeyHlim,
		tosKeywordCst, tnlTclassCst:
		return true
	}
	return matchesPrefix(w, tnlDsfieldCst)
}

// tnlV4Value is parse_args' arms that take a value, past NEXT_ARG: dev's
// device is returned for after the loop.
func tnlV4Value(p *xtcpnl.IPTunnelParm, w, v string, isatap *bool) (medium string, err error) {
	switch w {
	case tnlModeCst:
		return "", tnlModeV4(p, v, isatap)
	case kindKeyRemote:
		p.Daddr, err = tnlAddr32(v)
	case addrLocalKeywordCst:
		p.Saddr, err = tnlAddr32(v)
	case devKeywordCst:
		return v, nil
	case kindKeyTTL, kindKeyHoplimit, tnlKeyHlim:
		if v != tnlInheritCst {
			u, ok := getU8(v, 0)
			if !ok {
				return "", tnlUsage(invargErr("invalid TTL\n", v))
			}
			p.TTL = u
		}
	default: // tos, tclass and a prefix of dsfield
		p.TOS, err = tnlTOS(v)
	}
	return "", err
}

// parseTunnelV4 is iptunnel.c parse_args for SIOCGETTUNNEL.
func parseTunnelV4(c *runCtx, args []string) (xtcpnl.IPTunnelParm, error) {
	p := xtcpnl.IPTunnelParm{VersionIHL: 0x45, FragOff: ipDFCst}
	var medium string
	var isatap bool
	var err error
	for i := 0; i < len(args); i++ {
		w := args[i]
		switch {
		case w == tnlKeyKey || w == tnlKeyIkey || w == tnlKeyOkey:
			if i, err = tnlKeyWord(args, i, &p.IFlags, &p.OFlags, &p.IKey, &p.OKey); err != nil {
				return p, err
			}
		case tnlFlagWord(w, &p.IFlags, &p.OFlags):
		case w == "nopmtudisc":
			p.FragOff = 0
		case w == "pmtudisc":
			p.FragOff = ipDFCst
		case tnlV4TakesValue(w):
			if i, err = tnlNext(args, i); err != nil {
				return p, err
			}
			m, err := tnlV4Value(&p, w, args[i], &isatap)
			if err != nil {
				return p, err
			}
			if m != "" {
				medium = m
			}
		default:
			if p.Name, i, err = tnlParseName(args, i, p.Name, tunnelUsageCst); err != nil {
				return p, err
			}
		}
	}
	return p, tnlParseV4Tail(c, &p, medium, isatap)
}

// tnlParseV4Tail is parse_args after its loop: the protocol a name prefix
// implies, the key check, dev, a multicast remote's keys, and isatap.
func tnlParseV4Tail(c *runCtx, p *xtcpnl.IPTunnelParm, medium string, isatap bool) error {
	if p.Protocol == 0 {
		n := p.Name
		switch {
		case hasPrefix(n, "gre"):
			p.Protocol = tnlProtoGRE
		case hasPrefix(n, "ipip"):
			p.Protocol = tnlProtoIPIP
		case hasPrefix(n, "sit"):
			p.Protocol = tnlProtoIPv6
		case hasPrefix(n, "isatap"):
			p.Protocol = tnlProtoIPv6
			isatap = true
		case hasPrefix(n, "vti"):
			p.Protocol = tnlProtoIPIP
			p.IFlags |= xtcpnl.VtiIsVti
		}
	}
	if (p.IFlags|p.OFlags)&xtcpnl.GreFlagKey != 0 && p.IFlags&xtcpnl.VtiIsVti == 0 && p.Protocol != tnlProtoGRE {
		return fmt.Errorf("%w: Keys are not allowed with ipip and sit tunnels", ErrTunnelRefused)
	}
	if medium != "" {
		idx, err := tnlDev(c, medium)
		if err != nil {
			return err
		}
		p.Link = idx
	}
	if tnlMulticast(p.Daddr) {
		d := p.Daddr.As4()
		k := binary.BigEndian.Uint32(d[:])
		if p.IKey == 0 {
			p.IKey = k
			p.IFlags |= xtcpnl.GreFlagKey
		}
		if p.OKey == 0 {
			p.OKey = k
			p.OFlags |= xtcpnl.GreFlagKey
		}
		if !p.Saddr.IsValid() || p.Saddr.IsUnspecified() {
			return fmt.Errorf("%w: A broadcast tunnel requires a source address", ErrTunnelRefused)
		}
	}
	if isatap {
		p.IFlags |= xtcpnl.SitIsatap
	}
	return nil
}

// hasPrefix is memcmp(name, prefix, len(prefix)) == 0 over a zeroed
// name[IFNAMSIZ].
func hasPrefix(name, prefix string) bool {
	return len(name) >= len(prefix) && name[:len(prefix)] == prefix
}

// tnlDefname is tnl_defname: the fallback device of the protocol, or ""
// for the list.
func tnlDefname(p *xtcpnl.IPTunnelParm) string {
	switch p.Protocol {
	case tnlProtoIPIP:
		if p.IFlags&xtcpnl.VtiIsVti != 0 {
			return "ip_vti0"
		}
		return "tunl0"
	case tnlProtoGRE:
		return "gre0"
	case tnlProtoIPv6:
		return "sit0"
	}
	return ""
}

// tnlIoctl makes one ioctl through c.sys. ok is false for a refusal, after
// msg's line for a kernel errno (msg returns "" to stay silent) or
// tnl_*_ioctl's socket line; err is a source error, a replay gap.
func tnlIoctl(c *runCtx, family int, req uint32, name string, arg []byte,
	msg func(unix.Errno) string) (bool, error) {
	_, err := c.sys.Ioctl(family, req, name, arg)
	if err == nil {
		return true, nil
	}
	var se *tnlsys.SocketError
	if errors.As(err, &se) {
		var e unix.Errno
		if errors.As(se.Err, &e) {
			_, werr := fmt.Fprintf(c.errOut, "create socket failed: %s\n", cStrerror(e))
			return false, werr
		}
		return false, err
	}
	var e unix.Errno
	if !errors.As(err, &e) {
		return false, err
	}
	if s := msg(e); s != "" {
		if _, werr := io.WriteString(c.errOut, s); werr != nil {
			return false, werr
		}
	}
	return false, nil
}

// tnlGetMsg is tnl_get_ioctl's refusal line.
func tnlGetMsg(name string) func(unix.Errno) string {
	return func(e unix.Errno) string {
		return fmt.Sprintf("get tunnel \"%s\" failed: %s\n", name, cStrerror(e))
	}
}

// tnlNoSkip is tnl_prl_ioctl's skiperr, -1: no errno is silent.
const tnlNoSkip unix.Errno = 0

// tnlGenMsg is tnl_gen_ioctl's refusal line, silent for skip.
func tnlGenMsg(name string, req uint32, skip unix.Errno) func(unix.Errno) string {
	return func(e unix.Errno) string {
		if e == skip {
			return ""
		}
		return fmt.Sprintf("%s: ioctl %x failed: %s\n", name, req, cStrerror(e))
	}
}

// tnlIndexName is ll_index_to_name for a tunnel's link.
func tnlIndexName(c *runCtx, link int32) string {
	if link == 0 {
		return ""
	}
	return genlIndexToName(c, link)
}

// tunnelViewV4 asks what print_tunnel asks of one v4 tunnel: the routers
// of an isatap tunnel, the link's name, a sit tunnel's 6rd prefix.
func tunnelViewV4(c *runCtx, p xtcpnl.IPTunnelParm) (*render.TunnelV4, error) {
	t := &render.TunnelV4{Parm: p}
	if p.Protocol == tnlProtoIPv6 && p.IFlags&xtcpnl.SitIsatap != 0 {
		buf := xtcpnl.TunnelPRLRequest(16)
		ok, err := tnlIoctl(c, unix.AF_INET, tnlsys.SIOCGETPRL, p.Name, buf, tnlGenMsg(p.Name, tnlsys.SIOCGETPRL, tnlNoSkip))
		if err != nil {
			return nil, err
		}
		if ok {
			if t.PRL, err = xtcpnl.ParseTunnelPRL(buf); err != nil {
				return nil, err
			}
		}
	}
	t.Dev = tnlIndexName(c, p.Link)
	if p.Protocol == tnlProtoIPv6 {
		buf := make([]byte, xtcpnl.Tunnel6RDSize)
		ok, err := tnlIoctl(c, unix.AF_INET, tnlsys.SIOCGET6RD, p.Name, buf,
			tnlGenMsg(p.Name, tnlsys.SIOCGET6RD, unix.EINVAL))
		if err != nil {
			return nil, err
		}
		if ok {
			r, err := xtcpnl.ParseTunnel6RD(buf)
			if err != nil {
				return nil, err
			}
			t.SixRD = &r
		}
	}
	return t, nil
}

// tunnelShowV4 is iptunnel.c do_show.
func tunnelShowV4(c *runCtx, args []string) error {
	p, err := parseTunnelV4(c, args)
	if err != nil {
		return err
	}
	basedev := tnlDefname(&p)
	if basedev == "" {
		return tunnelsList(c, unix.AF_INET, p.Name, func(pr *render.Printer, li *xtcpnl.LinkInfo) (bool, error) {
			return tunnelListV4(c, pr, &p, li)
		})
	}
	name := p.Name
	if name == "" {
		name = basedev
	}
	buf := p.Marshal()
	ok, err := tnlIoctl(c, unix.AF_INET, tnlsys.SIOCGETTUNNEL, name, buf, tnlGetMsg(name))
	if err != nil {
		return err
	}
	if !ok {
		return ErrTunnelRefused
	}
	q, err := xtcpnl.ParseIPTunnelParm(buf)
	if err != nil {
		return err
	}
	t, err := tunnelViewV4(c, q)
	if err != nil {
		return err
	}
	// No new_json_obj on this path: print_tunnel's JSON calls find no
	// writer and print text, under -j too.
	pr := render.NewPrinter(false)
	render.PrintTunnelV4(pr, t, &render.Dsfield{}, tnlSL(c))
	pr.Raw("\n")
	_, err = c.out.Write(pr.Bytes())
	return err
}

// tunnelListV4 is print_nlmsg_tunnel's per-tunnel part for v4: the zeroed
// parm, its ioctl, ip_tunnel_parm_match and print_tunnel.
func tunnelListV4(c *runCtx, pr *render.Printer, p1 *xtcpnl.IPTunnelParm, li *xtcpnl.LinkInfo) (bool, error) {
	buf := make([]byte, xtcpnl.IPTunnelParmSize)
	ok, err := tnlIoctl(c, unix.AF_INET, tnlsys.SIOCGETTUNNEL, li.Name, buf, tnlGetMsg(li.Name))
	if err != nil || !ok {
		return false, err
	}
	p2, err := xtcpnl.ParseIPTunnelParm(buf)
	if err != nil {
		return false, err
	}
	if !tnlMatchV4(p1, &p2) {
		return false, nil
	}
	t, err := tunnelViewV4(c, p2)
	if err != nil {
		return false, err
	}
	render.PrintTunnelV4(pr, t, &render.Dsfield{}, tnlSL(c))
	return true, nil
}

// addrSet is an address the match tests: one that is not zero.
func addrSet(a netip.Addr) bool {
	return a.IsValid() && !a.IsUnspecified()
}

// tnlMatchV4 is ip_tunnel_parm_match.
func tnlMatchV4(p1, p2 *xtcpnl.IPTunnelParm) bool {
	return (p1.Link == 0 || p1.Link == p2.Link) &&
		(p1.Name == "" || p1.Name == p2.Name) &&
		(!addrSet(p1.Daddr) || p1.Daddr == p2.Daddr) &&
		(!addrSet(p1.Saddr) || p1.Saddr == p2.Saddr) &&
		(p1.IKey == 0 || p1.IKey == p2.IKey)
}

// tnlArphrdOK is print_nlmsg_tunnel's ARPHRD filter for the family.
func tnlArphrdOK(family int, t uint16) bool {
	if family == unix.AF_INET {
		return t == unix.ARPHRD_TUNNEL || t == unix.ARPHRD_IPGRE || t == unix.ARPHRD_SIT
	}
	return t == unix.ARPHRD_TUNNEL6 || t == unix.ARPHRD_IP6GRE
}

// tunnelsList is do_tunnels_list: the family's link dump, each tunnel of
// the family's ARPHRDs (and p1name, when set) handed to each, which
// prints it or not; then -s's stats block and a bare newline, under -j
// too. A message too short for its ifinfomsg, and a printed tunnel
// with no stats under -s, end the dump: "Dump terminated", rc 1.
func tunnelsList(c *runCtx, family int, p1name string,
	each func(*render.Printer, *xtcpnl.LinkInfo) (bool, error)) error {
	pr := render.NewPrinter(c.json)
	pr.NewObj()
	err := tunnelsWalk(c, pr, family, p1name, each)
	pr.DeleteObj()
	if _, werr := c.out.Write(pr.Bytes()); werr != nil {
		return werr
	}
	return err
}

// tunnelsWalk is do_tunnels_list's rtnl_dump_filter.
func tunnelsWalk(c *runCtx, pr *render.Printer, family int, p1name string,
	each func(*render.Printer, *xtcpnl.LinkInfo) (bool, error)) error {
	fam, ok := tnlFamily(family)
	if !ok {
		return fmt.Errorf("%w: family %d", ErrTunnelUsage, family)
	}
	bodies, err := c.src.Dump(xtcpnl.BuildDumpLinkRequestFamily(fam, c.nextSeq()), unix.RTM_NEWLINK)
	if err != nil {
		return fmt.Errorf("%w: Dump terminated: %w", ErrTunnelRefused, err)
	}
	sl := tnlSL(c)
	for _, body := range bodies {
		li, err := xtcpnl.ParseNewLink(body)
		if err != nil {
			return fmt.Errorf("%w: Dump terminated: %w", ErrTunnelRefused, err)
		}
		if !tnlArphrdOK(family, li.Type) || li.Name == "" || (p1name != "" && p1name != li.Name) {
			continue
		}
		printed, err := each(pr, &li)
		if err != nil {
			return err
		}
		if !printed {
			continue
		}
		if c.showStats > 0 {
			if li.Stats == nil {
				return fmt.Errorf("%w: Dump terminated", ErrTunnelRefused)
			}
			pr.Raw(render.TunnelStats(li.Stats, sl))
		}
		pr.Raw("\n")
	}
	return nil
}

// tnlFamily is the AF_* as ifi_family.
func tnlFamily(family int) (uint8, bool) {
	switch family {
	case unix.AF_INET:
		return unix.AF_INET, true
	case unix.AF_INET6:
		return unix.AF_INET6, true
	}
	return 0, false
}
