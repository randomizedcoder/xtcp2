package main

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestSIGHUPReconcilesWithoutRebaselineOrShutdown(t *testing.T) {
	sigs := make(chan os.Signal, 3)
	complete := make(chan struct{}, 1)
	complete <- struct{}{}
	reconciled := make(chan struct{}, 1)
	rebaselined := make(chan struct{}, 1)
	done := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		awaitSignalAndShutdown(sigs, cancel, complete, time.Second, false, func() { rebaselined <- struct{}{} }, func() { reconciled <- struct{}{} })
		close(done)
	}()
	sigs <- syscall.SIGHUP
	select {
	case <-reconciled:
	case <-time.After(time.Second):
		t.Fatal("SIGHUP did not reconcile")
	}
	select {
	case <-ctx.Done():
		t.Fatal("SIGHUP shut down daemon")
	case <-rebaselined:
		t.Fatal("SIGHUP changed link baseline")
	default:
	}
	sigs <- syscall.SIGUSR1
	select {
	case <-rebaselined:
	case <-time.After(time.Second):
		t.Fatal("SIGUSR1 regression")
	}
	sigs <- syscall.SIGTERM
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("SIGTERM did not stop")
	}
}
