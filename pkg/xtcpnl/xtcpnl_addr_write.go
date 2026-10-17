package xtcpnl

import (
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// This file encodes the requests `ip address add/change/replace/delete` sends:
// the ifaddrmsg and the IFA_* attributes. Phase 5 of
// docs/netlink/addr-write-path.md.
//
// It encodes; it does not parse argv. ipaddr_modify (ip/ipaddress.c:2448)
// appends an attribute as each keyword is read (:2477-2585), so that order is
// argv order and internal/goip's business, and then appends more after the
// loop: IFA_FLAGS, the IFA_ADDRESS copy of the local, the computed broadcast
// and IFA_CACHEINFO (:2589-2656). An AddrWrite carries the two groups as two
// ordered lists, and this file writes them as given. What it owns is the one
// rule between them: flags that fit a byte go in the header, and wider flags
// go only in IFA_FLAGS, between the groups, with the header byte 0. That rule
// is here and nowhere else, so a caller's IFA_FLAGS is refused.
//
// Nothing here sends. BuildAddrWriteRequest goes through BuildWriteRequest,
// and goip writes the result to a file (link-write-path.md, decision 5).

// addrWriteBufCst is ipaddr_modify's char buf[256], the room after the
// nlmsghdr and ifaddrmsg: every addattr call gets sizeof(req) as maxlen.
//
// ipaddr_modify ignores addattr_l's return, so past the bound `ip` prints
// "addattr_l ERROR: message exceeded bound of 280" and sends the request
// without that attribute. Refusing is the one divergence, and only a label
// can reach it; the kernel refuses any label over 15 bytes anyway.
const addrWriteBufCst = 256

// ifaFlagsHdrMaxCst is the widest flags word ifa_flags holds.
const ifaFlagsHdrMaxCst = 0xff

var (
	// ErrAddrWriteType is an AddrWrite whose MsgType is not RTM_NEWADDR or
	// RTM_DELADDR. BuildWriteRequest would admit route and link types too;
	// an address write must not carry one.
	ErrAddrWriteType = errors.New("xtcpnl: an address write is RTM_NEWADDR or RTM_DELADDR")
	// ErrAddrWriteFlagsAttr is an IFA_FLAGS in an AddrWrite's attributes.
	// BuildAddrWriteRequest writes IFA_FLAGS from IfaFlags, and a second one
	// would put the flags on the wire twice.
	ErrAddrWriteFlagsAttr = errors.New("xtcpnl: IFA_FLAGS is written from AddrWrite.IfaFlags, not passed as an attribute")
)

// AddrAttr is one IFA_* attribute of an address write, payload already
// encoded. The constructors below are the shapes ipaddr_modify writes.
type AddrAttr struct {
	Type    uint16
	Payload []byte
}

// AddrAttrU8 is addattr8: IFA_PROTO. rta_len is 5, padded to 8.
func AddrAttrU8(atype uint16, v uint8) AddrAttr {
	return AddrAttr{Type: atype, Payload: []byte{v}}
}

// AddrAttrU32 is addattr32: IFA_RT_PRIORITY.
func AddrAttrU32(atype uint16, v uint32) AddrAttr {
	p := make([]byte, 4)
	binary.LittleEndian.PutUint32(p, v)
	return AddrAttr{Type: atype, Payload: p}
}

// AddrAttrString is addattr_l with strlen+1: the NUL is part of the payload.
// IFA_LABEL is written this way.
func AddrAttrString(atype uint16, s string) AddrAttr {
	return AddrAttr{Type: atype, Payload: append([]byte(s), 0)}
}

// AddrAttrBytes is addattr_l of raw bytes: IFA_LOCAL, IFA_ADDRESS,
// IFA_BROADCAST and IFA_ANYCAST, each the address's 4 or 16 bytes.
func AddrAttrBytes(atype uint16, b []byte) AddrAttr {
	return AddrAttr{Type: atype, Payload: b}
}

// AddrAttrCacheinfo is IFA_CACHEINFO as ipaddr_modify fills it: a struct
// ifa_cacheinfo with the two lifetimes and both timestamps 0 (:2641-2656).
func AddrAttrCacheinfo(preferred, valid uint32) AddrAttr {
	p := make([]byte, 0, 16)
	p = binary.LittleEndian.AppendUint32(p, preferred)
	p = binary.LittleEndian.AppendUint32(p, valid)
	p = binary.LittleEndian.AppendUint32(p, 0) // cstamp
	p = binary.LittleEndian.AppendUint32(p, 0) // tstamp
	return AddrAttr{Type: uint16(unix.IFA_CACHEINFO), Payload: p}
}

// AddrWrite is one address write as ipaddr_modify assembles it.
type AddrWrite struct {
	// MsgType is RTM_NEWADDR (add, change, replace) or RTM_DELADDR.
	MsgType uint16
	// Flags are the NLM_F_* bits besides NLM_F_REQUEST: the verb's, from
	// do_ipaddr, and NLM_F_ACK, which rtnl_talk adds.
	Flags uint16

	// The ifaddrmsg, less ifa_flags, which comes from IfaFlags.
	Family    uint8
	Prefixlen uint8
	Scope     uint8
	Index     uint32

	// IfaFlags is the whole IFA_F_* word. It is the header byte when it fits
	// one, and otherwise IFA_FLAGS with the header byte 0; never both
	// (:2589-2592).
	IfaFlags uint32

	// Attrs are the attributes the argv loop appends, in argv order.
	Attrs []AddrAttr
	// PostAttrs are the ones appended after the loop and after IFA_FLAGS:
	// the IFA_ADDRESS copy of the local, the computed broadcast of `brd +`
	// or `brd -`, and IFA_CACHEINFO, in that order.
	PostAttrs []AddrAttr
}

// EncodeIfAddrmsg lays out a struct ifaddrmsg.
func EncodeIfAddrmsg(family, prefixlen, flags, scope uint8, index uint32) []byte {
	b := make([]byte, 0, IfAddrmsgSizeCst)
	b = append(b, family, prefixlen, flags, scope)
	return binary.LittleEndian.AppendUint32(b, index)
}

// BuildAddrWriteRequest lays out w: the ifaddrmsg, w.Attrs in order,
// IFA_FLAGS when the flags do not fit the header byte, then w.PostAttrs in
// order.
func BuildAddrWriteRequest(w AddrWrite, seq uint32) ([]byte, error) {
	if w.MsgType != uint16(unix.RTM_NEWADDR) && w.MsgType != uint16(unix.RTM_DELADDR) {
		return nil, ErrAddrWriteType
	}
	for _, list := range [][]AddrAttr{w.Attrs, w.PostAttrs} {
		for _, a := range list {
			if a.Type == uint16(unix.IFA_FLAGS) {
				return nil, ErrAddrWriteFlagsAttr
			}
		}
	}

	// The low byte of the little-endian word, taken without a narrowing
	// conversion; it is used only when it is the whole word.
	var hdrFlags uint8
	if w.IfaFlags <= ifaFlagsHdrMaxCst {
		hdrFlags = binary.LittleEndian.AppendUint32(nil, w.IfaFlags)[0]
	}

	var raw [addrWriteBufCst]byte
	ab := NewAttrBuilder(raw[:])
	if err := putAddrAttrs(&ab, w.Attrs); err != nil {
		return nil, err
	}
	if w.IfaFlags > ifaFlagsHdrMaxCst {
		if err := ab.PutU32(uint16(unix.IFA_FLAGS), w.IfaFlags); err != nil {
			return nil, err
		}
	}
	if err := putAddrAttrs(&ab, w.PostAttrs); err != nil {
		return nil, err
	}
	hdr := EncodeIfAddrmsg(w.Family, w.Prefixlen, hdrFlags, w.Scope, w.Index)
	return BuildWriteRequest(w.MsgType, w.Flags, seq, hdr, ab.Bytes())
}

func putAddrAttrs(ab *AttrBuilder, attrs []AddrAttr) error {
	for _, a := range attrs {
		if err := ab.PutBytes(a.Type, a.Payload); err != nil {
			return err
		}
	}
	return nil
}
