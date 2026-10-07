package netlink

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"golang.org/x/sys/unix"
)

func TestReceiveMetadata(t *testing.T) {
	kernel := &unix.SockaddrNetlink{Family: unix.AF_NETLINK}
	for _, row := range []struct {
		description    string
		n, flags, size int
		from           unix.Sockaddr
		want           error
		wantSize       int
	}{
		{"kernel accepted independently of message header", 16, 0, initialBuffer, kernel, nil, initialBuffer},
		{"userspace sender rejected", 16, 0, initialBuffer, &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Pid: 100}, ErrSender, initialBuffer},
		{"wrong sockaddr type rejected", 16, 0, initialBuffer, &unix.SockaddrInet4{}, ErrSender, initialBuffer},
		{"wrong address family rejected", 16, 0, initialBuffer, &unix.SockaddrNetlink{Family: unix.AF_INET}, ErrSender, initialBuffer},
		{"missing sender rejected", 16, 0, initialBuffer, nil, ErrSender, initialBuffer},
		{"typed nil sender rejected", 16, 0, initialBuffer, (*unix.SockaddrNetlink)(nil), ErrSender, initialBuffer},
		{"truncation grows next buffer only", initialBuffer, unix.MSG_TRUNC, initialBuffer, kernel, ErrTruncated, initialBuffer * 2},
		{"reported full length cannot be sliced", maxBuffer + 1, unix.MSG_TRUNC, initialBuffer, kernel, ErrTruncated, initialBuffer * 2},
		{"truncation at cap fails visibly", maxBuffer, unix.MSG_TRUNC, maxBuffer, kernel, ErrOversize, maxBuffer},
		{"ancillary truncation rejected", 16, unix.MSG_CTRUNC, initialBuffer, kernel, ErrAncillary, initialBuffer},
		{"empty datagram rejected", 0, 0, initialBuffer, kernel, ErrLength, initialBuffer},
		{"negative length rejected before slice", -1, 0, initialBuffer, kernel, ErrLength, initialBuffer},
		{"impossible unflagged length rejected", initialBuffer + 1, 0, initialBuffer, kernel, ErrLength, initialBuffer},
		{"exact buffer length accepted", initialBuffer, 0, initialBuffer, kernel, nil, initialBuffer},
		{"untrusted truncation cannot grow buffer", initialBuffer, unix.MSG_TRUNC, initialBuffer, &unix.SockaddrNetlink{Family: unix.AF_NETLINK, Pid: 1}, ErrSender, initialBuffer},
	} {
		t.Run(row.description, func(t *testing.T) {
			c, _, _ := testConn()
			c.buffer = make([]byte, row.size)
			calls := 0
			c.recv = func(fd int, b, oob []byte, flags int) (int, int, int, unix.Sockaddr, error) {
				calls++
				if flags != unix.MSG_DONTWAIT || fd != 123 || len(oob) != 0 {
					t.Fatal("wrong recvmsg contract")
				}
				if calls > 1 {
					return 0, 0, 0, nil, unix.EAGAIN
				}
				binary.LittleEndian.PutUint32(b, 16)
				binary.LittleEndian.PutUint32(b[8:], 123)  // nonzero origin sequence
				binary.LittleEndian.PutUint32(b[12:], 456) // header PID is not authentication
				return row.n, 0, row.flags, row.from, nil
			}
			visited := 0
			n, err := c.Receive(context.Background(), func(b []byte) error {
				visited++
				if len(b) != row.n || cap(b) != len(b) || &b[0] != &c.buffer[0] {
					t.Fatal("invalid borrowed slice")
				}
				if binary.LittleEndian.Uint32(b[8:]) != 123 || binary.LittleEndian.Uint32(b[12:]) != 456 {
					t.Fatal("header altered")
				}
				return nil
			})
			wantVisits := 0
			if row.want == nil {
				wantVisits = 1
			}
			if !errors.Is(err, row.want) || n != wantVisits || visited != wantVisits || len(c.buffer) != row.wantSize {
				t.Fatalf("err=%v n=%d visits=%d size=%d", err, n, visited, len(c.buffer))
			}
		})
	}
}

