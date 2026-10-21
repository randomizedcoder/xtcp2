package xtcpnl

import (
	"net/netip"
)

// This file decodes the fou generic netlink family behind `ip fou`: ip's
// ADD, DEL and GET requests and the kernel's GET answers (net/ipv4/fou_nl.c,
// fou_core.c, ip/ipfou.c). The family id is assigned at boot, so the caller
// resolves "fou" through the controller before handing a body here.

const (
	// FOUGenlName is the family's name in the controller.
	FOUGenlName = "fou"

	// FOU_CMD_* (include/uapi/linux/fou.h).
	FOUCmdAdd uint8 = 1
	FOUCmdDel uint8 = 2
	FOUCmdGet uint8 = 3

	// FOU_ATTR_*.
	FOUAttrPort             uint16 = 1  // u16, network order
	FOUAttrAF               uint16 = 2  // u8, AF_INET or AF_INET6
	FOUAttrIPProto          uint16 = 3  // u8, the encapsulated protocol
	FOUAttrType             uint16 = 4  // u8, FOU_ENCAP_DIRECT (1) or GUE (2)
	FOUAttrRemcsumNoPartial uint16 = 5  // flag
	FOUAttrLocalV4          uint16 = 6  // u32, network order
	FOUAttrLocalV6          uint16 = 7  // struct in6_addr
	FOUAttrPeerV4           uint16 = 8  // u32, network order
	FOUAttrPeerV6           uint16 = 9  // struct in6_addr
	FOUAttrPeerPort         uint16 = 10 // u16, network order
	FOUAttrIfindex          uint16 = 11 // s32
)

// FOU is a decoded fou message. Local and Peer are the zero netip.Addr when
// absent; the other optional fields are nil when absent. Port and PeerPort
// are in host order, converted from the wire's network order.
//
// The kernel's GET answer always carries both addresses and the peer port,
// zero when the receiver has none (fou_fill_info): an unbound IPv4 port
// answers with local 0.0.0.0, an IPv6 one with ::. They are reported as
// sent.
type FOU struct {
	GenericNetlinkMessage
	Port             *uint16
	AF               *uint8
	IPProto          *uint8
	Type             *uint8
	RemcsumNoPartial bool
	Local            netip.Addr // LOCAL_V4 or LOCAL_V6
	Peer             netip.Addr // PEER_V4 or PEER_V6
	PeerPort         *uint16
	Ifindex          *int32
}

// ParseFOU decodes a fou body AFTER the caller has resolved its family as
// FOUGenlName. It builds on ParseGenericNetlink, so framing is checked as
// for every genl family, a repeated attribute is an error, each scalar must
// be its exact width, an address must be 4 or 16 bytes (with both, the
// IPv4 one wins), and REMCSUM_NOPARTIAL must be empty. An unknown command
// or attribute is not an error.
func ParseFOU(body []byte) (FOU, error) {
	var m FOU
	g, err := ParseGenericNetlink(body)
	m.GenericNetlinkMessage = g
	if err != nil {
		return m, err
	}
	d := genlDecoder{attrs: g.Attributes}
	d.be16(FOUAttrPort, &m.Port)
	d.u8(FOUAttrAF, &m.AF)
	d.u8(FOUAttrIPProto, &m.IPProto)
	d.u8(FOUAttrType, &m.Type)
	m.RemcsumNoPartial = d.flag(FOUAttrRemcsumNoPartial)
	m.Local = d.addr(FOUAttrLocalV4, FOUAttrLocalV6)
	m.Peer = d.addr(FOUAttrPeerV4, FOUAttrPeerV6)
	d.be16(FOUAttrPeerPort, &m.PeerPort)
	d.s32(FOUAttrIfindex, &m.Ifindex)
	return m, d.err
}
