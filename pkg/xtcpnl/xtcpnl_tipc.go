package xtcpnl

import (
	"encoding/binary"
	"fmt"

	"golang.org/x/sys/unix"
)

// This file decodes the TIPC generic netlink family ("TIPCv2") behind
// iproute2's tipc(8) (tipc/): the kernel's answers to the read-only
// bearer, link, media, monitor, name-table, node and socket commands
// (net/tipc/netlink.c and each object's tipc_nl_* fill function), and the
// requests tipc sends. The family id is assigned at boot, so the caller
// resolves "TIPCv2" through the controller before handing a body here.
//
// Every TIPC message carries its object in one top-level nest
// (TIPC_NLA_BEARER, TIPC_NLA_LINK, ...), in requests and replies alike, so
// ParseTIPC needs no classification: it decodes each top-level nest that is
// present into its section. Malformed framing, a duplicate singleton or a
// scalar of the wrong width is an error, as for every genl family here;
// the sections decoded before it are kept. An attribute this package does
// not model stays in GenericNetlinkMessage.Attributes (top level) or is
// skipped (inside a nest).

const (
	// TIPCGenlName is the family's name in the controller, and
	// TIPCGenlVersion the version tipc opens it at (TIPC_GENL_V2_*).
	TIPCGenlName    = "TIPCv2"
	TIPCGenlVersion = 1

	// TIPC_NL_* (include/uapi/linux/tipc_netlink.h): the read-only ones.
	TIPCCmdBearerGet    uint8 = 4
	TIPCCmdSockGet      uint8 = 6
	TIPCCmdPublGet      uint8 = 7
	TIPCCmdLinkGet      uint8 = 8
	TIPCCmdMediaGet     uint8 = 11
	TIPCCmdNodeGet      uint8 = 13
	TIPCCmdNetGet       uint8 = 14
	TIPCCmdNameTableGet uint8 = 16
	TIPCCmdMonGet       uint8 = 18
	TIPCCmdMonPeerGet   uint8 = 19

	// TIPC_NLA_*: the top-level nests.
	TIPCAttrBearer    uint16 = 1
	TIPCAttrSock      uint16 = 2
	TIPCAttrPubl      uint16 = 3
	TIPCAttrLink      uint16 = 4
	TIPCAttrMedia     uint16 = 5
	TIPCAttrNode      uint16 = 6
	TIPCAttrNet       uint16 = 7
	TIPCAttrNameTable uint16 = 8
	TIPCAttrMon       uint16 = 9
	TIPCAttrMonPeer   uint16 = 10

	// TIPC_NLA_BEARER_* and TIPC_NLA_UDP_*.
	TIPCBearerName     uint16 = 1
	TIPCBearerProp     uint16 = 2
	TIPCBearerDomain   uint16 = 3
	TIPCBearerUDPOpts  uint16 = 4
	tipcUDPLocal       uint16 = 1
	tipcUDPRemote      uint16 = 2
	tipcUDPMultiRemote uint16 = 3

	// TIPC_NLA_SOCK_* and TIPC_NLA_CON_*.
	TIPCSockAddr      uint16 = 1
	TIPCSockRef       uint16 = 2
	TIPCSockCon       uint16 = 3
	TIPCSockHasPubl   uint16 = 4
	tipcSockType      uint16 = 6
	tipcSockIno       uint16 = 7
	tipcSockUID       uint16 = 8
	tipcSockTIPCState uint16 = 9
	tipcSockCookie    uint16 = 10
	tipcConFlag       uint16 = 1
	tipcConNode       uint16 = 2
	tipcConSock       uint16 = 3
	tipcConType       uint16 = 4
	tipcConInst       uint16 = 5

	// TIPC_NLA_LINK_*.
	TIPCLinkName      uint16 = 1
	tipcLinkDest      uint16 = 2
	tipcLinkMTU       uint16 = 3
	TIPCLinkBroadcast uint16 = 4
	tipcLinkUp        uint16 = 5
	tipcLinkActive    uint16 = 6
	tipcLinkProp      uint16 = 7
	tipcLinkStats     uint16 = 8
	tipcLinkRX        uint16 = 9
	tipcLinkTX        uint16 = 10

	// TIPC_NLA_MEDIA_*.
	TIPCMediaName uint16 = 1
	TIPCMediaProp uint16 = 2

	// TIPC_NLA_NODE_*.
	tipcNodeAddr      uint16 = 1
	tipcNodeUp        uint16 = 2
	tipcNodeID        uint16 = 3
	tipcNodeKey       uint16 = 4
	tipcNodeKeyMaster uint16 = 5
	tipcNodeRekeying  uint16 = 6

	// TIPC_NLA_NET_*.
	tipcNetID         uint16 = 1
	tipcNetAddr       uint16 = 2
	tipcNetNodeID     uint16 = 3
	tipcNetNodeIDW1   uint16 = 4
	tipcNetAddrLegacy uint16 = 5

	// TIPC_NLA_NAME_TABLE_PUBL and TIPC_NLA_PUBL_*.
	tipcNameTablePubl uint16 = 1
	tipcPublType      uint16 = 1
	tipcPublLower     uint16 = 2
	tipcPublUpper     uint16 = 3
	tipcPublScope     uint16 = 4
	tipcPublNode      uint16 = 5
	tipcPublRef       uint16 = 6
	tipcPublKey       uint16 = 7

	// TIPC_NLA_MON_* and TIPC_NLA_MON_PEER_*.
	TIPCMonActivationThreshold uint16 = 1
	TIPCMonRef                 uint16 = 2
	tipcMonActive              uint16 = 3
	tipcMonBearerName          uint16 = 4
	tipcMonPeerCnt             uint16 = 5
	tipcMonListGen             uint16 = 6
	tipcMonPeerAddr            uint16 = 1
	tipcMonPeerDomGen          uint16 = 2
	tipcMonPeerApplied         uint16 = 3
	tipcMonPeerUpMap           uint16 = 4
	tipcMonPeerMembers         uint16 = 5
	tipcMonPeerUp              uint16 = 6
	tipcMonPeerHead            uint16 = 7
	tipcMonPeerLocal           uint16 = 8

	// TIPC_NLA_PROP_*.
	TIPCPropPrio           uint16 = 1
	TIPCPropTol            uint16 = 2
	TIPCPropWin            uint16 = 3
	TIPCPropMTU            uint16 = 4
	TIPCPropBroadcast      uint16 = 5
	TIPCPropBroadcastRatio uint16 = 6

	// TIPCStatsMax is TIPC_NLA_STATS_MAX: the stats nest's ids run 1..33.
	TIPCStatsMax uint16 = 33
)

