//go:build goip_write

package nlparity

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// TestXfrmMsg is what an XFRM message is to the comparator: its name, its
// attributes' offset, its role and whether it writes, each apart from the
// rtnetlink type with its number.
//
// go test ./pkg/nlparity/ -run TestXfrmMsg
func TestXfrmMsg(t *testing.T) {
	const dump = uint16(unix.NLM_F_DUMP | unix.NLM_F_REQUEST)
	msg := func(typ, flags uint16, body []byte) Msg {
		return Msg{Hdr: xtcpnl.NlMsgHdr{Type: typ, Flags: flags}, Body: body, Proto: unix.NETLINK_XFRM}
	}
	filter := []byte{40, 0, 26, 0}
	tests := []struct {
		description string
		m           Msg
		xfrm        bool
		name, role  string
		hdrLen      int
		write       bool
	}{
		{"positive: a GETSA dump, its attributes at 0", msg(xtcpnl.XfrmMsgGetSA, dump, filter), true,
			"XFRM_MSG_GETSA", "dump", 0, false},
		{"positive: a GETSA get, its xfrm_usersa_id first", msg(xtcpnl.XfrmMsgGetSA, unix.NLM_F_REQUEST,
			make([]byte, 24)), true, "XFRM_MSG_GETSA", "get", 24, false},
		{"positive: GETSADINFO, a get", msg(xtcpnl.XfrmMsgGetSADInfo, unix.NLM_F_REQUEST, make([]byte, 4)), true,
			"XFRM_MSG_GETSADINFO", "get", 4, false},
		{"corner: NEWSA, RTM_NEWLINK's number, is XFRM's admitted write", msg(xtcpnl.XfrmMsgNewSA,
			unix.NLM_F_REQUEST|unix.NLM_F_CREATE|unix.NLM_F_EXCL, make([]byte, 224)), true, "XFRM_MSG_NEWSA", "do",
			224, true},
		{"corner: NLMSG_DONE on XFRM is the control message", msg(unix.NLMSG_DONE, 0, make([]byte, 4)), false,
			"NLMSG_DONE", "get", xtcpnl.FamilyHdrLen(unix.NLMSG_DONE), false},
		{"negative: the same NEWSA on the route socket is RTM_NEWLINK", Msg{Hdr: xtcpnl.NlMsgHdr{Type: 16,
			Flags: unix.NLM_F_REQUEST}, Body: make([]byte, 16), Proto: unix.NETLINK_ROUTE}, false, "RTM_NEWLINK",
			"write", 16, true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.m.IsXfrm(); got != tc.xfrm {
				t.Errorf("IsXfrm %v, want %v", got, tc.xfrm)
			}
			if got := MsgName(tc.m); got != tc.name {
				t.Errorf("MsgName %q, want %q", got, tc.name)
			}
			if got := requestRole(tc.m); got != tc.role {
				t.Errorf("requestRole %q, want %q", got, tc.role)
			}
			if hdr, _, _ := DecodeMsg(tc.m); len(hdr) != tc.hdrLen {
				t.Errorf("header %d bytes, want %d", len(hdr), tc.hdrLen)
			}
			if got := IsWrite(tc.m); got != tc.write {
				t.Errorf("IsWrite %v, want %v", got, tc.write)
			}
		})
	}
}
