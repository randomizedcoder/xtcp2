//go:build embedding_vm

package linkmonitor

import (
	"context"
	"errors"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/baseline"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// embeddingInventory changes only test-device eligibility. All observations
// still come from real route transactions in the disposable namespace.
type embeddingInventory struct{ *ethernetInventory }

func embeddingEligible(o model.Observation) model.Observation {
	if o.Device.Name == "lm0" || o.Device.Name == "lm-renamed" {
		o.Device.Eligibility = model.Eligible
		o.Device.HardwareID = "embedding-fixture"
	}
	return o
}

func (s embeddingInventory) Dump(ctx context.Context) (model.Candidate, error) {
	c, err := s.ethernetInventory.Dump(ctx)
	for i := range c.Devices {
		c.Devices[i] = embeddingEligible(c.Devices[i])
	}
	return c, err
}

func (s embeddingInventory) Query(ctx context.Context, key model.DeviceKey) (model.Observation, error) {
	o, _, err := s.QueryStatistics(ctx, key)
	return o, err
}

func (s embeddingInventory) QueryStatistics(ctx context.Context, key model.DeviceKey) (model.Observation, *model.LinkStatistics, error) {
	o, stats, err := s.ethernetInventory.QueryStatistics(ctx, key)
	return embeddingEligible(o), stats, err
}

type embeddingEvents struct {
	model.EventSource
	drop    atomic.Bool
	dropped atomic.Uint64
}

func (s *embeddingEvents) Run(ctx context.Context, emit func(model.Event) bool) error {
	return s.EventSource.Run(ctx, func(e model.Event) bool {
		if e.Kind == model.EventLink && e.Observation.Device.Name == "lm0" && s.drop.CompareAndSwap(true, false) {
			s.dropped.Add(1)
			return true
		}
		return emit(e)
	})
}

func embeddingIP(t *testing.T, args ...string) {
	t.Helper()
	output, err := exec.CommandContext(t.Context(), "ip", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("ip %v: %v: %s", args, err, output)
	}
}

func embeddingMonitor(t *testing.T, path string, eligible bool, events *embeddingEvents) *Monitor {
	t.Helper()
	cfg := DefaultConfig()
	cfg.BaselineFile, cfg.Settle, cfg.Resync = path, 0, 2*time.Second
	m, err := New(cfg, Options{})
	if err != nil {
		t.Fatal(err)
	}
	m.open = func(ctx context.Context) (session, error) {
		live, err := m.openProduction(ctx)
		if err != nil {
			return nil, err
		}
		s := live.(*lifecycleSession)
		if eligible {
			s.inventory = func(context.Context) (inventoryBackend, error) {
				source, err := newEthernetInventory(s.namespace, defaultNetRoot, s.clock)
				if err != nil {
					return nil, err
				}
				return s.wrapRDMAInventory(embeddingInventory{source})
			}
		}
		if events != nil {
			subscribe := s.subscribe
			s.subscribe = func(ctx context.Context, epoch uint64) (eventSubscription, error) {
				sub, err := subscribe(ctx, epoch)
				if err != nil {
					return sub, err
				}
				events.EventSource = sub.source
				sub.source = events
				return sub, nil
			}
		}
		return s, nil
	}
	return m
}

func embeddingWait(t *testing.T, m *Monitor, match func(Snapshot) bool) Snapshot {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		s := m.Snapshot()
		if match(s) {
			return s
		}
		select {
		case <-ctx.Done():
			t.Fatalf("snapshot condition: health=%+v counts=%+v", s.Health(), s.Counts())
		case <-ticker.C:
		}
	}
}

func embeddingCount(want uint64) func(Snapshot) bool {
	return func(s Snapshot) bool {
		current, known := s.Counts().Current()
		return known && current == want && s.Health().BaselineReady
	}
}