// TIPC_NLA_STATS_* ids a link's stats are keyed by.
const (
	TIPCStatsRxInfo uint16 = iota + 1
	TIPCStatsRxFragments
	TIPCStatsRxFragmented
	TIPCStatsRxBundles
	TIPCStatsRxBundled
	TIPCStatsTxInfo
	TIPCStatsTxFragments
	TIPCStatsTxFragmented
	TIPCStatsTxBundles
	TIPCStatsTxBundled
	TIPCStatsMsgProfTot
	TIPCStatsMsgLenCnt
	TIPCStatsMsgLenTot
	TIPCStatsMsgLenP0
	TIPCStatsMsgLenP1
	TIPCStatsMsgLenP2
	TIPCStatsMsgLenP3
	TIPCStatsMsgLenP4
	TIPCStatsMsgLenP5
	TIPCStatsMsgLenP6
	TIPCStatsRxStates
	TIPCStatsRxProbes
	TIPCStatsRxNacks
	TIPCStatsRxDeferred
	TIPCStatsTxStates
	TIPCStatsTxProbes
	TIPCStatsTxNacks
	TIPCStatsTxAcks
	TIPCStatsRetransmitted
	TIPCStatsDuplicates
	TIPCStatsLinkCongs
	TIPCStatsMaxQueue
	TIPCStatsAvgQueue
)

