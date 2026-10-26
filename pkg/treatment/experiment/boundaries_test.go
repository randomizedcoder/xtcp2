package experiment

import (
	"reflect"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/internal/fixtures"
)

func TestEvaluationBoundaries(t *testing.T) {
	now := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		name, description string
		want              treatment.Outcome
		wantError         bool
		change            func(*treatment.Experiment, *treatment.Evidence)
	}{
		{"no-phase", "unstarted experiment cannot be evaluated", treatment.OutcomeUnspecified, true, func(e *treatment.Experiment, _ *treatment.Evidence) { e.Phases = nil }},
		{"no-observation", "all canaries must acknowledge exposure", treatment.OutcomeAwaitingExposure, false, func(e *treatment.Experiment, _ *treatment.Evidence) { e.Phases[0].ObservationStartedAt = time.Time{} }},
		{"expiry-equality", "phase expires exactly at its deadline", treatment.OutcomeExpired, false, func(e *treatment.Experiment, _ *treatment.Evidence) {
			e.Phases[0].IssuedAt = now.Add(-24 * time.Hour)
		}},
		{"freshness-equality", "evidence at maximum age is usable", treatment.OutcomeBeneficial, false, func(_ *treatment.Experiment, v *treatment.Evidence) { v.WindowEnd = now.Add(-2 * time.Minute) }},
		{"freshness-over", "one nanosecond beyond maximum age is stale", treatment.OutcomeStale, false, func(_ *treatment.Experiment, v *treatment.Evidence) {
			v.WindowEnd = now.Add(-2*time.Minute - time.Nanosecond)
		}},
		{"clock-equality", "30 seconds of forward clock skew is allowed", treatment.OutcomeBeneficial, false, func(_ *treatment.Experiment, v *treatment.Evidence) { v.AnalyzedAt = now.Add(30 * time.Second) }},
		{"clock-over", "excess forward skew is rejected", treatment.OutcomeStale, false, func(_ *treatment.Experiment, v *treatment.Evidence) {
			v.AnalyzedAt = now.Add(30*time.Second + time.Nanosecond)
		}},
		{"duration-equality", "exactly minimum observation is sufficient", treatment.OutcomeBeneficial, false, func(_ *treatment.Experiment, v *treatment.Evidence) {
			v.WindowStart = v.WindowEnd.Add(-30 * time.Minute)
		}},
		{"duration-under", "one nanosecond short is insufficient", treatment.OutcomeInsufficientDuration, false, func(_ *treatment.Experiment, v *treatment.Evidence) {
			v.WindowStart = v.WindowEnd.Add(-30*time.Minute + time.Nanosecond)
		}},
		{"old-window", "pre-treatment data cannot support promotion", treatment.OutcomeInsufficientDuration, false, func(e *treatment.Experiment, v *treatment.Evidence) {
			v.WindowStart = e.Phases[0].ObservationStartedAt.Add(-time.Nanosecond)
		}},
		{"zero-target", "phase must have at least one canary", treatment.OutcomeUnspecified, true, func(e *treatment.Experiment, _ *treatment.Evidence) { e.Phases[0].TargetHosts = 0 }},
		{"over-target", "phase cannot exceed fleet size", treatment.OutcomeUnspecified, true, func(e *treatment.Experiment, _ *treatment.Evidence) { e.Phases[0].TargetHosts = 17 }},
		{"sample-equality", "exact sample and exposure thresholds suffice", treatment.OutcomeBeneficial, false, func(e *treatment.Experiment, v *treatment.Evidence) {
			for i := range v.Hosts {
				h := &v.Hosts[i]
				h.Sockets = e.Policy.MinimumSocketsPerHost
				h.ExposedSockets = h.Sockets
				h.BytesAcked = e.Policy.MinimumBytesPerHost
			}
		}},
		{"missing-host", "missing cohort host blocks evaluation", treatment.OutcomeInsufficientData, false, func(_ *treatment.Experiment, v *treatment.Evidence) { v.Hosts = v.Hosts[1:] }},
		{"missing-baseline", "missing baseline host blocks evaluation", treatment.OutcomeInsufficientData, false, func(e *treatment.Experiment, _ *treatment.Evidence) { e.Baseline.Hosts = e.Baseline.Hosts[1:] }},
		{"low-bytes", "too few bytes blocks evaluation", treatment.OutcomeInsufficientData, false, func(e *treatment.Experiment, v *treatment.Evidence) {
			v.Hosts[0].BytesAcked = e.Policy.MinimumBytesPerHost - 1
		}},
		{"zero-goodput", "zero goodput cannot enter log comparison", treatment.OutcomeInsufficientData, false, func(_ *treatment.Experiment, v *treatment.Evidence) { v.Hosts[0].GoodputBps = 0 }},
		{"zero-baseline-rtt", "zero baseline RTT cannot enter ratio", treatment.OutcomeInsufficientData, false, func(e *treatment.Experiment, _ *treatment.Evidence) { e.Baseline.Hosts[0].RttP95Ms = 0 }},
		{"first-canary", "small cohorts permit safety review only", treatment.OutcomeSafetyOnly, false, func(e *treatment.Experiment, _ *treatment.Evidence) { e.Phases[0].TargetHosts = 1 }},
		{"small-cohort-regression", "large goodput loss blocks small cohort promotion", treatment.OutcomeHarmful, false, func(e *treatment.Experiment, v *treatment.Evidence) {
			e.Phases[0].TargetHosts = 1
			v.Hosts[0].GoodputBps = 1
		}},
		{"one-control", "one control cannot estimate variance", treatment.OutcomeInsufficientControls, false, func(e *treatment.Experiment, _ *treatment.Evidence) { e.Phases[0].TargetHosts = 15 }},
		{"full-fleet", "last phase checks safety without controls", treatment.OutcomeFinalSafety, false, func(e *treatment.Experiment, _ *treatment.Evidence) { e.Phases[0].TargetHosts = 16 }},
		{"full-fleet-harm", "last phase still rejects goodput regression", treatment.OutcomeHarmful, false, func(e *treatment.Experiment, v *treatment.Evidence) {
			e.Phases[0].TargetHosts = 16
			for i := range v.Hosts {
				h := &v.Hosts[i]
				h.GoodputBps = 1
			}
		}},
		{"invalid-evidence", "malformed evidence fails before decision", treatment.OutcomeUnspecified, true, func(_ *treatment.Experiment, v *treatment.Evidence) { v.Hosts[0] = treatment.HostEvidence{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, v := fixtures.Experiment(now, treatment.DefaultPolicy())
			tc.change(e, v)
			before := e.Clone()
			evidenceBefore := v.Clone()
			got, err := Evaluate(e, v, now)
			if (err != nil) != tc.wantError || got.Outcome != tc.want {
				t.Fatalf("%s: got %+v err=%v; want %s error=%t", tc.description, got, err, tc.want, tc.wantError)
			}
			if !reflect.DeepEqual(e, before) || !reflect.DeepEqual(v, evidenceBefore) {
				t.Fatal("evaluation mutated its input")
			}
		})
	}
}

