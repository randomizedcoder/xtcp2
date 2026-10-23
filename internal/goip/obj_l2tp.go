package goip

import (
	"errors"
	"fmt"
	"io"
	"net/netip"
	"strings"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// `ip l2tp` is do_ipl2tp (ip/ipl2tp.c): usage with no argument or help by
// prefix, then genl_init_handle's l2tp lookup, then the verb by prefix,
// add, delete, or show, lst and list. Each runs parse_args, whose words
// are strcmp's, `tunnel` and `session` among them, and whose state starts
// zeroed but for its defaults:
//
//   - add: its missargs, then TUNNEL_CREATE for a tunnel and
//     SESSION_CREATE for a session, both for both, each rtnl_talk's with
//     NLM_F_ACK;
//   - delete: SESSION_DELETE when a session id was given, else
//     TUNNEL_DELETE;
//   - show: SESSION_GET for a session, else TUNNEL_GET, always a dump, the
//     ids in the request, each answer read by get_response over one struct
//     that starts as the arguments' and printed, under -j as one array.
//
// goip sends the lookup and the dump, and writes an add's or a delete's
// requests to GOIP_WRITE_OUT, an add of both as the two in one file.

const (
	l2tpHelpCst    = "help"
	l2tpAddCst     = "add"
	l2tpDeleteCst  = "delete"
	l2tpShowCst    = "show"
	l2tpLstCst     = "lst"
	l2tpListCst    = "list"
	l2tpTunnelCst  = "tunnel"
	l2tpSessionCst = "session"
	l2tpOnCst      = "on"
	l2tpOffCst     = "off"
	l2tpNoneCst    = "none"
	l2tpEncapCst   = "encap"
	// l2tpReqBufCst is GENL_REQUEST's 1024, the most room ipl2tp.c gives
	// a request.
	l2tpReqBufCst = 1024
	// l2tpProtoVersionCst is the L2TP version create_tunnel sends, 3.
	l2tpProtoVersionCst = 3
	// l2tpL2SpecLenCst is the default layer 2 header's length.
	l2tpL2SpecLenCst = 4
	// l2tpUsageCst is usage()'s text.
	l2tpUsageCst = "Usage: ip l2tp add tunnel\n" +
		"          remote ADDR local ADDR\n" +
		"          tunnel_id ID peer_tunnel_id ID\n" +
		"          [ encap { ip | udp } ]\n" +
		"          [ udp_sport PORT ] [ udp_dport PORT ]\n" +
		"          [ udp_csum { on | off } ]\n" +
		"          [ udp6_csum_tx { on | off } ]\n" +
		"          [ udp6_csum_rx { on | off } ]\n" +
		"Usage: ip l2tp add session [ name NAME ]\n" +
		"          tunnel_id ID\n" +
		"          session_id ID peer_session_id ID\n" +
		"          [ cookie HEXSTR ] [ peer_cookie HEXSTR ]\n" +
		"          [ seq { none | send | recv | both } ]\n" +
		"          [ l2spec_type L2SPEC ]\n" +
		"       ip l2tp del tunnel tunnel_id ID\n" +
		"       ip l2tp del session tunnel_id ID session_id ID\n" +
		"       ip l2tp show tunnel [ tunnel_id ID ]\n" +
		"       ip l2tp show session [ tunnel_id ID ] [ session_id ID ]\n" +
		"\n" +
		"Where: NAME   := STRING\n" +
		"       ADDR   := { IP_ADDRESS | any }\n" +
		"       PORT   := { 0..65535 }\n" +
		"       ID     := { 1..4294967295 }\n" +
		"       HEXSTR := { 8 or 16 hex digits (4 / 8 bytes) }\n" +
		"       L2SPEC := { none | default }"
)

var (
	// ErrL2tpUsage is a refusal ipl2tp.c ends with exit(-1), 255: usage,
	// an unknown command or word, invarg, missarg, incomplete_command, and
	// an encapsulation, layer 2 header type or sequencing it does not know.
	ErrL2tpUsage = errors.New("l2tp")
	// ErrL2tpDumpTerminated is a dump the kernel refuses or an answer
	// get_response refuses: "Dump terminated", exit(1).
	ErrL2tpDumpTerminated = errors.New("dump terminated")
)

// l2tpUsage marks err as one of ErrL2tpUsage's refusals.
func l2tpUsage(err error) error {
	return fmt.Errorf("%w: %w", ErrL2tpUsage, err)
}

// l2tpMissarg is missarg, exit(-1).
func l2tpMissarg(key string) error {
	return fmt.Errorf("%w: Error: argument %q is required", ErrL2tpUsage, key)
}

// l2tpListVerbs are do_ipl2tp's spellings of show, each by prefix.
var l2tpListVerbs = []string{l2tpShowCst, l2tpLstCst, l2tpListCst}

// runL2tp is do_ipl2tp.
func runL2tp(c *runCtx, args []string) error {
	if len(args) == 0 || matchesPrefix(args[0], l2tpHelpCst) {
		return fmt.Errorf("%w: %s", ErrL2tpUsage, l2tpUsageCst)
	}
	id, err := c.genlFamily(xtcpnl.L2TPGenlName)
	if err != nil {
		return err
	}
	verb, rest := args[0], args[1:]
	switch {
	case matchesPrefix(verb, l2tpAddCst):
		return l2tpAdd(c, id, rest)
	case matchesPrefix(verb, l2tpDeleteCst):
		return l2tpDel(c, id, rest)
	case matchesAny(verb, l2tpListVerbs):
		return l2tpShow(c, id, rest)
	}
	return fmt.Errorf("%w: Command %q is unknown, try \"ip l2tp help\"", ErrL2tpUsage, verb)
}

// l2tpOpt is parse_args' struct l2tp_parm: what show prints, and what the
// requests alone read.
type l2tpOpt struct {
	render.L2tpParm
	tunnel, session       bool
	l2specType, l2specLen uint8
}

// l2tpParseArgs is parse_args: no argument is usage, then the defaults,
// then each word.
func l2tpParseArgs(c *runCtx, args []string) (*l2tpOpt, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("%w: %s", ErrL2tpUsage, l2tpUsageCst)
	}
	o := &l2tpOpt{l2specType: xtcpnl.L2TPL2SpecDefault, l2specLen: l2tpL2SpecLenCst}
	o.UDP6CsumRx, o.UDP6CsumTx = true, true
	for i := 0; i < len(args); i++ {
		var err error
		if i, err = o.parseArg(c, args, i); err != nil {
			return nil, err
		}
	}
	return o, nil
}