func TestEmbeddingVirtualLinux(t *testing.T) {
	t.Log("positive/negative: real veth with production classifier, then test-only eligibility; expected: excluded by production and learned count one under injection")
	embeddingIP(t, "link", "add", "lm0", "type", "veth", "peer", "name", "lm1")
	embeddingIP(t, "link", "set", "lm0", "up")
	embeddingIP(t, "link", "set", "lm1", "up")
	path := filepath.Join(t.TempDir(), "baseline.json")
	production := embeddingMonitor(t, filepath.Join(t.TempDir(), "production.json"), false, nil)
	stop, done := runSession(t, production)
	embeddingWait(t, production, embeddingCount(0))
	stop()
	if err := receiveTest(t, done); err != nil {
		t.Fatal(err)
	}
	events := new(embeddingEvents)
	m := embeddingMonitor(t, path, true, events)
	stop, done = runSession(t, m)
	embeddingWait(t, m, embeddingCount(1))
	embeddingOwnership(t, path)
	embeddingTransitions(t, m, events)
	stop()
	if err := receiveTest(t, done); err != nil {
		t.Fatal(err)
	}
	// Restart from the durable learned value while the actual link is down.
	embeddingIP(t, "link", "set", "lm0", "down")
	restarted := embeddingMonitor(t, path, true, nil)
	stop, done = runSession(t, restarted)
	s := embeddingWait(t, restarted, embeddingCount(0))
	if expected, known := s.Counts().Expected(); !known || expected != 1 {
		t.Fatal("baseline not preserved", s.Counts())
	}
	stop()
	if err := receiveTest(t, done); err != nil {
		t.Fatal(err)
	}
}

func embeddingOwnership(t *testing.T, path string) {
	t.Helper()
	t.Log("negative/boundary: second monitor owns same baseline; expected: second Run fails while first continues")
	m := embeddingMonitor(t, path, true, nil)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := m.Run(ctx); !errors.Is(err, baseline.ErrLocked) {
		t.Fatal("expected baseline ownership error", err)
	}
}

func embeddingTransitions(t *testing.T, m *Monitor, events *embeddingEvents) {
	t.Helper()
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		args                                         []string
		count                                        uint64
	}{
		{"down", "positive", "real down notification", "count zero", []string{"link", "set", "lm0", "down"}, 0},
		{"up", "positive", "real up notification", "count one", []string{"link", "set", "lm0", "up"}, 1},
		{"rename", "corner", "rename existing link", "new name with same count", []string{"link", "set", "lm0", "name", "lm-renamed"}, 1},
		{"remove", "negative", "remove renamed link", "count zero", []string{"link", "delete", "lm-renamed"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			version := m.Snapshot().Version()
			embeddingIP(t, tc.args...)
			embeddingWait(t, m, func(s Snapshot) bool {
				if s.Version() <= version || !embeddingCount(tc.count)(s) {
					return false
				}
				if tc.name != "rename" {
					return true
				}
				found := false
				s.RangeDevices(func(d DeviceView) bool { found = found || d.Name() == "lm-renamed"; return true })
				return found
			})
		})
	}
	embeddingIP(t, "link", "add", "lm0", "type", "veth", "peer", "name", "lm1")
	embeddingIP(t, "link", "set", "lm0", "up")
	embeddingIP(t, "link", "set", "lm1", "up")
	embeddingWait(t, m, embeddingCount(1))
	t.Log("corner: discard next lm0 notification; expected: periodic resync restores actual down state without rebaseline")
	before := embeddingPeriodic(m.Snapshot())
	events.drop.Store(true)
	embeddingIP(t, "link", "set", "lm0", "down")
	s := embeddingWait(t, m, func(s Snapshot) bool { return embeddingCount(0)(s) && embeddingPeriodic(s) > before })
	if expected, known := s.Counts().Expected(); !known || expected != 1 {
		t.Fatal("resync changed baseline", s.Counts())
	}
	if events.dropped.Load() != 1 {
		t.Fatal("notification was not dropped")
	}
	embeddingIP(t, "link", "set", "lm0", "up")
	embeddingWait(t, m, embeddingCount(1))
}

func embeddingPeriodic(s Snapshot) uint64 {
	var n uint64
	s.RangeResyncs(func(reason, result string, count uint64) bool {
		if reason == "periodic" && result == "success" {
			n = count
		}
		return true
	})
	return n
}
