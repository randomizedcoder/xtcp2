package rdmanative

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"syscall"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmaevents"
	"golang.org/x/sys/unix"
)

// pathBatch is the most datagrams one Receive delivers, as nlconn's.
const pathBatch = 64

// socketRoute is a routeConn over one end of a SOCK_SEQPACKET socketpair. It
// receives as nlconn.Conn does: park in the runtime poller (epoll) for the
// first datagram, then drain what is queued without waiting, up to 64, with
// the RawConn callbacks bound once so a datagram allocates nothing. It reads
// with read(2) where nlconn calls recvmsg(2), because nlconn's netlink sender
// check is left out: the peer is not the kernel. nlconn's BenchmarkReceive
// measures nlconn itself. The event path benchmark writes notifications to
// the peer, so a batch crosses a real descriptor, epoll, the read, the
// envelope walk, the translator and delivery.
type socketRoute struct {
	file       *os.File
	raw        syscall.RawConn
	buf        []byte
	n          int
	err        error
	read       func(uintptr) bool
	readNoWait func(uintptr)
}

// newSocketRoute returns the source's end and the peer's descriptor, both
// closed at the end of the test.
func newSocketRoute(tb testing.TB) (*socketRoute, int) {
	tb.Helper()
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_SEQPACKET|unix.SOCK_NONBLOCK|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = unix.Close(fds[1]) })
	file := os.NewFile(uintptr(fds[0]), "event-path")
	raw, err := file.SyscallConn()
	if err != nil {
		tb.Fatal(err)
	}
	r := &socketRoute{file: file, raw: raw, buf: make([]byte, 32<<10)}
	r.read = r.attempt
	r.readNoWait = func(fd uintptr) { r.attempt(fd) }
	tb.Cleanup(func() { _ = r.Close() })
	return r, fds[1]
}

func (r *socketRoute) attempt(fd uintptr) bool {
	r.n, r.err = unix.Read(int(fd), r.buf)
	return !errors.Is(r.err, unix.EAGAIN)
}

func (r *socketRoute) Receive(_ context.Context, visit func([]byte) error) (int, error) {
	delivered := 0
	for delivered < pathBatch {
		var err error
		if delivered == 0 {
			err = r.raw.Read(r.read)
		} else {
			err = r.raw.Control(r.readNoWait)
		}
		switch {
		case err != nil:
			return delivered, err
		case delivered > 0 && errors.Is(r.err, unix.EAGAIN):
			return delivered, nil
		case r.err != nil:
			return delivered, r.err
		case r.n == 0:
			return delivered, io.EOF
		}
		delivered++
		if err := visit(r.buf[:r.n]); err != nil {
			return delivered, err
		}
	}
	return delivered, nil
}

func (r *socketRoute) Close() error { return r.file.Close() }

// pathMap holds ports bound netdevs 100 to 100+ports-1, one port each.
func pathMap(tb testing.TB, ports int) *PortMap {
	tb.Helper()
	bindings := make([]Binding, ports)
	links := make([]Link, ports)
	for i := range ports {
		bindings[i] = Binding{Device: fmt.Sprintf("bench_%02d", i), Port: 1, Netdev: uint32(100 + i)}
		links[i] = Link{Index: uint32(100 + i)}
	}
	m, err := NewPortMap(bindings, links)
	if err != nil {
		tb.Fatal(err)
	}
	return m
}

// pathDatagrams is one RTM_NEWLINK notification per port, each in its own
// datagram, as the kernel sends them.
func pathDatagrams(ports int, flags uint32) [][]byte {
	out := make([][]byte, ports)
	for i := range ports {
		out[i] = nlmsg(rtmNewLink, 0, seedBody(uint32(100+i), flags))
	}
	return out
}

// runPath starts the source's Run over r, calling deliver for each event.
// Run's error arrives on the returned channel; the function stops Run.
func runPath(tb testing.TB, r *socketRoute, m *PortMap, deliver func(rdmaevents.Event) bool) (<-chan error, func()) {
	tb.Helper()
	ctx, cancel := context.WithCancel(tb.Context())
	s := newSource(r, m)
	done := make(chan error, 1)
	go func() { done <- s.Run(ctx, deliver) }()
	return done, func() {
		cancel()
		_ = s.Close()
	}
}

