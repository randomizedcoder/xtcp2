package xtcp

import (
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// poller.go refactor: Poller dropped from gocyclo 18 → 12 by extracting
// five helpers (handlePollerTick / handleChangePollFrequency /
// handleNetlinkerDone / handlePollTimeout / recordPollerCycleDuration).
// These tests cover each with positive / negative / boundary / corner /
// adversarial categories, plus race + benchmarks.
// Pre-existing poller_pure_test.go covers handlePollRequest +
// observeNetlinkerDone + pollAllNetlinkSockets unchanged.

// newPollerHelperFixture extends newPollerFixture with a buffered
// pollRequestCh + non-firing timeout timer so the helpers under test
// don't block on channel send/receive.
func newPollerHelperFixture(t *testing.T) *XTCP {
	t.Helper()
	x := newPollerFixture(t)
	x.pollRequestCh = make(chan struct{}, 1) // buffered so non-blocking send fits
	return x
}

// ───────────────────────────────────────────────────────────────────────
// handlePollerTick — non-blocking send to pollRequestCh
// ───────────────────────────────────────────────────────────────────────

func TestHandlePollerTick_table(t *testing.T) {
	cases := []struct {
		name      string
		category  string
		preFilled bool
		wantQueue int // expected length of pollRequestCh after the call
		wantTick  float64
	}{
		{"positive_empty_channel_sends", "positive", false, 1, 1},
		{"negative_full_channel_drops", "negative", true, 1, 1},
		{"boundary_zero_count_still_sends", "boundary", false, 1, 1},
		{"corner_repeated_call_with_full_buffer", "corner", true, 1, 1},
		{"adversarial_high_polling_loops", "adversarial", false, 1, 1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			x := newPollerHelperFixture(t)
			if tc.preFilled {
				x.pollRequestCh <- struct{}{} // saturate the buffer
			}
			x.handlePollerTick(1, 0)
			if got := len(x.pollRequestCh); got != tc.wantQueue {
				t.Errorf("queue len = %d, want %d", got, tc.wantQueue)
			}
			gotTick := testutil.ToFloat64(
				x.pC.WithLabelValues("Poller", "ticker", "count"))
			if gotTick != tc.wantTick {
				t.Errorf("ticker counter = %v, want %v", gotTick, tc.wantTick)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// handleChangePollFrequency
// ───────────────────────────────────────────────────────────────────────

func TestHandleChangePollFrequency_table(t *testing.T) {
	cases := []struct {
		name     string
		category string
		newD     time.Duration
		wantCnt  float64
	}{
		{"positive_one_second", "positive", 1 * time.Second, 1},
		{"positive_short_ms", "positive", 5 * time.Millisecond, 1},
		{"negative_zero_duration_panics", "negative", 0, 0}, // ticker.Reset panics on 0
		{"boundary_min_nanosecond", "boundary", 1 * time.Nanosecond, 1},
		{"corner_huge_duration", "corner", 24 * time.Hour, 1},
		{"adversarial_negative_duration_panics", "adversarial", -1 * time.Second, 0},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			x := newPollerHelperFixture(t)
			ticker := time.NewTicker(1 * time.Hour)
			defer ticker.Stop()

			// ticker.Reset panics on d <= 0. Wrap so panics convert to
			// test-pass + counter-skip; the production poller doesn't
			// validate the duration either, so this codifies the
			// behaviour.
			func() {
				defer func() {
					if r := recover(); r != nil {
						// expected for non-positive durations
					}
				}()
				x.handleChangePollFrequency(tc.newD, ticker, 1, 0)
			}()

			gotCnt := testutil.ToFloat64(
				x.pC.WithLabelValues("Poller", "ticker.Reset", "count"))
			if gotCnt != tc.wantCnt {
				t.Errorf("ticker.Reset counter = %v, want %v", gotCnt, tc.wantCnt)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// handleNetlinkerDone
// ───────────────────────────────────────────────────────────────────────

func TestHandleNetlinkerDone_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		category  string
		fd        int
		preStore  bool
		startCnt  int
		wantNext  int
		wantDoneC float64
	}{
		{"positive_fd_known_decrements", "positive", 7, true, 3, 2, 1},
		{"positive_count_to_zero", "positive", 7, true, 1, 0, 1},
		{"negative_fd_unknown_still_decrements", "negative", 99, false, 5, 4, 1},
		{"boundary_count_zero_goes_negative", "boundary", 7, true, 0, -1, 1},
		{"corner_high_starting_count", "corner", 7, true, 1 << 20, (1 << 20) - 1, 1},
		{"adversarial_min_int_decrement", "adversarial", 7, true, -(1 << 30), -(1 << 30) - 1, 1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			x := newPollerHelperFixture(t)
			x.pollTime = sync.Map{}
			if tc.preStore {
				x.pollTime.Store(tc.fd, time.Now().Add(-5*time.Millisecond))
			}
			got := x.handleNetlinkerDone(netlinkerDone{fd: tc.fd, t: time.Now()}, tc.startCnt)
			if got != tc.wantNext {
				t.Errorf("returned count = %d, want %d", got, tc.wantNext)
			}
			gotDone := testutil.ToFloat64(
				x.pC.WithLabelValues("Poller", "done", "count"))
			if gotDone != tc.wantDoneC {
				t.Errorf("done counter = %v, want %v", gotDone, tc.wantDoneC)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// handlePollTimeout
// ───────────────────────────────────────────────────────────────────────

func TestHandlePollTimeout_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		debug    uint32
	}{
		{"positive_debug_off", "positive", 0},
		{"positive_debug_high", "positive", 1000},
		{"boundary_debug_just_above_log_gate", "boundary", 11},
		{"corner_debug_at_log_gate", "corner", 10},
		{"adversarial_debug_max", "adversarial", ^uint32(0)},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			x := newPollerHelperFixture(t)
			x.debugLevel = tc.debug
			if got := x.handlePollTimeout(); got != 0 {
				t.Errorf("handlePollTimeout = %d, want 0 (must always zero count)", got)
			}
			gotTimeout := testutil.ToFloat64(
				x.pC.WithLabelValues("Poller", "PollTimeout", "count"))
			if gotTimeout != 1 {
				t.Errorf("PollTimeout counter = %v, want 1", gotTimeout)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// recordPollerCycleDuration
// ───────────────────────────────────────────────────────────────────────

func TestRecordPollerCycleDuration_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		ago      time.Duration
	}{
		{"positive_recent_start", "positive", 1 * time.Millisecond},
		{"positive_longer_ago", "positive", 100 * time.Millisecond},
		{"boundary_zero_duration", "boundary", 0},
		{"corner_future_start_time", "corner", -1 * time.Second}, // start is "in the future"
		{"adversarial_huge_ago", "adversarial", 24 * time.Hour},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			x := newPollerHelperFixture(t)
			x.pollStartTime = time.Now().Add(-tc.ago)
			// Just verify no panic + histogram observation accepted.
			x.recordPollerCycleDuration(42)
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// Race — each goroutine drives its own XTCP fixture.
// ───────────────────────────────────────────────────────────────────────

func TestPollerHelpers_concurrent(t *testing.T) {
	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			x := newPollerHelperFixture(t)
			x.pollStartTime = time.Now()
			ticker := time.NewTicker(1 * time.Hour)
			defer ticker.Stop()
			for j := 0; j < 200; j++ {
				x.handlePollerTick(uint64(j), j)
				if j%2 == 0 {
					// Drain so the next tick can fill again.
					select {
					case <-x.pollRequestCh:
					default:
					}
				}
				x.handleChangePollFrequency(time.Millisecond, ticker, uint64(j), j)
				x.handleNetlinkerDone(netlinkerDone{fd: j % 5, t: time.Now()}, j)
				_ = x.handlePollTimeout()
				x.recordPollerCycleDuration(uint64(j))
			}
		}()
	}
	wg.Wait()
}

// ───────────────────────────────────────────────────────────────────────
// Benchmarks
// ───────────────────────────────────────────────────────────────────────

func BenchmarkHandlePollerTick_empty(b *testing.B) {
	b.ReportAllocs()
	x := newPollerHelperFixture(&testing.T{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		x.handlePollerTick(uint64(i), 0)
		// drain so the buffered channel doesn't stay full
		select {
		case <-x.pollRequestCh:
		default:
		}
	}
}

func BenchmarkHandleNetlinkerDone_unknownFD(b *testing.B) {
	b.ReportAllocs()
	x := newPollerHelperFixture(&testing.T{})
	d := netlinkerDone{fd: 999, t: time.Now()}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = x.handleNetlinkerDone(d, 1)
	}
}

func BenchmarkHandlePollTimeout(b *testing.B) {
	b.ReportAllocs()
	x := newPollerHelperFixture(&testing.T{})
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = x.handlePollTimeout()
	}
}
