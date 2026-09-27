package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// ---- WalkNlMsgs / DumpRtnetlink tests -----------------------------------------
//
// These exercise the dump-stream walker without a kernel: WalkNlMsgs is pure and
// takes a datagram, and DumpRtnetlink is driven over an AF_UNIX SOCK_SEQPACKET
// socketpair (each Write is one datagram, exactly like a netlink recv), with
// sa == nil so Sendto goes to the connected peer.

const testSeq uint32 = 0x1234

// nlmsg lays out one netlink message: 16-byte nlmsghdr + body, with the length
// set to the UNPADDED size (the kernel pads the stream, not nlmsg_len).
func nlmsg(typ, flags uint16, seq uint32, body []byte) []byte {
	b := make([]byte, NlMsgHdrSizeCst+len(body))
	binary.LittleEndian.PutUint32(b[0:4], uint32(len(b)))
	binary.LittleEndian.PutUint16(b[4:6], typ)
	binary.LittleEndian.PutUint16(b[6:8], flags)
	binary.LittleEndian.PutUint32(b[8:12], seq)
	binary.LittleEndian.PutUint32(b[12:16], 0)
	copy(b[NlMsgHdrSizeCst:], body)
	return b
}

// pad appends the 4-byte alignment padding the kernel inserts between messages.
func pad(msg []byte) []byte {
	return append(msg, make([]byte, FourByteAlignPadding(len(msg)))...)
}

// stream concatenates messages, padding every one except the last (the kernel
// does not pad the final message of a datagram past its nlmsg_len).
func stream(msgs ...[]byte) []byte {
	var out []byte
	for i, m := range msgs {
		if i < len(msgs)-1 {
			out = append(out, pad(m)...)
		} else {
			out = append(out, m...)
		}
	}
	return out
}

// errnoBody is an NLMSG_ERROR body: negative errno + echoed request header.
func errnoBody(errno syscall.Errno) []byte {
	b := make([]byte, 4+NlMsgHdrSizeCst)
	binary.LittleEndian.PutUint32(b[0:4], uint32(-int32(errno)))
	return b
}

var (
	doneMsg   = nlmsg(uint16(unix.NLMSG_DONE), uint16(unix.NLM_F_MULTI), testSeq, make([]byte, 4))
	ackMsg    = nlmsg(uint16(unix.NLMSG_ERROR), 0, testSeq, errnoBody(0))
	enoentMsg = nlmsg(uint16(unix.NLMSG_ERROR), 0, testSeq, errnoBody(syscall.ENOENT))
	noopMsg   = nlmsg(uint16(unix.NLMSG_NOOP), 0, testSeq, nil)
	linkA     = nlmsg(uint16(unix.RTM_NEWLINK), uint16(unix.NLM_F_MULTI), testSeq, []byte("link-A-body-16bt"))
	linkB     = nlmsg(uint16(unix.RTM_NEWLINK), uint16(unix.NLM_F_MULTI), testSeq, []byte("link-B-13byte"))             // 13-byte body -> 3 bytes padding
	linkIntr  = nlmsg(uint16(unix.RTM_NEWLINK), uint16(unix.NLM_F_MULTI|unix.NLM_F_DUMP_INTR), testSeq, []byte("intr")) // dump interrupted
	staleLink = nlmsg(uint16(unix.RTM_NEWLINK), uint16(unix.NLM_F_MULTI), testSeq+1, []byte("stale"))
	staleDone = nlmsg(uint16(unix.NLMSG_DONE), uint16(unix.NLM_F_MULTI), testSeq-1, make([]byte, 4))
)

// delivered records what onMsg saw.
type delivered struct {
	typ  uint16
	body string
}

func collector(sink *[]delivered, fail error) func(uint16, []byte) error {
	return func(mt uint16, body []byte) error {
		*sink = append(*sink, delivered{mt, string(body)})
		return fail
	}
}

