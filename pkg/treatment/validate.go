// Package treatment implements the controller's route-treatment contracts.
// Validation is shared by RPC handlers, local journals, and direct callers.
package treatment

import (
	"errors"
	"fmt"
	"math"
	"net/netip"
	"regexp"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"google.golang.org/protobuf/types/known/durationpb"
)

const MaxAssignments = 4096
const MaxAgents = 10000
const MaxStateBytes = 16 << 20
const TrialLease = time.Hour
const LeaseRenewInterval = 5 * time.Minute

var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

func ValidID(s string) bool { return identifier.MatchString(s) }

func ValidateTreatment(t *pb.Treatment) error {
	if t == nil || !ValidID(t.Id) || t.Revision == 0 || t.Settings == nil {
		return errors.New("treatment requires id, revision and settings")
	}
	p, err := netip.ParsePrefix(t.Destination)
	if err != nil || p != p.Masked() || p.Bits() != p.Addr().BitLen() || p.Addr().Is4In6() || !p.Addr().IsGlobalUnicast() || p.Addr().IsLoopback() {
		return errors.New("destination must be a canonical unicast /32 or /128 host prefix")
	}
	return ValidateTCPSettings(t.Settings, t.EcnFabricVerified)
}

// ValidateTCPSettings also validates discovery templates before querying traffic.
func ValidateTCPSettings(s *pb.TCPSettings, ecnFabricVerified bool) error {
	if s == nil {
		return errors.New("TCP settings required")
	}
	if s.CongestionControl != nil && !identifier.MatchString(*s.CongestionControl) {
		return errors.New("invalid congestion algorithm")
	}
	if len(s.GetCongestionControl()) > 15 {
		return errors.New("congestion algorithm exceeds kernel name limit")
	}
	if s.CongestionLock && s.CongestionControl == nil {
		return errors.New("congestion_lock requires an algorithm")
	}
	if s.GetCongestionControl() == "dctcp" && (!ecnFabricVerified || !s.GetEcn()) {
		return errors.New("DCTCP requires independently verified ECN fabric and explicit ECN")
	}
	if s.RtoMinMs != nil && (*s.RtoMinMs < 1 || *s.RtoMinMs > 120000) {
		return errors.New("rto_min_ms outside 1..120000")
	}
	for _, w := range []*uint32{s.InitialCongestionWindow, s.InitialReceiveWindow} {
		if w != nil && (*w < 1 || *w > 1024) {
			return errors.New("initial window outside 1..1024 segments")
		}
	}
	if s.CongestionControl == nil && s.RtoMinMs == nil && s.InitialCongestionWindow == nil && s.InitialReceiveWindow == nil && s.QuickAck == nil && s.Ecn == nil {
		return errors.New("empty treatment")
	}
	return nil
}

func ValidateDesired(d *pb.DesiredState) error {
	if d == nil || !ValidID(d.AgentId) || d.Revision == 0 || len(d.Assignments) > MaxAssignments || d.IssuedAt == nil || d.IssuedAt.CheckValid() != nil {
		return errors.New("invalid desired-state header")
	}
	ids, destinations := map[string]bool{}, map[string]bool{}
	for _, a := range d.Assignments {
		if a == nil || !ValidID(a.Id) || !ValidID(a.ExperimentId) || a.Generation == 0 || ids[a.Id] {
			return errors.New("invalid or duplicate assignment identity")
		}
		ids[a.Id] = true
		if err := ValidateTreatment(a.Treatment); err != nil {
			return err
		}
		if a.Mode < pb.AssignmentMode_ASSIGNMENT_MODE_TRIAL || a.Mode > pb.AssignmentMode_ASSIGNMENT_MODE_RETIRED {
			return errors.New("invalid assignment mode")
		}
		if a.Mode != pb.AssignmentMode_ASSIGNMENT_MODE_RETIRED {
			if destinations[a.Treatment.Destination] {
				return errors.New("overlapping treatments")
			}
			destinations[a.Treatment.Destination] = true
		}
		if a.Mode == pb.AssignmentMode_ASSIGNMENT_MODE_TRIAL && (a.LeaseExpiresAt == nil || a.LeaseExpiresAt.CheckValid() != nil) {
			return errors.New("trial requires a valid expiry")
		}
		if a.Predecessor != nil {
			if err := ValidateTreatment(a.Predecessor); err != nil {
				return err
			}
			if a.Predecessor.Destination != a.Treatment.Destination {
				return errors.New("predecessor destination differs")
			}
		}
	}
	return nil
}