// parseArg is one turn of parse_args' loop: a word that takes a value, by
// strcmp, then tunnel and session, then help by prefix. It returns the
// index of the last argument it used.
func (o *l2tpOpt) parseArg(c *runCtx, args []string, i int) (int, error) {
	arg := args[i]
	if set, ok := l2tpValueWords[arg]; ok {
		if arg == "offset" || arg == "peer_offset" {
			// Its message comes ahead of NEXT_ARG.
			if _, err := fmt.Fprintf(c.errOut, "Ignoring option %q\n", arg); err != nil {
				return i, err
			}
		}
		i, err := nextArg(args, i)
		if err != nil {
			return i, l2tpUsage(err)
		}
		return i, set(o, args[i])
	}
	switch {
	case arg == l2tpTunnelCst:
		o.tunnel = true
	case arg == l2tpSessionCst:
		o.session = true
	case matchesPrefix(arg, l2tpHelpCst):
		return i, fmt.Errorf("%w: %s", ErrL2tpUsage, l2tpUsageCst)
	default:
		return i, fmt.Errorf("%w: Unknown command: %s\n%s", ErrL2tpUsage, arg, l2tpUsageCst)
	}
	return i, nil
}

// l2tpValueWords are parse_args' words that take a value, by strcmp, each
// with what it does with the value. tid, ptid, sid and psid are the ids'
// short names; offset and peer_offset are read and ignored.
var l2tpValueWords = map[string]func(o *l2tpOpt, v string) error{
	l2tpEncapCst:        (*l2tpOpt).encap,
	"name":              (*l2tpOpt).name,
	"remote":            func(o *l2tpOpt, v string) error { return l2tpAddr(&o.Peer, v) },
	addrLocalKeywordCst: func(o *l2tpOpt, v string) error { return l2tpAddr(&o.Local, v) },
	"tunnel_id":         func(o *l2tpOpt, v string) error { return l2tpID(&o.TunnelID, v) },
	"tid":               func(o *l2tpOpt, v string) error { return l2tpID(&o.TunnelID, v) },
	"peer_tunnel_id":    func(o *l2tpOpt, v string) error { return l2tpID(&o.PeerTunnelID, v) },
	"ptid":              func(o *l2tpOpt, v string) error { return l2tpID(&o.PeerTunnelID, v) },
	"session_id":        func(o *l2tpOpt, v string) error { return l2tpID(&o.SessionID, v) },
	"sid":               func(o *l2tpOpt, v string) error { return l2tpID(&o.SessionID, v) },
	"peer_session_id":   func(o *l2tpOpt, v string) error { return l2tpID(&o.PeerSessionID, v) },
	"psid":              func(o *l2tpOpt, v string) error { return l2tpID(&o.PeerSessionID, v) },
	"udp_sport":         func(o *l2tpOpt, v string) error { return l2tpPort(&o.LocalPort, v) },
	"udp_dport":         func(o *l2tpOpt, v string) error { return l2tpPort(&o.PeerPort, v) },
	"udp_csum":          func(o *l2tpOpt, v string) error { return l2tpOnOff(&o.UDPCsum, v, "udp_csum") },
	"udp6_csum_rx":      func(o *l2tpOpt, v string) error { return l2tpOnOff(&o.UDP6CsumRx, v, "udp6_csum_rx") },
	"udp6_csum_tx":      func(o *l2tpOpt, v string) error { return l2tpOnOff(&o.UDP6CsumTx, v, "udp6_csum_tx") },
	"offset":            func(*l2tpOpt, string) error { return nil },
	"peer_offset":       func(*l2tpOpt, string) error { return nil },
	"cookie": func(o *l2tpOpt, v string) error {
		return l2tpParseCookie(&o.Cookie, &o.CookieLen, v)
	},
	"peer_cookie": func(o *l2tpOpt, v string) error {
		return l2tpParseCookie(&o.PeerCookie, &o.PeerCookieLen, v)
	},
	"l2spec_type": (*l2tpOpt).l2spec,
	"seq":         (*l2tpOpt).seq,
}

