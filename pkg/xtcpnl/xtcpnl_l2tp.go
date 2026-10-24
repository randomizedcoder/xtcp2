package xtcpnl

import (
	"net/netip"
)

// This file decodes the l2tp generic netlink family behind `ip l2tp`: ip's
// tunnel and session requests and the kernel's TUNNEL_GET and SESSION_GET
// answers (net/l2tp/l2tp_netlink.c, ip/ipl2tp.c). The family id is assigned
// at boot, so the caller resolves "l2tp" through the controller before
// handing a body here.

const (
	// L2TPGenlName is the family's name in the controller.
	L2TPGenlName = "l2tp"

	// L2TP_CMD_* (include/uapi/linux/l2tp.h); ip sends no MODIFY.
	L2TPCmdTunnelCreate  uint8 = 1
	L2TPCmdTunnelDelete  uint8 = 2
	L2TPCmdTunnelModify  uint8 = 3
	L2TPCmdTunnelGet     uint8 = 4
	L2TPCmdSessionCreate uint8 = 5
	L2TPCmdSessionDelete uint8 = 6
	L2TPCmdSessionModify uint8 = 7
	L2TPCmdSessionGet    uint8 = 8

	// L2TP_ENCAPTYPE_*, the values of L2TPAttrEncapType.
	L2TPEncapUDP uint16 = 0
	L2TPEncapIP  uint16 = 1

	// L2TPPwTypeEth is L2TP_PWTYPE_ETH, the one pseudowire type ip makes.
	L2TPPwTypeEth uint16 = 5

	// L2TP_L2SPECTYPE_*, the values of L2TPAttrL2SpecType.
	L2TPL2SpecNone    uint8 = 0
	L2TPL2SpecDefault uint8 = 1

	// L2TP_ATTR_*. OFFSET (3), DATA_SEQ (4), VLAN_ID (14), MTU (28) and
	// MRU (29) are marked unused in the header, and neither ip nor the
	// kernel sends them; they are left in Attributes. Type 35 is
	// L2TP_ATTR_PAD, the alignment pad before a u64; it is skipped.
	L2TPAttrPwType            uint16 = 1  // u16, enum l2tp_pwtype
	L2TPAttrEncapType         uint16 = 2  // u16, UDP (0) or IP (1)
	L2TPAttrL2SpecType        uint16 = 5  // u8
	L2TPAttrL2SpecLen         uint16 = 6  // u8; marked unused, but ip sends it
	L2TPAttrProtoVersion      uint16 = 7  // u8
	L2TPAttrIfname            uint16 = 8  // NUL-terminated string
	L2TPAttrConnID            uint16 = 9  // u32
	L2TPAttrPeerConnID        uint16 = 10 // u32
	L2TPAttrSessionID         uint16 = 11 // u32
	L2TPAttrPeerSessionID     uint16 = 12 // u32
	L2TPAttrUDPCsum           uint16 = 13 // u8
	L2TPAttrCookie            uint16 = 15 // 0, 4 or 8 bytes
	L2TPAttrPeerCookie        uint16 = 16 // 0, 4 or 8 bytes
	L2TPAttrDebug             uint16 = 17 // u32; the kernel always sends 0
	L2TPAttrRecvSeq           uint16 = 18 // u8
	L2TPAttrSendSeq           uint16 = 19 // u8
	L2TPAttrLnsMode           uint16 = 20 // u8
	L2TPAttrUsingIPSec        uint16 = 21 // u8
	L2TPAttrRecvTimeout       uint16 = 22 // u64 msecs
	L2TPAttrFD                uint16 = 23 // s32, a socket ip does not pass
	L2TPAttrIPSaddr           uint16 = 24 // u32, network order
	L2TPAttrIPDaddr           uint16 = 25 // u32, network order
	L2TPAttrUDPSport          uint16 = 26 // u16, host order
	L2TPAttrUDPDport          uint16 = 27 // u16, host order
	L2TPAttrStats             uint16 = 30 // nested L2TP_ATTR_* counters
	L2TPAttrIP6Saddr          uint16 = 31 // struct in6_addr
	L2TPAttrIP6Daddr          uint16 = 32 // struct in6_addr
	L2TPAttrUDPZeroCsum6Tx    uint16 = 33 // flag
	L2TPAttrUDPZeroCsum6Rx    uint16 = 34 // flag
	L2TPStatsTxPackets        uint16 = 1  // u64, each counter
	L2TPStatsTxBytes          uint16 = 2
	L2TPStatsTxErrors         uint16 = 3
	L2TPStatsRxPackets        uint16 = 4
	L2TPStatsRxBytes          uint16 = 5
	L2TPStatsRxSeqDiscards    uint16 = 6
	L2TPStatsRxOOSPackets     uint16 = 7
	L2TPStatsRxErrors         uint16 = 8
	L2TPStatsRxCookieDiscards uint16 = 10 // after L2TP_ATTR_STATS_PAD (9)
	L2TPStatsRxInvalid        uint16 = 11
)

// L2TPStats is a decoded L2TP_ATTR_STATS nest. Each counter is nil when
// absent.
type L2TPStats struct {
	TxPackets, TxBytes, TxErrors                    *uint64
	RxPackets, RxBytes, RxSeqDiscards, RxOOSPackets *uint64
	RxErrors, RxCookieDiscards, RxInvalid           *uint64
}

