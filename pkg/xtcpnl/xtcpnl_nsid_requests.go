package xtcpnl

import (
	"golang.org/x/sys/unix"
)

// The RTM_*NSID requests `ip netns list-id` and `ip netns set` send
// (ip/ipnetns.c, lib/namespace.c). Each is the four-byte rtgenmsg,
// rtgen_family AF_UNSPEC, then NETNSA_* attributes, sent at nlmsg_len with
// no oversend: rtnl_talk and the dump's send() both stop there.

// nsidReqBufCst holds the most attributes one of these requests carries:
// two four-byte NETNSA_* values, eight bytes each with their headers.
const nsidReqBufCst = 16

// BuildGetNsidByFDRequest is netns_id_from_name's RTM_GETNSID
// (lib/namespace.c): NLM_F_REQUEST alone, NETNSA_FD the namespace file's
// descriptor. The kernel answers with one RTM_NEWNSID, the namespace's nsid
// here or NETNSA_NSID_NOT_ASSIGNED.
func BuildGetNsidByFDRequest(fd, seq uint32) ([]byte, error) {
	var raw [nsidReqBufCst]byte
	ab := NewAttrBuilder(raw[:])
	if err := ab.PutU32(unix.NETNSA_FD, fd); err != nil {
		return nil, err
	}
	return BuildRequest(uint16(unix.RTM_GETNSID), 0, seq, make([]byte, RtGenMsgSizeCst), ab.Bytes())
}

// BuildGetNsidRequest is get_netnsid_from_netnsid's RTM_GETNSID, `ip netns
// list-id nsid N`: NLM_F_REQUEST alone, NETNSA_NSID, then NETNSA_TARGET_NSID
// when hasTarget.
func BuildGetNsidRequest(nsid, target int32, hasTarget bool, seq uint32) ([]byte, error) {
	var raw [nsidReqBufCst]byte
	ab := NewAttrBuilder(raw[:])
	if err := ab.PutU32(unix.NETNSA_NSID, uint32(nsid)); err != nil {
		return nil, err
	}
	if hasTarget {
		if err := ab.PutU32(unix.NETNSA_TARGET_NSID, uint32(target)); err != nil {
			return nil, err
		}
	}
	return BuildRequest(uint16(unix.RTM_GETNSID), 0, seq, make([]byte, RtGenMsgSizeCst), ab.Bytes())
}

// BuildDumpNsidRequest is rtnl_nsiddump_req_filter_fn's dump, `ip netns
// list-id`'s: NLM_F_DUMP, and NETNSA_TARGET_NSID when hasTarget
// (netns_filter_req).
func BuildDumpNsidRequest(target int32, hasTarget bool, seq uint32) ([]byte, error) {
	var raw [nsidReqBufCst]byte
	ab := NewAttrBuilder(raw[:])
	if hasTarget {
		if err := ab.PutU32(unix.NETNSA_TARGET_NSID, uint32(target)); err != nil {
			return nil, err
		}
	}
	return BuildRequest(uint16(unix.RTM_GETNSID), uint16(unix.NLM_F_DUMP), seq,
		make([]byte, RtGenMsgSizeCst), ab.Bytes())
}

// BuildSetNsidRequest is set_netns_id_from_name's RTM_NEWNSID, `ip netns set
// NAME NSID`: NLM_F_REQUEST|NLM_F_ACK (rtnl_talk with no answer), NETNSA_FD
// the namespace file's descriptor, then NETNSA_NSID, -1 for `auto`.
func BuildSetNsidRequest(fd uint32, nsid int32, seq uint32) ([]byte, error) {
	var raw [nsidReqBufCst]byte
	ab := NewAttrBuilder(raw[:])
	if err := ab.PutU32(unix.NETNSA_FD, fd); err != nil {
		return nil, err
	}
	if err := ab.PutU32(unix.NETNSA_NSID, uint32(nsid)); err != nil {
		return nil, err
	}
	return BuildWriteRequest(uint16(unix.RTM_NEWNSID), uint16(unix.NLM_F_ACK), seq,
		make([]byte, RtGenMsgSizeCst), ab.Bytes())
}
