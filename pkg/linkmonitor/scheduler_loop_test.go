package linkmonitor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
)

func TestSchedulerOwnerLoop(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		eventError                            bool
	}{
		{"timer and cancellation", "positive", "idle owner waits for its single timer; input channels close", "expiry publishes and cancellation joins idle resources", false},
		{"hook failure", "negative", "event policy fails", "error returned and resources canceled", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			clock := testkit.NewClock(time.Unix(100, 0))
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			pool, err := newCollectorPool(ctx, clock, func(int) (workerCollector, error) {
				return testWorkerCollector{CollectorFunc: func(context.Context, model.Job) model.Result { return model.Result{} }, closeFunc: func() error { return nil }}, nil
			})
			if err != nil {
				t.Fatal(err)
			}
			inventory := newInventoryExecutor(ctx, testInventoryBackend{closeFunc: func() error { return nil }})
			s := newScheduler(newReducer(1), pool, clock)
			s.reducer.deadlines.set(deadlineEntry{key: deadlineKey{kind: deadlineResync}, at: time.Second})
			events := make(chan model.Event, 4096)
			controls := make(chan struct{}, 1)
			publications := make(chan bool, 16)
			boom := errors.New("event policy error")
			loop := &schedulerLoop{scheduler: s, inventory: inventory, events: events, controls: controls, hooks: schedulerHooks{
				event:   func(model.Event) error { return boom },
				publish: func(model.Stamp) error { publications <- s.reducer.resyncOverdue; return nil },
			}}
			done := make(chan error, 1)
			go func() { done <- loop.run(ctx) }()
			t.Cleanup(func() { cancel(); receiveTest(t, pool.done); receiveTest(t, inventory.done) })
			if receiveTest(t, publications) {
				t.Fatal("deadline expired early")
			}
			if tc.eventError {
				events <- model.Event{}
				if err := receiveTest(t, done); !errors.Is(err, boom) {
					t.Fatal(err)
				}
				return
			}
			// Closing producer channels cannot create a permanently ready spin loop.
			close(events)
			receiveTest(t, publications)
			close(controls)
			receiveTest(t, publications)
			advanceSchedulerClock(t, clock, time.Second)
			for !receiveTest(t, publications) {
			}
			cancel()
			if err := receiveTest(t, done); !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			if loop.events != nil || loop.controls != nil {
				t.Fatal("closed input remained enabled")
			}
		})
	}
}

func TestSchedulerMalformedResultDoesNotStopOwner(t *testing.T) {
	s, _ := testScheduler(t)
	key := registerTestJob(t, s, 1, model.CollectorDriver, schedulePolicy{})
	dispatchTest(t, s)
	work := <-s.pool.inbox[0]
	s.complete(workerCompletion{worker: 0, result: model.Result{Job: work.job, Support: model.Unsupported, Samples: []model.Sample{counter("bad", 1, 64)}}})
	state, _, err := s.reducer.collector(key)
	if err != nil || state.reason != model.ErrorMalformed || s.active[0] != nil {
		t.Fatal("malformed collector result blocked scheduler")
	}
	s.request(key, urgencyPeriodic)
	dispatchTest(t, s)
	finishTest(t, s, 0)
	if !state.succeeded {
		t.Fatal("collector could not recover")
	}
}

func TestSchedulerPublicationFailure(t *testing.T) {
	s, _ := testScheduler(t)
	boom := errors.New("publication version exhausted")
	loop := &schedulerLoop{scheduler: s, inventory: &inventoryExecutor{}, hooks: schedulerHooks{
		publish: func(model.Stamp) error { return boom },
	}}
	if err := loop.turn(schedulerWake{}); !errors.Is(err, boom) {
		t.Fatalf("lost publication failure: %v", err)
	}
}
