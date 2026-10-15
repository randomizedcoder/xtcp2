package xtcp

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/testutil"

	xio "github.com/randomizedcoder/xtcp2/pkg/io_uring"
)

func newIouringFixture(t *testing.T) *XTCP {
	t.Helper()
	x := &XTCP{}
	reg := prometheus.NewRegistry()
	x.pC = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{Subsystem: "xtcp_iouring_test",
			Name: promNameCounts, Help: "test counts"},
		promLabels,
	)
	x.destBytesPool.Init(func() *[]byte { b := make([]byte, 64); return &b })
	x.packetBufferPool.Init(func() *[]byte { b := make([]byte, 64); return &b })
	return x
}

// ───────────────────────────────────────────────────────────────────────
// ringFromContext: present and absent
// ───────────────────────────────────────────────────────────────────────

func TestRingFromContext_absent(t *testing.T) {
	if got := ringFromContext(context.Background()); got != nil {
		t.Errorf("ringFromContext on bare ctx = %v, want nil", got)
	}
}

func TestRingFromContext_present(t *testing.T) {
	ring := &xio.Ring{}
	ctx := context.WithValue(context.Background(), ringCtxKey{}, ring)
	if got := ringFromContext(ctx); got != ring {
		t.Errorf("ringFromContext = %p, want %p", got, ring)
	}
}

// ───────────────────────────────────────────────────────────────────────
// isETimeError: ETIME / wrapped-errno-62 / nil / other
// ───────────────────────────────────────────────────────────────────────

