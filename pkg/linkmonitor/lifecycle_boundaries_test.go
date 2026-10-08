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

func TestLifecycleFreshRebaseline(t *testing.T) {
	t.Log("corner: rebaseline joins while an older dump is in flight; expected: older success cannot authorize save")
	l, _ := testLifecycle(t, time.Hour)
	l.expected = presentValue(uint64(4))
	c := l.coordinator
	advanceReconcile(t, c)
	older := takeInventory(t, c)
	if err := l.monitor.RequestRebaseline(); err != nil {
		t.Fatal(err)
	}
	if err := l.control(); err != nil {
		t.Fatal(err)
	}
	completeInventory(t, c, older, inventoryCompletion{candidate: model.Candidate{Complete: true}})
	tickLifecycle(t, l)
	if len(l.store.inbox) != 0 {
		t.Fatal("pre-request dump authorized rebaseline")
	}
	lifecycleDump(t, l)
	if got := receiveTest(t, l.store.inbox); got.Count != 0 {
		t.Fatal("fresh zero not captured")
	}
}

func TestLifecycleLearningLossAndRDMA(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		rdma, loss                            bool
	}{
		{"loss", "negative", "source epoch lost during settle", "discard settle and require new complete inventory", false, true},
		{"native RDMA", "positive", "known native port state without optional settings", "learn one without optional statistics", true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			l, clock := testLifecycle(t, 30*time.Second)
			c := l.coordinator
			o := observed(c.scheduler.reducer, 1, true)
			if tc.rdma {
				o.Device.Key = model.DeviceKey{Namespace: 1, Kind: model.DeviceNativeRDMA, RDMADevice: "mlx5_0", Port: 1}
				o.Device.Name = "rdma:mlx5_0:1"
				c.scheduler.reducer.rdmaEvents = true
			}
			lifecycleDump(t, l, o)
			if tc.loss {
				c.inbox.lose(c.scheduler.reducer.epoch)
				if err := c.syncLoss(); err != nil {
					t.Fatal(err)
				}
			}
			advanceClock(t, clock, 30*time.Second)
			tickLifecycle(t, l)
			if tc.loss {
				if l.settling || l.verifying || len(l.store.inbox) != 0 {
					t.Fatal("loss retained permission to learn")
				}
			} else {
				lifecycleDump(t, l, o)
				if record := receiveTest(t, l.store.inbox); record.Count != 1 {
					t.Fatal("native port omitted from baseline")
				}
			}
		})
	}
}

func TestLifecycleSaveErrorBoundary(t *testing.T) {
	t.Log("boundary: error counter at uint64 maximum; expected: explicit exhaustion without wrap or baseline replacement")
	l, _ := testLifecycle(t, 0)
	l.intended = &model.Baseline{Count: 4}
	l.writeErrors = math.MaxUint64
	if err := l.saved(model.SaveResult{}); !errors.Is(err, errSequenceExhausted) || l.expected.Present || l.writeErrors != math.MaxUint64 {
		t.Fatal("error counter wrapped or failed save published")
	}
	if (Snapshot{}).BaselineWriteErrors() != 0 {
		t.Fatal("empty snapshot has errors")
	}
}

func TestLifecycleCancellationDuringDispatch(t *testing.T) {
	t.Log("corner: cancellation arrives between owner turn entry and inventory dispatch; expected: cancellation, not a spurious occupied-executor failure")
	c, _ := testReconciler(t)
	c.inventory.cancel()
	if err := c.submit(model.DeviceKey{}, false, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("dispatch error: %v", err)
	}
}

