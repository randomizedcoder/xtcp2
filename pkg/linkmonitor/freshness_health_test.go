package linkmonitor

import (
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
)

func resynced(t *testing.T, r *reducer, c model.Clock) {
	t.Helper()
	if !r.recordResync(model.Token{SourceEpoch: r.epoch, Revision: r.revision}, c.Now()) {
		t.Fatal("current complete resync rejected")
	}
}

func TestConcurrentExpiryPublication(t *testing.T) {
	t.Log("corner/concurrency: required RDMA state repeatedly expires and recovers; expected: each snapshot agrees on sample presence, freshness and readiness")
	c := testkit.NewClock(time.Unix(1000, 0))
	r, m := newReducer(1), new(Monitor)
	o := observed(r, 1, true)
	o.Device.RDMA = true
	mustObserve(t, r, o)
	subscriptions(t, r, true, true)
	resynced(t, r, c)
	timedResult(t, r, c, model.CollectorRDMAState, nil)
	publishNow(t, r, m, c, true)
	start := make(chan struct{})
	var readers sync.WaitGroup
	for range 4 {
		readers.Add(1)
		go func() {
			defer readers.Done()
			<-start
			for range 500 {
				s := m.Snapshot()
				var fresh bool
				s.RangeDevices(func(d DeviceView) bool {
					d.RangeCollectors(func(v CollectorView) bool {
						if v.Name() == "rdma_state" {
							fresh = v.Fresh()
						}
						return true
					})
					return true
				})
				if fresh != (sampleCount(s) == 1) || fresh != s.Health().Ready || fresh != s.Health().CollectionHealthy {
					t.Error("reader observed mixed expiry/health publication")
					return
				}
				runtime.Gosched()
			}
		}()
	}
	close(start)
	for range 50 {
		advance(t, c, 45*time.Second)
		publishNow(t, r, m, c, true)
		runtime.Gosched()
		timedResult(t, r, c, model.CollectorRDMAState, nil)
		publishNow(t, r, m, c, true)
	}
	readers.Wait()
}

func subscriptions(t *testing.T, r *reducer, route, rdma bool) {
	t.Helper()
	if err := r.setSubscriptions(route, rdma); err != nil {
		t.Fatal(err)
	}
}

func TestCollectionHealth(t *testing.T) {
	for _, tc := range []struct {
		name, description, expected string
		change                      func(*testing.T, *reducer, *testkit.Clock)
		baseline, healthy, ready    bool
	}{
		{"ready", "complete inventory, live events and baseline", "ready and healthy", func(*testing.T, *reducer, *testkit.Clock) {}, true, true, true},
		{"no baseline", "complete collection without baseline", "healthy but not ready", func(*testing.T, *reducer, *testkit.Clock) {}, false, true, false},
		{"optional failure", "driver statistics fail", "collection health unaffected", func(t *testing.T, r *reducer, c *testkit.Clock) {
			timedResult(t, r, c, model.CollectorDriver, errors.New("optional failure"))
		}, true, true, true},
		{"optional expiry", "driver values expire", "collection health unaffected", func(t *testing.T, r *reducer, c *testkit.Clock) {
			timedResult(t, r, c, model.CollectorDriver, nil)
			advance(t, c, 45*time.Second)
		}, true, true, true},
		{"unknown classification", "inventory evidence becomes unknown", "unhealthy", func(t *testing.T, r *reducer, _ *testkit.Clock) {
			o := observed(r, 1, true)
			o.Device.Eligibility = model.EligibilityUnknown
			mustObserve(t, r, o)
		}, true, false, false},
		{"loss", "route subscription fails", "unhealthy immediately", func(t *testing.T, r *reducer, _ *testkit.Clock) {
			subscriptions(t, r, false, false)
		}, true, false, false},
		{"restart only", "subscription restarts without reconciliation", "stay unhealthy", func(t *testing.T, r *reducer, _ *testkit.Clock) {
			subscriptions(t, r, false, false)
			subscriptions(t, r, true, false)
		}, true, false, false},
		{"recovered", "new subscription and complete current-epoch reconciliation", "healthy again", func(t *testing.T, r *reducer, c *testkit.Clock) {
			subscriptions(t, r, false, false)
			subscriptions(t, r, true, false)
			resynced(t, r, c)
		}, true, true, true},
		{"resync boundary", "exactly two resync intervals", "not overdue yet", func(t *testing.T, _ *reducer, c *testkit.Clock) {
			advance(t, c, 2*time.Hour)
		}, true, true, true},
		{"resync overdue", "one nanosecond beyond two intervals", "unhealthy", func(t *testing.T, _ *reducer, c *testkit.Clock) {
			advance(t, c, 2*time.Hour+time.Nanosecond)
		}, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("positive/negative/boundary: %s; expected: %s", tc.description, tc.expected)
			c := testkit.NewClock(time.Unix(1000, 0))
			r, m := newReducer(1), new(Monitor)
			mustObserve(t, r, observed(r, 1, true))
			subscriptions(t, r, true, false)
			resynced(t, r, c)
			before := publishNow(t, r, m, c, true)
			tc.change(t, r, c)
			s := publishNow(t, r, m, c, tc.baseline)
			health := s.Health()
			if health.CollectionHealthy != tc.healthy || health.Ready != tc.ready || health.BaselineReady != tc.baseline || !health.Running {
				t.Fatalf("health=%+v", health)
			}
			if !before.Health().Ready {
				t.Fatal("later health update mutated old snapshot")
			}
			if wall, present := s.LastSuccessfulResync(); !present || !wall.Equal(time.Unix(1000, 0)) {
				t.Fatal("health failure erased last successful reconciliation")
			}
		})
	}
}

