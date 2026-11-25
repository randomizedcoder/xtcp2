package nlparity

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// le32 is one little-endian u32, and catBytes joins byte slices.
func le32b(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }

func catBytes(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

// monHdr is one nlmsghdr of typ and seq, its length as recorded.
func monHdr(typ uint16, seq, length uint32) []byte {
	h := make([]byte, xtcpnl.NlMsgHdrSizeCst)
	binary.LittleEndian.PutUint32(h, length)
	binary.LittleEndian.PutUint16(h[4:], typ)
	binary.LittleEndian.PutUint32(h[8:], seq)
	return h
}

// TestNotificationIndexes is which of a capture's messages a subscriber
// was sent: requests, dump parts, errors and a get's answer left out.
//
//	go test ./pkg/nlparity/ -run TestNotificationIndexes
func TestNotificationIndexes(t *testing.T) {
	m := func(typ, flags uint16, seq, pid uint32, body ...byte) Msg {
		return Msg{Hdr: xtcpnl.NlMsgHdr{Type: typ, Flags: flags, Seq: seq, Pid: pid}, Body: body}
	}
	errFor := func(typ uint16, seq uint32) Msg {
		return m(unix.NLMSG_ERROR, 0, seq, 100, catBytes(le32b(0), monHdr(typ, seq, xtcpnl.NlMsgHdrSizeCst))...)
	}
	const req = unix.NLM_F_REQUEST
	tests := []struct {
		description string
		msgs        []Msg
		expected    []int
	}{
		{"positive: a notification", []Msg{m(unix.RTM_NEWLINK, 0, 5, 4242)}, []int{0}},
		{"negative: a request", []Msg{m(unix.RTM_NEWLINK, req, 5, 0)}, nil},
		{"corner: a notification with the request bit, as nexthop_notify sends it",
			[]Msg{m(unix.RTM_NEWNEXTHOP, req|unix.NLM_F_CREATE, 5, 4242)}, []int{0}},
		{"negative: a dump part", []Msg{m(unix.RTM_NEWLINK, unix.NLM_F_MULTI, 5, 0)}, nil},
		{"negative: a refusal", []Msg{errFor(unix.RTM_SETLINK, 5)}, nil},
		{"negative: a get's answer", []Msg{m(unix.RTM_GETNSID, req, 7, 0), m(unix.RTM_NEWNSID, 0, 7, 100),
			m(unix.RTM_NEWNSID, 0, 0, 0)}, []int{2}},
		{"corner: a get's answer is the first of its type and seq alone", []Msg{
			m(unix.RTM_GETNSID, req, 7, 0), m(unix.RTM_NEWADDR, 0, 7, 0), m(unix.RTM_NEWNSID, 0, 7, 100),
			m(unix.RTM_NEWNSID, 0, 7, 100)}, []int{1, 3}},
		{"corner: a refused get waits for no answer", []Msg{m(unix.RTM_GETNSID, req, 7, 0),
			errFor(unix.RTM_GETNSID, 7), m(unix.RTM_NEWNSID, 0, 7, 0)}, []int{2}},
		{"boundary: a short error names nothing", []Msg{m(unix.RTM_GETNSID, req, 7, 0),
			m(unix.NLMSG_ERROR, 0, 7, 0, 0, 0, 0, 0), m(unix.RTM_NEWNSID, 0, 7, 0)}, nil},
		{"boundary: none", nil, nil},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := NotificationIndexes(tc.msgs)
			if fmt.Sprint(got) != fmt.Sprint(tc.expected) {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}