// encap is encap's value, ip or udp by strcmp.
func (o *l2tpOpt) encap(v string) error {
	switch v {
	case "ip":
		o.Encap = xtcpnl.L2TPEncapIP
	case "udp":
		o.Encap = xtcpnl.L2TPEncapUDP
	default:
		return fmt.Errorf("%w: Unknown tunnel encapsulation %q", ErrL2tpUsage, v)
	}
	return nil
}

// name is name's value, which check_ifname must accept.
func (o *l2tpOpt) name(v string) error {
	if !checkIfname(v) {
		return l2tpUsage(invargErr("\"name\" not a valid ifname", v))
	}
	o.Ifname = &v
	return nil
}

// l2spec is l2spec_type's value, default or none by strcasecmp.
func (o *l2tpOpt) l2spec(v string) error {
	switch strings.ToLower(v) {
	case addrNameDefaultCst:
		o.l2specType, o.l2specLen = xtcpnl.L2TPL2SpecDefault, l2tpL2SpecLenCst
	case l2tpNoneCst:
		o.l2specType, o.l2specLen = xtcpnl.L2TPL2SpecNone, 0
	default:
		return fmt.Errorf("%w: Unknown layer2specific header type %q", ErrL2tpUsage, v)
	}
	return nil
}

// seq is seq's value by strcasecmp: both, recv and send set theirs, none
// clears both.
func (o *l2tpOpt) seq(v string) error {
	switch strings.ToLower(v) {
	case "both":
		o.RecvSeq, o.SendSeq = true, true
	case "recv":
		o.RecvSeq = true
	case "send":
		o.SendSeq = true
	case l2tpNoneCst:
		o.RecvSeq, o.SendSeq = false, false
	default:
		return fmt.Errorf("%w: Unknown seq value %q", ErrL2tpUsage, v)
	}
	return nil
}

