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

type testWorkerCollector struct {
	testkit.CollectorFunc
	closeFunc func() error
}

func (c testWorkerCollector) Close() error { return c.closeFunc() }

type testInventoryBackend struct {
	testkit.InventoryFuncs
	closeFunc func() error
}

func (s testInventoryBackend) Close() error { return s.closeFunc() }

func receiveTest[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for test barrier")
		var zero T
		return zero
	}
}

func TestSchedulerPoolOwnership(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		failAt                                int
	}{
		{"success", "positive", "four independent collector resources", "four workers execute and close exactly once", -1},
		{"partial open", "negative", "factory fails after two resources", "opened resources close and error survives", 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			clock := testkit.NewClock(time.Unix(100, 0))
			var calls, closes [collectorWorkers]atomic.Int32
			boom := errors.New("factory failure")
			closeErr := errors.New("close failure")
			pool, err := newCollectorPool(t.Context(), clock, func(id int) (workerCollector, error) {
				if id == tc.failAt {
					return nil, boom
				}
				return testWorkerCollector{CollectorFunc: func(_ context.Context, _ model.Job) model.Result {
					calls[id].Add(1)
					return model.Result{Support: model.Supported, Job: model.Job{Token: model.Token{Attempt: 999}}}
				}, closeFunc: func() error {
					closes[id].Add(1)
					if id == 0 {
						return closeErr
					}
					return nil
				}}, nil
			})
			if tc.failAt >= 0 {
				if pool != nil || !errors.Is(err, boom) || !errors.Is(err, closeErr) {
					t.Fatalf("partial startup=%v", err)
				}
				for id := range calls {
					want := int32(0)
					if id < tc.failAt {
						want = 1
					}
					if calls[id].Load() != 0 || closes[id].Load() != want {
						t.Fatal("partial startup leaked or ran worker")
					}
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { pool.stop(); receiveTest(t, pool.done) })
			s := newScheduler(newReducer(1), pool, clock)
			for index := uint32(1); index <= 4; index++ {
				registerTestJob(t, s, index, model.CollectorDriver, schedulePolicy{})
			}
			dispatchTest(t, s)
			for range 4 {
				result := receiveTest(t, pool.results)
				if result.result.Job.Token.Attempt == 999 {
					t.Fatal("adapter forged completion identity")
				}
				s.complete(result)
			}
			for id := range calls {
				if calls[id].Load() != 1 || closes[id].Load() != 0 {
					t.Fatal("worker sharing or premature close")
				}
			}
			pool.stop()
			receiveTest(t, pool.done)
			if !errors.Is(pool.closeError(), closeErr) {
				t.Fatal("lost cleanup error")
			}
			for id := range closes {
				if closes[id].Load() != 1 {
					t.Fatal("resource not closed exactly once")
				}
			}
		})
	}
}

