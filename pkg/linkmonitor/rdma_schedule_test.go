package linkmonitor

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
)

func rdmaScheduled(t testing.TB, native bool) (*rdmaSchedule, *testkit.Clock, model.RDMAPort) {
	t.Helper()
	r := newReducer(1)
	o := observed(r, 1, true)
	p := model.RDMAPort{Device: "mlx5_0", Port: 1, Layer: "Ethernet", Eligibility: model.Eligible, State: presentValue(uint8(4)), Physical: presentValue(uint8(5)), Versions: []string{"v2"}, Aliases: []string{"eth1"}}
	o.Device.RDMA = true
	if native {
		o.Device.Key = model.DeviceKey{Namespace: 1, Kind: model.DeviceNativeRDMA, RDMADevice: p.Device, Port: 1}
		o.Device.Name = "rdma:mlx5_0:1"
		p.Layer = "InfiniBand"
	}
	if _, err := r.observe(o); err != nil {
		t.Fatal(err)
	}
	p.Canonical = o.Device.Key
	clock := testkit.NewClock(time.Unix(100, 0))
	e := &rdmaExecutor{ctx: context.Background(), inbox: make(chan rdmaWork, 1)}
	s := newRDMASchedule(r, e, 15*time.Second)
	if err := s.replace([]model.RDMAPort{p}, clock.Now()); err != nil {
		t.Fatal(err)
	}
	return s, clock, p
}

func TestRDMARequiredScheduling(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		change                                       string
		accepted                                     bool
	}{
		{"success", "positive", "required state finishes", "fresh state without claiming event health", "", true},
		{"overlap", "corner", "100 requests during one read", "one coalesced follow-up", "overlap", true},
		{"resync", "corner", "resync during read", "old association completion rejected", "resync", false},
		{"epoch", "negative", "source epoch changes during read", "old source completion rejected", "epoch", false},
		{"rename", "corner", "canonical name changes during read", "old revision completion rejected", "rename", false},
		{"timeout", "boundary", "five-second logical deadline", "worker remains occupied, late result ignored", "timeout", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, clock, p := rdmaScheduled(t, false)
			if err := s.advance(clock.Now()); err != nil {
				t.Fatal(err)
			}
			work := <-s.executor.inbox
			switch tc.change {
			case "overlap":
				for range 100 {
					s.request(p.Canonical)
				}
				if len(s.queue) != 1 {
					t.Fatal(tc.expectedOutcome)
				}
			case "resync":
				if err := s.replace([]model.RDMAPort{p}, clock.Now()); err != nil {
					t.Fatal(err)
				}
			case "epoch":
				if err := s.r.loseEvents(); err != nil {
					t.Fatal(err)
				}
			case "rename":
				d := s.r.slots[0].device
				d.Name = "renamed"
				mustObserve(t, s.r, model.Observation{Device: d})
			case "timeout":
				if err := clock.Advance(collectionBudget); err != nil {
					t.Fatal(err)
				}
				if err := s.advance(clock.Now()); err != nil {
					t.Fatal(err)
				}
				if s.active == nil || !s.timedOut {
					t.Fatal(tc.expectedOutcome)
				}
			}
			if err := s.complete(rdmaCompletion{work: work, ports: []model.RDMAPort{p}, finished: clock.Now()}); err != nil {
				t.Fatal(err)
			}
			state := &s.r.slots[0].collectors[model.CollectorRDMAState]
			if state.hasSuccess != tc.accepted || state.running || s.active != nil {
				t.Fatal(tc.expectedOutcome)
			}
			if s.r.publicationHealth(true, true).CollectionHealthy {
				t.Fatal("state polling claimed event health")
			}
		})
	}
}

