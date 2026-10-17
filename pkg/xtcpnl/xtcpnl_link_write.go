package xtcpnl

import (
	"encoding/binary"
	"errors"
	"net/netip"
	"strings"

	"golang.org/x/sys/unix"
)

// This file encodes the requests `ip link add/set/change/replace/delete`
// sends: the ifinfomsg, the generic IFLA_* attributes, the IFLA_LINKINFO nest,
// and the per-kind data nests for bond, bond_slave and veth. Phase 5 of
// docs/netlink/link-write-path.md.
//
// It encodes; it does not parse argv. The order of the generic attributes is
// iplink_parse's business (internal/goip), so a LinkWrite carries them as an
// ordered list and this file writes them as given. What this file owns is the
// layout iplink_modify imposes after parsing (ip/iplink.c): the attributes,
// then IFLA_LINKINFO with a NUL-terminated IFLA_INFO_KIND, then the data nest,
// whose type the kind's spelling decides.
//
// Nothing here sends. BuildLinkWriteRequest goes through BuildWriteRequest,
// and goip writes the result to a file (link-write-path.md, decision 5).

// linkWriteBufCst is sizeof(struct iplink_req) in ip/iplink.c: the nlmsghdr,
// the ifinfomsg and char buf[1024]. iproute2 passes it as maxlen to the
// generic addattr calls, so a request longer than this is one `ip` cannot
// build either.
const linkWriteBufCst = NlMsgHdrSizeCst + IfInfomsgSizeCst + 1024

// slaveKindSuffixCst is the spelling that sends a kind's options to
// IFLA_INFO_SLAVE_DATA instead of IFLA_INFO_DATA.
const slaveKindSuffixCst = "_slave"

var (
	// ErrLinkWriteType is a LinkWrite whose MsgType is not RTM_NEWLINK or
	// RTM_DELLINK. BuildWriteRequest would admit route and address types too;
	// a link write must not carry one.
	ErrLinkWriteType = errors.New("xtcpnl: a link write is RTM_NEWLINK or RTM_DELLINK")
	// ErrLinkWriteDataWithoutKind is kind data with no kind: iplink_modify
	// opens the data nest inside IFLA_LINKINFO, so there is nowhere to put it.
	ErrLinkWriteDataWithoutKind = errors.New("xtcpnl: link kind data without a kind")
)

// LinkAttr is one generic IFLA_* attribute of a link write, payload already
// encoded. The constructors below are the three shapes iplink_parse writes.
type LinkAttr struct {
	Type    uint16
	Payload []byte
}

// LinkAttrU32 is addattr32: IFLA_MTU, IFLA_MASTER, IFLA_TXQLEN, IFLA_GROUP.
func LinkAttrU32(atype uint16, v uint32) LinkAttr {
	p := make([]byte, 4)
	binary.LittleEndian.PutUint32(p, v)
	return LinkAttr{Type: atype, Payload: p}
}

// LinkAttrString is addattr_l with strlen+1: the NUL is part of the payload.
// IFLA_IFNAME is written this way.
func LinkAttrString(atype uint16, s string) LinkAttr {
	return LinkAttr{Type: atype, Payload: append([]byte(s), 0)}
}

// LinkAttrBytes is addattr_l of raw bytes: IFLA_ADDRESS, and IFLA_IFALIAS,
// the one string iplink_parse writes without a NUL (strlen, not strlen+1).
func LinkAttrBytes(atype uint16, b []byte) LinkAttr {
	return LinkAttr{Type: atype, Payload: b}
}

// LinkWrite is one link write as iplink_modify assembles it.
type LinkWrite struct {
	// MsgType is RTM_NEWLINK (add, set, change, replace) or RTM_DELLINK.
	MsgType uint16
	// Flags are the NLM_F_* bits besides NLM_F_REQUEST: CREATE|EXCL for add,
	// CREATE|REPLACE for replace, and NLM_F_ACK, which rtnl_talk adds.
	Flags uint16

	// The ifinfomsg. Index is 0 when the device is addressed by name.
	// IfFlags and IfChange go together: the kernel reads a zero change mask
	// as ~0 (net/core/rtnetlink.c), so a flags word without one would
	// replace every flag.
	Family   uint8
	Index    int32
	IfFlags  uint32
	IfChange uint32

	// Attrs are the generic attributes in wire order, IFLA_IFNAME included
	// where iplink_parse puts it.
	Attrs []LinkAttr

	// Kind is `type KIND`. Empty means no IFLA_LINKINFO at all.
	Kind string
	// Data is the inside of the kind's data nest, already encoded, and
	// HasData says whether the nest is there. They are separate because
	// iplink_modify opens the nest only when arguments follow the kind, so
	// a bare `type bond` has none while an empty nest is a different
	// request.
	Data    []byte
	HasData bool
}

