package xtcp

import (
	"errors"
	"os"
	"runtime"
	"slices"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	xio "github.com/randomizedcoder/xtcp2/pkg/io_uring"
)

const sockstatHostStyle = `sockets: used 1234
TCP: inuse 107 orphan 0 tw 4 alloc 120 mem 9
UDP: inuse 3 mem 2
UDPLITE: inuse 0
RAW: inuse 0
FRAG: inuse 0 memory 0
`

func sockstatFile(inuse, tw string) string {
	return "sockets: used 1\nTCP: inuse " + inuse + " orphan 0 tw " + tw + " alloc 0 mem 0\nUDP: inuse 0 mem 0\n"
}

// TestParseSockstat covers the sockstat TCP line parser.
func TestParseSockstat(t *testing.T) {
	tests := []struct {
		name          string
		category      string
		description   string
		in            string
		expectedInuse uint64
		expectedTW    uint64
		expectedOK    bool
	}{
		{name: "normal", category: "P", description: "host-style file", in: sockstatHostStyle, expectedInuse: 107, expectedTW: 4, expectedOK: true},
		{name: "fresh_netns", category: "P", description: "all zeros", in: sockstatFile("0", "0"), expectedOK: true},
		{name: "half_million", category: "P", description: "500k inuse, 20k TIME_WAIT", in: sockstatFile("500000", "20000"), expectedInuse: 500000, expectedTW: 20000, expectedOK: true},
		{name: "int32_max", category: "B", description: "counters at INT_MAX (the kernel prints %d)", in: sockstatFile("2147483647", "2147483647"), expectedInuse: 2147483647, expectedTW: 2147483647, expectedOK: true},
		{name: "first_line", category: "B", description: "TCP line first in the buffer", in: "TCP: inuse 5 orphan 0 tw 1 alloc 0 mem 0\n", expectedInuse: 5, expectedTW: 1, expectedOK: true},
		{name: "missing_tcp_line", category: "N", description: "no TCP: line", in: "sockets: used 1\nUDP: inuse 0 mem 0\n"},
		{name: "truncated", category: "N", description: "short read cut mid-number", in: "sockets: used 1\nTCP: inuse 10"},
		{name: "truncated_mid_line", category: "N", description: "short read cut after tw", in: "sockets: used 1\nTCP: inuse 10 orphan 0 tw 3"},
		{name: "garbage_digits", category: "N", description: "non-digit in a count", in: sockstatFile("12a4", "0")},
		{name: "negative_inuse", category: "N", description: "inuse is never negative (clamped by the kernel)", in: sockstatFile("-5", "0")},
		{name: "missing_value", category: "N", description: "key without a value", in: "TCP: inuse\n"},
		{name: "too_long", category: "N", description: "20 digits would overflow", in: sockstatFile("99999999999999999999", "0")},
		{name: "empty", category: "N", description: "empty read", in: ""},
		{name: "extra_whitespace", category: "C", description: "doubled spaces and tabs", in: "TCP:  inuse  7\torphan 0  tw  2 alloc 0 mem 0\n", expectedInuse: 7, expectedTW: 2, expectedOK: true},
		{name: "tw_minus_one", category: "C", description: "tw -1 during namespace teardown", in: sockstatFile("3", "-1"), expectedInuse: 3, expectedOK: true},
		{name: "tcp6_not_tcp", category: "C", description: "a TCP6: line (sockstat6) is not TCP:", in: "TCP6: inuse 9\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			inuse, tw, ok := parseSockstat([]byte(tc.in))
			if ok != tc.expectedOK || inuse != tc.expectedInuse || tw != tc.expectedTW {
				t.Errorf("%s: got (%d, %d, %v), expected (%d, %d, %v)",
					tc.description, inuse, tw, ok, tc.expectedInuse, tc.expectedTW, tc.expectedOK)
			}
		})
	}
}

// TestParseSockstatAllocs: the parser runs per namespace per poll.
func TestParseSockstatAllocs(t *testing.T) {
	b := []byte(sockstatHostStyle)
	if n := testing.AllocsPerRun(100, func() { parseSockstat(b) }); n != 0 {
		t.Errorf("parseSockstat allocates %v per call, expected 0", n)
	}
}

