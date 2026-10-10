package linkmonitor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/baseline"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func TestProductionOpen(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		backend                                      IOBackend
		canceled                                     bool
		want                                         error
	}{
		{"poller", "positive", "default Linux adapter composition", "all collectors and real ownership dependencies wired", IOBackendPoller, false, nil},
		{"uring", "negative", "explicit unavailable backend", "no acquisition or fallback", IOBackendIOUring, false, ErrBackendUnavailable},
		{"canceled", "boundary", "canceled before acquisition", "context error and no session", IOBackendPoller, true, context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			cfg := DefaultConfig()
			cfg.IOBackend, cfg.BaselineFile = tc.backend, filepath.Join(t.TempDir(), "baseline.json")
			m, err := New(cfg, Options{})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			live, err := m.open(ctx)
			if !errors.Is(err, tc.want) {
				t.Fatal(tc.expectedOutcome, err)
			}
			if err != nil {
				if live != nil {
					t.Fatal("unexpected session")
				}
				return
			}
			s := live.(*lifecycleSession)
			if s.namespace == 0 || !s.rdma || !s.settings || !s.statistics || !s.driverStatistics || !s.hostStatistics || s.subscribe == nil || s.collectors == nil {
				t.Fatal(tc.expectedOutcome)
			}
			if err := s.store.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestProductionClock(t *testing.T) {
	t.Log("positive/boundary: real clock and timer replacement; expected: monotonic progress and immediate reset wake")
	c := productionClock{origin: time.Now()}
	before := c.Now()
	timer := c.NewTimer(time.Hour)
	defer timer.Stop()
	timer.Reset(0)
	select {
	case <-timer.C():
	case <-time.After(time.Second):
		t.Fatal("timer did not wake")
	}
	after := c.Now()
	if after.Monotonic < before.Monotonic || after.Wall.IsZero() {
		t.Fatal("invalid clock")
	}
}

func TestProductionBaselineFailure(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		component                                    bool
	}{
		{"invalid record", "negative", "corrupt persisted baseline", "fail before network acquisition and release lifetime lock", false},
		{"file component", "corner", "baseline parent is a regular file", "preserve path error without acquiring network sources", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			path := filepath.Join(t.TempDir(), "baseline")
			if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
				t.Fatal(err)
			}
			cfg := DefaultConfig()
			cfg.BaselineFile = path
			if tc.component {
				cfg.BaselineFile = filepath.Join(path, "child")
			}
			// A second instance must fail for the original filesystem reason,
			// never because the failed first lifetime retained its lock.
			for range 2 {
				m, err := New(cfg, Options{})
				if err != nil {
					t.Fatal(err)
				}
				err = m.Run(t.Context())
				if err == nil || errors.Is(err, ErrShutdownIncomplete) || errors.Is(err, baseline.ErrLocked) || m.Health().Running {
					t.Fatal(tc.expectedOutcome, err, m.Health())
				}
			}
		})
	}
}

func TestProductionLinkIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		known                                        bool
		kind                                         model.EventKind
	}{
		{"known", "positive", "up/down of same interface", "hardware identity retained and scalar state updated", true, model.EventChange},
		{"renamed", "corner", "name differs from inventory", "refresh only until authoritative classification", false, model.EventRefresh},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			c, _ := testReconciler(t)
			r := c.scheduler.reducer
			o := observed(r, 1, true)
			o.Device.HardwareID = "hardware"
			if _, err := r.observe(o); err != nil {
				t.Fatal(err)
			}
			e := model.Event{Kind: model.EventLink, Observation: o}
			e.Observation.Device.Up = presentValue(false)
			e.Observation.Device.HardwareID = ""
			if !tc.known {
				e.Observation.Device.Name = "renamed"
			}
			got := c.knownLink(e)
			if got.Kind != tc.kind || got.Observation.Device.Up.Value {
				t.Fatal(tc.expectedOutcome, got)
			}
			if tc.known && got.Observation.Device.HardwareID != o.Device.HardwareID {
				t.Fatal("lost identity")
			}
		})
	}
}
