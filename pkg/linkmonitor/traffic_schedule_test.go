package linkmonitor

import (
	"errors"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
)

func testTraffic(t *testing.T, count int) (*trafficSchedule, *testkit.Clock) {
	t.Helper()
	c, clock := testReconciler(t)
	c.pending = false
	for i := 1; i <= count; i++ {
		mustObserve(t, c.scheduler.reducer, observed(c.scheduler.reducer, uint32(i), true))
	}
	return newTrafficSchedule(c.scheduler, c, 15*time.Second), clock
}

func dispatchTraffic(t *testing.T, traffic *trafficSchedule) (workerAssignment, map[model.DeviceKey]*model.LinkStatistics) {
	t.Helper()
	s := traffic.scheduler
	if err := s.dispatch(); err != nil {
		t.Fatal(err)
	}
	work := receiveTest(t, s.pool.inbox[0])
	if work.traffic == nil {
		t.Fatal("nontraffic assignment")
	}
	records := make(map[model.DeviceKey]*model.LinkStatistics)
	for _, slot := range s.reducer.slots {
		if slot.device.Key.Kind == model.DeviceEthernet && slot.device.Eligibility == model.Eligible {
			records[slot.device.Key] = trafficRecord(slot.device.Key)
		}
	}
	return work, records
}

func completeTraffic(t *testing.T, traffic *trafficSchedule, work workerAssignment, reply trafficReply) {
	t.Helper()
	s := traffic.scheduler
	s.complete(workerCompletion{worker: 0, result: model.Result{Job: work.job, Finished: s.clock.Now()}, traffic: &reply})
	if err := traffic.advance(s.clock.Now()); err != nil {
		t.Fatal(err)
	}
}

func TestTrafficSharedSweepAndFanout(t *testing.T) {
	t.Log("positive/boundary: 130 interfaces; expected: one assignment, fan-out of 64/64/2 without inventory mutation")
	traffic, _ := testTraffic(t, 130)
	s := traffic.scheduler
	work, records := dispatchTraffic(t, traffic)
	if work.traffic.index != 0 || len(s.active[0].traffic.targets) != 130 {
		t.Fatal("per-device dumps replaced shared sweep")
	}
	for _, inbox := range s.pool.inbox {
		if len(inbox) != 0 {
			t.Fatal("duplicate physical request")
		}
	}
	completeTraffic(t, traffic, work, trafficReply{records: records})
	if traffic.batch == nil || traffic.batch.offset != 64 {
		t.Fatal("unbounded fan-out")
	}
	if err := traffic.advance(s.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if traffic.batch == nil || traffic.batch.offset != 128 {
		t.Fatal("second fan-out budget")
	}
	if err := traffic.advance(s.clock.Now()); err != nil {
		t.Fatal(err)
	}
	if traffic.batch != nil || s.reducer.upCount != 130 || s.reducer.lastResync.Present {
		t.Fatal("stats mutated inventory or failed to finish")
	}
	for _, slot := range s.reducer.slots {
		if !slot.collectors[model.CollectorNetdev].fresh || !slot.collectors[model.CollectorCarrier].fresh {
			t.Fatal("missing per-device result")
		}
	}
}

func TestTrafficTargetCoalescing(t *testing.T) {
	traffic, _ := testTraffic(t, 1)
	traffic.bulk = false
	key := traffic.scheduler.reducer.slots[0].device.Key
	for range 100 {
		traffic.request(key, 0)
	}
	if traffic.queue.Len() != 1 {
		t.Fatal("unbounded event queue")
	}
	work, records := dispatchTraffic(t, traffic)
	if work.traffic.index != 1 {
		t.Fatal("targeted request became dump")
	}
	completeTraffic(t, traffic, work, trafficReply{records: records})
	if traffic.queue.Len() != 0 {
		t.Fatal("duplicate work after coalesced query")
	}
}

func TestTrafficTimeoutOccupancy(t *testing.T) {
	t.Log("negative: all-device call exceeds budget; expected: per-device timeout once, physical slot retained until original completion")
	traffic, clock := testTraffic(t, 2)
	s := traffic.scheduler
	work, _ := dispatchTraffic(t, traffic)
	advanceClock(t, clock, collectionBudget)
	s.expire(clock.Now())
	if err := traffic.advance(clock.Now()); err != nil {
		t.Fatal(err)
	}
	if s.active[0] == nil || !s.active[0].timedOut {
		t.Fatal("timeout released physical ownership")
	}
	for _, slot := range s.reducer.slots {
		if slot.collectors[model.CollectorNetdev].reason != model.ErrorTimeout {
			t.Fatal("missing device timeout")
		}
	}
	traffic.bulk = true
	if err := s.dispatch(); err != nil {
		t.Fatal(err)
	}
	for _, inbox := range s.pool.inbox {
		if len(inbox) != 0 {
			t.Fatal("replacement worker started")
		}
	}
	completeTraffic(t, traffic, work, trafficReply{records: map[model.DeviceKey]*model.LinkStatistics{}})
	if s.active[0] != nil {
		t.Fatal("late completion did not release own slot")
	}
}

func TestTrafficInventoryReuse(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		failure                               bool
	}{
		{"success", "positive", "in-flight inventory carries valid statistics", "reuse with zero duplicate requests", false},
		{"failure", "negative", "inventory fails after a prefix", "independent sweep allowed", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			traffic, _ := testTraffic(t, 1)
			c := traffic.coordinator
			c.pending = true
			advanceReconcile(t, c)
			work := takeInventory(t, c)
			if err := c.scheduler.dispatch(); err != nil {
				t.Fatal(err)
			}
			if len(c.scheduler.pool.inbox[0]) != 0 {
				t.Fatal("dump raced independent inventory")
			}
			o := observed(c.scheduler.reducer, 1, true)
			result := inventoryCompletion{candidate: model.Candidate{Complete: true, Devices: []model.Observation{o}, Statistics: []model.LinkStatistics{*trafficRecord(o.Device.Key)}}}
			if tc.failure {
				result.err = errors.New("failed inventory")
			}
			completeInventory(t, c, work, result)
			if err := traffic.advance(c.scheduler.clock.Now()); err != nil {
				t.Fatal(err)
			}
			if tc.failure {
				dispatchTraffic(t, traffic)
			} else {
				if err := c.scheduler.dispatch(); err != nil {
					t.Fatal(err)
				}
				if len(c.scheduler.pool.inbox[0]) != 0 || !c.scheduler.reducer.slots[0].collectors[model.CollectorNetdev].fresh {
					t.Fatal("inventory statistics not reused")
				}
			}
		})
	}
}

