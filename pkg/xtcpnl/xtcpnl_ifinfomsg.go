package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// IfInfomsg mirrors the kernel's `struct ifinfomsg` — the family header of an
// RTM_*LINK message.
//
//	struct ifinfomsg {
//		unsigned char	ifi_family;
//		unsigned char	__ifi_pad;
//		unsigned short	ifi_type;
//		int		ifi_index;
//		unsigned	ifi_flags;
//		unsigned	ifi_change;
//	};
//
// Reference: https://github.com/torvalds/linux/blob/master/include/uapi/linux/rtnetlink.h
type IfInfomsg struct {
	Family uint8  // 1
	Pad    uint8  // 1
	Type   uint16 // 2
	Index  int32  // 4
	Flags  uint32 // 4
	Change uint32 // 4 = 16 ( 16 / 4 = 4 )
}

const (
	IfInfomsgSizeCst = 16
	IfInfomsgReadCst = IfInfomsgSizeCst
)

var (
	ErrIfInfomsgSmall = errors.New("data too small for IfInfomsg")
)

// DeserializeIfInfomsg does a binary read of an IfInfomsg with a basic length
// check.
func DeserializeIfInfomsg(data []byte, m *IfInfomsg) (n int, err error) {
	if len(data) < IfInfomsgSizeCst {
		return 0, ErrIfInfomsgSmall
	}

	m.Family = data[0]
	m.Pad = data[1]
	m.Type = binary.LittleEndian.Uint16(data[2:4])
	m.Index = int32(binary.LittleEndian.Uint32(data[4:8]))
	m.Flags = binary.LittleEndian.Uint32(data[8:12])
	m.Change = binary.LittleEndian.Uint32(data[12:16])

	return IfInfomsgReadCst, nil
}

func DeserializeIfInfomsgReflection(data []byte, m *IfInfomsg) (n int, err error) {
	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, m)
	if err != nil {
		return 0, err
	}

	return IfInfomsgReadCst, err
}

// LinkInfo is the subset of an RTM_NEWLINK message xtcp2 keeps: the interface
// index, flags, and name (IFLA_IFNAME), used to label addresses/routes per
// link.
type LinkInfo struct {
	Index int32
	Flags uint32
	Name  string
}

// ParseNewLink decodes an RTM_NEWLINK message body (the bytes after the
// nlmsghdr): the ifinfomsg header followed by IFLA_* attributes. Only
// IFLA_IFNAME is extracted.
func ParseNewLink(body []byte) (LinkInfo, error) {
	var m IfInfomsg
	if _, err := DeserializeIfInfomsg(body, &m); err != nil {
		return LinkInfo{}, err
	}

	li := LinkInfo{Index: m.Index, Flags: m.Flags}
	err := walkRTAttrs(body[IfInfomsgSizeCst:], func(atype uint16, val []byte) {
		if atype == uint16(unix.IFLA_IFNAME) {
			li.Name = string(bytes.TrimRight(val, "\x00"))
		}
	})
	if err != nil {
		return LinkInfo{}, err
	}
	return li, nil
}