// go test ./pkg/xtcpnl/ -run TestWalkNlMsgs
func TestWalkNlMsgs(t *testing.T) {
	errOnMsg := errors.New("onMsg failed")

	tests := []struct {
		description string
		data        []byte
		onMsgErr    error       // returned by onMsg on every call
		wantDone    bool        // expected done
		wantErr     error       // expected errors.Is target (nil = no error)
		wantMsgs    []delivered // expected onMsg deliveries, in order
	}{
		// positive
		{"one RTM_NEWLINK then DONE -> delivered once, done", stream(linkA, doneMsg), nil, true, nil,
			[]delivered{{uint16(unix.RTM_NEWLINK), "link-A-body-16bt"}}},
		{"DONE alone -> done, nothing delivered", stream(doneMsg), nil, true, nil, nil},
		{"zero-errno ACK -> done, nil (not an error)", stream(ackMsg), nil, true, nil, nil},
		{"two messages, second body 13 bytes (padding) -> both delivered at aligned offsets", stream(linkB, linkA, doneMsg), nil, true, nil,
			[]delivered{{uint16(unix.RTM_NEWLINK), "link-B-13byte"}, {uint16(unix.RTM_NEWLINK), "link-A-body-16bt"}}},
		{"datagram without DONE -> not done, no error (dump continues in next recv)", stream(linkA, linkB), nil, false, nil,
			[]delivered{{uint16(unix.RTM_NEWLINK), "link-A-body-16bt"}, {uint16(unix.RTM_NEWLINK), "link-B-13byte"}}},
		{"NOOP is skipped, following message delivered", stream(noopMsg, linkA, doneMsg), nil, true, nil,
			[]delivered{{uint16(unix.RTM_NEWLINK), "link-A-body-16bt"}}},
		{"nil onMsg discards payload messages but still reaches DONE", stream(linkA, doneMsg), nil, true, nil, nil},

		// negative
		{"non-zero errno NLMSG_ERROR -> done, wrapped ENOENT", stream(enoentMsg), nil, true, syscall.ENOENT, nil},
		{"NLMSG_ERROR body shorter than errno -> ErrNetlinkError",
			stream(nlmsg(uint16(unix.NLMSG_ERROR), 0, testSeq, []byte{0xff, 0xff})), nil, true, ErrNetlinkError, nil},
		{"onMsg error is returned immediately, later messages not delivered", stream(linkA, linkB, doneMsg), errOnMsg, false, errOnMsg,
			[]delivered{{uint16(unix.RTM_NEWLINK), "link-A-body-16bt"}}},

		// boundary
		{"empty datagram -> ErrShortRecv", nil, nil, false, ErrShortRecv, nil},
		{"15-byte datagram -> ErrShortRecv", make([]byte, 15), nil, false, ErrShortRecv, nil},
		{"nlmsg_len 15 (< header) -> ErrBadMsgLen", func() []byte {
			m := nlmsg(uint16(unix.RTM_NEWLINK), 0, testSeq, nil)
			binary.LittleEndian.PutUint32(m[0:4], 15)
			return m
		}(), nil, false, ErrBadMsgLen, nil},
		{"nlmsg_len overruns datagram -> ErrBadMsgLen", func() []byte {
			m := nlmsg(uint16(unix.RTM_NEWLINK), 0, testSeq, []byte("abcd"))
			binary.LittleEndian.PutUint32(m[0:4], uint32(len(m)+8))
			return m
		}(), nil, false, ErrBadMsgLen, nil},
		{"trailing remainder shorter than a header is ignored", append(stream(linkA), 1, 2, 3), nil, false, nil,
			[]delivered{{uint16(unix.RTM_NEWLINK), "link-A-body-16bt"}}},
		{"bare header, zero-length body, then DONE", stream(nlmsg(uint16(unix.RTM_NEWLINK), 0, testSeq, nil), doneMsg), nil, true, nil,
			[]delivered{{uint16(unix.RTM_NEWLINK), ""}}},

		// corner — sequence filtering
		{"stale-seq message skipped, matching message delivered", stream(staleLink, linkA, doneMsg), nil, true, nil,
			[]delivered{{uint16(unix.RTM_NEWLINK), "link-A-body-16bt"}}},
		{"stale-seq DONE does not end the dump", stream(staleDone, linkA), nil, false, nil,
			[]delivered{{uint16(unix.RTM_NEWLINK), "link-A-body-16bt"}}},
		{"stale-seq NLMSG_ERROR is ignored too", stream(nlmsg(uint16(unix.NLMSG_ERROR), 0, testSeq+7, errnoBody(syscall.EPERM)), doneMsg), nil, true, nil, nil},

		// corner — NLM_F_DUMP_INTR
		{"DUMP_INTR message -> ErrDumpInterrupted, not done, nothing after it delivered", stream(linkA, linkIntr, linkB), nil, false, ErrDumpInterrupted,
			[]delivered{{uint16(unix.RTM_NEWLINK), "link-A-body-16bt"}}},
		{"DUMP_INTR then DONE in same datagram -> done AND ErrDumpInterrupted", stream(linkIntr, doneMsg), nil, true, ErrDumpInterrupted, nil},
		{"DUMP_INTR flag on the DONE itself -> done AND ErrDumpInterrupted",
			stream(linkA, nlmsg(uint16(unix.NLMSG_DONE), uint16(unix.NLM_F_MULTI|unix.NLM_F_DUMP_INTR), testSeq, make([]byte, 4))), nil, true, ErrDumpInterrupted,
			[]delivered{{uint16(unix.RTM_NEWLINK), "link-A-body-16bt"}}},
		{"DUMP_INTR on a stale-seq message is ignored (belongs to another dump)",
			stream(nlmsg(uint16(unix.RTM_NEWLINK), uint16(unix.NLM_F_DUMP_INTR), testSeq+1, []byte("x")), linkA, doneMsg), nil, true, nil,
			[]delivered{{uint16(unix.RTM_NEWLINK), "link-A-body-16bt"}}},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got []delivered
			var onMsg func(uint16, []byte) error
			if tc.description != "nil onMsg discards payload messages but still reaches DONE" {
				onMsg = collector(&got, tc.onMsgErr)
			}
			done, err := WalkNlMsgs(tc.data, testSeq, onMsg)
			if done != tc.wantDone {
				t.Errorf("done = %v, want %v", done, tc.wantDone)
			}
			if tc.wantErr == nil && err != nil {
				t.Errorf("err = %v, want nil", err)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want errors.Is(%v)", err, tc.wantErr)
			}
			if len(got) != len(tc.wantMsgs) {
				t.Fatalf("delivered %d messages %v, want %d %v", len(got), got, len(tc.wantMsgs), tc.wantMsgs)
			}
			for i := range got {
				if got[i] != tc.wantMsgs[i] {
					t.Errorf("delivered[%d] = %+v, want %+v", i, got[i], tc.wantMsgs[i])
				}
			}
		})
	}
}

