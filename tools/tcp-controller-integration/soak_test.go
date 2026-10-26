package main

import (
	"strings"
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/wire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestSoakRollbackEvidence(t *testing.T) {
	now := time.Now()
	baseline := &pb.Evidence{Id: "baseline", AnalyzedAt: timestamppb.New(now.Add(-3 * time.Minute)), WindowStart: timestamppb.New(now.Add(-4 * time.Minute)), WindowEnd: timestamppb.New(now.Add(-3 * time.Minute)), QueryVersion: "test", ArtifactSha256: strings.Repeat("a", 64), Hosts: []*pb.HostEvidence{{AgentId: "canary", Sockets: 32, BytesAcked: 1 << 25, GoodputBps: 4e6, RttP95Ms: 5.305, CollectionHealthy: true}}}
	latest := proto.Clone(baseline).(*pb.Evidence)
	latest.Id = "latest"
	latest.AnalyzedAt = timestamppb.New(now)
	latest.WindowStart = timestamppb.New(now.Add(-time.Minute))
	latest.WindowEnd = timestamppb.New(now.Add(-20 * time.Second))
	latest.Hosts[0].ExposedSockets = 32
	policy := wire.Policy(treatment.DefaultPolicy())
	policy.MaximumRttIncrease = 1
	fixture := &pb.Experiment{Status: pb.ExperimentStatus_EXPERIMENT_STATUS_ROLLED_BACK, CohortOrder: []string{"canary"}, Policy: policy, Baseline: baseline, LatestEvidence: latest, Phases: []*pb.Phase{{Outcome: pb.Outcome_OUTCOME_HARMFUL}}}
	for _, tc := range []struct {
		name            string
		injected, valid bool
		change          func(*pb.Experiment)
	}{
		{"observed-early-loss", false, true, func(e *pb.Experiment) { e.LatestEvidence.Hosts[0].RetransmissionRatio = .010227199171467409 }},
		{"early-loss-is-not-latency-coverage", true, false, func(e *pb.Experiment) { e.LatestEvidence.Hosts[0].RetransmissionRatio = .010227199171467409 }},
		{"injected-latency", true, true, func(e *pb.Experiment) { e.LatestEvidence.Hosts[0].RttP95Ms = 180 }},
		{"healthy-evidence", false, false, func(e *pb.Experiment) {}},
		{"unexposed", false, false, func(e *pb.Experiment) {
			e.LatestEvidence.Hosts[0].RttP95Ms = 180
			e.LatestEvidence.Hosts[0].ExposedSockets = 0
		}},
		{"missing-evidence", false, false, func(e *pb.Experiment) { e.LatestEvidence = nil }},
		{"wrong-outcome", false, false, func(e *pb.Experiment) { e.Phases[0].Outcome = pb.Outcome_OUTCOME_EXPIRED }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := proto.Clone(fixture).(*pb.Experiment)
			tc.change(e)
			if err := validateSoakRollback(e, tc.injected); (err == nil) != tc.valid {
				t.Fatalf("valid=%t err=%v", tc.valid, err)
			}
			if tc.name == "injected-latency" {
				if !injectedLatencyRollback(e, now.Add(-time.Minute)) {
					t.Fatal("lost injected latency coverage")
				}
				if injectedLatencyRollback(e, now.Add(-5*time.Second)) {
					t.Fatal("pre-change evidence counted as injected latency")
				}
			}
		})
	}
}

func TestSoakConfigBounds(t *testing.T) {
	for _, c := range []soakConfig{{3, 180}, {2, 180}, {6, 300}} {
		if err := c.validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []soakConfig{{0, 180}, {1, 180}, {7, 180}, {3, 0}, {3, 179}, {3, 301}} {
		if c.validate() == nil {
			t.Fatalf("accepted unsafe/vacuous run: %+v", c)
		}
	}
	for _, destination := range []string{"198.18.0.1", "192.168.1.254", "198.18.0.107", "198.18.0.101\nOTHER=value"} {
		if validSoakDestination(destination) {
			t.Fatalf("accepted %q", destination)
		}
	}
}