func TestIsETimeError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"syscall_ETIME", syscall.ETIME, true},
		{"syscall_EAGAIN_not_ETIME", syscall.EAGAIN, false},
		{"non_errno_error", errors.New("not an errno"), false},
		// String-fallback branch: errors that wrap an ETIME but whose
		// As-cast to syscall.Errno fails should still classify via the
		// "errno 62" string compare.
		{"errno_62_string_fallback", errors.New("errno 62"), true},
		{"errno_other_string", errors.New("errno 11"), false},
		// Bug 73 regression: a wrapped (fmt.Errorf %w) ETIME should
		// classify via errors.As walking the unwrap chain. The
		// previous direct type-assert against syscall.Errno missed
		// every wrapped errno from upstream giouring helpers.
		{"wrapped_ETIME_unwrap_chain", fmt.Errorf("giouring layer: %w", syscall.ETIME), true},
		{"wrapped_EAGAIN_unwrap_chain", fmt.Errorf("layer: %w", syscall.EAGAIN), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isETimeError(tc.err); got != tc.want {
				t.Errorf("isETimeError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// isTimeoutErrno: EAGAIN/EWOULDBLOCK/ETIME → true; else false
// ───────────────────────────────────────────────────────────────────────

// Table-driven combination of the previous matches/misses pair.
func TestIsTimeoutErrno(t *testing.T) {
	cases := []struct {
		name string
		e    syscall.Errno
		want bool
	}{
		{"EAGAIN", syscall.EAGAIN, true},
		{"EWOULDBLOCK", syscall.EWOULDBLOCK, true},
		{"ETIME", syscall.ETIME, true},
		{"EINVAL", syscall.EINVAL, false},
		{"EPERM", syscall.EPERM, false},
		{"zero", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTimeoutErrno(tc.e); got != tc.want {
				t.Errorf("isTimeoutErrno(%v) = %v, want %v", tc.e, got, tc.want)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// opLabel: per-op string mapping
// ───────────────────────────────────────────────────────────────────────

func TestOpLabel_table(t *testing.T) {
	cases := []struct {
		op   xio.Operation
		want string
	}{
		{xio.OpSendUDP, "destUDPIoUring"},
		{xio.OpSendUnix, "destUnixIoUring"},
		{xio.OpSendUnixGram, "destUnixGramIoUring"},
		{xio.OpRead, "destIoUring"}, // default branch
	}
	for _, c := range cases {
		if got := opLabel(c.op); got != c.want {
			t.Errorf("opLabel(%d) = %q, want %q", c.op, got, c.want)
		}
	}
}

// ───────────────────────────────────────────────────────────────────────
// handleSendCQE: error path (res.Res < 0) + success path (res.Res >= 0)
// + nil-buf path doesn't panic
// ───────────────────────────────────────────────────────────────────────

func TestHandleSendCQE_error(t *testing.T) {
	x := newIouringFixture(t)
	b := make([]byte, 4)
	x.handleSendCQE(xio.Result{Op: xio.OpSendUDP, Res: -1, Buf: &b})
}

func TestHandleSendCQE_success(t *testing.T) {
	x := newIouringFixture(t)
	b := make([]byte, 4)
	x.handleSendCQE(xio.Result{Op: xio.OpSendUnixGram, Res: 4, Buf: &b})
}

func TestHandleSendCQE_nilBuf(t *testing.T) {
	x := newIouringFixture(t)
	x.handleSendCQE(xio.Result{Op: xio.OpSendUnix, Res: 4, Buf: nil})
}

// debug-level branch for the error path
func TestHandleSendCQE_errorDebugLog(t *testing.T) {
	x := newIouringFixture(t)
	x.debugLevel = 200 // hit the debugLevel > 100 log branch
	b := make([]byte, 4)
	x.handleSendCQE(xio.Result{Op: xio.OpSendUDP, Res: -1, Buf: &b})
}

// ───────────────────────────────────────────────────────────────────────
// InputValidation: wrap validateInput with x.fatalf so we can capture
// instead of exiting
// ───────────────────────────────────────────────────────────────────────

// onRingClosedResult: nil buf, OpRead, send-op
// ───────────────────────────────────────────────────────────────────────

func TestOnRingClosedResult_nilBuf(t *testing.T) {
	x := newIouringFixture(t)
	x.onRingClosedResult(xio.Result{Op: xio.OpRead, Buf: nil})
}

func TestOnRingClosedResult_recvBuf(t *testing.T) {
	x := newIouringFixture(t)
	b := make([]byte, 64)
	x.onRingClosedResult(xio.Result{Op: xio.OpRead, Buf: &b})
}

func TestOnRingClosedResult_sendBuf(t *testing.T) {
	x := newIouringFixture(t)
	b := make([]byte, 64)
	x.onRingClosedResult(xio.Result{Op: xio.OpSendUDP, Buf: &b})
}

// iouringPrefillRecvs against a real ring and a real socketpair fd.
//
// withPinnedRing pins the goroutine for the whole lifetime of the ring.
// Rings are created with IORING_SETUP_SINGLE_ISSUER (see setupFlags in
// pkg/io_uring/ring.go), so the kernel binds each ring to the task that
// created it and io_uring_enter(2) from any other task fails with EEXIST
// — surfaced by Go as "file exists". Without the pin, Go's scheduler can
// migrate this goroutine between ring creation and Submit; that is what
// made these tests flaky under -race, which adds preemption points.
// Same reasoning as runIoUringDestRow in destinations_test.go.
func withPinnedRing(t *testing.T, fn func(ring *xio.Ring)) {
	t.Helper()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread() //nolint:forbidigo // safe: io_uring test pins to one thread so the SINGLE_ISSUER ring keeps one owning task; no netns mutation.

	ring, err := xioRingNew(t)
	if err != nil {
		t.Skipf("io_uring unavailable: %v", err)
	}
	defer ring.Close(time.Second, nil)

	fn(ring)
}

func TestIouringPrefillRecvs(t *testing.T) {
	cases := []struct {
		description string
		n           int
		emptyBufs   bool // pool yields zero-length buffers → EnqueueRecvMsg rejects
		closeFD     bool // close the recv end before enqueueing
		wantErr     bool
		wantSQReady uint32 // SQEs pending after prefill, before Submit
	}{
		{
			description: "positive: two recvs on a live socketpair fd are enqueued and submit cleanly",
			n:           2,
			wantSQReady: 2,
		},
		{
			description: "negative: an empty pool buffer makes EnqueueRecvMsg reject and prefill return the error",
			n:           1,
			emptyBufs:   true,
			wantErr:     true,
			wantSQReady: 0,
		},
		{
			description: "boundary: n=0 enqueues nothing and still submits cleanly",
			n:           0,
			wantSQReady: 0,
		},
		{
			description: "corner: a closed fd still enqueues — io_uring validates the fd at completion, not at prep",
			n:           1,
			closeFD:     true,
			wantSQReady: 1,
		},
	}

	for _, c := range cases {
		t.Run(c.description, func(t *testing.T) {
			readFD, _, closeRead := makeSocketPair(t)
			if c.closeFD {
				closeRead()
			}

			x := newIouringFixture(t)
			if c.emptyBufs {
				x.packetBufferPool.Init(func() *[]byte {
					b := make([]byte, 0)
					return &b
				})
			}

			withPinnedRing(t, func(ring *xio.Ring) {
				err := x.iouringPrefillRecvs(ring, readFD, c.n)
				if gotErr := err != nil; gotErr != c.wantErr {
					t.Fatalf("iouringPrefillRecvs err = %v, wantErr %v", err, c.wantErr)
				}
				if got := ring.SQReady(); got != c.wantSQReady {
					t.Errorf("SQReady = %d, want %d", got, c.wantSQReady)
				}
				// Submit must always succeed: a failure here means the
				// ring lost its owning task (EEXIST), not that the SQEs
				// were bad.
				submitted, err := ring.Submit()
				if err != nil {
					t.Fatalf("Submit: %v", err)
				}
				if submitted != int(c.wantSQReady) {
					t.Errorf("Submit submitted = %d, want %d", submitted, c.wantSQReady)
				}
			})
		})
	}
}

// TestHandleRecvCQE: a received packet goes to the decoders numbered for
// its dump; a recv error or an orphan CQE sends nothing and is counted.
func TestHandleRecvCQE(t *testing.T) {
	done := doneMessage()
	tests := []struct {
		description   string
		res           func(buf *[]byte) xio.Result
		debugLevel    uint32
		counter       string
		expectedSent  int
		expectedN     int
		expectedTotal uint64
	}{
		{
			description:  "negative: a timeout errno sends nothing",
			res:          func(b *[]byte) xio.Result { return xio.Result{Op: xio.OpRead, Res: -int32(syscall.EAGAIN), Buf: b} },
			counter:      "Timeout",
			expectedSent: 0,
		},
		{
			description:  "negative: another errno sends nothing, logged at debug>10",
			res:          func(b *[]byte) xio.Result { return xio.Result{Op: xio.OpRead, Res: -int32(syscall.EINVAL), Buf: b} },
			debugLevel:   11,
			counter:      "RecvErr",
			expectedSent: 0,
		},
		{
			description:  "corner: an error CQE with no buffer sends nothing",
			res:          func(*[]byte) xio.Result { return xio.Result{Op: xio.OpRead, Res: -int32(syscall.EINVAL)} },
			counter:      "RecvErr",
			expectedSent: 0,
		},
		{
			description:  "corner: a success CQE with no buffer is an orphan",
			res:          func(*[]byte) xio.Result { return xio.Result{Op: xio.OpRead, Res: 4} },
			counter:      "OrphanCQE",
			expectedSent: 0,
		},
		{
			description:   "positive: a data packet is sent with its length, its dump still open",
			res:           func(b *[]byte) xio.Result { return xio.Result{Op: xio.OpRead, Res: 4, Buf: b} },
			counter:       "packets",
			expectedSent:  1,
			expectedN:     4,
			expectedTotal: 0,
		},
		{
			description: "boundary: a done-only packet closes its dump at one packet",
			res: func(b *[]byte) xio.Result {
				copy(*b, done)
				return xio.Result{Op: xio.OpRead, Res: int32(len(done)), Buf: b}
			},
			counter:       "packets",
			expectedSent:  1,
			expectedN:     len(done),
			expectedTotal: 1,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			x := newIouringFixture(t)
			x.debugLevel = tc.debugLevel
			b := make([]byte, 64)
			packetCh := make(chan dumpPacket, 1)
			x.handleRecvCQE(3, tc.res(&b), packetCh, newDumpTracker())
			close(packetCh)

			sent := 0
			for p := range packetCh {
				sent++
				if p.n != tc.expectedN {
					t.Errorf("n = %d, expected %d", p.n, tc.expectedN)
				}
				if got := p.dump.total.Load(); got != tc.expectedTotal {
					t.Errorf("dump total = %d, expected %d", got, tc.expectedTotal)
				}
			}
			if sent != tc.expectedSent {
				t.Errorf("sent %d packets, expected %d", sent, tc.expectedSent)
			}
			label := "count"
			if tc.counter == "OrphanCQE" {
				label = "error"
			}
			if got := testutil.ToFloat64(x.pC.WithLabelValues("NetlinkerIoUring", tc.counter, label)); got != 1 {
				t.Errorf("%s = %v, expected 1", tc.counter, got)
			}
		})
	}
}

func TestIouringWaitWithTimeout_etime(t *testing.T) {
	x := newIouringFixture(t)
	withPinnedRing(t, func(ring *xio.Ring) {
		// No SQEs queued, no peer writes → WaitOneTimeout should return
		// an ETIME-like error.
		_, werr := x.iouringWaitWithTimeout(ring, 30*time.Millisecond)
		if werr == nil {
			t.Error("expected timeout error when no CQEs available")
		}
	})
}
