package linkmonitor

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
)

// demandHarness drives a demandOwner on a manual scheduler: netstat plus
// driver statistics on eth1 and eth2, all on demand (interval zero), each
// collected once at startup.
type demandHarness struct {
	s     *scheduler
	clock *testkit.Clock
	o     *demandOwner
	q     *demands
}

func newDemandHarness(t testing.TB, ttl time.Duration) *demandHarness {
	t.Helper()
	s, clock := testScheduler(t)
	registerTestJob(t, s, 0, model.CollectorNetstat, schedulePolicy{})
	registerTestJob(t, s, 1, model.CollectorDriver, schedulePolicy{})
	registerTestJob(t, s, 2, model.CollectorDriver, schedulePolicy{})
	h := &demandHarness{s: s, clock: clock, q: &demands{accepting: true}}
	h.o = &demandOwner{queue: h.q, ttl: ttl, s: s}
	dispatchTest(t, s)
	if n := h.finish(t, nil); n != 3 {
		t.Fatalf("startup collected %d jobs, want 3", n)
	}
	return h
}

// finish completes every dispatched job, failing each with err when set,
// and returns how many there were.
func (h *demandHarness) finish(t testing.TB, err error) int {
	t.Helper()
	n := 0
	for worker := range h.s.pool.inbox {
		select {
		case assignment := <-h.s.pool.inbox[worker]:
			result := model.Result{Job: assignment.job, Finished: h.s.clock.Now(), Support: model.Supported}
			if err != nil {
				result.Support, result.Err = model.SupportUnknown, err
			}
			h.s.complete(workerCompletion{worker: worker, result: result})
			n++
		default:
		}
	}
	return n
}

// turn runs one owner turn: begin, dispatch, then settle as publish would.
func (h *demandHarness) turn(t testing.TB) {
	t.Helper()
	h.o.begin(h.clock.Now())
	dispatchTest(t, h.s)
	h.o.settle()
}

func (h *demandHarness) submit(t testing.TB, scope RefreshScope) *demand {
	t.Helper()
	d := &demand{scope: scope, done: make(chan struct{})}
	if err := h.q.submit(d); err != nil {
		t.Fatal(err)
	}
	return d
}

func finished(d *demand) bool {
	select {
	case <-d.done:
		return true
	default:
		return false
	}
}