func TestRDMAHealth(t *testing.T) {
	for _, tc := range []struct {
		name, description, expected         string
		native, state, capabilities, events bool
		unsupported                         bool
		elapsed                             time.Duration
		healthy                             bool
	}{
		{"native ready", "native state and events healthy, capabilities unavailable", "ready with unknown speed and inapplicable duplex", true, true, false, true, false, 0, true},
		{"RoCE ready", "Ethernet up plus fresh RDMA state", "ready without a second counted link", false, true, false, true, false, 0, true},
		{"no state", "capabilities present but required state missing", "unhealthy", true, false, true, true, false, 0, false},
		{"no events", "fresh state, RDMA subscription not (yet) established", "unhealthy: only a kernel without events falls back", true, true, true, false, false, 0, false},
		{"just before expiry", "required state age just under 45s", "healthy", true, true, true, true, false, 45*time.Second - 1, true},
		{"state expires", "required state reaches 45s", "unhealthy with unknown speed/width/readiness", true, true, true, true, false, 45 * time.Second, false},
		{"RoCE expires", "RDMA state expires while Ethernet remains up", "unhealthy but preserve Ethernet up count", false, true, true, true, false, 45 * time.Second, false},
		{"RoCE events unsupported", "fresh RDMA state, kernel without RDMA events (stock ib_core)", "healthy on polled state", false, true, false, false, true, 0, true},
		{"native events unsupported", "fresh native state, kernel without RDMA events", "healthy on polled state", true, true, true, false, true, 0, true},
		{"unsupported no state", "kernel without RDMA events and no RDMA state", "unhealthy: the fallback needs fresh state", false, false, false, false, true, 0, false},
		{"unsupported just before expiry", "kernel without RDMA events, state age just under 45s", "healthy", false, true, false, false, true, 45*time.Second - 1, true},
		{"unsupported state expires", "kernel without RDMA events, state reaches 45s", "unhealthy: a stalled poller is not hidden", false, true, false, false, true, 45 * time.Second, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("positive/negative/boundary: %s; expected: %s", tc.description, tc.expected)
			c := testkit.NewClock(time.Unix(1000, 0))
			r, m := newReducer(1), new(Monitor)
			o := observed(r, 1, true)
			o.Device.RDMA = true
			if tc.native {
				o.Device.Key = model.DeviceKey{Namespace: 1, Kind: model.DeviceNativeRDMA, RDMADevice: "mlx5_0", Port: 1}
			}
			mustObserve(t, r, o)
			if tc.state {
				job := timedResult(t, r, c, model.CollectorRDMAState, nil)
				if !r.setChecks(job, deviceChecks{rdmaReadiness: model.CheckPass}) {
					t.Fatal("fresh RDMA readiness rejected")
				}
			}
			if tc.capabilities {
				job := timedResult(t, r, c, model.CollectorRDMACapabilities, nil)
				if tc.native && !r.setChecks(job, deviceChecks{maximumSpeed: model.CheckPass, maximumWidth: model.CheckPass}) {
					t.Fatal("fresh native capability checks rejected")
				}
			}
			subscriptions(t, r, true, tc.events)
			r.rdmaEventsUnsupported = tc.unsupported
			resynced(t, r, c)
			advance(t, c, tc.elapsed)
			s := publishNow(t, r, m, c, true)
			if s.Health().CollectionHealthy != tc.healthy || s.Health().Ready != tc.healthy {
				t.Fatalf("health=%+v", s.Health())
			}
			if count, known := s.Counts().Current(); count != 1 || !known {
				t.Fatal("RDMA expiry changed retained event-driven count")
			}
			s.RangeDevices(func(d DeviceView) bool {
				if tc.native && d.FullDuplexCheck() != CheckNotApplicable {
					t.Fatal("native duplex presented as measured")
				}
				if tc.elapsed >= 45*time.Second && (d.MaximumSpeedCheck() != CheckUnknown || d.RDMAReadinessCheck() != CheckUnknown) {
					t.Fatal("expired RDMA source retained policy pass")
				}
				return true
			})
			if _, err := r.remove(o.Device.Key, model.Token{SourceEpoch: r.epoch}); err != nil {
				t.Fatal(err)
			}
			if r.requiredRDMA != 0 || r.missingRDMA != 0 {
				t.Fatal("retired RDMA device remained in health accounting")
			}
		})
	}
}

