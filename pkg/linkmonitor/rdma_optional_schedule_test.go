package linkmonitor

import (
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func optionalRDMAScheduler(t *testing.T) (*scheduler, model.JobKey) {
	t.Helper()
	rdma, clock, port := rdmaScheduled(t, true)
	s := newScheduler(rdma.r, nil, clock)
	s.rdmaInterval = 15 * time.Second
	s.reducer.rdmaPorts = []model.RDMAPort{port}
	s.replaceRDMAOptional()
	return s, model.JobKey{Namespace: 1, Device: port.Canonical, Collector: model.CollectorRDMACapabilities}
}

func TestRDMAOptionalFencing(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		change                                       string
		accepted                                     bool
	}{
		{"current", "positive", "matching capability completion", "checks and samples accepted together", "", true},
		{"resync", "corner", "inventory resync during query", "old completion rejected", "resync", false},
		{"event", "corner", "port event during query", "old completion rejected", "event", false},
		{"removed", "negative", "association removed without changing canonical link", "unregister and reject old result", "removed", false},
		{"epoch", "negative", "lost event epoch", "old completion rejected", "epoch", false},
		{"stale", "boundary", "source freshness expires", "previous maximum check becomes unknown", "stale", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, key := optionalRDMAScheduler(t)
			job, err := s.reducer.startCollection(key, s.clock.Now())
			if err != nil {
				t.Fatal(err)
			}
			switch tc.change {
			case "resync":
				s.replaceRDMAOptional()
			case "event":
				s.refreshRDMAOptional(key.Device, true)
			case "removed":
				s.reducer.rdmaPorts = nil
				s.replaceRDMAOptional()
			case "epoch":
				if err := s.reducer.loseEvents(); err != nil {
					t.Fatal(err)
				}
			}
			s.recordResult(model.Result{Job: job, Finished: s.clock.Now(), Support: model.Supported, Settings: &model.SettingsChecks{Speed: model.CheckPass, Width: model.CheckPass}})
			slot := s.reducer.slots[s.reducer.index[key.Device]]
			if slot.collectors[key.Collector].hasSuccess != tc.accepted {
				t.Fatal(tc.expectedOutcome)
			}
			if tc.accepted && slot.checks.maximumSpeed != model.CheckPass {
				t.Fatal("check not installed")
			}
			if tc.change == "removed" && len(s.jobs) != 0 {
				t.Fatal("orphan jobs retained")
			}
			if tc.change == "stale" {
				s.reducer.freshness.poll = time.Second
				s.reducer.armExpiry(job, &slot.collectors[key.Collector])
				s.expire(model.Stamp{Monotonic: time.Second})
				if slot.collectors[key.Collector].fresh || effectiveChecks(slot).maximumSpeed != model.CheckUnknown {
					t.Fatal(tc.expectedOutcome)
				}
			}
		})
	}
}

func TestRDMAOptionalCounterDiscontinuity(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		next                                         uint64
		source                                       string
		want                                         uint64
	}{
		{"increase", "positive", "flap counter increases", "raw increase preserved without discontinuity", 11, "counters/link_downed", 0},
		{"reset", "negative", "counter decreases", "one discontinuity, no assumed wrap", 1, "counters/link_downed", 1},
		{"source", "corner", "same value from replacement register", "one source discontinuity", 10, "other/link_downed", 1},
		{"zero", "boundary", "counter resets to zero", "present zero plus discontinuity", 0, "counters/link_downed", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, key := optionalRDMAScheduler(t)
			key.Collector = model.CollectorRDMACounters
			for _, value := range []struct {
				n      uint64
				source string
			}{{10, "counters/link_downed"}, {tc.next, tc.source}} {
				job, err := s.reducer.startCollection(key, s.clock.Now())
				if err != nil {
					t.Fatal(err)
				}
				s.recordResult(model.Result{Job: job, Finished: s.clock.Now(), Support: model.Supported, Samples: []model.Sample{{Descriptor: "go_link_monitor_infiniband_link_downed_total", Kind: model.SampleCounter, Number: model.Unsigned(value.n), Counter: model.CounterIdentity{Source: value.source, Lifetime: 1}}}})
			}
			state := &s.reducer.slots[0].collectors[key.Collector]
			if state.discontinuities != tc.want || state.block.values[0] != model.Unsigned(tc.next) {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}