// openSockstatFDs opens n fds on this process's sockstat.
func openSockstatFDs(t *testing.T, n int) []int {
	t.Helper()
	fds := make([]int, n)
	for i := range fds {
		f, err := os.Open("/proc/self/net/sockstat")
		if err != nil {
			t.Skipf("sockstat: %v", err)
		}
		t.Cleanup(func() { _ = f.Close() })
		fds[i] = int(f.Fd())
	}
	return fds
}

// directSockstat is the count a plain read gives, for comparison.
func directSockstat(t *testing.T) sockstatCount {
	t.Helper()
	b, err := os.ReadFile("/proc/self/net/sockstat")
	if err != nil {
		t.Skip(err)
	}
	inuse, tw, ok := parseSockstat(b)
	if !ok {
		t.Fatalf("unparsable sockstat %q", b)
	}
	return sockstatCount{inuse: inuse, tw: tw, known: true}
}

// lockedRing makes a real ring for the batcher, owned by this test's
// thread (SINGLE_ISSUER).
func lockedRing(t *testing.T, batch int) *xio.Ring {
	t.Helper()
	runtime.LockOSThread()
	t.Cleanup(func() {
		runtime.UnlockOSThread() //nolint:forbidigo // safe: test pins the SINGLE_ISSUER ring to one thread; no netns change
	})
	r, err := xio.New(xio.Config{RecvBatchSize: batch, CQEBatchSize: batch})
	if err != nil {
		t.Skipf("io_uring unavailable: %v", err)
	}
	t.Cleanup(func() { r.Close(0, nil) })
	return r
}

// TestSockstatReader runs the same table through the pread reader and the
// io_uring batcher. Counts are checked on a quiet test process, so the
// direct read and the reader's read see the same sockets.
func TestSockstatReader(t *testing.T) {
	const badFD = 1 << 20 // EBADF
	tests := []struct {
		name            string
		category        string
		description     string
		n               int
		batch           int
		bad             []int // indexes given a bad fd
		minusOne        []int // indexes given fd -1
		expectedSubmits int   // io_uring only
	}{
		{name: "one_ns", category: "P", description: "one real sockstat fd", n: 1, batch: 64, expectedSubmits: 1},
		{name: "batch_exact", category: "P", description: "128 fds, batch 64", n: 128, batch: 64, expectedSubmits: 2},
		{name: "batch_remainder", category: "B", description: "130 fds, batch 64: the last batch has 2", n: 130, batch: 64, expectedSubmits: 3},
		{name: "batch_one", category: "B", description: "batch 1: one submit per namespace", n: 5, batch: 1, expectedSubmits: 5},
		{name: "batch_over_n", category: "B", description: "10 fds, batch 128", n: 10, batch: 128, expectedSubmits: 1},
		{name: "zero_ns", category: "B", description: "no namespaces", n: 0, batch: 64, expectedSubmits: 0},
		{name: "bad_fd", category: "N", description: "one bad fd: only it is unknown", n: 8, batch: 4, bad: []int{5}, expectedSubmits: 2},
		{name: "statfd_minus_one", category: "N", description: "fd -1 is skipped, unknown, and queues no SQE", n: 4, batch: 4, minusOne: []int{0, 3}, expectedSubmits: 1},
		{name: "all_minus_one", category: "C", description: "every fd -1: no submit at all", n: 3, batch: 4, minusOne: []int{0, 1, 2}, expectedSubmits: 0},
	}
	want := directSockstat(t)
	for _, tc := range tests {
		build := func(t *testing.T) []int {
			fds := openSockstatFDs(t, tc.n)
			for _, i := range tc.bad {
				fds[i] = badFD
			}
			for _, i := range tc.minusOne {
				fds[i] = -1
			}
			return fds
		}
		check := func(t *testing.T, kind string, counts []sockstatCount, failed int) {
			t.Helper()
			for i, c := range counts {
				unknown := slices.Contains(tc.bad, i) || slices.Contains(tc.minusOne, i)
				if unknown && c.known {
					t.Errorf("%s %s: ns %d known, expected unknown", kind, tc.description, i)
				}
				if !unknown && c != want {
					t.Errorf("%s %s: ns %d = %+v, expected %+v", kind, tc.description, i, c, want)
				}
			}
			if failed != len(tc.bad) {
				t.Errorf("%s %s: failed=%d, expected %d", kind, tc.description, failed, len(tc.bad))
			}
		}
		t.Run(tc.name+"/syscall", func(t *testing.T) {
			fds := build(t)
			counts := make([]sockstatCount, len(fds))
			failed, err := newSyscallSockstatReader().read(fds, counts)
			if err != nil {
				t.Fatal(err)
			}
			check(t, "syscall", counts, failed)
		})
		t.Run(tc.name+"/io_uring", func(t *testing.T) {
			fds := build(t)
			b := newRingBatcher(lockedRing(t, tc.batch), tc.batch)
			counts := make([]sockstatCount, len(fds))
			failed, submits, err := b.read(fds, counts)
			if err != nil {
				t.Fatal(err)
			}
			check(t, "io_uring", counts, failed)
			if submits != tc.expectedSubmits {
				t.Errorf("%s: %d submits, expected %d", tc.description, submits, tc.expectedSubmits)
			}
		})
	}
}