// TIPCProp is a PROP nest: a bearer's, a link's or a media's properties.
type TIPCProp struct {
	Prio, Tol, Win, MTU, Broadcast, BroadcastRatio *uint32
}

// TIPCBearer is a BEARER nest. UDPLocal and UDPRemote are the UDP media's
// sockaddr_storage bytes as the kernel put them.
type TIPCBearer struct {
	Name                *string
	Prop                *TIPCProp
	Domain              *uint32
	UDPLocal, UDPRemote []byte
	UDPMultiRemote      bool
}

// TIPCCon is a socket's CON nest: its peer, and the name it connected by
// when Flag is set.
type TIPCCon struct {
	Flag                   bool
	Node, Sock, Type, Inst *uint32
}

// TIPCSock is a SOCK nest from SOCK_GET (tipc socket list), or the
// selector of a PUBL_GET request (Ref alone).
type TIPCSock struct {
	Addr, Ref                 *uint32
	Con                       *TIPCCon
	HasPubl                   bool
	Type, Ino, UID, TIPCState *uint32
	Cookie                    *uint64
}

// TIPCPubl is a publication: a PUBL nest from PUBL_GET (a socket's
// bindings), or the PUBL inside a NAME_TABLE nest (tipc nametable show).
type TIPCPubl struct {
	Type, Lower, Upper, Scope, Node, Ref, Key *uint32
}

// TIPCLink is a LINK nest. Stats is the STATS nest, keyed by
// TIPC_NLA_STATS_* id; nil when absent.
type TIPCLink struct {
	Name                  *string
	Dest, MTU             *uint32
	Broadcast, Up, Active bool
	Prop                  *TIPCProp
	Stats                 map[uint16]uint32
	RX, TX                *uint32
}

// TIPCMedia is a MEDIA nest.
type TIPCMedia struct {
	Name *string
	Prop *TIPCProp
}

// TIPCNode is a NODE nest from NODE_GET (tipc node list).
type TIPCNode struct {
	Addr      *uint32
	Up        bool
	ID, Key   []byte
	KeyMaster bool
	Rekeying  *uint32
}

// TIPCNet is a NET nest from NET_GET (tipc node get). NodeID and NodeIDW1
// are the node identity's two halves as the kernel's u64 words, in host
// (little-endian) order, so their bytes are the identity's.
type TIPCNet struct {
	ID, Addr         *uint32
	NodeID, NodeIDW1 *uint64
	AddrLegacy       bool
}

// TIPCMon is a MON nest from MON_GET (tipc link monitor summary/get), or
// a MON_PEER_GET request's selector (Ref alone).
type TIPCMon struct {
	ActivationThreshold, Ref, PeerCnt, ListGen *uint32
	Active                                     bool
	BearerName                                 *string
}

// TIPCMonPeer is a MON_PEER nest (tipc link monitor list). Members is the
// peer's domain, a u32 node address each, in host order.
type TIPCMonPeer struct {
	Addr, DomGen, Applied *uint32
	UpMap                 *uint64
	Members               []uint32
	Up, Head, Local       bool
}

// TIPCMessage is one decoded TIPC message: each top-level nest present,
// in its section. NameTable is the publication inside a NAME_TABLE nest.
type TIPCMessage struct {
	GenericNetlinkMessage
	Command   uint8
	Request   bool
	Bearer    *TIPCBearer
	Sock      *TIPCSock
	Publ      *TIPCPubl
	Link      *TIPCLink
	Media     *TIPCMedia
	Node      *TIPCNode
	Net       *TIPCNet
	NameTable *TIPCPubl
	Mon       *TIPCMon
	MonPeer   *TIPCMonPeer
}

