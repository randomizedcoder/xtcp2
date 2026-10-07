package netlink

import (
	"context"
	"errors"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type fakeFile struct {
	closes                      atomic.Int32
	closed                      chan struct{}
	closeErr, deadlineErr       error
	readDeadline, writeDeadline time.Time
}

func (f *fakeFile) Close() error {
	if f.closes.Add(1) == 1 && f.closed != nil {
		close(f.closed)
	}
	return f.closeErr
}
func (f *fakeFile) SetReadDeadline(d time.Time) error  { f.readDeadline = d; return f.deadlineErr }
func (f *fakeFile) SetWriteDeadline(d time.Time) error { f.writeDeadline = d; return f.deadlineErr }

type fakeRaw struct {
	read, write                    func(func(uintptr) bool) error
	control                        func(func(uintptr)) error
	reads, writes, controls, parks int
	inside                         bool
}

func (r *fakeRaw) Read(fn func(uintptr) bool) error {
	r.reads++
	if r.read != nil {
		return r.read(fn)
	}
	return r.retry(fn)
}
func (r *fakeRaw) Write(fn func(uintptr) bool) error {
	r.writes++
	if r.write != nil {
		return r.write(fn)
	}
	return r.retry(fn)
}
func (r *fakeRaw) Control(fn func(uintptr)) error {
	r.controls++
	if r.control != nil {
		return r.control(fn)
	}
	r.inside = true
	defer func() { r.inside = false }()
	fn(123)
	return nil
}
func (r *fakeRaw) retry(fn func(uintptr) bool) error {
	r.inside = true
	defer func() { r.inside = false }()
	for range 100 {
		if fn(123) {
			return nil
		}
		r.parks++
	}
	return errors.New("script exhausted readiness retries")
}

func testConn() (*Conn, *fakeFile, *fakeRaw) {
	f, r := &fakeFile{closed: make(chan struct{})}, &fakeRaw{}
	return &Conn{file: f, raw: r, buffer: make([]byte, initialBuffer)}, f, r
}

func TestOpenOwnership(t *testing.T) {
	boom := errors.New("injected setup failure")
	for _, row := range []struct {
		description, fail                     string
		protocol                              int
		groups                                []uint32
		cancelBefore, cancelAfter             bool
		wantErr                               error
		wantOpen, wantRawClose, wantFileClose int
	}{
		{"route socket uses atomic flags and auto port", "", unix.NETLINK_ROUTE, nil, false, false, nil, 1, 0, 1},
		{"generic memberships use IDs not masks", "", unix.NETLINK_GENERIC, []uint32{1, 65}, false, false, nil, 1, 0, 1},
		{"RDMA protocol accepted independently", "", unix.NETLINK_RDMA, nil, false, false, nil, 1, 0, 1},
		{"invalid protocol before acquisition", "", -1, nil, false, false, ErrConfig, 0, 0, 0},
		{"zero membership rejected", "", unix.NETLINK_ROUTE, []uint32{0}, false, false, ErrConfig, 0, 0, 0},
		{"overflow membership rejected", "", unix.NETLINK_ROUTE, []uint32{0x80000000}, false, false, ErrConfig, 0, 0, 0},
		{"pre-cancel acquires nothing", "", unix.NETLINK_ROUTE, nil, true, false, context.Canceled, 0, 0, 0},
		{"socket failure owns no fd", "socket", unix.NETLINK_ROUTE, nil, false, false, boom, 1, 0, 0},
		{"bind failure raw-closes once", "bind", unix.NETLINK_ROUTE, nil, false, false, boom, 1, 1, 0},
		{"join failure raw-closes once", "join", unix.NETLINK_ROUTE, []uint32{1}, false, false, boom, 1, 1, 0},
		{"wrapper failure transfers ownership", "wrap", unix.NETLINK_ROUTE, nil, false, false, boom, 1, 0, 1},
		{"post-acquisition cancel closes wrapped fd", "", unix.NETLINK_ROUTE, nil, false, true, context.Canceled, 1, 0, 1},
	} {
		t.Run(row.description, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if row.cancelBefore {
				cancel()
			}
			opened, rawClosed, joined := 0, 0, 0
			f := &fakeFile{}
			ops := socketOps{
				socket: func(domain, typ, protocol int) (int, error) {
					opened++
					if domain != unix.AF_NETLINK || typ != unix.SOCK_RAW|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC || protocol != row.protocol {
						t.Fatal("wrong socket setup")
					}
					if row.fail == "socket" {
						return -1, boom
					}
					return 123, nil
				},
				bind: func(fd int, sa unix.Sockaddr) error {
					v, ok := sa.(*unix.SockaddrNetlink)
					if fd != 123 || !ok || v.Family != unix.AF_NETLINK || v.Pid != 0 || v.Groups != 0 {
						t.Fatal("wrong bind")
					}
					if row.fail == "bind" {
						return boom
					}
					return nil
				},
				join: func(fd, level, opt, value int) error {
					if fd != 123 || level != unix.SOL_NETLINK || opt != unix.NETLINK_ADD_MEMBERSHIP || value != int(row.groups[joined]) {
						t.Fatal("wrong membership")
					}
					joined++
					if row.fail == "join" {
						return boom
					}
					return nil
				},
				close: func(fd int) error {
					if fd != 123 {
						t.Fatal("wrong close")
					}
					rawClosed++
					return nil
				},
				wrap: func(fd int) (pollFile, syscall.RawConn, error) {
					if fd != 123 {
						t.Fatal("wrong wrap")
					}
					if row.fail == "wrap" {
						return nil, nil, errors.Join(boom, f.Close())
					}
					if row.cancelAfter {
						cancel()
					}
					return f, &fakeRaw{}, nil
				},
			}
			c, err := openSocket(ctx, row.protocol, row.groups, ops)
			if !errors.Is(err, row.wantErr) {
				t.Fatalf("error=%v want=%v", err, row.wantErr)
			}
			if c != nil {
				if len(c.buffer) != initialBuffer {
					t.Fatal("wrong initial buffer")
				}
				for range 3 {
					if err := c.Close(); err != nil {
						t.Fatal(err)
					}
				}
			}
			if opened != row.wantOpen || rawClosed != row.wantRawClose || int(f.closes.Load()) != row.wantFileClose {
				t.Fatalf("opened=%d rawclosed=%d fileclosed=%d", opened, rawClosed, f.closes.Load())
			}
		})
	}
}