func TestSafetyGateBoundaries(t *testing.T) {
	now := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		name, description string
		change            func(*treatment.Experiment, *treatment.Evidence)
		healthy, harmful  bool
	}{
		{"healthy", "complete unchanged observations renew leases", func(*treatment.Experiment, *treatment.Evidence) {}, true, false},
		{"missing-host", "partial evidence cannot renew leases", func(_ *treatment.Experiment, v *treatment.Evidence) { v.Hosts = v.Hosts[1:] }, false, false},
		{"unhealthy-collector", "collection failures cannot renew leases", func(_ *treatment.Experiment, v *treatment.Evidence) { v.Hosts[0].CollectionHealthy = false }, false, false},
		{"low-samples", "too few samples cannot renew leases", func(e *treatment.Experiment, v *treatment.Evidence) {
			v.Hosts[0].Sockets = e.Policy.MinimumSocketsPerHost - 1
			v.Hosts[0].ExposedSockets = 0
		}, false, false},
		{"stale", "old observations cannot renew leases", func(_ *treatment.Experiment, v *treatment.Evidence) { v.WindowEnd = now.Add(-3 * time.Minute) }, false, false},
		{"loss-equality", "loss guard is strictly greater than its limit", func(e *treatment.Experiment, v *treatment.Evidence) {
			v.Hosts[0].RetransmissionRatio = e.Policy.MaximumRetransmissionIncrease
		}, true, false},
		{"loss-over", "excess loss is harmful", func(e *treatment.Experiment, v *treatment.Evidence) {
			v.Hosts[0].RetransmissionRatio = e.Policy.MaximumRetransmissionIncrease + 1e-9
		}, false, true},
		{"rtt-equality", "exactly representable RTT limit is inclusive", func(e *treatment.Experiment, v *treatment.Evidence) {
			e.Policy.MaximumRTTIncrease = .5
			v.Hosts[0].RttP95Ms = 1.5
		}, true, false},
		{"control-harm", "controls are protected too", func(_ *treatment.Experiment, v *treatment.Evidence) { v.Hosts[15].RttP95Ms = 10 }, false, true},
		{"partial-harm", "missing data does not conceal observed harm", func(_ *treatment.Experiment, v *treatment.Evidence) { v.Hosts = v.Hosts[1:]; v.Hosts[0].RttP95Ms = 10 }, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, v := fixtures.Experiment(now, treatment.DefaultPolicy())
			tc.change(e, v)
			h, bad := HealthyEvidence(e, v, now)
			if h != tc.healthy || bad != tc.harmful {
				t.Fatalf("%s: healthy=%t harmful=%t; want %t/%t", tc.description, h, bad, tc.healthy, tc.harmful)
			}
		})
	}
}

