package xtcpnl

import (
	"encoding/binary"
	"errors"
)

// The bridge MDB messages: RTM_NEWMDB, RTM_DELMDB and their dump. A write is
//
//	struct br_port_msg { __u8 family; __u32 ifindex; }   (8 bytes, padded)
//	[MDBA_SET_ENTRY]       struct br_mdb_entry
//	[MDBA_SET_ENTRY_ATTRS] { [MDBE_ATTR_*] ... }
//
// and a dump reply is
//
//	struct br_port_msg
//	[MDBA_MDB] { [MDBA_MDB_ENTRY] { [MDBA_MDB_ENTRY_INFO] struct br_mdb_entry,
//	                                  [MDBA_MDB_EATTR_*] ... } ... }
//	[MDBA_ROUTER] { ... }
//
// with struct br_mdb_entry
//
//	__u32 ifindex; __u8 state; __u8 flags; __u16 vid;
//	struct { union { __be32 ip4; struct in6_addr ip6; u8 mac_addr[6]; } u;
//	         __be16 proto; } addr;                       (28 bytes)
//
// Reference: include/uapi/linux/if_bridge.h. The request's MDBE_ATTR_* and
// the dump's MDBA_MDB_EATTR_* number the same things differently (the dump
// has TIMER first), so each has its own table below.

const (
	// BrPortMsgSizeCst is struct br_port_msg: a u8 and a u32, padded.
	BrPortMsgSizeCst = 8
	// MdbEntrySizeCst is struct br_mdb_entry.
	MdbEntrySizeCst = 28

	MdbaSetEntry      uint16 = 1 // MDBA_SET_ENTRY
	MdbaSetEntryAttrs uint16 = 2 // MDBA_SET_ENTRY_ATTRS
	MdbaMdb           uint16 = 1 // MDBA_MDB
	MdbaMdbEntry      uint16 = 1 // MDBA_MDB_ENTRY
	MdbaMdbEntryInfo  uint16 = 1 // MDBA_MDB_ENTRY_INFO
	MdbSrcListEntry   uint16 = 1 // MDBE_SRC_LIST_ENTRY and MDBA_MDB_SRCLIST_ENTRY
	MdbSrcAttrAddress uint16 = 1 // MDBE_SRCATTR_ADDRESS and MDBA_MDB_SRCATTR_ADDRESS

	MdbeAttrSource    uint16 = 1 // MDBE_ATTR_SOURCE
	MdbeAttrSrcList   uint16 = 2 // MDBE_ATTR_SRC_LIST
	MdbeAttrGroupMode uint16 = 3 // MDBE_ATTR_GROUP_MODE
	MdbeAttrRtProt    uint16 = 4 // MDBE_ATTR_RTPROT
	MdbeAttrDst       uint16 = 5 // MDBE_ATTR_DST
	MdbeAttrDstPort   uint16 = 6 // MDBE_ATTR_DST_PORT
	MdbeAttrVni       uint16 = 7 // MDBE_ATTR_VNI
	MdbeAttrIfindex   uint16 = 8 // MDBE_ATTR_IFINDEX
	MdbeAttrSrcVni    uint16 = 9 // MDBE_ATTR_SRC_VNI

	MdbEattrTimer     uint16 = 1  // MDBA_MDB_EATTR_TIMER
	MdbEattrSrcList   uint16 = 2  // MDBA_MDB_EATTR_SRC_LIST
	MdbEattrGroupMode uint16 = 3  // MDBA_MDB_EATTR_GROUP_MODE
	MdbEattrSource    uint16 = 4  // MDBA_MDB_EATTR_SOURCE
	MdbEattrRtProt    uint16 = 5  // MDBA_MDB_EATTR_RTPROT
	MdbEattrDst       uint16 = 6  // MDBA_MDB_EATTR_DST
	MdbEattrDstPort   uint16 = 7  // MDBA_MDB_EATTR_DST_PORT
	MdbEattrVni       uint16 = 8  // MDBA_MDB_EATTR_VNI
	MdbEattrIfindex   uint16 = 9  // MDBA_MDB_EATTR_IFINDEX
	MdbEattrSrcVni    uint16 = 10 // MDBA_MDB_EATTR_SRC_VNI

	// MdbProtoIP and MdbProtoIPv6 are addr.proto's ETH_P_IP and
	// ETH_P_IPV6, read in host order; 0 is an L2 group.
	MdbProtoIP   uint16 = 0x0800
	MdbProtoIPv6 uint16 = 0x86dd
)

