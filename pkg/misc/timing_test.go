package misc

import (
	"context"
	"testing"
	"time"
)

func TestJitterDuration(t *testing.T) {
	tests := []struct {
		name string
		max  time.Duration
		kind string // "range", "zero"
	}{
		{"positive typical", 100 * time.Millisecond, "range"},
		{"positive large (24h)", 24 * time.Hour, "range"},
		{"boundary max==1ns", 1, "range"},
		{"corner zero", 0, "zero"},
		{"negative", -5 * time.Second, "zero"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Sample many draws: jitter is random, so assert the invariant
			// (always within [0, max)) rather than a single value.
			for range 1000 {
				got := JitterDuration(tt.max)
				switch tt.kind {
				case "zero":
					if got != 0 {
						t.Fatalf("JitterDuration(%v) = %v, want 0", tt.max, got)
					}
				case "range":
					if got < 0 || got >= tt.max {
						t.Fatalf("JitterDuration(%v) = %v, want [0,%v)", tt.max, got, tt.max)
					}
				}
			}
		})
	}
}

func TestJitterIntN(t *testing.T) {
	tests := []struct {
		name string
		max  int
		zero bool
	}{
		{"positive typical", 63 * 1024 * 1024, false},
		{"boundary max==1", 1, false}, // only valid value is 0
		{"corner zero", 0, true},
		{"negative", -10, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for range 1000 {
				got := JitterIntN(tt.max)
				if tt.zero {
					if got != 0 {
						t.Fatalf("JitterIntN(%d) = %d, want 0", tt.max, got)
					}
					continue
				}
				if got < 0 || got >= tt.max {
					t.Fatalf("JitterIntN(%d) = %d, want [0,%d)", tt.max, got, tt.max)
				}
			}
		})
	}
}

func TestScalePct(t *testing.T) {
	tests := []struct {
		name string
		d    time.Duration
		pct  uint32
		want time.Duration
	}{
		{"positive 20% of 1m", time.Minute, 20, 12 * time.Second},
		{"boundary 100%", time.Minute, 100, time.Minute},
		{"corner 0%", time.Minute, 0, 0},
		{"corner zero duration", 0, 50, 0},
		{"negative duration", -time.Minute, 50, 0},
		{"large 10% of 24h (no overflow)", 24 * time.Hour, 10, 144 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ScalePct(tt.d, tt.pct); got != tt.want {
				t.Fatalf("ScalePct(%v, %d) = %v, want %v", tt.d, tt.pct, got, tt.want)
			}
		})
	}
}

func TestScaleIntPct(t *testing.T) {
	const mib = 1024 * 1024
	tests := []struct {
		name string
		n    int
		pct  uint32
		want int
	}{
		{"positive 20% of 63MiB", 63 * mib, 20, 63 * mib / 5},
		{"boundary 100%", 63 * mib, 100, 63 * mib},
		{"corner 0%", 63 * mib, 0, 0},
		{"corner zero n", 0, 20, 0},
		{"negative n", -mib, 20, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ScaleIntPct(tt.n, tt.pct); got != tt.want {
				t.Fatalf("ScaleIntPct(%d, %d) = %d, want %d", tt.n, tt.pct, got, tt.want)
			}
		})
	}
}

func TestSleepCtx(t *testing.T) {
	t.Run("positive sleeps full duration", func(t *testing.T) {
		start := time.Now()
		if !SleepCtx(context.Background(), 20*time.Millisecond) {
			t.Fatal("SleepCtx returned false without cancellation")
		}
		if elapsed := time.Since(start); elapsed < 15*time.Millisecond {
			t.Fatalf("SleepCtx returned after %v, expected ~20ms", elapsed)
		}
	})

	t.Run("negative canceled ctx returns false", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel() // pre-canceled
		start := time.Now()
		if SleepCtx(ctx, time.Hour) {
			t.Fatal("SleepCtx returned true on canceled ctx")
		}
		if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
			t.Fatalf("SleepCtx blocked %v on canceled ctx, expected prompt return", elapsed)
		}
	})

	t.Run("corner non-positive d returns true immediately", func(t *testing.T) {
		if !SleepCtx(context.Background(), 0) {
			t.Fatal("SleepCtx(_, 0) = false, want true")
		}
		if !SleepCtx(context.Background(), -time.Second) {
			t.Fatal("SleepCtx(_, negative) = false, want true")
		}
	})
}
