package xtcpnl

import "golang.org/x/sys/unix"

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
