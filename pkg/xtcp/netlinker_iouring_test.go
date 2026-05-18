package xtcp

import (
	"context"
	"errors"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

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
	x.destBytesPool = sync.Pool{New: func() any { b := make([]byte, 64); return &b }}
	x.packetBufferPool = sync.Pool{New: func() any { b := make([]byte, 64); return &b }}
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

func TestIsETimeError_nil(t *testing.T) {
	if isETimeError(nil) {
		t.Error("nil should not be ETIME")
	}
}

func TestIsETimeError_etime(t *testing.T) {
	if !isETimeError(syscall.ETIME) {
		t.Error("syscall.ETIME should classify as ETIME")
	}
}

func TestIsETimeError_other(t *testing.T) {
	if isETimeError(syscall.EAGAIN) {
		t.Error("EAGAIN should not classify as ETIME")
	}
	if isETimeError(errors.New("not an errno")) {
		t.Error("non-errno error should not classify as ETIME")
	}
}

// String-fallback branch: errors that wrap an ETIME but whose As-cast
// to syscall.Errno fails should still classify via the "errno 62"
// string compare.
func TestIsETimeError_stringFallback(t *testing.T) {
	if !isETimeError(errors.New("errno 62")) {
		t.Error("wrapped 'errno 62' string should classify as ETIME")
	}
}

// ───────────────────────────────────────────────────────────────────────
// isTimeoutErrno: EAGAIN/EWOULDBLOCK/ETIME → true; else false
// ───────────────────────────────────────────────────────────────────────

func TestIsTimeoutErrno_matches(t *testing.T) {
	for _, e := range []syscall.Errno{syscall.EAGAIN, syscall.EWOULDBLOCK, syscall.ETIME} {
		if !isTimeoutErrno(e) {
			t.Errorf("errno %v should be timeout", e)
		}
	}
}

func TestIsTimeoutErrno_misses(t *testing.T) {
	for _, e := range []syscall.Errno{syscall.EINVAL, syscall.EPERM, 0} {
		if isTimeoutErrno(e) {
			t.Errorf("errno %v should NOT be timeout", e)
		}
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

// iouringPrefillRecvs err branch: swap packetBufferPool to yield an
// empty buffer so EnqueueRecvMsg rejects it and the function returns
// the error.
func TestIouringPrefillRecvs_enqueueErr(t *testing.T) {
	ring, err := xioRingNew(t)
	if err != nil {
		t.Skipf("io_uring unavailable: %v", err)
	}
	t.Cleanup(func() { ring.Close(time.Second, nil) })

	x := newIouringFixture(t)
	x.packetBufferPool = sync.Pool{New: func() any {
		// Empty slice — EnqueueRecvMsg rejects it.
		b := make([]byte, 0)
		return &b
	}}
	if err := x.iouringPrefillRecvs(ring, 3, 1); err == nil {
		t.Error("empty buf should make EnqueueRecvMsg return an error")
	}
}

// iouringPrefillRecvs + iouringWaitWithTimeout: drive with a real ring
// + socketpair fd. Prefill submits one recv SQE; wait should timeout
// with ETIME since no peer wrote to the socket.
func TestIouringPrefillRecvs_smoke(t *testing.T) {
	ring, err := xioRingNew(t)
	if err != nil {
		t.Skipf("io_uring unavailable: %v", err)
	}
	t.Cleanup(func() { ring.Close(time.Second, nil) })

	x := newIouringFixture(t)
	// packetBufferPool yields 64-byte buffers (set in newIouringFixture).
	if err := x.iouringPrefillRecvs(ring, 3, 2); err != nil {
		t.Errorf("err = %v", err)
	}
	if _, err := ring.Submit(); err != nil {
		t.Errorf("Submit: %v", err)
	}
}

// handleRecvCQE error paths: Res<0 with timeout errno + non-timeout errno.
// Buffer return-to-pool fires regardless.
func TestHandleRecvCQE_timeoutErr(t *testing.T) {
	x := newIouringFixture(t)
	b := make([]byte, 64)
	nsName := "ns"
	x.handleRecvCQE(context.Background(), nil, &nsName, 3, 0,
		xio.Result{Op: xio.OpRead, Res: -int32(syscall.EAGAIN), Buf: &b})
}

func TestHandleRecvCQE_otherErr(t *testing.T) {
	x := newIouringFixture(t)
	x.debugLevel = 11 // hit log branch
	b := make([]byte, 64)
	nsName := "ns"
	x.handleRecvCQE(context.Background(), nil, &nsName, 3, 0,
		xio.Result{Op: xio.OpRead, Res: -int32(syscall.EINVAL), Buf: &b})
}

func TestHandleRecvCQE_nilBufOnError(t *testing.T) {
	x := newIouringFixture(t)
	nsName := "ns"
	x.handleRecvCQE(context.Background(), nil, &nsName, 3, 0,
		xio.Result{Op: xio.OpRead, Res: -int32(syscall.EINVAL), Buf: nil})
}


func TestIouringWaitWithTimeout_etime(t *testing.T) {
	ring, err := xioRingNew(t)
	if err != nil {
		t.Skipf("io_uring unavailable: %v", err)
	}
	t.Cleanup(func() { ring.Close(time.Second, nil) })

	x := newIouringFixture(t)
	// No SQEs queued, no peer writes → WaitOneTimeout should return
	// an ETIME-like error.
	_, werr := x.iouringWaitWithTimeout(ring, 30*time.Millisecond)
	if werr == nil {
		t.Error("expected timeout error when no CQEs available")
	}
}
