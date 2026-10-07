package testkit

import (
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// Clock advances only when instructed; shifting wall time never changes timers.
// Advance synchronously delivers all due wakes, so tests need no real sleeps.
type Clock struct {
	mu     sync.Mutex
	now    model.Stamp
	timers map[*timer]struct{}
}

// NewClock starts a clock at monotonic zero and the supplied wall timestamp.
func NewClock(wall time.Time) *Clock {
	return &Clock{now: model.Stamp{Wall: wall}, timers: make(map[*timer]struct{})}
}

// Now returns an atomic copy of wall and monotonic time.
func (c *Clock) Now() model.Stamp {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// NewTimer creates a capacity-one wake channel. Nonpositive delays fire now.
func (c *Clock) NewTimer(delay time.Duration) model.Timer {
	t := &timer{clock: c, wake: make(chan time.Time, 1)}
	t.Reset(delay)
	return t
}

// Advance moves elapsed and wall time forward, rejecting negative durations.
func (c *Clock) Advance(elapsed time.Duration) error {
	if elapsed < 0 {
		return fmt.Errorf("fake monotonic clock cannot move backward")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if elapsed > math.MaxInt64-c.now.Monotonic {
		return fmt.Errorf("fake monotonic clock duration overflow")
	}
	c.now.Monotonic += elapsed
	c.now.Wall = c.now.Wall.Add(elapsed)
	for t := range c.timers {
		if t.deadline <= c.now.Monotonic {
			t.fireLocked()
		}
	}
	return nil
}

// ShiftWall simulates an NTP/manual clock jump without advancing elapsed time.
func (c *Clock) ShiftWall(delta time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now.Wall = c.now.Wall.Add(delta)
}

type timer struct {
	clock    *Clock
	wake     chan time.Time
	deadline time.Duration
}

func (t *timer) C() <-chan time.Time { return t.wake }

func (t *timer) Reset(delay time.Duration) {
	c := t.clock
	c.mu.Lock()
	defer c.mu.Unlock()
	t.stopLocked()
	delay = max(delay, 0)
	t.deadline = math.MaxInt64
	if delay <= math.MaxInt64-c.now.Monotonic {
		t.deadline = c.now.Monotonic + delay
	}
	if t.deadline <= c.now.Monotonic {
		t.fireLocked()
		return
	}
	c.timers[t] = struct{}{}
}

func (t *timer) Stop() {
	t.clock.mu.Lock()
	defer t.clock.mu.Unlock()
	t.stopLocked()
}

func (t *timer) stopLocked() {
	delete(t.clock.timers, t)
	select {
	case <-t.wake:
	default:
	}
}

func (t *timer) fireLocked() {
	delete(t.clock.timers, t)
	select {
	case t.wake <- t.clock.now.Wall:
	default:
	}
}

var _ model.Clock = (*Clock)(nil)
var _ model.Timer = (*timer)(nil)
