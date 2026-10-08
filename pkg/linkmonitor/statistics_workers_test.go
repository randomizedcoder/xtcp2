package linkmonitor

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
)

func TestStatisticBlockedWorkers(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		blocked                                      int
	}{
		{"one", "corner", "single driver call blocks through timeout and shutdown", "no resource close before call returns", 1},
		{"all", "boundary", "all four driver calls block", "bounded occupancy and owner remains responsive", collectorWorkers},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			clock := testkit.NewClock(time.Unix(100, 0))
			entered, release := make(chan struct{}, collectorWorkers), make(chan struct{})
			var closes atomic.Int32
			pool, err := newCollectorPool(t.Context(), clock, func(int) (workerCollector, error) {
				f := &statisticFixture{names: []string{"rx"}, values: []uint64{8}}
				source := statisticTestSource(t, f)
				source.ioctl.invoke = func(name string, data []byte) error { entered <- struct{}{}; <-release; return f.invoke(name, data) }
				return testWorkerCollector{CollectorFunc: func(ctx context.Context, job model.Job) model.Result {
					return (&statisticsCollector{source: source}).Collect(ctx, job)
				},
					closeFunc: func() error { closes.Add(1); return source.buffer.Close() }}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			s := newScheduler(newReducer(1), pool, clock)
			for i := range tc.blocked {
				registerTestJob(t, s, uint32(i+1), model.CollectorDriver, schedulePolicy{})
			}
			dispatchTest(t, s)
			for range tc.blocked {
				receiveTest(t, entered)
			}
			if err := clock.Advance(collectionBudget); err != nil {
				t.Fatal(err)
			}
			s.expire(clock.Now())
			for i := range tc.blocked {
				if s.active[i] == nil || !s.active[i].timedOut {
					t.Fatal("worker prematurely released")
				}
			}
			pool.stop()
			select {
			case <-pool.done:
				t.Fatal("closed blocked resources")
			default:
			}
			if closes.Load() > int32(collectorWorkers-tc.blocked) {
				t.Fatal(tc.expectedOutcome)
			}
			close(release)
			for range tc.blocked {
				s.complete(receiveTest(t, pool.results))
			}
			receiveTest(t, pool.done)
			if closes.Load() != collectorWorkers {
				t.Fatal("resources not closed")
			}
			for _, slot := range s.reducer.slots {
				if slot.collectors[model.CollectorDriver].hasSuccess {
					t.Fatal("late result accepted")
				}
			}
		})
	}
}
