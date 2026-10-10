package linkmonitor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type sessionFunc func(context.Context, *Monitor) error

func (f sessionFunc) Run(ctx context.Context, m *Monitor) error { return f(ctx, m) }

func newTestMonitor(t *testing.T) *Monitor {
	t.Helper()
	m, err := New(DefaultConfig(), Options{})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("test synchronization timed out")
		var zero T
		return zero
	}
}

func TestRunFailures(t *testing.T) {
	startupError := errors.New("scripted startup failure")
	tests := []struct {
		name, category, description, expected string
		startup                               error
		cancel                                bool
	}{
		{"unavailable", "negative", "explicit io_uring transport is not wired", "ErrBackendUnavailable, then ErrAlreadyRun", ErrBackendUnavailable, false},
		{"startup failure", "negative", "resource acquisition fails", "original error, then ErrAlreadyRun", startupError, false},
		{"already canceled", "boundary", "context canceled before acquisition", "context.Canceled without acquisition", context.Canceled, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			m := newTestMonitor(t)
			m.cfg.IOBackend = IOBackendIOUring
			calls := 0
			if tc.startup != ErrBackendUnavailable {
				m.open = func(context.Context) (session, error) { calls++; return nil, tc.startup }
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.cancel {
				cancel()
			}
			if err := m.Run(ctx); !errors.Is(err, tc.startup) {
				t.Fatalf("Run = %v", err)
			}
			if tc.cancel && calls != 0 {
				t.Fatal("acquired resources after cancellation")
			}
			if err := m.Run(ctx); !errors.Is(err, ErrAlreadyRun) {
				t.Fatalf("second Run = %v", err)
			}
			if m.Health() != (Health{}) {
				t.Fatal("failed startup became healthy")
			}
			if !errors.Is(m.RequestResync(), ErrNotRunning) || !errors.Is(m.RequestRebaseline(), ErrNotRunning) {
				t.Fatal("failed monitor accepts controls")
			}
		})
	}
}

func TestLifecycleAndConcurrentControls(t *testing.T) {
	m := newTestMonitor(t)
	before := m.Snapshot()
	if !errors.Is(m.RequestResync(), ErrNotRunning) || !errors.Is(m.RequestRebaseline(), ErrNotRunning) {
		t.Fatal("pre-start control accepted")
	}
	entered, stopping, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	m.open = func(context.Context) (session, error) {
		return sessionFunc(func(ctx context.Context, _ *Monitor) error {
			m.control.start(ctx)
			close(entered)
			<-ctx.Done()
			close(stopping)
			<-release
			return nil
		}), nil
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	defer close(release)
	finished := make(chan error, 1)
	go func() { finished <- m.Run(ctx) }()
	await(t, entered)
	running := m.Snapshot()
	if running.Health() != (Health{Running: true}) {
		t.Fatal("startup health incorrect")
	}
	if err := m.Run(ctx); !errors.Is(err, ErrAlreadyRun) {
		t.Fatalf("concurrent Run = %v", err)
	}
	var wg sync.WaitGroup
	for range 64 {
		wg.Go(func() {
			if err := m.RequestResync(); err != nil {
				t.Error(err)
			}
			if err := m.RequestRebaseline(); err != nil {
				t.Error(err)
			}
			_ = m.Health()
			_ = m.Snapshot()
		})
	}
	wg.Wait()
	await(t, m.control.wake)
	work := m.control.begin()
	if work != resyncRequest|rebaselineRequest {
		t.Fatalf("pending operations = %b", work)
	}
	if err := m.RequestResync(); err != nil {
		t.Fatal(err)
	}
	if err := m.RequestRebaseline(); err != nil {
		t.Fatal(err)
	}
	if m.control.begin() != 0 {
		t.Fatal("in-flight requests scheduled duplicate work")
	}
	m.control.complete(work)
	if err := m.RequestResync(); err != nil {
		t.Fatal(err)
	}
	if work = m.control.begin(); work != resyncRequest {
		t.Fatalf("resync implicitly rebaselines: %b", work)
	}
	m.control.complete(work)
	cancel()
	await(t, stopping)
	if !errors.Is(m.RequestResync(), ErrNotRunning) {
		t.Fatal("request accepted during shutdown")
	}
	release <- struct{}{}
	if err := await(t, finished); err != nil {
		t.Fatal(err)
	}
	if m.Health() != (Health{}) || m.Snapshot().Version() <= running.Version() {
		t.Fatal("shutdown not published")
	}
	if before.Version() != 0 || running.Health() != (Health{Running: true}) {
		t.Fatal("retained snapshot mutated")
	}
	if !errors.Is(m.RequestRebaseline(), ErrNotRunning) {
		t.Fatal("post-shutdown request accepted")
	}
}

func TestRunSessionError(t *testing.T) {
	for _, failure := range []error{errors.New("fatal source failure"), ErrShutdownIncomplete} {
		t.Run(failure.Error(), func(t *testing.T) {
			m := newTestMonitor(t)
			m.open = func(context.Context) (session, error) {
				return sessionFunc(func(context.Context, *Monitor) error { return failure }), nil
			}
			if err := m.Run(t.Context()); !errors.Is(err, failure) {
				t.Fatalf("lost session failure: %v", err)
			}
			if m.Health() != (Health{}) {
				t.Fatal("failed session remains healthy")
			}
		})
	}
}