func DefaultPolicy() *pb.ExperimentPolicy {
	return &pb.ExperimentPolicy{MinimumObservation: durationpb.New(30 * time.Minute), MaximumPhase: durationpb.New(24 * time.Hour), MaximumEvidenceAge: durationpb.New(2 * time.Minute), MinimumSocketsPerHost: 30, MinimumBytesPerHost: 1 << 20, MinimumTreatedHosts: 4, MinimumImprovement: .05, MaximumRetransmissionIncrease: .005, MaximumRttIncrease: .1}
}

func ValidatePolicy(p *pb.ExperimentPolicy) error {
	if p == nil {
		return errors.New("policy required")
	}
	for _, d := range []*durationpb.Duration{p.MinimumObservation, p.MaximumPhase, p.MaximumEvidenceAge} {
		if d == nil || d.CheckValid() != nil || d.AsDuration() <= 0 || d.AsDuration() > 7*24*time.Hour {
			return errors.New("invalid policy duration")
		}
	}
	if p.MinimumObservation.AsDuration() > p.MaximumPhase.AsDuration() || p.MinimumSocketsPerHost == 0 || p.MinimumBytesPerHost == 0 || p.MinimumTreatedHosts < 2 || p.MinimumTreatedHosts > MaxAgents {
		return errors.New("invalid policy sample bounds")
	}
	for _, v := range []float64{p.MinimumImprovement, p.MaximumRetransmissionIncrease, p.MaximumRttIncrease} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
			return errors.New("invalid effect/guardrail bound")
		}
	}
	return nil
}

func ValidateEvidence(e *pb.Evidence) error {
	if e == nil || !ValidID(e.Id) || e.AnalyzedAt == nil || e.WindowStart == nil || e.WindowEnd == nil || e.AnalyzedAt.CheckValid() != nil || e.WindowStart.CheckValid() != nil || e.WindowEnd.CheckValid() != nil || !e.WindowEnd.AsTime().After(e.WindowStart.AsTime()) || e.AnalyzedAt.AsTime().Before(e.WindowEnd.AsTime()) {
		return errors.New("invalid evidence interval")
	}
	if e.QueryVersion == "" || len(e.QueryVersion) > 256 || len(e.EnrichmentVersion) > 256 || len(e.ArtifactSha256) != 64 {
		return errors.New("evidence provenance required")
	}
	for _, c := range e.ArtifactSha256 {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return errors.New("invalid evidence sha256")
		}
	}
	if len(e.Hosts) == 0 || len(e.Hosts) > MaxAgents {
		return errors.New("invalid evidence population")
	}
	seen := make(map[string]bool, len(e.Hosts))
	for _, h := range e.Hosts {
		if h == nil || !ValidID(h.AgentId) || seen[h.AgentId] || h.ExposedSockets > h.Sockets {
			return errors.New("invalid/duplicate evidence host")
		}
		seen[h.AgentId] = true
		for _, v := range []float64{h.GoodputBps, h.RetransmissionRatio, h.RttP95Ms} {
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
				return errors.New("invalid host measurement")
			}
		}
	}
	return nil
}

func ValidateProposal(e *pb.Experiment) error {
	if e == nil || !ValidID(e.Id) || !ValidID(e.Location) || len(e.Hypothesis) == 0 || len(e.Hypothesis) > 8192 || len(e.CohortOrder) < 3 || len(e.CohortOrder) > MaxAgents {
		return errors.New("invalid experiment identity or population (minimum three hosts)")
	}
	if err := ValidateTreatment(e.Treatment); err != nil {
		return err
	}
	if err := ValidatePolicy(e.Policy); err != nil {
		return err
	}
	if err := ValidateEvidence(e.Baseline); err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, id := range e.CohortOrder {
		if !ValidID(id) || seen[id] {
			return errors.New("invalid cohort")
		}
		seen[id] = true
	}
	for _, h := range e.Baseline.Hosts {
		delete(seen, h.AgentId)
	}
	if len(seen) > 0 {
		return fmt.Errorf("baseline missing %d cohort hosts", len(seen))
	}
	return nil
}
