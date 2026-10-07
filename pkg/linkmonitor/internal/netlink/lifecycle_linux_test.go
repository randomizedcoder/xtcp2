package netlink

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestOperationErrorBoundaries(t *testing.T) {
	boom := errors.New("injected failure")
	for _, row := range []struct {
		description                                  string
		send, preCancel, deadlineFailure, rawFailure bool
		want                                         error
	}{
		{"pre-canceled receive does no I/O", false, true, false, false, context.Canceled},
		{"pre-canceled send does no I/O", true, true, false, false, context.Canceled},
		{"read deadline setup failure preserved", false, false, true, false, boom},
		{"write deadline setup failure preserved", true, false, true, false, boom},
		{"raw read failure preserved", false, false, false, true, boom},
		{"raw write failure preserved", true, false, false, true, boom},
	} {
		t.Run(row.description, func(t *testing.T) {
			c, f, r := testConn()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if row.preCancel {
				cancel()
				f.closeErr = boom
			}
			if row.deadlineFailure {
				f.deadlineErr = boom
			}
			if row.rawFailure {
				r.read = func(func(uintptr) bool) error { return boom }
				r.write = r.read
			}
			var err error
			if row.send {
				err = c.Send(ctx, []byte{1})
			} else {
				_, err = c.Receive(ctx, func([]byte) error { return nil })
			}
			if !errors.Is(err, row.want) {
				t.Fatalf("err=%v want=%v", err, row.want)
			}
			if row.preCancel && (!errors.Is(err, boom) || f.closes.Load() != 1) {
				t.Fatal("cancel lost close error")
			}
			if !row.rawFailure && r.reads+r.writes+r.controls != 0 {
				t.Fatal("I/O happened after setup failure")
			}
			if c.busy.Load() {
				t.Fatal("owner not released on error")
			}
		})
	}
	c, f, r := testConn()
	if _, err := c.Receive(context.Background(), nil); !errors.Is(err, ErrConfig) {
		t.Fatalf("nil callback: %v", err)
	}
	for _, data := range [][]byte{nil, make([]byte, maxBuffer+1)} {
		if err := c.Send(context.Background(), data); !errors.Is(err, ErrLength) {
			t.Fatalf("bad request length: %v", err)
		}
	}
	if r.reads+r.writes+r.controls != 0 || f.closes.Load() != 0 {
		t.Fatal("invalid operation touched socket")
	}
}

func TestDeadlineResetAndDrainFailure(t *testing.T) {
	c, f, r := testConn()
	c.recv = func(int, []byte, []byte, int) (int, int, int, unix.Sockaddr, error) {
		return 1, 0, 0, &unix.SockaddrNetlink{Family: unix.AF_NETLINK}, nil
	}
	c.send = func(int, []byte, []byte, unix.Sockaddr, int) (int, error) { return 1, nil }
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(time.Hour))
	defer cancel()
	stop := errors.New("stop")
	for _, opCtx := range []context.Context{ctx, context.Background()} {
		if _, err := c.Receive(opCtx, func([]byte) error { return stop }); !errors.Is(err, stop) {
			t.Fatal(err)
		}
		if err := c.Send(opCtx, []byte{1}); err != nil {
			t.Fatal(err)
		}
		want, _ := opCtx.Deadline()
		if !f.readDeadline.Equal(want) || !f.writeDeadline.Equal(want) {
			t.Fatal("old deadline retained")
		}
	}
	controlErr := errors.New("drain control failure")
	r.control = func(func(uintptr)) error { return controlErr }
	n, err := c.Receive(context.Background(), func([]byte) error { return nil })
	if n != 1 || !errors.Is(err, controlErr) {
		t.Fatalf("drain hid error: n=%d err=%v", n, err)
	}
}

func TestCancellationCloseRaces(t *testing.T) {
	// Exercise real cancellation callbacks against multiple concurrent Close
	// calls. Only one file close occurs and its failure stays observable.
	for range 100 {
		c, f, r := testConn()
		closeErr := errors.New("close failure")
		f.closeErr = closeErr
		ctx, cancel := context.WithCancel(context.Background())
		started := make(chan struct{})
		r.read = func(func(uintptr) bool) error { close(started); <-f.closed; return os.ErrClosed }
		result := make(chan error, 1)
		go func() { _, err := c.Receive(ctx, func([]byte) error { return nil }); result <- err }()
		<-started
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() {
				cancel()
				if err := c.Close(); !errors.Is(err, closeErr) {
					t.Errorf("close error=%v", err)
				}
			})
		}
		wg.Wait()
		cancel()
		if err := <-result; !errors.Is(err, context.Canceled) || !errors.Is(err, os.ErrClosed) || !errors.Is(err, closeErr) {
			t.Fatalf("lost classification: %v", err)
		}
		if f.closes.Load() != 1 {
			t.Fatalf("double close: %d", f.closes.Load())
		}
		if err := c.Send(context.Background(), []byte{1}); !errors.Is(err, os.ErrClosed) {
			t.Fatal("closed socket reused")
		}
	}
}

func TestWrapRequiresPoller(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "regular-file")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	// Dup gives wrapSocket sole ownership; the original file stays independent.
	fd, err := unix.Dup(int(f.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	file, raw, err := wrapSocket(fd)
	if !errors.Is(err, os.ErrNoDeadline) || file != nil || raw != nil {
		t.Fatalf("accepted nonpollable fd: %v", err)
	}
	if _, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0); !errors.Is(err, unix.EBADF) {
		t.Fatalf("wrapper leaked fd: %v", err)
	}
	if _, err := f.Write([]byte{1}); err != nil {
		t.Fatalf("wrapper closed unowned original fd: %v", err)
	}
}
