package linkmonitor

import (
	"errors"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
)

func testLifecycle(t *testing.T, settle time.Duration) (*lifecycle, *testkit.Clock) {
	t.Helper()
	c, clock := testReconciler(t)
	m := newTestMonitor(t)
	m.cfg.Settle = settle
	m.control.start(t.Context())
	store := &storageExecutor{ctx: t.Context(), inbox: make(chan model.Baseline, 1), results: make(chan model.SaveResult, 1)}
	return &lifecycle{monitor: m, coordinator: c, store: store}, clock
}

func tickLifecycle(t *testing.T, l *lifecycle) {
	t.Helper()
	now := l.coordinator.scheduler.clock.Now()
	if err := l.advance(now); err != nil {
		t.Fatal(err)
	}
	if err := l.publish(now); err != nil {
		t.Fatal(err)
	}
}

func lifecycleDump(t *testing.T, l *lifecycle, records ...model.Observation) {
	t.Helper()
	c := l.coordinator
	advanceReconcile(t, c)
	work := takeInventory(t, c)
	completeInventory(t, c, work, inventoryCompletion{candidate: model.Candidate{Complete: true, Devices: records}})
	tickLifecycle(t, l)
}

func TestLifecycleLearning(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		count                                 int
		settle                                time.Duration
	}{
		{"stable", "positive", "four up links stable for thirty seconds", "final dump then durable four", 4, 30 * time.Second},
		{"empty", "boundary", "verified empty inventory", "learn zero only after final dump", 0, 30 * time.Second},
		{"immediate", "boundary", "zero settle duration", "two reconciliations without delay", 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			l, clock := testLifecycle(t, tc.settle)
			records := make([]model.Observation, tc.count)
			for i := range records {
				records[i] = observed(l.coordinator.scheduler.reducer, uint32(i+1), true)
			}
			tickLifecycle(t, l)
			if _, known := l.monitor.Snapshot().Counts().Current(); known {
				t.Fatal("startup empty mistaken for complete inventory")
			}
			lifecycleDump(t, l, records...)
			if len(l.store.inbox) != 0 {
				t.Fatal("saved before final reconciliation")
			}
			advanceClock(t, clock, tc.settle)
			tickLifecycle(t, l)
			lifecycleDump(t, l, records...)
			record := receiveTest(t, l.store.inbox)
			if record.Count != uint64(tc.count) || record.Version != 1 || record.RecordedAt.IsZero() {
				t.Fatalf("bad record: %+v", record)
			}
			if l.monitor.Health().BaselineReady {
				t.Fatal("baseline published before durable save")
			}
			if err := l.saved(model.SaveResult{Outcome: model.SaveDurable}); err != nil {
				t.Fatal(err)
			}
			tickLifecycle(t, l)
			if negative, delta, known := l.monitor.Snapshot().Counts().Delta(); negative || delta != 0 || !known {
				t.Fatal("durable baseline/delta unavailable")
			}
		})
	}
}

func advanceClock(t *testing.T, clock *testkit.Clock, duration time.Duration) {
	t.Helper()
	if err := clock.Advance(duration); err != nil {
		t.Fatal(err)
	}
}

func TestLifecycleSettleChanges(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		change                                func(*model.Observation)
		restart                               bool
	}{
		{"down", "corner", "link down at 29.999 seconds", "restart settle", func(o *model.Observation) { o.Device.Up = presentValue(false) }, true},
		{"replacement", "corner", "replacement with the same count", "restart settle", func(o *model.Observation) { o.Device.HardwareID = "replacement" }, true},
		{"unknown", "negative", "required operational state becomes unknown", "invalidate settle", func(o *model.Observation) { o.Device.Up.Present = false }, true},
		{"rename", "positive", "only interface name changes", "retain settle deadline", func(o *model.Observation) { o.Device.Name = "renamed" }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			l, clock := testLifecycle(t, 30*time.Second)
			o := observed(l.coordinator.scheduler.reducer, 1, true)
			o.Device.HardwareID = "original"
			lifecycleDump(t, l, o)
			advanceClock(t, clock, 30*time.Second-time.Millisecond)
			tc.change(&o)
			deliverEvent(t, l.coordinator, model.EventChange, o)
			tickLifecycle(t, l)
			advanceClock(t, clock, time.Millisecond)
			tickLifecycle(t, l)
			if l.verifying == tc.restart {
				t.Fatalf("verification=%v", l.verifying)
			}
			if len(l.store.inbox) != 0 {
				t.Fatal("premature save")
			}
		})
	}
}

