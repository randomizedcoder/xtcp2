package misc

import (
	"context"
	"math/rand/v2"
	"time"
)

// Timing helpers for fleet-wide jitter and context-aware sleeping. See
// docs/design-jitter-and-backoff.md for the why: on a large fleet, deterministic
// timers synchronize (thundering herd), so poll scheduling, S3 flushing, and
// upload retry backoff all draw jitter from these helpers.
//
// Randomness comes from math/rand/v2's top-level functions, which are seeded
// from a per-process random source and are safe for concurrent use. Per-process
// seeding is exactly what the threat model needs: even a fleet of identically
// configured processes started at the same instant draws independent jitter.

// JitterDuration returns a uniform random duration in [0, limit). A
// non-positive limit returns 0, so callers can pass a "disabled" (zero) window
// without a guard.
func JitterDuration(limit time.Duration) time.Duration {
	if limit <= 0 {
		return 0
	}
	return time.Duration(rand.Int64N(int64(limit)))
}

// JitterIntN returns a uniform random int in [0, limit). A non-positive limit
// returns 0. Used for the per-object S3 byte-threshold jitter.
func JitterIntN(limit int) int {
	if limit <= 0 {
		return 0
	}
	return rand.IntN(limit)
}

// SleepCtx sleeps for d, or until ctx is done, whichever comes first. It
// returns true if it slept the full duration and false if ctx was canceled
// first. A non-positive d returns true immediately without allocating a timer.
func SleepCtx(ctx context.Context, d time.Duration) bool {
	if d <= 0 {
		return true
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

// ScalePct returns d*pct/100, computed in int64 nanoseconds so a large d (e.g.
// a 24h poll frequency) times a percentage cannot overflow. pct is a whole
// percent; callers clamp it to [0,100] via proto validation.
func ScalePct(d time.Duration, pct uint32) time.Duration {
	if d <= 0 || pct == 0 {
		return 0
	}
	return time.Duration(int64(d) * int64(pct) / 100)
}

// ScaleIntPct returns n*pct/100 for byte-sized thresholds, computed in int64 to
// avoid overflow on large n. A non-positive n or zero pct returns 0.
func ScaleIntPct(n int, pct uint32) int {
	if n <= 0 || pct == 0 {
		return 0
	}
	return int(int64(n) * int64(pct) / 100)
}
