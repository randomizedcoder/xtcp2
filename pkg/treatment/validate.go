// Package treatment is the route-treatment domain: the model, its
// enumerations, and the validation rules shared by the controller, the agent
// and the watchdog. It has no I/O and does not import the wire protocol.
package treatment

import (
	"errors"
	"fmt"
	"math"
	"net/netip"
	"regexp"
	"time"
)

const MaxAssignments = 4096
const MaxAgents = 10000
const MaxStateBytes = 16 << 20
const TrialLease = time.Hour
const LeaseRenewInterval = 5 * time.Minute

var identifier = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,127}$`)

func ValidID(s string) bool { return identifier.MatchString(s) }

// ValidDestination reports whether p is a canonical, globally routable /32
// or /128 host prefix.
func ValidDestination(p netip.Prefix) bool {
	return p.IsValid() && p == p.Masked() && p.Bits() == p.Addr().BitLen() && !p.Addr().Is4In6() && p.Addr().IsGlobalUnicast() && !p.Addr().IsLoopback()
}

func ValidateTreatment(t *Treatment) error {
	if t == nil || !ValidID(t.ID) || t.Revision == 0 {
		return errors.New("treatment requires id, revision and settings")
	}
	if !ValidDestination(t.Destination) {
		return errors.New("destination must be a canonical unicast /32 or /128 host prefix")
	}
	return ValidateTCPSettings(t.Settings, t.ECNFabricVerified)
}

// ValidateTCPSettings also validates discovery templates before querying traffic.
func ValidateTCPSettings(s TCPSettings, ecnFabricVerified bool) error {
	if s.CongestionControl != nil && !identifier.MatchString(*s.CongestionControl) {
		return errors.New("invalid congestion algorithm")
	}
	if len(s.Algorithm()) > 15 {
		return errors.New("congestion algorithm exceeds kernel name limit")
	}
	if s.CongestionLock && s.CongestionControl == nil {
		return errors.New("congestion_lock requires an algorithm")
	}
	if s.Algorithm() == "dctcp" && (!ecnFabricVerified || s.ECN == nil || !*s.ECN) {
		return errors.New("DCTCP requires independently verified ECN fabric and explicit ECN")
	}
	if s.RTOMinMs != nil && (*s.RTOMinMs < 1 || *s.RTOMinMs > 120000) {
		return errors.New("rto_min_ms outside 1..120000")
	}
	for _, w := range []*uint32{s.InitialCongestionWindow, s.InitialReceiveWindow} {
		if w != nil && (*w < 1 || *w > 1024) {
			return errors.New("initial window outside 1..1024 segments")
		}
	}
	if s.Empty() {
		return errors.New("empty treatment")
	}
	return nil
}

func ValidateDesired(d *DesiredState) error {
	if d == nil || !ValidID(d.AgentID) || d.Revision == 0 || len(d.Assignments) > MaxAssignments || d.IssuedAt.IsZero() {
		return errors.New("invalid desired-state header")
	}
	ids, destinations := map[string]bool{}, map[netip.Prefix]bool{}
	for i := range d.Assignments {
		a := &d.Assignments[i]
		if !ValidID(a.ExperimentID) || a.Generation == 0 || ids[a.ExperimentID] {
			return errors.New("invalid or duplicate assignment identity")
		}
		ids[a.ExperimentID] = true
		if err := ValidateTreatment(&a.Treatment); err != nil {
			return err
		}
		if !a.Mode.Valid() {
			return errors.New("invalid assignment mode")
		}
		if a.Mode != ModeRetired {
			if destinations[a.Treatment.Destination] {
				return errors.New("overlapping treatments")
			}
			destinations[a.Treatment.Destination] = true
		}
		if a.Mode == ModeTrial && a.LeaseExpiresAt.IsZero() {
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

func DefaultPolicy() Policy {
	return Policy{MinimumObservation: 30 * time.Minute, MaximumPhase: 24 * time.Hour, MaximumEvidenceAge: 2 * time.Minute, MinimumSocketsPerHost: 30, MinimumBytesPerHost: 1 << 20, MinimumTreatedHosts: 4, MinimumImprovement: .05, MaximumRetransmissionIncrease: .005, MaximumRTTIncrease: .1}
}

func ValidatePolicy(p Policy) error {
	for _, d := range []time.Duration{p.MinimumObservation, p.MaximumPhase, p.MaximumEvidenceAge} {
		if d <= 0 || d > 7*24*time.Hour {
			return errors.New("invalid policy duration")
		}
	}
	if p.MinimumObservation > p.MaximumPhase || p.MinimumSocketsPerHost == 0 || p.MinimumBytesPerHost == 0 || p.MinimumTreatedHosts < 2 || p.MinimumTreatedHosts > MaxAgents {
		return errors.New("invalid policy sample bounds")
	}
	for _, v := range []float64{p.MinimumImprovement, p.MaximumRetransmissionIncrease, p.MaximumRTTIncrease} {
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > 1 {
			return errors.New("invalid effect/guardrail bound")
		}
	}
	return nil
}

func ValidateEvidence(e *Evidence) error {
	if e == nil || !ValidID(e.ID) || e.AnalyzedAt.IsZero() || e.WindowStart.IsZero() || e.WindowEnd.IsZero() || !e.WindowEnd.After(e.WindowStart) || e.AnalyzedAt.Before(e.WindowEnd) {
		return errors.New("invalid evidence interval")
	}
	if e.QueryVersion == "" || len(e.QueryVersion) > 256 || len(e.EnrichmentVersion) > 256 || len(e.ArtifactSHA256) != 64 {
		return errors.New("evidence provenance required")
	}
	for _, c := range e.ArtifactSHA256 {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return errors.New("invalid evidence sha256")
		}
	}
	if len(e.Hosts) == 0 || len(e.Hosts) > MaxAgents {
		return errors.New("invalid evidence population")
	}
	seen := make(map[string]bool, len(e.Hosts))
	for i := range e.Hosts {
		h := &e.Hosts[i]
		if !ValidID(h.AgentID) || seen[h.AgentID] || h.ExposedSockets > h.Sockets {
			return errors.New("invalid/duplicate evidence host")
		}
		seen[h.AgentID] = true
		for _, v := range []float64{h.GoodputBps, h.RetransmissionRatio, h.RttP95Ms} {
			if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
				return errors.New("invalid host measurement")
			}
		}
	}
	return nil
}

func ValidateProposal(e *Experiment) error {
	if e == nil || !ValidID(e.ID) || !ValidID(e.Location) || len(e.Hypothesis) == 0 || len(e.Hypothesis) > 8192 || len(e.CohortOrder) < 3 || len(e.CohortOrder) > MaxAgents {
		return errors.New("invalid experiment identity or population (minimum three hosts)")
	}
	if err := ValidateTreatment(&e.Treatment); err != nil {
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
		delete(seen, h.AgentID)
	}
	if len(seen) > 0 {
		return fmt.Errorf("baseline missing %d cohort hosts", len(seen))
	}
	return nil
}

// ValidateReport checks a check-in before it is stored. Kernel bytes and boot
// IDs of installed routes are local to the host and must not be reported.
func ValidateReport(r *AgentReport) error {
	if r == nil || !ValidID(r.AgentID) || !ValidID(r.Location) || !ValidID(r.Capabilities.BootID) || len(r.Capabilities.KernelRelease) > 256 || len(r.Capabilities.CongestionAlgorithms) > 64 || len(r.Records) > MaxAssignments*2 {
		return errors.New("invalid agent report")
	}
	for _, a := range r.Capabilities.CongestionAlgorithms {
		if !ValidID(a) || len(a) > 15 {
			return errors.New("invalid algorithm capability")
		}
	}
	seen := map[string]bool{}
	for i := range r.Records {
		v := &r.Records[i]
		if !ValidID(v.Assignment.ExperimentID) || seen[v.Assignment.ExperimentID] || len(v.Error) > 4096 || !v.Status.Valid() {
			return errors.New("invalid application record")
		}
		seen[v.Assignment.ExperimentID] = true
		if err := ValidateTreatment(&v.Assignment.Treatment); err != nil {
			return err
		}
	}
	return nil
}
