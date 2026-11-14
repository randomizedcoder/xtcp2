// Package fixtures holds test fixtures shared by the treatment packages'
// tests, in domain form and, through package wire, in protobuf form for the
// code that still keeps messages at rest (the agent's journal).
package fixtures

import (
	"fmt"
	"net/netip"
	"strings"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/wire"
)

func ptr[T any](v T) *T { return &v }

// CollectionID is a well-formed collection nonce for reports and sessions.
const CollectionID = "0123456789abcdef0123456789abcdef"

// Treatment is CUBIC on a single /32.
func Treatment() treatment.Treatment {
	return treatment.Treatment{ID: "cubic", Revision: 1, Destination: netip.MustParsePrefix("192.0.2.1/32"), Settings: treatment.TCPSettings{CongestionControl: ptr("cubic")}}
}

// Desired is a minimal valid desired state for agent host-1: one trial
// assignment of CUBIC to a single /32 with a one-hour lease.
func Desired(now time.Time) *treatment.DesiredState {
	return &treatment.DesiredState{
		AgentID:  "host-1",
		Revision: 1,
		IssuedAt: now,
		Assignments: []treatment.Assignment{{
			ExperimentID:   "trial-1",
			Generation:     1,
			Mode:           treatment.ModeTrial,
			LeaseExpiresAt: now.Add(time.Hour),
			Treatment:      Treatment(),
		}},
	}
}

// DesiredWire is Desired in protobuf form.
func DesiredWire(now time.Time) *pb.DesiredState { return wire.Desired(Desired(now)) }

// Experiment is a running 16-host experiment at its first 4-host phase with
// baseline and a current evidence window in which the treated hosts gained 30%.
func Experiment(now time.Time, policy treatment.Policy) (*treatment.Experiment, *treatment.Evidence) {
	b := &treatment.Evidence{ID: "baseline", AnalyzedAt: now.Add(-time.Hour), WindowStart: now.Add(-2 * time.Hour), WindowEnd: now.Add(-time.Hour), QueryVersion: "v1", ArtifactSHA256: strings.Repeat("a", 64)}
	e := &treatment.Experiment{ID: "exp", Location: "dc1", Hypothesis: "cubic improves throughput", Treatment: Treatment(), Policy: policy, Baseline: b, Status: treatment.StatusRunning, Revision: 2, Phases: []treatment.Phase{{TargetHosts: 4, IssuedAt: now.Add(-40 * time.Minute), ObservationStartedAt: now.Add(-35 * time.Minute)}}}
	for i := range 16 {
		id := fmt.Sprintf("host-%d", i)
		e.CohortOrder = append(e.CohortOrder, id)
		b.Hosts = append(b.Hosts, treatment.HostEvidence{AgentID: id, Sockets: 100, ExposedSockets: 100, BytesAcked: 1 << 25, GoodputBps: 1e8, RttP95Ms: 1, CollectionHealthy: true})
	}
	ev := b.Clone()
	ev.ID = "after"
	ev.WindowStart = now.Add(-34 * time.Minute)
	ev.WindowEnd = now.Add(-time.Second)
	ev.AnalyzedAt = now
	for i := range ev.Hosts[:4] {
		ev.Hosts[i].GoodputBps *= 1.3
	}
	return e, ev
}

// ExperimentWire is Experiment in protobuf form.
func ExperimentWire(now time.Time, policy treatment.Policy) (*pb.Experiment, *pb.Evidence) {
	e, v := Experiment(now, policy)
	return wire.Experiment(e), wire.Evidence(v)
}
