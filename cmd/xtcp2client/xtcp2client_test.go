package main

import (
	"testing"

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

func TestFastRand(t *testing.T) {
	// Smoke test: just verify it doesn't panic. Different calls should generally
	// differ (very high probability over 100 iterations), but we don't assert
	// that to avoid flakes.
	for range 10 {
		_ = FastRand()
	}
}