// ParseTIPC decodes a TIPC body AFTER the caller has resolved its family as
// TIPCGenlName. flags are the message's nlmsg_flags, for Request.
func ParseTIPC(body []byte, flags uint16) (TIPCMessage, error) {
	var m TIPCMessage
	g, err := ParseGenericNetlink(body)
	m.GenericNetlinkMessage = g
	if err != nil {
		return m, err
	}
	m.Command = g.Command
	m.Request = flags&unix.NLM_F_REQUEST != 0
	d := genlDecoder{attrs: g.Attributes}
	if nd, ok := d.nested(TIPCAttrBearer); ok {
		m.Bearer = nd.tipcBearer()
		d.err = nd.err
	}
	if nd, ok := d.nested(TIPCAttrSock); ok {
		m.Sock = nd.tipcSock()
		d.err = nd.err
	}
	if nd, ok := d.nested(TIPCAttrPubl); ok {
		m.Publ = nd.tipcPubl()
		d.err = nd.err
	}
	if nd, ok := d.nested(TIPCAttrLink); ok {
		m.Link = nd.tipcLink()
		d.err = nd.err
	}
	if nd, ok := d.nested(TIPCAttrMedia); ok {
		m.Media = &TIPCMedia{Name: nd.str(TIPCMediaName), Prop: nd.tipcProp(TIPCMediaProp)}
		d.err = nd.err
	}
	if nd, ok := d.nested(TIPCAttrNode); ok {
		m.Node = nd.tipcNode()
		d.err = nd.err
	}
	if nd, ok := d.nested(TIPCAttrNet); ok {
		m.Net = nd.tipcNet()
		d.err = nd.err
	}
	if nd, ok := d.nested(TIPCAttrNameTable); ok {
		if pd, ok := nd.nested(tipcNameTablePubl); ok {
			m.NameTable = pd.tipcPubl()
			nd.err = pd.err
		}
		d.err = nd.err
	}
	if nd, ok := d.nested(TIPCAttrMon); ok {
		m.Mon = nd.tipcMon()
		d.err = nd.err
	}
	if nd, ok := d.nested(TIPCAttrMonPeer); ok {
		m.MonPeer = nd.tipcMonPeer()
		d.err = nd.err
	}
	return m, d.err
}

// tipcProp reads the PROP nest at id; nil when absent. Unlike other
// singletons, a property may repeat with the same value: the kernel puts a
// unicast link's PRIO twice (net/tipc/link.c:2708 and 2715). A repeat that
// disagrees is still an error.
func (d *genlDecoder) tipcProp(id uint16) *TIPCProp {
	nd, ok := d.nested(id)
	if !ok {
		return nil
	}
	p := &TIPCProp{}
	slots := map[uint16]**uint32{
		TIPCPropPrio: &p.Prio, TIPCPropTol: &p.Tol, TIPCPropWin: &p.Win,
		TIPCPropMTU: &p.MTU, TIPCPropBroadcast: &p.Broadcast, TIPCPropBroadcastRatio: &p.BroadcastRatio,
	}
	for _, a := range nd.attrs {
		dst, known := slots[a.ID()]
		if !known {
			continue
		}
		if len(a.Data) != 4 || a.Type&0xc000 != 0 {
			d.err = fmt.Errorf("%w: tipc property %d needs an unflagged 4-byte scalar", ErrGenericNetlink, a.ID())
			return p
		}
		v := binary.LittleEndian.Uint32(a.Data)
		if *dst != nil && **dst != v {
			d.err = fmt.Errorf("%w: tipc property %d repeated as %d and %d", ErrGenericNetlink, a.ID(), **dst, v)
			return p
		}
		*dst = &v
	}
	return p
}