func TestLifecycleCapturedRecordRetries(t *testing.T) {
	l, clock := testLifecycle(t, 0)
	o := observed(l.coordinator.scheduler.reducer, 1, true)
	lifecycleDump(t, l, o)
	lifecycleDump(t, l, o)
	want := receiveTest(t, l.store.inbox)
	o.Device.Up = presentValue(false)
	deliverEvent(t, l.coordinator, model.EventChange, o)
	for i, delay := range []int{1, 2, 4, 8, 16, 30, 30} {
		outcome := model.SaveFailed
		if i%2 != 0 {
			outcome = model.SaveIndeterminate
		}
		if err := l.saved(model.SaveResult{Outcome: outcome, Err: errors.New("scripted storage failure")}); err != nil {
			t.Fatal(err)
		}
		before := l.monitor.Snapshot()
		tickLifecycle(t, l)
		if len(l.store.inbox) != 0 {
			t.Fatal("retry ignored backoff")
		}
		if l.monitor.Snapshot().BaselineWriteErrors() != uint64(i+1) || before.BaselineWriteErrors() != uint64(i) {
			t.Fatal("error count or immutable snapshot changed")
		}
		advanceClock(t, clock, time.Duration(delay)*time.Second)
		tickLifecycle(t, l)
		if got := receiveTest(t, l.store.inbox); got != want {
			t.Fatal("retry changed intended record")
		}
	}
	if err := l.saved(model.SaveResult{Outcome: model.SaveDurable}); err != nil {
		t.Fatal(err)
	}
	tickLifecycle(t, l)
	if negative, delta, known := l.monitor.Snapshot().Counts().Delta(); !negative || delta != 1 || !known {
		t.Fatal("post-dispatch event did not become delta")
	}
}

func TestLifecycleLoadedBaselineAndControls(t *testing.T) {
	l, _ := testLifecycle(t, 30*time.Second)
	l.expected = presentValue(uint64(4))
	tickLifecycle(t, l)
	if count, known := l.monitor.Snapshot().Counts().Expected(); count != 4 || !known {
		t.Fatal("loaded baseline hidden")
	}
	if _, _, known := l.monitor.Snapshot().Counts().Delta(); known {
		t.Fatal("delta before inventory")
	}
	for _, count := range []int{3, 5} {
		records := make([]model.Observation, count)
		for i := range records {
			records[i] = observed(l.coordinator.scheduler.reducer, uint32(i+1), true)
		}
		l.coordinator.requestResync()
		lifecycleDump(t, l, records...)
		negative, delta, known := l.monitor.Snapshot().Counts().Delta()
		if negative != (count == 3) || delta != 1 || !known || len(l.store.inbox) != 0 {
			t.Fatal("restart implicitly relearned count")
		}
	}
	if err := l.monitor.RequestRebaseline(); err != nil {
		t.Fatal(err)
	}
	if err := l.control(); err != nil {
		t.Fatal(err)
	}
	for range 64 {
		if err := l.monitor.RequestRebaseline(); err != nil {
			t.Fatal(err)
		}
	}
	if l.monitor.control.begin() != 0 {
		t.Fatal("duplicate rebaseline queued")
	}
	lifecycleDump(t, l)
	// The first fresh dump changed membership; verify that new observation again.
	lifecycleDump(t, l)
	if record := receiveTest(t, l.store.inbox); record.Count != 0 {
		t.Fatal("explicit rebaseline did not capture fresh zero")
	}
	if err := l.saved(model.SaveResult{Outcome: model.SaveDurable}); err != nil {
		t.Fatal(err)
	}
	tickLifecycle(t, l)
	if err := l.monitor.RequestResync(); err != nil {
		t.Fatal(err)
	}
	if err := l.control(); err != nil {
		t.Fatal(err)
	}
	lifecycleDump(t, l)
	if l.resync || len(l.store.inbox) != 0 {
		t.Fatal("resync remained active or triggered save")
	}
}