// l2tpAddr is remote's and local's value, get_addr of AF_UNSPEC, which
// exits 1 itself on a word it cannot read. any and default leave the
// family AF_UNSPEC, which add's missarg refuses.
func l2tpAddr(dst *render.L2tpAddr, v string) error {
	a, err := routeGetAddr(v, unix.AF_UNSPEC)
	if err != nil {
		return err
	}
	dst.Family = a.family
	copy(dst.Data[:], a.data)
	return nil
}

// l2tpID is an id's value, get_u32 base 0.
func l2tpID(dst *uint32, v string) error {
	n, ok := getU32(v, 0)
	if !ok {
		return l2tpUsage(invargErr("invalid ID\n", v))
	}
	*dst = n
	return nil
}

// l2tpPort is a port's value, get_u16 base 0.
func l2tpPort(dst *uint16, v string) error {
	n, ok := getU16(v, 0)
	if !ok {
		return l2tpUsage(invargErr("invalid port\n", v))
	}
	*dst = n
	return nil
}

// l2tpOnOff is a checksum's value, on or off by strcmp.
func l2tpOnOff(dst *bool, v, key string) error {
	switch v {
	case l2tpOnCst:
		*dst = true
	case l2tpOffCst:
		*dst = false
	default:
		return l2tpUsage(invargErr("invalid option for "+key+"\n", v))
	}
	return nil
}

// l2tpParseCookie is a cookie's value: 8 or 16 characters, then hex2mem's
// pairs of hex digits.
func l2tpParseCookie(dst *[8]byte, n *int, v string) error {
	if len(v) != 8 && len(v) != 16 {
		return l2tpUsage(invargErr("cookie must be either 8 or 16 hex digits\n", v))
	}
	*n = len(v) / 2
	for i := 0; i < *n; i++ {
		hi, lo := l2tpHex(v[2*i]), l2tpHex(v[2*i+1])
		if hi < 0 || lo < 0 {
			return l2tpUsage(invargErr("cookie must be a hex string\n", v))
		}
		dst[i] = byte(hi<<4 | lo)
	}
	return nil
}

