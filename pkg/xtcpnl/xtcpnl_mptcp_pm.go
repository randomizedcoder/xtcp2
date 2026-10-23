package xtcpnl

import (
	"net/netip"
)

// This file decodes the mptcp_pm generic netlink family behind `ip mptcp`:
// ip's endpoint and limits requests, the kernel's GET_ADDR and GET_LIMITS
// answers (net/mptcp/pm_netlink.c, ip/ipmptcp.c), and the events the
// kernel multicasts to the mptcp_pm_events group for `ip mptcp monitor`.
// The family id is assigned at boot, so the caller resolves "mptcp_pm"
// through the controller before handing a body here.
//
// The events' numbers overlap the commands': CLOSED is 3, as GET_ADDR is,
// and ANNOUNCED 6, as GET_LIMITS is, so the command cannot tell them apart.
// The header can: an event is multicast, so its nlmsg_pid is 0, while the
// kernel's answers, dumps included, carry the requester's port id. A kernel
// message with nlmsg_pid 0 is for ParseMPTCPEvent; every other mptcp_pm
// body is for ParseMPTCPPM.

const (
	// MPTCPPMGenlName is the family's name in the controller.
	MPTCPPMGenlName = "mptcp_pm"
	// MPTCPPMEventGroupName is MPTCP_PM_EV_GRP_NAME, the multicast group
	// the events go to.
	MPTCPPMEventGroupName = "mptcp_pm_events"

	// MPTCP_PM_ADDR_FLAG_* (include/uapi/linux/mptcp.h), an endpoint's
	// MPTCPAddrAttrFlags.
	MPTCPAddrFlagSignal   uint32 = 1 << 0
	MPTCPAddrFlagSubflow  uint32 = 1 << 1
	MPTCPAddrFlagBackup   uint32 = 1 << 2
	MPTCPAddrFlagFullmesh uint32 = 1 << 3
	MPTCPAddrFlagImplicit uint32 = 1 << 4
	MPTCPAddrFlagLaminar  uint32 = 1 << 5

	// MPTCP_PM_EV_FLAG_*, an event's MPTCPAttrFlags.
	MPTCPEventFlagDenyJoinID0 uint16 = 1 << 0
	MPTCPEventFlagServerSide  uint16 = 1 << 1

	// MPTCP_PM_CMD_* (include/uapi/linux/mptcp_pm.h) that ip sends; 8 to
	// 11 are the userspace path manager's.
	MPTCPPMCmdAddAddr        uint8 = 1
	MPTCPPMCmdDelAddr        uint8 = 2
	MPTCPPMCmdGetAddr        uint8 = 3
	MPTCPPMCmdFlushAddrs     uint8 = 4
	MPTCPPMCmdSetLimits      uint8 = 5
	MPTCPPMCmdGetLimits      uint8 = 6
	MPTCPPMCmdSetFlags       uint8 = 7
	MPTCPPMCmdAnnounce       uint8 = 8
	MPTCPPMCmdRemove         uint8 = 9
	MPTCPPMCmdSubflowCreate  uint8 = 10
	MPTCPPMCmdSubflowDestroy uint8 = 11

	// MPTCP_PM_ATTR_*.
	MPTCPPMAttrAddr        uint16 = 1 // nested MPTCP_PM_ADDR_ATTR_*
	MPTCPPMAttrRcvAddAddrs uint16 = 2 // u32
	MPTCPPMAttrSubflows    uint16 = 3 // u32
	MPTCPPMAttrToken       uint16 = 4 // u32
	MPTCPPMAttrLocID       uint16 = 5 // u8
	MPTCPPMAttrAddrRemote  uint16 = 6 // nested MPTCP_PM_ADDR_ATTR_*

	// MPTCP_PM_ADDR_ATTR_*, inside an endpoint.
	MPTCPAddrAttrFamily uint16 = 1 // u16
	MPTCPAddrAttrID     uint16 = 2 // u8
	MPTCPAddrAttrAddr4  uint16 = 3 // u32, network order
	MPTCPAddrAttrAddr6  uint16 = 4 // struct in6_addr
	MPTCPAddrAttrPort   uint16 = 5 // u16, host order
	MPTCPAddrAttrFlags  uint16 = 6 // u32, MPTCP_PM_ADDR_FLAG_*
	MPTCPAddrAttrIfIdx  uint16 = 7 // s32

	// MPTCP_EVENT_*.
	MPTCPEventCreated         uint8 = 1
	MPTCPEventEstablished     uint8 = 2
	MPTCPEventClosed          uint8 = 3
	MPTCPEventAnnounced       uint8 = 6
	MPTCPEventRemoved         uint8 = 7
	MPTCPEventSubEstablished  uint8 = 10
	MPTCPEventSubClosed       uint8 = 11
	MPTCPEventSubPriority     uint8 = 13
	MPTCPEventListenerCreated uint8 = 15
	MPTCPEventListenerClosed  uint8 = 16

	// MPTCP_ATTR_*, an event's attributes.
	MPTCPAttrToken       uint16 = 1  // u32
	MPTCPAttrFamily      uint16 = 2  // u16
	MPTCPAttrLocID       uint16 = 3  // u8
	MPTCPAttrRemID       uint16 = 4  // u8
	MPTCPAttrSaddr4      uint16 = 5  // u32, network order
	MPTCPAttrSaddr6      uint16 = 6  // struct in6_addr
	MPTCPAttrDaddr4      uint16 = 7  // u32, network order
	MPTCPAttrDaddr6      uint16 = 8  // struct in6_addr
	MPTCPAttrSport       uint16 = 9  // u16, network order
	MPTCPAttrDport       uint16 = 10 // u16, network order
	MPTCPAttrBackup      uint16 = 11 // u8
	MPTCPAttrError       uint16 = 12 // u8
	MPTCPAttrFlags       uint16 = 13 // u16, MPTCP_PM_EV_FLAG_*
	MPTCPAttrTimeout     uint16 = 14 // u32
	MPTCPAttrIfIdx       uint16 = 15 // s32
	MPTCPAttrResetReason uint16 = 16 // u32
	MPTCPAttrResetFlags  uint16 = 17 // u32
	MPTCPAttrServerSide  uint16 = 18 // u8
)

