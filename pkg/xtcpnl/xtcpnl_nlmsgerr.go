package xtcpnl

import (
	"encoding/binary"
	"errors"
	"strings"

	"golang.org/x/sys/unix"
)

// NlMsgErr is a decoded NLMSG_ERROR: the errno, the header of the request
// it answers, and the extended ack the kernel attached.
//
// The layout is netlink_ack's (net/netlink/af_netlink.c): an int error,
// the request's nlmsghdr, the request's payload unless NLM_F_CAPPED is set
// on the error message, then NLMSGERR_ATTR_* TLVs if NLM_F_ACK_TLVS is set.
// A success ACK is always capped; a refusal is capped only when the socket
// asked for NETLINK_CAP_ACK. iproute2's does not: rtnl_open sets
// NETLINK_EXT_ACK alone (lib/libnetlink.c:222), so every refusal in the
// link-write corpus echoes the whole request ahead of its TLVs.
//
// This is the write path's reply. The read path stops at the errno
// (netlinkErr), because a dump that fails has nothing else to say; a
// refused write does, and its extack message is what `ip` prints after
// "Error:".
type NlMsgErr struct {
	// Errno is the kernel's int: 0 for an ACK, otherwise a negative errno.
	Errno int32
	// RequestType, RequestFlags and RequestSeq are from the echoed header.
	RequestType  uint16
	RequestFlags uint16
	RequestSeq   uint32
	// Capped is NLM_F_CAPPED: the request's payload was not echoed.
	Capped bool
	// Msg is NLMSGERR_ATTR_MSG without its NUL; HasMsg says it was there.
	Msg    string
	HasMsg bool
	// Offset is NLMSGERR_ATTR_OFFS, the byte offset of the attribute the
	// kernel objected to, counted from the start of the request's header.
	Offset    uint32
	HasOffset bool
}

// ErrNlMsgErrShort is an NLMSG_ERROR body too short for what its flags say
// it holds.
var ErrNlMsgErrShort = errors.New("xtcpnl: NLMSG_ERROR body is truncated")

// nlMsgErrFixedCst is the int error plus the echoed nlmsghdr.
const nlMsgErrFixedCst = 4 + NlMsgHdrSizeCst

// ParseNlMsgErr decodes an NLMSG_ERROR body. flags is the error message's
// own nlmsg_flags, which say whether the payload is capped and whether TLVs
// follow; they are not inside the body.
func ParseNlMsgErr(flags uint16, body []byte) (NlMsgErr, error) {
	if len(body) < nlMsgErrFixedCst {
		return NlMsgErr{}, ErrNlMsgErrShort
	}
	e := NlMsgErr{
		Errno:        int32(binary.LittleEndian.Uint32(body[0:4])), //nolint:gosec // the wire int
		RequestType:  binary.LittleEndian.Uint16(body[8:10]),
		RequestFlags: binary.LittleEndian.Uint16(body[10:12]),
		RequestSeq:   binary.LittleEndian.Uint32(body[12:16]),
		Capped:       flags&unix.NLM_F_CAPPED != 0,
	}
	rest := body[nlMsgErrFixedCst:]
	if !e.Capped {
		reqLen := int(binary.LittleEndian.Uint32(body[4:8]))
		if reqLen < NlMsgHdrSizeCst {
			return NlMsgErr{}, ErrNlMsgErrShort
		}
		echoed := nlmsgAlign(reqLen - NlMsgHdrSizeCst)
		if echoed > len(rest) {
			return NlMsgErr{}, ErrNlMsgErrShort
		}
		rest = rest[echoed:]
	}
	if flags&unix.NLM_F_ACK_TLVS == 0 {
		return e, nil
	}
	err := WalkRTAttrs(rest, func(atype uint16, val []byte) {
		switch atype {
		case unix.NLMSGERR_ATTR_MSG:
			e.Msg = strings.TrimRight(string(val), "\x00")
			e.HasMsg = true
		case unix.NLMSGERR_ATTR_OFFS:
			if len(val) >= 4 {
				e.Offset = binary.LittleEndian.Uint32(val)
				e.HasOffset = true
			}
		default:
			// COOKIE, POLICY and the MISS_* pair: no rtnetlink link
			// write in the corpus provokes them.
		}
	})
	if err != nil {
		return NlMsgErr{}, err
	}
	return e, nil
}

func nlmsgAlign(n int) int { return (n + 3) &^ 3 }
