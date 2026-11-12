//go:build goip_write

package xtcpnl

import "golang.org/x/sys/unix"

// BuildWriteRequest is BuildRequest for the write path: the same layout and
// the same family-header check, behind a gate that admits only
// IsAdmittedWriteType. Anything else returns ErrNotAnAdmittedWrite, GETs
// included, because they belong on BuildRequest.
//
// The flags pass through verbatim, plus NLM_F_REQUEST. The gate is on the type
// for the reason BuildRequest's is: the kernel aliases NLM_F_CREATE|NLM_F_EXCL
// (0x600) with NLM_F_ATOMIC|NLM_F_MATCH, so the flags cannot say whether a
// message writes. NLM_F_ACK is the caller's to set, as iproute2's rtnl_talk
// sets it (lib/libnetlink.c), so a request matches the one `ip` sends.
//
// Building is all this does. goip writes the bytes to a file for comparison
// and never sends them (link-write-path.md, decision 5).
func BuildWriteRequest(msgType, flags uint16, seq uint32, familyHdr, attrs []byte) ([]byte, error) {
	if !IsAdmittedWriteType(msgType) {
		return nil, ErrNotAnAdmittedWrite
	}
	if len(familyHdr) != FamilyHdrLen(msgType) {
		return nil, ErrBadFamilyHdr
	}
	return layoutRequest(msgType, flags|uint16(unix.NLM_F_REQUEST), seq, familyHdr, attrs), nil
}
