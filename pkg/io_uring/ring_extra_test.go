package io_uring

import (
	"testing"
	"time"
)

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
