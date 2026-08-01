package misc

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"time"
)

// Timing helpers for fleet-wide jitter and context-aware sleeping. See
// docs/design-jitter-and-backoff.md for the why: on a large fleet, deterministic
// timers synchronize (thundering herd), so poll scheduling, S3 flushing, and
// upload retry backoff all draw jitter from these helpers.
//
// Randomness comes from crypto/rand, which is backed by a fast per-process
// userspace CSPRNG (Go 1.24+) and is safe for concurrent use — so it needs no
// seeding, and even a fleet of identically configured processes started at the
// same instant draws independent jitter. These are low-frequency draws (per
// poll cycle / per S3 flush), so the CSPRNG cost is irrelevant.

// randUint64 returns a random uint64 from crypto/rand. The read cannot fail in
// practice (the userspace generator has no error path once initialized); on the
// theoretical error it returns 0, which the jitter helpers treat as "no jitter"
// this cycle — harmless, since a single skipped jitter draw only momentarily
// reduces spread and can't corrupt anything.
func randUint64() uint64 {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return 0
	}
	return binary.LittleEndian.Uint64(b[:])
}

// JitterDuration returns a uniform-ish random duration in [0, limit). A
// non-positive limit returns 0, so callers can pass a "disabled" (zero) window
// without a guard. (The modulo reduction is negligibly biased — irrelevant for
// spreading a thundering herd.)
func JitterDuration(limit time.Duration) time.Duration {
	if limit <= 0 {
		return 0
	}
	return time.Duration(randUint64() % uint64(limit))
}

// JitterIntN returns a uniform-ish random int in [0, limit). A non-positive
// limit returns 0. Used for the per-object S3 byte-threshold jitter.
func JitterIntN(limit int) int {
	if limit <= 0 {
		return 0
	}
	return int(randUint64() % uint64(limit))
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
