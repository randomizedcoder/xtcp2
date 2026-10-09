package linkmonitor

import (
	"context"
	"errors"
	"math"
	"syscall"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmaevents"
)

func eventScheduleFixture(t *testing.T) (*rdmaEventSchedule, model.RDMAPort) {
	t.Helper()
	rdma, clock, p := rdmaScheduled(t, true)
	w := &rdmaEventWorker{ctx: context.Background(), requests: make(chan rdmaEventRequest, 1),
		status: make(chan rdmaEventStatus, 2), records: make(chan rdmaEventRecord, eventQueueCapacity), wake: make(chan struct{}, 1)}
	c := &reconciler{scheduler: newScheduler(rdma.r, nil, clock), rdma: rdma,
		inbox: newEventInbox(1), inventory: &inventoryExecutor{}}
	s := newRDMAEventSchedule(c, w)
	c.rdmaEvents = s
	s.ids = rdmaIdentities([]model.RDMAPort{p})
	if err := s.advance(clock.Now()); err != nil {
		t.Fatal(err)
	}
	request := <-w.requests
	t.Cleanup(s.interrupt)
	w.status <- rdmaEventStatus{generation: request.generation, ready: true}
	if err := s.before(); err != nil {
		t.Fatal(err)
	}
	return s, p
}

func TestRDMAEventScheduling(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		kind                                         rdmaevents.Kind
		stale                                        bool
	}{
		{"active", "positive", "active port event during state read", "late result rejected and one follow-up queued", rdmaevents.Refresh, false},
		{"topology", "positive", "GID association changed", "inventory invalidated and state refreshed", rdmaevents.Topology, false},
		{"fatal", "negative", "fatal HCA event", "health false and retry scheduled", rdmaevents.Fatal, false},
		{"unknown", "corner", "unknown future event", "conservative resync", rdmaevents.Unknown, false},
		{"obsolete", "corner", "old subscription delivers late event", "current revision unchanged", rdmaevents.Fatal, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, p := eventScheduleFixture(t)
			s.committed([]model.RDMAPort{p}, s.barrier+1)
			if !s.c.scheduler.reducer.rdmaEvents {
				t.Fatal("subscription/resync barrier did not open")
			}
			if err := s.c.rdma.advance(s.c.scheduler.clock.Now()); err != nil {
				t.Fatal(err)
			}
			work := <-s.c.rdma.executor.inbox
			revision := s.c.rdma.revision
			generation := s.generation
			if tc.stale {
				generation--
			}
			s.w.records <- rdmaEventRecord{generation: generation, event: rdmaevents.Event{Device: p.Device, Port: p.Port, Kind: tc.kind}}
			if err := s.before(); err != nil {
				t.Fatal(err)
			}
			if (s.c.rdma.revision == revision) != tc.stale {
				t.Fatal(tc.expectedOutcome)
			}
			if err := s.c.rdma.complete(rdmaCompletion{work: work, ports: []model.RDMAPort{p}, finished: s.c.scheduler.clock.Now()}); err != nil {
				t.Fatal(err)
			}
			state, _, err := s.c.scheduler.reducer.collector(work.job.Key)
			if err != nil {
				t.Fatal(err)
			}
			if state.fresh != tc.stale {
				t.Fatal("old event-relative result accepted", tc.expectedOutcome)
			}
			if tc.kind == rdmaevents.Fatal && !tc.stale && (s.c.scheduler.reducer.rdmaEvents || s.retryAt == 0) {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}

func TestRDMAEventInboxBounds(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		count                                        int
		lost                                         bool
	}{
		{"below", "boundary", "4095 records", "all fit", 4095, false},
		{"full", "boundary", "4096 records", "all fit", 4096, false},
		{"overflow", "negative", "4097 records", "independent loss wake", 4097, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, _ := eventScheduleFixture(t)
			accepted := 0
			for range tc.count {
				if s.w.emit(s.generation, rdmaevents.Event{}) {
					accepted++
				}
			}
			if accepted != min(tc.count, eventQueueCapacity) || (s.w.lost.Load() == s.generation) != tc.lost {
				t.Fatal(tc.expectedOutcome)
			}
			select {
			case <-s.w.wake:
			default:
				t.Fatal("missing independent wake")
			}
		})
	}
}

func TestRDMAEventBarrierAndRetry(t *testing.T) {
	s, p := eventScheduleFixture(t)
	t.Log("corner: subscription established while old inventory exists; expected: a later dump is required")
	s.committed([]model.RDMAPort{p}, s.barrier)
	if s.c.scheduler.reducer.rdmaEvents {
		t.Fatal("old dump established coverage")
	}
	s.committed([]model.RDMAPort{p}, s.barrier+1)
	if !s.c.scheduler.reducer.rdmaEvents {
		t.Fatal("new dump failed coverage")
	}
	s.failed(rdmaevents.ErrLost)
	t.Log("boundary: retry expires with source still occupied; expected: no overlapping replacement")
	if err := s.advance(model.Stamp{Monotonic: time.Minute}); err != nil {
		t.Fatal(err)
	}
	if len(s.w.requests) != 0 {
		t.Fatal("occupied event source replaced")
	}
	s.w.status <- rdmaEventStatus{generation: s.generation, err: rdmaevents.ErrLost}
	if err := s.before(); err != nil {
		t.Fatal(err)
	}
	if err := s.advance(model.Stamp{Monotonic: time.Minute}); err != nil {
		t.Fatal(err)
	}
	if len(s.w.requests) != 1 || s.generation != 2 {
		t.Fatal("joined source did not retry")
	}
	s.interrupt()
	s.busy, s.ready, s.dirty, s.generation = false, false, true, math.MaxUint64
	if err := s.advance(model.Stamp{}); !errors.Is(err, errSequenceExhausted) {
		t.Fatal("generation wrapped", err)
	}
}