// TestRingSockstatReader runs the goroutine-owned reader end to end.
func TestRingSockstatReader(t *testing.T) {
	r, err := newRingSockstatReader(16, 2)
	if err != nil {
		t.Skipf("io_uring unavailable: %v", err)
	}
	defer r.close()
	fds := openSockstatFDs(t, 40)
	counts := make([]sockstatCount, len(fds))
	for range 3 { // reused across polls
		failed, err := r.read(fds, counts)
		if err != nil || failed != 0 {
			t.Fatalf("failed=%d err=%v", failed, err)
		}
		for i, c := range counts {
			if !c.known {
				t.Fatalf("ns %d unknown", i)
			}
		}
	}
}

// fakeSockstatRing completes reads from a fd → file table, in an order
// the test picks.
type fakeSockstatRing struct {
	files     map[int]string
	queued    []fakeRead
	next      uint32
	reverse   bool
	failWait  bool
	completed []xio.Result
}

type fakeRead struct {
	id  uint32
	fd  int
	buf *[]byte
}

func (f *fakeSockstatRing) EnqueueReadAt(fd int, buf *[]byte, _ uint64, _ bool) (uint32, error) {
	f.next++
	f.queued = append(f.queued, fakeRead{id: f.next, fd: fd, buf: buf})
	return f.next, nil
}

func (f *fakeSockstatRing) SubmitAndWait(uint32) (int, error) {
	if f.failWait {
		return 0, errors.New("EINTR")
	}
	n := len(f.queued)
	for _, q := range f.queued {
		res := int32(copy(*q.buf, f.files[q.fd]))
		if _, ok := f.files[q.fd]; !ok {
			res = -9 // -EBADF
		}
		f.completed = append(f.completed, xio.Result{Op: xio.OpReadAt, RequestID: q.id, Res: res})
	}
	f.queued = f.queued[:0]
	if f.reverse {
		slices.Reverse(f.completed)
	}
	return n, nil
}

func (f *fakeSockstatRing) DrainBatch() []xio.Result {
	out := f.completed
	f.completed = nil
	return out
}

func (f *fakeSockstatRing) WaitOne() ([]xio.Result, error) { return nil, errors.New("nothing pending") }

