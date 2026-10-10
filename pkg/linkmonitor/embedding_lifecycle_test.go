package linkmonitor

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"testing"
)

type embeddingLog struct {
	mu       sync.Mutex
	messages []string
}

func (*embeddingLog) Enabled(context.Context, slog.Level) bool { return true }
func (l *embeddingLog) Handle(_ context.Context, r slog.Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.messages = append(l.messages, r.Message)
	return nil
}
func (l *embeddingLog) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *embeddingLog) WithGroup(string) slog.Handler      { return l }

func TestEmbeddingIndependentLifecycles(t *testing.T) {
	t.Log("positive/corner: two caller contexts and loggers; expected: stopping one leaves the other running, no default logger mutation")
	global := slog.Default()
	var monitors [2]*Monitor
	var cancels [2]context.CancelFunc
	var done [2]<-chan error
	for i := range monitors {
		_, m, _ := sessionFixture(t)
		log := new(embeddingLog)
		m.logger = slog.New(log)
		monitors[i] = m
		cancels[i], done[i] = runSession(t, m)
		awaitSnapshot(t, m, func(s Snapshot) bool { return s.Health().Ready })
		log.mu.Lock()
		count := len(log.messages)
		log.mu.Unlock()
		if count == 0 {
			t.Fatal("caller logger unused")
		}
	}
	cancels[0]()
	if err := receiveTest(t, done[0]); err != nil {
		t.Fatal(err)
	}
	if monitors[0].Health().Running || !monitors[1].Health().Running {
		t.Fatal("monitor lifetimes coupled")
	}
	if err := monitors[1].RequestResync(); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(monitors[0].RequestResync(), ErrNotRunning) {
		t.Fatal("stopped monitor accepted control")
	}
	cancels[1]()
	if err := receiveTest(t, done[1]); err != nil {
		t.Fatal(err)
	}
	if slog.Default() != global {
		t.Fatal("default logger changed")
	}
}

// WithBlockedEmbedding is a test-only bridge: real lifecycle inventory is held
// at a barrier while an external test exercises the public exporter.
func WithBlockedEmbedding(t *testing.T, visit func(*Monitor)) {
	t.Helper()
	s, m, _ := sessionFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	blockSessionStage(s, "inventory", func() { once.Do(func() { close(entered) }); <-release })
	cancel, done := runSession(t, m)
	defer func() {
		cancel()
		close(release)
		if err := receiveTest(t, done); err != nil {
			t.Error(err)
		}
	}()
	receiveTest(t, entered)
	visit(m)
}