func TestReceiveBatchAndRetries(t *testing.T) {
	for _, row := range []struct {
		description                        string
		available, wantReads, wantControls int
	}{
		{"one datagram returns without filling batch", 1, 1, 1},
		{"63 datagrams stop on EAGAIN", 63, 1, 63},
		{"64 datagrams yield at batch bound", 64, 1, 63},
		{"65th datagram stays queued for next turn", 65, 1, 63},
	} {
		t.Run(row.description, func(t *testing.T) {
			c, _, r := testConn()
			attempts, available := 0, row.available
			c.recv = func(_ int, b, _ []byte, _ int) (int, int, int, unix.Sockaddr, error) {
				attempts++
				if attempts == 1 {
					return 0, 0, 0, nil, unix.EINTR
				}
				if attempts == 2 {
					return 0, 0, 0, nil, unix.EAGAIN
				}
				if available == 0 {
					return 0, 0, 0, nil, unix.EAGAIN
				}
				b[0] = byte(row.available - available)
				available--
				return 1, 0, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}, nil
			}
			seen := 0
			n, err := c.Receive(context.Background(), func(b []byte) error {
				if r.inside {
					t.Fatal("decoding inside RawConn callback")
				}
				if b[0] != byte(seen) {
					t.Fatal("datagram ordering changed")
				}
				seen++
				return nil
			})
			if err != nil || n != min(row.available, 64) || r.reads != row.wantReads || r.controls != row.wantControls || r.parks != 1 {
				t.Fatalf("err=%v n=%d raw=%+v", err, n, r)
			}
			if available != max(row.available-64, 0) {
				t.Fatal("batch consumed extra datagram")
			}
		})
	}
}

func TestReceiveLossAndGrowth(t *testing.T) {
	c, _, r := testConn()
	c.recv = func(int, []byte, []byte, int) (int, int, int, unix.Sockaddr, error) {
		return 0, 0, unix.MSG_TRUNC, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}, nil
	}
	for size := initialBuffer; size < maxBuffer; size *= 2 {
		n, err := c.Receive(context.Background(), func([]byte) error { t.Fatal("truncated payload delivered"); return nil })
		if n != 0 || !errors.Is(err, ErrTruncated) || len(c.buffer) != size*2 {
			t.Fatalf("n=%d err=%v buffer=%d", n, err, len(c.buffer))
		}
	}
	for range 2 {
		if _, err := c.Receive(context.Background(), func([]byte) error { return nil }); !errors.Is(err, ErrOversize) || len(c.buffer) != maxBuffer {
			t.Fatalf("cap: %v size=%d", err, len(c.buffer))
		}
	}
	// ENOBUFS after a delivered prefix is still surfaced, not swallowed as an
	// empty batch. The transaction layer must discard that prefix.
	calls := 0
	c.recv = func(int, []byte, []byte, int) (int, int, int, unix.Sockaddr, error) {
		calls++
		if calls == 1 {
			return 1, 0, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}, nil
		}
		return 0, 0, 0, nil, unix.ENOBUFS
	}
	n, err := c.Receive(context.Background(), func([]byte) error { return nil })
	if n != 1 || !errors.Is(err, unix.ENOBUFS) || r.controls != 1 {
		t.Fatalf("loss hidden: n=%d err=%v controls=%d", n, err, r.controls)
	}
}

func TestSendRetriesAndFailures(t *testing.T) {
	for _, row := range []struct {
		description   string
		count         int
		failure, want error
	}{
		{"complete datagram sent after retries", 3, nil, nil},
		{"short send never retried as suffix", 2, nil, io.ErrShortWrite},
		{"syscall failure preserved", 0, unix.EPERM, unix.EPERM},
	} {
		t.Run(row.description, func(t *testing.T) {
			c, _, r := testConn()
			calls := 0
			c.send = func(fd int, b, oob []byte, to unix.Sockaddr, flags int) (int, error) {
				calls++
				sa, ok := to.(*unix.SockaddrNetlink)
				if fd != 123 || len(b) != 3 || len(oob) != 0 || !ok || sa.Family != unix.AF_NETLINK || sa.Pid != 0 || sa.Groups != 0 || flags != unix.MSG_DONTWAIT {
					t.Fatal("wrong send contract")
				}
				switch calls {
				case 1:
					return 0, unix.EINTR
				case 2:
					return 0, unix.EAGAIN
				default:
					return row.count, row.failure
				}
			}
			err := c.Send(context.Background(), []byte{1, 2, 3})
			if !errors.Is(err, row.want) || calls != 3 || r.parks != 1 || r.writes != 1 {
				t.Fatalf("err=%v calls=%d raw=%+v", err, calls, r)
			}
		})
	}
}
