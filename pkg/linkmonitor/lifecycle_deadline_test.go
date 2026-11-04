package linkmonitor

import (
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// deadlineRecords is two up links; stuck makes the first an RDMA device with
// no RDMA events, so the count never becomes ready (a stuck RDMA port, a
// failed event source).
func deadlineRecords(l *lifecycle, stuck bool) []model.Observation {
	r := l.coordinator.scheduler.reducer
	a, b := observed(r, 1, true), observed(r, 2, true)
	a.Device.RDMA = stuck
	return []model.Observation{a, b}
}

// TestLifecycleLearnDeadline: when the count can't become ready, a learn
// deadline records a provisional, in-memory baseline from the visible up
// links, so up_links_delta still reports what it can.
func TestLifecycleLearnDeadline(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		deadline, elapsed                            time.Duration
		stuck, dump, lose                            bool
		loaded                                       model.Optional[uint64]
		provisional                                  bool
		expected                                     model.Optional[uint64]
	}{
		{"stuck", "positive", "count stuck not ready, an hour after start", "provisional baseline of the two visible up links", 5 * time.Minute, time.Hour, true, true, false, model.Optional[uint64]{}, true, presentValue(uint64(2))},
		{"disabled", "negative", "deadline 0, count stuck for an hour", "no baseline, as before the deadline existed", 0, time.Hour, true, true, false, model.Optional[uint64]{}, false, model.Optional[uint64]{}},
		{"no-resync", "negative", "no complete inventory, past the deadline", "no baseline: nothing honest to report", 5 * time.Minute, time.Hour, true, false, false, model.Optional[uint64]{}, false, model.Optional[uint64]{}},
		{"events-lost", "negative", "count stuck, then the event source lost: the inventory is from an old epoch", "no baseline until a new complete inventory", 5 * time.Minute, time.Hour, true, true, true, model.Optional[uint64]{}, false, model.Optional[uint64]{}},
		{"just-before", "boundary", "count stuck, 1 ns before the deadline", "no baseline yet", 5 * time.Minute, 5*time.Minute - time.Nanosecond, true, true, false, model.Optional[uint64]{}, false, model.Optional[uint64]{}},
		{"at-deadline", "boundary", "count stuck, exactly at the deadline", "provisional baseline", 5 * time.Minute, 5 * time.Minute, true, true, false, model.Optional[uint64]{}, true, presentValue(uint64(2))},
		{"ready", "corner", "count ready, past the deadline", "normal learning owns it; nothing provisional", 5 * time.Minute, time.Hour, false, true, false, model.Optional[uint64]{}, false, model.Optional[uint64]{}},
		{"loaded", "corner", "durable baseline loaded from disk, count stuck", "the loaded baseline stands; nothing provisional", 5 * time.Minute, time.Hour, true, true, false, presentValue(uint64(3)), false, presentValue(uint64(3))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			l, clock := testLifecycle(t, 30*time.Second)
			l.monitor.cfg.LearnDeadline = tc.deadline
			l.expected = tc.loaded
			tickLifecycle(t, l)
			if tc.dump {
				lifecycleDump(t, l, deadlineRecords(l, tc.stuck)...)
			}
			if tc.lose {
				if err := l.coordinator.scheduler.reducer.loseEvents(); err != nil {
					t.Fatal(err)
				}
			}
			advanceClock(t, clock, tc.elapsed)
			tickLifecycle(t, l)
			s := l.monitor.Snapshot()
			expected, known := s.Counts().Expected()
			if s.Health().BaselineProvisional != tc.provisional || l.provisional != tc.provisional ||
				known != tc.expected.Present || expected != tc.expected.Value {
				t.Fatalf("%s: provisional=%v expected=%d/%v", tc.expectedOutcome, s.Health().BaselineProvisional, expected, known)
			}
			if s.Health().BaselineReady != tc.expected.Present {
				t.Fatalf("%s: baseline_ready=%v", tc.expectedOutcome, s.Health().BaselineReady)
			}
			if len(l.store.inbox) != 0 {
				t.Fatal("a provisional or unready baseline reached the file")
			}
		})
	}
}

