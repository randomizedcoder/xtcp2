package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
)

func TestNewGRPCClient(t *testing.T) {
	// newGRPCClient builds a grpc.ClientConn without dialing (lazy connect).
	conn := newGRPCClient("localhost:0")
	if conn == nil {
		t.Fatal("newGRPCClient returned nil")
	}
	_ = conn.Close() //nolint:errcheck // test plumbing
}

func TestPrintFlatRecordsResponse_silent(t *testing.T) {
	// debugLevel 0 → no log output, just the early-return path.
	resp := &xtcp_flat_record.FlatRecordsResponse{
		XtcpFlatRecord: &xtcp_flat_record.XtcpFlatRecord{Hostname: "h1"},
	}
	printFlatRecordsResponse(resp, 1, false, 0)
	printFlatRecordsResponse(resp, 1, true, 0)
}

func TestPrintFlatRecordsResponse_verbose(t *testing.T) {
	resp := &xtcp_flat_record.FlatRecordsResponse{
		XtcpFlatRecord: &xtcp_flat_record.XtcpFlatRecord{Hostname: "h2"},
	}
	// debugLevel > 10 → both proto.Marshal branch AND the per-format printing.
	printFlatRecordsResponse(resp, 7, false, 11)
	printFlatRecordsResponse(resp, 7, true, 11) // json branch
}

func TestPrintPollFlatRecordsResponse_silent(t *testing.T) {
	resp := &xtcp_flat_record.PollFlatRecordsResponse{
		XtcpFlatRecord: &xtcp_flat_record.XtcpFlatRecord{Hostname: "p1"},
	}
	printPollFlatRecordsResponse(resp, 1, false, 0)
	printPollFlatRecordsResponse(resp, 1, true, 0)
}

func TestPrintPollFlatRecordsResponse_verbose(t *testing.T) {
	resp := &xtcp_flat_record.PollFlatRecordsResponse{
		XtcpFlatRecord: &xtcp_flat_record.XtcpFlatRecord{Hostname: "p2"},
	}
	printPollFlatRecordsResponse(resp, 7, false, 11)
	printPollFlatRecordsResponse(resp, 7, true, 11)
}

func TestFastRandN(t *testing.T) {
	// Smoke test: runtime linkname FastRandN should return a value in [0, n).
	for range 10 {
		v := FastRandN(100)
		if v >= 100 {
			t.Errorf("FastRandN(100) = %d, want < 100", v)
		}
	}
}

func TestRunMain_version(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if rc := runMain(t.Context(), []string{"-v"}, &stdout, &stderr); rc != 0 {
		t.Errorf("rc = %d, want 0", rc)
	}
	if !strings.Contains(stdout.String(), "xtcp commit:") {
		t.Errorf("stdout = %q, want xtcp commit: prefix", stdout.String())
	}
}

func TestRunMain_invalidFlag(t *testing.T) {
	if rc := runMain(t.Context(), []string{"-not-a-flag"}, &bytes.Buffer{}, &bytes.Buffer{}); rc != 2 {
		t.Errorf("rc = %d, want 2", rc)
	}
}

func TestRunMain_listenModeCancellable(t *testing.T) {
	// listenMode dials gRPC against the default target then spawns workers
	// that loop until ctx is cancelled. workers=0 makes wg.Wait return
	// immediately without any active streams.
	ctx, cancel := context.WithCancel(t.Context())
	cancel() // listenMode's loop will still fan out workers; with workers=0 wg.Wait is a no-op.
	done := make(chan int, 1)
	go func() {
		done <- runMain(ctx, []string{"-workers", "0"}, &bytes.Buffer{}, &bytes.Buffer{})
	}()
	select {
	case rc := <-done:
		if rc != 0 {
			t.Errorf("rc = %d, want 0", rc)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runMain did not exit on cancel + workers=0")
	}
}

func TestFastRand(t *testing.T) {
	// Smoke test: just verify it doesn't panic. Different calls should generally
	// differ (very high probability over 100 iterations), but we don't assert
	// that to avoid flakes.
	for range 10 {
		_ = FastRand()
	}
}
