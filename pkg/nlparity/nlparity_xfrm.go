package nlparity

import (
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// NETLINK_XFRM, `ip xfrm`'s protocol. Its types are fixed, XFRM_MSG_*, but
// start at 16 as rtnetlink's do, so an XFRM message is told apart by its
// protocol before anything reads its type: NEWSA is not RTM_NEWLINK, and
// XFRM's fixed headers and attributes are not rtnetlink's. Its family header
// also depends on NLM_F_DUMP, which a GETSA or GETPOLICY dump sends with no
// header at all (xtcpnl.XfrmFamilyHdrLen).

// xfrmCurlftTimesOffCst is add_time in an xfrm_usersa_info, its curlft's
// third u64, with use_time after it: the SA's clocks, set when each side's
// setup added it, whose seconds two runs need not share.
//
// xfrmPolicyTimesOffCst is the same pair in an xfrm_userpolicy_info, after
// its selector, lft and curlft's bytes and packets: the policy's clocks.
const (
	xfrmCurlftTimesOffCst = 176
	xfrmCurlftTimesLenCst = 16
	xfrmPolicyTimesOffCst = 136
)

// IsXfrm reports whether m is an XFRM message, as opposed to one of the
// NLMSG_* control messages below NLMSG_MIN_TYPE.
func (m Msg) IsXfrm() bool {
	return m.Proto == unix.NETLINK_XFRM && m.Hdr.Type >= unix.NLMSG_MIN_TYPE
}

// xfrmRequestRole is requestRole for an XFRM request: a read's dump or get,
// told apart by NLM_F_DUMP, or a do.
func xfrmRequestRole(m Msg) string {
	if !xtcpnl.IsXfrmReadType(m.Hdr.Type) {
		return roleDoCst
	}
	if m.Hdr.Flags&unix.NLM_F_DUMP == unix.NLM_F_DUMP {
		return roleDumpCst
	}
	return roleGetCst
}

// normalizeXfrm zeroes an SA's and a policy's clocks in place: an SA or
// policy message's add_time and use_time, and XFRMA_LASTUSED's value, the
// time it was last used. Their byte and packet counts, limits, statistics
// and every other field stay compared.
func normalizeXfrm(m Msg, body []byte) {
	switch m.Hdr.Type {
	case xtcpnl.XfrmMsgNewSA, xtcpnl.XfrmMsgUpdSA:
		if len(body) >= xtcpnl.XfrmUsersaInfoSizeCst {
			zeroRange(body, xfrmCurlftTimesOffCst, xfrmCurlftTimesLenCst)
		}
	case xtcpnl.XfrmMsgNewPolicy, xtcpnl.XfrmMsgUpdPolicy:
		if len(body) >= xtcpnl.XfrmUserpolicyInfoSizeCst {
			zeroRange(body, xfrmPolicyTimesOffCst, xfrmCurlftTimesLenCst)
		}
	}
	_, attrs, _ := decodeBody(xtcpnl.XfrmFamilyHdrLen(m.Hdr.Type, m.Hdr.Flags), body)
	for _, a := range attrs {
		if a.BareType() == xtcpnl.XfrmAttrLastUsed {
			zeroRange(a.Val, 0, len(a.Val))
		}
	}
}