func TestAdvanceTransitions(t *testing.T) {
	now := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		name, description string
		change            func(*treatment.Experiment, *treatment.Evidence)
		want              treatment.Status
		phases            int
		wantError         bool
	}{
		{"initial", "approved proposal starts one canary", func(e *treatment.Experiment, _ *treatment.Evidence) {
			e.Phases = nil
			e.Status = treatment.StatusProposed
		}, treatment.StatusRunning, 1, false},
		{"promotion", "benefit permits the next reviewed phase", func(*treatment.Experiment, *treatment.Evidence) {}, treatment.StatusRunning, 2, false},
		{"completion", "full fleet safety review completes the experiment", func(e *treatment.Experiment, _ *treatment.Evidence) { e.Phases[0].TargetHosts = 16 }, treatment.StatusComplete, 1, false},
		{"rolled-back", "rolled back experiments cannot advance", func(e *treatment.Experiment, _ *treatment.Evidence) { e.Status = treatment.StatusRolledBack }, 0, 0, true},
		{"harm", "harm cannot be promoted", func(_ *treatment.Experiment, v *treatment.Evidence) { v.Hosts[0].RttP95Ms = 100 }, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, v := fixtures.Experiment(now, treatment.DefaultPolicy())
			tc.change(e, v)
			before := e.Clone()
			got, err := Advance(e, v, now, "reviewer", "measured benefit")
			if (err != nil) != tc.wantError {
				t.Fatalf("%s: err=%v wantError=%t", tc.description, err, tc.wantError)
			}
			if err == nil {
				if got.Status != tc.want || len(got.Phases) != tc.phases || got.Revision != e.Revision+1 {
					t.Fatalf("%s: wrong transition: %v", tc.description, got)
				}
				last := got.Phases[len(got.Phases)-1]
				if last.Reviewer != "reviewer" || last.Reason != "measured benefit" {
					t.Fatal("review provenance missing")
				}
			}
			if !reflect.DeepEqual(e, before) {
				t.Fatal("advance mutated input")
			}
		})
	}
}

func TestNextCohortBoundaries(t *testing.T) {
	for _, tc := range []struct {
		description          string
		current, total, want int
	}{
		{"start smallest fleet", 0, 3, 1}, {"reserve two controls", 1, 3, 3}, {"start large fleet", 0, 10000, 1}, {"double first canary", 1, 10000, 2}, {"double second phase", 2, 10000, 4}, {"cap before consuming controls", 4096, 10000, 8192}, {"retain final two controls", 8192, 10000, 9998}, {"final reviewed expansion", 9998, 10000, 10000}, {"completed fleet is unchanged", 10000, 10000, 10000},
	} {
		t.Run(tc.description, func(t *testing.T) {
			if got := NextCohort(tc.current, tc.total); got != tc.want {
				t.Fatalf("got %d want %d", got, tc.want)
			}
		})
	}
}

func TestDefaultRTTGuardExactBoundary(t *testing.T) {
	for _, tc := range []struct {
		description string
		rtt         float64
		want        treatment.Outcome
		healthy     bool
	}{
		{"exactly ten percent increase is within the configured guard", 5.5, treatment.OutcomeBeneficial, true},
		{"measurable increase above ten percent is harmful", 5.500001, treatment.OutcomeHarmful, false},
	} {
		t.Run(tc.description, func(t *testing.T) {
			now := time.Unix(1700000000, 0)
			e, v := fixtures.Experiment(now, treatment.DefaultPolicy())
			for i := range e.Baseline.Hosts {
				h := &e.Baseline.Hosts[i]
				h.RttP95Ms = 5
			}
			for i := range v.Hosts {
				h := &v.Hosts[i]
				h.RttP95Ms = tc.rtt
			}
			healthy, harm := HealthyEvidence(e, v, now)
			if healthy != tc.healthy || harm == tc.healthy {
				t.Errorf("safety gate healthy=%t harmful=%t; want healthy=%t", healthy, harm, tc.healthy)
			}
			got, err := Evaluate(e, v, now)
			if err != nil || got.Outcome != tc.want {
				t.Fatalf("evaluation %v %v; want %s", got, err, tc.want)
			}
		})
	}
}

func TestRTTGuardFleetBoundary(t *testing.T) {
	for _, n := range []int{100, 1000, 10000} {
		e, v, now := fleetFixture(n)
		for i := range v.Hosts {
			h := &v.Hosts[i]
			h.RttP95Ms = 5.5
		}
		got, err := Evaluate(e, v, now)
		if err != nil || got.Outcome != treatment.OutcomeBeneficial {
			t.Errorf("%d hosts at exactly the RTT guard: %v %v", n, got, err)
		}
	}
}
