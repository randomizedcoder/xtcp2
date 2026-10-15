package xtcp

import (
	"bytes"
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

// onRingClosedResult: only send buffers go back to a pool
// ───────────────────────────────────────────────────────────────────────

func TestOnRingClosedResult(t *testing.T) {
	tests := []struct {
		description string
		res         func(buf *[]byte) xio.Result
		expectedPut bool // buf goes back to destBytesPool
	}{
		{description: "negative: no buffer puts nothing", res: func(*[]byte) xio.Result { return xio.Result{Op: xio.OpSendUDP} }},
		{description: "positive: a send buffer goes back to destBytesPool", res: func(b *[]byte) xio.Result { return xio.Result{Op: xio.OpSendUDP, Buf: b} }, expectedPut: true},
		{description: "corner: a recv CQE's ring data is not a pool buffer", res: func(b *[]byte) xio.Result { return xio.Result{Op: xio.OpRecvMultishot, Res: 4, Data: (*b)[:4]} }},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			x := newIouringFixture(t)
			b := make([]byte, 64)
			if put := x.onRingClosedResult(tc.res(&b)); put != tc.expectedPut {
				t.Errorf("put = %v, expected %v", put, tc.expectedPut)
			}
		})
	}
}

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
	withPinnedRingConfig(t, xio.Config{RecvBatchSize: 4, CQEBatchSize: 16}, fn)
}

// withPinnedRingConfig is withPinnedRing for a ring built from cfg.
func withPinnedRingConfig(t *testing.T, cfg xio.Config, fn func(ring *xio.Ring)) {
	t.Helper()

	runtime.LockOSThread()
	defer runtime.UnlockOSThread() //nolint:forbidigo // safe: io_uring test pins to one thread so the SINGLE_ISSUER ring keeps one owning task; no netns mutation.

	ring, err := xio.New(cfg)
	if err != nil {
		skipNoIoUring(t, err)
		return
	}
	defer ring.Close(time.Second, nil)

	fn(ring)
}

// TestIouringArmRecv arms the multishot recv against a real ring and a
// real socketpair fd.
func TestIouringArmRecv(t *testing.T) {
	cases := []struct {
		description string
		recvBufSize int // 0: a ring with no buffer ring
		closeFD     bool
		wantErr     bool
		wantSQReady uint32 // SQEs pending after the arm, before Submit
	}{
		{
			description: "positive: a live socketpair fd is armed and submits cleanly",
			recvBufSize: 64,
			wantSQReady: 1,
		},
		{
			description: "negative: a ring with no buffer ring can't arm, and counts it",
			wantErr:     true,
			wantSQReady: 0,
		},
		{
			description: "corner: a closed fd still arms — io_uring validates the fd at completion, not at prep",
			recvBufSize: 64,
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
			cfg := xio.Config{RecvBatchSize: 4, CQEBatchSize: 16, RecvBufSize: c.recvBufSize}
			withPinnedRingConfig(t, cfg, func(ring *xio.Ring) {
				err := x.iouringArmRecv(ring, readFD)
				if gotErr := err != nil; gotErr != c.wantErr {
					t.Fatalf("iouringArmRecv err = %v, wantErr %v", err, c.wantErr)
				}
				wantArmErrs := 0.0
				if c.wantErr {
					wantArmErrs = 1
				}
				if got := testutil.ToFloat64(x.pC.WithLabelValues("NetlinkerIoUring", "Arm", "error")); got != wantArmErrs {
					t.Errorf("Arm errors = %v, want %v", got, wantArmErrs)
				}
				if got := ring.SQReady(); got != c.wantSQReady {
					t.Errorf("SQReady = %d, want %d", got, c.wantSQReady)
				}
				// Submit must always succeed: a failure here means the
				// ring lost its owning task (EEXIST), not that the SQE
				// was bad.
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

// TestHandleRecvCQE: a received packet is copied into a pool buffer and
// goes to the decoders numbered for its dump; a recv error or a CQE with
// no data sends nothing and is counted.
func TestHandleRecvCQE(t *testing.T) {
	done := doneMessage()
	recv := func(data []byte) xio.Result {
		return xio.Result{Op: xio.OpRecvMultishot, Res: int32(len(data)), Data: data, More: true}
	}
	errno := func(e syscall.Errno) xio.Result {
		return xio.Result{Op: xio.OpRecvMultishot, Res: -int32(e)}
	}
	tests := []struct {
		description   string
		res           xio.Result
		debugLevel    uint32
		counter       string
		expectedSent  int
		expectedN     int
		expectedTotal uint64
	}{
		{
			description:  "negative: a timeout errno sends nothing",
			res:          errno(syscall.EAGAIN),
			counter:      "Timeout",
			expectedSent: 0,
		},
		{
			description:  "negative: another errno sends nothing, logged at debug>10",
			res:          errno(syscall.EINVAL),
			debugLevel:   11,
			counter:      "RecvErr",
			expectedSent: 0,
		},
		{
			description:  "negative: ENOBUFS (every ring buffer in use) is counted apart from errors",
			res:          errno(syscall.ENOBUFS),
			counter:      "NoBufs",
			expectedSent: 0,
		},
		{
			description:  "corner: a success CQE with no data is an orphan",
			res:          xio.Result{Op: xio.OpRecvMultishot, Res: 0},
			counter:      "OrphanCQE",
			expectedSent: 0,
		},
		{
			description:   "positive: a data packet is sent with its length, its dump still open",
			res:           recv(make([]byte, 4)),
			counter:       "packets",
			expectedSent:  1,
			expectedN:     4,
			expectedTotal: 0,
		},
		{
			description:   "boundary: a done-only packet closes its dump at one packet",
			res:           recv(done),
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
			packetCh := make(chan dumpPacket, 1)
			x.handleRecvCQE(3, tc.res, packetCh, newDumpTracker())
			close(packetCh)

			sent := 0
			for p := range packetCh {
				sent++
				if p.n != tc.expectedN {
					t.Errorf("n = %d, expected %d", p.n, tc.expectedN)
				}
				if !bytes.Equal((*p.buf)[:p.n], tc.res.Data) {
					t.Errorf("packet = %x, expected a copy of %x", (*p.buf)[:p.n], tc.res.Data)
				}
				if len(tc.res.Data) > 0 && &(*p.buf)[0] == &tc.res.Data[0] {
					t.Error("packet aliases the ring buffer, expected a copy")
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
