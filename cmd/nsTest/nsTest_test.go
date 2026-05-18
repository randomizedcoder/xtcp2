package main

import (
	"bytes"
	"context"
	"testing"
	"time"
)

// namespaceName composes the canonical "<base><index>" name.
func TestNamespaceName(t *testing.T) {
	cases := []struct {
		idx  int
		want string
	}{
		{0, "ns0"},
		{1, "ns1"},
		{999, "ns999"},
		{initialNamespaces - 1, "ns999"},
	}
	for _, tc := range cases {
		if got := namespaceName(tc.idx); got != tc.want {
			t.Errorf("namespaceName(%d) = %q, want %q", tc.idx, got, tc.want)
		}
	}
}

// createNamespace / removeNamespace shell out to `ip netns`; without
// CAP_NET_ADMIN they fail but should not panic and should log the
// error. We can at least exercise the call path on a known-bad name
// so the exec branch runs.
func TestCreateNamespace_logsError(t *testing.T) {
	// Use a name with characters that ip netns rejects so the call
	// fails fast without requiring privileges.
	createNamespace("test/invalid/name/with/slashes")
	// No panic = pass; we don't introspect log output.
}

func TestRemoveNamespace_logsError(t *testing.T) {
	removeNamespace("test/invalid/name/with/slashes")
}

func TestRunMain_invalidFlag(t *testing.T) {
	if rc := runMain(t.Context(), []string{"-not-a-flag"}, &bytes.Buffer{}); rc != 2 {
		t.Errorf("rc = %d, want 2", rc)
	}
}

func TestRunMain_cancelDuringInitial(t *testing.T) {
	// Pre-cancelled ctx + initial=5: the initial-fill loop checks
	// ctx.Err() at the top of each iter and exits without calling
	// createNamespace 5 times — verifying the cancel hook fires.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	rc := runMain(ctx, []string{"-initial", "5", "-sleep", "1ms"}, &bytes.Buffer{})
	if rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
}

func TestRunMain_churnExitsOnCancel(t *testing.T) {
	// initial=0 → fill loop is a no-op; churn loop runs once before
	// cancel fires, then exits on the select's <-ctx.Done() branch.
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan int, 1)
	go func() {
		done <- runMain(ctx, []string{"-initial", "0", "-sleep", "10ms"}, &bytes.Buffer{})
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case rc := <-done:
		if rc != 0 {
			t.Errorf("rc = %d, want 0", rc)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runMain did not exit on cancel")
	}
}

func TestChurn_cancelImmediate(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if rc := churn(ctx, 0, time.Hour); rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
}