func TestTrafficStaleAndNative(t *testing.T) {
	traffic, _ := testTraffic(t, 1)
	s := traffic.scheduler
	work, records := dispatchTraffic(t, traffic)
	o := observed(s.reducer, 1, false)
	deliverEvent(t, traffic.coordinator, model.EventChange, o)
	completeTraffic(t, traffic, work, trafficReply{records: records})
	if s.reducer.slots[0].collectors[model.CollectorNetdev].fresh {
		t.Fatal("old revision accepted")
	}
	native := model.Observation{Device: model.Device{Key: model.DeviceKey{Namespace: 1, Kind: model.DeviceNativeRDMA, RDMADevice: "mlx5_0", Port: 1}, Token: model.Token{SourceEpoch: 1}, Eligibility: model.Eligible, Up: presentValue(true)}}
	mustObserve(t, s.reducer, native)
	traffic.request(native.Device.Key, 0)
	if _, exists := traffic.pending[native.Device.Key]; exists {
		t.Fatal("native port queued Ethernet statistics")
	}
	if s.reducer.slots[1].collectors[model.CollectorCarrier].support != model.NotApplicable {
		t.Fatal("native carrier fabricated")
	}
}

func TestTrafficMissedPolls(t *testing.T) {
	traffic, clock := testTraffic(t, 1)
	work, records := dispatchTraffic(t, traffic)
	completeTraffic(t, traffic, work, trafficReply{records: records})
	advanceClock(t, clock, time.Hour)
	if err := traffic.advance(clock.Now()); err != nil {
		t.Fatal(err)
	}
	if !traffic.bulk || traffic.nextPoll != time.Hour+15*time.Second {
		t.Fatal("missed intervals queued catch-up work")
	}
}

func TestTrafficReuseObservationTime(t *testing.T) {
	for _, tc := range []struct {
		name, description, expected string
		observed                    time.Duration
		fresh, refresh              bool
	}{
		{"recent", "inventory statistics observed ten seconds ago", "original freshness retained", 50 * time.Second, true, false},
		{"expired", "inventory statistics observed sixty seconds ago", "no renewed freshness", 0, false, false},
		{"future", "invalid future observation", "targeted refresh without publication", 61 * time.Second, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s; expected: %s", tc.description, tc.expected)
			traffic, clock := testTraffic(t, 1)
			r := traffic.scheduler.reducer
			advanceClock(t, clock, time.Minute)
			key := r.slots[0].device.Key
			record := trafficRecord(key)
			record.Observed.Value.Monotonic = tc.observed
			if err := traffic.reuse(map[model.DeviceKey]*model.LinkStatistics{key: record}, clock.Now()); err != nil {
				t.Fatal(err)
			}
			if err := traffic.advance(clock.Now()); err != nil {
				t.Fatal(err)
			}
			traffic.scheduler.expire(clock.Now())
			if r.slots[0].collectors[model.CollectorNetdev].fresh != tc.fresh || r.slots[0].collectors[model.CollectorCarrier].fresh != tc.fresh {
				t.Fatal("reuse changed observation freshness")
			}
			if (traffic.queue.Len() != 0) != tc.refresh {
				t.Fatal("invalid observation refresh mismatch")
			}
		})
	}
}
