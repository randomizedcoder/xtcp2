package linkmonitor

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
)

// This is an integration harness for real coordinator/executor goroutines,
// with deterministic source I/O rather than changes to physical interfaces.
type reconcileHarness struct {
	coordinator  *reconciler
	clock        *testkit.Clock
	monitor      *Monitor
	publications chan Snapshot
	done         chan error
	cancel       context.CancelFunc
}

func runReconcileHarness(t *testing.T, factory subscriptionFactory, source inventoryBackend) *reconcileHarness {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	clock := testkit.NewClock(time.Unix(100, 0))
	pool, err := newCollectorPool(ctx, clock, func(int) (workerCollector, error) {
		return testWorkerCollector{CollectorFunc: func(context.Context, model.Job) model.Result { return model.Result{Support: model.Supported} }, closeFunc: func() error { return nil }}, nil
	})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	inventory := newInventoryExecutor(ctx, source)
	events := newEventExecutor(ctx, newEventInbox(1), factory)
	s := newScheduler(newReducer(1), pool, clock)
	c, err := newReconciler(s, inventory, events, 10*time.Second)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	m, err := New(DefaultConfig(), Options{})
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	h := &reconcileHarness{coordinator: c, clock: clock, monitor: m, publications: make(chan Snapshot, 128), done: make(chan error, 1), cancel: cancel}
	loop := &schedulerLoop{scheduler: s, inventory: inventory, hooks: schedulerHooks{publish: func(now model.Stamp) error {
		if err := s.reducer.publish(m, publicationState{now: now, expected: presentValue(uint64(1)), health: Health{Running: true}}); err != nil {
			return err
		}
		h.publications <- m.Snapshot()
		return nil
	}}}
	go func() { h.done <- c.run(ctx, loop) }()
	t.Cleanup(func() {
		cancel()
		receiveTest(t, pool.done)
		receiveTest(t, inventory.done)
		receiveTest(t, events.done)
	})
	return h
}

func waitReconcileHealth(t *testing.T, h *reconcileHarness, healthy bool) Snapshot {
	t.Helper()
	for {
		snapshot := receiveTest(t, h.publications)
		if snapshot.Health().CollectionHealthy == healthy {
			return snapshot
		}
	}
}

func TestReconcileSubscribeBeforeDump(t *testing.T) {
	var subscribed atomic.Bool
	var queries atomic.Int32
	dumpStarted := make(chan struct{}, 1)
	emit := make(chan struct{})
	eventSent := make(chan struct{}, 1)
	releaseDump := make(chan struct{})
	stopped := make(chan struct{})
	var closeOnce sync.Once
	initial := observed(newReducer(1), 1, true)
	down := initial
	down.Device.Up = presentValue(false)
	factory := func(context.Context, uint64) (eventSubscription, error) {
		subscribed.Store(true)
		return eventSubscription{source: testkit.EventFuncs{
			RunFunc: func(_ context.Context, visit func(model.Event) bool) error {
				select {
				case <-emit:
				case <-stopped:
					return context.Canceled
				}
				if !visit(model.Event{Kind: model.EventChange, Observation: down}) {
					return errors.New("event rejected")
				}
				eventSent <- struct{}{}
				<-stopped
				return context.Canceled
			}, CloseFunc: func() error { closeOnce.Do(func() { close(stopped) }); return nil },
		}}, nil
	}
	source := testInventoryBackend{InventoryFuncs: testkit.InventoryFuncs{
		DumpFunc: func(ctx context.Context) (model.Candidate, error) {
			if !subscribed.Load() {
				return model.Candidate{}, errors.New("dump preceded subscription")
			}
			dumpStarted <- struct{}{}
			select {
			case <-releaseDump:
				return model.Candidate{Complete: true, Devices: []model.Observation{initial}}, nil
			case <-ctx.Done():
				return model.Candidate{}, ctx.Err()
			}
		}, QueryFunc: func(context.Context, model.DeviceKey) (model.Observation, error) { queries.Add(1); return down, nil },
	}, closeFunc: func() error { return nil }}
	h := runReconcileHarness(t, factory, source)
	receiveTest(t, dumpStarted)
	close(emit)
	receiveTest(t, eventSent)
	close(releaseDump)
	snapshot := waitReconcileHealth(t, h, true)
	found := false
	snapshot.RangeDevices(func(device DeviceView) bool {
		up, known := device.Up()
		found = true
		if up || !known {
			t.Error("stale dump resurrected link")
		}
		return true
	})
	if !found || queries.Load() != 1 {
		t.Fatal("startup dirty query missing")
	}
	h.cancel()
	if err := receiveTest(t, h.done); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestReconcileReaderRecovery(t *testing.T) {
	var opens, closed, active atomic.Int32
	fail := make(chan struct{})
	initial := observed(newReducer(1), 1, true)
	factory := func(_ context.Context, epoch uint64) (eventSubscription, error) {
		id := opens.Add(1)
		if active.Load() != 0 || (id > 1 && closed.Load() != id-1) {
			return eventSubscription{}, fmt.Errorf("replacement opened before previous reader joined")
		}
		if uint64(id) != epoch {
			return eventSubscription{}, fmt.Errorf("subscription did not use fresh epoch")
		}
		stop := make(chan struct{})
		var once sync.Once
		return eventSubscription{source: testkit.EventFuncs{
			RunFunc: func(_ context.Context, _ func(model.Event) bool) error {
				active.Add(1)
				defer active.Add(-1)
				if id == 1 {
					select {
					case <-fail:
						return syscall.ENOBUFS
					case <-stop:
						return context.Canceled
					}
				}
				<-stop
				return context.Canceled
			}, CloseFunc: func() error { once.Do(func() { closed.Add(1); close(stop) }); return nil },
		}}, nil
	}
	source := testInventoryBackend{InventoryFuncs: testkit.InventoryFuncs{
		DumpFunc: func(context.Context) (model.Candidate, error) {
			return model.Candidate{Complete: true, Devices: []model.Observation{initial}}, nil
		},
	}, closeFunc: func() error { return nil }}
	h := runReconcileHarness(t, factory, source)
	waitReconcileHealth(t, h, true)
	// Drain already published healthy snapshots before forcing transport loss.
	close(fail)
	waitReconcileHealth(t, h, false)
	advanceSchedulerClock(t, h.clock, time.Second)
	waitReconcileHealth(t, h, true)
	if opens.Load() != 2 || closed.Load() != 1 {
		t.Fatal("fresh subscription/reconciliation missing")
	}
	h.cancel()
	if err := receiveTest(t, h.done); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}