// L2TP is a decoded l2tp message, a tunnel's or a session's. Saddr and
// Daddr are the zero netip.Addr when absent; the other optional fields are
// nil when absent. UDPSport and UDPDport are in host order, as the kernel
// and ip put them.
type L2TP struct {
	GenericNetlinkMessage
	PwType         *uint16
	EncapType      *uint16
	L2SpecType     *uint8
	L2SpecLen      *uint8
	ProtoVersion   *uint8
	Ifname         *string
	ConnID         *uint32
	PeerConnID     *uint32
	SessionID      *uint32
	PeerSessionID  *uint32
	UDPCsum        *uint8
	Cookie         []byte // owned; nil when absent
	PeerCookie     []byte // owned; nil when absent
	Debug          *uint32
	RecvSeq        *uint8
	SendSeq        *uint8
	LnsMode        *uint8
	UsingIPSec     *uint8
	RecvTimeout    *uint64 // msecs
	FD             *int32
	Saddr          netip.Addr // IP_SADDR or IP6_SADDR
	Daddr          netip.Addr // IP_DADDR or IP6_DADDR
	UDPSport       *uint16
	UDPDport       *uint16
	UDPZeroCsum6Tx bool
	UDPZeroCsum6Rx bool
	Stats          *L2TPStats
}

// ParseL2TP decodes an l2tp body AFTER the caller has resolved its family
// as L2TPGenlName. It builds on ParseGenericNetlink, so framing is checked
// as for every genl family, a repeated attribute is an error, each scalar
// must be its exact width, an address must be 4 or 16 bytes (with both,
// the IPv4 one wins), IFNAME must be NUL-terminated, and the zero-checksum
// flags must be empty. A cookie's length is not checked. An unknown
// command or attribute is not an error.
func ParseL2TP(body []byte) (L2TP, error) {
	var m L2TP
	g, err := ParseGenericNetlink(body)
	m.GenericNetlinkMessage = g
	if err != nil {
		return m, err
	}
	d := genlDecoder{attrs: g.Attributes}
	d.u16(L2TPAttrPwType, &m.PwType)
	d.u16(L2TPAttrEncapType, &m.EncapType)
	d.u8(L2TPAttrL2SpecType, &m.L2SpecType)
	d.u8(L2TPAttrL2SpecLen, &m.L2SpecLen)
	d.u8(L2TPAttrProtoVersion, &m.ProtoVersion)
	m.Ifname = d.str(L2TPAttrIfname)
	d.u32(L2TPAttrConnID, &m.ConnID)
	d.u32(L2TPAttrPeerConnID, &m.PeerConnID)
	d.u32(L2TPAttrSessionID, &m.SessionID)
	d.u32(L2TPAttrPeerSessionID, &m.PeerSessionID)
	d.u8(L2TPAttrUDPCsum, &m.UDPCsum)
	m.Cookie = d.bytes(L2TPAttrCookie)
	m.PeerCookie = d.bytes(L2TPAttrPeerCookie)
	d.u32(L2TPAttrDebug, &m.Debug)
	d.u8(L2TPAttrRecvSeq, &m.RecvSeq)
	d.u8(L2TPAttrSendSeq, &m.SendSeq)
	d.u8(L2TPAttrLnsMode, &m.LnsMode)
	d.u8(L2TPAttrUsingIPSec, &m.UsingIPSec)
	d.u64(L2TPAttrRecvTimeout, &m.RecvTimeout)
	d.s32(L2TPAttrFD, &m.FD)
	m.Saddr = d.addr(L2TPAttrIPSaddr, L2TPAttrIP6Saddr)
	m.Daddr = d.addr(L2TPAttrIPDaddr, L2TPAttrIP6Daddr)
	d.u16(L2TPAttrUDPSport, &m.UDPSport)
	d.u16(L2TPAttrUDPDport, &m.UDPDport)
	m.UDPZeroCsum6Tx = d.flag(L2TPAttrUDPZeroCsum6Tx)
	m.UDPZeroCsum6Rx = d.flag(L2TPAttrUDPZeroCsum6Rx)
	m.Stats = d.l2tpStats()
	return m, d.err
}

// l2tpStats reads the L2TP_ATTR_STATS nest, nil when absent.
func (d *genlDecoder) l2tpStats() *L2TPStats {
	if d.err != nil {
		return nil
	}
	attrs, ok, err := attrNested(d.attrs, L2TPAttrStats)
	if err != nil || !ok {
		d.err = err
		return nil
	}
	var s L2TPStats
	nd := genlDecoder{attrs: attrs}
	for _, c := range []struct {
		id  uint16
		dst **uint64
	}{
		{L2TPStatsTxPackets, &s.TxPackets}, {L2TPStatsTxBytes, &s.TxBytes}, {L2TPStatsTxErrors, &s.TxErrors},
		{L2TPStatsRxPackets, &s.RxPackets}, {L2TPStatsRxBytes, &s.RxBytes},
		{L2TPStatsRxSeqDiscards, &s.RxSeqDiscards}, {L2TPStatsRxOOSPackets, &s.RxOOSPackets},
		{L2TPStatsRxErrors, &s.RxErrors}, {L2TPStatsRxCookieDiscards, &s.RxCookieDiscards},
		{L2TPStatsRxInvalid, &s.RxInvalid},
	} {
		nd.u64(c.id, c.dst)
	}
	d.err = nd.err
	return &s
}
