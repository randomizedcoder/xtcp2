package linkmonitor

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
)

// Manual assignment mailboxes isolate deterministic scheduling from goroutine
// execution. The worker integration tests below exercise the actual pool.
func testScheduler(t *testing.T) (*scheduler, *testkit.Clock) {
	t.Helper()
	clock := testkit.NewClock(time.Unix(100, 0))
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	p := &collectorPool{ctx: ctx, cancel: cancel, results: make(chan workerCompletion, collectorWorkers)}
	for i := range p.inbox {
		p.inbox[i] = make(chan workerAssignment, 1)
	}
	return newScheduler(newReducer(1), p, clock), clock
}

func registerTestJob(t *testing.T, s *scheduler, index uint32, kind model.CollectorKind, policy schedulePolicy) model.JobKey {
	t.Helper()
	key := model.JobKey{Namespace: 1, Collector: kind}
	if kind != model.CollectorNetstat {
		key.Device = observed(s.reducer, index, true).Device.Key
		if _, exists := s.reducer.index[key.Device]; !exists {
			mustObserve(t, s.reducer, observed(s.reducer, index, true))
		}
	}
	if err := s.register(key, policy); err != nil {
		t.Fatal(err)
	}
	return key
}

func dispatchTest(t *testing.T, s *scheduler) {
	t.Helper()
	if err := s.dispatch(); err != nil {
		t.Fatal(err)
	}
}

func finishTest(t *testing.T, s *scheduler, worker int) model.Job {
	t.Helper()
	select {
	case assignment := <-s.pool.inbox[worker]:
		s.complete(workerCompletion{worker: worker, result: model.Result{Job: assignment.job, Finished: s.clock.Now(), Support: model.Supported}})
		return assignment.job
	default:
		t.Fatal("missing assignment")
		return model.Job{}
	}
}

func TestSchedulerCoalescing(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		change                                bool
	}{
		{"compatible", "positive", "one hundred requests during one collection", "requests join with no pending duplicate", false},
		{"new revision", "corner", "observation changes during a collection", "one pending refresh and no overlap", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			s, _ := testScheduler(t)
			key := registerTestJob(t, s, 1, model.CollectorDriver, schedulePolicy{})
			dispatchTest(t, s)
			old := s.active[0].job
			if tc.change {
				mustObserve(t, s.reducer, observed(s.reducer, 1, false))
			}
			for range 100 {
				s.request(key, urgencyPeriodic)
				s.request(key, urgencyEvent)
				dispatchTest(t, s)
			}
			if s.jobs[key].pending != tc.change || s.jobs[key].ready != nil || s.active[1] != nil {
				t.Fatal("coalescing or occupancy failed")
			}
			finishTest(t, s, 0)
			if tc.change {
				state, _, err := s.reducer.collector(key)
				if err != nil || state.hasSuccess {
					t.Fatal("obsolete completion published")
				}
				dispatchTest(t, s)
				if s.active[0] == nil || s.active[0].job.Token.Revision == old.Token.Revision {
					t.Fatal("new revision was not dispatched")
				}
				finishTest(t, s, 0)
			} else {
				dispatchTest(t, s)
				if s.active[0] != nil {
					t.Fatal("duplicate work")
				}
			}
		})
	}
}

func TestSchedulerFairness(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		urgency                               jobUrgency
	}{
		{"events", "boundary", "continuous event refreshes compete with a periodic job", "periodic dispatch follows eight urgent dispatches", urgencyEvent},
		{"reconcile", "boundary", "continuous reconcile requests compete with periodic work", "same eight-dispatch bound", urgencyReconcile},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			s, _ := testScheduler(t)
			urgent := registerTestJob(t, s, 1, model.CollectorDriver, schedulePolicy{})
			periodic := registerTestJob(t, s, 2, model.CollectorDriver, schedulePolicy{})
			for s.next() != nil {
			} // Consume initial intents before the deliberate load.
			s.jobs[urgent].pending, s.jobs[periodic].pending = false, false
			s.urgent = 0
			s.request(periodic, urgencyPeriodic)
			for i := range 9 {
				s.request(urgent, tc.urgency)
				next := s.next()
				if next == nil {
					t.Fatal("missing ready job")
				}
				next.pending = false
				want := urgent
				if i == 8 {
					want = periodic
				}
				if next.key != want {
					t.Fatalf("dispatch %d: got %v want %v", i, next.key, want)
				}
			}
		})
	}
}