// TestRingBatcherCorners uses the fake ring for completions a real kernel
// won't produce on demand.
func TestRingBatcherCorners(t *testing.T) {
	files := map[int]string{
		10: sockstatFile("1", "0"), 11: sockstatFile("2", "0"), 12: sockstatFile("3", "0"),
		13: sockstatFile("4", "0"), 14: "garbage",
	}
	tests := []struct {
		name           string
		category       string
		description    string
		fds            []int
		reverse        bool
		failWait       bool
		expectedInuse  []int64 // -1: unknown
		expectedFailed int
		expectedErr    bool
	}{
		{name: "cqe_out_of_order", category: "C", description: "CQEs complete in reverse: each lands on its own namespace",
			fds: []int{10, 11, 12, 13}, reverse: true, expectedInuse: []int64{1, 2, 3, 4}},
		{name: "in_order", category: "P", description: "CQEs in order", fds: []int{13, 12, 11, 10}, expectedInuse: []int64{4, 3, 2, 1}},
		{name: "negative_res", category: "N", description: "a CQE with -EBADF marks only its namespace unknown",
			fds: []int{10, 99, 12}, expectedInuse: []int64{1, -1, 3}, expectedFailed: 1},
		{name: "unparsable", category: "N", description: "a short or garbage read is unknown",
			fds: []int{14, 11}, expectedInuse: []int64{-1, 2}, expectedFailed: 1},
		{name: "submit_fails", category: "N", description: "a failed submit marks the batch unknown and the ring broken",
			fds: []int{10, 11, 12}, failWait: true, expectedInuse: []int64{-1, -1, -1}, expectedFailed: 3, expectedErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ring := &fakeSockstatRing{files: files, reverse: tc.reverse, failWait: tc.failWait}
			b := newRingBatcher(ring, 2)
			counts := make([]sockstatCount, len(tc.fds))
			failed, _, err := b.read(tc.fds, counts)
			if (err != nil) != tc.expectedErr || failed != tc.expectedFailed {
				t.Errorf("%s: failed=%d err=%v, expected failed=%d err=%v", tc.description, failed, err, tc.expectedFailed, tc.expectedErr)
			}
			for i, c := range counts {
				got := int64(-1)
				if c.known {
					got = int64(c.inuse)
				}
				if got != tc.expectedInuse[i] {
					t.Errorf("%s: ns %d inuse=%d, expected %d", tc.description, i, got, tc.expectedInuse[i])
				}
			}
		})
	}
}

// TestNewPollGateFallback: the reader choice, and the counted pread
// fallback when the ring can't be set up.
func TestNewPollGateFallback(t *testing.T) {
	tests := []struct {
		name             string
		category         string
		description      string
		ioUring          bool
		ringErr          error
		expectedSyscall  bool
		expectedFallback float64
	}{
		{name: "syscall_default", category: "P", description: "without -ioUring the pread reader is used", expectedSyscall: true},
		{name: "ring", category: "P", description: "with -ioUring the ring reader is used", ioUring: true},
		{name: "ring_setup_fails", category: "N", description: "ring setup error falls back to pread, counted", ioUring: true,
			ringErr: errors.New("ENOSYS"), expectedSyscall: true, expectedFallback: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			prev := newSockstatRingReader
			t.Cleanup(func() { newSockstatRingReader = prev })
			newSockstatRingReader = func(uint32, uint32) (sockstatReader, error) {
				if tc.ringErr != nil {
					return nil, tc.ringErr
				}
				return &syscallSockstatReader{}, nil // stand-in: any non-pread-default value
			}
			x := newCapsXTCP(t)
			x.config = &xtcp_config.XtcpConfig{IoUring: tc.ioUring, MaxRecordsPerPoll: 1}
			req := make([]byte, 72)
			x.nlRequest = &req
			g := x.newPollGate()
			_, isDefault := g.reader.(*syscallSockstatReader)
			isDefault = isDefault && g.reader.(*syscallSockstatReader).pread != nil
			if isDefault != tc.expectedSyscall {
				t.Errorf("%s: pread reader=%v, expected %v", tc.description, isDefault, tc.expectedSyscall)
			}
			got := testutil.ToFloat64(x.pC.WithLabelValues("poller", "sockstatRing", "fallback"))
			if got != tc.expectedFallback {
				t.Errorf("%s: fallback=%v, expected %v", tc.description, got, tc.expectedFallback)
			}
		})
	}
}

// TestSockstatPathIsThreadSelf guards the path: /proc/self/net follows the
// thread-group leader, not the thread that just called Setns.
func TestSockstatPathIsThreadSelf(t *testing.T) {
	if sockstatPath != "/proc/thread-self/net/sockstat" {
		t.Errorf("sockstatPath=%q: must follow the calling thread's namespace after Setns", sockstatPath)
	}
}