// LinkKindDataType is the data nest a kind's options go in. iplink_modify
// takes the FIRST underscore in the kind and compares the rest with
// "_slave", so "bond_slave" is IFLA_INFO_SLAVE_DATA and a hypothetical
// "foo_bar_slave" is IFLA_INFO_DATA.
func LinkKindDataType(kind string) uint16 {
	if i := strings.IndexByte(kind, '_'); i >= 0 && kind[i:] == slaveKindSuffixCst {
		return uint16(unix.IFLA_INFO_SLAVE_DATA)
	}
	return uint16(unix.IFLA_INFO_DATA)
}

// EncodeIfInfomsg lays out a struct ifinfomsg. ifi_type is 0 on a request.
func EncodeIfInfomsg(family uint8, index int32, flags, change uint32) []byte {
	// family, pad, ifi_type, then index, flags and change.
	b := make([]byte, 0, IfInfomsgSizeCst)
	b = append(b, family, 0, 0, 0)
	b = binary.LittleEndian.AppendUint32(b, uint32(index)) //nolint:gosec // the wire int, reinterpreted
	b = binary.LittleEndian.AppendUint32(b, flags)
	return binary.LittleEndian.AppendUint32(b, change)
}

// BuildLinkWriteRequest lays out w: the ifinfomsg, w.Attrs in order, then
// IFLA_LINKINFO{IFLA_INFO_KIND "kind\0", data nest} when w.Kind is set.
//
// Neither nest carries NLA_F_NESTED, because addattr_nest does not set it
// and the kernel parses these nests with nla_parse_nested_deprecated.
func BuildLinkWriteRequest(w LinkWrite, seq uint32) ([]byte, error) {
	if w.MsgType != uint16(unix.RTM_NEWLINK) && w.MsgType != uint16(unix.RTM_DELLINK) {
		return nil, ErrLinkWriteType
	}
	if w.Kind == "" && w.HasData {
		return nil, ErrLinkWriteDataWithoutKind
	}

	var raw [linkWriteBufCst - NlMsgHdrSizeCst - IfInfomsgSizeCst]byte
	ab := NewAttrBuilder(raw[:])
	for _, a := range w.Attrs {
		if err := ab.PutBytes(a.Type, a.Payload); err != nil {
			return nil, err
		}
	}
	if w.Kind != "" {
		if err := putLinkInfo(&ab, w); err != nil {
			return nil, err
		}
	}
	hdr := EncodeIfInfomsg(w.Family, w.Index, w.IfFlags, w.IfChange)
	return BuildWriteRequest(w.MsgType, w.Flags, seq, hdr, ab.Bytes())
}

func putLinkInfo(ab *AttrBuilder, w LinkWrite) error {
	linkinfo, err := ab.BeginNest(uint16(unix.IFLA_LINKINFO))
	if err != nil {
		return err
	}
	if err := ab.PutString(uint16(unix.IFLA_INFO_KIND), w.Kind); err != nil {
		return err
	}
	if w.HasData {
		data, err := ab.BeginNest(LinkKindDataType(w.Kind))
		if err != nil {
			return err
		}
		if err := putRaw(ab, w.Data); err != nil {
			return err
		}
		if err := ab.EndNest(data); err != nil {
			return err
		}
	}
	return ab.EndNest(linkinfo)
}

// putRaw appends pre-encoded attributes. They are already aligned, because
// every encoder here writes whole padded attributes.
func putRaw(ab *AttrBuilder, b []byte) error {
	if len(b) > len(ab.buf)-ab.n {
		return ErrAttrNoSpace
	}
	ab.n += copy(ab.buf[ab.n:], b)
	return nil
}

// BondSetting is one bond or bond-port option as bond_parse_opt (or
// bond_slave_parse_opt) puts it on the wire: the table row, and the value in
// the field the row's Wire reads.
type BondSetting struct {
	Spec *BondOptionSpec
	// Value is the scalar for U8, U16, U32 and S32 rows, already in range.
	Value uint32
	// Bytes is the MAC row's payload as ll_addr_a2n produced it. Its length
	// is whatever the argument had, not ETH_ALEN: `ip` sends a short address
	// and the kernel refuses it.
	Bytes []byte
	// Targets is the IPv4Set or IPv6Set row's list, at most 16 long. Empty
	// is the keyword with no value: an empty nest, which clears every
	// target.
	Targets []netip.Addr
}

// EncodeBondInfoData encodes the inside of IFLA_INFO_DATA for kind "bond":
// one attribute per setting, in the order given, which is argv order. The
// kernel applies them in its own order whatever the wire says.
func EncodeBondInfoData(settings []BondSetting) ([]byte, error) {
	return encodeBondSettings(settings)
}

