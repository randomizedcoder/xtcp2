package xtcpnl

import (
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// This file decodes RTM_NEWADDRLABEL, the reply to the RTM_GETADDRLABEL dump
// that `ip addrlabel show` issues. The addrlabel table is an IPv6-only, per-netns
// policy table (net/ipv6/addrlabel.c); a fresh netns carries the kernel's
// built-in default entries. See internal/goip/obj_addrlabel.go for the
// transaction and render/addrlabel.go for the line.

// IfAddrLblMsg mirrors the kernel's `struct ifaddrlblmsg`, the fixed header of
// an RTM_*ADDRLABEL message.
//
//	struct ifaddrlblmsg {
//		__u8	ifal_family;		/* Address family */
//		__u8	__ifal_reserved;	/* Reserved */
//		__u8	ifal_prefixlen;		/* Prefix length */
//		__u8	ifal_flags;		/* Flags */
//		__u32	ifal_index;		/* Link index */
//		__u32	ifal_seq;		/* sequence number */
//	};
//
// Reference: linux/include/uapi/linux/if_addrlabel.h
type IfAddrLblMsg struct {
	Family    uint8
	Reserved  uint8
	Prefixlen uint8
	Flags     uint8
	Index     int32
	Seq       uint32
}

const (
	// IfAddrlblmsgSizeCst is sizeof(struct ifaddrlblmsg): four bytes plus two
	// u32s. unix.SizeofIfAddrlblmsg carries the same value; this mirrors the
	// sibling bodies (NhMsgSizeCst, NdMsgSizeCst) so the deserializer reads a
	// local constant like the rest of the package.
	IfAddrlblmsgSizeCst = 12
)

var (
	// ErrIfAddrlblmsgSmall is a reply body shorter than the fixed ifaddrlblmsg
	// header.
	ErrIfAddrlblmsgSmall = errors.New("data too small for IfAddrLblMsg")
)

// AddrLabelInfo is a decoded RTM_NEWADDRLABEL reply: the ifaddrlblmsg header
// fields print_addrlabel renders plus its IFAL_* attributes.
//
// print_addrlabel (ip/ipaddrlabel.c:44-97) renders only Family (to format the
// address), Prefixlen, Index (the `dev`/`ifname` token, nonzero only for a
// per-device entry) and the two attributes. ifal_flags and ifal_seq are decoded
// for completeness but never printed.
type AddrLabelInfo struct {
	Family    uint8
	Prefixlen uint8
	Flags     uint8
	Index     int32 // 0 is absent: index 0 is not a device (the default table)
	Seq       uint32
	Address   []byte // IFAL_ADDRESS, network order, nil when absent
	Label     uint32 // IFAL_LABEL, valid when HasLabel
	HasLabel  bool
}

// DeserializeIfAddrLblMsg does an offset read of the fixed ifaddrlblmsg header.
func DeserializeIfAddrLblMsg(data []byte, h *IfAddrLblMsg) (n int, err error) {
	if len(data) < IfAddrlblmsgSizeCst {
		return 0, ErrIfAddrlblmsgSmall
	}
	h.Family = data[0]
	h.Reserved = data[1]
	h.Prefixlen = data[2]
	h.Flags = data[3]
	h.Index = int32(binary.LittleEndian.Uint32(data[4:8]))
	h.Seq = binary.LittleEndian.Uint32(data[8:12])
	return IfAddrlblmsgSizeCst, nil
}

// ParseNewAddrLabel decodes an RTM_NEWADDRLABEL reply body: the ifaddrlblmsg
// header then its IFAL_* attributes. Address is copied, so it does not alias the
// buffer.
//
// IFAL_LABEL is only accepted at exactly four bytes, mirroring print_addrlabel's
// `RTA_PAYLOAD(tb[IFAL_LABEL]) == sizeof(uint32_t)` guard (ip/ipaddrlabel.c:87):
// any other width leaves HasLabel false and renders no label token.
func ParseNewAddrLabel(body []byte) (AddrLabelInfo, error) {
	var ai AddrLabelInfo
	var h IfAddrLblMsg
	if _, err := DeserializeIfAddrLblMsg(body, &h); err != nil {
		return ai, err
	}
	ai.Family = h.Family
	ai.Prefixlen = h.Prefixlen
	ai.Flags = h.Flags
	ai.Index = h.Index
	ai.Seq = h.Seq

	err := WalkRTAttrs(body[IfAddrlblmsgSizeCst:], func(atype uint16, val []byte) {
		switch atype {
		case unix.IFAL_ADDRESS:
			ai.Address = CopyBytes(val)
		case unix.IFAL_LABEL:
			if len(val) == 4 {
				ai.Label = binary.LittleEndian.Uint32(val[0:4])
				ai.HasLabel = true
			}
		}
	})
	if err != nil {
		return ai, err
	}
	return ai, nil
}