var (
	ErrBrPortMsgSmall = errors.New("data too small for br_port_msg")
	ErrMdbEntrySize   = errors.New("br_mdb_entry is not 28 bytes")
)

// MdbEntry is struct br_mdb_entry. Group is the address's meaningful bytes:
// 4 for IPv4, 16 for IPv6 and 6 for an L2 group (proto 0).
type MdbEntry struct {
	Ifindex uint32
	State   uint8
	Flags   uint8
	Vid     uint16
	Proto   uint16 // addr.proto in host order
	Group   []byte
}

// MdbAttrs are an entry's attributes, from a request's MDBE_ATTR_* or a
// dump's MDBA_MDB_EATTR_*. A short fixed-width attribute is skipped.
type MdbAttrs struct {
	Source       []byte
	SrcList      [][]byte // each MDB*_SRCATTR_ADDRESS, in order
	HasSrcList   bool
	GroupMode    uint8
	HasGroupMode bool
	RtProt       uint8
	HasRtProt    bool
	Dst          []byte
	DstPort      uint16 // host order, as both iproute2 and the kernel use it
	HasDstPort   bool
	Vni          uint32
	HasVni       bool
	Ifindex      uint32
	HasIfindex   bool
	SrcVni       uint32
	HasSrcVni    bool
	Timer        uint32 // dump only
	HasTimer     bool
}

// MdbRequest is an RTM_NEWMDB or RTM_DELMDB request.
type MdbRequest struct {
	Family   uint8
	Ifindex  uint32 // br_port_msg's: the bridge or VXLAN
	Entry    MdbEntry
	HasEntry bool
	Attrs    MdbAttrs
	HasAttrs bool
}

// MdbDumpEntry is one MDBA_MDB_ENTRY_INFO of a dump reply.
type MdbDumpEntry struct {
	Bridge uint32 // br_port_msg's ifindex
	Entry  MdbEntry
	Attrs  MdbAttrs
}

// mdbAttrIDs maps one numbering of the entry attributes.
type mdbAttrIDs struct {
	source, srcList, groupMode, rtProt, dst, dstPort, vni, ifindex, srcVni, timer uint16
}

var (
	mdbeIDs  = mdbAttrIDs{MdbeAttrSource, MdbeAttrSrcList, MdbeAttrGroupMode, MdbeAttrRtProt, MdbeAttrDst, MdbeAttrDstPort, MdbeAttrVni, MdbeAttrIfindex, MdbeAttrSrcVni, 0}
	eattrIDs = mdbAttrIDs{MdbEattrSource, MdbEattrSrcList, MdbEattrGroupMode, MdbEattrRtProt, MdbEattrDst, MdbEattrDstPort, MdbEattrVni, MdbEattrIfindex, MdbEattrSrcVni, MdbEattrTimer}
)

// ParseMdbEntry decodes struct br_mdb_entry, which must be exactly 28
// bytes, as rtnl_validate_mdb_entry requires.
func ParseMdbEntry(b []byte) (MdbEntry, error) {
	if len(b) != MdbEntrySizeCst {
		return MdbEntry{}, ErrMdbEntrySize
	}
	e := MdbEntry{
		Ifindex: binary.LittleEndian.Uint32(b[0:4]),
		State:   b[4],
		Flags:   b[5],
		Vid:     binary.LittleEndian.Uint16(b[6:8]),
		Proto:   binary.BigEndian.Uint16(b[24:26]),
	}
	switch e.Proto {
	case MdbProtoIP:
		e.Group = CopyBytes(b[8:12])
	case MdbProtoIPv6:
		e.Group = CopyBytes(b[8:24])
	default:
		e.Group = CopyBytes(b[8:14])
	}
	return e, nil
}

// ParseMdbRequest decodes an RTM_NEWMDB or RTM_DELMDB body.
func ParseMdbRequest(body []byte) (MdbRequest, error) {
	if len(body) < BrPortMsgSizeCst {
		return MdbRequest{}, ErrBrPortMsgSmall
	}
	r := MdbRequest{Family: body[0], Ifindex: binary.LittleEndian.Uint32(body[4:8])}
	var perr error
	err := WalkRTAttrs(body[BrPortMsgSizeCst:], func(atype uint16, val []byte) {
		switch atype {
		case MdbaSetEntry:
			e, err := ParseMdbEntry(val)
			if err != nil {
				perr = err
				return
			}
			r.Entry, r.HasEntry = e, true
		case MdbaSetEntryAttrs:
			r.HasAttrs = true
			perr = errors.Join(perr, parseMdbAttrs(val, mdbeIDs, &r.Attrs))
		}
	})
	if err = errors.Join(err, perr); err != nil {
		return MdbRequest{}, err
	}
	return r, nil
}

