package goip

import (
	"errors"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// l2tpProtoVersionCst is the L2TP version create_tunnel sends, 3.
const l2tpProtoVersionCst = 3

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
