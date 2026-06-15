package main

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// stream_helpers_test.go covers the four helpers extracted from
// stream() in the gocyclo-14 → 9 refactor (classifyRecvErr,
// resourceExhaustedSleep, ctxDone, handleRecvContinueErr) with the
// standard five-category matrix plus race + benchmarks.

// ───────────────────────────────────────────────────────────────────────
// classifyRecvErr
// ───────────────────────────────────────────────────────────────────────

func TestClassifyRecvErr_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		err      error
		want     recvAction
	}{
		{"positive_nil_err_print", "positive", nil, recvPrint},
		{"positive_eof_break", "positive", io.EOF, recvBreak},
		{"negative_generic_err_continue", "negative", errors.New("transient"), recvContinue},
		{"negative_resource_exhausted_continue", "negative", status.Error(codes.ResourceExhausted, "x"), recvContinue},
		{"boundary_unavailable_continue", "boundary", status.Error(codes.Unavailable, "x"), recvContinue},
		{"corner_canceled_continue", "corner", status.Error(codes.Canceled, "x"), recvContinue},
		{"corner_wrapped_eof_NOT_treated_as_eof", "corner",
			// The helper uses `err == io.EOF` (sentinel equality), not
			// errors.Is. A wrapped EOF doesn't compare equal — pin this
			// behaviour against a future shift to errors.Is.
			wrapErr(io.EOF), recvContinue},
		{"adversarial_internal_err_continue", "adversarial", status.Error(codes.Internal, "x"), recvContinue},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			if got := classifyRecvErr(tc.err); got != tc.want {
				t.Errorf("classifyRecvErr(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// ctxDone
// ───────────────────────────────────────────────────────────────────────

func TestCtxDone_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		setup    func(t *testing.T) context.Context
		want     bool
	}{
		{"positive_live_ctx_false", "positive",
			func(_ *testing.T) context.Context { return context.Background() }, false},
		{"positive_cancelled_ctx_true", "positive",
			func(_ *testing.T) context.Context {
				c, cancel := context.WithCancel(context.Background())
				cancel()
				return c
			}, true},
		{"negative_with_value_passthrough", "negative",
			func(_ *testing.T) context.Context {
				return context.WithValue(context.Background(), struct{ k string }{"x"}, 1)
			}, false},
		{"boundary_expired_timeout", "boundary",
			func(t *testing.T) context.Context {
				c, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
				t.Cleanup(cancel)
				time.Sleep(10 * time.Microsecond)
				return c
			}, true},
		{"corner_future_deadline_still_live", "corner",
			func(t *testing.T) context.Context {
				c, cancel := context.WithTimeout(context.Background(), 1*time.Hour)
				t.Cleanup(cancel)
				return c
			}, false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ctxDone(tc.setup(t)); got != tc.want {
				t.Errorf("ctxDone = %v, want %v", got, tc.want)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// resourceExhaustedSleep — verify ctx-cancel short-circuits the wait.
// Full sleep duration isn't asserted because the production constants
// are operator-tunable; we only pin "ctx cancel → return true".
// ───────────────────────────────────────────────────────────────────────

func TestResourceExhaustedSleep_ctxCancelReturnsTrue(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already cancelled
	start := time.Now()
	got := resourceExhaustedSleep(ctx, errors.New("RE"))
	if !got {
		t.Error("cancelled ctx should return true (caller should break loop)")
	}
	// Sanity: returned promptly, not waiting the full ResourceExhaustedSleepTime.
	if time.Since(start) > 1*time.Second {
		t.Errorf("returned after %v on cancelled ctx; should be near-instant", time.Since(start))
	}
}

// (No live-sleep test here: the production sleep is 30-40s and gating
// it behind an env var is anti-pattern per project convention. The
// cancel-path test above + the live-ctx benchmark below are
// sufficient deterministic coverage; the full-sleep behaviour is
// exercised by the production reconnect loop in singleStreamingClient,
// which already has integration coverage in xtcp2client_test.go.)

// ───────────────────────────────────────────────────────────────────────
// handleRecvContinueErr
// ───────────────────────────────────────────────────────────────────────

func TestHandleRecvContinueErr_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		category  string
		ctxBuild  func() context.Context
		err       error
		wantBreak bool
		// Don't assert duration — production constants vary, and the
		// cancel branch returns near-instantly anyway.
	}{
		{
			name:      "positive_ctx_live_non_resource_exhausted_continues",
			category:  "positive",
			ctxBuild:  func() context.Context { return context.Background() },
			err:       errors.New("Unavailable"),
			wantBreak: false,
		},
		{
			name:     "negative_ctx_cancelled_breaks",
			category: "negative",
			ctxBuild: func() context.Context {
				c, cancel := context.WithCancel(context.Background())
				cancel()
				return c
			},
			err:       errors.New("transient"),
			wantBreak: true,
		},
		{
			name:     "corner_resource_exhausted_with_cancelled_ctx_breaks_during_sleep",
			category: "corner",
			ctxBuild: func() context.Context {
				c, cancel := context.WithCancel(context.Background())
				cancel()
				return c
			},
			err:       status.Error(codes.ResourceExhausted, "x"),
			wantBreak: true,
		},
		{
			name:     "boundary_nil_err_doesnt_panic",
			category: "boundary",
			// Caller invariant: handleRecvContinueErr is only entered
			// for the recvContinue case (i.e. err != nil and not EOF).
			// But the function itself should still be robust.
			ctxBuild:  func() context.Context { return context.Background() },
			err:       nil,
			wantBreak: false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			got := handleRecvContinueErr(tc.ctxBuild(), "fake-client", tc.err)
			if got != tc.wantBreak {
				t.Errorf("handleRecvContinueErr = %v, want %v", got, tc.wantBreak)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// Race
// ───────────────────────────────────────────────────────────────────────

func TestStreamHelpers_concurrent(t *testing.T) {
	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	var breaks atomic.Int64
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = classifyRecvErr(io.EOF)
				_ = classifyRecvErr(nil)
				_ = ctxDone(context.Background())
				cancelled, cancel := context.WithCancel(context.Background())
				cancel()
				if handleRecvContinueErr(cancelled, "c", errors.New("x")) {
					breaks.Add(1)
				}
			}
		}()
	}
	wg.Wait()
}

// ───────────────────────────────────────────────────────────────────────
// Benchmarks
// ───────────────────────────────────────────────────────────────────────

func BenchmarkClassifyRecvErr_eof(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = classifyRecvErr(io.EOF)
	}
}

func BenchmarkClassifyRecvErr_nil(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = classifyRecvErr(nil)
	}
}

func BenchmarkCtxDone_live(b *testing.B) {
	b.ReportAllocs()
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = ctxDone(ctx)
	}
}

func BenchmarkHandleRecvContinueErr_continue(b *testing.B) {
	b.ReportAllocs()
	ctx := context.Background()
	err := errors.New("transient")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = handleRecvContinueErr(ctx, "client", err)
	}
}

// helpers ----------------------------------------------------------------

// wrapErr wraps an error so the wrapped value is reachable only via
// errors.Is / errors.As, never via the bare `==` equality test used
// inside classifyRecvErr.
func wrapErr(inner error) error {
	return wrappedErr{inner: inner}
}

type wrappedErr struct{ inner error }

func (w wrappedErr) Error() string { return "wrapped: " + w.inner.Error() }
func (w wrappedErr) Unwrap() error { return w.inner }