// FuzzWalkNlMsgs asserts the walker never panics and never hands onMsg a body
// that is not a sub-slice of the input, for any byte soup.
//
// go test ./pkg/xtcpnl/ -run '^$' -fuzz FuzzWalkNlMsgs -fuzztime 20s
func FuzzWalkNlMsgs(f *testing.F) {
	for _, seed := range [][]byte{
		nil,
		stream(linkA, doneMsg),
		stream(linkB, linkA),
		stream(enoentMsg),
		stream(linkIntr, doneMsg),
		stream(staleDone, linkA, doneMsg),
		make([]byte, 15),
		make([]byte, 16),
	} {
		f.Add(seed, testSeq)
	}
	f.Fuzz(func(t *testing.T, data []byte, seq uint32) {
		done, err := WalkNlMsgs(data, seq, func(_ uint16, body []byte) error {
			if len(body) > len(data) {
				t.Fatalf("body longer than input: %d > %d", len(body), len(data))
			}
			return nil
		})
		if len(data) < NlMsgHdrSizeCst && !errors.Is(err, ErrShortRecv) {
			t.Fatalf("short input must yield ErrShortRecv, got done=%v err=%v", done, err)
		}
	})
}

// seqpacketPair returns a connected AF_UNIX SOCK_SEQPACKET socketpair, closed at
// test end. fds[0] plays the daemon side (DumpRtnetlink), fds[1] the "kernel".
func seqpacketPair(t *testing.T) [2]int {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("Socketpair: %v", err)
	}
	tv := unix.Timeval{Sec: 2}
	if err := unix.SetsockoptTimeval(fds[0], unix.SOL_SOCKET, unix.SO_RCVTIMEO, &tv); err != nil {
		t.Fatalf("SO_RCVTIMEO: %v", err)
	}
	t.Cleanup(func() {
		_ = unix.Close(fds[0])
		_ = unix.Close(fds[1])
	})
	return [2]int{fds[0], fds[1]}
}

