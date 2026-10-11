package linkmonitor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// startedMonitor returns a monitor whose controls accept requests without a
// session, so tests drive begin and complete by hand.
func startedMonitor(t *testing.T) *Monitor {
	t.Helper()
	m := newTestMonitor(t)
	m.control.start(t.Context())
	return m
}

// pass begins pending work and completes it, as one session publication does.
func pass(m *Monitor) requestKind {
	work := m.control.begin()
	m.control.complete(work)
	return work
}

func shortContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

func TestControlTickets(t *testing.T) {
	tests := []struct {
		category    string
		description string
		run         func(*testing.T, *Monitor) error
		expected    error
	}{
		{"positive", "Await returns once the pass that began after Submit completes", func(t *testing.T, m *Monitor) error {
			ticket, err := m.Submit(ControlRebaseline)
			if err != nil {
				return err
			}
			pass(m)
			return m.Await(shortContext(t), ticket)
		}, nil},
		{"positive", "two submits before the pass begins share one generation", func(t *testing.T, m *Monitor) error {
			first, _ := m.Submit(ControlResync)
			second, _ := m.Submit(ControlResync)
			if first != second || first.Generation() != 1 || first.Kind() != ControlResync {
				t.Errorf("tickets %+v and %+v differ", first, second)
			}
			pass(m)
			return m.Await(shortContext(t), second)
		}, nil},
		{"positive", "a waiter blocked before completion is released by it", func(t *testing.T, m *Monitor) error {
			ticket, _ := m.Submit(ControlResync)
			done := make(chan error, 1)
			go func() { done <- m.Await(t.Context(), ticket) }()
			pass(m)
			return await(t, done)
		}, nil},
		{"negative", "Await before completion times out with the context error", func(t *testing.T, m *Monitor) error {
			ticket, _ := m.Submit(ControlRebaseline)
			return m.Await(shortContext(t), ticket)
		}, context.DeadlineExceeded},
		{"negative", "stop releases waiters with ErrNotRunning", func(t *testing.T, m *Monitor) error {
			ticket, _ := m.Submit(ControlRebaseline)
			done := make(chan error, 1)
			go func() { done <- m.Await(t.Context(), ticket) }()
			m.control.stop()
			return await(t, done)
		}, ErrNotRunning},
		{"negative", "Submit before start is ErrNotRunning", func(t *testing.T, _ *Monitor) error {
			_, err := newTestMonitor(t).Submit(ControlResync)
			return err
		}, ErrNotRunning},
		{"negative", "an unknown kind is rejected", func(_ *testing.T, m *Monitor) error {
			_, err := m.Submit(ControlKind(9))
			return err
		}, ErrUnknownControl},
		{"negative", "the zero ticket is rejected", func(t *testing.T, m *Monitor) error {
			return m.Await(shortContext(t), ControlTicket{})
		}, ErrUnknownControl},
		{"boundary", "a completed ticket stays satisfied after stop", func(t *testing.T, m *Monitor) error {
			ticket, _ := m.Submit(ControlResync)
			pass(m)
			m.control.stop()
			return m.Await(shortContext(t), ticket)
		}, nil},
		{"boundary", "an already-canceled context still reports a completed ticket", func(t *testing.T, m *Monitor) error {
			ticket, _ := m.Submit(ControlResync)
			pass(m)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			return m.Await(ctx, ticket)
		}, nil},
		{"corner", "a request during an active pass is not satisfied by that pass", func(t *testing.T, m *Monitor) error {
			if _, err := m.Submit(ControlRebaseline); err != nil {
				return err
			}
			work := m.control.begin()
			late, _ := m.Submit(ControlRebaseline)
			if late.Generation() != 2 {
				t.Errorf("late generation = %d, want 2", late.Generation())
			}
			m.control.complete(work)
			return m.Await(shortContext(t), late)
		}, context.DeadlineExceeded},
		{"corner", "the follow-up pass satisfies the late request", func(t *testing.T, m *Monitor) error {
			_, _ = m.Submit(ControlRebaseline)
			work := m.control.begin()
			late, _ := m.Submit(ControlRebaseline)
			m.control.complete(work)
			if pass(m) != rebaselineRequest {
				t.Error("follow-up pass did not begin after completion")
			}
			return m.Await(shortContext(t), late)
		}, nil},
		{"corner", "completing a resync does not release a rebaseline ticket", func(t *testing.T, m *Monitor) error {
			rebaseline, _ := m.Submit(ControlRebaseline)
			_, _ = m.Submit(ControlResync)
			m.control.begin()
			m.control.complete(resyncRequest)
			return m.Await(shortContext(t), rebaseline)
		}, context.DeadlineExceeded},
		{"corner", "completing work that is not active changes nothing", func(t *testing.T, m *Monitor) error {
			ticket, _ := m.Submit(ControlResync)
			m.control.complete(resyncRequest)
			return m.Await(shortContext(t), ticket)
		}, context.DeadlineExceeded},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %v", tc.category, tc.description, tc.expected)
			if err := tc.run(t, startedMonitor(t)); !errors.Is(err, tc.expected) {
				t.Fatalf("error = %v, want %v", err, tc.expected)
			}
		})
	}
}

