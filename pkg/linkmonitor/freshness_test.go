package linkmonitor

import (
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
)

func timedResult(t *testing.T, r *reducer, clock model.Clock, kind model.CollectorKind, failure error) model.Job {
	t.Helper()
	key := model.JobKey{Namespace: r.namespace, Collector: kind}
	if kind != model.CollectorNetstat {
		key.Device = r.slots[0].device.Key
	}
	job, err := r.startCollection(key, clock.Now())
	if err != nil {
		t.Fatal(err)
	}
	result := model.Result{Job: job, Finished: clock.Now(), Support: model.Supported, Err: failure,
		Samples: []model.Sample{counter("sample_"+strconv.Itoa(int(kind)), 7, 64)}}
	if ok, err := r.finishCollection(result); !ok || err != nil {
		t.Fatalf("completion=%v,%v", ok, err)
	}
	return job
}

func publishNow(t *testing.T, r *reducer, m *Monitor, c model.Clock, baseline bool) Snapshot {
	t.Helper()
	state := publicationState{health: Health{Running: true}, now: c.Now(), expected: model.Optional[uint64]{Value: 1, Present: baseline}}
	if err := r.publish(m, state); err != nil {
		t.Fatal(err)
	}
	return m.Snapshot()
}

func advance(t *testing.T, c *testkit.Clock, duration time.Duration) {
	t.Helper()
	if err := c.Advance(duration); err != nil {
		t.Fatal(err)
	}
}

func deviceCollector(t *testing.T, s Snapshot, name string) CollectorView {
	t.Helper()
	var found CollectorView
	s.RangeDevices(func(d DeviceView) bool {
		d.RangeCollectors(func(c CollectorView) bool {
			if c.Name() == name {
				found = c
				return false
			}
			return true
		})
		return false
	})
	if found.Name() == "" {
		t.Fatalf("missing collector %s", name)
	}
	return found
}

func sampleCount(s Snapshot) int {
	n := 0
	s.RangeSamples(func(SampleView) bool { n++; return true })
	return n
}

func TestCollectorExpiry(t *testing.T) {
	for _, tc := range []struct {
		name, description, expected string
		kind                        model.CollectorKind
		lifetime                    time.Duration
	}{
		{"driver", "poll-derived driver counters", "expire at three poll intervals", model.CollectorDriver, 45 * time.Second},
		{"settings", "negotiated settings", "expire settings and applicable checks together", model.CollectorSettings, 45 * time.Second},
		{"identity", "configuration identity", "expire at two resync intervals", model.CollectorInventory, 2 * time.Hour},
		{"channels", "channel configuration", "use configuration lifetime", model.CollectorChannels, 2 * time.Hour},
		{"rings", "ring configuration", "use configuration lifetime", model.CollectorRings, 2 * time.Hour},
		{"netstat", "namespace host statistics", "expire independently of device state", model.CollectorNetstat, 45 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("boundary: %s; expected: %s", tc.description, tc.expected)
			c := testkit.NewClock(time.Unix(1000, 0))
			r, m := newReducer(1), new(Monitor)
			mustObserve(t, r, observed(r, 1, true))
			job := timedResult(t, r, c, tc.kind, nil)
			if tc.kind == model.CollectorSettings && !r.setChecks(job, deviceChecks{maximumSpeed: model.CheckPass, fullDuplex: model.CheckPass}) {
				t.Fatal("fresh policy rejected")
			}
			old := publishNow(t, r, m, c, true)
			advance(t, c, time.Second)
			timedResult(t, r, c, tc.kind, errors.New("temporary failure"))
			advance(t, c, tc.lifetime-time.Second-time.Nanosecond)
			c.ShiftWall(-7 * 24 * time.Hour)
			before := publishNow(t, r, m, c, true)
			if sampleCount(before) != 1 {
				t.Fatal("failure or wall-clock jump expired source early")
			}
			advance(t, c, time.Nanosecond)
			after := publishNow(t, r, m, c, true)
			if sampleCount(after) != 0 || sampleCount(old) != 1 {
				t.Fatal("expiry retained stale values or mutated old snapshot")
			}
			view := after.HostCollector()
			if tc.kind != model.CollectorNetstat {
				name := (CollectorView{kind: tc.kind}).Name()
				view = deviceCollector(t, after, name)
			}
			if wall, known := view.LastSuccess(); !known || !wall.Equal(time.Unix(1000, 0)) || view.Fresh() {
				t.Fatal("expiry lost last-success diagnostics or remained fresh")
			}
			if success, known := view.Success(); !known || success || view.ErrorReason() != "io" || view.Support() != "supported" {
				t.Fatal("latest failure/support lost after expiry")
			}
			if duration, known := view.Duration(); !known || duration != 0 {
				t.Fatal("zero-duration attempt lost presence")
			}
			if tc.kind == model.CollectorSettings {
				after.RangeDevices(func(d DeviceView) bool {
					if d.MaximumSpeedCheck() != CheckUnknown || d.FullDuplexCheck() != CheckUnknown {
						t.Fatal("expired settings left a healthy check")
					}
					return true
				})
			}
			timedResult(t, r, c, tc.kind, nil)
			fresh := publishNow(t, r, m, c, true)
			if sampleCount(fresh) != 1 || sampleCount(after) != 0 {
				t.Fatal("recovery changed historical expiry or failed to restore sample")
			}
			if len(r.deadlines.entries) != 1 {
				t.Fatal("recovery accumulated expiry entries")
			}
		})
	}
}

