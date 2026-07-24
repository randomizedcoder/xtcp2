package xtcp

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/randomizedcoder/xtcp2/pkg/misc"
)

// identityJitter returns its argument, so computeStartupDelay/computePollInterval
// become deterministic for exact-value assertions.
func identityJitter(d time.Duration) time.Duration { return d }

// zeroJitter always returns 0 (the low end of the jitter range).
func zeroJitter(time.Duration) time.Duration { return 0 }

func TestComputeStartupDelay(t *testing.T) {
	tests := []struct {
		name   string
		freq   time.Duration
		pct    uint32
		jitter func(time.Duration) time.Duration
		want   time.Duration
	}{
		{"positive 20% identity", time.Minute, 20, identityJitter, 12 * time.Second},
		{"positive 20% zero-draw", time.Minute, 20, zeroJitter, 0},
		{"boundary 100% identity", time.Minute, 100, identityJitter, time.Minute},
		{"corner pct==0", time.Minute, 0, identityJitter, 0},
		{"corner freq==0", 0, 20, identityJitter, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := computeStartupDelay(tt.freq, tt.pct, tt.jitter); got != tt.want {
				t.Fatalf("computeStartupDelay(%v,%d) = %v, want %v", tt.freq, tt.pct, got, tt.want)
			}
		})
	}
}

func TestComputePollInterval(t *testing.T) {
	tests := []struct {
		name   string
		freq   time.Duration
		pct    uint32
		jitter func(time.Duration) time.Duration
		want   time.Duration
	}{
		// max = freq*pct/100; result = freq - max/2 + jitter(max).
		{"positive 20% identity (high end)", time.Minute, 20, identityJitter, 66 * time.Second}, // 60 - 6 + 12
		{"positive 20% zero-draw (low end)", time.Minute, 20, zeroJitter, 54 * time.Second},     // 60 - 6 + 0
		{"corner pct==0 → exact freq", time.Minute, 0, identityJitter, time.Minute},
		{"corner freq==0 → exact freq", 0, 20, identityJitter, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := computePollInterval(tt.freq, tt.pct, tt.jitter); got != tt.want {
				t.Fatalf("computePollInterval(%v,%d) = %v, want %v", tt.freq, tt.pct, got, tt.want)
			}
		})
	}
}

// With the real jitter source, computePollInterval must stay within
// [freq-max/2, freq+max/2) and always be strictly positive (a non-positive
// interval would panic time.Ticker).
func TestComputePollInterval_boundsRealJitter(t *testing.T) {
	const freq = time.Minute
	const pct = 20
	limit := misc.ScalePct(freq, pct)
	lo := freq - limit/2
	hi := freq + limit/2
	for range 2000 {
		got := computePollInterval(freq, pct, misc.JitterDuration)
		if got <= 0 {
			t.Fatalf("computePollInterval returned non-positive %v", got)
		}
		if got < lo || got >= hi {
			t.Fatalf("computePollInterval = %v, want [%v,%v)", got, lo, hi)
		}
	}
}

// startPollerFixture wires the channels + a recording pollerSleep, runs Poller,
// signals DestinationReady, lets it reach the first poll, then cancels.
func runPollerStartup(t *testing.T, pct uint32) []time.Duration {
	t.Helper()
	x := newPollerFixture(t)
	x.config.PollFrequency = durationpb.New(time.Hour) // ticker never fires
	x.config.PollJitterPct = pct
	x.config.MaxLoops = 1
	x.DestinationReady = make(chan struct{}, 1)
	x.pollRequestCh = make(chan struct{}, 1)
	x.changePollFrequencyCh = make(chan time.Duration, 1)
	x.netlinkerDoneCh = make(chan netlinkerDone, 1)
	x.pollStartTime = time.Now()

	var mu sync.Mutex
	var calls []time.Duration
	x.pollerSleep = func(_ context.Context, d time.Duration) bool {
		mu.Lock()
		calls = append(calls, d)
		mu.Unlock()
		return true
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go x.Poller(ctx, &wg)
	x.DestinationReady <- struct{}{}
	time.Sleep(60 * time.Millisecond) // let it run the startup delay + first poll
	cancel()

	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Poller did not exit after cancel")
	}
	mu.Lock()
	defer mu.Unlock()
	out := make([]time.Duration, len(calls))
	copy(out, calls)
	return out
}

func TestPoller_startupJitterWiring(t *testing.T) {
	t.Run("applied when pct>0", func(t *testing.T) {
		calls := runPollerStartup(t, 20)
		if len(calls) != 1 {
			t.Fatalf("pollerSleep called %d times, want exactly 1", len(calls))
		}
		maxDelay := misc.ScalePct(time.Hour, 20)
		if calls[0] < 0 || calls[0] >= maxDelay {
			t.Fatalf("startup delay %v out of [0,%v)", calls[0], maxDelay)
		}
	})

	t.Run("skipped when pct==0", func(t *testing.T) {
		if calls := runPollerStartup(t, 0); len(calls) != 0 {
			t.Fatalf("pollerSleep called %d times with pct==0, want 0", len(calls))
		}
	})
}
