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

// IF_OPER_* are the RFC 2863 operational states carried in IFLA_OPERSTATE.
// golang.org/x/sys/unix does not export them (unlike IFLA_OPERSTATE itself), so
// they are declared here from the kernel UAPI, as RtaNhID is in
// xtcpnl_rtmsg.go.
//
// Reference: https://github.com/torvalds/linux/blob/master/include/uapi/linux/if.h
const (
	IfOperUnknown        uint8 = 0
	IfOperNotPresent     uint8 = 1
	IfOperDown           uint8 = 2
	IfOperLowerLayerDown uint8 = 3
	IfOperTesting        uint8 = 4
	IfOperDormant        uint8 = 5
	IfOperUp             uint8 = 6
)

// LinkInfo is the subset of an RTM_*LINK message xtcp2 keeps: the interface
// index, flags, and name (IFLA_IFNAME), used to label addresses/routes per
// link, plus the state fields a link up/down event turns on.
//
// Change (ifi_change) is a mask of which IFF_* bits this particular message
// reports as having changed. A full dump reply carries 0; a notification
// triggered by `ip link set dev X down` carries IFF_UP. It is therefore the way
// to tell "this link happens to be down" from "this link just went down".
//
// OperState and Carrier come from optional attributes: both read 0 when the
// attribute is absent, which for OperState coincides with the real value
// IfOperUnknown. Prefer the Flags-derived helpers (IsUp, IsAdminDown,
// IsCarrierDown) for decisions, since ifi_flags is always present.
type LinkInfo struct {
	Index     int32
	Flags     uint32
	Name      string
	Change    uint32 // ifi_change — which IFF_* bits this message reports changing
	Type      uint16 // ifi_type — ARPHRD_* (ARPHRD_ETHER, ARPHRD_LOOPBACK, …)
	OperState uint8  // IFLA_OPERSTATE (IF_OPER_*); IfOperUnknown if absent
	Carrier   uint8  // IFLA_CARRIER (0/1); 0 if absent
	MTU       uint32 // IFLA_MTU; 0 if absent
}

// IsUp reports whether the link is both administratively up and operationally
// running — IFF_UP and IFF_RUNNING together, which is what "usable" means.
func (li LinkInfo) IsUp() bool {
	const up = unix.IFF_UP | unix.IFF_RUNNING
	return li.Flags&up == up
}

// IsAdminDown reports an administrative down: IFF_UP is clear, i.e. someone ran
// `ip link set dev X down`.
func (li LinkInfo) IsAdminDown() bool {
	return li.Flags&unix.IFF_UP == 0
}

// IsCarrierDown reports a carrier loss: the link is administratively up but not
// running, i.e. the cable is out or the veth peer went away.
//
// This is the distinction that makes link events worth having — both cases show
// up as "not usable", but only one of them is an operator action.
func (li LinkInfo) IsCarrierDown() bool {
	return li.Flags&unix.IFF_UP != 0 && li.Flags&unix.IFF_RUNNING == 0
}

// ParseNewLink decodes an RTM_NEWLINK or RTM_DELLINK message body (the bytes
// after the nlmsghdr): the ifinfomsg header followed by IFLA_* attributes.
// IFLA_IFNAME, IFLA_OPERSTATE, IFLA_CARRIER and IFLA_MTU are extracted.
//
// The name is retained for the dump path; RTM_DELLINK carries the same layout,
// so ParseRtnetlinkEvent reuses this for both senses.
func ParseNewLink(body []byte) (LinkInfo, error) {
	var m IfInfomsg
	if _, err := DeserializeIfInfomsg(body, &m); err != nil {
		return LinkInfo{}, err
	}

	li := LinkInfo{
		Index:  m.Index,
		Flags:  m.Flags,
		Change: m.Change,
		Type:   m.Type,
	}
	err := walkRTAttrs(body[IfInfomsgSizeCst:], func(atype uint16, val []byte) {
		switch atype {
		case uint16(unix.IFLA_IFNAME):
			li.Name = string(bytes.TrimRight(val, "\x00"))
		case uint16(unix.IFLA_OPERSTATE):
			if len(val) >= 1 {
				li.OperState = val[0]
			}
		case uint16(unix.IFLA_CARRIER):
			if len(val) >= 1 {
				li.Carrier = val[0]
			}
		case uint16(unix.IFLA_MTU):
			if len(val) >= 4 {
				li.MTU = binary.LittleEndian.Uint32(val[0:4])
			}
		}
	})
	if err != nil {
		return LinkInfo{}, err
	}
	return li, nil
}