func TestOperationCancellationAndTimeout(t *testing.T) {
	for _, row := range []struct {
		description  string
		send, cancel bool
		want         error
	}{
		{"read cancellation wakes and closes", false, true, context.Canceled},
		{"write cancellation wakes and closes", true, true, context.Canceled},
		{"read poll deadline retires socket", false, false, os.ErrDeadlineExceeded},
		{"write poll deadline retires socket", true, false, os.ErrDeadlineExceeded},
	} {
		t.Run(row.description, func(t *testing.T) {
			c, f, r := testConn()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			run := func(func(uintptr) bool) error {
				if row.cancel {
					cancel()
					<-f.closed
					return os.ErrClosed
				}
				return os.ErrDeadlineExceeded
			}
			r.read, r.write = run, run
			var err error
			if row.send {
				err = c.Send(ctx, []byte{1})
			} else {
				_, err = c.Receive(ctx, func([]byte) error { return nil })
			}
			if !errors.Is(err, row.want) || f.closes.Load() != 1 {
				t.Fatalf("error=%v closes=%d", err, f.closes.Load())
			}
			if err := c.Close(); err != nil || f.closes.Load() != 1 {
				t.Fatalf("close=%v calls=%d", err, f.closes.Load())
			}
		})
	}
}

func TestOperationDeadlineAndOwner(t *testing.T) {
	c, f, r := testConn()
	deadline := time.Now().Add(time.Hour)
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	c.recv = func(int, []byte, []byte, int) (int, int, int, unix.Sockaddr, error) {
		return 1, 0, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}, nil
	}
	stop := errors.New("stop")
	_, err := c.Receive(ctx, func([]byte) error {
		if r.inside {
			t.Fatal("decode inside raw callback")
		}
		if err := c.Send(ctx, []byte{1}); !errors.Is(err, ErrConcurrent) {
			t.Fatalf("overlapping send: %v", err)
		}
		if _, err := c.Receive(ctx, func([]byte) error { return nil }); !errors.Is(err, ErrConcurrent) {
			t.Fatalf("overlapping receive: %v", err)
		}
		return stop
	})
	if !errors.Is(err, stop) || !f.readDeadline.Equal(deadline) {
		t.Fatalf("error=%v deadline=%v", err, f.readDeadline)
	}
	// Completion unregisters cancellation: canceling this old context must not
	// close a later operation on the same socket.
	cancel()
	if f.closes.Load() != 0 {
		t.Fatal("completed context closed socket")
	}
	c.send = func(int, []byte, []byte, unix.Sockaddr, int) (int, error) { return 1, nil }
	if err := c.Send(context.Background(), []byte{1}); err != nil {
		t.Fatal(err)
	}
	if !f.writeDeadline.IsZero() {
		t.Fatal("deadline was not cleared")
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}