func TestControlConcurrentSubmitsBounded(t *testing.T) {
	m := startedMonitor(t)
	_, _ = m.Submit(ControlRebaseline)
	work := m.control.begin()
	tickets := make(chan ControlTicket, 256)
	var wg sync.WaitGroup
	for range cap(tickets) {
		wg.Go(func() {
			ticket, err := m.Submit(ControlRebaseline)
			if err != nil {
				t.Error(err)
			}
			tickets <- ticket
		})
	}
	wg.Wait()
	close(tickets)
	for ticket := range tickets {
		if ticket.Generation() != 2 {
			t.Fatalf("concurrent submit generation = %d, want 2", ticket.Generation())
		}
	}
	m.control.complete(work)
	passes := 1
	for pass(m) != 0 {
		passes++
	}
	if passes != 2 {
		t.Fatalf("passes = %d, want 2", passes)
	}
	if got := m.ControlProgress(ControlRebaseline); got != (ControlProgress{Requested: 2, Completed: 2}) {
		t.Fatalf("progress = %+v", got)
	}
}

// TestControlFailingSaveKeepsAwaitBlocked drives the real lifecycle: a
// rebaseline whose save fails must not complete its ticket, and the failure is
// visible to the waiter through the snapshot.
func TestControlFailingSaveKeepsAwaitBlocked(t *testing.T) {
	l, _ := testLifecycle(t, 30*time.Second)
	l.expected = presentValue(uint64(4))
	tickLifecycle(t, l)
	ticket, err := l.monitor.Submit(ControlRebaseline)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.control(); err != nil {
		t.Fatal(err)
	}
	lifecycleDump(t, l)
	receiveTest(t, l.store.inbox)
	if err := l.saved(model.SaveResult{Outcome: model.SaveFailed}); err != nil {
		t.Fatal(err)
	}
	tickLifecycle(t, l)
	if err := l.monitor.Await(shortContext(t), ticket); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Await after failed save = %v, want DeadlineExceeded", err)
	}
	if l.monitor.Snapshot().BaselineWriteErrors() != 1 {
		t.Fatal("failed save not visible to the waiter")
	}
	if got := l.monitor.ControlProgress(ControlRebaseline); got != (ControlProgress{Requested: 1, Completed: 0}) {
		t.Fatalf("progress = %+v", got)
	}
}

func TestControlKindString(t *testing.T) {
	tests := []struct {
		category, description string
		kind                  ControlKind
		expected              string
	}{
		{"positive", "resync", ControlResync, "resync"},
		{"positive", "rebaseline", ControlRebaseline, "rebaseline"},
		{"negative", "zero kind", 0, "unknown"},
		{"corner", "out of range kind", ControlKind(255), "unknown"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			if got := tc.kind.String(); got != tc.expected {
				t.Fatalf("String() = %q", got)
			}
		})
	}
}

// TestControlNoAllocs guards the publication path, which calls complete with
// no finished work on every publish, and Await on a completed ticket.
func TestControlNoAllocs(t *testing.T) {
	m := startedMonitor(t)
	ticket, _ := m.Submit(ControlResync)
	pass(m)
	ctx := t.Context()
	tests := []struct {
		category, description string
		run                   func()
	}{
		{"positive", "complete with no finished work", func() { m.control.complete(0) }},
		{"positive", "Await on a completed ticket", func() { _ = m.Await(ctx, ticket) }},
		{"corner", "ControlProgress", func() { _ = m.ControlProgress(ControlResync) }},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: 0 allocations", tc.category, tc.description)
			if n := testing.AllocsPerRun(100, tc.run); n != 0 {
				t.Fatalf("%.0f allocations per call", n)
			}
		})
	}
}

func BenchmarkControlSubmit(b *testing.B) {
	m, err := New(DefaultConfig(), Options{})
	if err != nil {
		b.Fatal(err)
	}
	m.control.start(b.Context())
	b.ReportAllocs()
	for b.Loop() {
		if _, err := m.Submit(ControlResync); err != nil {
			b.Fatal(err)
		}
		pass(m)
	}
}
