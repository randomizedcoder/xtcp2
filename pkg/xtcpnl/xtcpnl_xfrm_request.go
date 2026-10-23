package xtcpnl

import (
	"errors"
	"fmt"

	"golang.org/x/sys/unix"
)

// This file is what a NETLINK_XFRM request needs that the rtnetlink request
// layer does not give: each type's fixed header, which a dump does without,
// the read types goip may send, and their names. XFRM's type numbers start
// at 16 as rtnetlink's do, so none of RTM_*'s helpers can be asked about one.

// ErrNotAnXfrmRead is a type BuildXfrmRequest refuses: not one of the XFRM
// reads IsXfrmReadType admits.
var ErrNotAnXfrmRead = errors.New("xtcpnl: only the XFRM_MSG_GET* reads may be built")

// xfrmHdrSizes is each XFRM type's fixed header as the kernel's
// xfrm_msg_min sizes it (net/xfrm/xfrm_user.c), and the NEW*INFO answers'
// u32: the struct's size, before alignment. REPORT and MIGRATE_STATE, which
// `ip` never sends and the corpus never holds, are not modeled.
var xfrmHdrSizes = map[uint16]int{
	XfrmMsgNewSA:       XfrmUsersaInfoSizeCst,
	XfrmMsgUpdSA:       XfrmUsersaInfoSizeCst,
	XfrmMsgDelSA:       XfrmUsersaIDSizeCst,
	XfrmMsgGetSA:       XfrmUsersaIDSizeCst,
	XfrmMsgNewPolicy:   XfrmUserpolicyInfoSizeCst,
	XfrmMsgUpdPolicy:   XfrmUserpolicyInfoSizeCst,
	XfrmMsgDelPolicy:   XfrmUserpolicyIDSizeCst,
	XfrmMsgGetPolicy:   XfrmUserpolicyIDSizeCst,
	XfrmMsgMigrate:     XfrmUserpolicyIDSizeCst,
	XfrmMsgAllocSPI:    XfrmUserspiInfoSizeCst,
	XfrmMsgAcquire:     XfrmUserAcquireSizeCst,
	XfrmMsgExpire:      XfrmUserExpireSizeCst,
	XfrmMsgPolExpire:   XfrmUserPolexpireSizeCst,
	XfrmMsgFlushSA:     XfrmUsersaFlushSizeCst,
	XfrmMsgFlushPolicy: 0,
	XfrmMsgNewAE:       XfrmAeventIDSizeCst,
	XfrmMsgGetAE:       XfrmAeventIDSizeCst,
	XfrmMsgNewSADInfo:  XfrmInfoFlagsSizeCst,
	XfrmMsgGetSADInfo:  XfrmInfoFlagsSizeCst,
	XfrmMsgNewSPDInfo:  XfrmInfoFlagsSizeCst,
	XfrmMsgGetSPDInfo:  XfrmInfoFlagsSizeCst,
	XfrmMsgMapping:     XfrmUserMappingSizeCst,
	XfrmMsgSetDefault:  XfrmUserpolicyDefaultSizeCst,
	XfrmMsgGetDefault:  XfrmUserpolicyDefaultSizeCst,
}

// isXfrmDump reports whether a request is the dump of a type the kernel
// dumps, GETSA or GETPOLICY with NLM_F_DUMP (xfrm_user_rcv_msg). Its
// attributes start right after the nlmsghdr: xfrm_dump_sa parses them at
// offset 0, and `ip` sends no header.
func isXfrmDump(msgType, flags uint16) bool {
	return (msgType == XfrmMsgGetSA || msgType == XfrmMsgGetPolicy) &&
		flags&unix.NLM_F_DUMP == unix.NLM_F_DUMP
}

// XfrmFamilyHdrLen is where an XFRM message's attributes start after its
// nlmsghdr: its type's fixed header aligned to four bytes, as nlmsg_attrdata
// places them, 0 for a GETSA or GETPOLICY dump, or -1 for a type not
// modeled. flags matter only for the two dumps.
func XfrmFamilyHdrLen(msgType, flags uint16) int {
	if isXfrmDump(msgType, flags) {
		return 0
	}
	n, ok := xfrmHdrSizes[msgType]
	if !ok {
		return -1
	}
	return (n + 3) &^ 3
}

// IsXfrmReadType reports whether msgType is an XFRM read, which cannot
// change the SAD or SPD: GETSA, GETPOLICY, GETAE, GETSADINFO, GETSPDINFO and
// GETDEFAULT.
func IsXfrmReadType(msgType uint16) bool {
	switch msgType {
	case XfrmMsgGetSA, XfrmMsgGetPolicy, XfrmMsgGetAE, XfrmMsgGetSADInfo, XfrmMsgGetSPDInfo, XfrmMsgGetDefault:
		return true
	}
	return false
}

// BuildXfrmRequest lays out an XFRM read request: flags plus NLM_F_REQUEST,
// the fixed header, which must be its type's struct (none for a dump), then
// attrs, after the header's alignment padding when there are any. A type
// IsXfrmReadType does not admit is refused with ErrNotAnXfrmRead.
func BuildXfrmRequest(msgType, flags uint16, seq uint32, hdr, attrs []byte) ([]byte, error) {
	if !IsXfrmReadType(msgType) {
		return nil, ErrNotAnXfrmRead
	}
	want := xfrmHdrSizes[msgType]
	if isXfrmDump(msgType, flags) {
		want = 0
	}
	if len(hdr) != want {
		return nil, ErrBadFamilyHdr
	}
	if len(attrs) != 0 && len(hdr)%4 != 0 {
		hdr = append(append([]byte(nil), hdr...), make([]byte, 4-len(hdr)%4)...)
	}
	return layoutRequest(msgType, flags|uint16(unix.NLM_F_REQUEST), seq, hdr, attrs), nil
}

// xfrmMsgNames are the XFRM_MSG_* names, indexed from XFRM_MSG_BASE.
var xfrmMsgNames = []string{
	"NEWSA", "DELSA", "GETSA", "NEWPOLICY", "DELPOLICY", "GETPOLICY", "ALLOCSPI", "ACQUIRE", "EXPIRE",
	"UPDPOLICY", "UPDSA", "POLEXPIRE", "FLUSHSA", "FLUSHPOLICY", "NEWAE", "GETAE", "REPORT", "MIGRATE",
	"NEWSADINFO", "GETSADINFO", "NEWSPDINFO", "GETSPDINFO", "MAPPING", "SETDEFAULT", "GETDEFAULT",
	"MIGRATE_STATE",
}

// XfrmMsgTypeName names an XFRM message type, XFRM_MSG_NEWSA and the like,
// or type(N) for one outside the enum.
func XfrmMsgTypeName(msgType uint16) string {
	if i := int(msgType) - int(XfrmMsgNewSA); i >= 0 && i < len(xfrmMsgNames) {
		return "XFRM_MSG_" + xfrmMsgNames[i]
	}
	return fmt.Sprintf("type(%d)", msgType)
}