// TestLearnDeadlineWakeup: the owner loop must wake at the deadline, or in
// on-demand mode the provisional baseline would wait for the next hourly
// resync.
func TestLearnDeadlineWakeup(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		deadline                                     time.Duration
		provisional                                  bool
		wake                                         bool
	}{
		{"enabled", "positive", "count stuck, deadline 5 min", "lifecycle wakes at start + 5 min", 5 * time.Minute, false, true},
		{"disabled", "negative", "count stuck, deadline 0", "no lifecycle wake", 0, false, false},
		{"after", "corner", "already provisional", "no deadline wake left", 5 * time.Minute, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			l, clock := testLifecycle(t, 30*time.Second)
			l.monitor.cfg.LearnDeadline = tc.deadline
			start := clock.Now().Monotonic
			tickLifecycle(t, l)
			lifecycleDump(t, l, deadlineRecords(l, true)...)
			if tc.provisional {
				advanceClock(t, clock, tc.deadline)
				tickLifecycle(t, l)
				if !l.provisional {
					t.Fatal("setup: not provisional")
				}
			}
			q := &l.coordinator.scheduler.reducer.deadlines
			i, ok := q.index[deadlineKey{kind: deadlineLifecycle}]
			if ok != tc.wake || (ok && q.entries[i].at != start+tc.deadline) {
				t.Fatalf("%s: wake=%v", tc.expectedOutcome, ok)
			}
		})
	}
}

// TestLearnDeadlineUpgrade: a provisional baseline is never the end state. It
// is replaced by normal learning once the count is ready, and it holds still
// meanwhile, so a link lost after it was taken shows as a negative delta.
func TestLearnDeadlineUpgrade(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		linkDown, ready                              bool
		delta                                        int64
		written                                      bool
	}{
		{"upgrade", "positive", "provisional, then the count becomes ready", "durable baseline written; provisional cleared", false, true, 0, true},
		{"link-down", "negative", "provisional, then a counted link goes down", "baseline holds at 2, delta -1, nothing written", true, false, -1, false},
		{"still-stuck", "boundary", "provisional and still not ready an hour later", "unchanged provisional, nothing written", false, false, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			l, clock := testLifecycle(t, 30*time.Second)
			l.monitor.cfg.LearnDeadline = 5 * time.Minute
			r := l.coordinator.scheduler.reducer
			tickLifecycle(t, l)
			records := deadlineRecords(l, true)
			lifecycleDump(t, l, records...)
			advanceClock(t, clock, 5*time.Minute)
			tickLifecycle(t, l)
			if !l.provisional {
				t.Fatal("setup: not provisional")
			}
			if tc.linkDown {
				records[1].Device.Up = presentValue(false)
				deliverEvent(t, l.coordinator, model.EventChange, records[1])
			}
			if tc.ready {
				r.rdmaEventsUnsupported = true
			}
			tickLifecycle(t, l)
			advanceClock(t, clock, time.Hour)
			tickLifecycle(t, l)
			lifecycleDump(t, l, records...)
			written := len(l.store.inbox) != 0
			if written != tc.written {
				t.Fatalf("%s: written=%v", tc.expectedOutcome, written)
			}
			if written {
				record := receiveTest(t, l.store.inbox)
				if record.Count != 2 {
					t.Fatalf("%s: count %d", tc.expectedOutcome, record.Count)
				}
				if err := l.saved(model.SaveResult{Outcome: model.SaveDurable}); err != nil {
					t.Fatal(err)
				}
				tickLifecycle(t, l)
			}
			h := l.monitor.Snapshot().Health()
			if h.BaselineProvisional == tc.written || !h.BaselineReady {
				t.Fatalf("%s: health=%+v", tc.expectedOutcome, h)
			}
			if negative, magnitude, known := l.monitor.Snapshot().Counts().Delta(); !known || (negative && -int64(magnitude) != tc.delta) || (!negative && int64(magnitude) != tc.delta) {
				t.Fatalf("%s: delta negative=%v magnitude=%d known=%v", tc.expectedOutcome, negative, magnitude, known)
			}
		})
	}
}

func TestLearnDeadlineConfig(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		deadline                                     time.Duration
		valid                                        bool
	}{
		{"default", "positive", "DefaultConfig", "5 min, valid", DefaultConfig().LearnDeadline, true},
		{"disabled", "boundary", "zero", "valid: wait for a ready count forever", 0, true},
		{"one-ns", "boundary", "1 ns", "valid", time.Nanosecond, true},
		{"negative", "negative", "-1 ns", "rejected", -time.Nanosecond, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			if tc.name == "default" && tc.deadline != 5*time.Minute {
				t.Fatalf("default %v", tc.deadline)
			}
			cfg := DefaultConfig()
			cfg.LearnDeadline = tc.deadline
			if _, err := validateConfig(cfg); (err == nil) != tc.valid {
				t.Fatalf("%s: %v", tc.expectedOutcome, err)
			}
		})
	}
}