func (d *genlDecoder) tipcBearer() *TIPCBearer {
	b := &TIPCBearer{Name: d.str(TIPCBearerName)}
	b.Prop = d.tipcProp(TIPCBearerProp)
	d.u32(TIPCBearerDomain, &b.Domain)
	if nd, ok := d.nested(TIPCBearerUDPOpts); ok {
		if v := nd.bytes(tipcUDPLocal); v != nil {
			b.UDPLocal = append([]byte(nil), v...)
		}
		if v := nd.bytes(tipcUDPRemote); v != nil {
			b.UDPRemote = append([]byte(nil), v...)
		}
		b.UDPMultiRemote = nd.flag(tipcUDPMultiRemote)
		d.err = nd.err
	}
	return b
}

func (d *genlDecoder) tipcSock() *TIPCSock {
	s := &TIPCSock{}
	d.u32(TIPCSockAddr, &s.Addr)
	d.u32(TIPCSockRef, &s.Ref)
	if nd, ok := d.nested(TIPCSockCon); ok {
		c := &TIPCCon{Flag: nd.flag(tipcConFlag)}
		nd.u32(tipcConNode, &c.Node)
		nd.u32(tipcConSock, &c.Sock)
		nd.u32(tipcConType, &c.Type)
		nd.u32(tipcConInst, &c.Inst)
		s.Con = c
		d.err = nd.err
	}
	s.HasPubl = d.flag(TIPCSockHasPubl)
	d.u32(tipcSockType, &s.Type)
	d.u32(tipcSockIno, &s.Ino)
	d.u32(tipcSockUID, &s.UID)
	d.u32(tipcSockTIPCState, &s.TIPCState)
	d.u64(tipcSockCookie, &s.Cookie)
	return s
}

func (d *genlDecoder) tipcPubl() *TIPCPubl {
	p := &TIPCPubl{}
	d.u32(tipcPublType, &p.Type)
	d.u32(tipcPublLower, &p.Lower)
	d.u32(tipcPublUpper, &p.Upper)
	d.u32(tipcPublScope, &p.Scope)
	d.u32(tipcPublNode, &p.Node)
	d.u32(tipcPublRef, &p.Ref)
	d.u32(tipcPublKey, &p.Key)
	return p
}

func (d *genlDecoder) tipcLink() *TIPCLink {
	l := &TIPCLink{Name: d.str(TIPCLinkName)}
	d.u32(tipcLinkDest, &l.Dest)
	d.u32(tipcLinkMTU, &l.MTU)
	l.Broadcast = d.flag(TIPCLinkBroadcast)
	l.Up = d.flag(tipcLinkUp)
	l.Active = d.flag(tipcLinkActive)
	l.Prop = d.tipcProp(tipcLinkProp)
	if nd, ok := d.nested(tipcLinkStats); ok {
		l.Stats = nd.tipcStats()
		d.err = nd.err
	}
	d.u32(tipcLinkRX, &l.RX)
	d.u32(tipcLinkTX, &l.TX)
	return l
}

// tipcStats reads a STATS nest: every id 1..TIPCStatsMax present, each an
// unflagged u32, none twice. An id past the max is skipped, as a newer
// kernel's counter would be.
func (d *genlDecoder) tipcStats() map[uint16]uint32 {
	st := make(map[uint16]uint32, len(d.attrs))
	for id := uint16(1); id <= TIPCStatsMax && d.err == nil; id++ {
		var v *uint32
		d.u32(id, &v)
		if v != nil {
			st[id] = *v
		}
	}
	return st
}

func (d *genlDecoder) tipcNode() *TIPCNode {
	n := &TIPCNode{}
	d.u32(tipcNodeAddr, &n.Addr)
	n.Up = d.flag(tipcNodeUp)
	if v := d.bytes(tipcNodeID); v != nil {
		n.ID = append([]byte(nil), v...)
	}
	if v := d.bytes(tipcNodeKey); v != nil {
		n.Key = append([]byte(nil), v...)
	}
	n.KeyMaster = d.flag(tipcNodeKeyMaster)
	d.u32(tipcNodeRekeying, &n.Rekeying)
	return n
}

