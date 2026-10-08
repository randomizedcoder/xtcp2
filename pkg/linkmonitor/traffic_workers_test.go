package linkmonitor

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
)

type testTrafficWorker struct {
	testWorkerCollector
	read func(context.Context, trafficRequest) trafficReply
}

func (s testTrafficWorker) readTraffic(ctx context.Context, work trafficRequest) trafficReply {
	return s.read(ctx, work)
}

func TestTrafficWorkerOwnership(t *testing.T) {
	t.Log("negative: one shared call ignores cancellation; expected: no fifth worker, original resources close only after return")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	clock := testkit.NewClock(time.Unix(100, 0))
	entered, release := make(chan struct{}), make(chan struct{})
	var closes atomic.Int32
	pool, err := newCollectorPool(ctx, clock, func(int) (workerCollector, error) {
		return testTrafficWorker{
			testWorkerCollector: testWorkerCollector{closeFunc: func() error { closes.Add(1); return nil }},
			read:                func(context.Context, trafficRequest) trafficReply { close(entered); <-release; return trafficReply{} },
		}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	pool.inbox[0] <- workerAssignment{ctx: ctx, traffic: &trafficRequest{}}
	receiveTest(t, entered)
	cancel()
	select {
	case <-pool.done:
		t.Fatal("pool joined while traffic still owned resources")
	default:
	}
	if closes.Load() >= collectorWorkers {
		t.Fatal("active owner closed before return")
	}
	close(release)
	receiveTest(t, pool.done)
	result := receiveTest(t, pool.results)
	if result.traffic == nil || closes.Load() != collectorWorkers {
		t.Fatal("traffic completion or fixed worker cleanup lost")
	}
}

func TestTrafficDisabledAndEnabledSession(t *testing.T) {
	t.Log("positive: lifecycle opt-in owns traffic resources and shuts down even with an empty inventory; expected: no interface I/O required for zero eligible links")
	s, m, _ := sessionFixture(t)
	s.statistics = true
	cancel, done := runSession(t, m)
	awaitSnapshot(t, m, func(s Snapshot) bool { return s.Health().Ready })
	cancel()
	if err := receiveTest(t, done); err != nil {
		t.Fatal(err)
	}
}

func TestTrafficPartialQueryInvalidatesOldStatistics(t *testing.T) {
	traffic, _ := testTraffic(t, 1)
	c := traffic.coordinator
	c.pending = true
	advanceReconcile(t, c)
	dump := takeInventory(t, c)
	o := observed(c.scheduler.reducer, 1, true)
	deliverEvent(t, c, model.EventRefresh, o)
	completeInventory(t, c, dump, inventoryCompletion{candidate: model.Candidate{Complete: true, Devices: []model.Observation{o}, Statistics: []model.LinkStatistics{*trafficRecord(o.Device.Key)}}})
	query := takeInventory(t, c)
	completeInventory(t, c, query, inventoryCompletion{observation: o})
	if traffic.batch != nil {
		t.Fatal("query without statistics reused old dump counters")
	}
	if !traffic.bulk {
		t.Fatal("missing statistics did not request replacement collection")
	}
}

func TestTrafficInvalidInventoryStatistics(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		mutate                                func([]model.LinkStatistics) []model.LinkStatistics
	}{
		{"duplicate", "negative", "duplicate statistics records", "reject candidate", func(records []model.LinkStatistics) []model.LinkStatistics { return append(records, records[0]) }},
		{"identity", "negative", "statistics lack inventory identity", "reject candidate", func(records []model.LinkStatistics) []model.LinkStatistics { records[0].Key.Index = 2; return records }},
		{"timestamp", "negative", "reuse without observation timestamp", "reject candidate", func(records []model.LinkStatistics) []model.LinkStatistics {
			records[0].Observed.Present = false
			return records
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			traffic, _ := testTraffic(t, 1)
			c := traffic.coordinator
			c.pending = true
			advanceReconcile(t, c)
			dump := takeInventory(t, c)
			o := observed(c.scheduler.reducer, 1, true)
			completeInventory(t, c, dump, inventoryCompletion{candidate: model.Candidate{Complete: true, Devices: []model.Observation{o}, Statistics: tc.mutate([]model.LinkStatistics{*trafficRecord(o.Device.Key)})}})
			if c.scheduler.reducer.lastResync.Present || c.lastError == nil {
				t.Fatal("invalid statistics candidate accepted")
			}
		})
	}
}

func TestTrafficFailureClassification(t *testing.T) {
	if trafficReason(context.DeadlineExceeded) != model.ErrorTimeout || trafficReason(errCarrierValue) != model.ErrorMalformed || trafficReason(errors.New("I/O")) != model.ErrorIO {
		t.Fatal("wrong diagnostic classification")
	}
}
