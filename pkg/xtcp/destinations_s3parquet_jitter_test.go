//go:build dest_s3parquet

package xtcp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
)

// ─── Pure helpers ────────────────────────────────────────────────────────

func TestResolveFlushInterval(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
		freq     time.Duration
		want     time.Duration
	}{
		{"positive explicit interval", 10 * time.Minute, time.Hour, 10 * time.Minute},
		{"corner 0 → derive, freq>floor", 0, 2 * time.Hour, 2 * time.Hour},
		{"boundary 0 → derive, freq<floor → floor", 0, time.Minute, s3FlushIntervalFloorCst},
		{"corner 0 → derive, freq==0 → floor", 0, 0, s3FlushIntervalFloorCst},
		{"boundary freq==floor → floor (not >)", 0, s3FlushIntervalFloorCst, s3FlushIntervalFloorCst},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &xtcp_config.XtcpConfig{
				S3FlushInterval: durationpb.New(tt.interval),
				PollFrequency:   durationpb.New(tt.freq),
			}
			if got := resolveFlushInterval(c); got != tt.want {
				t.Fatalf("resolveFlushInterval = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestResolveBackoffCap(t *testing.T) {
	tests := []struct {
		name string
		cap  time.Duration
		freq time.Duration
		want time.Duration
	}{
		{"positive explicit cap", 30 * time.Second, time.Hour, 30 * time.Second},
		{"corner 0 → freq/10", 0, 10 * time.Minute, time.Minute},
		{"boundary 0 → clamp to min", 0, time.Second, s3UploadBackoffCapMinCst},
		{"boundary 0 → clamp to max", 0, 24 * time.Hour, s3UploadBackoffCapMaxCst},
		{"corner 0 & freq 0 → min", 0, 0, s3UploadBackoffCapMinCst},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &xtcp_config.XtcpConfig{
				S3UploadBackoffCap: durationpb.New(tt.cap),
				PollFrequency:      durationpb.New(tt.freq),
			}
			if got := resolveBackoffCap(c); got != tt.want {
				t.Fatalf("resolveBackoffCap = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBackoffWindow(t *testing.T) {
	const base = time.Second
	const capD = 8 * time.Second
	tests := []struct {
		name    string
		attempt int
		want    time.Duration
	}{
		{"positive attempt1 → base", 1, time.Second},
		{"positive attempt2 → 2x", 2, 2 * time.Second},
		{"positive attempt3 → 4x", 3, 4 * time.Second},
		{"boundary attempt4 → cap", 4, 8 * time.Second},
		{"boundary attempt5 → cap (clamped)", 5, 8 * time.Second},
		{"corner large attempt → cap (overflow-safe)", 99, 8 * time.Second},
		{"corner attempt<1 → base", 0, time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := backoffWindow(base, capD, tt.attempt); got != tt.want {
				t.Fatalf("backoffWindow(%v,%v,%d) = %v, want %v", base, capD, tt.attempt, got, tt.want)
			}
		})
	}
}

func TestJitteredThreshold(t *testing.T) {
	const threshold = 1000
	tests := []struct {
		name      string
		pct       uint32
		jitterInt func(int) int
		want      int
	}{
		{"corner pct==0 → exact threshold", 0, func(n int) int { return n }, 1000},
		{"positive 20% max draw → low end", 20, func(n int) int { return n }, 800},   // 1000 - 200
		{"positive 20% zero draw → threshold", 20, func(int) int { return 0 }, 1000}, // downward-only
		{"boundary 100% max draw → 0", 100, func(n int) int { return n }, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jitteredThreshold(threshold, tt.pct, tt.jitterInt)
			if got != tt.want {
				t.Fatalf("jitteredThreshold = %d, want %d", got, tt.want)
			}
			if got > threshold {
				t.Fatalf("jitteredThreshold %d exceeds threshold %d (must be downward-only)", got, threshold)
			}
		})
	}
}

func TestNextFlushDelay(t *testing.T) {
	tests := []struct {
		name     string
		interval time.Duration
		pct      uint32
		jitter   func(time.Duration) time.Duration
		want     time.Duration
	}{
		{"positive 20% identity (high)", 10 * time.Minute, 20, func(d time.Duration) time.Duration { return d }, 11 * time.Minute},
		{"positive 20% zero (low)", 10 * time.Minute, 20, func(time.Duration) time.Duration { return 0 }, 9 * time.Minute},
		{"corner pct==0 → interval", 10 * time.Minute, 0, func(d time.Duration) time.Duration { return d }, 10 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nextFlushDelay(tt.interval, tt.pct, tt.jitter); got != tt.want {
				t.Fatalf("nextFlushDelay = %v, want %v", got, tt.want)
			}
		})
	}
}

// ─── Worker-level: time-based staleness flush ────────────────────────────

func TestS3ParquetDest_timeFlush(t *testing.T) {
	// Byte cap never reached (huge threshold); firing the injected flush timer
	// must upload the accumulated rows WITHOUT a Close.
	flushCh := make(chan time.Time, 1)
	d, upl, x := newS3ParquetFixtureCustom(t, 1<<30, nil, func(d *s3ParquetDest) {
		d.newTimer = func(time.Duration) (<-chan time.Time, func() bool) {
			return flushCh, func() bool { return true }
		}
	})

	buf := marshalEnvelopeBuf(t, x, mkEnvelope(5))
	if _, err := d.Send(context.Background(), buf); err != nil {
		t.Fatalf("Send err: %v", err)
	}
	// Let the worker consume the item (append rows; no byte-cap finalize).
	time.Sleep(30 * time.Millisecond)
	if got := len(upl.Calls()); got != 0 {
		t.Fatalf("upload before timer fire = %d, want 0", got)
	}

	flushCh <- time.Now() // fire the staleness ceiling
	time.Sleep(30 * time.Millisecond)
	if got := len(upl.Calls()); got != 1 {
		t.Fatalf("upload after timer fire = %d, want 1", got)
	}

	if err := d.Close(); err != nil {
		t.Fatalf("Close err: %v", err)
	}
}

// ─── Worker-level: full-jitter exponential backoff ───────────────────────

func TestS3ParquetDest_backoffWindows(t *testing.T) {
	// Always-fail upload with maxAttempts=5 and cap=8s. With jitterDur as the
	// identity, the sleep records the exact backoff windows; there are
	// maxAttempts-1 sleeps (the last attempt doesn't sleep before dropping).
	var mu sync.Mutex
	var sleeps []time.Duration
	d, _, x := newS3ParquetFixtureCustom(t, 1<<30,
		func(int) error { return errors.New("always fail") },
		func(d *s3ParquetDest) {
			d.maxAttempts = 5
			d.backoffCap = 8 * time.Second
			d.jitterDur = func(w time.Duration) time.Duration { return w } // identity → record window
			d.sleep = func(_ context.Context, dur time.Duration) bool {
				mu.Lock()
				sleeps = append(sleeps, dur)
				mu.Unlock()
				return true
			}
		})

	buf := marshalEnvelopeBuf(t, x, mkEnvelope(3))
	if _, err := d.Send(context.Background(), buf); err != nil {
		t.Fatalf("Send err: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close err: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}
	if len(sleeps) != len(want) {
		t.Fatalf("recorded %d backoff windows %v, want %d %v", len(sleeps), sleeps, len(want), want)
	}
	for i := range want {
		if sleeps[i] != want[i] {
			t.Fatalf("backoff window[%d] = %v, want %v (full seq %v)", i, sleeps[i], want[i], sleeps)
		}
	}
	// Terminal failure bumps the upload/error counter.
	if v := promCounterValue(t, x, "destS3Parquet", "upload", "error"); v != 1 {
		t.Fatalf("upload/error counter = %v, want 1", v)
	}
}

// Backoff aborts promptly when the sleep seam reports ctx cancellation.
func TestS3ParquetDest_backoffCtxCancel(t *testing.T) {
	var attempts int
	var mu sync.Mutex
	d, _, x := newS3ParquetFixtureCustom(t, 1<<30,
		func(int) error {
			mu.Lock()
			attempts++
			mu.Unlock()
			return errors.New("fail")
		},
		func(d *s3ParquetDest) {
			d.maxAttempts = 10
			d.sleep = func(context.Context, time.Duration) bool { return false } // simulate ctx cancel
		})

	buf := marshalEnvelopeBuf(t, x, mkEnvelope(3))
	if _, err := d.Send(context.Background(), buf); err != nil {
		t.Fatalf("Send err: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close err: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	// First attempt fails → sleep returns false → return before a 2nd attempt.
	if attempts != 1 {
		t.Fatalf("upload attempts = %d, want 1 (aborted on ctx cancel)", attempts)
	}
}
