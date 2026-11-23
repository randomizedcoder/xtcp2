package xtcpnl

import (
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// The requests `bridge fdb show/get` and `bridge mdb show/get` send
// (bridge/fdb.c, bridge/mdb.c; byte layouts confirmed by the 7_3_0 fdb*/mdb*
// readback captures and 7_3_1/ws/bridgefdb, bridgemdb).

// The RTM_GETMDB get request's attributes (enum MDBA_GET_ENTRY_*,
// include/uapi/linux/if_bridge.h).
const (
	MdbaGetEntry      uint16 = 1 // MDBA_GET_ENTRY: a struct br_mdb_entry
	MdbaGetEntryAttrs uint16 = 2 // MDBA_GET_ENTRY_ATTRS: nested MDBE_ATTR_*
)

var (
	// ErrFdbLladdrLen is an fdb get address that is not ETH_ALEN bytes:
	// fdb_get sscanf's exactly six octets.
	ErrFdbLladdrLen = errors.New("xtcpnl: fdb get address is not 6 bytes")
	// ErrMdbGroupLen is an mdb get group whose length fits no protocol.
	ErrMdbGroupLen = errors.New("xtcpnl: mdb group is not 4, 16 or 6 bytes")
	// ErrMdbSourceLen is an mdb get source that is neither IPv4 nor IPv6.
	ErrMdbSourceLen = errors.New("xtcpnl: mdb source is not 4 or 16 bytes")
)

// BuildFdbDumpRequest is fdb_show's dump in strict mode
// (rtnl_neighdump_req(PF_BRIDGE, fdb_dump_filter), fdb.c:334-351): an
// RTM_GETNEIGH dump whose ndmsg carries AF_BRIDGE and the `dev` filter's
// ifindex, and NDA_MASTER u32 when `br` was given.
//
// bridge sends sizeof(req), the 28-byte message and its 256-byte unused
// buffer (a 284-byte datagram); the kernel reads nlmsg_len, and so does
// replay. This builds the 28 (or 36) bytes nlmsg_len covers.
func BuildFdbDumpRequest(ifindex, master, seq uint32) ([]byte, error) {
	hdr := make([]byte, NdMsgSizeCst)
	hdr[0] = unix.AF_BRIDGE
	binary.LittleEndian.PutUint32(hdr[4:8], ifindex)
	var attrs []byte
	if master != 0 {
		var raw [reqAttrBufCst]byte
		ab := NewAttrBuilder(raw[:])
		if err := ab.PutU32(uint16(unix.NDA_MASTER), master); err != nil {
			return nil, err
		}
		attrs = ab.Bytes()
	}
	return BuildRequest(uint16(unix.RTM_GETNEIGH), uint16(unix.NLM_F_DUMP), seq, hdr, attrs)
}

// FdbGet is the request `bridge fdb get` builds (fdb_get, fdb.c:631-728).
type FdbGet struct {
	Ifindex uint32 // ndm_ifindex: `dev`/`brport`, 0 without
	Flags   uint8  // NTF_SELF, NTF_MASTER
	Lladdr  []byte // NDA_LLADDR, ETH_ALEN bytes
	Vlan    uint16 // NDA_VLAN when HasVlan: fdb_get sends it when its short `vlan` is >= 0
	HasVlan bool
	Vni     uint32 // NDA_VNI when HasVni
	HasVni  bool
	Master  uint32 // NDA_MASTER when non-zero (`br`)
}

// BuildFdbGetRequest builds fdb_get's RTM_GETNEIGH: NLM_F_REQUEST alone
// (rtnl_talk with an answer), an AF_BRIDGE ndmsg, then NDA_LLADDR, NDA_VLAN,
// NDA_VNI and NDA_MASTER in that order.
func BuildFdbGetRequest(g FdbGet, seq uint32) ([]byte, error) {
	if len(g.Lladdr) != 6 {
		return nil, ErrFdbLladdrLen
	}
	hdr := make([]byte, NdMsgSizeCst)
	hdr[0] = unix.AF_BRIDGE
	binary.LittleEndian.PutUint32(hdr[4:8], g.Ifindex)
	hdr[NdMsgFlagsOffCst] = g.Flags
	var raw [2 * reqAttrBufCst]byte // LLADDR 12 + VLAN 8 + VNI 8 + MASTER 8
	ab := NewAttrBuilder(raw[:])
	err := ab.PutBytes(uint16(unix.NDA_LLADDR), g.Lladdr)
	if err == nil && g.HasVlan {
		err = ab.PutU16(uint16(unix.NDA_VLAN), g.Vlan)
	}
	if err == nil && g.HasVni {
		err = ab.PutU32(uint16(unix.NDA_VNI), g.Vni)
	}
	if err == nil && g.Master != 0 {
		err = ab.PutU32(uint16(unix.NDA_MASTER), g.Master)
	}
	if err != nil {
		return nil, err
	}
	return BuildRequest(uint16(unix.RTM_GETNEIGH), 0, seq, hdr, ab.Bytes())
}

// BuildMdbDumpRequest is rtnl_mdbdump_req(PF_BRIDGE): an RTM_GETMDB dump
// whose br_port_msg names no device. mdb_show sends it twice, once for the
// entries and once for the router ports; the kernel's strict check refuses
// a device index, so `dev` filters in userspace.
func BuildMdbDumpRequest(seq uint32) ([]byte, error) {
	hdr := make([]byte, BrPortMsgSizeCst)
	hdr[0] = unix.AF_BRIDGE
	return BuildRequest(uint16(unix.RTM_GETMDB), uint16(unix.NLM_F_DUMP), seq, hdr, nil)
}

// MdbGet is the request `bridge mdb get` builds (mdb_get, mdb.c:857-936).
type MdbGet struct {
	Ifindex   uint32 // br_port_msg's: `dev`
	Vid       uint16 // the entry's vid: mdb_get's short, as sent
	Group     []byte // 4 (IPv4), 16 (IPv6) or 6 (L2, proto 0) bytes
	Source    []byte // MDBE_ATTR_SOURCE, 4 or 16 bytes; nil for none
	SrcVni    uint32 // MDBE_ATTR_SRC_VNI when HasSrcVni
	HasSrcVni bool
}

// MarshalMdbGetEntry is the struct br_mdb_entry mdb_get sends: ifindex,
// state and flags zero (the kernel requires it), vid, the group in
// addr.u and addr.proto in network order, the trailing pad zero.
func MarshalMdbGetEntry(vid uint16, group []byte) ([]byte, error) {
	e := make([]byte, MdbEntrySizeCst)
	binary.LittleEndian.PutUint16(e[6:8], vid)
	var proto uint16
	switch len(group) {
	case 4:
		proto = MdbProtoIP
	case 16:
		proto = MdbProtoIPv6
	case 6:
	default:
		return nil, ErrMdbGroupLen
	}
	copy(e[8:24], group)
	binary.BigEndian.PutUint16(e[24:26], proto)
	return e, nil
}

// BuildMdbGetRequest builds mdb_get's RTM_GETMDB: NLM_F_REQUEST alone, a
// br_port_msg naming the device, MDBA_GET_ENTRY, and, when a source or
// source VNI was given, MDBA_GET_ENTRY_ATTRS with NLA_F_NESTED set by hand,
// holding MDBE_ATTR_SOURCE then MDBE_ATTR_SRC_VNI.
func BuildMdbGetRequest(g MdbGet, seq uint32) ([]byte, error) {
	entry, err := MarshalMdbGetEntry(g.Vid, g.Group)
	if err != nil {
		return nil, err
	}
	if g.Source != nil && len(g.Source) != 4 && len(g.Source) != 16 {
		return nil, ErrMdbSourceLen
	}
	hdr := make([]byte, BrPortMsgSizeCst)
	hdr[0] = unix.AF_BRIDGE
	binary.LittleEndian.PutUint32(hdr[4:8], g.Ifindex)
	var raw [4 * reqAttrBufCst]byte // GET_ENTRY 32 + the nest 4 + SOURCE 20 + SRC_VNI 8
	ab := NewAttrBuilder(raw[:])
	if err := ab.PutBytes(MdbaGetEntry, entry); err != nil {
		return nil, err
	}
	if g.Source != nil || g.HasSrcVni {
		m, err := ab.BeginNest(MdbaGetEntryAttrs | unix.NLA_F_NESTED)
		if err != nil {
			return nil, err
		}
		if g.Source != nil {
			err = ab.PutBytes(MdbeAttrSource, g.Source)
		}
		if err == nil && g.HasSrcVni {
			err = ab.PutU32(MdbeAttrSrcVni, g.SrcVni)
		}
		if err == nil {
			err = ab.EndNest(m)
		}
		if err != nil {
			return nil, err
		}
	}
	return BuildRequest(uint16(unix.RTM_GETMDB), 0, seq, hdr, ab.Bytes())
}