func collectorSet(t *testing.T, names ...string) CollectorSet {
	t.Helper()
	set, err := Collectors(names...)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func TestOnDemandRefresh(t *testing.T) {
	const ttl = 15 * time.Second
	netstat := RefreshScope{Collectors: 1 << model.CollectorNetstat}
	driver := RefreshScope{Collectors: 1 << model.CollectorDriver}
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		ttl                                          time.Duration
		age                                          time.Duration // since the startup collection
		scope                                        RefreshScope
		demands                                      int
		running                                      bool  // the job is in flight when the refresh begins
		fail                                         error // the requested collection fails
		remove                                       bool  // eth2 is removed while the refresh waits
		wantErr                                      error
		wantCached, wantCollected, wantDispatched    int
	}{
		{"cold", "positive", "netstat collected 16 s ago, TTL 15 s", "one collection, done once it reports", ttl, 16 * time.Second, netstat, 1, false, nil, false, nil, 0, 1, 1},
		{"cached", "positive", "netstat collected 10 s ago", "cached, done at once, nothing dispatched", ttl, 10 * time.Second, netstat, 1, false, nil, false, nil, 1, 0, 0},
		{"expired", "positive", "collected TTL plus 1 ns ago", "one collection", ttl, ttl + 1, netstat, 1, false, nil, false, nil, 0, 1, 1},
		{"at-ttl", "boundary", "collected exactly TTL ago", "cached: an age of TTL is fresh", ttl, ttl, netstat, 1, false, nil, false, nil, 1, 0, 0},
		{"ttl-zero", "boundary", "TTL 0, collected 1 ns ago", "collects every time", 0, 1, netstat, 1, false, nil, false, nil, 0, 1, 1},
		{"coalesce", "positive", "10 refreshes of one stale scope in one turn", "one collection; all 10 done, each counting it", ttl, time.Minute, netstat, 10, false, nil, false, nil, 0, 1, 1},
		{"overlap", "corner", "netstat already in flight when the refresh begins", "joins it: no second dispatch, done when it reports", ttl, time.Minute, netstat, 1, true, nil, false, nil, 0, 1, 0},
		{"interface-scope", "positive", "driver on eth2 only", "only eth2's job collected", ttl, time.Minute, RefreshScope{Collectors: driver.Collectors, Interface: "eth2"}, 1, false, nil, false, nil, 0, 1, 1},
		{"host-wide", "positive", "driver on every device", "eth1 and eth2 collected; netstat untouched", ttl, time.Minute, driver, 1, false, nil, false, nil, 0, 2, 2},
		{"all", "positive", "every collector", "netstat and both drivers collected; kinds without jobs skipped", ttl, time.Minute, RefreshScope{Collectors: AllCollectors()}, 1, false, nil, false, nil, 0, 3, 3},
		{"unknown-interface", "negative", "interface eth9", "ErrUnknownInterface; nothing requested", ttl, time.Minute, RefreshScope{Collectors: driver.Collectors, Interface: "eth9"}, 1, false, nil, false, ErrUnknownInterface, 0, 0, 0},
		{"failure", "negative", "the collection fails with EIO", "done all the same: a failure is a report", ttl, time.Minute, netstat, 1, false, syscall.EIO, false, nil, 0, 1, 1},
		{"device-removed", "corner", "eth2 removed while its refresh waits", "done without eth2's report", ttl, time.Minute, driver, 1, false, nil, true, nil, 0, 2, 2},
		{"no-lane", "boundary", "rdma_state with no RDMA lane", "done at once with nothing counted", ttl, time.Minute, RefreshScope{Collectors: 1 << model.CollectorRDMAState}, 1, false, nil, false, nil, 0, 0, 0},
		{"empty-set", "boundary", "no collectors", "done at once with nothing counted", ttl, time.Minute, RefreshScope{}, 1, false, nil, false, nil, 0, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			h := newDemandHarness(t, tc.ttl)
			advanceSchedulerClock(t, h.clock, tc.age)
			if tc.running {
				h.s.request(model.JobKey{Namespace: 1, Collector: model.CollectorNetstat}, urgencyEvent)
				dispatchTest(t, h.s)
			}
			ds := make([]*demand, tc.demands)
			for i := range ds {
				ds[i] = h.submit(t, tc.scope)
			}
			h.o.begin(h.clock.Now())
			dispatched := 0
			dispatchTest(t, h.s)
			for _, inbox := range h.s.pool.inbox {
				dispatched += len(inbox)
			}
			if tc.running {
				dispatched-- // the attempt already in flight
			}
			for _, d := range ds {
				if d.result.Cached != tc.wantCached || d.result.Collected != tc.wantCollected {
					t.Fatalf("result %+v, want cached %d collected %d", d.result, tc.wantCached, tc.wantCollected)
				}
			}
			if dispatched != tc.wantDispatched {
				t.Fatalf("dispatched %d jobs, want %d", dispatched, tc.wantDispatched)
			}
			if finished(ds[0]) != (tc.wantErr != nil) {
				t.Fatal("only a failed refresh may finish before this turn publishes")
			}
			waits := tc.wantErr == nil && tc.wantCollected != 0
			h.o.settle()
			if finished(ds[0]) == waits {
				t.Fatalf("done before its jobs reported: %v, want %v", finished(ds[0]), !waits)
			}
			if tc.remove {
				eth2 := slotAt(h.s.reducer, 2).device
				if _, err := h.s.reducer.remove(eth2.Key, eth2.Token); err != nil {
					t.Fatal(err)
				}
			}
			advanceSchedulerClock(t, h.clock, time.Millisecond)
			if !tc.remove {
				h.finish(t, tc.fail)
			} else {
				// Only eth1 reports; eth2's attempt is obsolete.
				h.finish(t, nil)
			}
			h.o.settle()
			for _, d := range ds {
				if !finished(d) || !errors.Is(d.err, tc.wantErr) {
					t.Fatalf("finished %v err %v, want done with %v", finished(d), d.err, tc.wantErr)
				}
			}
			if len(h.q.waiting) != 0 {
				t.Fatalf("%d refreshes still waiting", len(h.q.waiting))
			}
			// A refresh after a collection, failed or not, reuses it.
			if tc.wantCollected != 0 && tc.wantErr == nil && tc.ttl != 0 && !tc.remove {
				again := h.submit(t, tc.scope)
				h.turn(t)
				if !finished(again) || again.result.Collected != 0 {
					t.Fatalf("refresh within the TTL collected again: %+v", again.result)
				}
			}
		})
	}
}

