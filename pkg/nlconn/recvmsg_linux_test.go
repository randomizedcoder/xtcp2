package nlconn

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// kernelReplyFD returns a bound NETLINK_ROUTE socket with the kernel's first
// RTM_GETLINK dump reply queued. The kernel runs a dump's first pass inside
// sendmsg, so the reply is queued when Sendto returns.
func kernelReplyFD(t *testing.T) int {
	t.Helper()
	fd, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, unix.NETLINK_ROUTE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })
	if err := unix.Bind(fd, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		t.Fatal(err)
	}
	req := make([]byte, unix.SizeofNlMsghdr+unix.SizeofIfInfomsg)
	binary.LittleEndian.PutUint32(req, uint32(len(req)))
	binary.LittleEndian.PutUint16(req[4:], unix.RTM_GETLINK)
	binary.LittleEndian.PutUint16(req[6:], unix.NLM_F_REQUEST|unix.NLM_F_DUMP)
	binary.LittleEndian.PutUint32(req[8:], 1)
	if err := unix.Sendto(fd, req, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}); err != nil {
		t.Fatal(err)
	}
	return fd
}

// peerFD returns one end of a SOCK_SEQPACKET pair with datagram queued from
// the other end, or nothing queued when datagram is nil.
func peerFD(t *testing.T, datagram []byte) int {
	t.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fds[0]); _ = unix.Close(fds[1]) })
	if datagram != nil {
		if _, err := unix.Write(fds[1], datagram); err != nil {
			t.Fatal(err)
		}
	}
	return fds[0]
}

// TestRecvmsgNetlink is the raw receive's condition table: what it reports
// for the sender, the length and the flags, against real descriptors.
func TestRecvmsgNetlink(t *testing.T) {
	closedFD := func(t *testing.T) int {
		fd := peerFD(t, nil)
		dup, err := unix.Dup(fd)
		if err != nil {
			t.Fatal(err)
		}
		if err := unix.Close(dup); err != nil {
			t.Fatal(err)
		}
		return dup
	}
	for _, tc := range []struct {
		name, category, description, expected string
		fd                                    func(*testing.T) int
		size                                  int
		wantErr                               error
		wantFamily                            uint16
		wantTrunc                             bool
	}{
		{"kernel", "positive", "a dump reply from the kernel", "AF_NETLINK, pid 0, a whole message",
			kernelReplyFD, initialBuffer, nil, unix.AF_NETLINK, false},
		{"non_netlink_peer", "negative", "a datagram from an AF_UNIX peer, whose address is shorter than a netlink one", "the sender left zeroed, so validateReceive rejects it",
			func(t *testing.T) int { return peerFD(t, make([]byte, 20)) }, initialBuffer, nil, 0, false},
		{"closed", "negative", "a closed descriptor", "EBADF", closedFD, initialBuffer, unix.EBADF, 0, false},
		{"empty_queue", "boundary", "nothing queued", "EAGAIN, without waiting",
			func(t *testing.T) int { return peerFD(t, nil) }, initialBuffer, unix.EAGAIN, 0, false},
		{"short_buffer", "boundary", "a kernel reply into a 16-byte buffer", "MSG_TRUNC set",
			kernelReplyFD, unix.SizeofNlMsghdr, nil, unix.AF_NETLINK, true},
		{"zero_buffer", "corner", "a kernel reply into an empty buffer", "MSG_TRUNC set, nothing written",
			kernelReplyFD, 0, nil, unix.AF_NETLINK, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			fd := tc.fd(t)
			buf := make([]byte, tc.size)
			from := unix.RawSockaddrNetlink{Family: 0xffff, Pid: 7}
			n, flags, err := recvmsgNetlink(fd, buf, &from, unix.MSG_DONTWAIT)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("%s: err %v", tc.expected, err)
			}
			if err != nil {
				return
			}
			trunc := flags&unix.MSG_TRUNC != 0
			if from.Family != tc.wantFamily || from.Pid != 0 || trunc != tc.wantTrunc || n < 0 {
				t.Fatalf("%s: from %+v, n %d, flags %#x", tc.expected, from, n, flags)
			}
			if !trunc && tc.wantFamily == unix.AF_NETLINK && int(binary.LittleEndian.Uint32(buf)) > n {
				t.Fatalf("%s: message length %d over %d received", tc.expected, binary.LittleEndian.Uint32(buf), n)
			}
		})
	}
}