// TestCountReadyRDMAEvents is the gate on recording the baseline. Before the
// fix a kernel without RDMA events held it closed for good, so 68 prod hosts
// never wrote link-monitor-baseline.json.
func TestCountReadyRDMAEvents(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		rdma, events, unsupported, resync            bool
		rdmaUncertain                                uint64
		ready                                        bool
	}{
		{"no-rdma", "positive", "no RDMA device, no RDMA events", "ready", false, false, false, true, 0, true},
		{"events", "positive", "RDMA device with RDMA events", "ready", true, true, false, true, 0, true},
		{"unsupported", "positive", "RDMA device, kernel without RDMA events", "ready on polled state", true, false, true, true, 0, true},
		{"not-subscribed", "negative", "RDMA device, events not (yet) subscribed", "not ready", true, false, false, true, 0, false},
		{"unsupported-no-resync", "boundary", "kernel without RDMA events, before the first complete resync", "not ready", true, false, true, false, 0, false},
		{"unsupported-uncertain", "corner", "kernel without RDMA events and an uncertain RDMA association", "not ready: uncertainty still blocks", true, false, true, true, 1, false},
		{"unsupported-and-events", "corner", "flag set while events are also live", "ready", true, true, true, true, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			c := testkit.NewClock(time.Unix(1000, 0))
			r := newReducer(1)
			o := observed(r, 1, true)
			o.Device.RDMA = tc.rdma
			mustObserve(t, r, o)
			subscriptions(t, r, true, tc.events)
			r.rdmaEventsUnsupported = tc.unsupported
			r.rdmaUncertain = tc.rdmaUncertain
			if tc.resync {
				resynced(t, r, c)
			}
			if got := r.countReady(); got != tc.ready {
				t.Fatalf("%s: countReady=%v requiredRDMA=%d", tc.expectedOutcome, got, r.requiredRDMA)
			}
		})
	}
}