func TestSchedulerDeviceRotationAndPromotion(t *testing.T) {
	s, _ := testScheduler(t)
	a := registerTestJob(t, s, 1, model.CollectorDriver, schedulePolicy{})
	b := registerTestJob(t, s, 1, model.CollectorPHY, schedulePolicy{})
	c := registerTestJob(t, s, 2, model.CollectorDriver, schedulePolicy{})
	for _, want := range []model.JobKey{a, c, b} {
		next := s.next()
		if next == nil || next.key != want {
			t.Fatalf("expected device round-robin %v; got %+v", want, next)
		}
		next.pending = false
	}
	s.request(a, urgencyPeriodic)
	for range 100 {
		s.request(a, urgencyEvent)
	}
	if s.queues[urgencyPeriodic].devices.Len() != 0 || s.queues[urgencyEvent].devices.Len() != 1 || s.next().key != a || s.next() != nil {
		t.Fatal("promotion left duplicate queue entries")
	}
}

func TestSchedulerTimeoutBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		elapsed                               time.Duration
		timeout                               bool
	}{
		{"before", "positive", "completion one nanosecond before budget", "successful result", 5*time.Second - time.Nanosecond, false},
		{"exact", "boundary", "completion at exact budget before timer processing", "timeout with rejected samples", 5 * time.Second, true},
		{"late", "negative", "completion after budget", "one timeout with rejected samples", 6 * time.Second, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			s, clock := testScheduler(t)
			key := registerTestJob(t, s, 1, model.CollectorDriver, schedulePolicy{})
			dispatchTest(t, s)
			if err := clock.Advance(tc.elapsed); err != nil {
				t.Fatal(err)
			}
			finishTest(t, s, 0)
			state, _, err := s.reducer.collector(key)
			if err != nil || state.hasSuccess == tc.timeout || (state.reason == model.ErrorTimeout) != tc.timeout || s.active[0] != nil {
				t.Fatalf("bad completion: %+v, %v", state, err)
			}
			s.expire(clock.Now())
			if s.running(key) != nil {
				t.Fatal("completed slot retained")
			}
		})
	}
}

func TestSchedulerTimeoutRetainsOccupancy(t *testing.T) {
	s, clock := testScheduler(t)
	key := registerTestJob(t, s, 1, model.CollectorDriver, schedulePolicy{})
	dispatchTest(t, s)
	active := s.active[0]
	work := <-s.pool.inbox[0]
	advanceSchedulerClock(t, clock, 5*time.Second)
	s.expire(clock.Now())
	if !active.timedOut || !errors.Is(context.Cause(work.ctx), context.DeadlineExceeded) {
		t.Fatal("attempt was not canceled as timeout")
	}
	for range 100 {
		s.request(key, urgencyEvent)
		dispatchTest(t, s)
	}
	if s.active[0] != active || s.active[1] != nil || !s.jobs[key].pending {
		t.Fatal("timed out worker was replaced")
	}
	late := workerCompletion{worker: 0, result: model.Result{Job: work.job, Finished: clock.Now(), Support: model.Supported}}
	s.complete(late)
	dispatchTest(t, s)
	replacement := s.active[0]
	s.complete(late)
	if replacement == nil || s.active[0] != replacement || replacement.job.Token.Attempt == active.job.Token.Attempt {
		t.Fatal("late completion released new attempt")
	}
	finishTest(t, s, 0)
}

func TestSchedulerValidation(t *testing.T) {
	s, _ := testScheduler(t)
	key := registerTestJob(t, s, 0, model.CollectorNetstat, schedulePolicy{})
	for _, tc := range []struct {
		name, description, expected string
		key                         model.JobKey
		policy                      schedulePolicy
	}{
		{"duplicate", "register same host key twice", "error without duplicate", key, schedulePolicy{}},
		{"negative", "negative interval", "error without registration", key, schedulePolicy{interval: -1}},
		{"wrong host", "host statistics with device identity", "error", model.JobKey{Namespace: 1, Device: model.DeviceKey{Index: 2}, Collector: model.CollectorNetstat}, schedulePolicy{}},
		{"missing device", "device is not inventoried", "error", model.JobKey{Namespace: 1, Collector: model.CollectorDriver}, schedulePolicy{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("negative: %s; expected: %s", tc.description, tc.expected)
			if err := s.register(tc.key, tc.policy); err == nil || len(s.jobs) != 1 {
				t.Fatal("invalid registration accepted")
			}
		})
	}
	s.reducer.attempt = math.MaxUint64
	if err := s.dispatch(); !errors.Is(err, errSequenceExhausted) {
		t.Fatalf("overflow: %v", err)
	}
}

func advanceSchedulerClock(t *testing.T, clock *testkit.Clock, elapsed time.Duration) {
	t.Helper()
	if err := clock.Advance(elapsed); err != nil {
		t.Fatal(err)
	}
}
