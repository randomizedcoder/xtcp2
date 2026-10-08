package linkmonitor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
	"golang.org/x/sys/unix"
)

func TestSettingsBlockedIoctlOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		stop                                         bool
	}{
		{"timeout", "boundary", "ioctl outlives logical collection deadline", "worker remains occupied and late result rejected", false},
		{"shutdown", "corner", "shutdown during uncancellable ioctl", "close only after original call returns", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			clock := testkit.NewClock(time.Unix(100, 0))
			entered, release := make(chan struct{}), make(chan struct{})
			var closes atomic.Int32
			pool, err := newCollectorPool(ctx, clock, func(int) (workerCollector, error) {
				fallback := &ethtoolIoctl{validate: func(context.Context, string, uint32) error { return nil }, invoke: func(string, []byte) error { close(entered); <-release; return nil }}
				return testWorkerCollector{CollectorFunc: func(ctx context.Context, job model.Job) model.Result {
					c := &settingsCollector{client: &fakeSettingsRequests{getError: unix.EOPNOTSUPP}, ioctl: fallback}
					return c.Collect(ctx, job)
				}, closeFunc: func() error { closes.Add(1); return nil }}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			s := newScheduler(newReducer(1), pool, clock)
			key := registerTestJob(t, s, 1, model.CollectorRings, schedulePolicy{})
			dispatchTest(t, s)
			receiveTest(t, entered)
			if err := clock.Advance(collectionBudget); err != nil {
				t.Fatal(err)
			}
			s.expire(clock.Now())
			if s.active[0] == nil || !s.active[0].timedOut {
				t.Fatal(tc.expectedOutcome)
			}
			if tc.stop {
				pool.stop()
			}
			select {
			case <-pool.done:
				t.Fatal("worker resources closed during ioctl")
			default:
			}
			if closes.Load() == collectorWorkers {
				t.Fatal(tc.expectedOutcome)
			}
			close(release)
			s.complete(receiveTest(t, pool.results))
			state, _, err := s.reducer.collector(key)
			if err != nil || state.fresh || state.block != nil {
				t.Fatalf("late result accepted: %v", err)
			}
			pool.stop()
			receiveTest(t, pool.done)
			if closes.Load() != collectorWorkers {
				t.Fatal("workers not joined")
			}
		})
	}
}
