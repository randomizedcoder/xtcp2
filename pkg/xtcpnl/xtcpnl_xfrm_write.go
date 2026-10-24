package xtcpnl

import (
	"errors"

	"golang.org/x/sys/unix"
)

// The XFRM write gate. Like rtnetlink's, it admits by type, but by XFRM's
// own: XFRM_MSG_NEWSA is 16, RTM_NEWLINK's number, and neither gate may be
// asked about the other's types.

// ErrXfrmWrite is a type BuildXfrmWriteRequest refuses: not one of the XFRM
// writes IsAdmittedXfrmWrite admits.
var ErrXfrmWrite = errors.New("xtcpnl: XFRM writes are built by BuildXfrmWriteRequest, and only those IsAdmittedXfrmWrite admits")

// IsAdmittedXfrmWrite reports whether msgType is an XFRM write goip may
// build: the SA writes `ip xfrm state` sends, NEWSA (`add`), UPDSA
// (`update`), ALLOCSPI (`allocspi`), DELSA (`delete` and `deleteall`) and
// FLUSHSA (`flush`) (ip/xfrm_state.c), and the policy writes `ip xfrm
// policy` sends, NEWPOLICY (`add`), UPDPOLICY (`update`), DELPOLICY
// (`delete` and `deleteall`), FLUSHPOLICY (`flush`), NEWSPDINFO (`set`)
// and SETDEFAULT (`setdefault`) (ip/xfrm_policy.c).
func IsAdmittedXfrmWrite(msgType uint16) bool {
	switch msgType {
	case XfrmMsgNewSA, XfrmMsgUpdSA, XfrmMsgAllocSPI, XfrmMsgDelSA, XfrmMsgFlushSA,
		XfrmMsgNewPolicy, XfrmMsgUpdPolicy, XfrmMsgDelPolicy, XfrmMsgFlushPolicy, XfrmMsgNewSPDInfo,
		XfrmMsgSetDefault:
		return true
	}
	return false
}

// BuildXfrmWriteRequest lays out an XFRM write as BuildXfrmRequest lays out
// a read: flags plus NLM_F_REQUEST, the type's fixed header, which must be
// its struct, then attrs after the header's padding. A type
// IsAdmittedXfrmWrite does not admit is refused with ErrXfrmWrite.
func BuildXfrmWriteRequest(msgType, flags uint16, seq uint32, hdr, attrs []byte) ([]byte, error) {
	if !IsAdmittedXfrmWrite(msgType) {
		return nil, ErrXfrmWrite
	}
	if len(hdr) != xfrmHdrSizes[msgType] {
		return nil, ErrBadFamilyHdr
	}
	if len(attrs) != 0 && len(hdr)%4 != 0 {
		hdr = append(append([]byte(nil), hdr...), make([]byte, 4-len(hdr)%4)...)
	}
	return layoutRequest(msgType, flags|uint16(unix.NLM_F_REQUEST), seq, hdr, attrs), nil
}
