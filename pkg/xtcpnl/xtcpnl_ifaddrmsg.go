package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// IfAddrmsg mirrors the kernel's `struct ifaddrmsg` — the family header of an
// RTM_*ADDR message.
//
//	struct ifaddrmsg {
//		__u8	ifa_family;
//		__u8	ifa_prefixlen;	/* The prefix length		*/
//		__u8	ifa_flags;	/* Flags			*/
//		__u8	ifa_scope;	/* Address scope		*/
//		__u32	ifa_index;	/* Link index			*/
//	};
//
// Reference: https://github.com/torvalds/linux/blob/master/include/uapi/linux/if_addr.h
type IfAddrmsg struct {
	Family    uint8  // 1
	Prefixlen uint8  // 1
	Flags     uint8  // 1
	Scope     uint8  // 1
	Index     uint32 // 4 = 8 ( 8 / 4 = 2 )
}

const (
	IfAddrmsgSizeCst = 8
	IfAddrmsgReadCst = IfAddrmsgSizeCst
)

var (
	ErrIfAddrmsgSmall = errors.New("data too small for IfAddrmsg")
)

// DeserializeIfAddrmsg does a binary read of an IfAddrmsg with a basic length
// check.
func DeserializeIfAddrmsg(data []byte, m *IfAddrmsg) (n int, err error) {
	if len(data) < IfAddrmsgSizeCst {
		return 0, ErrIfAddrmsgSmall
	}

	m.Family = data[0]
	m.Prefixlen = data[1]
	m.Flags = data[2]
	m.Scope = data[3]
	m.Index = binary.LittleEndian.Uint32(data[4:8])

	return IfAddrmsgReadCst, nil
}

func DeserializeIfAddrmsgReflection(data []byte, m *IfAddrmsg) (n int, err error) {
	reader := bytes.NewReader(data)

	err = binary.Read(reader, binary.LittleEndian, m)
	if err != nil {
		return 0, err
	}

	return IfAddrmsgReadCst, err
}

// AddrInfo is the subset of an RTM_NEWADDR message xtcp2 keeps. The prefix
// length comes from the ifaddrmsg header (not an attribute). Address/Local hold
// the raw network-order address bytes (4 for IPv4, 16 for IPv6); the caller
// builds a netip.Addr from them. For IPv4 the kernel sends both IFA_LOCAL (the
// local address) and IFA_ADDRESS (the peer, on point-to-point links); when they
// differ, Local is authoritative for "this host's address".
type AddrInfo struct {
	Family    uint8
	Prefixlen uint8
	Scope     uint8
	Index     uint32
	Address   []byte // IFA_ADDRESS
	Local     []byte // IFA_LOCAL
	Label     string // IFA_LABEL
}

// ParseNewAddr decodes an RTM_NEWADDR message body (the bytes after the
// nlmsghdr): the ifaddrmsg header followed by IFA_* attributes.
func ParseNewAddr(body []byte) (AddrInfo, error) {
	var m IfAddrmsg
	if _, err := DeserializeIfAddrmsg(body, &m); err != nil {
		return AddrInfo{}, err
	}

	ai := AddrInfo{
		Family:    m.Family,
		Prefixlen: m.Prefixlen,
		Scope:     m.Scope,
		Index:     m.Index,
	}
	err := walkRTAttrs(body[IfAddrmsgSizeCst:], func(atype uint16, val []byte) {
		switch atype {
		case uint16(unix.IFA_ADDRESS):
			ai.Address = copyBytes(val)
		case uint16(unix.IFA_LOCAL):
			ai.Local = copyBytes(val)
		case uint16(unix.IFA_LABEL):
			ai.Label = string(bytes.TrimRight(val, "\x00"))
		}
	})
	if err != nil {
		return AddrInfo{}, err
	}
	return ai, nil
}
