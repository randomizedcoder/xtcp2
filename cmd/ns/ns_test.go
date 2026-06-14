package main

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestAwaitSignalAndShutdown_completeBeforeTimeout(t *testing.T) {
	sigs := make(chan os.Signal, 1)
	complete := make(chan struct{}, 1)
	_, cancel := context.WithCancel(context.Background())
	var cancelCalled bool
	wrap := func() {
		cancelCalled = true
		cancel()
	}
	done := make(chan struct{})
	go func() {
		awaitSignalAndShutdown(sigs, wrap, complete, 200*time.Millisecond, false)
		close(done)
	}()
	sigs <- syscall.SIGTERM
	// Give cancel() a moment, then signal completion.
	time.Sleep(20 * time.Millisecond)
	complete <- struct{}{}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("awaitSignalAndShutdown did not return after complete")
	}
	if !cancelCalled {
		t.Error("cancel() was not called on signal")
	}
}

func TestAwaitSignalAndShutdown_timeoutPath(t *testing.T) {
	sigs := make(chan os.Signal, 1)
	complete := make(chan struct{}) // never signalled
	_, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		awaitSignalAndShutdown(sigs, cancel, complete, 30*time.Millisecond, false)
		close(done)
	}()
	sigs <- syscall.SIGINT
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout path did not fire")
	}
}

// initPromHandler can't be called directly in-process: it registers on the
// default mux and runs ListenAndServe forever in a goroutine, with log.Fatal
// on bind failure. Tested via the lifecycle microvm harness instead.
