package experiment

import (
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_controller"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/internal/fixtures"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestEvaluationBoundaries(t *testing.T) {
	now := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		name, description, want string
		wantError               bool
		change                  func(*pb.Experiment, *pb.Evidence)
	}{
		{"no-phase", "unstarted experiment cannot be evaluated", "", true, func(e *pb.Experiment, _ *pb.Evidence) { e.Phases = nil }},
		{"no-observation", "all canaries must acknowledge exposure", "awaiting_exposure", false, func(e *pb.Experiment, _ *pb.Evidence) { e.Phases[0].ObservationStartedAt = nil }},
		{"expiry-equality", "phase expires exactly at its deadline", "expired", false, func(e *pb.Experiment, _ *pb.Evidence) {
			e.Phases[0].IssuedAt = timestamppb.New(now.Add(-24 * time.Hour))
		}},
		{"freshness-equality", "evidence at maximum age is usable", "beneficial", false, func(_ *pb.Experiment, v *pb.Evidence) { v.WindowEnd = timestamppb.New(now.Add(-2 * time.Minute)) }},
		{"freshness-over", "one nanosecond beyond maximum age is stale", "stale", false, func(_ *pb.Experiment, v *pb.Evidence) {
			v.WindowEnd = timestamppb.New(now.Add(-2*time.Minute - time.Nanosecond))
		}},
		{"clock-equality", "30 seconds of forward clock skew is allowed", "beneficial", false, func(_ *pb.Experiment, v *pb.Evidence) { v.AnalyzedAt = timestamppb.New(now.Add(30 * time.Second)) }},
		{"clock-over", "excess forward skew is rejected", "stale", false, func(_ *pb.Experiment, v *pb.Evidence) {
			v.AnalyzedAt = timestamppb.New(now.Add(30*time.Second + time.Nanosecond))
		}},
		{"duration-equality", "exactly minimum observation is sufficient", "beneficial", false, func(_ *pb.Experiment, v *pb.Evidence) {
			v.WindowStart = timestamppb.New(v.WindowEnd.AsTime().Add(-30 * time.Minute))
		}},
		{"duration-under", "one nanosecond short is insufficient", "insufficient_duration", false, func(_ *pb.Experiment, v *pb.Evidence) {
			v.WindowStart = timestamppb.New(v.WindowEnd.AsTime().Add(-30*time.Minute + time.Nanosecond))
		}},
		{"old-window", "pre-treatment data cannot support promotion", "insufficient_duration", false, func(e *pb.Experiment, v *pb.Evidence) {
			v.WindowStart = timestamppb.New(e.Phases[0].ObservationStartedAt.AsTime().Add(-time.Nanosecond))
		}},
		{"zero-target", "phase must have at least one canary", "", true, func(e *pb.Experiment, _ *pb.Evidence) { e.Phases[0].TargetHosts = 0 }},
		{"over-target", "phase cannot exceed fleet size", "", true, func(e *pb.Experiment, _ *pb.Evidence) { e.Phases[0].TargetHosts = 17 }},
		{"sample-equality", "exact sample and exposure thresholds suffice", "beneficial", false, func(e *pb.Experiment, v *pb.Evidence) {
			for _, h := range v.Hosts {
				h.Sockets = e.Policy.MinimumSocketsPerHost
				h.ExposedSockets = h.Sockets
				h.BytesAcked = e.Policy.MinimumBytesPerHost
			}
		}},
		{"missing-host", "missing cohort host blocks evaluation", "insufficient_data", false, func(_ *pb.Experiment, v *pb.Evidence) { v.Hosts = v.Hosts[1:] }},
		{"missing-baseline", "missing baseline host blocks evaluation", "insufficient_data", false, func(e *pb.Experiment, _ *pb.Evidence) { e.Baseline.Hosts = e.Baseline.Hosts[1:] }},
		{"low-bytes", "too few bytes blocks evaluation", "insufficient_data", false, func(e *pb.Experiment, v *pb.Evidence) { v.Hosts[0].BytesAcked = e.Policy.MinimumBytesPerHost - 1 }},
		{"zero-goodput", "zero goodput cannot enter log comparison", "insufficient_data", false, func(_ *pb.Experiment, v *pb.Evidence) { v.Hosts[0].GoodputBps = 0 }},
		{"zero-baseline-rtt", "zero baseline RTT cannot enter ratio", "insufficient_data", false, func(e *pb.Experiment, _ *pb.Evidence) { e.Baseline.Hosts[0].RttP95Ms = 0 }},
		{"first-canary", "small cohorts permit safety review only", "safety_only", false, func(e *pb.Experiment, _ *pb.Evidence) { e.Phases[0].TargetHosts = 1 }},
		{"small-cohort-regression", "large goodput loss blocks small cohort promotion", "harmful", false, func(e *pb.Experiment, v *pb.Evidence) { e.Phases[0].TargetHosts = 1; v.Hosts[0].GoodputBps = 1 }},
		{"one-control", "one control cannot estimate variance", "insufficient_controls", false, func(e *pb.Experiment, _ *pb.Evidence) { e.Phases[0].TargetHosts = 15 }},
		{"full-fleet", "last phase checks safety without controls", "final_safety", false, func(e *pb.Experiment, _ *pb.Evidence) { e.Phases[0].TargetHosts = 16 }},
		{"full-fleet-harm", "last phase still rejects goodput regression", "harmful", false, func(e *pb.Experiment, v *pb.Evidence) {
			e.Phases[0].TargetHosts = 16
			for _, h := range v.Hosts {
				h.GoodputBps = 1
			}
		}},
		{"invalid-evidence", "malformed evidence fails before decision", "", true, func(_ *pb.Experiment, v *pb.Evidence) { v.Hosts[0] = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, v := fixtures.Experiment(now, treatment.DefaultPolicy())
			tc.change(e, v)
			before := proto.Clone(e)
			evidenceBefore := proto.Clone(v)
			got, err := Evaluate(e, v, now)
			if (err != nil) != tc.wantError || got.Outcome != tc.want {
				t.Fatalf("%s: got %+v err=%v; want %s error=%t", tc.description, got, err, tc.want, tc.wantError)
			}
			if !proto.Equal(e, before) || !proto.Equal(v, evidenceBefore) {
				t.Fatal("evaluation mutated its input")
			}
		})
	}
}

