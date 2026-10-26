package experiment

import (
	"math"
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/internal/fixtures"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestEvaluationBenefitHarmAndEvidenceGates(t *testing.T) {
	now := time.Now()
	e, v := fixtures.Experiment(now, treatment.DefaultPolicy())
	for _, tc := range []struct {
		name   string
		want   Outcome
		change func(*pb.Experiment, *pb.Evidence)
	}{
		{"benefit", OutcomeBeneficial, func(*pb.Experiment, *pb.Evidence) {}},
		{"loss", OutcomeHarmful, func(_ *pb.Experiment, v *pb.Evidence) { v.Hosts[0].RetransmissionRatio = .2 }},
		{"control harm", OutcomeHarmful, func(_ *pb.Experiment, v *pb.Evidence) {
			for _, h := range v.Hosts[4:] {
				h.RttP95Ms = 3
			}
		}},
		{"missing exposure", OutcomeAwaitingExposure, func(_ *pb.Experiment, v *pb.Evidence) { v.Hosts[0].ExposedSockets = 0 }},
		{"stale", OutcomeStale, func(_ *pb.Experiment, v *pb.Evidence) { v.WindowEnd = timestamppb.New(now.Add(-3 * time.Minute)) }},
		{"missing data", OutcomeInsufficientData, func(_ *pb.Experiment, v *pb.Evidence) { v.Hosts[0].CollectionHealthy = false }},
		{"too early", OutcomeInsufficientDuration, func(_ *pb.Experiment, v *pb.Evidence) { v.WindowStart = timestamppb.New(now.Add(-5 * time.Minute)) }},
		{"no benefit", OutcomeInconclusive, func(_ *pb.Experiment, v *pb.Evidence) {
			for _, h := range v.Hosts {
				h.GoodputBps = 1e8
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := proto.Clone(e).(*pb.Experiment)
			w := proto.Clone(v).(*pb.Evidence)
			tc.change(n, w)
			got, err := Evaluate(n, w, now)
			if err != nil || got.Outcome != tc.want {
				t.Fatalf("got %+v %v want %s", got, err, tc.want)
			}
		})
	}
}

func TestHealthyEvidenceSoakRTTRegression(t *testing.T) {
	// Reproduce the low-netem failure: 5.305 -> 5.922ms crosses production's
	// 10% guard. The fixture's explicit tolerance must still reject 180ms.
	for _, tc := range []struct {
		name                string
		limit, rtt, retrans float64
		wantHarm            bool
	}{
		{"production-rejects-observed-increase", .1, 5.922, .00005452860025083156, true},
		{"soak-allows-observed-increase", 1, 5.922, .00005452860025083156, false},
		{"soak-rejects-netem-regression", 1, 180, 0, true},
		{"soak-still-rejects-loss", 1, 5.305, .006, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Now()
			e, v := fixtures.Experiment(now, treatment.DefaultPolicy())
			e.Policy.MaximumRttIncrease = tc.limit
			for _, h := range e.Baseline.Hosts {
				h.RttP95Ms = 5.305
			}
			for _, h := range v.Hosts {
				h.RttP95Ms = 5.305
			}
			v.Hosts[0].RttP95Ms, v.Hosts[0].RetransmissionRatio = tc.rtt, tc.retrans
			healthy, harmful := HealthyEvidence(e, v, now)
			if harmful != tc.wantHarm || healthy == harmful {
				t.Fatalf("healthy=%t harmful=%t", healthy, harmful)
			}
		})
	}
}
func TestAdvancePreservesInputAndRequiresReviewableEvidence(t *testing.T) {
	now := time.Now()
	e, v := fixtures.Experiment(now, treatment.DefaultPolicy())
	next, err := Advance(e, v, now, "operator", "reviewed")
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Phases) != 1 || len(next.Phases) != 2 || next.Phases[1].TargetHosts != 8 {
		t.Fatal("incorrect phase transition")
	}
	if _, err = Advance(next, v, now, "operator", "reuse evidence"); err == nil {
		t.Fatal("reused previous phase evidence")
	}
}
func TestStudentQuantile(t *testing.T) {
	for _, tc := range []struct{ p, df, want float64 }{{.975, 1, 12.706204736}, {.975, 10, 2.228138852}, {.95, 30, 1.697260887}} {
		if got := studentQuantile(tc.p, tc.df); math.Abs(got-tc.want) > 1e-5 {
			t.Fatalf("got %g want %g", got, tc.want)
		}
	}
}
func TestTenThousandAgentCohorts(t *testing.T) {
	n := 0
	steps := 0
	for n < 10000 {
		next := NextCohort(n, 10000)
		if next <= n || next > 10000 {
			t.Fatal("invalid growth")
		}
		n = next
		steps++
	}
	if steps > 16 {
		t.Fatal("unexpected phases", steps)
	}
}