func (d *genlDecoder) tipcNet() *TIPCNet {
	n := &TIPCNet{}
	d.u32(tipcNetID, &n.ID)
	d.u32(tipcNetAddr, &n.Addr)
	d.u64(tipcNetNodeID, &n.NodeID)
	d.u64(tipcNetNodeIDW1, &n.NodeIDW1)
	n.AddrLegacy = d.flag(tipcNetAddrLegacy)
	return n
}

func (d *genlDecoder) tipcMon() *TIPCMon {
	m := &TIPCMon{}
	d.u32(TIPCMonActivationThreshold, &m.ActivationThreshold)
	d.u32(TIPCMonRef, &m.Ref)
	m.Active = d.flag(tipcMonActive)
	m.BearerName = d.str(tipcMonBearerName)
	d.u32(tipcMonPeerCnt, &m.PeerCnt)
	d.u32(tipcMonListGen, &m.ListGen)
	return m
}

func (d *genlDecoder) tipcMonPeer() *TIPCMonPeer {
	p := &TIPCMonPeer{}
	d.u32(tipcMonPeerAddr, &p.Addr)
	d.u32(tipcMonPeerDomGen, &p.DomGen)
	d.u32(tipcMonPeerApplied, &p.Applied)
	d.u64(tipcMonPeerUpMap, &p.UpMap)
	if v := d.bytes(tipcMonPeerMembers); v != nil {
		if len(v)%4 != 0 {
			d.err = fmt.Errorf("%w: tipc monitor members are %d bytes, not a whole number of u32s", ErrGenericNetlink, len(v))
			return p
		}
		p.Members = make([]uint32, 0, len(v)/4)
		for i := 0; i < len(v); i += 4 {
			p.Members = append(p.Members, binary.LittleEndian.Uint32(v[i:]))
		}
	}
	p.Up = d.flag(tipcMonPeerUp)
	p.Head = d.flag(tipcMonPeerHead)
	p.Local = d.flag(tipcMonPeerLocal)
	return p
}

// BuildTIPCRequest lays out a tipc(8) request (libmnl's
// mnlu_gen_socket_cmd_prepare, then msg_doit or msg_dumpit): the family id
// as the type, flags plus NLM_F_REQUEST, genl version 1, and, when top is
// non-zero, one top-level nest with NLA_F_NESTED set (libmnl's
// mnl_attr_nest_start sets it) holding what put writes. tipc sends a dump
// with NLM_F_DUMP and a get with NLM_F_ACK.
func BuildTIPCRequest(id uint16, cmd uint8, flags uint16, seq uint32, top uint16,
	put func(*AttrBuilder) error) ([]byte, error) {
	var raw [256]byte
	ab := NewAttrBuilder(raw[:])
	if top != 0 {
		m, err := ab.BeginNest(top | unix.NLA_F_NESTED)
		if err != nil {
			return nil, err
		}
		if put != nil {
			if err := put(&ab); err != nil {
				return nil, err
			}
		}
		if err := ab.EndNest(m); err != nil {
			return nil, err
		}
	}
	return BuildGenlRequest(TIPCGenlName, id, cmd, TIPCGenlVersion, flags, seq, ab.Bytes())
}

// BuildTIPCFamilyRequest is tipc's family lookup (mnlu_gen_socket_open):
// CTRL_CMD_GETFAMILY at genl version 1 with NLM_F_ACK, naming "TIPCv2".
// tipc sends it at start-up and again before each per-socket or
// per-bearer follow-up query.
func BuildTIPCFamilyRequest(seq uint32) ([]byte, error) {
	var raw [20]byte
	ab := NewAttrBuilder(raw[:])
	if err := ab.PutString(2, TIPCGenlName); err != nil {
		return nil, err
	}
	return BuildGenlRequest("nlctrl", unix.GENL_ID_CTRL, 3, 1, unix.NLM_F_ACK, seq, ab.Bytes())
}