func TestOnDemandShutdown(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		begin                                        bool
	}{
		{"queued", "corner", "monitor stops before the owner takes the refresh", "ErrNotRunning", false},
		{"waiting", "corner", "monitor stops while the refresh waits for its job", "ErrNotRunning; nothing left waiting", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			h := newDemandHarness(t, 0)
			advanceSchedulerClock(t, h.clock, 1)
			d := h.submit(t, RefreshScope{Collectors: 1 << model.CollectorNetstat})
			if tc.begin {
				h.turn(t)
				if finished(d) {
					t.Fatal("refresh done before its job reported")
				}
			}
			h.q.stop()
			if !finished(d) || !errors.Is(d.err, ErrNotRunning) || len(h.q.waiting) != 0 || len(h.q.queue) != 0 {
				t.Fatal(tc.expectedOutcome)
			}
			if err := h.q.submit(&demand{done: make(chan struct{})}); !errors.Is(err, ErrNotRunning) {
				t.Fatalf("stopped queue accepted a refresh: %v", err)
			}
		})
	}
}

func TestCollectors(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		names                                        []string
		want                                         CollectorSet
		wantErr                                      bool
	}{
		{"one", "positive", "netstat", "the netstat bit", []string{"netstat"}, 1 << model.CollectorNetstat, false},
		{"several", "positive", "driver, phy and rdma_errors", "their three bits", []string{"driver", "phy", "rdma_errors"}, 1<<model.CollectorDriver | 1<<model.CollectorPHY | 1<<model.CollectorRDMAErrors, false},
		{"none", "boundary", "no names", "the empty set", nil, 0, false},
		{"duplicate", "boundary", "netstat twice", "one bit", []string{"netstat", "netstat"}, 1 << model.CollectorNetstat, false},
		{"unknown", "negative", "a name no collector has", "error", []string{"lazy"}, 0, true},
		{"empty-name", "negative", "an empty name, the unknown kind's", "error", []string{""}, 0, true},
		{"all", "corner", "every collector name", "AllCollectors", collectorNames[1:], AllCollectors(), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			got, err := Collectors(tc.names...)
			if (err != nil) != tc.wantErr || got != tc.want {
				t.Fatalf("Collectors(%q) = %b, %v; want %b", tc.names, got, err, tc.want)
			}
		})
	}
	if AllCollectors().has(model.CollectorUnknown) || !AllCollectors().has(model.CollectorNetstat) {
		t.Fatal("AllCollectors must hold every kind but the unknown one")
	}
}

