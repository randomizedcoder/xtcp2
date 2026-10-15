package linkmonitor

import (
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// BenchmarkOnDemandCached is a detail scrape's refresh when every job is
// inside the TTL: one handoff to the owner and back after its publish.
func BenchmarkOnDemandCached(b *testing.B) {
	b.Log("refresh of netstat inside the TTL on a running monitor; expected: one owner round trip, a few allocations, far under 0.1 ms")
	m, _, release, cancel, done := onDemandSession(b, CollectionOnDemand, false)
	defer func() {
		release()
		cancel()
		<-done
	}()
	scope := RefreshScope{Collectors: 1 << model.CollectorNetstat}
	// A refresh may join a netstat collection startup's resync requested;
	// the clock never moves, so every other one is a cache hit.
	b.ReportAllocs()
	for b.Loop() {
		result, err := m.Refresh(b.Context(), scope)
		if err != nil || result.Cached+result.Collected != 1 || result.TimedOut {
			b.Fatal(result, err)
		}
	}
}

// BenchmarkOnDemandScope is the owner's work for one refresh on a host of
// 64 devices, every job fresh: listing the scope's jobs and settling.
func BenchmarkOnDemandScope(b *testing.B) {
	s, clock := testScheduler(b)
	for i := range uint32(64) {
		key := registerTestJob(b, s, i+1, model.CollectorDriver, schedulePolicy{})
		state, _, _ := s.reducer.collector(key)
		state.attempted, state.lastAttempt = true, clock.Now()
	}
	q := &demands{accepting: true}
	o := &demandOwner{queue: q, ttl: time.Minute, s: s}
	driver := CollectorSet(1 << model.CollectorDriver)
	for _, tc := range []struct {
		name, description, expected string
		scope                       RefreshScope
	}{
		{"host-wide", "driver statistics on every device", "linear in devices, independent of counter count", RefreshScope{Collectors: driver}},
		{"interface", "driver statistics on eth64", "the name scan only", RefreshScope{Collectors: driver, Interface: "eth64"}},
		{"all-collectors", "every collector on every device", "a constant factor over host-wide", RefreshScope{Collectors: AllCollectors()}},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.Logf("%s; expected: %s", tc.description, tc.expected)
			b.ReportAllocs()
			for b.Loop() {
				d := &demand{scope: tc.scope, done: make(chan struct{})}
				if err := q.submit(d); err != nil {
					b.Fatal(err)
				}
				o.begin(clock.Now())
				o.settle()
				if d.err != nil || d.result.Collected != 0 {
					b.Fatal(d.result, d.err)
				}
			}
		})
	}
}

// BenchmarkRDMAWatchPartition splits one port's catalog-sized schema, as a
// new schema does once per on-demand job.
func BenchmarkRDMAWatchPartition(b *testing.B) {
	names := slices.Sorted(maps.Keys(rdmaCounterCatalog))
	schema := rdmaTestSchema(append(names, "hw_counters/vendor_a", "hw_counters/vendor_b")...)
	policy, err := newProblemPolicy(DefaultConfig())
	if err != nil {
		b.Fatal(err)
	}
	for _, kind := range [...]model.CollectorKind{model.CollectorRDMAErrors, model.CollectorRDMACounters} {
		job := model.Job{Key: model.JobKey{Collector: kind}, Device: model.Device{Name: "ib0"}}
		b.Run(collectorNames[kind], func(b *testing.B) {
			b.Logf("%d entries for %s; expected: once per schema, so cost is for the record", len(schema.Names), collectorNames[kind])
			b.ReportAllocs()
			for b.Loop() {
				rdmaPartition(schema, job, &policy)
			}
		})
	}
}

func TestOnDemandAllocationGuards(t *testing.T) {
	s, clock := testScheduler(t)
	for i := range uint32(64) {
		key := registerTestJob(t, s, i+1, model.CollectorDriver, schedulePolicy{})
		state, _, _ := s.reducer.collector(key)
		state.attempted, state.lastAttempt = true, clock.Now()
	}
	q := &demands{accepting: true}
	o := &demandOwner{queue: q, ttl: time.Minute, s: s}
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		scope                                        RefreshScope
		max                                          float64
	}{
		{"host-wide", "positive", "a cached refresh of driver statistics on 64 devices", "the demand, its channel and the queue entry: independent of devices", RefreshScope{Collectors: 1 << model.CollectorDriver}, 3},
		{"interface", "boundary", "a cached refresh of one interface", "the same three", RefreshScope{Collectors: 1 << model.CollectorDriver, Interface: "eth64"}, 3},
		{"unknown", "negative", "a refresh of an unknown interface", "no more than a found one", RefreshScope{Collectors: 1 << model.CollectorDriver, Interface: "eth99"}, 3},
		{"all", "corner", "every collector on every device", "the same three: no allocation per job", RefreshScope{Collectors: AllCollectors()}, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			got := testing.AllocsPerRun(200, func() {
				d := &demand{scope: tc.scope, done: make(chan struct{})}
				if err := q.submit(d); err != nil {
					t.Fatal(err)
				}
				o.begin(clock.Now())
				o.settle()
			})
			if got > tc.max {
				t.Fatalf("%v allocations, want at most %v", got, tc.max)
			}
		})
	}
	t.Run("monitor", func(t *testing.T) {
		t.Log("positive: a cached Refresh through a running monitor; expected: at most 10 allocations")
		m, _, release, cancel, done := onDemandSession(t, CollectionOnDemand, false)
		defer func() { release(); cancel(); <-done }()
		scope := RefreshScope{Collectors: 1 << model.CollectorNetstat}
		if got := testing.AllocsPerRun(200, func() { _, _ = m.Refresh(t.Context(), scope) }); got > 10 {
			t.Fatalf("%v allocations, want at most 10", got)
		}
	})
}
