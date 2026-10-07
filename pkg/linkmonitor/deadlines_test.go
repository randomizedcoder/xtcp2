package linkmonitor

import (
	"math"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
)

func timerKey(i uint32) deadlineKey {
	return deadlineKey{job: model.JobKey{Device: model.DeviceKey{Index: i}}}
}

func TestDeadlineQueue(t *testing.T) {
	for _, tc := range []struct {
		name, description, expected string
		initial, replacement, now   time.Duration
		cancel, due                 bool
	}{
		{"before", "time before deadline", "retain pending entry", 10, 10, 9, false, false},
		{"at", "time equals deadline", "deliver exactly once", 10, 10, 10, false, true},
		{"earlier", "replace deadline with earlier value", "deliver replacement", 100, 5, 5, false, true},
		{"later", "replace deadline with later value", "do not deliver obsolete entry", 5, 100, 5, false, false},
		{"cancel", "cancel scheduled key", "no retained entry", 10, 10, 10, true, false},
		{"maximum", "deadline at maximum duration", "deliver without arithmetic wrap", math.MaxInt64, math.MaxInt64, math.MaxInt64, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("boundary/corner: %s; expected: %s", tc.description, tc.expected)
			var q deadlineQueue
			key := timerKey(1)
			q.set(deadlineEntry{key: key, at: tc.initial, generation: 1})
			q.set(deadlineEntry{key: key, at: tc.replacement, generation: 2})
			if len(q.entries) != 1 || len(q.index) != 1 {
				t.Fatal("replacement accumulated obsolete entry")
			}
			if tc.cancel {
				q.cancel(key)
			}
			entry, due := q.due(tc.now)
			if due != tc.due || (due && (entry.at != tc.replacement || entry.generation != 2)) {
				t.Fatalf("entry=%+v due=%v", entry, due)
			}
			if _, again := q.due(tc.now); again {
				t.Fatal("deadline delivered twice")
			}
			q.cancel(key)
			if q.entries != nil || q.index != nil {
				t.Fatal("empty queue retains historical identities")
			}
		})
	}
}

func TestDeadlineQueueReference(t *testing.T) {
	t.Log("corner/churn: interleave insertion, replacement and removal; expected: indexed heap agrees with independent minimum scan")
	var q deadlineQueue
	want := make(map[deadlineKey]time.Duration)
	for i := range 2000 {
		key := timerKey(uint32(i * 37 % 127))
		if i%4 == 0 {
			q.cancel(key)
			delete(want, key)
		} else {
			at := time.Duration(i * 67 % 97)
			q.set(deadlineEntry{key: key, at: at})
			want[key] = at
		}
		if len(q.index) != len(want) || len(q.entries) != len(want) {
			t.Fatal("queue retained canceled/replaced entries")
		}
		minimum := time.Duration(math.MaxInt64)
		for _, at := range want {
			minimum = min(minimum, at)
		}
		first, exists := q.first()
		if exists != (len(want) > 0) || (exists && first.at != minimum) {
			t.Fatalf("minimum=%v, want %v", first.at, minimum)
		}
		for j := range q.entries {
			entry := &q.entries[j]
			if q.index[entry.key] != j || want[entry.key] != entry.at {
				t.Fatal("index points to the wrong heap entry")
			}
		}
	}
	for key := range want {
		q.cancel(key)
	}
	if cap(q.entries) != 0 || q.index != nil {
		t.Fatal("queue retained churn high-water storage")
	}
}

type timerClock struct {
	*testkit.Clock
	created, resets int
}

func (c *timerClock) NewTimer(d time.Duration) model.Timer {
	c.created++
	return &countedTimer{Timer: c.Clock.NewTimer(d), owner: c}
}

type countedTimer struct {
	model.Timer
	owner *timerClock
}

func (t *countedTimer) Reset(d time.Duration) { t.owner.resets++; t.Timer.Reset(d) }

func TestDeadlineWake(t *testing.T) {
	t.Log("corner: reschedule earliest deadline and jump wall time; expected: one timer, replacement wake only, no reset for unchanged deadline")
	c := &timerClock{Clock: testkit.NewClock(time.Unix(100, 0))}
	w := deadlineWake{clock: c}
	var q deadlineQueue
	q.set(deadlineEntry{key: timerKey(1), at: 100})
	w.sync(&q)
	w.sync(&q)
	if c.created != 1 || c.resets != 0 {
		t.Fatal("unchanged deadline allocated/reset a timer")
	}
	q.set(deadlineEntry{key: timerKey(1), at: 10})
	w.sync(&q)
	c.ShiftWall(-24 * time.Hour)
	if err := c.Advance(9); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.channel():
		t.Fatal("wall jump or replaced deadline fired early")
	default:
	}
	if err := c.Advance(1); err != nil {
		t.Fatal(err)
	}
	select {
	case wall := <-w.channel():
		if !wall.Equal(c.Now().Wall) {
			t.Fatal("timer wake lost current wall timestamp")
		}
	default:
		t.Fatal("deadline did not fire at boundary")
	}
	w.consumed()
	if _, exists := q.due(c.Now().Monotonic); !exists {
		t.Fatal("timer woke without due work")
	}
	w.sync(&q)
	if w.channel() != nil || c.created != 1 || c.resets != 1 {
		t.Fatal("empty queue retained wake or allocated replacement timer")
	}
	q.set(deadlineEntry{key: timerKey(2), at: 20})
	w.sync(&q)
	w.stop()
	if c.created != 1 || w.channel() != nil {
		t.Fatal("stopped scheduler retained a selectable wake")
	}
}

func TestFreshnessConfigBoundaries(t *testing.T) {
	for _, tc := range []struct {
		name, description, expected string
		stats, resync               time.Duration
		valid                       bool
	}{
		{"minimum", "one nanosecond intervals", "accept without rounding", 1, 1, true},
		{"maximum", "largest representable expiry multipliers", "accept", math.MaxInt64 / 3, (math.MaxInt64 - 1) / 2, true},
		{"stats overflow", "three poll intervals overflow", "reject", math.MaxInt64/3 + 1, 1, false},
		{"resync overflow", "two resync intervals overflow", "reject", 1, math.MaxInt64/2 + 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("boundary: %s; expected: %s", tc.description, tc.expected)
			cfg := DefaultConfig()
			cfg.StatsInterval, cfg.Resync = tc.stats, tc.resync
			if _, err := New(cfg, Options{}); (err == nil) != tc.valid {
				t.Fatalf("configuration error=%v", err)
			}
		})
	}
	if got := deadlineAfter(math.MaxInt64-2, 3); got != math.MaxInt64 {
		t.Fatalf("deadline overflow=%v", got)
	}
}
