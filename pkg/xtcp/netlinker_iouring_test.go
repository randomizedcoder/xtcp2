package xtcp

import (
	"context"
	"errors"
	"fmt"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	xio "github.com/randomizedcoder/xtcp2/pkg/io_uring"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
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
	x.packetBufferPool.Init(func() *[]byte {
		// Empty slice — EnqueueRecvMsg rejects it.
		b := make([]byte, 0)
		return &b
	})
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

// handleRecvCQE success path (Res>=0): a too-short buffer (4 bytes < 16
// NlMsgHdr minimum) makes Deserialize return ErrParseDeserializeNlMsgHdr
// after the safety check, exercising the errD counter increment + the
// buffer pool put. Without this, the entire success arm of handleRecvCQE
// stayed at 0% in host tests.
func TestHandleRecvCQE_successPathTruncated(t *testing.T) {
	x := newIouringFixture(t)
	// Deserialize needs a usable config, pools, and pH on the args.
	x.config = &xtcp_config.XtcpConfig{Modulus: 1}
	x.xtcpRecordPool.Init(func() *xtcp_flat_record.XtcpFlatRecord { return new(xtcp_flat_record.XtcpFlatRecord) })
	x.nlhPool.Init(func() *xtcpnl.NlMsgHdr { return new(xtcpnl.NlMsgHdr) })
	x.rtaPool.Init(func() *xtcpnl.RTAttr { return new(xtcpnl.RTAttr) })
	reg := prometheus.NewRegistry()
	x.pH = promauto.With(reg).NewSummaryVec(
		prometheus.SummaryOpts{Subsystem: "xtcp_iouring_recv_test",
			Name: promNameHistograms, Help: "test",
			Objectives: map[float64]float64{0.5: quantileError},
			MaxAge:     summaryVecMaxAge},
		promLabels,
	)

	b := make([]byte, 64)
	nsName := "ns"
	// Res=4 → b[:4] is shorter than NlMsgHdrSizeCst → Deserialize
	// returns the truncated-header error → handleRecvCQE counter inc.
	x.handleRecvCQE(context.Background(), nil, &nsName, 7, 0,
		xio.Result{Op: xio.OpRead, Res: 4, Buf: &b})
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
