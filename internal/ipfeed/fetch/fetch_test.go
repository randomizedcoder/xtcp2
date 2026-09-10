package fetch

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// newSeqServer returns a server that replies with the given status codes in
// order (repeating the last one after the slice is exhausted), writing the
// body "ok" for any 2xx.
func newSeqServer(statuses ...int) (*httptest.Server, *int) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st := statuses[min(calls, len(statuses)-1)]
		calls++
		w.WriteHeader(st)
		if st >= 200 && st < 300 {
			_, _ = w.Write([]byte("ok"))
		}
	}))
	return srv, &calls
}

// testClient builds a Client with deterministic (no-op) jitter/sleep seams so
// retries are exercised without real waiting.
func testClient(maxAttempts int, sleep func(context.Context, time.Duration) bool) *Client {
	if sleep == nil {
		sleep = func(context.Context, time.Duration) bool { return true }
	}
	return NewClient(Options{
		MaxAttempts: maxAttempts,
		Jitter:      func(time.Duration) time.Duration { return 0 },
		Sleep:       sleep,
	})
}

func TestGet(t *testing.T) {
	tests := []struct {
		name         string
		desc         string
		class        string
		statuses     []int
		maxAttempts  int
		wantErr      bool
		wantAttempts int
		wantBody     string
	}{
		{"positive_ok", "positive: a 200 on the first try returns the body", "positive",
			[]int{200}, 3, false, 1, "ok"},
		{"corner_500_then_200", "corner: a 500 is retried and the following 200 succeeds", "corner",
			[]int{500, 200}, 3, false, 2, "ok"},
		{"corner_429_then_200", "corner: 429 Too Many Requests is retryable", "corner",
			[]int{429, 200}, 3, false, 2, "ok"},
		{"negative_always_500", "negative: persistent 5xx fails after exhausting attempts", "negative",
			[]int{500}, 3, true, 3, ""},
		{"boundary_404_no_retry", "boundary: a non-retryable 404 fails immediately on attempt 1", "boundary",
			[]int{404}, 3, true, 1, ""},
		{"boundary_single_attempt", "boundary: max-attempts=1 makes one attempt only", "boundary",
			[]int{500}, 1, true, 1, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv, calls := newSeqServer(tc.statuses...)
			defer srv.Close()
			c := testClient(tc.maxAttempts, nil)

			res, err := c.Get(context.Background(), srv.URL, Conditional{})
			if tc.wantErr && err == nil {
				t.Fatalf("%s: expected error, got nil", tc.desc)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.desc, err)
			}
			if res.Attempts != tc.wantAttempts {
				t.Errorf("%s: attempts = %d, want %d", tc.desc, res.Attempts, tc.wantAttempts)
			}
			if *calls != tc.wantAttempts {
				t.Errorf("%s: server calls = %d, want %d", tc.desc, *calls, tc.wantAttempts)
			}
			if !tc.wantErr && string(res.Body) != tc.wantBody {
				t.Errorf("%s: body = %q, want %q", tc.desc, res.Body, tc.wantBody)
			}
		})
	}
}

// TestGetContextCancelDuringBackoff verifies that a canceled context during
// the backoff wait aborts with the context error rather than retrying.
func TestGetContextCancelDuringBackoff(t *testing.T) {
	srv, _ := newSeqServer(500)
	defer srv.Close()
	ctx, cancel := context.WithCancel(context.Background())
	// Sleep seam cancels the context and reports "did not complete".
	c := testClient(5, func(context.Context, time.Duration) bool {
		cancel()
		return false
	})
	_, err := c.Get(ctx, srv.URL, Conditional{})
	if err != context.Canceled {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestBackoffWindow(t *testing.T) {
	c := NewClient(Options{BackoffBase: time.Second, BackoffCap: 8 * time.Second})
	tests := []struct {
		name    string
		desc    string
		class   string
		attempt int
		want    time.Duration
	}{
		{"positive_first", "positive: attempt 1 window equals base", "positive", 1, time.Second},
		{"positive_grow", "positive: attempt 3 window is base<<2", "positive", 3, 4 * time.Second},
		{"boundary_at_cap", "boundary: attempt 4 window is exactly the cap", "boundary", 4, 8 * time.Second},
		{"corner_beyond_cap", "corner: a large attempt is clamped to the cap", "corner", 20, 8 * time.Second},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.backoffWindow(tc.attempt); got != tc.want {
				t.Errorf("%s: backoffWindow(%d) = %v, want %v", tc.desc, tc.attempt, got, tc.want)
			}
		})
	}
}
