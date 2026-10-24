package render

import (
	"fmt"
	"net/netip"
	"strconv"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// L2tpAddr is an inet_prefix as get_response leaves it: the family of the
// attribute it read, and 16 bytes of data, of which an IPv4 address
// overwrites the first four and an IPv6 one all; with neither attribute
// the family is AF_INET6 over whatever the data held.
type L2tpAddr struct {
	Family uint8
	Data   [16]byte
}

// String is inet_ntop of the family over the data.
func (a *L2tpAddr) String() string {
	if a.Family == unix.AF_INET {
		return netip.AddrFrom4([4]byte(a.Data[:4])).String()
	}
	return inet6Ntop(a.Data)
}

// L2tpParm is struct l2tp_parm as `ip l2tp show` prints it: parse_args'
// state, then each answer's attributes over it, one struct for the whole
// dump, so a field an answer does not carry keeps the last value it had.
type L2tpParm struct {
	TunnelID, PeerTunnelID, SessionID, PeerSessionID uint32
	Encap                                            uint16
	LocalPort, PeerPort                              uint16
	Cookie, PeerCookie                               [8]byte
	CookieLen, PeerCookieLen                         int
	Local, Peer                                      L2tpAddr
	UDP6CsumTx, UDP6CsumRx, UDPCsum                  bool
	RecvSeq, SendSeq                                 bool
	ReorderTimeout                                   int32
	Ifname                                           *string
}

// L2tpTunnel is print_tunnel's lines and object (ip/ipl2tp.c): the id and
// encapsulation, the addresses, the peer's id, and for UDP the ports and
// the checksum, AF_INET's one flag or AF_INET6's two. json is
// is_json_context, which picks AF_INET6's two members over its line.
func L2tpTunnel(p *L2tpParm, json bool) (string, []byte) {
	var o genlOut
	o.uint("tunnel_id", "Tunnel "+strconv.FormatUint(uint64(p.TunnelID), 10)+",", uint64(p.TunnelID))
	encap := "??"
	switch p.Encap {
	case xtcpnl.L2TPEncapUDP:
		encap = "UDP"
	case xtcpnl.L2TPEncapIP:
		encap = "IP"
	}
	o.str("encap", " encap "+encap+"\n", encap)
	local, peer := p.Local.String(), p.Peer.String()
	o.str("local", "  From "+local+" ", local)
	o.str("peer", "to "+peer+"\n", peer)
	o.uint("peer_tunnel", "  Peer tunnel "+strconv.FormatUint(uint64(p.PeerTunnelID), 10)+"\n",
		uint64(p.PeerTunnelID))
	if p.Encap == xtcpnl.L2TPEncapUDP {
		o.text.WriteString("  UDP source / dest ports:")
		o.uint("local_port", " "+strconv.FormatUint(uint64(p.LocalPort), 10), uint64(p.LocalPort))
		o.uint("peer_port", "/"+strconv.FormatUint(uint64(p.PeerPort), 10)+"\n", uint64(p.PeerPort))
		switch p.Local.Family {
		case unix.AF_INET:
			o.jsonBool("checksum", p.UDPCsum)
			o.text.WriteString("  UDP checksum: " + l2tpEnabled(p.UDPCsum) + "\n")
		case unix.AF_INET6:
			if json {
				o.jsonBool("checksum_tx", p.UDP6CsumTx)
				o.jsonBool("checksum_rx", p.UDP6CsumRx)
			} else {
				o.text.WriteString("  UDP checksum: " + l2tpCsum6(p.UDP6CsumTx, p.UDP6CsumRx) + "\n")
			}
		}
	}
	text, obj := o.done()
	// Each line above ends itself; print_tunnel has no print_nl of its own
	// at the end, so done's is dropped.
	return text[:len(text)-1], obj
}

// l2tpEnabled is "enabled" or "disabled".
func l2tpEnabled(on bool) string {
	if on {
		return "enabled"
	}
	return "disabled"
}

// l2tpCsum6 is print_tunnel's IPv6 checksum word, of four.
func l2tpCsum6(tx, rx bool) string {
	switch {
	case tx && rx:
		return "enabled"
	case tx:
		return "tx"
	case rx:
		return "rx"
	}
	return "disabled"
}

// L2tpSession is print_session's lines and object: the ids, the
// interface, the text's fixed offsets, then the cookies and the reorder
// timeout run together on one line, the sequence numbering on the next,
// and the line's end, which leaves a session with none of them a blank
// line after it.
func L2tpSession(p *L2tpParm) (string, []byte) {
	var o genlOut
	o.uint("session_id", "Session "+strconv.FormatUint(uint64(p.SessionID), 10), uint64(p.SessionID))
	o.uint("tunnel_id", " in tunnel "+strconv.FormatUint(uint64(p.TunnelID), 10)+"\n", uint64(p.TunnelID))
	o.uint("peer_session_id", "  Peer session "+strconv.FormatUint(uint64(p.PeerSessionID), 10)+",",
		uint64(p.PeerSessionID))
	o.uint("peer_tunnel_id", " tunnel "+strconv.FormatUint(uint64(p.PeerTunnelID), 10)+"\n",
		uint64(p.PeerTunnelID))
	if p.Ifname != nil {
		o.str("interface", "  interface name: "+*p.Ifname+"\n", *p.Ifname)
	}
	o.text.WriteString("  offset 0, peer offset 0\n")
	if p.CookieLen > 0 {
		c := l2tpCookie(p.Cookie, p.CookieLen)
		o.str("cookie", "  cookie "+c, c)
	}
	if p.PeerCookieLen > 0 {
		c := l2tpCookie(p.PeerCookie, p.PeerCookieLen)
		o.str("peer_cookie", "  peer cookie "+c, c)
	}
	if p.ReorderTimeout != 0 {
		// print_uint's %u of the int.
		v := uint64(uint32(p.ReorderTimeout))
		o.uint("reorder_timeout", "  reorder timeout: "+strconv.FormatUint(v, 10), v)
	}
	if p.SendSeq || p.RecvSeq {
		o.text.WriteString("\n  sequence numbering:")
		if p.SendSeq {
			o.null("send_seq", " send")
		}
		if p.RecvSeq {
			o.null("recv_seq", " recv")
		}
	}
	return o.done()
}

// l2tpCookie is print_cookie: the first four bytes in hex, all eight when
// the length is 8.
func l2tpCookie(c [8]byte, n int) string {
	if n == 8 {
		return fmt.Sprintf("%x", c[:])
	}
	return fmt.Sprintf("%x", c[:4])
}
