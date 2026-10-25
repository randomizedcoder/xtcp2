// Package fixtures holds protobuf test fixtures shared by the treatment
// packages' tests. It imports only generated types so that the contract
// package's own tests can use it without an import cycle.
package fixtures

import (
	"fmt"
	"strings"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_controller"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// Desired is a minimal valid desired state for agent host-1: one trial
// assignment of CUBIC to a single /32 with a one-hour lease.
func Desired(now time.Time) *pb.DesiredState {
	return &pb.DesiredState{
		AgentId:  "host-1",
		Revision: 1,
		IssuedAt: timestamppb.New(now),
		Assignments: []*pb.Assignment{{
			Id:             "trial-1",
			ExperimentId:   "trial-1",
			Generation:     1,
			Mode:           pb.AssignmentMode_ASSIGNMENT_MODE_TRIAL,
			LeaseExpiresAt: timestamppb.New(now.Add(time.Hour)),
			Treatment: &pb.Treatment{
				Id:          "cubic",
				Revision:    1,
				Destination: "192.0.2.1/32",
				Settings:    &pb.TCPSettings{CongestionControl: proto.String("cubic")},
			},
		}},
	}
}

// Experiment is a running 16-host experiment at its first 4-host phase with
// baseline and a current evidence window in which the treated hosts gained 30%.
func Experiment(now time.Time, policy *pb.ExperimentPolicy) (*pb.Experiment, *pb.Evidence) {
	b := &pb.Evidence{Id: "baseline", AnalyzedAt: timestamppb.New(now.Add(-time.Hour)), WindowStart: timestamppb.New(now.Add(-2 * time.Hour)), WindowEnd: timestamppb.New(now.Add(-time.Hour)), QueryVersion: "v1", ArtifactSha256: strings.Repeat("a", 64)}
	e := &pb.Experiment{Id: "exp", Location: "dc1", Hypothesis: "cubic improves throughput", Treatment: Desired(now).Assignments[0].Treatment, Policy: policy, Baseline: b, Status: pb.ExperimentStatus_EXPERIMENT_STATUS_RUNNING, Revision: 2, Phases: []*pb.Phase{{TargetHosts: 4, IssuedAt: timestamppb.New(now.Add(-40 * time.Minute)), ObservationStartedAt: timestamppb.New(now.Add(-35 * time.Minute))}}}
	for i := range 16 {
		id := fmt.Sprintf("host-%d", i)
		e.CohortOrder = append(e.CohortOrder, id)
		b.Hosts = append(b.Hosts, &pb.HostEvidence{AgentId: id, Sockets: 100, ExposedSockets: 100, BytesAcked: 1 << 25, GoodputBps: 1e8, RttP95Ms: 1, CollectionHealthy: true})
	}
	ev := proto.Clone(b).(*pb.Evidence)
	ev.Id = "after"
	ev.WindowStart = timestamppb.New(now.Add(-34 * time.Minute))
	ev.WindowEnd = timestamppb.New(now.Add(-time.Second))
	ev.AnalyzedAt = timestamppb.New(now)
	for _, h := range ev.Hosts[:4] {
		h.GoodputBps *= 1.3
	}
	return e, ev
}
