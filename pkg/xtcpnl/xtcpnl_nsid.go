package xtcpnl

import (
	"encoding/binary"
	"errors"

	"golang.org/x/sys/unix"
)

// This file decodes the RTM_*NSID messages: the kernel's RTM_NEWNSID reply to
// an RTM_GETNSID (one per peer in a dump), its RTM_NEWNSID and RTM_DELNSID
// events, and ip's own requests. ip renders them in print_nsid
// (ip/ipnetns.c), for `ip netns list-id` and `ip netns monitor`.

const (
	// RtGenMsgSizeCst is the fixed header of an RTM_*NSID message: a
	// one-byte `struct rtgenmsg` (rtgen_family), padded to four bytes by
	// NLMSG_ALIGN, as the kernel's nlmsg_put(..., sizeof(*rth), ...) and
	// ip's NLMSG_SPACE(sizeof(*rthdr)) both lay it out.
	RtGenMsgSizeCst = 4

	// NsidNotAssigned is NETNSA_NSID_NOT_ASSIGNED: the peer namespace has no
	// id in this one. print_nsid renders any negative id as "unassigned".
	NsidNotAssigned = -1
)

// ErrNsidMsgSmall is an RTM_*NSID body shorter than its rtgenmsg header.
var ErrNsidMsgSmall = errors.New("data too small for an RTM_*NSID rtgenmsg")

// NsidInfo is a decoded RTM_*NSID body: the rtgenmsg family and the
// NETNSA_* attributes (include/uapi/linux/net_namespace.h).
//
// The kernel's rtnl_net_fill (net/core/net_namespace.c) sends NETNSA_NSID
// always and NETNSA_CURRENT_NSID when the request named a target namespace.
// NETNSA_PID, NETNSA_FD and NETNSA_TARGET_NSID are request attributes: ip
// sends them, the kernel never does. Each is decoded only at exactly four
// bytes, the width of rtnl_net_policy's NLA_S32 and NLA_U32; any other width
// leaves its Has flag false.
type NsidInfo struct {
	Family uint8

	NSID    int32 // NETNSA_NSID; NsidNotAssigned or another negative is unassigned
	HasNSID bool

	CurrentNSID    int32 // NETNSA_CURRENT_NSID
	HasCurrentNSID bool

	PID    uint32 // NETNSA_PID, a request attribute
	HasPID bool

	FD    uint32 // NETNSA_FD, a request attribute
	HasFD bool

	TargetNSID    int32 // NETNSA_TARGET_NSID, a request attribute
	HasTargetNSID bool
}

// ParseNsid decodes an RTM_NEWNSID, RTM_DELNSID or RTM_GETNSID body: the
// rtgenmsg header then the NETNSA_* attributes. When an attribute repeats,
// the first wins, as in ip's parse_rtattr (lib/libnetlink.c).
func ParseNsid(body []byte) (NsidInfo, error) {
	var ni NsidInfo
	if len(body) < RtGenMsgSizeCst {
		return ni, ErrNsidMsgSmall
	}
	ni.Family = body[0]

	err := WalkRTAttrs(body[RtGenMsgSizeCst:], func(atype uint16, val []byte) {
		if len(val) != 4 {
			return
		}
		v := binary.LittleEndian.Uint32(val)
		switch atype {
		case unix.NETNSA_NSID:
			if !ni.HasNSID {
				ni.NSID, ni.HasNSID = int32(v), true
			}
		case unix.NETNSA_CURRENT_NSID:
			if !ni.HasCurrentNSID {
				ni.CurrentNSID, ni.HasCurrentNSID = int32(v), true
			}
		case unix.NETNSA_PID:
			if !ni.HasPID {
				ni.PID, ni.HasPID = v, true
			}
		case unix.NETNSA_FD:
			if !ni.HasFD {
				ni.FD, ni.HasFD = v, true
			}
		case unix.NETNSA_TARGET_NSID:
			if !ni.HasTargetNSID {
				ni.TargetNSID, ni.HasTargetNSID = int32(v), true
			}
		}
	})
	if err != nil {
		return ni, err
	}
	return ni, nil
}