func TestSafetyGateBoundaries(t *testing.T) {
	now := time.Unix(1700000000, 0)
	for _, tc := range []struct {
		name, description string
		change            func(*pb.Experiment, *pb.Evidence)
		healthy, harmful  bool
	}{
		{"healthy", "complete unchanged observations renew leases", func(*pb.Experiment, *pb.Evidence) {}, true, false},
		{"missing-host", "partial evidence cannot renew leases", func(_ *pb.Experiment, v *pb.Evidence) { v.Hosts = v.Hosts[1:] }, false, false},
		{"unhealthy-collector", "collection failures cannot renew leases", func(_ *pb.Experiment, v *pb.Evidence) { v.Hosts[0].CollectionHealthy = false }, false, false},
		{"low-samples", "too few samples cannot renew leases", func(e *pb.Experiment, v *pb.Evidence) {
			v.Hosts[0].Sockets = e.Policy.MinimumSocketsPerHost - 1
			v.Hosts[0].ExposedSockets = 0
		}, false, false},
		{"stale", "old observations cannot renew leases", func(_ *pb.Experiment, v *pb.Evidence) { v.WindowEnd = timestamppb.New(now.Add(-3 * time.Minute)) }, false, false},
		{"loss-equality", "loss guard is strictly greater than its limit", func(e *pb.Experiment, v *pb.Evidence) {
			v.Hosts[0].RetransmissionRatio = e.Policy.MaximumRetransmissionIncrease
		}, true, false},
		{"loss-over", "excess loss is harmful", func(e *pb.Experiment, v *pb.Evidence) {
			v.Hosts[0].RetransmissionRatio = e.Policy.MaximumRetransmissionIncrease + 1e-9
		}, false, true},
		{"rtt-equality", "exactly representable RTT limit is inclusive", func(e *pb.Experiment, v *pb.Evidence) { e.Policy.MaximumRttIncrease = .5; v.Hosts[0].RttP95Ms = 1.5 }, true, false},
		{"control-harm", "controls are protected too", func(_ *pb.Experiment, v *pb.Evidence) { v.Hosts[15].RttP95Ms = 10 }, false, true},
		{"partial-harm", "missing data does not conceal observed harm", func(_ *pb.Experiment, v *pb.Evidence) { v.Hosts = v.Hosts[1:]; v.Hosts[0].RttP95Ms = 10 }, false, true},
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
		change            func(*pb.Experiment, *pb.Evidence)
		want              pb.ExperimentStatus
		phases            int
		wantError         bool
	}{
		{"initial", "approved proposal starts one canary", func(e *pb.Experiment, _ *pb.Evidence) {
			e.Phases = nil
			e.Status = pb.ExperimentStatus_EXPERIMENT_STATUS_PROPOSED
		}, pb.ExperimentStatus_EXPERIMENT_STATUS_RUNNING, 1, false},
		{"promotion", "benefit permits the next reviewed phase", func(*pb.Experiment, *pb.Evidence) {}, pb.ExperimentStatus_EXPERIMENT_STATUS_RUNNING, 2, false},
		{"completion", "full fleet safety review completes the experiment", func(e *pb.Experiment, _ *pb.Evidence) { e.Phases[0].TargetHosts = 16 }, pb.ExperimentStatus_EXPERIMENT_STATUS_COMPLETE, 1, false},
		{"rolled-back", "rolled back experiments cannot advance", func(e *pb.Experiment, _ *pb.Evidence) { e.Status = pb.ExperimentStatus_EXPERIMENT_STATUS_ROLLED_BACK }, 0, 0, true},
		{"harm", "harm cannot be promoted", func(_ *pb.Experiment, v *pb.Evidence) { v.Hosts[0].RttP95Ms = 100 }, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e, v := fixtures.Experiment(now, treatment.DefaultPolicy())
			tc.change(e, v)
			before := proto.Clone(e)
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
			if !proto.Equal(e, before) {
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
		want        string
		healthy     bool
	}{
		{"exactly ten percent increase is within the configured guard", 5.5, "beneficial", true},
		{"measurable increase above ten percent is harmful", 5.500001, "harmful", false},
	} {
		t.Run(tc.description, func(t *testing.T) {
			now := time.Unix(1700000000, 0)
			e, v := fixtures.Experiment(now, treatment.DefaultPolicy())
			for _, h := range e.Baseline.Hosts {
				h.RttP95Ms = 5
			}
			for _, h := range v.Hosts {
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
		for _, h := range v.Hosts {
			h.RttP95Ms = 5.5
		}
		got, err := Evaluate(e, v, now)
		if err != nil || got.Outcome != "beneficial" {
			t.Errorf("%d hosts at exactly the RTT guard: %v %v", n, got, err)
		}
	}
}