// ParseMdbDump decodes one dump reply (a bridge's RTM_GETMDB or a VXLAN's
// RTM_NEWMDB) into its entries; the router ports (MDBA_ROUTER) are left out.
func ParseMdbDump(body []byte) ([]MdbDumpEntry, error) {
	if len(body) < BrPortMsgSizeCst {
		return nil, ErrBrPortMsgSmall
	}
	bridge := binary.LittleEndian.Uint32(body[4:8])
	var out []MdbDumpEntry
	var perr error
	err := WalkRTAttrs(body[BrPortMsgSizeCst:], func(atype uint16, mdb []byte) {
		if atype != MdbaMdb {
			return
		}
		perr = errors.Join(perr, WalkRTAttrsNested(mdb, func(atype uint16, ent []byte) {
			if atype != MdbaMdbEntry {
				return
			}
			perr = errors.Join(perr, WalkRTAttrsNested(ent, func(atype uint16, info []byte) {
				if atype != MdbaMdbEntryInfo {
					return
				}
				d, err := parseMdbEntryInfo(bridge, info)
				if err != nil {
					perr = errors.Join(perr, err)
					return
				}
				out = append(out, d)
			}))
		}))
	})
	if err = errors.Join(err, perr); err != nil {
		return nil, err
	}
	return out, nil
}

// parseMdbEntryInfo decodes MDBA_MDB_ENTRY_INFO: a br_mdb_entry with no
// attribute header (nla_put_nohdr), then the entry's attributes.
func parseMdbEntryInfo(bridge uint32, info []byte) (MdbDumpEntry, error) {
	if len(info) < MdbEntrySizeCst {
		return MdbDumpEntry{}, ErrMdbEntrySize
	}
	e, err := ParseMdbEntry(info[:MdbEntrySizeCst])
	if err != nil {
		return MdbDumpEntry{}, err
	}
	d := MdbDumpEntry{Bridge: bridge, Entry: e}
	if err := parseMdbAttrs(info[MdbEntrySizeCst:], eattrIDs, &d.Attrs); err != nil {
		return MdbDumpEntry{}, err
	}
	return d, nil
}

// parseMdbAttrs decodes one numbering of the entry attributes into a.
func parseMdbAttrs(nest []byte, ids mdbAttrIDs, a *MdbAttrs) error {
	var perr error
	err := WalkRTAttrsNested(nest, func(atype uint16, val []byte) {
		switch atype {
		case ids.source:
			a.Source = CopyBytes(val)
		case ids.srcList:
			a.HasSrcList = true
			perr = errors.Join(perr, parseMdbSrcList(val, a))
		case ids.groupMode:
			if len(val) >= 1 {
				a.GroupMode, a.HasGroupMode = val[0], true
			}
		case ids.rtProt:
			if len(val) >= 1 {
				a.RtProt, a.HasRtProt = val[0], true
			}
		case ids.dst:
			a.Dst = CopyBytes(val)
		case ids.dstPort:
			if len(val) >= 2 {
				a.DstPort, a.HasDstPort = binary.LittleEndian.Uint16(val[0:2]), true
			}
		case ids.vni:
			a.Vni, a.HasVni = neighU32(val)
		case ids.ifindex:
			a.Ifindex, a.HasIfindex = neighU32(val)
		case ids.srcVni:
			a.SrcVni, a.HasSrcVni = neighU32(val)
		case ids.timer:
			if ids.timer != 0 {
				a.Timer, a.HasTimer = neighU32(val)
			}
		}
	})
	return errors.Join(err, perr)
}

// parseMdbSrcList decodes a source list: each entry's address, in order.
func parseMdbSrcList(nest []byte, a *MdbAttrs) error {
	var perr error
	err := WalkRTAttrsNested(nest, func(atype uint16, ent []byte) {
		if atype != MdbSrcListEntry {
			return
		}
		perr = errors.Join(perr, WalkRTAttrsNested(ent, func(atype uint16, val []byte) {
			if atype == MdbSrcAttrAddress {
				a.SrcList = append(a.SrcList, CopyBytes(val))
			}
		}))
	})
	return errors.Join(err, perr)
}