// go test ./pkg/xtcpnl/ -run TestDumpRtnetlinkSocketpair
func TestDumpRtnetlinkSocketpair(t *testing.T) {
	request := BuildDumpLinkRequest(testSeq)

	tests := []struct {
		description string
		request     []byte
		replies     [][]byte // datagrams the fake kernel writes after reading the request
		closeAfter  bool     // fake kernel closes its end after writing replies (EOF)
		wantErr     error    // errors.Is target; nil = success
		wantSent    bool     // expected: the request reached the peer
		wantMsgs    []delivered
	}{
		// positive
		{"two datagrams then DONE -> both bodies, nil", request,
			[][]byte{stream(linkA), stream(linkB, doneMsg)}, false, nil, true,
			[]delivered{{uint16(unix.RTM_NEWLINK), "link-A-body-16bt"}, {uint16(unix.RTM_NEWLINK), "link-B-13byte"}}},
		{"single datagram carrying DONE -> nil", request, [][]byte{stream(linkA, doneMsg)}, false, nil, true,
			[]delivered{{uint16(unix.RTM_NEWLINK), "link-A-body-16bt"}}},
		{"zero-errno ACK ends the dump cleanly", request, [][]byte{stream(ackMsg)}, false, nil, true, nil},
		{"stale-seq DONE datagram skipped, real DONE later", request,
			[][]byte{stream(staleLink, staleDone), stream(linkA, doneMsg)}, false, nil, true,
			[]delivered{{uint16(unix.RTM_NEWLINK), "link-A-body-16bt"}}},

		// negative
		{"NLMSG_ERROR ENOENT -> wrapped errno", request, [][]byte{stream(enoentMsg)}, false, syscall.ENOENT, true, nil},
		{"peer closes without DONE -> EOF surfaces as ErrShortRecv", request, [][]byte{stream(linkA)}, true, ErrShortRecv, true,
			[]delivered{{uint16(unix.RTM_NEWLINK), "link-A-body-16bt"}}},
		{"malformed nlmsg_len in second datagram -> ErrBadMsgLen", request,
			[][]byte{stream(linkA), func() []byte {
				m := nlmsg(uint16(unix.RTM_NEWLINK), 0, testSeq, []byte("abcd"))
				binary.LittleEndian.PutUint32(m[0:4], 200)
				return m
			}()}, false, ErrBadMsgLen, true,
			[]delivered{{uint16(unix.RTM_NEWLINK), "link-A-body-16bt"}}},

		// boundary
		{"request shorter than nlmsghdr -> ErrShortRequest, nothing sent", request[:NlMsgHdrSizeCst-1], nil, false, ErrShortRequest, false, nil},

		// corner — interruption is drained to DONE before being reported
		{"DUMP_INTR in first datagram, DONE two datagrams later -> ErrDumpInterrupted after drain", request,
			[][]byte{stream(linkA, linkIntr), stream(linkB), stream(doneMsg)}, false, ErrDumpInterrupted, true,
			[]delivered{{uint16(unix.RTM_NEWLINK), "link-A-body-16bt"}}},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			fds := seqpacketPair(t)

			// Fake kernel: read the request, verify it, then script the replies.
			sentCh := make(chan []byte, 1)
			go func() {
				defer func() {
					if tc.closeAfter {
						_ = unix.Close(fds[1])
					}
				}()
				if !tc.wantSent {
					return
				}
				rb := make([]byte, 256)
				n, _, err := unix.Recvfrom(fds[1], rb, 0)
				if err != nil {
					sentCh <- nil
					return
				}
				sentCh <- rb[:n]
				for _, d := range tc.replies {
					if _, err := unix.Write(fds[1], d); err != nil {
						return
					}
				}
			}()

			var got []delivered
			err := DumpRtnetlink(fds[0], tc.request, nil, collector(&got, nil))

			if tc.wantErr == nil && err != nil {
				t.Errorf("err = %v, want nil", err)
			}
			if tc.wantErr != nil && !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want errors.Is(%v)", err, tc.wantErr)
			}
			if tc.wantSent {
				select {
				case sent := <-sentCh:
					if !bytes.Equal(sent, tc.request) {
						t.Errorf("peer received %x, want the request %x", sent, tc.request)
					}
				case <-time.After(2 * time.Second):
					t.Error("peer never received the request")
				}
			}
			if len(got) != len(tc.wantMsgs) {
				t.Fatalf("delivered %d messages %v, want %d %v", len(got), got, len(tc.wantMsgs), tc.wantMsgs)
			}
			for i := range got {
				if got[i] != tc.wantMsgs[i] {
					t.Errorf("delivered[%d] = %+v, want %+v", i, got[i], tc.wantMsgs[i])
				}
			}
		})
	}
}

// TestFromKernel covers the sender-pid filter in isolation.
//
// go test ./pkg/xtcpnl/ -run TestFromKernel
func TestFromKernel(t *testing.T) {
	tests := []struct {
		description string
		from        unix.Sockaddr
		want        bool
	}{
		{"netlink pid 0 (kernel) -> accepted", &unix.SockaddrNetlink{Family: unix.AF_NETLINK}, true},
		{"netlink pid 4242 (userspace peer) -> rejected", &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Pid: 4242}, false},
		{"nil address (connected socket) -> accepted", nil, true},
		{"AF_UNIX address (socketpair) -> accepted", &unix.SockaddrUnix{Name: "@x"}, true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := fromKernel(tc.from); got != tc.want {
				t.Errorf("fromKernel(%+v) = %v, want %v", tc.from, got, tc.want)
			}
		})
	}
}
