package xtcpnl

import (
	"encoding/binary"
	"errors"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// ---- DoneErr / DumpRtnetlinkMsgsDoneErr tests ---------------------------------
//
// rtnl_dump_done's rule, which goip opts into: an NLMSG_DONE carrying a
// negative errno ends the dump with that error.

// doneBody is an NLMSG_DONE body holding v, and an extended ack's message
// after it when msg is set, as route_flush_no_table's capture holds one.
func doneBody(v int32, msg string) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(v))
	if msg != "" {
		b = append(b, []byte(msg)...)
	}
	return b
}

// go test ./pkg/xtcpnl/ -run TestDoneErr
func TestDoneErr(t *testing.T) {
	tests := []struct {
		description string
		body        []byte
		expected    error
	}{
		{"positive: zero is the ordinary end", doneBody(0, ""), nil},
		{"positive: a positive count is the ordinary end", doneBody(5, ""), nil},
		{"negative: -ENOENT is ENOENT", doneBody(-int32(syscall.ENOENT), ""), unix.ENOENT},
		{"negative: an extended ack after the errno does not hide it", doneBody(-int32(syscall.ENOENT), "ipv4: FIB table does not exist"), unix.ENOENT},
		{"boundary: an empty body is the end", nil, nil},
		{"corner: a body shorter than the int is the end", []byte{0xfe, 0xff}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			err := DoneErr(tc.body)
			if tc.expected == nil && err != nil || tc.expected != nil && !errors.Is(err, tc.expected) {
				t.Errorf("DoneErr = %v, want %v", err, tc.expected)
			}
		})
	}
}

// go test ./pkg/xtcpnl/ -run TestWalkNlMsgsDoneErr
func TestWalkNlMsgsDoneErr(t *testing.T) {
	enoentDone := nlmsg(uint16(unix.NLMSG_DONE), uint16(unix.NLM_F_MULTI), testSeq, doneBody(-int32(syscall.ENOENT), ""))
	staleENOENT := nlmsg(uint16(unix.NLMSG_DONE), uint16(unix.NLM_F_MULTI), testSeq-1, doneBody(-int32(syscall.ENOENT), ""))
	tests := []struct {
		description string
		data        []byte
		doneErr     bool
		wantDone    bool
		wantErr     error
		delivered   int
	}{
		{"positive: an errno DONE is the error when asked", stream(linkA, enoentDone), true, true, unix.ENOENT, 1},
		{"positive: and the end of an empty dump when not, as before", stream(linkA, enoentDone), false, true, nil, 1},
		{"positive: a zero DONE ends the dump either way", stream(linkA, doneMsg), true, true, nil, 1},
		{"corner: an interrupted dump is ErrDumpInterrupted, not the errno", stream(linkIntr, enoentDone), true, true, ErrDumpInterrupted, 0},
		{"corner: a stale errno DONE is skipped by its seq", stream(staleENOENT, linkA, doneMsg), true, true, nil, 1},
		{"boundary: no DONE yet continues", stream(linkA), true, false, nil, 1},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got []hdrDelivered
			done, err := walkNlMsgsHdr(tc.data, testSeq, hdrCollector(&got), tc.doneErr)
			if done != tc.wantDone || len(got) != tc.delivered ||
				tc.wantErr == nil && err != nil || tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("done %v, err %v, %d delivered; want %v, %v, %d", done, err, len(got), tc.wantDone, tc.wantErr, tc.delivered)
			}
		})
	}
}

// go test ./pkg/xtcpnl/ -run TestDumpRtnetlinkMsgsDoneErrSocketpair
func TestDumpRtnetlinkMsgsDoneErrSocketpair(t *testing.T) {
	enoentDone := nlmsg(uint16(unix.NLMSG_DONE), uint16(unix.NLM_F_MULTI), testSeq, doneBody(-int32(syscall.ENOENT), ""))
	request := BuildDumpLinkRequest(testSeq)
	tests := []struct {
		description string
		strict      bool
		replies     [][]byte
		wantErr     error
	}{
		{"positive: the errno DONE fails the strict dump", true, [][]byte{stream(linkA), stream(enoentDone)}, unix.ENOENT},
		{"positive: DumpRtnetlinkMsgs still reads it as the end", false, [][]byte{stream(linkA), stream(enoentDone)}, nil},
		{"boundary: a zero DONE passes the strict dump", true, [][]byte{stream(linkA, doneMsg)}, nil},
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
			dump := DumpRtnetlinkMsgs
			if tc.strict {
				dump = DumpRtnetlinkMsgsDoneErr
			}
			err := dump(fds[0], request, nil, hdrCollector(&got))
			if tc.wantErr == nil && err != nil || tc.wantErr != nil && !errors.Is(err, tc.wantErr) || len(got) != 1 {
				t.Errorf("err %v, %d delivered; want %v, 1", err, len(got), tc.wantErr)
			}
		})
	}
}