func TestExpiryRefreshAndCancellation(t *testing.T) {
	t.Log("corner: success replaces expiry, deletion cancels it; expected: old deadline does not erase newer/reused device state")
	c := testkit.NewClock(time.Unix(1000, 0))
	r, m := newReducer(1), new(Monitor)
	mustObserve(t, r, observed(r, 1, true))
	timedResult(t, r, c, model.CollectorDriver, nil)
	advance(t, c, 44*time.Second)
	timedResult(t, r, c, model.CollectorDriver, nil)
	advance(t, c, time.Second)
	if sampleCount(publishNow(t, r, m, c, true)) != 1 || len(r.deadlines.entries) != 1 {
		t.Fatal("old deadline removed refreshed value or was retained")
	}
	key := r.slots[0].device.Key
	if _, err := r.remove(key, model.Token{SourceEpoch: r.epoch}); err != nil {
		t.Fatal(err)
	}
	if len(r.deadlines.entries) != 0 {
		t.Fatal("device retirement retained pending expiry")
	}
	mustObserve(t, r, observed(r, 1, true))
	timedResult(t, r, c, model.CollectorDriver, nil)
	advance(t, c, 44*time.Second)
	if sampleCount(publishNow(t, r, m, c, true)) != 1 {
		t.Fatal("retired device's deadline affected replacement")
	}
}

func TestPolicyRevisionAndRecovery(t *testing.T) {
	t.Log("corner: event during settings lifetime, then late policy; expected: unknown until fresh matching policy succeeds")
	c := testkit.NewClock(time.Unix(1000, 0))
	r, m := newReducer(1), new(Monitor)
	mustObserve(t, r, observed(r, 1, true))
	job := timedResult(t, r, c, model.CollectorSettings, nil)
	checks := deviceChecks{maximumSpeed: model.CheckPass, fullDuplex: model.CheckFail}
	if !r.setChecks(job, checks) {
		t.Fatal("fresh policy rejected")
	}
	before := publishNow(t, r, m, c, true)
	mustObserve(t, r, observed(r, 1, false))
	mustObserve(t, r, observed(r, 1, true))
	if r.setChecks(job, checks) {
		t.Fatal("old revision reinstated policy")
	}
	after := publishNow(t, r, m, c, true)
	after.RangeDevices(func(d DeviceView) bool {
		if d.MaximumSpeedCheck() != CheckUnknown || d.FullDuplexCheck() != CheckUnknown {
			t.Fatal("new negotiation inherited old policy")
		}
		return true
	})
	before.RangeDevices(func(d DeviceView) bool {
		if d.MaximumSpeedCheck() != CheckPass || d.FullDuplexCheck() != CheckFail {
			t.Fatal("new revision changed historical checks")
		}
		return true
	})
	job = timedResult(t, r, c, model.CollectorSettings, nil)
	if !r.setChecks(job, checks) {
		t.Fatal("replacement policy rejected")
	}
}
