package io_uring

import (
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

// ring_close_helpers_test.go covers the three helpers extracted from
// Ring.Close in the gocyclo-15 → 7 refactor (waitForNextDrainCQE +
// dispatchDrainResults + dispatchAbandonedInFlight). Existing
// ring_test.go covers the full Close path against a real io_uring;
// these tests exercise the units in isolation, including paths that
// don't need a real kernel ring.

// ───────────────────────────────────────────────────────────────────────
// dispatchDrainResults — pure function; trivial to drive.
// ───────────────────────────────────────────────────────────────────────

func TestDispatchDrainResults_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		results  []Result
		nilCb    bool
		wantCalls int
	}{
		{"positive_two_results_two_calls", "positive",
			[]Result{{Op: OpRead}, {Op: OpSendUDP}}, false, 2},
		{"positive_one_result_one_call", "positive",
			[]Result{{Op: OpRead}}, false, 1},
		{"negative_nil_callback_skips_all", "negative",
			[]Result{{Op: OpRead}, {Op: OpRead}}, true, 0},
		{"boundary_empty_results_no_calls", "boundary",
			nil, false, 0},
		{"boundary_nil_callback_empty_results", "boundary",
			nil, true, 0},
		{"corner_results_with_nil_buf", "corner",
			[]Result{{Op: OpRead, Buf: nil}}, false, 1},
		{"adversarial_thousand_results", "adversarial",
			makeResults(1000), false, 1000},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			calls := 0
			var cb func(Result)
			if !tc.nilCb {
				cb = func(_ Result) { calls++ }
			}
			dispatchDrainResults(tc.results, cb)
			if calls != tc.wantCalls {
				t.Errorf("calls = %d, want %d", calls, tc.wantCalls)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// dispatchAbandonedInFlight — method on *Ring, but only touches
// r.inFlight; we can construct a bare *Ring with seeded map.
// ───────────────────────────────────────────────────────────────────────

func TestDispatchAbandonedInFlight_table(t *testing.T) {
	t.Parallel()
	mkBuf := func(s string) *[]byte { b := []byte(s); return &b }
	cases := []struct {
		name      string
		category  string
		seed      map[uint32]inFlight
		nilCb     bool
		wantCalls int
		wantClear bool
	}{
		{
			name:     "positive_three_entries_all_dispatched_and_cleared",
			category: "positive",
			seed: map[uint32]inFlight{
				1: {op: OpRead, buf: mkBuf("a")},
				2: {op: OpSendUDP, buf: mkBuf("b")},
				3: {op: OpSendUnixGram, buf: mkBuf("c")},
			},
			wantCalls: 3, wantClear: true,
		},
		{
			name:      "negative_nil_callback_still_clears_map",
			category:  "negative",
			seed:      map[uint32]inFlight{42: {op: OpRead}},
			nilCb:     true,
			wantCalls: 0, wantClear: true,
		},
		{
			name:      "boundary_empty_inflight",
			category:  "boundary",
			seed:      map[uint32]inFlight{},
			wantCalls: 0, wantClear: true,
		},
		{
			name:      "corner_entry_with_nil_buf",
			category:  "corner",
			seed:      map[uint32]inFlight{7: {op: OpRead, buf: nil}},
			wantCalls: 1, wantClear: true,
		},
		{
			name:      "adversarial_thousand_entries",
			category:  "adversarial",
			seed:      makeInFlight(1000),
			wantCalls: 1000, wantClear: true,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			r := &Ring{inFlight: copyInFlightMap(tc.seed)}
			calls := 0
			var sawSynthETIME int
			var cb func(Result)
			if !tc.nilCb {
				cb = func(res Result) {
					calls++
					if res.Res == -int32(syscall.ETIME) {
						sawSynthETIME++
					}
				}
			}
			r.dispatchAbandonedInFlight(cb)
			if calls != tc.wantCalls {
				t.Errorf("calls = %d, want %d", calls, tc.wantCalls)
			}
			if tc.wantClear && len(r.inFlight) != 0 {
				t.Errorf("inFlight len = %d, want 0 (must be cleared)", len(r.inFlight))
			}
			// When callback fires, every Res must be synthetic -ETIME so
			// production callers can distinguish "abandoned at teardown"
			// from "real CQE with bytes".
			if !tc.nilCb && tc.wantCalls > 0 && sawSynthETIME != tc.wantCalls {
				t.Errorf("synthetic -ETIME Res = %d, want %d", sawSynthETIME, tc.wantCalls)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// waitForNextDrainCQE — the only deterministically testable branch
// without a real io_uring is "deadline already past" → drainAbort.
// ───────────────────────────────────────────────────────────────────────

func TestWaitForNextDrainCQE_pastDeadlineAborts(t *testing.T) {
	r := &Ring{} // r.r is nil; we deliberately do NOT touch it
	results, outcome := r.waitForNextDrainCQE(time.Now().Add(-1 * time.Second))
	if results != nil {
		t.Errorf("results = %v, want nil", results)
	}
	if outcome != drainAbort {
		t.Errorf("outcome = %v, want drainAbort", outcome)
	}
}

func TestWaitForNextDrainCQE_zeroDeadlineAborts(t *testing.T) {
	r := &Ring{}
	_, outcome := r.waitForNextDrainCQE(time.Time{})
	if outcome != drainAbort {
		t.Errorf("outcome = %v, want drainAbort", outcome)
	}
}

// ───────────────────────────────────────────────────────────────────────
// Race — each goroutine drives its own Ring fixture.
// ───────────────────────────────────────────────────────────────────────

func TestRingCloseHelpers_concurrent(t *testing.T) {
	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	var totalCalls atomic.Int64
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				// Pure helper — no shared state.
				dispatchDrainResults([]Result{{Op: OpRead}, {Op: OpSendUDP}},
					func(_ Result) { totalCalls.Add(1) })

				// Per-goroutine ring fixture.
				r := &Ring{inFlight: makeInFlight(5)}
				r.dispatchAbandonedInFlight(func(_ Result) { totalCalls.Add(1) })

				// Past-deadline branch — touches no shared state.
				r2 := &Ring{}
				_, _ = r2.waitForNextDrainCQE(time.Now().Add(-1 * time.Millisecond))
			}
		}()
	}
	wg.Wait()
}

// ───────────────────────────────────────────────────────────────────────
// Benchmarks
// ───────────────────────────────────────────────────────────────────────

func BenchmarkDispatchDrainResults_ten(b *testing.B) {
	b.ReportAllocs()
	res := makeResults(10)
	cb := func(_ Result) {}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dispatchDrainResults(res, cb)
	}
}

func BenchmarkDispatchAbandonedInFlight_hundred(b *testing.B) {
	b.ReportAllocs()
	cb := func(_ Result) {}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		r := &Ring{inFlight: makeInFlight(100)}
		r.dispatchAbandonedInFlight(cb)
	}
}

func BenchmarkWaitForNextDrainCQE_pastDeadline(b *testing.B) {
	b.ReportAllocs()
	r := &Ring{}
	past := time.Now().Add(-1 * time.Second)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = r.waitForNextDrainCQE(past)
	}
}

// helpers ----------------------------------------------------------------

func makeResults(n int) []Result {
	out := make([]Result, n)
	for i := range out {
		out[i] = Result{Op: OpRead, Res: int32(i)}
	}
	return out
}

func makeInFlight(n int) map[uint32]inFlight {
	out := make(map[uint32]inFlight, n)
	for i := 0; i < n; i++ {
		buf := []byte{byte(i)}
		out[uint32(i+1)] = inFlight{op: OpRead, buf: &buf}
	}
	return out
}

// copyInFlightMap returns a defensive copy so per-row test mutation
// doesn't leak across t.Parallel siblings.
func copyInFlightMap(src map[uint32]inFlight) map[uint32]inFlight {
	out := make(map[uint32]inFlight, len(src))
	for k, v := range src {
		out[k] = v
	}
	return out
}