type testRDMAEventSource struct{ started, closed chan struct{} }

func (s *testRDMAEventSource) Run(ctx context.Context, _ func(rdmaevents.Event) bool) error {
	close(s.started)
	<-ctx.Done()
	return ctx.Err()
}
func (s *testRDMAEventSource) Close() error {
	select {
	case <-s.closed:
	default:
		close(s.closed)
	}
	return nil
}

func TestRDMAEventWorkerLifetime(t *testing.T) {
	t.Log("corner: cancellation during blocked acquisition; expected: acquired source is run/canceled and closed before worker joins")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	source := &testRDMAEventSource{started: make(chan struct{}), closed: make(chan struct{})}
	w := newRDMAEventWorker(ctx, func(context.Context, []rdmaevents.Identity) (rdmaevents.Source, error) {
		close(entered)
		<-release
		return source, nil
	})
	w.requests <- rdmaEventRequest{ctx: ctx, generation: 1}
	receiveTest(t, entered)
	cancel()
	select {
	case <-w.done:
		t.Fatal("released occupancy before acquisition returned")
	default:
	}
	close(release)
	receiveTest(t, w.done)
	receiveTest(t, source.started)
	receiveTest(t, source.closed)
}

func TestRDMAEventOwnerBudgets(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		count, remaining                             int
	}{
		{"below", "boundary", "63 queued events", "all consumed in one turn", 63, 0},
		{"at", "boundary", "64 queued events", "exact budget consumed", 64, 0},
		{"above", "boundary", "65 queued events", "one record remains with wake pending", 65, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, p := eventScheduleFixture(t)
			s.committed([]model.RDMAPort{p}, s.barrier+1)
			for range tc.count {
				s.w.emit(s.generation, rdmaevents.Event{Device: p.Device, Port: p.Port})
			}
			if err := s.before(); err != nil {
				t.Fatal(err)
			}
			if len(s.w.records) != tc.remaining || len(s.c.rdma.queue) != 1 {
				t.Fatal(tc.expectedOutcome)
			}
			if tc.remaining != 0 {
				select {
				case <-s.w.wake:
				default:
					t.Fatal("no continuation wake")
				}
			}
		})
	}
}

func TestRDMAEventPartialCoverage(t *testing.T) {
	s, p := eventScheduleFixture(t)
	t.Log("negative: source opens only some HCAs; expected: no transient healthy state before fatal diagnostics arrive")
	s.ready = false
	s.w.status <- rdmaEventStatus{generation: s.generation, ready: true, partial: true}
	if err := s.before(); err != nil {
		t.Fatal(err)
	}
	s.committed([]model.RDMAPort{p}, s.barrier+1)
	if s.ready || s.c.scheduler.reducer.rdmaEvents {
		t.Fatal("partial coverage reported healthy")
	}
}

func BenchmarkRDMAEventTargets(b *testing.B) {
	ports := make([]model.RDMAPort, 256)
	for i := range ports {
		ports[i] = model.RDMAPort{Device: "hca", Port: uint32(i + 1), Eligibility: model.Eligible}
	}
	targets := rdmaEventTargets(ports)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if len(targets[rdmaEventTarget{"hca", 128}]) != 1 {
			b.Fatal("target missing")
		}
	}
}

func TestRDMAEventDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		failure                                      error
		support                                      model.Support
		reason                                       model.ErrorReason
	}{
		{"ready", "positive", "subscribed and reconciled", "supported fresh successful event collector", nil, model.Supported, model.ErrorNone},
		{"denied", "negative", "permission denied", "unknown support and permission diagnostic", syscall.EACCES, model.SupportUnknown, model.ErrorPermission},
		{"unavailable", "negative", "build has no binding", "unsupported event collector", rdmaevents.ErrUnavailable, model.Unsupported, model.ErrorIO},
		{"lost", "corner", "event stream lost", "unknown support and failed event collector", rdmaevents.ErrLost, model.SupportUnknown, model.ErrorIO},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, p := eventScheduleFixture(t)
			if tc.failure == nil {
				s.committed([]model.RDMAPort{p}, s.barrier+1)
			} else {
				s.failed(tc.failure)
			}
			state, _, err := s.c.scheduler.reducer.collector(model.JobKey{Namespace: 1, Device: p.Canonical, Collector: model.CollectorRDMAEvents})
			if err != nil {
				t.Fatal(err)
			}
			if state.support != tc.support || state.reason != tc.reason || state.fresh != (tc.failure == nil) || !state.attempted {
				t.Fatal(tc.expectedOutcome, state)
			}
		})
	}
}