// l2tpHex is get_hex: a hex digit's value, or -1.
func l2tpHex(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// l2tpAdd is do_add: the missargs in its order, then the tunnel's request
// and the session's, each when asked for.
func l2tpAdd(c *runCtx, id uint16, args []string) error {
	o, err := l2tpParseArgs(c, args)
	if err != nil {
		return err
	}
	if err := o.addMissarg(); err != nil {
		return err
	}
	var out []byte
	if o.tunnel {
		attrs, err := o.tunnelAttrs()
		if err != nil {
			return err
		}
		if out, err = l2tpAppendReq(c, out, id, xtcpnl.L2TPCmdTunnelCreate, attrs); err != nil {
			return err
		}
	}
	if o.session {
		attrs, err := o.sessionAttrs()
		if err != nil {
			return err
		}
		if out, err = l2tpAppendReq(c, out, id, xtcpnl.L2TPCmdSessionCreate, attrs); err != nil {
			return err
		}
	}
	return writeAddrOut(out)
}

// addMissarg is do_add's checks: tunnel or session, the tunnel id, a
// session's two ids, a tunnel's peer id, addresses and, for UDP, ports.
func (o *l2tpOpt) addMissarg() error {
	switch {
	case !o.tunnel && !o.session:
		return l2tpMissarg("tunnel or session")
	case o.TunnelID == 0:
		return l2tpMissarg("tunnel_id")
	case o.session && o.PeerSessionID == 0:
		return l2tpMissarg("peer_session_id")
	case o.session && o.SessionID == 0:
		return l2tpMissarg("session_id")
	case !o.tunnel:
		return nil
	case o.PeerTunnelID == 0:
		return l2tpMissarg("peer_tunnel_id")
	case o.Local.Family == unix.AF_UNSPEC:
		return l2tpMissarg(addrLocalKeywordCst)
	case o.Peer.Family == unix.AF_UNSPEC:
		return l2tpMissarg("remote")
	case o.Encap == xtcpnl.L2TPEncapUDP && o.LocalPort == 0:
		return l2tpMissarg("udp_sport")
	case o.Encap == xtcpnl.L2TPEncapUDP && o.PeerPort == 0:
		return l2tpMissarg("udp_dport")
	}
	return nil
}

// l2tpAppendReq appends one write request, its own sequence number, to
// out.
func l2tpAppendReq(c *runCtx, out []byte, id uint16, cmd uint8, attrs []byte) ([]byte, error) {
	req, err := xtcpnl.BuildGenlWriteRequest(xtcpnl.L2TPGenlName, id, cmd, xtcpnl.L2TPGenlVersion,
		unix.NLM_F_ACK, c.nextSeq(), attrs)
	if err != nil {
		return nil, err
	}
	return append(out, req...), nil
}

// l2tpAddrBytes is an address's bytelen bytes, 16 for AF_INET6 and 4
// otherwise; add's missarg has refused AF_UNSPEC.
func l2tpAddrBytes(a *render.L2tpAddr) (bool, []byte) {
	if a.Family == unix.AF_INET6 {
		return true, a.Data[:]
	}
	return false, a.Data[:4]
}

// tunnelAttrs is create_tunnel's attributes: the ids, the version, the
// encapsulation, the addresses, each IPv6 when it is, and for UDP the
// ports, the IPv4 checksum when on, and each IPv6 zero-checksum flag for a
// direction that is off.
func (o *l2tpOpt) tunnelAttrs() ([]byte, error) {
	var raw [l2tpReqBufCst]byte
	b := xtcpnl.NewAttrBuilder(raw[:])
	localAttr, peerAttr := xtcpnl.L2TPAttrIPSaddr, xtcpnl.L2TPAttrIPDaddr
	local6, local := l2tpAddrBytes(&o.Local)
	if local6 {
		localAttr = xtcpnl.L2TPAttrIP6Saddr
	}
	peer6, peer := l2tpAddrBytes(&o.Peer)
	if peer6 {
		peerAttr = xtcpnl.L2TPAttrIP6Daddr
	}
	errs := []error{
		b.PutU32(xtcpnl.L2TPAttrConnID, o.TunnelID),
		b.PutU32(xtcpnl.L2TPAttrPeerConnID, o.PeerTunnelID),
		b.PutU8(xtcpnl.L2TPAttrProtoVersion, l2tpProtoVersionCst),
		b.PutU16(xtcpnl.L2TPAttrEncapType, o.Encap),
		b.PutBytes(localAttr, local),
		b.PutBytes(peerAttr, peer),
	}
	if o.Encap == xtcpnl.L2TPEncapUDP {
		errs = append(errs, b.PutU16(xtcpnl.L2TPAttrUDPSport, o.LocalPort),
			b.PutU16(xtcpnl.L2TPAttrUDPDport, o.PeerPort))
		if o.UDPCsum {
			errs = append(errs, b.PutU8(xtcpnl.L2TPAttrUDPCsum, 1))
		}
		if !o.UDP6CsumTx {
			errs = append(errs, b.PutBytes(xtcpnl.L2TPAttrUDPZeroCsum6Tx, nil))
		}
		if !o.UDP6CsumRx {
			errs = append(errs, b.PutBytes(xtcpnl.L2TPAttrUDPZeroCsum6Rx, nil))
		}
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// sessionAttrs is create_session's attributes: the four ids, the
// ethernet pseudowire, the layer 2 header's type and length, then each
// sequencing direction, cookie and the name when given. parse_args never
// sets the reorder timeout, so its attribute is never sent.
func (o *l2tpOpt) sessionAttrs() ([]byte, error) {
	var raw [l2tpReqBufCst]byte
	b := xtcpnl.NewAttrBuilder(raw[:])
	errs := []error{
		b.PutU32(xtcpnl.L2TPAttrConnID, o.TunnelID),
		b.PutU32(xtcpnl.L2TPAttrPeerConnID, o.PeerTunnelID),
		b.PutU32(xtcpnl.L2TPAttrSessionID, o.SessionID),
		b.PutU32(xtcpnl.L2TPAttrPeerSessionID, o.PeerSessionID),
		b.PutU16(xtcpnl.L2TPAttrPwType, xtcpnl.L2TPPwTypeEth),
		b.PutU8(xtcpnl.L2TPAttrL2SpecType, o.l2specType),
		b.PutU8(xtcpnl.L2TPAttrL2SpecLen, o.l2specLen),
	}
	if o.RecvSeq {
		errs = append(errs, b.PutU8(xtcpnl.L2TPAttrRecvSeq, 1))
	}
	if o.SendSeq {
		errs = append(errs, b.PutU8(xtcpnl.L2TPAttrSendSeq, 1))
	}
	if o.CookieLen != 0 {
		errs = append(errs, b.PutBytes(xtcpnl.L2TPAttrCookie, o.Cookie[:o.CookieLen]))
	}
	if o.PeerCookieLen != 0 {
		errs = append(errs, b.PutBytes(xtcpnl.L2TPAttrPeerCookie, o.PeerCookie[:o.PeerCookieLen]))
	}
	if o.Ifname != nil {
		errs = append(errs, b.PutString(xtcpnl.L2TPAttrIfname, *o.Ifname))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// l2tpDel is do_del: tunnel or session, the id each needs, then
// SESSION_DELETE of the tunnel's session when a session id was given,
// else TUNNEL_DELETE.
func l2tpDel(c *runCtx, id uint16, args []string) error {
	o, err := l2tpParseArgs(c, args)
	if err != nil {
		return err
	}
	switch {
	case !o.tunnel && !o.session:
		return l2tpMissarg("tunnel or session")
	case o.tunnel && o.TunnelID == 0:
		return l2tpMissarg("tunnel_id")
	case o.session && o.SessionID == 0:
		return l2tpMissarg("session_id")
	}
	var raw [l2tpReqBufCst]byte
	b := xtcpnl.NewAttrBuilder(raw[:])
	cmd := xtcpnl.L2TPCmdTunnelDelete
	errs := []error{b.PutU32(xtcpnl.L2TPAttrConnID, o.TunnelID)}
	if o.SessionID != 0 {
		cmd = xtcpnl.L2TPCmdSessionDelete
		errs = append(errs, b.PutU32(xtcpnl.L2TPAttrSessionID, o.SessionID))
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	out, err := l2tpAppendReq(c, nil, id, cmd, b.Bytes())
	if err != nil {
		return err
	}
	return writeAddrOut(out)
}

// l2tpShow is do_show: tunnel or session, then get_session's or
// get_tunnel's dump. A session's request carries the tunnel and session
// ids when both were given, a tunnel's the tunnel id when given. Each
// answer is read over the state parse_args left and printed, under -j in
// one array, closed however the dump ends.
func l2tpShow(c *runCtx, id uint16, args []string) error {
	o, err := l2tpParseArgs(c, args)
	if err != nil {
		return err
	}
	if !o.tunnel && !o.session {
		return l2tpMissarg("tunnel or session")
	}
	var raw [l2tpReqBufCst]byte
	b := xtcpnl.NewAttrBuilder(raw[:])
	cmd := xtcpnl.L2TPCmdTunnelGet
	var errs []error
	switch {
	case o.session:
		cmd = xtcpnl.L2TPCmdSessionGet
		if o.TunnelID != 0 && o.SessionID != 0 {
			errs = append(errs, b.PutU32(xtcpnl.L2TPAttrConnID, o.TunnelID),
				b.PutU32(xtcpnl.L2TPAttrSessionID, o.SessionID))
		}
	case o.TunnelID != 0:
		errs = append(errs, b.PutU32(xtcpnl.L2TPAttrConnID, o.TunnelID))
	}
	if err := errors.Join(errs...); err != nil {
		return err
	}
	gs, err := genlSource(c)
	if err != nil {
		return err
	}
	req, err := xtcpnl.BuildGenlRequest(xtcpnl.L2TPGenlName, id, cmd, xtcpnl.L2TPGenlVersion,
		unix.NLM_F_DUMP, c.nextSeq(), b.Bytes())
	if err != nil {
		return err
	}
	bodies, err := gs.GenlDump(req)
	var arr []byte
	p := &o.L2tpParm
	for i := 0; err == nil && i < len(bodies); i++ {
		err = l2tpPrint(c, p, o.session, bodies[i], &arr)
	}
	if jerr := genlJSONEnd(c, arr); jerr != nil {
		return jerr
	}
	if err != nil {
		return fmt.Errorf("%w: %w", ErrL2tpDumpTerminated, err)
	}
	return nil
}

// l2tpPrint is session_nlmsg or tunnel_nlmsg: get_response over p, then
// print_session or print_tunnel, as text or into arr under -j. An answer
// shorter than its genlmsghdr is get_response's -1; ParseL2TP's stricter
// refusals, which the kernel's answers do not meet, end the dump the same
// way.
func l2tpPrint(c *runCtx, p *render.L2tpParm, session bool, body []byte, arr *[]byte) error {
	m, err := xtcpnl.ParseL2TP(body)
	if err != nil {
		return err
	}
	l2tpGetResponse(p, &m)
	var text string
	var obj []byte
	if session {
		text, obj = render.L2tpSession(p)
	} else {
		text, obj = render.L2tpTunnel(p, c.json)
	}
	if c.json {
		genlJSONAppend(arr, obj)
		return nil
	}
	_, err = io.WriteString(c.out, text)
	return err
}

// l2tpGetResponse is get_response's reading of one answer into p: each
// attribute present sets its field, but the IPv6 checksums, which are on
// unless their zero-checksum flag is present, and the addresses, whose
// family is AF_INET when the IPv4 attribute is present and AF_INET6
// otherwise, over the data the last answer left when neither is.
func l2tpGetResponse(p *render.L2tpParm, m *xtcpnl.L2TP) {
	setU32 := func(dst *uint32, v *uint32) {
		if v != nil {
			*dst = *v
		}
	}
	setBool := func(dst *bool, v *uint8) {
		if v != nil {
			*dst = *v != 0
		}
	}
	if m.EncapType != nil {
		p.Encap = *m.EncapType
	}
	setU32(&p.TunnelID, m.ConnID)
	setU32(&p.PeerTunnelID, m.PeerConnID)
	setU32(&p.SessionID, m.SessionID)
	setU32(&p.PeerSessionID, m.PeerSessionID)
	setBool(&p.UDPCsum, m.UDPCsum)
	p.UDP6CsumTx, p.UDP6CsumRx = !m.UDPZeroCsum6Tx, !m.UDPZeroCsum6Rx
	if m.Cookie != nil {
		p.CookieLen = copy(p.Cookie[:], m.Cookie)
	}
	if m.PeerCookie != nil {
		p.PeerCookieLen = copy(p.PeerCookie[:], m.PeerCookie)
	}
	setBool(&p.RecvSeq, m.RecvSeq)
	setBool(&p.SendSeq, m.SendSeq)
	if m.RecvTimeout != nil {
		// get_response's int of the u64.
		p.ReorderTimeout = int32(*m.RecvTimeout)
	}
	l2tpRespAddr(&p.Local, m.Saddr)
	l2tpRespAddr(&p.Peer, m.Daddr)
	if m.UDPSport != nil {
		p.LocalPort = *m.UDPSport
	}
	if m.UDPDport != nil {
		p.PeerPort = *m.UDPDport
	}
	if m.Ifname != nil {
		name := *m.Ifname
		p.Ifname = &name
	}
}

// l2tpRespAddr is get_response's address: AF_INET over the first four
// bytes for an IPv4 one, AF_INET6 over all sixteen for an IPv6 one, and
// AF_INET6 over the data as it was for neither.
func l2tpRespAddr(dst *render.L2tpAddr, a netip.Addr) {
	switch {
	case a.Is4():
		dst.Family = unix.AF_INET
		b := a.As4()
		copy(dst.Data[:4], b[:])
	case a.Is6():
		dst.Family = unix.AF_INET6
		dst.Data = a.As16()
	default:
		dst.Family = unix.AF_INET6
	}
}
