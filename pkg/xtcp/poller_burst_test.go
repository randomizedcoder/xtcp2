package xtcp

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestPollBurstRunner_firesCountPokes drives a burst through pollBurstRunner
// and asserts it pokes pollRequestCh exactly `count` times, spaced by
// `interval` sleeps (count-1 of them, none after the last poke). pollerSleep
// is stubbed to record durations and return immediately, so the test doesn't
// actually wait.
func TestPollBurstRunner_firesCountPokes(t *testing.T) {
	x := newPollerFixture(t)
	// Buffer >= count so every non-blocking poke lands (the poller isn't
	// draining in this test).
	x.pollRequestCh = make(chan struct{}, 8)
	x.pollBurstCh = make(chan pollBurst, 1)

	var mu sync.Mutex
	var sleeps []time.Duration
	x.pollerSleep = func(_ context.Context, d time.Duration) bool {
		mu.Lock()
		sleeps = append(sleeps, d)
		mu.Unlock()
		return true
	}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go x.pollBurstRunner(ctx, &wg)

	x.pollBurstCh <- pollBurst{count: 3, interval: 10 * time.Second}

	// Wait for the 3 pokes to arrive.
	for i := 0; i < 3; i++ {
		select {
		case <-x.pollRequestCh:
		case <-time.After(2 * time.Second):
			t.Fatalf("only received %d/3 pokes", i)
		}
	}
	// No 4th poke.
	select {
	case <-x.pollRequestCh:
		t.Fatal("received an unexpected 4th poke")
	case <-time.After(50 * time.Millisecond):
	}

	cancel()
	waitWG(t, &wg)

	mu.Lock()
	defer mu.Unlock()
	if len(sleeps) != 2 {
		t.Fatalf("pollerSleep called %d times, want 2 (count-1)", len(sleeps))
	}
	for i, d := range sleeps {
		if d != 10*time.Second {
			t.Errorf("sleep[%d]=%v, want 10s", i, d)
		}
	}
}

// TestPollBurstRunner_shutdownMidBurst verifies a canceled pollerSleep
// (returns false) aborts the burst early rather than firing all pokes.
func TestPollBurstRunner_shutdownMidBurst(t *testing.T) {
	x := newPollerFixture(t)
	x.pollRequestCh = make(chan struct{}, 8)
	x.pollBurstCh = make(chan pollBurst, 1)

	// First sleep returns false → burst bails after the first poke.
	x.pollerSleep = func(_ context.Context, _ time.Duration) bool { return false }

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var wg sync.WaitGroup
	wg.Add(1)
	go x.pollBurstRunner(ctx, &wg)

	x.pollBurstCh <- pollBurst{count: 5, interval: time.Second}

	waitWG(t, &wg)

	// Exactly one poke landed before the aborting sleep.
	if got := len(x.pollRequestCh); got != 1 {
		t.Fatalf("expected 1 poke before shutdown, got %d", got)
	}
}

func waitWG(t *testing.T, wg *sync.WaitGroup) {
	t.Helper()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("pollBurstRunner did not exit")
	}
}
