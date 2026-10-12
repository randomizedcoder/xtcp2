package nlconn

import (
	"context"
	"errors"
	"os"
	"slices"
	"syscall"
	"testing"

	"golang.org/x/sys/unix"
)

// go test ./pkg/nlconn/ -run TestDrain
func TestDrain(t *testing.T) {
	kernel := &unix.SockaddrNetlink{Family: unix.AF_NETLINK}
	boom := errors.New("injected receive failure")
	type step struct {
		n     int
		flags int
		err   error
	}
	ok := step{n: 16}
	again := step{err: unix.EAGAIN}
	for _, row := range []struct {
		description string
		steps       []step
		closed      bool
		expected    int
		expectedErr error
	}{
		{description: "positive: Drain empties a queue", steps: []step{ok, ok, ok, again}, expected: 3},
		{description: "negative: Drain on a closed Conn", closed: true, expectedErr: os.ErrClosed},
		{description: "negative: a receive failure is returned", steps: []step{ok, {err: boom}}, expected: 1, expectedErr: boom},
		{description: "boundary: Drain on an empty queue", steps: []step{again}, expected: 0},
		{description: "corner: Drain sees ENOBUFS", steps: []step{{err: unix.ENOBUFS}, ok, again}, expected: 1},
		{description: "corner: a truncated datagram is discarded too", steps: []step{{n: initialBuffer, flags: unix.MSG_TRUNC}, ok, again}, expected: 1},
	} {
		t.Run(row.description, func(t *testing.T) {
			c, _, raw := testConn()
			if row.closed {
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			c.recv = func(fd int, b, oob []byte, flags int) (int, int, int, unix.Sockaddr, error) {
				if flags != unix.MSG_DONTWAIT {
					t.Fatal("Drain must never wait")
				}
				s := row.steps[calls]
				calls++
				if s.err != nil {
					return 0, 0, 0, nil, s.err
				}
				return s.n, 0, s.flags, kernel, nil
			}
			n, err := c.Drain(context.Background())
			if n != row.expected || !errors.Is(err, row.expectedErr) || (row.expectedErr == nil && err != nil) {
				t.Fatalf("n=%d err=%v, expected n=%d err=%v", n, err, row.expected, row.expectedErr)
			}
			if raw.reads != 0 {
				t.Fatalf("Drain parked %d times; it must only use Control", raw.reads)
			}
		})
	}
}

// go test ./pkg/nlconn/ -run TestOpenReceiveBuffer
func TestOpenReceiveBuffer(t *testing.T) {
	type call struct{ level, opt, value int }
	for _, row := range []struct {
		description string
		rcvBuf      int
		forceErr    error
		plainErr    error
		expected    []call
		expectedErr error
	}{
		{
			description: "positive: RcvBuf is forced",
			rcvBuf:      1 << 20,
			expected:    []call{{unix.SOL_SOCKET, unix.SO_RCVBUFFORCE, 1 << 20}, {unix.SOL_NETLINK, unix.NETLINK_ADD_MEMBERSHIP, unix.RTNLGRP_LINK}},
		},
		{
			description: "positive: without CAP_NET_ADMIN it falls back to SO_RCVBUF",
			rcvBuf:      4096,
			forceErr:    unix.EPERM,
			expected:    []call{{unix.SOL_SOCKET, unix.SO_RCVBUFFORCE, 4096}, {unix.SOL_SOCKET, unix.SO_RCVBUF, 4096}, {unix.SOL_NETLINK, unix.NETLINK_ADD_MEMBERSHIP, unix.RTNLGRP_LINK}},
		},
		{
			description: "negative: a failing fallback is returned",
			rcvBuf:      4096,
			forceErr:    unix.EPERM,
			plainErr:    unix.EINVAL,
			expected:    []call{{unix.SOL_SOCKET, unix.SO_RCVBUFFORCE, 4096}, {unix.SOL_SOCKET, unix.SO_RCVBUF, 4096}},
			expectedErr: unix.EINVAL,
		},
		{
			description: "negative: a force failure other than EPERM is not retried",
			rcvBuf:      4096,
			forceErr:    unix.EINVAL,
			expected:    []call{{unix.SOL_SOCKET, unix.SO_RCVBUFFORCE, 4096}},
			expectedErr: unix.EINVAL,
		},
		{
			description: "boundary: RcvBuf 0 sets nothing",
			expected:    []call{{unix.SOL_NETLINK, unix.NETLINK_ADD_MEMBERSHIP, unix.RTNLGRP_LINK}},
		},
		{
			description: "corner: a negative RcvBuf is a configuration error",
			rcvBuf:      -1,
			expectedErr: ErrConfig,
		},
	} {
		t.Run(row.description, func(t *testing.T) {
			var calls []call
			bound := false
			ops := socketOps{
				socket: func(int, int, int) (int, error) { return 123, nil },
				bind: func(int, unix.Sockaddr) error {
					if len(calls) > 0 && calls[len(calls)-1].level == unix.SOL_NETLINK {
						t.Fatal("membership joined before bind")
					}
					bound = true
					return nil
				},
				setInt: func(_, level, opt, value int) error {
					if level == unix.SOL_SOCKET && bound {
						t.Fatal("receive buffer set after bind")
					}
					calls = append(calls, call{level, opt, value})
					switch opt {
					case unix.SO_RCVBUFFORCE:
						return row.forceErr
					case unix.SO_RCVBUF:
						return row.plainErr
					}
					return nil
				},
				close: func(int) error { return nil },
				wrap: func(int) (pollFile, syscall.RawConn, error) {
					return &fakeFile{}, &fakeRaw{}, nil
				},
			}
			c, err := openSocket(context.Background(), unix.NETLINK_ROUTE, []uint32{unix.RTNLGRP_LINK}, Options{RcvBuf: row.rcvBuf}, ops)
			if !errors.Is(err, row.expectedErr) || (row.expectedErr == nil && err != nil) {
				t.Fatalf("err=%v, expected %v", err, row.expectedErr)
			}
			if c != nil {
				if err := c.Close(); err != nil {
					t.Fatal(err)
				}
			}
			if !slices.Equal(calls, row.expected) {
				t.Fatalf("setsockopt calls %v, expected %v", calls, row.expected)
			}
		})
	}
}
