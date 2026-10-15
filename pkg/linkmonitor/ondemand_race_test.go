package linkmonitor

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
)

// settledGoroutines waits for the goroutine count to fall to at most limit.
func settledGoroutines(t *testing.T, limit int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for runtime.NumGoroutine() > limit {
		if time.Now().After(deadline) {
			t.Fatalf("%d goroutines, want at most %d", runtime.NumGoroutine(), limit)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestOnDemandRace(t *testing.T) {
	netstat := RefreshScope{Collectors: 1 << model.CollectorNetstat}
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		run                                          func(t *testing.T, m *Monitor, clock *testkit.Clock, cancel context.CancelFunc)
	}{
		{"scrape-refresh-publish", "positive", "8 refreshing scrapers while the clock passes the TTL", "no race; every refresh served", func(t *testing.T, m *Monitor, clock *testkit.Clock, _ context.CancelFunc) {
			var wg sync.WaitGroup
			for range 8 {
				wg.Go(func() {
					for range 20 {
						if _, err := m.Refresh(t.Context(), netstat); err != nil {
							t.Error(err)
							return
						}
						_ = m.Snapshot().HostCollector().Fresh()
					}
				})
			}
			for range 10 {
				if err := clock.Advance(time.Second); err != nil {
					t.Fatal(err)
				}
			}
			wg.Wait()
		}},
		{"resync-during-refresh", "positive", "resyncs, which request the same jobs, while refreshes wait", "no race; refreshes complete", func(t *testing.T, m *Monitor, clock *testkit.Clock, _ context.CancelFunc) {
			var wg sync.WaitGroup
			wg.Go(func() {
				for range 20 {
					if err := m.RequestResync(); err != nil {
						t.Error(err)
					}
				}
			})
			for range 20 {
				if err := clock.Advance(3 * time.Second); err != nil {
					t.Fatal(err)
				}
				if _, err := m.Refresh(t.Context(), RefreshScope{Collectors: AllCollectors()}); err != nil {
					t.Fatal(err)
				}
			}
			wg.Wait()
		}},
		{"cancel-storm", "negative", "100 refreshes whose contexts end at once", "no goroutine left behind; the monitor still answers", func(t *testing.T, m *Monitor, clock *testkit.Clock, _ context.CancelFunc) {
			baseline := runtime.NumGoroutine()
			if err := clock.Advance(3 * time.Second); err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			for i := range 100 {
				wg.Go(func() {
					ctx, cancel := context.WithTimeout(t.Context(), time.Duration(i%3)*time.Microsecond)
					defer cancel()
					if _, err := m.Refresh(ctx, netstat); err != nil && !errors.Is(err, context.DeadlineExceeded) {
						t.Error(err)
					}
				})
			}
			wg.Wait()
			settledGoroutines(t, baseline+2)
			if _, err := m.Refresh(t.Context(), netstat); err != nil {
				t.Fatal(err)
			}
		}},
		{"ttl-edge", "boundary", "refreshes as the clock steps across the TTL edge", "each finds the job fresh or collects it; none times out", func(t *testing.T, m *Monitor, clock *testkit.Clock, _ context.CancelFunc) {
			for range 10 {
				if err := clock.Advance(time.Second); err != nil {
					t.Fatal(err)
				}
				var wg sync.WaitGroup
				for range 4 {
					wg.Go(func() {
						if result, err := m.Refresh(t.Context(), netstat); err != nil || result.TimedOut || result.Cached+result.Collected != 1 {
							t.Errorf("%+v, %v", result, err)
						}
					})
				}
				wg.Wait()
			}
		}},
		{"close-mid-refresh", "corner", "the monitor stops with refreshes in flight", "every refresh returns; no goroutine left behind", func(t *testing.T, m *Monitor, clock *testkit.Clock, cancel context.CancelFunc) {
			var wg sync.WaitGroup
			for range 16 {
				wg.Go(func() {
					if _, err := m.Refresh(t.Context(), netstat); err != nil && !errors.Is(err, ErrNotRunning) {
						t.Error(err)
					}
				})
			}
			if err := clock.Advance(3 * time.Second); err != nil {
				t.Fatal(err)
			}
			cancel()
			wg.Wait()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			m, clock, release, cancel, done := onDemandSession(t, CollectionOnDemand, false)
			tc.run(t, m, clock, cancel)
			release()
			cancel()
			if err := receiveTest(t, done); err != nil {
				t.Fatal(err)
			}
		})
	}
}