// MPTCPAddr is a decoded endpoint, a nested MPTCP_PM_ATTR_ADDR or
// ADDR_REMOTE. Addr is the zero netip.Addr when absent; the other fields
// are nil when absent.
type MPTCPAddr struct {
	Family  *uint16
	ID      *uint8
	Addr    netip.Addr // ADDR4 or ADDR6
	Port    *uint16
	Flags   *uint32
	IfIndex *int32
}

// MPTCPPM is a decoded mptcp_pm command or answer. Each field is nil when
// absent.
type MPTCPPM struct {
	GenericNetlinkMessage
	Addr        *MPTCPAddr
	RcvAddAddrs *uint32
	Subflows    *uint32
	Token       *uint32
	LocID       *uint8
	AddrRemote  *MPTCPAddr
}

// ParseMPTCPPM decodes an mptcp_pm command or answer body AFTER the caller
// has resolved its family as MPTCPPMGenlName; an event is for
// ParseMPTCPEvent. It builds on ParseGenericNetlink, so framing is checked
// as for every genl family, a repeated attribute is an error, each scalar
// must be its exact width, and an endpoint's address must be 4 or 16
// bytes; with both, ADDR4 wins. An unknown command or attribute is not an
// error.
func ParseMPTCPPM(body []byte) (MPTCPPM, error) {
	var m MPTCPPM
	g, err := ParseGenericNetlink(body)
	m.GenericNetlinkMessage = g
	if err != nil {
		return m, err
	}
	d := genlDecoder{attrs: g.Attributes}
	m.Addr = d.mptcpAddr(MPTCPPMAttrAddr)
	d.u32(MPTCPPMAttrRcvAddAddrs, &m.RcvAddAddrs)
	d.u32(MPTCPPMAttrSubflows, &m.Subflows)
	d.u32(MPTCPPMAttrToken, &m.Token)
	d.u8(MPTCPPMAttrLocID, &m.LocID)
	m.AddrRemote = d.mptcpAddr(MPTCPPMAttrAddrRemote)
	return m, d.err
}

