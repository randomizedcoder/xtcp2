// Package fetch downloads feed bodies over HTTP with retries and full-jitter
// exponential backoff. The jitter and sleep are injectable so tests are
// deterministic and never actually sleep. It mirrors the backoff discipline
// used elsewhere in xtcp2 (crypto/rand full-jitter, context-aware sleep).
package fetch

import (
	"context"
	crand "crypto/rand"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"time"
)

// Result is the outcome of a successful (or not-modified) fetch.
type Result struct {
	URL          string
	Status       int
	Body         []byte
	NotModified  bool // server returned 304 for a conditional request
	Attempts     int
	ETag         string
	LastModified string
}

// Options configures a Client. Zero values are replaced with sensible defaults
// by NewClient, and the Jitter/Sleep seams default to crypto/rand + real time.
type Options struct {
	MaxAttempts int           // total attempts (>=1); default 10
	BackoffBase time.Duration // base window; default 1s
	BackoffCap  time.Duration // max window; default 1h
	Timeout     time.Duration // per-request timeout; default 30s
	UserAgent   string

	// Jitter returns a duration uniformly in [0, max). Injectable for tests.
	Jitter func(max time.Duration) time.Duration
	// Sleep waits d or until ctx is done; returns true if it slept fully.
	Sleep func(ctx context.Context, d time.Duration) bool
}

// Client performs retrying HTTP GETs. It reuses one http.Client for keep-alive.
type Client struct {
	hc   *http.Client
	opts Options
}

// NewClient builds a Client, applying defaults to any zero Options fields.
func NewClient(opts Options) *Client {
	if opts.MaxAttempts < 1 {
		opts.MaxAttempts = 10
	}
	if opts.BackoffBase <= 0 {
		opts.BackoffBase = time.Second
	}
	if opts.BackoffCap <= 0 {
		opts.BackoffCap = time.Hour
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Second
	}
	if opts.UserAgent == "" {
		opts.UserAgent = "ipfeed-collector/1"
	}
	if opts.Jitter == nil {
		opts.Jitter = cryptoJitter
	}
	if opts.Sleep == nil {
		opts.Sleep = SleepCtx
	}
	return &Client{
		hc:   &http.Client{Timeout: opts.Timeout},
		opts: opts,
	}
}

// Conditional carries optional cache validators for a conditional request.
type Conditional struct {
	ETag         string
	LastModified string
}

// Get fetches url with retries. A 2xx returns the body; a 304 (only possible
// when cond is set) returns NotModified. Non-retryable 4xx (other than 429)
// fail immediately; 5xx, 429, and transport errors are retried with backoff.
func (c *Client) Get(ctx context.Context, url string, cond Conditional) (Result, error) {
	var lastErr error
	var lastRes Result
	for attempt := 1; attempt <= c.opts.MaxAttempts; attempt++ {
		res, retryable, err := c.attempt(ctx, url, cond)
		res.Attempts = attempt
		lastRes = res
		if err == nil {
			return res, nil
		}
		lastErr = err
		if !retryable || attempt == c.opts.MaxAttempts {
			break
		}
		// Full-jitter: window grows exponentially, clamped to cap; the actual
		// wait is drawn uniformly in [0, window].
		window := c.backoffWindow(attempt)
		if !c.opts.Sleep(ctx, c.opts.Jitter(window)) {
			return lastRes, ctx.Err()
		}
	}
	return lastRes, fmt.Errorf("fetch %s: %w", url, lastErr)
}

// backoffWindow returns base<<(attempt-1) clamped to cap, guarding overflow.
func (c *Client) backoffWindow(attempt int) time.Duration {
	w := c.opts.BackoffBase
	for i := 1; i < attempt; i++ {
		w <<= 1
		if w <= 0 || w >= c.opts.BackoffCap {
			return c.opts.BackoffCap
		}
	}
	if w > c.opts.BackoffCap {
		w = c.opts.BackoffCap
	}
	return w
}

// attempt performs a single request. The bool reports whether a failure is
// worth retrying.
func (c *Client) attempt(ctx context.Context, url string, cond Conditional) (Result, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Result{URL: url}, false, err // malformed URL: not retryable
	}
	req.Header.Set("User-Agent", c.opts.UserAgent)
	if cond.ETag != "" {
		req.Header.Set("If-None-Match", cond.ETag)
	}
	if cond.LastModified != "" {
		req.Header.Set("If-Modified-Since", cond.LastModified)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		// Context cancellation is terminal; transport errors are retryable.
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Result{URL: url}, false, err
		}
		return Result{URL: url}, true, err
	}
	defer resp.Body.Close()

	res := Result{
		URL:          url,
		Status:       resp.StatusCode,
		ETag:         resp.Header.Get("ETag"),
		LastModified: resp.Header.Get("Last-Modified"),
	}

	switch {
	case resp.StatusCode == http.StatusNotModified:
		// #nosec G104 -- best-effort drain to enable connection reuse; body content is unused
		io.Copy(io.Discard, resp.Body) //nolint:errcheck,gosec // best-effort drain to enable connection reuse
		res.NotModified = true
		return res, false, nil
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return res, true, err // truncated read: retry
		}
		res.Body = body
		return res, false, nil
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500:
		// #nosec G104 -- best-effort drain to enable connection reuse; body content is unused
		io.Copy(io.Discard, resp.Body) //nolint:errcheck,gosec // best-effort drain to enable connection reuse
		return res, true, fmt.Errorf("http status %d", resp.StatusCode)
	default:
		// #nosec G104 -- best-effort drain to enable connection reuse; body content is unused
		io.Copy(io.Discard, resp.Body) //nolint:errcheck,gosec // best-effort drain to enable connection reuse
		return res, false, fmt.Errorf("http status %d", resp.StatusCode)
	}
}

// cryptoJitter returns a uniform duration in [0, limit) using crypto/rand.
func cryptoJitter(limit time.Duration) time.Duration {
	if limit <= 0 {
		return 0
	}
	n, err := crand.Int(crand.Reader, big.NewInt(int64(limit)))
	if err != nil {
		return limit / 2 // extremely unlikely; degrade to a fixed mid wait
	}
	return time.Duration(n.Int64())
}

// SleepCtx sleeps for d or until ctx is done. It returns true if the full
// duration elapsed, false if ctx was canceled first.
func SleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return ctx.Err() == nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return true
	case <-ctx.Done():
		return false
	}
}
