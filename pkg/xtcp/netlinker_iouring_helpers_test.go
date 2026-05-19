package xtcp

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// netlinker_iouring.go refactor (gocyclo 18 → 8) extracted five helpers
// from the io_uring netlinker body. These tests cover the pure ones
// (iouringResolveBatchSizes / iouringResolveTimeout /
// maybeForceGCIoUring / iouringRecordWaitErr) with the standard
// five-category matrix, plus race + benchmarks. iouringProcessResults
// drives the io_uring ring which needs CAP_SYS_ADMIN; covered by the
// microvm-lifecycle integration tests, not here.

// ───────────────────────────────────────────────────────────────────────
// iouringResolveBatchSizes
// ───────────────────────────────────────────────────────────────────────

func TestIouringResolveBatchSizes_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		recvIn   uint32
		cqeIn    uint32
		wantRecv int
		wantCqe  int
	}{
		{"positive_both_explicit", "positive", 32, 64, 32, 64},
		{"positive_large_explicit", "positive", 8192, 16384, 8192, 16384},
		{"negative_both_zero_use_defaults", "negative", 0, 0, iouringRecvBatchDefaultCst, iouringCqeBatchDefaultCst},
		{"negative_only_recv_zero", "negative", 0, 200, iouringRecvBatchDefaultCst, 200},
		{"negative_only_cqe_zero", "negative", 8, 0, 8, iouringCqeBatchDefaultCst},
		{"boundary_recv_one_cqe_one", "boundary", 1, 1, 1, 1},
		{"boundary_max_uint32", "boundary", ^uint32(0), ^uint32(0), int(^uint32(0)), int(^uint32(0))},
		{"corner_recv_one_cqe_default", "corner", 1, 0, 1, iouringCqeBatchDefaultCst},
		{"adversarial_giant_recv", "adversarial", 1 << 30, 1 << 30, 1 << 30, 1 << 30},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			gotRecv, gotCqe := iouringResolveBatchSizes(tc.recvIn, tc.cqeIn)
			if gotRecv != tc.wantRecv {
				t.Errorf("recv = %d, want %d", gotRecv, tc.wantRecv)
			}
			if gotCqe != tc.wantCqe {
				t.Errorf("cqe = %d, want %d", gotCqe, tc.wantCqe)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// iouringResolveTimeout
// ───────────────────────────────────────────────────────────────────────

func TestIouringResolveTimeout_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		input    uint64
		want     time.Duration
	}{
		{"positive_one_ms", "positive", 1, 1 * time.Millisecond},
		{"positive_one_sec", "positive", 1000, 1 * time.Second},
		{"positive_minute", "positive", 60_000, 60 * time.Second},
		{"negative_zero_returns_default", "negative", 0, iouringTimeoutDefaultCst},
		{"boundary_one_ms_minimum", "boundary", 1, 1 * time.Millisecond},
		{"boundary_default_seconds_value", "boundary", uint64(iouringTimeoutDefaultCst / time.Millisecond), iouringTimeoutDefaultCst},
		{"corner_huge_value_no_overflow_panic", "corner", 1 << 30, (1 << 30) * time.Millisecond},
		{"adversarial_billion_ms", "adversarial", 1_000_000_000, 1_000_000_000 * time.Millisecond},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			if got := iouringResolveTimeout(tc.input); got != tc.want {
				t.Errorf("iouringResolveTimeout(%d) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// maybeForceGCIoUring
// ───────────────────────────────────────────────────────────────────────

func TestMaybeForceGCIoUring_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		packets  uint64
		wantGC   float64
	}{
		{"positive_modulus_hit", "positive", forceGCModulesCst, 1},
		{"positive_double_modulus", "positive", forceGCModulesCst * 2, 1},
		{"negative_packet_zero_no_gc", "negative", 0, 0},
		{"negative_off_modulus", "negative", forceGCModulesCst + 1, 0},
		{"boundary_one_less", "boundary", forceGCModulesCst - 1, 0},
		{"boundary_one_more", "boundary", forceGCModulesCst + 1, 0},
		{"corner_max_uint64_off_mod", "corner", ^uint64(0), 0},
		{"adversarial_huge_on_mod", "adversarial", forceGCModulesCst * 1_000_000, 1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			x := newTestXTCP(t, "null:")
			before := testutil.ToFloat64(
				x.pC.WithLabelValues("NetlinkerIoUring", "runtime.GC()", "count"))
			x.maybeForceGCIoUring(tc.packets)
			after := testutil.ToFloat64(
				x.pC.WithLabelValues("NetlinkerIoUring", "runtime.GC()", "count"))
			if diff := after - before; diff != tc.wantGC {
				t.Errorf("GC counter delta = %v, want %v (packets=%d)", diff, tc.wantGC, tc.packets)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// iouringRecordWaitErr
// ───────────────────────────────────────────────────────────────────────

func TestIouringRecordWaitErr_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		category     string
		err          error
		wantTimeout  float64
		wantWaitErr  float64
	}{
		{"positive_etime_increments_timeout", "positive", syscall.ETIME, 1, 0},
		{"positive_wrapped_etime", "positive", fmt.Errorf("wait failed: %w", syscall.ETIME), 1, 0},
		{"negative_non_etime_increments_waiterr", "negative", errors.New("other err"), 0, 1},
		{"negative_eperm_is_waiterr", "negative", syscall.EPERM, 0, 1},
		{"boundary_wrapped_string_errno_62", "boundary", errors.New("errno 62"), 1, 0},
		{"corner_eintr_is_waiterr", "corner", syscall.EINTR, 0, 1},
		{"adversarial_deeply_wrapped_etime", "adversarial",
			fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", syscall.ETIME)), 1, 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			x := newTestXTCP(t, "null:")
			x.debugLevel = 0
			x.iouringRecordWaitErr(7, tc.err)
			gotTimeout := testutil.ToFloat64(
				x.pC.WithLabelValues("NetlinkerIoUring", "Timeout", "count"))
			gotWaitErr := testutil.ToFloat64(
				x.pC.WithLabelValues("NetlinkerIoUring", "WaitErr", "count"))
			if gotTimeout != tc.wantTimeout {
				t.Errorf("Timeout counter = %v, want %v", gotTimeout, tc.wantTimeout)
			}
			if gotWaitErr != tc.wantWaitErr {
				t.Errorf("WaitErr counter = %v, want %v", gotWaitErr, tc.wantWaitErr)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// isETimeError — already had ad-hoc coverage; pin the cases that
// matter for iouringRecordWaitErr's dispatch.
// ───────────────────────────────────────────────────────────────────────

func TestIsETimeError_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil_returns_false", nil, false},
		{"direct_etime", syscall.ETIME, true},
		{"wrapped_etime", fmt.Errorf("x: %w", syscall.ETIME), true},
		{"different_errno", syscall.EINTR, false},
		{"plain_string_match_errno_62", errors.New("errno 62"), true},
		{"plain_string_other", errors.New("something else"), false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isETimeError(tc.err); got != tc.want {
				t.Errorf("isETimeError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// Race — drive the pure helpers + iouringRecordWaitErr concurrently.
// Each goroutine gets its own XTCP fixture (counter state isolation).
// ───────────────────────────────────────────────────────────────────────

func TestIouringHelpers_concurrent(t *testing.T) {
	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	var totalErrs atomic.Int64
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			x := newTestXTCP(t, "null:")
			x.debugLevel = 0
			for j := 0; j < 200; j++ {
				_, _ = iouringResolveBatchSizes(uint32(j), uint32(j*2))
				_ = iouringResolveTimeout(uint64(j))
				x.maybeForceGCIoUring(uint64(i*j) * uint64(forceGCModulesCst))
				if j%3 == 0 {
					x.iouringRecordWaitErr(uint32(i), syscall.ETIME)
				} else {
					x.iouringRecordWaitErr(uint32(i), errors.New("nope"))
					totalErrs.Add(1)
				}
			}
		}()
	}
	wg.Wait()
}

// ───────────────────────────────────────────────────────────────────────
// Benchmarks
// ───────────────────────────────────────────────────────────────────────

func BenchmarkIouringResolveBatchSizes_defaults(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = iouringResolveBatchSizes(0, 0)
	}
}

func BenchmarkIouringResolveTimeout_default(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = iouringResolveTimeout(0)
	}
}

func BenchmarkIouringResolveTimeout_explicit(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = iouringResolveTimeout(500)
	}
}

func BenchmarkMaybeForceGCIoUring_skip(b *testing.B) {
	b.ReportAllocs()
	x := newTestXTCP(&testing.T{}, "null:")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x.maybeForceGCIoUring(uint64(i)) // most iterations skip the modulus
	}
}

func BenchmarkIouringRecordWaitErr_etime(b *testing.B) {
	b.ReportAllocs()
	x := newTestXTCP(&testing.T{}, "null:")
	x.debugLevel = 0
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x.iouringRecordWaitErr(0, syscall.ETIME)
	}
}