// onDemandSession runs a session with the host collector on a proc tree;
// snmp is a FIFO when block is set, so each netstat read waits for a writer.
func onDemandSession(t testing.TB, mode CollectionMode, block bool) (*Monitor, *testkit.Clock, func(), context.CancelFunc, <-chan error) {
	t.Helper()
	s, m, clock := sessionFixture(t)
	// Advancing the clock past the TTL stays inside collectionBudget, so an
	// attempt still in flight is not timed out by the jump.
	m.cfg.CollectionMode, m.cfg.OnDemandTTL, m.cfg.OnDemandWait = mode, 2*time.Second, 100*time.Millisecond
	s.hostStatistics, s.procRoot = true, t.TempDir()
	snmp := filepath.Join(s.procRoot, "snmp")
	if err := os.WriteFile(filepath.Join(s.procRoot, "netstat"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	release := func() {}
	if block {
		if err := syscall.Mkfifo(snmp, 0600); err != nil {
			t.Fatal(err)
		}
		var once sync.Once
		// A writer end unblocks a reader waiting in open, and closing it is
		// EOF. O_RDWR never blocks on Linux, reader or not. The regular file
		// renamed over the FIFO first answers every later read at once.
		release = func() {
			once.Do(func() {
				f, err := os.OpenFile(snmp, os.O_RDWR, 0)
				if err != nil {
					t.Error(err)
					return
				}
				regular := snmp + ".regular"
				if err := os.WriteFile(regular, []byte("Tcp: MaxConn\nTcp: -1"), 0600); err != nil {
					t.Error(err)
				} else if err := os.Rename(regular, snmp); err != nil {
					t.Error(err)
				}
				_ = f.Close()
			})
		}
		t.Cleanup(release)
	} else if err := os.WriteFile(snmp, []byte("Tcp: MaxConn\nTcp: -1"), 0600); err != nil {
		t.Fatal(err)
	}
	cancel, done := runSession(t, m)
	// Refreshes are accepted once startup has published.
	awaitSnapshot(t, m, func(s Snapshot) bool { return s.Health().Ready })
	return m, clock.Clock, release, cancel, done
}

func TestOnDemandMonitor(t *testing.T) {
	netstat := RefreshScope{Collectors: 1 << model.CollectorNetstat}
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		mode                                         CollectionMode
		block                                        bool
		run                                          func(t *testing.T, m *Monitor, clock *testkit.Clock, release func())
	}{
		{"poll-mode", "positive", "Refresh in poll mode", "returns at once with nothing counted", CollectionPoll, false, func(t *testing.T, m *Monitor, _ *testkit.Clock, _ func()) {
			result, err := m.Refresh(t.Context(), netstat)
			if err != nil || result != (RefreshResult{}) {
				t.Fatalf("%+v, %v", result, err)
			}
		}},
		{"cached-then-collected", "positive", "refresh at startup, then after the TTL", "cached first; then collected, published before return", CollectionOnDemand, false, func(t *testing.T, m *Monitor, clock *testkit.Clock, _ func()) {
			awaitSnapshot(t, m, func(s Snapshot) bool { return s.HostCollector().Fresh() })
			// Startup may still be collecting netstat again for the inventory
			// commit; the refresh then joins that attempt.
			result, err := m.Refresh(t.Context(), netstat)
			if err != nil || result.Cached+result.Collected != 1 || result.TimedOut {
				t.Fatalf("startup refresh %+v, %v", result, err)
			}
			before, _ := m.Snapshot().HostCollector().LastSuccess()
			if err := clock.Advance(3 * time.Second); err != nil {
				t.Fatal(err)
			}
			// The refresh collects, or reuses an attempt that finished as the
			// clock moved; either way the snapshot it returns to holds data
			// no older than the TTL.
			result, err = m.Refresh(t.Context(), netstat)
			after, _ := m.Snapshot().HostCollector().LastSuccess()
			oldest := clock.Now().Wall.Add(-2 * time.Second)
			if err != nil || result.Cached+result.Collected != 1 || result.TimedOut || !after.After(before) || after.Before(oldest) {
				t.Fatalf("refresh %+v, %v; last success %v then %v, oldest %v", result, err, before, after, oldest)
			}
		}},
		{"unknown-interface", "negative", "refresh of interface eth9", "ErrUnknownInterface", CollectionOnDemand, false, func(t *testing.T, m *Monitor, _ *testkit.Clock, _ func()) {
			awaitSnapshot(t, m, func(s Snapshot) bool { return s.Health().Ready })
			if _, err := m.Refresh(t.Context(), RefreshScope{Collectors: AllCollectors(), Interface: "eth9"}); !errors.Is(err, ErrUnknownInterface) {
				t.Fatal(err)
			}
		}},
		{"timeout", "negative", "netstat blocks past the 100 ms wait", "TimedOut; the later result is kept and cached", CollectionOnDemand, true, func(t *testing.T, m *Monitor, _ *testkit.Clock, release func()) {
			result, err := m.Refresh(t.Context(), netstat)
			if err != nil || !result.TimedOut {
				t.Fatalf("%+v, %v", result, err)
			}
			release()
			awaitSnapshot(t, m, func(s Snapshot) bool { _, ok := s.HostCollector().LastSuccess(); return ok })
			if result, err := m.Refresh(t.Context(), netstat); err != nil || result.Cached != 1 {
				t.Fatalf("after release %+v, %v", result, err)
			}
		}},
		{"client-cancel", "corner", "the caller's context ends while netstat blocks", "the context error; the collection completes", CollectionOnDemand, true, func(t *testing.T, m *Monitor, _ *testkit.Clock, release func()) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
			defer cancel()
			if _, err := m.Refresh(ctx, netstat); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatal(err)
			}
			release()
			awaitSnapshot(t, m, func(s Snapshot) bool { _, ok := s.HostCollector().LastSuccess(); return ok })
		}},
		{"concurrent", "corner", "8 concurrent refreshes after the TTL", "every one collected, the job run once", CollectionOnDemand, false, func(t *testing.T, m *Monitor, clock *testkit.Clock, _ func()) {
			awaitSnapshot(t, m, func(s Snapshot) bool { return s.HostCollector().Fresh() })
			if err := clock.Advance(3 * time.Second); err != nil {
				t.Fatal(err)
			}
			var wg sync.WaitGroup
			for range 8 {
				wg.Go(func() {
					if result, err := m.Refresh(t.Context(), netstat); err != nil || result.TimedOut || result.Cached+result.Collected != 1 {
						t.Errorf("%+v, %v", result, err)
					}
				})
			}
			wg.Wait()
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			m, clock, release, cancel, done := onDemandSession(t, tc.mode, tc.block)
			tc.run(t, m, clock, release)
			release()
			cancel()
			if err := receiveTest(t, done); err != nil {
				t.Fatal(err)
			}
			if _, err := m.Refresh(t.Context(), netstat); tc.mode == CollectionOnDemand && !errors.Is(err, ErrNotRunning) {
				t.Fatalf("stopped monitor refreshed: %v", err)
			}
		})
	}
}