func writeAll(tb testing.TB, fd int, datagrams [][]byte) {
	tb.Helper()
	for _, d := range datagrams {
		if _, err := unix.Write(fd, d); err != nil {
			tb.Fatal(err)
		}
	}
}

// TestEventPath is the condition table for the path BenchmarkEventPath
// measures: what crosses the descriptor, and how Run ends.
func TestEventPath(t *testing.T) {
	refreshes := func(ports int) []rdmaevents.Event {
		out := make([]rdmaevents.Event, ports)
		for i := range ports {
			out[i] = refresh(fmt.Sprintf("bench_%02d", i), 1)
		}
		return out
	}
	for _, tc := range []struct {
		name, category, description, expected string
		ports                                 int
		rounds                                [][][]byte
		want                                  []rdmaevents.Event
		closePeer                             bool
		wantErr                               error
	}{
		{"one", "positive", "one port's link goes down", "one Refresh",
			1, [][][]byte{pathDatagrams(1, down)}, refreshes(1), false, context.Canceled},
		{"full_batch", "boundary", "64 ports change in one burst, one datagram each", "64 Refresh in port order",
			pathBatch, [][][]byte{pathDatagrams(pathBatch, down)}, refreshes(pathBatch), false, context.Canceled},
		{"over_batch", "boundary", "65 datagrams, one more than a Receive drains", "the 65th port's Refresh from a second Receive",
			pathBatch + 1, [][][]byte{pathDatagrams(pathBatch+1, down)}, refreshes(pathBatch + 1), false, context.Canceled},
		{"unchanged", "corner", "the same state sent twice", "one Refresh: the second is not a change",
			1, [][][]byte{pathDatagrams(1, down), pathDatagrams(1, down), pathDatagrams(1, up)},
			append(refreshes(1), refreshes(1)...), false, context.Canceled},
		{"peer_closed", "negative", "the writer closes its end", "Run returns io.EOF",
			1, nil, nil, true, io.EOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			r, peer := newSocketRoute(t)
			got := make(chan rdmaevents.Event, len(tc.want)+1)
			done, stop := runPath(t, r, pathMap(t, tc.ports), func(e rdmaevents.Event) bool {
				got <- e
				return true
			})
			for _, round := range tc.rounds {
				writeAll(t, peer, round)
			}
			events := make([]rdmaevents.Event, 0, len(tc.want))
			for range tc.want {
				events = append(events, <-got)
			}
			if tc.closePeer {
				if err := unix.Shutdown(peer, unix.SHUT_WR); err != nil {
					t.Fatal(err)
				}
			} else {
				stop()
			}
			err := <-done
			stop()
			if !errors.Is(err, tc.wantErr) || !slices.Equal(events, tc.want) {
				t.Fatalf("%s: got %v, %v", tc.expected, events, err)
			}
		})
	}
}

// BenchmarkEventPath is a batch of link notifications from write to delivery:
// one datagram per port through a real descriptor and the runtime poller,
// then recvmsg, the envelope walk, the translator and emit, which hands the
// batch back to the benchmark goroutine as the monitor's worker would take
// it. Rounds alternate down and up so every notification is a state change
// and is delivered. ns/event is comparable with the removed cgo backend's
// BenchmarkPerformanceCGO (R0-T05's committed output, which
// bench-linkmonitor-rdma-native --baseline reads), which covered only the cgo
// copy and acknowledgement of each event, with no descriptor, poll or read.
func BenchmarkEventPath(b *testing.B) {
	for _, size := range []int{1, 8, 32, 64} {
		b.Run(fmt.Sprintf("batch=%d", size), func(b *testing.B) {
			r, peer := newSocketRoute(b)
			rounds := [2][][]byte{pathDatagrams(size, down), pathDatagrams(size, up)}
			batch := make(chan struct{}, 1)
			delivered := 0
			done, stop := runPath(b, r, pathMap(b, size), func(rdmaevents.Event) bool {
				if delivered++; delivered == size {
					delivered = 0
					batch <- struct{}{}
				}
				return true
			})
			b.ReportAllocs()
			for i := 0; b.Loop(); i++ {
				writeAll(b, peer, rounds[i%2])
				<-batch
			}
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*size), "ns/event")
			stop()
			if err := <-done; !errors.Is(err, context.Canceled) {
				b.Fatal(err)
			}
		})
	}
}
