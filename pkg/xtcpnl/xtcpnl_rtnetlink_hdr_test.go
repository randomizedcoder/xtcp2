package xtcpnl

import (
	"encoding/binary"
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

// ---- WalkNlMsgsHdr / DumpRtnetlinkMsgs tests ----------------------------------
//
// The header-keeping walker `ip addr save` needs: the same rules as
// WalkNlMsgs, with each reply's whole nlmsghdr delivered.

// filteredFlags is NLM_F_MULTI|NLM_F_DUMP_FILTERED, the flags the kernel set
// on save_dev's replies.
const filteredFlags = uint16(unix.NLM_F_MULTI | unix.NLM_F_DUMP_FILTERED)

// withPid sets a message's nlmsg_pid.
func withPid(m []byte, pid uint32) []byte {
	out := append([]byte(nil), m...)
	binary.LittleEndian.PutUint32(out[12:16], pid)
	return out
}

// hdrDelivered records what a header callback saw.
type hdrDelivered struct {
	hdr  NlMsgHdr
	body string
}

func hdrCollector(sink *[]hdrDelivered) func(NlMsgHdr, []byte) error {
	return func(h NlMsgHdr, body []byte) error {
		*sink = append(*sink, hdrDelivered{h, string(body)})
		return nil
	}
}

// go test ./pkg/xtcpnl/ -run TestWalkNlMsgsHdr
func TestWalkNlMsgsHdr(t *testing.T) {
	addrA := withPid(nlmsg(uint16(unix.RTM_NEWADDR), filteredFlags, testSeq, []byte("addr-A-13byte")), 4242)
	addrIntr := nlmsg(uint16(unix.RTM_NEWADDR), uint16(unix.NLM_F_MULTI|unix.NLM_F_DUMP_INTR), testSeq, []byte("intr"))
	staleAddr := nlmsg(uint16(unix.RTM_NEWADDR), filteredFlags, testSeq+1, []byte("stale"))
	hdrA := NlMsgHdr{Len: uint32(len(addrA)), Type: uint16(unix.RTM_NEWADDR), Flags: filteredFlags, Seq: testSeq, Pid: 4242}
	hdrB := NlMsgHdr{Len: uint32(len(linkA)), Type: uint16(unix.RTM_NEWLINK), Flags: uint16(unix.NLM_F_MULTI), Seq: testSeq}

	tests := []struct {
		description string
		data        []byte
		wantDone    bool
		wantErr     error
		expected    []hdrDelivered
	}{
		{description: "positive: the header is passed as received, flags 0x22, seq and pid intact",
			data: stream(addrA, doneMsg), wantDone: true,
			expected: []hdrDelivered{{hdrA, "addr-A-13byte"}}},
		{description: "positive: two replies, the first padded, each with its own header",
			data: stream(addrA, linkA, doneMsg), wantDone: true,
			expected: []hdrDelivered{{hdrA, "addr-A-13byte"}, {hdrB, "link-A-body-16bt"}}},
		{description: "negative: another seq is skipped",
			data: stream(staleAddr, doneMsg), wantDone: true},
		{description: "corner: DUMP_INTR is ErrDumpInterrupted after DONE, nothing delivered after it",
			data: stream(addrA, addrIntr, linkA, doneMsg), wantDone: true, wantErr: ErrDumpInterrupted,
			expected: []hdrDelivered{{hdrA, "addr-A-13byte"}}},
		{description: "boundary: a datagram without DONE is not done",
			data: stream(addrA), expected: []hdrDelivered{{hdrA, "addr-A-13byte"}}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got []hdrDelivered
			done, err := WalkNlMsgsHdr(tc.data, testSeq, hdrCollector(&got))
			if done != tc.wantDone {
				t.Errorf("done = %v, want %v", done, tc.wantDone)
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
			if len(got) != len(tc.expected) {
				t.Fatalf("delivered %+v, want %+v", got, tc.expected)
			}
			for i := range got {
				if got[i] != tc.expected[i] {
					t.Errorf("delivered[%d] = %+v, want %+v", i, got[i], tc.expected[i])
				}
			}
		})
	}

	t.Run("positive: WalkNlMsgs over the same datagram delivers the same types and bodies", func(t *testing.T) {
		data := stream(addrA, linkA, doneMsg)
		var typed []delivered
		var hdrs []hdrDelivered
		doneT, errT := WalkNlMsgs(data, testSeq, collector(&typed, nil))
		doneH, errH := WalkNlMsgsHdr(data, testSeq, hdrCollector(&hdrs))
		if doneT != doneH || !errors.Is(errT, errH) || len(typed) != len(hdrs) {
			t.Fatalf("WalkNlMsgs (%v, %v, %v) and WalkNlMsgsHdr (%v, %v, %v) differ",
				doneT, errT, typed, doneH, errH, hdrs)
		}
		for i := range typed {
			if typed[i] != (delivered{hdrs[i].hdr.Type, hdrs[i].body}) {
				t.Errorf("[%d]: %+v vs %+v", i, typed[i], hdrs[i])
			}
		}
	})
}

// go test ./pkg/xtcpnl/ -run TestDumpRtnetlinkMsgsSocketpair
func TestDumpRtnetlinkMsgsSocketpair(t *testing.T) {
	addrA := nlmsg(uint16(unix.RTM_NEWADDR), filteredFlags, testSeq, []byte("addr-A-13byte"))
	request := BuildDumpLinkRequest(testSeq)

	tests := []struct {
		description string
		replies     [][]byte
		wantErr     error
		expected    []hdrDelivered
	}{
		{description: "positive: replies over two datagrams keep their headers",
			replies: [][]byte{stream(addrA), stream(linkA, doneMsg)},
			expected: []hdrDelivered{
				{NlMsgHdr{Len: uint32(len(addrA)), Type: uint16(unix.RTM_NEWADDR), Flags: filteredFlags, Seq: testSeq}, "addr-A-13byte"},
				{NlMsgHdr{Len: uint32(len(linkA)), Type: uint16(unix.RTM_NEWLINK), Flags: uint16(unix.NLM_F_MULTI), Seq: testSeq}, "link-A-body-16bt"},
			}},
		{description: "boundary: DONE alone delivers nothing",
			replies: [][]byte{stream(doneMsg)}},
		{description: "negative: an errno reply is returned",
			replies: [][]byte{stream(enoentMsg)}, wantErr: unix.ENOENT},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			fds := seqpacketPair(t)
			go func() {
				rb := make([]byte, 256)
				if _, _, err := unix.Recvfrom(fds[1], rb, 0); err != nil {
					return
				}
				for _, d := range tc.replies {
					if _, err := unix.Write(fds[1], d); err != nil {
						return
					}
				}
			}()
			var got []hdrDelivered
			err := DumpRtnetlinkMsgs(fds[0], request, nil, hdrCollector(&got))
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
			if len(got) != len(tc.expected) {
				t.Fatalf("delivered %+v, want %+v", got, tc.expected)
			}
			for i := range got {
				if got[i] != tc.expected[i] {
					t.Errorf("delivered[%d] = %+v, want %+v", i, got[i], tc.expected[i])
				}
			}
		})
	}
}
