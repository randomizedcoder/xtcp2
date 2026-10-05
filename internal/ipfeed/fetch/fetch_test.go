package fetch

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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

// TestGetBodyLimit covers the response-body size cap. The package-level
// maxBodyBytes is lowered for the test (and restored via t.Cleanup) so the
// boundary can be exercised with a few KiB rather than 256 MiB over httptest.
func TestGetBodyLimit(t *testing.T) {
	const limit = 4096
	prev := maxBodyBytes
	maxBodyBytes = limit
	t.Cleanup(func() { maxBodyBytes = prev })

	tests := []struct {
		description   string
		bodySize      int
		maxAttempts   int
		expectErr     bool
		expectTooBig  bool // errors.Is(err, ErrBodyTooLarge)
		expectBodyLen int  // only checked when !expectErr
		expectCalls   int  // server hits: an over-limit body must not be retried
	}{
		// positive
		{description: "positive: a small body under the limit is returned whole",
			bodySize: 10, maxAttempts: 3, expectErr: false, expectBodyLen: 10, expectCalls: 1},
		{description: "positive: a body well under the limit is returned whole",
			bodySize: limit / 2, maxAttempts: 3, expectErr: false, expectBodyLen: limit / 2, expectCalls: 1},
		// negative
		{description: "negative: a body far over the limit fails with ErrBodyTooLarge",
			bodySize: limit * 4, maxAttempts: 3, expectErr: true, expectTooBig: true, expectCalls: 1},
		// boundary
		{description: "boundary: a body exactly at the limit is accepted",
			bodySize: limit, maxAttempts: 3, expectErr: false, expectBodyLen: limit, expectCalls: 1},
		{description: "boundary: a body one byte over the limit is rejected",
			bodySize: limit + 1, maxAttempts: 3, expectErr: true, expectTooBig: true, expectCalls: 1},
		{description: "boundary: an empty 200 body is accepted (zero bytes)",
			bodySize: 0, maxAttempts: 3, expectErr: false, expectBodyLen: 0, expectCalls: 1},
		// corner
		{description: "corner: an over-limit body is not retried even with attempts remaining",
			bodySize: limit + 1, maxAttempts: 5, expectErr: true, expectTooBig: true, expectCalls: 1},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			calls := 0
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write(bytes.Repeat([]byte{'x'}, tc.bodySize))
			}))
			defer srv.Close()
			c := testClient(tc.maxAttempts, nil)

			res, err := c.Get(context.Background(), srv.URL, Conditional{})
			if tc.expectErr && err == nil {
				t.Fatalf("expected error, got nil (body len %d)", len(res.Body))
			}
			if !tc.expectErr && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if errors.Is(err, ErrBodyTooLarge) != tc.expectTooBig {
				t.Errorf("errors.Is(err, ErrBodyTooLarge) = %v, want %v (err=%v)", !tc.expectTooBig, tc.expectTooBig, err)
			}
			if !tc.expectErr && len(res.Body) != tc.expectBodyLen {
				t.Errorf("body len = %d, want %d", len(res.Body), tc.expectBodyLen)
			}
			if tc.expectErr && res.Body != nil {
				t.Errorf("body should not be retained on error, got %d bytes", len(res.Body))
			}
			if calls != tc.expectCalls {
				t.Errorf("server calls = %d, want %d", calls, tc.expectCalls)
			}
		})
	}
}

func TestGetFileURL(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "feed.json")
	if err := os.WriteFile(path, []byte(`{"ok":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	escapedPath := filepath.Join(dir, "feed with space.json")
	if err := os.WriteFile(escapedPath, []byte(`{"escaped":true}`), 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		description string
		rawurl      string
		wantErr     bool
		wantBody    string
	}{
		{"positive: file URL returns the local file body", "file://" + path, false, `{"ok":true}`},
		{"negative: missing file returns an error", "file://" + filepath.Join(dir, "missing.json"), true, ""},
		{"negative: file URL with a host is rejected", "file://example.com/feed.json", true, ""},
		{"corner: escaped path characters are decoded", "file://" + (&url.URL{Path: escapedPath}).EscapedPath(), false, `{"escaped":true}`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			res, err := testClient(3, nil).Get(context.Background(), tc.rawurl, Conditional{})
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if res.Attempts != 1 {
				t.Fatalf("attempts = %d, want 1", res.Attempts)
			}
			if res.Status != http.StatusOK {
				t.Fatalf("status = %d, want 200", res.Status)
			}
			if string(res.Body) != tc.wantBody {
				t.Fatalf("body = %q, want %q", res.Body, tc.wantBody)
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