// EncodeBondSlaveData is EncodeBondInfoData for IFLA_INFO_SLAVE_DATA, kind
// "bond_slave", over BondSlaveOptions rows.
func EncodeBondSlaveData(settings []BondSetting) ([]byte, error) {
	return encodeBondSettings(settings)
}

// ErrBondSettingWire is a setting whose row has no write encoding: AD_INFO,
// which the kernel reports and no keyword sets, or a nil row.
var ErrBondSettingWire = errors.New("xtcpnl: bond option has no write encoding")

func encodeBondSettings(settings []BondSetting) ([]byte, error) {
	var raw [linkWriteBufCst]byte
	ab := NewAttrBuilder(raw[:])
	for _, s := range settings {
		if err := putBondSetting(&ab, s); err != nil {
			return nil, err
		}
	}
	return append([]byte(nil), ab.Bytes()...), nil
}

func putBondSetting(ab *AttrBuilder, s BondSetting) error {
	if s.Spec == nil || s.Spec.ReadOnly {
		return ErrBondSettingWire
	}
	id := s.Spec.ID
	switch s.Spec.Wire {
	case BondWireU8:
		return ab.PutU8(id, uint8(s.Value)) //nolint:gosec // range checked by the parser
	case BondWireU16:
		return ab.PutU16(id, uint16(s.Value)) //nolint:gosec // range checked by the parser
	case BondWireU32, BondWireS32:
		return ab.PutU32(id, s.Value)
	case BondWireMAC:
		return ab.PutBytes(id, s.Bytes)
	case BondWireIPv4Set, BondWireIPv6Set:
		return putBondTargets(ab, s)
	case BondWireNest:
		return ErrBondSettingWire
	}
	return ErrBondSettingWire
}

// putBondTargets is the arp_ip_target / ns_ip6_target nest: each address is a
// sub-attribute whose type is its position (iplink_bond.c, addattr32(n,
// 1024, i, addr) and addattr_l(n, 1024, i, …, 16)). An IPv4 address goes in
// network order, which is As4's order.
func putBondTargets(ab *AttrBuilder, s BondSetting) error {
	if len(s.Targets) > bondMaxTargetsCst {
		return ErrAttrTooLong
	}
	m, err := ab.BeginNest(s.Spec.ID)
	if err != nil {
		return err
	}
	for i, a := range s.Targets {
		var p []byte
		if s.Spec.Wire == BondWireIPv4Set {
			b := a.As4()
			p = b[:]
		} else {
			b := a.As16()
			p = b[:]
		}
		if err := ab.PutBytes(uint16(i), p); err != nil { //nolint:gosec // i < 16
			return err
		}
	}
	return ab.EndNest(m)
}

// VethPeer is the `peer` half of `type veth peer …`: its own ifinfomsg and
// attributes, which veth_parse_opt writes inside VETH_INFO_PEER by running
// iplink_parse a second time on the same request.
type VethPeer struct {
	Index    int32
	IfFlags  uint32
	IfChange uint32
	Attrs    []LinkAttr
}

// VethInfoPeer is VETH_INFO_PEER, include/uapi/linux/veth.h. x/sys/unix does
// not name it.
const VethInfoPeer uint16 = 1

// EncodeVethPeer encodes the inside of IFLA_INFO_DATA for kind "veth": one
// VETH_INFO_PEER nest whose payload opens with a raw 16-byte ifinfomsg (the
// kernel's policy is .len = sizeof(struct ifinfomsg), drivers/net/veth.c)
// followed by the peer's attributes.
//
// veth_parse_opt zeroes the outer header's index, flags and change while it
// parses the peer, moves what the peer parse set into the peer header, and
// restores the outer ones. It never writes the peer header's family or type:
// those bytes are the request buffer's, zero-initialized, so both are 0 even
// under `ip -6 link add`.
func EncodeVethPeer(p VethPeer) ([]byte, error) {
	var raw [linkWriteBufCst]byte
	ab := NewAttrBuilder(raw[:])
	m, err := ab.BeginNest(VethInfoPeer)
	if err != nil {
		return nil, err
	}
	if err := putRaw(&ab, EncodeIfInfomsg(unix.AF_UNSPEC, p.Index, p.IfFlags, p.IfChange)); err != nil {
		return nil, err
	}
	for _, a := range p.Attrs {
		if err := ab.PutBytes(a.Type, a.Payload); err != nil {
			return nil, err
		}
	}
	if err := ab.EndNest(m); err != nil {
		return nil, err
	}
	return append([]byte(nil), ab.Bytes()...), nil
}
