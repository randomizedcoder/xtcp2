package linkmonitor

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/baseline"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
)

func blockSessionStage(s *lifecycleSession, stage string, block func()) {
	store := s.store.(testkit.StoreFuncs)
	switch stage {
	case "load":
		store.LoadFunc = func(context.Context) (model.Baseline, bool, error) { block(); return model.Baseline{}, false, nil }
	case "save":
		store.SaveFunc = func(context.Context, model.Baseline) model.SaveResult {
			block()
			return model.SaveResult{Outcome: model.SaveDurable}
		}
	case "store close":
		store.CloseFunc = func() error { block(); return nil }
	case "factory":
		s.inventory = func(context.Context) (inventoryBackend, error) {
			block()
			return nil, errors.New("late factory failure")
		}
	case "inventory":
		s.inventory = func(context.Context) (inventoryBackend, error) {
			return testInventoryBackend{
				InventoryFuncs: testkit.InventoryFuncs{DumpFunc: func(context.Context) (model.Candidate, error) { block(); return model.Candidate{Complete: true}, nil }},
				closeFunc:      func() error { return nil },
			}, nil
		}
	case "worker close":
		s.collectors = func(id int) (workerCollector, error) {
			return testWorkerCollector{
				CollectorFunc: func(context.Context, model.Job) model.Result { return model.Result{} },
				closeFunc: func() error {
					if id == 0 {
						block()
					}
					return nil
				},
			}, nil
		}
	case "event close":
		s.subscribe = func(context.Context, uint64) (eventSubscription, error) {
			return eventSubscription{source: testkit.EventFuncs{
				RunFunc:   func(ctx context.Context, _ func(model.Event) bool) error { <-ctx.Done(); return ctx.Err() },
				CloseFunc: func() error { block(); return nil },
			}}, nil
		}
	}
	s.store = store
}

func TestLifecycleIncompleteShutdown(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		closing                               bool
	}{
		{"load", "negative", "uncancellable startup load", "retain store until original load returns", false},
		{"save", "negative", "uncancellable durable save", "retain intended record and store lock", false},
		{"factory", "negative", "uncancellable source acquisition", "retain acquired resources until acquisition returns", false},
		{"inventory", "negative", "uncancellable inventory call", "retain source and store lock", false},
		{"store close", "corner", "blocked storage Close", "bounded Run despite blocked lock release", true},
		{"worker close", "corner", "blocked collector Close", "retain store lock until collector joins", true},
		{"event close", "corner", "blocked event Close callback", "join original reader and callback before lock release", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: ErrShutdownIncomplete and %s", tc.category, tc.description, tc.expected)
			s, m, clock := sessionFixture(t)
			entered, release, closed := make(chan struct{}), make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			t.Cleanup(unblock)
			blockSessionStage(s, tc.name, func() { close(entered); <-release })
			store := s.store.(testkit.StoreFuncs)
			closeStore := store.CloseFunc
			store.CloseFunc = func() error { defer close(closed); return closeStore() }
			s.store = store
			cancel, done := runSession(t, m)
			if tc.closing {
				awaitSnapshot(t, m, func(s Snapshot) bool { return s.Health().Ready })
				drainGrace(clock)
				cancel()
			}
			receiveTest(t, entered)
			if !tc.closing {
				drainGrace(clock)
				cancel()
			}
			receiveTest(t, clock.grace)
			advanceClock(t, clock.Clock, shutdownGrace)
			if err := receiveTest(t, done); !errors.Is(err, ErrShutdownIncomplete) {
				t.Fatalf("Run: %v", err)
			}
			stopped := m.Snapshot()
			if stopped.Health() != (Health{}) {
				t.Fatal("incomplete shutdown still running")
			}
			select {
			case <-closed:
				t.Fatal("store released before blocked owner returned")
			default:
			}
			unblock()
			receiveTest(t, closed)
			receiveTest(t, s.cleaned)
			if m.Snapshot().Version() != stopped.Version() {
				t.Fatal("deferred cleanup published after Run returned")
			}
			if err := m.Run(t.Context()); !errors.Is(err, ErrAlreadyRun) {
				t.Fatal("incomplete monitor restarted")
			}
		})
	}
}

func drainGrace(clock lifecycleClock) {
	// Subscription acquisition also has a five-second timer during startup.
	for len(clock.grace) != 0 {
		<-clock.grace
	}
}

func TestLifecycleStartupAndCleanupErrors(t *testing.T) {
	failure := errors.New("scripted failure")
	for _, tc := range []struct {
		name, category, description, expected string
		configure                             func(*lifecycleSession)
		started                               bool
	}{
		{"load error", "negative", "invalid baseline", "fail without collection", func(s *lifecycleSession) {
			store := s.store.(testkit.StoreFuncs)
			store.LoadFunc = func(context.Context) (model.Baseline, bool, error) { return model.Baseline{}, false, failure }
			s.store = store
		}, false},
		{"partial pool", "negative", "third collector factory fails", "join acquired resources", func(s *lifecycleSession) {
			original := s.collectors
			s.collectors = func(id int) (workerCollector, error) {
				if id == 2 {
					return nil, failure
				}
				return original(id)
			}
		}, false},
		{"inventory open", "negative", "inventory factory fails", "join pool and release lock", func(s *lifecycleSession) {
			s.inventory = func(context.Context) (inventoryBackend, error) { return nil, failure }
		}, false},
		{"store cleanup", "negative", "Close fails after normal cancellation", "preserve cleanup error", func(s *lifecycleSession) {
			store := s.store.(testkit.StoreFuncs)
			store.CloseFunc = func() error { return failure }
			s.store = store
		}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			s, m, _ := sessionFixture(t)
			tc.configure(s)
			cancel, done := runSession(t, m)
			if tc.started {
				awaitSnapshot(t, m, func(s Snapshot) bool { return s.Health().Ready })
				cancel()
			}
			if err := receiveTest(t, done); !errors.Is(err, failure) {
				t.Fatalf("lost error: %v", err)
			}
			if err := m.RequestResync(); !errors.Is(err, ErrNotRunning) {
				t.Fatal("failed session accepted control")
			}
		})
	}
}

func TestLifecycleRealStoreLifetime(t *testing.T) {
	t.Log("positive/negative: real durable zero and a competing instance; expected: persisted baseline, lifetime exclusion and reusable lock after shutdown")
	path := filepath.Join(t.TempDir(), "nested", "baseline.json")
	s, m, _ := sessionFixture(t)
	store, err := baseline.New(path)
	if err != nil {
		t.Fatal(err)
	}
	s.store = store
	cancel, done := runSession(t, m)
	awaitSnapshot(t, m, func(s Snapshot) bool { return s.Health().Ready })
	other, err := baseline.New(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := other.LoadAndLock(t.Context()); err == nil {
		t.Fatal("competing session acquired lifetime lock")
	}
	cancel()
	if err := receiveTest(t, done); err != nil {
		t.Fatal(err)
	}
	record, present, err := other.LoadAndLock(t.Context())
	if err != nil || !present || record.Count != 0 {
		t.Fatalf("durable baseline: %+v, %v, %v", record, present, err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
}
