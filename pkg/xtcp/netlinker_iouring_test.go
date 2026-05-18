package xtcp

import (
	"context"
	"errors"
	"sync"
	"syscall"
	"testing"

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