func TestSchedulerExhaustedWorkers(t *testing.T) {
	clock := testkit.NewClock(time.Unix(100, 0))
	entered := make(chan model.Job, collectorWorkers)
	release := make(chan struct{})
	var started, closed atomic.Int32
	pool, err := newCollectorPool(t.Context(), clock, func(_ int) (workerCollector, error) {
		return testWorkerCollector{CollectorFunc: func(_ context.Context, job model.Job) model.Result {
			started.Add(1)
			entered <- job
			<-release // Deliberately model an ioctl which cannot honor cancellation.
			return model.Result{Support: model.Supported, Samples: []model.Sample{counter("late", 99, 64)}}
		}, closeFunc: func() error { closed.Add(1); return nil }}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pool.stop(); close(release); receiveTest(t, pool.done) })
	s := newScheduler(newReducer(1), pool, clock)
	monitor, err := New(DefaultConfig(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	for index := uint32(1); index <= 5; index++ {
		registerTestJob(t, s, index, model.CollectorDriver, schedulePolicy{interval: time.Second})
	}
	// Existing fresh counters must expire even if all following requests stick.
	collect(t, s.reducer, model.CollectorCarrier, counter("flaps", 4, 64))
	inventory := newInventoryExecutor(t.Context(), testInventoryBackend{InventoryFuncs: testkit.InventoryFuncs{
		DumpFunc: func(context.Context) (model.Candidate, error) { return model.Candidate{Complete: true}, nil },
	}, closeFunc: func() error { return nil }})
	t.Cleanup(func() { inventory.stop(); receiveTest(t, inventory.done) })
	events := make(chan model.Event, 4096)
	controls := make(chan struct{}, 1)
	eventCount, inventoryCount, controlCount := 0, 0, 0
	var publishErr error
	loop := &schedulerLoop{scheduler: s, inventory: inventory, events: events, controls: controls, hooks: schedulerHooks{
		event: func(event model.Event) error {
			eventCount++
			_, err := s.reducer.observe(event.Observation)
			s.refreshDevice(event.Observation.Device.Key)
			return err
		},
		inventory: func(result inventoryCompletion) error {
			inventoryCount++
			if !result.candidate.Complete {
				t.Error("lost complete inventory")
			}
			return result.err
		},
		control: func() error { controlCount++; return nil },
		publish: func(now model.Stamp) error {
			publishErr = s.reducer.publish(monitor, publicationState{now: now})
			return publishErr
		},
	}}
	if err := loop.turn(schedulerWake{}); err != nil {
		t.Fatal(err)
	}
	for range 4 {
		receiveTest(t, entered)
	}
	if !inventory.submit(inventoryRequest{}) || inventory.submit(inventoryRequest{}) {
		t.Fatal("inventory path unbounded or unavailable")
	}
	result := receiveTest(t, inventory.results)
	advanceSchedulerClock(t, clock, 46*time.Second)
	for i := range 200 {
		events <- model.Event{Kind: model.EventChange, Observation: observed(s.reducer, 1, i%2 != 0)}
	}
	controls <- struct{}{}
	if err := loop.turn(schedulerWake{inventory: &result}); err != nil {
		t.Fatal(err)
	}
	if eventCount != 64 || inventoryCount != 1 || controlCount != 1 || publishErr != nil {
		t.Fatalf("owner progress=%d/%d/%d, %v", eventCount, inventoryCount, controlCount, publishErr)
	}
	if started.Load() != 4 || closed.Load() != 0 || s.active[0] == nil {
		t.Fatal("worker was replaced or resource closed")
	}
	if slotAt(s.reducer, 1).collectors[model.CollectorCarrier].fresh {
		t.Fatal("stuck workers prevented expiry")
	}
	// Continue reducing the event backlog; no replacement or optional catch-up.
	for range 3 {
		if err := loop.turn(schedulerWake{}); err != nil {
			t.Fatal(err)
		}
	}
	if eventCount != 200 || started.Load() != 4 || monitor.Snapshot().Version() == 0 {
		t.Fatal("event/publication progress stopped")
	}
	for _, active := range s.active {
		if active == nil || !active.timedOut {
			t.Fatal("worker occupancy not retained after timeout")
		}
	}
	pool.stop()
	select {
	case <-pool.done:
		t.Fatal("stuck workers reported completion")
	default:
	}
	if closed.Load() != 0 {
		t.Fatal("resources closed while in use")
	}
}

func TestSchedulerInventoryExecutor(t *testing.T) {
	boom := errors.New("query failure")
	var closed atomic.Int32
	executor := newInventoryExecutor(t.Context(), testInventoryBackend{InventoryFuncs: testkit.InventoryFuncs{
		DumpFunc: func(context.Context) (model.Candidate, error) { return model.Candidate{Complete: true}, nil },
		QueryFunc: func(_ context.Context, key model.DeviceKey) (model.Observation, error) {
			return model.Observation{Device: model.Device{Key: key}}, boom
		},
	}, closeFunc: func() error { closed.Add(1); return boom }})
	t.Cleanup(func() { executor.stop(); receiveTest(t, executor.done) })
	for _, query := range []bool{false, true} {
		request := inventoryRequest{query: query, key: model.DeviceKey{Index: 3}, token: model.Token{SourceEpoch: 8}}
		if !executor.submit(request) || executor.submit(request) {
			t.Fatal("inventory request occupancy failed")
		}
		result := receiveTest(t, executor.results)
		if result.request != request || (query && !errors.Is(result.err, boom)) || (!query && !result.candidate.Complete) {
			t.Fatal("inventory result lost identity/status")
		}
		if executor.submit(request) {
			t.Fatal("completed but unconsumed operation allowed overlap")
		}
		executor.completed()
	}
	executor.stop()
	receiveTest(t, executor.done)
	if executor.submit(inventoryRequest{}) || closed.Load() != 1 || !errors.Is(executor.closeErr, boom) {
		t.Fatal("inventory shutdown failed")
	}
}

func TestSchedulerWorkerCancellation(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		timeout                               bool
	}{
		{"attempt budget", "negative", "collector waits on attempt context", "deadline cause wakes original worker and late result is ignored", true},
		{"pool stop", "positive", "collector waits on context during stop", "parent cancellation wakes collector before resources close", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			clock := testkit.NewClock(time.Unix(100, 0))
			entered := make(chan struct{}, 1)
			pool, err := newCollectorPool(t.Context(), clock, func(int) (workerCollector, error) {
				return testWorkerCollector{CollectorFunc: func(ctx context.Context, _ model.Job) model.Result {
					entered <- struct{}{}
					<-ctx.Done()
					return model.Result{Err: context.Cause(ctx)}
				}, closeFunc: func() error { return nil }}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { pool.stop(); receiveTest(t, pool.done) })
			s := newScheduler(newReducer(1), pool, clock)
			key := registerTestJob(t, s, 1, model.CollectorDriver, schedulePolicy{})
			dispatchTest(t, s)
			receiveTest(t, entered)
			want := context.Canceled
			if tc.timeout {
				advanceSchedulerClock(t, clock, 5*time.Second)
				s.expire(clock.Now())
				want = context.DeadlineExceeded
			} else {
				pool.stop()
			}
			completion := receiveTest(t, pool.results)
			if !errors.Is(completion.result.Err, want) {
				t.Fatalf("cancellation cause=%v", completion.result.Err)
			}
			s.complete(completion)
			if s.running(key) != nil {
				t.Fatal("returned worker remained occupied")
			}
		})
	}
}
