package nlparity

import (
	"encoding/binary"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The C3 captures the XFRM tests read: two of `ip xfrm state list` over the
// same two SAs, added at different seconds, one with nokeys.
const (
	tdXfrm             = "../../pkg/xtcpnl/testdata/7_1_4/link-write/xfrm"
	tdXfrmList         = tdXfrm + "/state_list/ip.pcap"
	tdXfrmListNokeys   = tdXfrm + "/state_list_nokeys/ip.pcap"
	xfrmUsersaReqidCst = 208
)

// xfrmMsgs reads a capture's XFRM messages.
func xfrmMsgs(t *testing.T, path string) []Msg {
	t.Helper()
	c, err := ParseCaptureFile(path, unix.NETLINK_XFRM)
	if err != nil {
		t.Fatalf("ParseCaptureFile: %v", err)
	}
	return c.Msgs()
}

// TestXfrmPair compares state_list's capture with state_list_nokeys', the
// same requests and SAs added at other seconds, and with copies of it
// edited the way a different tool would send or the kernel answer them.
//
// go test ./pkg/nlparity/ -run TestXfrmPair
func TestXfrmPair(t *testing.T) {
	ref := Segment(xfrmMsgs(t, tdXfrmList))
	setBody := func(off int, v byte) func(m *Msg) {
		return func(m *Msg) {
			b := append([]byte(nil), m.Body...)
			b[off] = v
			m.Body = b
		}
	}
	type edit struct {
		msg int
		f   func(m *Msg)
	}
	tests := []struct {
		description string
		sub         string
		edits       []edit
		want        []string
	}{
		{description: "positive: the capture against itself", sub: tdXfrmList, want: []string{}},
		{description: "positive: the SAs added at other seconds compare clean, their clocks zeroed",
			sub: tdXfrmListNokeys, want: []string{}},
		{description: "negative: a dump's address filter read from offset 0, its prefix length a finding",
			sub: tdXfrmList, edits: []edit{{msg: 0, f: setBody(4+35, 32)}},
			want: []string{"L2|value|request:XFRM_MSG_GETSA:attr(26):dump"}},
		{description: "negative: a get where ip dumps is its flags, and its body read as a get's", sub: tdXfrmList,
			edits: []edit{{msg: 0, f: func(m *Msg) { m.Hdr.Flags &^= unix.NLM_F_DUMP }}},
			want: []string{"L2|value|request:XFRM_MSG_GETSA:header.flags:dump",
				"L2|value|request:XFRM_MSG_GETSA:familyhdr:dump", "L2|value|request:XFRM_MSG_GETSA:body-remainder:dump",
				"L2|presence|request:XFRM_MSG_GETSA:attr(26):dump"}},
		{description: "negative: an SA's reqid is a family header finding", sub: tdXfrmList,
			edits: []edit{{msg: 1, f: setBody(xfrmUsersaReqidCst, 9)}},
			want:  []string{"L3|value|reply:XFRM_MSG_NEWSA:familyhdr"}},
		{description: "corner: an SA's add_time alone is no finding", sub: tdXfrmList,
			edits: []edit{{msg: 1, f: setBody(xfrmCurlftTimesOffCst, 0x77)}}, want: []string{}},
		{description: "negative: the byte after use_time, the replay window statistic, is", sub: tdXfrmList,
			edits: []edit{{msg: 1, f: setBody(xfrmCurlftTimesOffCst+xfrmCurlftTimesLenCst, 1)}},
			want:  []string{"L3|value|reply:XFRM_MSG_NEWSA:familyhdr"}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			msgs := xfrmMsgs(t, tc.sub)
			for _, e := range tc.edits {
				e.f(&msgs[e.msg])
			}
			got := findingKeys(Diff("xfrm state list", ref, Segment(msgs)))
			if !sameStrings(got, tc.want) {
				t.Errorf("findings\n  got  %v\n  want %v", got, tc.want)
			}
		})
	}
}

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
		{"corner: NEWSA, RTM_NEWLINK's number, is no admitted write", msg(xtcpnl.XfrmMsgNewSA,
			unix.NLM_F_REQUEST|unix.NLM_F_CREATE|unix.NLM_F_EXCL, make([]byte, 224)), true, "XFRM_MSG_NEWSA", "do",
			224, false},
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

// TestXfrmNormalize zeroes an SA's clocks and nothing else.
//
// go test ./pkg/nlparity/ -run TestXfrmNormalize
func TestXfrmNormalize(t *testing.T) {
	sa := make([]byte, xtcpnl.XfrmUsersaInfoSizeCst)
	for i := range sa {
		sa[i] = byte(i)
	}
	lastUsed := append(append([]byte(nil), sa...), 12, 0, byte(xtcpnl.XfrmAttrLastUsed), 0, 1, 2, 3, 4, 5, 6, 7, 8)
	tests := []struct {
		description string
		typ         uint16
		body        []byte
		zeroed      [][2]int // [from, to) ranges zeroed
	}{
		{"positive: NEWSA's add_time and use_time", xtcpnl.XfrmMsgNewSA, sa, [][2]int{{176, 192}}},
		{"positive: UPDSA's", xtcpnl.XfrmMsgUpdSA, sa, [][2]int{{176, 192}}},
		{"positive: XFRMA_LASTUSED's value too", xtcpnl.XfrmMsgNewSA, lastUsed, [][2]int{{176, 192},
			{228, 236}}},
		{"negative: GETSA's usersa_id is no SA", xtcpnl.XfrmMsgGetSA, sa[:24], nil},
		{"boundary: a NEWSA short of its struct is left alone", xtcpnl.XfrmMsgNewSA, sa[:200], nil},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			m := Msg{Hdr: xtcpnl.NlMsgHdr{Type: tc.typ, Seq: 9, Pid: 9}, Body: tc.body, Proto: unix.NETLINK_XFRM}
			got := NormalizeMsg(m).Body
			for i := range got {
				want := tc.body[i]
				for _, r := range tc.zeroed {
					if i >= r[0] && i < r[1] {
						want = 0
					}
				}
				if got[i] != want {
					t.Fatalf("byte %d = %d, want %d", i, got[i], want)
				}
			}
			if binary.LittleEndian.Uint32(tc.body[:4]) != 0x03020100 {
				t.Error("the original body was modified")
			}
		})
	}
	t.Run("corner: the capture's SAs differ only in their clocks", func(t *testing.T) {
		a, b := xfrmMsgs(t, tdXfrmList), xfrmMsgs(t, tdXfrmListNokeys)
		if string(a[1].Body) == string(b[1].Body) {
			t.Fatal("the two captures' first SA are the same bytes; the test proves nothing")
		}
		if string(NormalizeMsg(a[1]).Body) != string(NormalizeMsg(b[1]).Body) {
			t.Error("normalized, the two first SAs still differ")
		}
	})
}
