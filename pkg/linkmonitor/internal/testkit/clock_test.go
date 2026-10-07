package testkit

import (
	"math"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func TestClockOverflowBoundary(t *testing.T) {
	c := NewClock(time.Unix(100, 0))
	if err := c.Advance(math.MaxInt64 - 1); err != nil {
		t.Fatal(err)
	}
	timer := c.NewTimer(time.Second)
	if hasWake(timer) {
		t.Fatal("overflow caused premature wake")
	}
	if err := c.Advance(2); err == nil {
		t.Fatal("elapsed overflow accepted")
	}
	if err := c.Advance(1); err != nil {
		t.Fatal(err)
	}
	if !hasWake(timer) {
		t.Fatal("saturated deadline did not fire at the representable limit")
	}
}

func TestClockDeadlines(t *testing.T) {
	tests := []struct {
		name, category, description, expected string
		delay, advance                        time.Duration
		change                                func(*Clock, model.Timer)
		want                                  bool
	}{
		{"before", "boundary", "one nanosecond before deadline", "no wake", time.Second, time.Second - 1, nil, false},
		{"exact", "boundary", "exactly at deadline", "one wake", time.Second, time.Second, nil, true},
		{"after", "positive", "advance far past deadline", "one wake, not catch-up wakes", time.Second, time.Hour, nil, true},
		{"immediate", "boundary", "zero delay", "one immediate wake", 0, 0, nil, true},
		{"negative", "boundary", "negative delay is immediately due", "one immediate wake", -1, 0, nil, true},
		{"stop", "positive", "stop before due", "no wake", time.Second, time.Hour, func(_ *Clock, timer model.Timer) { timer.Stop() }, false},
		{"reset", "corner", "reset replaces the earlier deadline", "no wake at old deadline", time.Second, time.Second, func(_ *Clock, timer model.Timer) { timer.Reset(2 * time.Second) }, false},
		{"pending reset", "corner", "reset after a due wake was queued", "old wake removed", 0, time.Second, func(_ *Clock, timer model.Timer) { timer.Reset(2 * time.Second) }, false},
		{"pending stop", "corner", "stop after due wake was queued", "old wake removed", 0, time.Second, func(_ *Clock, timer model.Timer) { timer.Stop() }, false},
		{"wall forward", "corner", "jump wall time forward one day", "elapsed deadline unchanged", time.Second, time.Second - 1, func(c *Clock, _ model.Timer) { c.ShiftWall(24 * time.Hour) }, false},
		{"wall backward", "corner", "jump wall time backward one day", "elapsed deadline still fires", time.Second, time.Second, func(c *Clock, _ model.Timer) { c.ShiftWall(-24 * time.Hour) }, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			clock := NewClock(time.Unix(100, 0))
			timer := clock.NewTimer(tc.delay)
			if tc.change != nil {
				tc.change(clock, timer)
			}
			if err := clock.Advance(tc.advance); err != nil {
				t.Fatal(err)
			}
			if got := hasWake(timer); got != tc.want {
				t.Fatalf("wake = %v, want %v", got, tc.want)
			}
			if err := clock.Advance(time.Hour); err != nil {
				t.Fatal(err)
			}
			if tc.want && hasWake(timer) {
				t.Fatal("one-shot timer fired twice")
			}
		})
	}
}

func hasWake(timer model.Timer) bool {
	select {
	case <-timer.C():
		return true
	default:
		return false
	}
}

func TestClockRejectsBackwardElapsedTime(t *testing.T) {
	c := NewClock(time.Unix(100, 0))
	before := c.Now()
	if err := c.Advance(-1); err == nil {
		t.Fatal("negative elapsed time accepted")
	}
	if c.Now() != before {
		t.Fatal("rejected advance changed clock")
	}
	c.ShiftWall(time.Hour)
	if now := c.Now(); now.Monotonic != before.Monotonic || !now.Wall.Equal(before.Wall.Add(time.Hour)) {
		t.Fatal("wall and monotonic clocks coupled")
	}
}

func TestClockSimultaneousTimers(t *testing.T) {
	c := NewClock(time.Unix(100, 0))
	first, second := c.NewTimer(time.Second), c.NewTimer(time.Second)
	if err := c.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	if !hasWake(first) || !hasWake(second) {
		t.Fatal("same-deadline timer starved")
	}
	first.Reset(time.Second)
	if err := c.Advance(time.Second); err != nil {
		t.Fatal(err)
	}
	if !hasWake(first) || hasWake(second) {
		t.Fatal("reset resurrected unrelated timer")
	}
}
