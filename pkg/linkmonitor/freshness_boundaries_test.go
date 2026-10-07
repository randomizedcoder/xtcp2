package linkmonitor

import (
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
)

func TestConfiguredFreshnessAndWallRecovery(t *testing.T) {
	t.Log("positive/corner: configured two-second poll, wall jumps forward; expected: expiry at six elapsed seconds, timestamp from wall clock on recovery")
	r, m := newReducer(1), new(Monitor)
	cfg := DefaultConfig()
	cfg.StatsInterval, cfg.Resync = 2*time.Second, 10*time.Second
	if err := r.configureFreshness(cfg); err != nil {
		t.Fatal(err)
	}
	c := testkit.NewClock(time.Time{}) // A successful sample at wall zero remains present.
	mustObserve(t, r, observed(r, 1, true))
	if err := r.configureFreshness(DefaultConfig()); err == nil {
		t.Fatal("active deadlines could be silently reconfigured")
	}
	timedResult(t, r, c, model.CollectorDriver, nil)
	c.ShiftWall(7 * 24 * time.Hour)
	advance(t, c, 6*time.Second-time.Nanosecond)
	before := publishNow(t, r, m, c, true)
	view := deviceCollector(t, before, "driver")
	if wall, known := view.LastSuccess(); !known || !wall.IsZero() || !view.Fresh() {
		t.Fatal("zero wall timestamp lost presence or wall jump changed expiry")
	}
	advance(t, c, time.Nanosecond)
	after := publishNow(t, r, m, c, true)
	view = deviceCollector(t, after, "driver")
	if sampleCount(after) != 0 || view.Fresh() {
		t.Fatal("custom lifetime was ignored")
	}
	if success, known := view.Success(); !known || !success {
		t.Fatal("expiry overwrote the last attempt's successful outcome")
	}
	timedResult(t, r, c, model.CollectorDriver, nil)
	view = deviceCollector(t, publishNow(t, r, m, c, true), "driver")
	if wall, known := view.LastSuccess(); !known || !wall.Equal(c.Now().Wall) {
		t.Fatal("recovery timestamp did not follow shifted wall clock")
	}
}

func TestUnsupportedAndMalformedFreshness(t *testing.T) {
	for _, tc := range []struct {
		name, description, expected string
		support                     model.Support
		malformed                   bool
	}{
		{"unsupported", "supported source becomes unsupported", "omit values and cancel expiry", model.Unsupported, false},
		{"not applicable", "source becomes inapplicable", "omit without an I/O failure", model.NotApplicable, false},
		{"malformed", "negative collection duration", "retain prior sample and original expiry", model.Supported, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("negative/corner: %s; expected: %s", tc.description, tc.expected)
			r, m := newReducer(1), new(Monitor)
			c := testkit.NewClock(time.Unix(1000, 0))
			mustObserve(t, r, observed(r, 1, true))
			timedResult(t, r, c, model.CollectorDriver, nil)
			advance(t, c, time.Second)
			key := model.JobKey{Namespace: 1, Device: r.slots[0].device.Key, Collector: model.CollectorDriver}
			job, err := r.startCollection(key, c.Now())
			if err != nil {
				t.Fatal(err)
			}
			result := model.Result{Job: job, Finished: c.Now(), Support: tc.support}
			if tc.malformed {
				result.Finished.Monotonic = 0
			}
			if accepted, err := r.finishCollection(result); !accepted || (err != nil) != tc.malformed {
				t.Fatalf("completion=%v,%v", accepted, err)
			}
			snapshot := publishNow(t, r, m, c, true)
			view := deviceCollector(t, snapshot, "driver")
			if tc.malformed {
				if !view.Fresh() || view.ErrorReason() != "malformed" || sampleCount(snapshot) != 1 || len(r.deadlines.entries) != 1 {
					t.Fatal("malformed completion changed sample/expiry")
				}
			} else if view.Fresh() || view.ErrorReason() != "" || sampleCount(snapshot) != 0 || len(r.deadlines.entries) != 0 {
				t.Fatal("unsupported source retained data/deadline or became an error")
			}
			if wall, known := view.LastSuccess(); !known || !wall.Equal(time.Unix(1000, 0)) {
				t.Fatal("latest support/failure erased previous sample timestamp")
			}
		})
	}
}

func TestResyncAndRDMARecoveryBoundaries(t *testing.T) {
	t.Log("corner: RDMA event loss, obsolete resync and state recovery; expected: no healthy publication before both current-epoch resync and fresh state")
	c := testkit.NewClock(time.Unix(1000, 0))
	r, m := newReducer(1), new(Monitor)
	o := observed(r, 1, true)
	o.Device.RDMA = true
	mustObserve(t, r, o)
	timedResult(t, r, c, model.CollectorRDMAState, nil)
	subscriptions(t, r, true, true)
	resynced(t, r, c)
	oldToken := model.Token{SourceEpoch: r.epoch, Revision: r.revision}
	subscriptions(t, r, true, false)
	subscriptions(t, r, true, true)
	if r.recordResync(oldToken, c.Now()) {
		t.Fatal("old-epoch resync accepted")
	}
	advance(t, c, 45*time.Second)
	resynced(t, r, c)
	if publishNow(t, r, m, c, true).Health().Ready {
		t.Fatal("fresh resync concealed stale required RDMA state")
	}
	timedResult(t, r, c, model.CollectorRDMAState, nil)
	if !publishNow(t, r, m, c, true).Health().Ready {
		t.Fatal("recovered state failed to restore health")
	}
	invalid := model.Stamp{Monotonic: -1}
	if r.recordResync(model.Token{SourceEpoch: r.epoch, Revision: r.revision}, invalid) {
		t.Fatal("negative monotonic timestamp accepted")
	}
	var zero Snapshot
	if _, known := zero.LastSuccessfulResync(); known {
		t.Fatal("uninitialized snapshot fabricated reconciliation")
	}
	if _, known := zero.HostCollector().Success(); known || zero.HostCollector().Support() != "unknown" {
		t.Fatal("unprobed host collector fabricated success/support")
	}
}
