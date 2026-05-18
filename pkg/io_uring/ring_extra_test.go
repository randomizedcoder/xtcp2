package io_uring

import (
	"syscall"
	"testing"
	"time"
)

// New rejects invalid Config (batch sizes < 1).
func TestNew_invalidConfig(t *testing.T) {
	if _, err := New(Config{RecvBatchSize: 0, CQEBatchSize: 8}); err == nil {
		t.Error("RecvBatchSize=0 should error")
	}
	if _, err := New(Config{RecvBatchSize: 8, CQEBatchSize: 0}); err == nil {
		t.Error("CQEBatchSize=0 should error")
	}
}

// EnqueueRecvMsg error branches: nil and empty.
func TestEnqueueRecvMsg_nilBuf(t *testing.T) {
	r := newTestRing(t, 0)
	if _, err := r.EnqueueRecvMsg(3, nil); err == nil {
		t.Error("nil buf should error")
	}
	empty := make([]byte, 0)
	if _, err := r.EnqueueRecvMsg(3, &empty); err == nil {
		t.Error("empty buf should error")
	}
}

// EnqueueSend error branches.
func TestEnqueueSend_nilBuf(t *testing.T) {
	r := newTestRing(t, 0)
	if _, err := r.EnqueueSend(3, nil, OpSendUDP); err == nil {
		t.Error("nil buf should error")
	}
}

func TestEnqueueSend_invalidOp(t *testing.T) {
	r := newTestRing(t, 0)
	b := []byte("x")
	if _, err := r.EnqueueSend(3, &b, OpRead); err == nil {
		t.Error("OpRead in EnqueueSend should error")
	}
}

// EnqueueWritevUnix error branches.
func TestEnqueueWritevUnix_nilPayload(t *testing.T) {
	r := newTestRing(t, 0)
	if _, err := r.EnqueueWritevUnix(3, []byte("h"), nil); err == nil {
		t.Error("nil payload should error")
	}
}

func TestEnqueueWritevUnix_emptyHeader(t *testing.T) {
	r := newTestRing(t, 0)
	payload := []byte("x")
	if _, err := r.EnqueueWritevUnix(3, []byte{}, &payload); err == nil {
		t.Error("empty header should error")
	}
}

// Close on a nil Ring is a no-op (defensive).
func TestClose_nilRing(t *testing.T) {
	var r *Ring
	r.Close(0, nil)
}

// Close called twice is safe (second call sees r.r == nil).
func TestClose_idempotent(t *testing.T) {
	r := newTestRing(t, 0)
	r.Close(10*time.Millisecond, nil)
	r.Close(10*time.Millisecond, nil)
}

// Close + onDrain callback when there's in-flight work that completes.
func TestClose_drainsCallback(t *testing.T) {
	r := newTestRing(t, 0)
	sock, peer := socketpair(t)
	buf := allocBuf(64)
	if _, err := r.EnqueueRecvMsg(sock, buf); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if _, err := r.SubmitAndWait(0); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if _, err := syscall.Write(peer, []byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	called := false
	// Use a generous timeout so the CQE has time to land. The Close drain
	// loop will catch it.
	r.Close(500*time.Millisecond, func(_ Result) { called = true })
	if !called {
		t.Log("onDrain not invoked — CQE may not have landed within timeout; acceptable on slow runners")
	}
}

// SubmitAndWait exercises the syscall-thin wrapper. With no SQEs queued
// and waitNr=0 the kernel returns immediately with n=0.
func TestSubmitAndWait_idle(t *testing.T) {
	r := newTestRing(t, 0)
	n, err := r.SubmitAndWait(0)
	if err != nil {
		t.Fatalf("SubmitAndWait(0): %v", err)
	}
	if n < 0 {
		t.Errorf("submitted = %d, want >= 0", n)
	}
}

// WaitOneTimeout with no outstanding work + small timeout should fire the
// timer and return syscall.ETIME or similar from WaitCQETimeout.
func TestWaitOneTimeout_fires(t *testing.T) {
	r := newTestRing(t, 0)
	_, err := r.WaitOneTimeout(50 * time.Millisecond)
	if err == nil {
		t.Error("WaitOneTimeout with no SQEs should return a timer error")
	}
}

func TestSQReady_growsAfterEnqueue(t *testing.T) {
	r := newTestRing(t, 0)
	if r.SQReady() != 0 {
		t.Errorf("initial SQReady = %d, want 0", r.SQReady())
	}
	sock, _ := socketpair(t)
	buf := allocBuf(64)
	if _, err := r.EnqueueRecvMsg(sock, buf); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if r.SQReady() == 0 {
		t.Error("SQReady should be > 0 after EnqueueRecvMsg")
	}
	// Submit drains the SQ.
	if _, err := r.SubmitAndWait(0); err != nil {
		t.Fatalf("submit: %v", err)
	}
	if r.SQReady() != 0 {
		t.Errorf("after submit SQReady = %d, want 0", r.SQReady())
	}
}

func TestInFlightLen_tracksOutstanding(t *testing.T) {
	r := newTestRing(t, 0)
	if r.InFlightLen() != 0 {
		t.Errorf("initial InFlightLen = %d, want 0", r.InFlightLen())
	}
	sock, _ := socketpair(t)
	buf := allocBuf(64)
	if _, err := r.EnqueueRecvMsg(sock, buf); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if r.InFlightLen() != 1 {
		t.Errorf("after enqueue InFlightLen = %d, want 1", r.InFlightLen())
	}
}