// mptcpAddr reads the endpoint nested under id, nil when absent.
func (d *genlDecoder) mptcpAddr(id uint16) *MPTCPAddr {
	if d.err != nil {
		return nil
	}
	attrs, ok, err := attrNested(d.attrs, id)
	if err != nil || !ok {
		d.err = err
		return nil
	}
	var a MPTCPAddr
	nd := genlDecoder{attrs: attrs}
	nd.u16(MPTCPAddrAttrFamily, &a.Family)
	nd.u8(MPTCPAddrAttrID, &a.ID)
	a.Addr = nd.addr(MPTCPAddrAttrAddr4, MPTCPAddrAttrAddr6)
	nd.u16(MPTCPAddrAttrPort, &a.Port)
	nd.u32(MPTCPAddrAttrFlags, &a.Flags)
	nd.s32(MPTCPAddrAttrIfIdx, &a.IfIndex)
	d.err = nd.err
	return &a
}

// MPTCPEvent is a decoded mptcp_pm event. Saddr and Daddr are the zero
// netip.Addr when absent; the other fields are nil when absent. Sport and
// Dport are in host order, converted from the wire's network order.
type MPTCPEvent struct {
	GenericNetlinkMessage
	Token       *uint32
	Family      *uint16
	LocID       *uint8
	RemID       *uint8
	Saddr       netip.Addr // SADDR4 or SADDR6
	Daddr       netip.Addr // DADDR4 or DADDR6
	Sport       *uint16
	Dport       *uint16
	Backup      *uint8
	Error       *uint8
	Flags       *uint16
	Timeout     *uint32
	IfIndex     *int32
	ResetReason *uint32
	ResetFlags  *uint32
	ServerSide  *uint8
}

// ParseMPTCPEvent decodes an mptcp_pm event body, a kernel message with
// nlmsg_pid 0, AFTER the caller has resolved its family as
// MPTCPPMGenlName. Its checks are ParseMPTCPPM's; an address must be 4 or
// 16 bytes, and with both, the IPv4 one wins.
func ParseMPTCPEvent(body []byte) (MPTCPEvent, error) {
	var m MPTCPEvent
	g, err := ParseGenericNetlink(body)
	m.GenericNetlinkMessage = g
	if err != nil {
		return m, err
	}
	d := genlDecoder{attrs: g.Attributes}
	d.u32(MPTCPAttrToken, &m.Token)
	d.u16(MPTCPAttrFamily, &m.Family)
	d.u8(MPTCPAttrLocID, &m.LocID)
	d.u8(MPTCPAttrRemID, &m.RemID)
	m.Saddr = d.addr(MPTCPAttrSaddr4, MPTCPAttrSaddr6)
	m.Daddr = d.addr(MPTCPAttrDaddr4, MPTCPAttrDaddr6)
	d.be16(MPTCPAttrSport, &m.Sport)
	d.be16(MPTCPAttrDport, &m.Dport)
	d.u8(MPTCPAttrBackup, &m.Backup)
	d.u8(MPTCPAttrError, &m.Error)
	d.u16(MPTCPAttrFlags, &m.Flags)
	d.u32(MPTCPAttrTimeout, &m.Timeout)
	d.s32(MPTCPAttrIfIdx, &m.IfIndex)
	d.u32(MPTCPAttrResetReason, &m.ResetReason)
	d.u32(MPTCPAttrResetFlags, &m.ResetFlags)
	d.u8(MPTCPAttrServerSide, &m.ServerSide)
	return m, d.err
}