func TestLifecycleOldBaselineSurvivesFailure(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		outcome                               model.SaveOutcome
	}{
		{"definite", "negative", "replacement save fails before rename", "retain four and retry intended zero", model.SaveFailed},
		{"indeterminate", "corner", "replacement may already be visible on disk", "retain four and retry identical record", model.SaveIndeterminate},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			l, clock := testLifecycle(t, 0)
			l.expected = presentValue(uint64(4))
			lifecycleDump(t, l)
			if err := l.monitor.RequestRebaseline(); err != nil {
				t.Fatal(err)
			}
			if err := l.control(); err != nil {
				t.Fatal(err)
			}
			lifecycleDump(t, l)
			record := receiveTest(t, l.store.inbox)
			if err := l.saved(model.SaveResult{Outcome: tc.outcome}); err != nil {
				t.Fatal(err)
			}
			tickLifecycle(t, l)
			if value, known := l.monitor.Snapshot().Counts().Expected(); !known || value != 4 {
				t.Fatal("failed write replaced baseline")
			}
			if err := l.monitor.RequestRebaseline(); err != nil {
				t.Fatal(err)
			}
			if l.monitor.control.begin() != 0 {
				t.Fatal("retry accepted a second operation")
			}
			advanceClock(t, clock, time.Second)
			tickLifecycle(t, l)
			if got := receiveTest(t, l.store.inbox); got != record {
				t.Fatal("retry target changed")
			}
		})
	}
}

func TestLifecycleShortFlapRestartsSettle(t *testing.T) {
	t.Log("corner: down/up returns to the original count within one owner turn; expected: restart settling despite equal final count")
	l, clock := testLifecycle(t, 30*time.Second)
	o := observed(l.coordinator.scheduler.reducer, 1, true)
	lifecycleDump(t, l, o)
	advanceClock(t, clock, 29*time.Second)
	down := o
	down.Device.Up = presentValue(false)
	deliverEvent(t, l.coordinator, model.EventChange, down)
	deliverEvent(t, l.coordinator, model.EventChange, o)
	tickLifecycle(t, l)
	if l.settleAt != 59*time.Second || l.verifying {
		t.Fatal("short flap did not restart settle")
	}
}

func TestLifecycleLoadPublicationAndCancellation(t *testing.T) {
	t.Log("boundary: loaded baseline with blocked acquisition; expected: expected count available, controls/delta unavailable, bounded clean cancellation")
	s, m, _ := sessionFixture(t)
	store := s.store.(testkit.StoreFuncs)
	store.LoadFunc = func(context.Context) (model.Baseline, bool, error) {
		return model.Baseline{Version: 1, Count: 4, RecordedAt: time.Unix(1, 0)}, true, nil
	}
	s.store = store
	s.inventory = func(ctx context.Context) (inventoryBackend, error) { <-ctx.Done(); return nil, ctx.Err() }
	cancel, done := runSession(t, m)
	snapshot := awaitSnapshot(t, m, func(s Snapshot) bool { value, known := s.Counts().Expected(); return known && value == 4 })
	if _, _, known := snapshot.Counts().Delta(); known {
		t.Fatal("delta available without inventory")
	}
	if err := m.RequestResync(); !errors.Is(err, ErrNotRunning) {
		t.Fatal("control accepted during acquisition")
	}
	cancel()
	if err := receiveTest(t, done); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleSaveCompletesDuringShutdown(t *testing.T) {
	t.Log("corner: cancellation during a save that returns durable before grace; expected: retain durable outcome and join before return")
	s, m, clock := sessionFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	store := s.store.(testkit.StoreFuncs)
	store.SaveFunc = func(context.Context, model.Baseline) model.SaveResult {
		close(entered)
		<-release
		return model.SaveResult{Outcome: model.SaveDurable}
	}
	s.store = store
	cancel, done := runSession(t, m)
	receiveTest(t, entered)
	drainGrace(clock)
	cancel()
	receiveTest(t, clock.grace)
	close(release)
	if err := receiveTest(t, done); err != nil {
		t.Fatal(err)
	}
	if value, known := m.Snapshot().Counts().Expected(); !known || value != 0 {
		t.Fatal("durable outcome lost during shutdown")
	}
}