// TestReceiveAllocs holds the receive path to no allocation per datagram,
// and Receive's per-call cost to the cancellation hook: none for a context
// that is never canceled, and context.AfterFunc's own registration for one
// that can be. The hook's callback and channel are built once per Conn.
func TestReceiveAllocs(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unix.Close(fds[0]); _ = unix.Close(fds[1]) }()
	buf := make([]byte, 64)
	var from unix.RawSockaddrNetlink
	if got := testing.AllocsPerRun(100, func() {
		_, _ = unix.Write(fds[1], buf[:16])
		_, _, _ = recvmsgNetlink(fds[0], buf, &from, unix.MSG_DONTWAIT)
	}); got != 0 {
		t.Fatalf("recvmsgNetlink: %v allocs", got)
	}

	cancelable, cancel := context.WithCancel(context.Background())
	defer cancel()
	for _, tc := range []struct {
		name, category, description, expected string
		ctx                                   context.Context
		datagrams                             int
		maxAllocs                             float64
	}{
		{"background_one", "positive", "one datagram, a context that is never canceled", "no allocation", context.Background(), 1, 0},
		{"background_full", "boundary", "a full 64-datagram batch, a context that is never canceled", "no allocation", context.Background(), maxBatch, 0},
		{"cancelable_one", "positive", "one datagram, a cancelable context", "only AfterFunc's registration", cancelable, 1, maxHookAllocs},
		{"cancelable_two", "boundary", "two datagrams, the second read without waiting", "the same as one datagram", cancelable, 2, maxHookAllocs},
		{"cancelable_full", "boundary", "a full 64-datagram batch, a cancelable context", "the same as one datagram", cancelable, maxBatch, maxHookAllocs},
		{"deadline", "corner", "a context with a deadline, which is cancelable", "only AfterFunc's registration", deadlineCtx(t), maxBatch, maxHookAllocs},
	} {
		t.Logf("%s: %s: %s; expected: %s", tc.category, tc.name, tc.description, tc.expected)
		c, _, _ := testConn()
		left := 0
		c.recv = func(_ int, b []byte, from *unix.RawSockaddrNetlink, _ int) (int, int, error) {
			if left == 0 {
				return 0, 0, unix.EAGAIN
			}
			left--
			*from = unix.RawSockaddrNetlink{Family: unix.AF_NETLINK}
			b[0] = 1
			return 1, 0, nil
		}
		visit := func([]byte) error { return nil }
		got := testing.AllocsPerRun(100, func() {
			left = tc.datagrams
			if n, err := c.Receive(tc.ctx, visit); n != tc.datagrams || err != nil {
				t.Fatalf("%s: Receive: %d, %v", tc.name, n, err)
			}
		})
		if got > tc.maxAllocs {
			t.Fatalf("%s: %s: %v allocs, at most %v", tc.name, tc.expected, got, tc.maxAllocs)
		}
	}
}

// maxHookAllocs is what one context.AfterFunc registration allocates on a
// cancelable context: its internal context and the stop function.
const maxHookAllocs = 2

func deadlineCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	t.Cleanup(cancel)
	return ctx
}

// TestCancelHookReuse is the reused cancellation hook's condition table. The
// hook and its channel are built once per Conn; every operation must leave
// the channel empty, so no Close result from one operation is read by the
// next, and a context canceled after its operation must not close the Conn.
func TestCancelHookReuse(t *testing.T) {
	kernelRecv := func(_ int, b []byte, from *unix.RawSockaddrNetlink, _ int) (int, int, error) {
		*from = unix.RawSockaddrNetlink{Family: unix.AF_NETLINK}
		b[0] = 1
		return 1, 0, nil
	}
	stop := errors.New("stop")
	visitStop := func([]byte) error { return stop }
	for _, tc := range []struct {
		name, category, description, expected string
		run                                   func(t *testing.T, c *Conn, f *fakeFile, r *fakeRaw) error
		wantErr                               error
		wantCloses                            int32
	}{
		{"repeated", "positive", "three operations, each with its own cancelable context", "one hook for all three, the Conn open",
			func(t *testing.T, c *Conn, _ *fakeFile, _ *fakeRaw) error {
				var first chan error
				for i := range 3 {
					ctx, cancel := context.WithCancel(context.Background())
					_, err := c.Receive(ctx, visitStop)
					cancel()
					if !errors.Is(err, stop) {
						return err
					}
					if i == 0 {
						first = c.cancelDone
					} else if c.cancelDone != first {
						t.Fatal("hook rebuilt")
					}
				}
				return nil
			}, nil, 0},
		{"background", "boundary", "a context that is never canceled", "no hook built", func(t *testing.T, c *Conn, _ *fakeFile, _ *fakeRaw) error {
			_, err := c.Receive(context.Background(), visitStop)
			if c.cancel != nil {
				t.Fatal("hook built for context.Background")
			}
			if errors.Is(err, stop) {
				return nil
			}
			return err
		}, nil, 0},
		{"canceled_after", "corner", "the context is canceled after its operation returned", "the Conn stays open, a later operation succeeds",
			func(_ *testing.T, c *Conn, _ *fakeFile, _ *fakeRaw) error {
				ctx, cancel := context.WithCancel(context.Background())
				_, err := c.Receive(ctx, visitStop)
				cancel()
				if !errors.Is(err, stop) {
					return err
				}
				if _, err := c.Receive(context.Background(), visitStop); !errors.Is(err, stop) {
					return err
				}
				return nil
			}, nil, 0},
		{"canceled_during", "negative", "the context is canceled while the read is parked", "context.Canceled, the Conn closed once, a later operation os.ErrClosed",
			func(t *testing.T, c *Conn, f *fakeFile, r *fakeRaw) error {
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				r.read = func(func(uintptr) bool) error {
					cancel()
					<-f.closed
					return os.ErrClosed
				}
				_, err := c.Receive(ctx, visitStop)
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("canceled read: %v", err)
				}
				if _, again := c.Receive(context.Background(), visitStop); !errors.Is(again, os.ErrClosed) {
					t.Fatalf("after close: %v", again)
				}
				return err
			}, context.Canceled, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			c, f, r := testConn()
			c.recv = kernelRecv
			err := tc.run(t, c, f, r)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("%s: %v", tc.expected, err)
			}
			if len(c.cancelDone) != 0 || f.closes.Load() != tc.wantCloses {
				t.Fatalf("%s: %d results left, %d closes", tc.expected, len(c.cancelDone), f.closes.Load())
			}
		})
	}
}
