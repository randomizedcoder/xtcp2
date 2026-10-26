// Package wire converts between the treatment domain model and the
// xtcp_treatment.v1 protobuf messages. It is the only place that knows both
// shapes: the controller's gRPC handlers, the agent's journal and the CLIs
// call it at their edges and work on domain values everywhere else.
//
// Domain → wire functions never fail. Wire → domain functions are named
// Parse* and fail closed on unknown enum values, invalid timestamps and
// malformed durations so a newer or hostile peer cannot smuggle a value this
// build does not understand.
package wire

import (
	"errors"
	"fmt"
	"net/netip"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func stamp(t time.Time) *timestamppb.Timestamp {
	if t.IsZero() {
		return nil
	}
	return timestamppb.New(t)
}

func parseStamp(field string, t *timestamppb.Timestamp) (time.Time, error) {
	if t == nil {
		return time.Time{}, nil
	}
	if err := t.CheckValid(); err != nil {
		return time.Time{}, fmt.Errorf("%s: %w", field, err)
	}
	return t.AsTime(), nil
}

func parseDuration(field string, d *durationpb.Duration) (time.Duration, error) {
	if d == nil {
		return 0, nil
	}
	if err := d.CheckValid(); err != nil {
		return 0, fmt.Errorf("%s: %w", field, err)
	}
	return d.AsDuration(), nil
}

// Enumerations. The domain values mirror the wire values numerically; the
// range check is what makes the cast safe (TestEnumsMirrorWire pins it).

func Status(s treatment.Status) pb.ExperimentStatus { return pb.ExperimentStatus(s) }

func ParseStatus(w pb.ExperimentStatus) (treatment.Status, error) {
	n, _, _, _ := treatment.EnumCounts()
	if w < 0 || int(w) >= n {
		return 0, fmt.Errorf("unknown wire experiment status %d", int32(w))
	}
	return treatment.Status(w), nil
}

func Mode(m treatment.AssignmentMode) pb.AssignmentMode { return pb.AssignmentMode(m) }

func ParseMode(w pb.AssignmentMode) (treatment.AssignmentMode, error) {
	_, n, _, _ := treatment.EnumCounts()
	if w < 0 || int(w) >= n {
		return 0, fmt.Errorf("unknown wire assignment mode %d", int32(w))
	}
	return treatment.AssignmentMode(w), nil
}

func ApplyStatus(a treatment.ApplyStatus) pb.ApplyStatus { return pb.ApplyStatus(a) }

func ParseApplyStatus(w pb.ApplyStatus) (treatment.ApplyStatus, error) {
	_, _, n, _ := treatment.EnumCounts()
	if w < 0 || int(w) >= n {
		return 0, fmt.Errorf("unknown wire apply status %d", int32(w))
	}
	return treatment.ApplyStatus(w), nil
}

func Outcome(o treatment.Outcome) pb.Outcome { return pb.Outcome(o) }

func ParseOutcome(w pb.Outcome) (treatment.Outcome, error) {
	_, _, _, n := treatment.EnumCounts()
	if w < 0 || int(w) >= n {
		return 0, fmt.Errorf("unknown wire outcome %d", int32(w))
	}
	return treatment.Outcome(w), nil
}

// Settings and treatments.

func Settings(s treatment.TCPSettings) *pb.TCPSettings {
	return &pb.TCPSettings{CongestionControl: s.CongestionControl, CongestionLock: s.CongestionLock, RtoMinMs: s.RTOMinMs, InitialCongestionWindow: s.InitialCongestionWindow, InitialReceiveWindow: s.InitialReceiveWindow, QuickAck: s.QuickAck, Ecn: s.ECN}
}

// ParseSettings copies optional fields by value so later mutation of the
// message cannot alias the domain value.
func ParseSettings(p *pb.TCPSettings) treatment.TCPSettings {
	if p == nil {
		return treatment.TCPSettings{}
	}
	return treatment.TCPSettings{CongestionControl: clonePtr(p.CongestionControl), CongestionLock: p.CongestionLock, RTOMinMs: clonePtr(p.RtoMinMs), InitialCongestionWindow: clonePtr(p.InitialCongestionWindow), InitialReceiveWindow: clonePtr(p.InitialReceiveWindow), QuickAck: clonePtr(p.QuickAck), ECN: clonePtr(p.Ecn)}
}

func clonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func Treatment(t *treatment.Treatment) *pb.Treatment {
	if t == nil {
		return nil
	}
	destination := ""
	if t.Destination.IsValid() {
		destination = t.Destination.String()
	}
	return &pb.Treatment{Id: t.ID, Revision: t.Revision, Destination: destination, Settings: Settings(t.Settings), EcnFabricVerified: t.ECNFabricVerified}
}

// ParseTreatment leaves an unparsable destination as the zero prefix so
// treatment.ValidateTreatment reports it with the same message as any other
// bad destination.
func ParseTreatment(p *pb.Treatment) treatment.Treatment {
	if p == nil {
		return treatment.Treatment{}
	}
	t := treatment.Treatment{ID: p.Id, Revision: p.Revision, Settings: ParseSettings(p.Settings), ECNFabricVerified: p.EcnFabricVerified}
	if d, err := netip.ParsePrefix(p.Destination); err == nil {
		t.Destination = d
	}
	return t
}

func Assignment(a *treatment.Assignment) *pb.Assignment {
	if a == nil {
		return nil
	}
	return &pb.Assignment{ExperimentId: a.ExperimentID, Treatment: Treatment(&a.Treatment), Mode: Mode(a.Mode), LeaseExpiresAt: stamp(a.LeaseExpiresAt), Predecessor: Treatment(a.Predecessor), Generation: a.Generation}
}

func ParseAssignment(p *pb.Assignment) (treatment.Assignment, error) {
	if p == nil {
		return treatment.Assignment{}, errors.New("missing assignment")
	}
	mode, err := ParseMode(p.Mode)
	if err != nil {
		return treatment.Assignment{}, err
	}
	lease, err := parseStamp("lease_expires_at", p.LeaseExpiresAt)
	if err != nil {
		return treatment.Assignment{}, err
	}
	a := treatment.Assignment{ExperimentID: p.ExperimentId, Treatment: ParseTreatment(p.Treatment), Mode: mode, LeaseExpiresAt: lease, Generation: p.Generation}
	if p.Predecessor != nil {
		pred := ParseTreatment(p.Predecessor)
		a.Predecessor = &pred
	}
	return a, nil
}

func Desired(d *treatment.DesiredState) *pb.DesiredState {
	if d == nil {
		return nil
	}
	out := &pb.DesiredState{AgentId: d.AgentID, Revision: d.Revision, Suspended: d.Suspended, IssuedAt: stamp(d.IssuedAt)}
	for i := range d.Assignments {
		out.Assignments = append(out.Assignments, Assignment(&d.Assignments[i]))
	}
	return out
}

func ParseDesired(p *pb.DesiredState) (*treatment.DesiredState, error) {
	if p == nil {
		return nil, errors.New("missing desired state")
	}
	issued, err := parseStamp("issued_at", p.IssuedAt)
	if err != nil {
		return nil, err
	}
	d := &treatment.DesiredState{AgentID: p.AgentId, Revision: p.Revision, Suspended: p.Suspended, IssuedAt: issued}
	for _, a := range p.Assignments {
		v, err := ParseAssignment(a)
		if err != nil {
			return nil, err
		}
		d.Assignments = append(d.Assignments, v)
	}
	return d, nil
}

// Agent reports.

func Capabilities(c *treatment.Capabilities) *pb.Capabilities {
	return &pb.Capabilities{BootId: c.BootID, KernelRelease: c.KernelRelease, CongestionAlgorithms: append([]string(nil), c.CongestionAlgorithms...), SimpleHostRouting: c.SimpleHostRouting, ActuatorEnabled: c.ActuatorEnabled, CollectionId: c.CollectionID}
}

func ParseCapabilities(p *pb.Capabilities) treatment.Capabilities {
	if p == nil {
		return treatment.Capabilities{}
	}
	return treatment.Capabilities{BootID: p.BootId, KernelRelease: p.KernelRelease, CongestionAlgorithms: append([]string(nil), p.CongestionAlgorithms...), SimpleHostRouting: p.SimpleHostRouting, ActuatorEnabled: p.ActuatorEnabled, CollectionID: p.CollectionId}
}

func Record(r *treatment.ApplicationRecord) *pb.ApplicationRecord {
	return &pb.ApplicationRecord{Assignment: Assignment(&r.Assignment), DesiredRevision: r.DesiredRevision, Status: ApplyStatus(r.Status), ReceivedAt: stamp(r.ReceivedAt), AttemptedAt: stamp(r.AttemptedAt), AppliedAt: stamp(r.AppliedAt), VerifiedAt: stamp(r.VerifiedAt), RemovedAt: stamp(r.RemovedAt), Error: r.Error, ExpiredLatched: r.ExpiredLatched}
}

// ParseRecord rejects records carrying kernel route bytes or an installed
// boot ID: those are local to the host and never cross the wire.
func ParseRecord(p *pb.ApplicationRecord) (treatment.ApplicationRecord, error) {
	if p == nil {
		return treatment.ApplicationRecord{}, errors.New("missing application record")
	}
	if len(p.InstalledRoute) > 0 || p.InstalledBootId != "" {
		return treatment.ApplicationRecord{}, errors.New("application record carries local kernel state")
	}
	a, err := ParseAssignment(p.Assignment)
	if err != nil {
		return treatment.ApplicationRecord{}, err
	}
	st, err := ParseApplyStatus(p.Status)
	if err != nil {
		return treatment.ApplicationRecord{}, err
	}
	r := treatment.ApplicationRecord{Assignment: a, DesiredRevision: p.DesiredRevision, Status: st, Error: p.Error, ExpiredLatched: p.ExpiredLatched}
	for _, f := range []struct {
		name string
		src  *timestamppb.Timestamp
		dst  *time.Time
	}{{"received_at", p.ReceivedAt, &r.ReceivedAt}, {"attempted_at", p.AttemptedAt, &r.AttemptedAt}, {"applied_at", p.AppliedAt, &r.AppliedAt}, {"verified_at", p.VerifiedAt, &r.VerifiedAt}, {"removed_at", p.RemovedAt, &r.RemovedAt}} {
		if *f.dst, err = parseStamp(f.name, f.src); err != nil {
			return treatment.ApplicationRecord{}, err
		}
	}
	return r, nil
}

func Report(r *treatment.AgentReport) *pb.CheckInRequest {
	if r == nil {
		return nil
	}
	out := &pb.CheckInRequest{AgentId: r.AgentID, Location: r.Location, Capabilities: Capabilities(&r.Capabilities), DesiredRevision: r.DesiredRevision, LocallySuspended: r.LocallySuspended}
	for i := range r.Records {
		out.Records = append(out.Records, Record(&r.Records[i]))
	}
	return out
}

func ParseReport(p *pb.CheckInRequest) (*treatment.AgentReport, error) {
	if p == nil {
		return nil, errors.New("missing check-in")
	}
	r := &treatment.AgentReport{AgentID: p.AgentId, Location: p.Location, Capabilities: ParseCapabilities(p.Capabilities), DesiredRevision: p.DesiredRevision, LocallySuspended: p.LocallySuspended}
	for _, v := range p.Records {
		rec, err := ParseRecord(v)
		if err != nil {
			return nil, err
		}
		r.Records = append(r.Records, rec)
	}
	return r, nil
}

// Evidence.

func Host(h *treatment.HostEvidence) *pb.HostEvidence {
	return &pb.HostEvidence{AgentId: h.AgentID, Sockets: h.Sockets, BytesAcked: h.BytesAcked, GoodputBps: h.GoodputBps, RetransmissionRatio: h.RetransmissionRatio, RttP95Ms: h.RttP95Ms, ExposedSockets: h.ExposedSockets, CollectionHealthy: h.CollectionHealthy, UnboundSockets: h.UnboundSockets}
}

func ParseHost(p *pb.HostEvidence) treatment.HostEvidence {
	if p == nil {
		return treatment.HostEvidence{}
	}
	return treatment.HostEvidence{AgentID: p.AgentId, Sockets: p.Sockets, BytesAcked: p.BytesAcked, GoodputBps: p.GoodputBps, RetransmissionRatio: p.RetransmissionRatio, RttP95Ms: p.RttP95Ms, ExposedSockets: p.ExposedSockets, CollectionHealthy: p.CollectionHealthy, UnboundSockets: p.UnboundSockets}
}

func Evidence(e *treatment.Evidence) *pb.Evidence {
	if e == nil {
		return nil
	}
	out := &pb.Evidence{Id: e.ID, AnalyzedAt: stamp(e.AnalyzedAt), WindowStart: stamp(e.WindowStart), WindowEnd: stamp(e.WindowEnd), QueryVersion: e.QueryVersion, EnrichmentVersion: e.EnrichmentVersion, ArtifactSha256: e.ArtifactSHA256}
	for i := range e.Hosts {
		out.Hosts = append(out.Hosts, Host(&e.Hosts[i]))
	}
	return out
}

// ParseEvidence maps a nil message to nil evidence; validation decides
// whether evidence was required.
func ParseEvidence(p *pb.Evidence) (*treatment.Evidence, error) {
	if p == nil {
		return nil, nil
	}
	e := &treatment.Evidence{ID: p.Id, QueryVersion: p.QueryVersion, EnrichmentVersion: p.EnrichmentVersion, ArtifactSHA256: p.ArtifactSha256}
	var err error
	for _, f := range []struct {
		name string
		src  *timestamppb.Timestamp
		dst  *time.Time
	}{{"analyzed_at", p.AnalyzedAt, &e.AnalyzedAt}, {"window_start", p.WindowStart, &e.WindowStart}, {"window_end", p.WindowEnd, &e.WindowEnd}} {
		if *f.dst, err = parseStamp(f.name, f.src); err != nil {
			return nil, err
		}
	}
	for _, h := range p.Hosts {
		e.Hosts = append(e.Hosts, ParseHost(h))
	}
	return e, nil
}

// Experiments.

func Policy(p treatment.Policy) *pb.ExperimentPolicy {
	return &pb.ExperimentPolicy{MinimumObservation: durationpb.New(p.MinimumObservation), MaximumPhase: durationpb.New(p.MaximumPhase), MaximumEvidenceAge: durationpb.New(p.MaximumEvidenceAge), MinimumSocketsPerHost: p.MinimumSocketsPerHost, MinimumBytesPerHost: p.MinimumBytesPerHost, MinimumTreatedHosts: p.MinimumTreatedHosts, MinimumImprovement: p.MinimumImprovement, MaximumRetransmissionIncrease: p.MaximumRetransmissionIncrease, MaximumRttIncrease: p.MaximumRTTIncrease}
}

func ParsePolicy(p *pb.ExperimentPolicy) (treatment.Policy, error) {
	if p == nil {
		return treatment.Policy{}, errors.New("policy required")
	}
	out := treatment.Policy{MinimumSocketsPerHost: p.MinimumSocketsPerHost, MinimumBytesPerHost: p.MinimumBytesPerHost, MinimumTreatedHosts: p.MinimumTreatedHosts, MinimumImprovement: p.MinimumImprovement, MaximumRetransmissionIncrease: p.MaximumRetransmissionIncrease, MaximumRTTIncrease: p.MaximumRttIncrease}
	var err error
	for _, f := range []struct {
		name string
		src  *durationpb.Duration
		dst  *time.Duration
	}{{"minimum_observation", p.MinimumObservation, &out.MinimumObservation}, {"maximum_phase", p.MaximumPhase, &out.MaximumPhase}, {"maximum_evidence_age", p.MaximumEvidenceAge, &out.MaximumEvidenceAge}} {
		if *f.dst, err = parseDuration(f.name, f.src); err != nil {
			return treatment.Policy{}, err
		}
	}
	return out, nil
}

func Phase(p *treatment.Phase) *pb.Phase {
	return &pb.Phase{TargetHosts: p.TargetHosts, IssuedAt: stamp(p.IssuedAt), ObservationStartedAt: stamp(p.ObservationStartedAt), EvaluatedAt: stamp(p.EvaluatedAt), Evidence: Evidence(p.Evidence), Outcome: Outcome(p.Outcome), Reviewer: p.Reviewer, Reason: p.Reason}
}

func ParsePhase(p *pb.Phase) (treatment.Phase, error) {
	if p == nil {
		return treatment.Phase{}, errors.New("missing phase")
	}
	outcome, err := ParseOutcome(p.Outcome)
	if err != nil {
		return treatment.Phase{}, err
	}
	ev, err := ParseEvidence(p.Evidence)
	if err != nil {
		return treatment.Phase{}, err
	}
	out := treatment.Phase{TargetHosts: p.TargetHosts, Evidence: ev, Outcome: outcome, Reviewer: p.Reviewer, Reason: p.Reason}
	for _, f := range []struct {
		name string
		src  *timestamppb.Timestamp
		dst  *time.Time
	}{{"issued_at", p.IssuedAt, &out.IssuedAt}, {"observation_started_at", p.ObservationStartedAt, &out.ObservationStartedAt}, {"evaluated_at", p.EvaluatedAt, &out.EvaluatedAt}} {
		if *f.dst, err = parseStamp(f.name, f.src); err != nil {
			return treatment.Phase{}, err
		}
	}
	return out, nil
}

func Experiment(e *treatment.Experiment) *pb.Experiment {
	if e == nil {
		return nil
	}
	out := &pb.Experiment{Id: e.ID, Location: e.Location, Hypothesis: e.Hypothesis, Treatment: Treatment(&e.Treatment), Policy: Policy(e.Policy), Baseline: Evidence(e.Baseline), CohortOrder: append([]string(nil), e.CohortOrder...), Status: Status(e.Status), Revision: e.Revision, ProposedAt: stamp(e.ProposedAt), LatestEvidence: Evidence(e.LatestEvidence), LatestOutcome: Outcome(e.LatestOutcome), UpdatedAt: stamp(e.UpdatedAt)}
	for i := range e.Phases {
		out.Phases = append(out.Phases, Phase(&e.Phases[i]))
	}
	return out
}

// ParseExperiment tolerates a missing policy (the zero policy fails
// validation) so a proposal with no policy is reported by the validator
// rather than as a decoding error.
func ParseExperiment(p *pb.Experiment) (*treatment.Experiment, error) {
	if p == nil {
		return nil, errors.New("missing experiment")
	}
	status, err := ParseStatus(p.Status)
	if err != nil {
		return nil, err
	}
	outcome, err := ParseOutcome(p.LatestOutcome)
	if err != nil {
		return nil, err
	}
	e := &treatment.Experiment{ID: p.Id, Location: p.Location, Hypothesis: p.Hypothesis, Treatment: ParseTreatment(p.Treatment), CohortOrder: append([]string(nil), p.CohortOrder...), Status: status, Revision: p.Revision, LatestOutcome: outcome}
	if p.Policy != nil {
		if e.Policy, err = ParsePolicy(p.Policy); err != nil {
			return nil, err
		}
	}
	if e.Baseline, err = ParseEvidence(p.Baseline); err != nil {
		return nil, err
	}
	if e.LatestEvidence, err = ParseEvidence(p.LatestEvidence); err != nil {
		return nil, err
	}
	if e.ProposedAt, err = parseStamp("proposed_at", p.ProposedAt); err != nil {
		return nil, err
	}
	if e.UpdatedAt, err = parseStamp("updated_at", p.UpdatedAt); err != nil {
		return nil, err
	}
	for _, ph := range p.Phases {
		v, err := ParsePhase(ph)
		if err != nil {
			return nil, err
		}
		e.Phases = append(e.Phases, v)
	}
	return e, nil
}

// Validators over wire messages, for callers that keep messages at rest (the
// agent's prototext journal). Each converts and then applies the domain rule
// so there is exactly one set of bounds.

func ValidateTreatment(p *pb.Treatment) error {
	if p == nil || p.Settings == nil {
		return errors.New("treatment requires id, revision and settings")
	}
	t := ParseTreatment(p)
	return treatment.ValidateTreatment(&t)
}

func ValidateDesired(p *pb.DesiredState) error {
	if p == nil {
		return errors.New("invalid desired-state header")
	}
	for _, a := range p.Assignments {
		if a == nil || a.Treatment == nil || a.Treatment.Settings == nil {
			return errors.New("treatment requires id, revision and settings")
		}
	}
	d, err := ParseDesired(p)
	if err != nil {
		return err
	}
	return treatment.ValidateDesired(d)
}

func ValidateEvidence(p *pb.Evidence) error {
	e, err := ParseEvidence(p)
	if err != nil {
		return err
	}
	return treatment.ValidateEvidence(e)
}

func ValidatePolicy(p *pb.ExperimentPolicy) error {
	v, err := ParsePolicy(p)
	if err != nil {
		return err
	}
	return treatment.ValidatePolicy(v)
}
