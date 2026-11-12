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
	tdXfrmPolicyGet    = tdXfrm + "/policy_get/ip.pcap"
	tdXfrmPolicyIndex  = tdXfrm + "/policy_get_index/ip.pcap"
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

// TestXfrmNormalize zeroes an SA's clocks and nothing else.
//
// go test ./pkg/nlparity/ -run TestXfrmNormalize
func TestXfrmNormalize(t *testing.T) {
	sa := make([]byte, xtcpnl.XfrmUsersaInfoSizeCst)
	for i := range sa {
		sa[i] = byte(i)
	}
	lastUsed := append(append([]byte(nil), sa...), 12, 0, byte(xtcpnl.XfrmAttrLastUsed), 0, 1, 2, 3, 4, 5, 6, 7, 8)
	pol := sa[:xtcpnl.XfrmUserpolicyInfoSizeCst]
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
		{"positive: NEWPOLICY's add_time and use_time", xtcpnl.XfrmMsgNewPolicy, pol, [][2]int{{136, 152}}},
		{"positive: UPDPOLICY's", xtcpnl.XfrmMsgUpdPolicy, pol, [][2]int{{136, 152}}},
		{"negative: GETPOLICY's userpolicy_id is no policy", xtcpnl.XfrmMsgGetPolicy, sa[:64], nil},
		{"boundary: a NEWPOLICY short of its struct is left alone", xtcpnl.XfrmMsgNewPolicy, pol[:167], nil},
		{"corner: NEWSA's offsets are not a policy's", xtcpnl.XfrmMsgNewSA, pol, nil},
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
	t.Run("corner: the captures' policy 1 differs only in its clocks", func(t *testing.T) {
		a, b := xfrmMsgs(t, tdXfrmPolicyGet), xfrmMsgs(t, tdXfrmPolicyIndex)
		if string(a[1].Body) == string(b[1].Body) {
			t.Fatal("the two captures' policies are the same bytes; the test proves nothing")
		}
		if string(NormalizeMsg(a[1]).Body) != string(NormalizeMsg(b[1]).Body) {
			t.Error("normalized, the two policies still differ")
		}
	})
}
