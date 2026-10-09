package rdmaevents

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

type providerFunc func(context.Context, Identity) (Handle, error)

func (f providerFunc) Open(ctx context.Context, id Identity) (Handle, error) { return f(ctx, id) }

type pipeHandle struct {
	fd, writer    int
	reads, closes atomic.Int64
	kind          Kind
}

func newPipe(t testing.TB) *pipeHandle {
	t.Helper()
	var fds [2]int
	if err := unix.Pipe2(fds[:], unix.O_NONBLOCK|unix.O_CLOEXEC); err != nil {
		t.Fatal(err)
	}
	h := &pipeHandle{fd: fds[0], writer: fds[1]}
	t.Cleanup(func() {
		if err := unix.Close(h.writer); err != nil {
			t.Error(err)
		}
	})
	return h
}

func (h *pipeHandle) FD() int { return h.fd }
func (h *pipeHandle) Next() (Event, error) {
	var b [1]byte
	n, err := unix.Read(h.fd, b[:])
	if err != nil {
		return Event{}, err
	}
	if n != 1 {
		return Event{}, ErrLost
	}
	h.reads.Add(1)
	return Event{Device: "hca", Port: 1, Kind: h.kind}, nil
}
func (h *pipeHandle) Close() error { h.closes.Add(1); return unix.Close(h.fd) }

func TestPollSourceDelivery(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		count                                        int
		reject                                       bool
	}{
		{"one", "positive", "one ready event", "one owned delivery then joined cancellation", 1, false},
		{"below", "boundary", "63 ready events", "all delivered and consumed once", 63, false},
		{"batch", "boundary", "64 ready events", "exact batch consumed once", 64, false},
		{"above", "boundary", "65 ready events", "level readiness drains the next batch", 65, false},
		{"reject", "negative", "consumer rejects first event", "consumed event released before loss return", 65, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			h := newPipe(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			s, err := Open(ctx, providerFunc(func(context.Context, Identity) (Handle, error) { return h, nil }), []Identity{{Name: "hca"}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := unix.Write(h.writer, make([]byte, tc.count)); err != nil {
				t.Fatal(err)
			}
			delivered := 0
			err = s.Run(ctx, func(e Event) bool {
				delivered++
				if e.Device != "hca" || e.Port != 1 || h.reads.Load() != int64(delivered) {
					t.Error(tc.expectedOutcome)
				}
				if delivered == tc.count {
					cancel()
				}
				return !tc.reject
			})
			want := tc.count
			if tc.reject {
				want = 1
				if !errors.Is(err, ErrLost) {
					t.Fatal(err)
				}
			} else if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if delivered != want || h.closes.Load() != 1 {
				t.Fatal(tc.expectedOutcome, delivered, h.closes.Load())
			}
			if err := s.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPollSourceAcquisition(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		ids                                          []Identity
		failAt                                       int
		want                                         error
	}{
		{"empty", "boundary", "no HCAs", "empty poll source can cancel", nil, -1, nil},
		{"duplicate", "negative", "duplicate HCA name", "partial resources closed exactly once", []Identity{{Name: "a"}, {Name: "a"}}, -1, ErrIdentity},
		{"denied", "negative", "second acquisition denied", "first context retained and denial emitted before joined cleanup", []Identity{{Name: "a"}, {Name: "b"}}, 1, nil},
		{"oversize", "boundary", "discovery bound plus one", "reject before any acquisition", make([]Identity, Limit+1), -1, ErrLimit},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			var handles []*pipeHandle
			provider := providerFunc(func(context.Context, Identity) (Handle, error) {
				if len(handles) == tc.failAt {
					return nil, unix.EACCES
				}
				h := newPipe(t)
				handles = append(handles, h)
				return h, nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s, err := Open(ctx, provider, tc.ids)
			if !errors.Is(err, tc.want) {
				t.Fatal(err, tc.want)
			}
			if err == nil {
				cancel()
				faults := 0
				if err := s.Run(ctx, func(e Event) bool {
					if errors.Is(e.Err, unix.EACCES) {
						faults++
					}
					return true
				}); !errors.Is(err, context.Canceled) {
					t.Fatal(err)
				}
				if tc.failAt == 1 && faults != 1 {
					t.Fatal("missing acquisition diagnostic")
				}
			}
			for _, h := range handles {
				if h.closes.Load() != 1 {
					t.Fatal(tc.expectedOutcome)
				}
			}
		})
	}
}

func BenchmarkEventDelivery(b *testing.B) {
	for _, count := range []int{1, 64} {
		b.Run(fmt.Sprint(count), func(b *testing.B) {
			h := newPipe(b)
			defer func() {
				if err := h.Close(); err != nil {
					b.Fatal(err)
				}
			}()
			s := &pollSource{handles: map[int32]Handle{1: h}}
			data := make([]byte, count)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if _, err := unix.Write(h.writer, data); err != nil {
					b.Fatal(err)
				}
				if err := s.drain(unix.EpollEvent{Fd: 1, Events: unix.EPOLLIN}, func(Event) bool { return true }); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestFatalContextIsolation(t *testing.T) {
	t.Log("corner: one of two HCAs is fatal; expected: fatal handle retired once, second descriptor continues and stale readiness is ignored")
	bad, good := newPipe(t), newPipe(t)
	bad.kind = Fatal
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	source, err := Open(ctx, providerFunc(func(_ context.Context, id Identity) (Handle, error) {
		if id.Name == "bad" {
			return bad, nil
		}
		return good, nil
	}), []Identity{{Name: "bad"}, {Name: "good"}})
	if err != nil {
		t.Fatal(err)
	}
	s := source.(*pollSource)
	if _, err := unix.Write(bad.writer, []byte{1}); err != nil {
		t.Fatal(err)
	}
	if err := s.drain(unix.EpollEvent{Fd: 1, Events: unix.EPOLLIN}, func(e Event) bool { return e.Kind == Fatal }); err != nil {
		t.Fatal(err)
	}
	if bad.closes.Load() != 1 || good.closes.Load() != 0 {
		t.Fatal("fatal context ownership incorrect")
	}
	if err := s.drain(unix.EpollEvent{Fd: 1, Events: unix.EPOLLIN}, func(Event) bool { t.Fatal("obsolete token delivered"); return false }); err != nil {
		t.Fatal(err)
	}
	if _, err := unix.Write(good.writer, []byte{1}); err != nil {
		t.Fatal(err)
	}
	n := 0
	if err := s.Run(ctx, func(e Event) bool { n++; cancel(); return e.Kind == Refresh }); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if n != 1 || good.closes.Load() != 1 || bad.closes.Load() != 1 {
		t.Fatal("healthy HCA failed progress")
	}
}