func TestRDMAStatePublication(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		native                                       bool
		state, physical                              uint8
		check                                        model.Check
		up                                           uint64
	}{
		{"nativeActive", "positive", "ACTIVE and LINK_UP", "native counted up with passing readiness", true, 4, 5, model.CheckPass, 1},
		{"nativeInit", "negative", "INIT but physical LINK_UP", "native not up and readiness fails", true, 2, 5, model.CheckFail, 0},
		{"nativeArmed", "negative", "ARMED but physical LINK_UP", "readiness fails", true, 3, 5, model.CheckFail, 0},
		{"nativeDown", "positive", "physical link down", "count down, readiness not applicable", true, 1, 3, model.CheckNotApplicable, 0},
		{"roceActive", "positive", "Ethernet up and RDMA ACTIVE", "Ethernet counted once", false, 4, 5, model.CheckPass, 1},
		{"roceInactive", "negative", "Ethernet up and RDMA INIT", "readiness fails, Ethernet count retained", false, 2, 5, model.CheckFail, 1},
		{"future", "boundary", "future state enum", "unknown readiness", false, 255, 5, model.CheckUnknown, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			s, clock, p := rdmaScheduled(t, tc.native)
			p.State, p.Physical = presentValue(tc.state), presentValue(tc.physical)
			if err := s.advance(clock.Now()); err != nil {
				t.Fatal(err)
			}
			work := <-s.executor.inbox
			if err := s.complete(rdmaCompletion{work: work, ports: []model.RDMAPort{p}, finished: clock.Now()}); err != nil {
				t.Fatal(err)
			}
			slot := s.r.slots[0]
			if s.r.upCount != tc.up || effectiveChecks(slot).rdmaReadiness != tc.check {
				t.Fatalf("%s: count %d checks %+v", tc.expectedOutcome, s.r.upCount, effectiveChecks(slot))
			}
			if tc.native && effectiveChecks(slot).fullDuplex != model.CheckNotApplicable {
				t.Fatal("native duplex was negotiated")
			}
			if tc.native {
				samples, _ := rdmaSamples([]model.RDMAPort{p}, work.job.Device.Up)
				found := false
				for _, sample := range samples {
					if sample.Descriptor != "interface_duplex_info" {
						continue
					}
					found = true
					if len(sample.Labels) != 2 || sample.Labels[0] != (model.Label{Name: duplexLabel, Value: fullDuplexLabel}) || sample.Labels[1] != (model.Label{Name: "source", Value: "transport"}) {
						t.Fatal("native duplex metric changed the documented label contract")
					}
				}
				if !found {
					t.Fatal("native duplex information missing")
				}
			}
			block := slot.collectors[model.CollectorRDMAState].block
			if block == nil || len(block.values) == 0 {
				t.Fatal("no published RDMA samples")
			}
			s.r.deadlines.cancel(deadlineKey{kind: deadlineRDMA})
			s.r.expire(45*time.Second - 1)
			if !slot.collectors[model.CollectorRDMAState].fresh {
				t.Fatal("expired early")
			}
			s.r.expire(45 * time.Second)
			if effectiveChecks(slot).rdmaReadiness != model.CheckUnknown || s.r.upCount != tc.up {
				t.Fatal("expiry changed count or retained readiness")
			}
			if len(block.values) == 0 {
				t.Fatal("expiry mutated retained block")
			}
		})
	}
}

type blockedRDMAState struct{ entered, release, closed chan struct{} }

func (s *blockedRDMAState) Read(_ context.Context, p model.RDMAPort) (model.RDMAPort, error) {
	close(s.entered)
	<-s.release
	return p, nil
}
func (s *blockedRDMAState) Close() error { close(s.closed); return nil }

func TestRDMABlockedShutdown(t *testing.T) {
	t.Log("corner: physical read ignores cancellation; expected occupied executor and no close until read returns")
	s, clock, p := rdmaScheduled(t, false)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	source := &blockedRDMAState{entered: make(chan struct{}), release: make(chan struct{}), closed: make(chan struct{})}
	e := newRDMAExecutor(ctx, clock, source)
	s.executor = e
	if err := s.advance(clock.Now()); err != nil {
		t.Fatal(err)
	}
	<-source.entered
	cancel()
	if err := clock.Advance(collectionBudget); err != nil {
		t.Fatal(err)
	}
	if err := s.advance(clock.Now()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-source.closed:
		t.Fatal("closed active source")
	default:
	}
	if s.active == nil {
		t.Fatal("physical occupancy lost")
	}
	close(source.release)
	result := <-e.results
	if !errors.Is(result.err, context.Canceled) {
		t.Fatal(result.err)
	}
	if err := s.complete(result); err != nil {
		t.Fatal(err)
	}
	<-e.done
	if s.r.slots[0].collectors[model.CollectorRDMAState].hasSuccess || s.r.upCount != 1 || p.Canonical == (model.DeviceKey{}) {
		t.Fatal("late completion changed state")
	}
}

func TestRDMAMultiplePortsAndExceptions(t *testing.T) {
	t.Log("corner: two ports share Ethernet; expected independent samples, fail precedence, one counted link and current aliases")
	s, clock, p := rdmaScheduled(t, false)
	q := p
	q.Port = 2
	q.State = presentValue(uint8(2))
	q.Aliases = []string{"alias1"}
	ports := []model.RDMAPort{p, q}
	s.r.rdmaPorts = ports
	if err := s.replace(ports, clock.Now()); err != nil {
		t.Fatal(err)
	}
	if err := s.advance(clock.Now()); err != nil {
		t.Fatal(err)
	}
	work := <-s.executor.inbox
	if err := s.complete(rdmaCompletion{work: work, ports: ports, finished: clock.Now()}); err != nil {
		t.Fatal(err)
	}
	if s.r.upCount != 1 || effectiveChecks(s.r.slots[0]).rdmaReadiness != model.CheckFail {
		t.Fatal("port overwritten or double counted")
	}
	resolved := resolveExceptions([]string{"rdma:mlx5_0:2", "alias1"}, s.r.rdmaExceptionDevices())
	for _, item := range resolved {
		if item.status != exceptionMatched || item.target.key != p.Canonical {
			t.Fatal("alias did not resolve")
		}
	}
}

func BenchmarkRDMAUnchangedState(b *testing.B) {
	s, clock, p := rdmaScheduled(b, false)
	b.ReportAllocs()
	for b.Loop() {
		s.request(p.Canonical)
		if err := s.advance(clock.Now()); err != nil {
			b.Fatal(err)
		}
		work := <-s.executor.inbox
		if err := s.complete(rdmaCompletion{work: work, ports: []model.RDMAPort{p}, finished: clock.Now()}); err != nil {
			b.Fatal(err)
		}
	}
}
