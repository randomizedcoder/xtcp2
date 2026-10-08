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

type lifecycleClock struct {
	*testkit.Clock
	grace chan struct{}
}

func (c lifecycleClock) NewTimer(delay time.Duration) model.Timer {
	timer := c.Clock.NewTimer(delay)
	if delay == shutdownGrace {
		c.grace <- struct{}{}
	}
	return timer
}

func sessionFixture(t *testing.T) (*lifecycleSession, *Monitor, lifecycleClock) {
	t.Helper()
	clock := lifecycleClock{Clock: testkit.NewClock(time.Unix(100, 0)), grace: make(chan struct{}, 4)}
	m := newTestMonitor(t)
	m.cfg.Settle = 0
	s := &lifecycleSession{clock: clock, namespace: 1,
		store: testkit.StoreFuncs{
			LoadFunc: func(context.Context) (model.Baseline, bool, error) { return model.Baseline{}, false, nil },
			SaveFunc: func(context.Context, model.Baseline) model.SaveResult {
				return model.SaveResult{Outcome: model.SaveDurable}
			},
			CloseFunc: func() error { return nil },
		},
		collectors: func(int) (workerCollector, error) {
			return testWorkerCollector{
				CollectorFunc: func(context.Context, model.Job) model.Result { return model.Result{} }, closeFunc: func() error { return nil },
			}, nil
		},
		inventory: func(context.Context) (inventoryBackend, error) {
			return testInventoryBackend{
				InventoryFuncs: testkit.InventoryFuncs{DumpFunc: func(context.Context) (model.Candidate, error) { return model.Candidate{Complete: true}, nil }},
				closeFunc:      func() error { return nil },
			}, nil
		},
		subscribe: func(context.Context, uint64) (eventSubscription, error) {
			return eventSubscription{source: testkit.EventFuncs{
				RunFunc:   func(ctx context.Context, _ func(model.Event) bool) error { <-ctx.Done(); return ctx.Err() },
				CloseFunc: func() error { return nil },
			}}, nil
		},
	}
	m.open = func(context.Context) (session, error) { return s, nil }
	return s, m, clock
}

func runSession(t *testing.T, m *Monitor) (context.CancelFunc, <-chan error) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(cancel)
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()
	return cancel, done
}

func awaitSnapshot(t *testing.T, m *Monitor, match func(Snapshot) bool) Snapshot {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		s := m.Snapshot()
		if match(s) {
			return s
		}
		select {
		case <-deadline.C:
			t.Fatal("snapshot condition timed out")
		default:
			runtime.Gosched()
		}
	}
}

func TestLifecycleSessionControlsAndShutdown(t *testing.T) {
	t.Log("positive/corner: real owner, workers and concurrent controls; expected: durable zero, coherent snapshots and joined shutdown")
	_, m, _ := sessionFixture(t)
	cancel, done := runSession(t, m)
	before := awaitSnapshot(t, m, func(s Snapshot) bool { return s.Health().Ready })
	var callers sync.WaitGroup
	for range 32 {
		callers.Go(func() {
			if err := m.RequestResync(); err != nil {
				t.Error(err)
			}
			if err := m.RequestRebaseline(); err != nil {
				t.Error(err)
			}
			_ = m.Snapshot().BaselineWriteErrors()
		})
	}
	callers.Wait()
	cancel()
	if err := receiveTest(t, done); err != nil {
		t.Fatal(err)
	}
	if m.Health() != (Health{}) || !before.Health().Ready {
		t.Fatal("shutdown mutated retained health or left monitor running")
	}
	if err := m.RequestRebaseline(); !errors.Is(err, ErrNotRunning) {
		t.Fatal("accepted stopped control")
	}
	if err := m.Run(t.Context()); !errors.Is(err, ErrAlreadyRun) {
		t.Fatal("restarted stopped session")
	}
}

func TestLifecycleSessionBlockedSave(t *testing.T) {
	t.Log("corner: durable save blocks while live down event arrives; expected: responsive delta and captured baseline, then clean shutdown")
	s, m, _ := sessionFixture(t)
	saved, release := make(chan model.Baseline, 1), make(chan struct{})
	emit := make(chan model.Event, 1)
	store := s.store.(testkit.StoreFuncs)
	store.SaveFunc = func(_ context.Context, record model.Baseline) model.SaveResult {
		saved <- record
		<-release
		return model.SaveResult{Outcome: model.SaveDurable}
	}
	s.store = store
	o := observed(newReducer(1), 1, true)
	s.inventory = func(context.Context) (inventoryBackend, error) {
		return testInventoryBackend{
			InventoryFuncs: testkit.InventoryFuncs{DumpFunc: func(context.Context) (model.Candidate, error) {
				return model.Candidate{Complete: true, Devices: []model.Observation{o}}, nil
			}},
			closeFunc: func() error { return nil },
		}, nil
	}
	s.subscribe = func(context.Context, uint64) (eventSubscription, error) {
		return eventSubscription{source: testkit.EventFuncs{
			RunFunc: func(ctx context.Context, visit func(model.Event) bool) error {
				select {
				case event := <-emit:
					visit(event)
				case <-ctx.Done():
					return ctx.Err()
				}
				<-ctx.Done()
				return ctx.Err()
			}, CloseFunc: func() error { return nil },
		}}, nil
	}
	cancel, done := runSession(t, m)
	if record := receiveTest(t, saved); record.Count != 1 {
		t.Fatal("unexpected saved count")
	}
	down := o
	down.Device.Up = presentValue(false)
	emit <- model.Event{Kind: model.EventChange, Observation: down}
	awaitSnapshot(t, m, func(s Snapshot) bool { count, known := s.Counts().Current(); return known && count == 0 })
	close(release)
	awaitSnapshot(t, m, func(s Snapshot) bool {
		negative, delta, known := s.Counts().Delta()
		return known && negative && delta == 1
	})
	cancel()
	if err := receiveTest(t, done); err != nil {
		t.Fatal(err)
	}
}
