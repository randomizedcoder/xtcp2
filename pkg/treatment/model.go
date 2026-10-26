package treatment

import (
	"net/netip"
	"slices"
	"time"
)

// The types in this file are the controller's domain model. The protobuf
// messages in gen/go/xtcp_treatment are a wire format for them (see package
// wire) and the PostgreSQL tables are a storage format for them (see
// controller/store/postgres); neither is the model. Optional fields are
// pointers so "unset" survives every conversion. A zero time.Time means a
// timestamp was not set.

// TCPSettings are the per-route socket parameters a treatment applies.
type TCPSettings struct {
	CongestionControl       *string
	CongestionLock          bool
	RTOMinMs                *uint32
	InitialCongestionWindow *uint32
	InitialReceiveWindow    *uint32
	QuickAck                *bool
	ECN                     *bool
}

// Empty reports whether no parameter is set.
func (s TCPSettings) Empty() bool {
	return s.CongestionControl == nil && s.RTOMinMs == nil && s.InitialCongestionWindow == nil && s.InitialReceiveWindow == nil && s.QuickAck == nil && s.ECN == nil
}

// Algorithm returns the congestion control name or "" when unset.
func (s TCPSettings) Algorithm() string {
	if s.CongestionControl == nil {
		return ""
	}
	return *s.CongestionControl
}

// Equal compares every parameter including whether it is set.
func (s TCPSettings) Equal(o TCPSettings) bool {
	return s.CongestionLock == o.CongestionLock && eqPtr(s.CongestionControl, o.CongestionControl) && eqPtr(s.RTOMinMs, o.RTOMinMs) && eqPtr(s.InitialCongestionWindow, o.InitialCongestionWindow) && eqPtr(s.InitialReceiveWindow, o.InitialReceiveWindow) && eqPtr(s.QuickAck, o.QuickAck) && eqPtr(s.ECN, o.ECN)
}

func eqPtr[T comparable](a, b *T) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// Treatment is a versioned set of TCP settings for one host destination.
type Treatment struct {
	ID       string
	Revision uint64
	// Destination is a /32 or /128 host prefix; the zero value is unset.
	Destination       netip.Prefix
	Settings          TCPSettings
	ECNFabricVerified bool
}

// Equal is a field-wise comparison.
func (t Treatment) Equal(o Treatment) bool {
	return t.ID == o.ID && t.Revision == o.Revision && t.Destination == o.Destination && t.ECNFabricVerified == o.ECNFabricVerified && t.Settings.Equal(o.Settings)
}

// Assignment binds a treatment to one agent on behalf of one experiment.
type Assignment struct {
	ExperimentID string
	Treatment    Treatment
	Mode         AssignmentMode
	// LeaseExpiresAt is zero for assignments without a lease.
	LeaseExpiresAt time.Time
	// Predecessor is the stable treatment this one replaced, restored on
	// rollback. Nil when the destination had none.
	Predecessor *Treatment
	Generation  uint64
}

// DesiredState is everything the controller wants one agent to hold.
type DesiredState struct {
	AgentID     string
	Revision    uint64
	Suspended   bool
	Assignments []Assignment
	IssuedAt    time.Time
}

// Find returns a pointer into d.Assignments for the experiment, or nil.
func (d *DesiredState) Find(experimentID string) *Assignment {
	for i := range d.Assignments {
		if d.Assignments[i].ExperimentID == experimentID {
			return &d.Assignments[i]
		}
	}
	return nil
}

// Capabilities are the facts an agent reports about its host.
type Capabilities struct {
	BootID               string
	KernelRelease        string
	CongestionAlgorithms []string
	SimpleHostRouting    bool
	ActuatorEnabled      bool
}

// Supports reports whether the host offers the congestion algorithm.
func (c Capabilities) Supports(algorithm string) bool {
	return slices.Contains(c.CongestionAlgorithms, algorithm)
}

// ApplicationRecord is an agent's report of what it did with one assignment.
type ApplicationRecord struct {
	Assignment      Assignment
	DesiredRevision uint64
	Status          ApplyStatus
	ReceivedAt      time.Time
	AttemptedAt     time.Time
	AppliedAt       time.Time
	VerifiedAt      time.Time
	RemovedAt       time.Time
	Error           string
	ExpiredLatched  bool
}

// AgentReport is one check-in: identity, capabilities and the application
// records for every assignment the agent knows about.
type AgentReport struct {
	AgentID          string
	Location         string
	Capabilities     Capabilities
	DesiredRevision  uint64
	Records          []ApplicationRecord
	LocallySuspended bool
}

// Record returns the record for the experiment, or nil.
func (r *AgentReport) Record(experimentID string) *ApplicationRecord {
	for i := range r.Records {
		if r.Records[i].Assignment.ExperimentID == experimentID {
			return &r.Records[i]
		}
	}
	return nil
}

// HostEvidence is one host's aggregate for one evidence window.
type HostEvidence struct {
	AgentID             string
	Sockets             uint64
	BytesAcked          uint64
	GoodputBps          float64
	RetransmissionRatio float64
	RttP95Ms            float64
	ExposedSockets      uint64
	CollectionHealthy   bool
}

// Evidence is an immutable per-host measurement set for a window.
type Evidence struct {
	ID                string
	AnalyzedAt        time.Time
	WindowStart       time.Time
	WindowEnd         time.Time
	QueryVersion      string
	EnrichmentVersion string
	Hosts             []HostEvidence
	ArtifactSHA256    string
}

// Clone copies the host slice so the caller can mutate independently.
func (e *Evidence) Clone() *Evidence {
	if e == nil {
		return nil
	}
	c := *e
	c.Hosts = slices.Clone(e.Hosts)
	return &c
}

// Equal compares every field and every host in order.
func (e *Evidence) Equal(o *Evidence) bool {
	if e == nil || o == nil {
		return e == o
	}
	return e.ID == o.ID && e.AnalyzedAt.Equal(o.AnalyzedAt) && e.WindowStart.Equal(o.WindowStart) && e.WindowEnd.Equal(o.WindowEnd) && e.QueryVersion == o.QueryVersion && e.EnrichmentVersion == o.EnrichmentVersion && e.ArtifactSHA256 == o.ArtifactSHA256 && slices.Equal(e.Hosts, o.Hosts)
}

// Policy bounds how an experiment is run and judged.
type Policy struct {
	MinimumObservation            time.Duration
	MaximumPhase                  time.Duration
	MaximumEvidenceAge            time.Duration
	MinimumSocketsPerHost         uint64
	MinimumBytesPerHost           uint64
	MinimumTreatedHosts           uint32
	MinimumImprovement            float64
	MaximumRetransmissionIncrease float64
	MaximumRTTIncrease            float64
}

// Phase is one step of exposure: the first TargetHosts of the cohort order.
type Phase struct {
	TargetHosts          uint32
	IssuedAt             time.Time
	ObservationStartedAt time.Time
	EvaluatedAt          time.Time
	Evidence             *Evidence
	Outcome              Outcome
	Reviewer             string
	Reason               string
}

// Experiment is a reviewed, phased rollout of one treatment in one location.
type Experiment struct {
	ID             string
	Location       string
	Hypothesis     string
	Treatment      Treatment
	Policy         Policy
	Baseline       *Evidence
	CohortOrder    []string
	Phases         []Phase
	Status         Status
	Revision       uint64
	ProposedAt     time.Time
	LatestEvidence *Evidence
	LatestOutcome  Outcome
	// UpdatedAt is when the controller last changed the experiment. The
	// discovery cooldown counts from it for terminal experiments.
	UpdatedAt time.Time
}

// Clone deep-copies the experiment so pure functions can return a new value.
func (e *Experiment) Clone() *Experiment {
	if e == nil {
		return nil
	}
	c := *e
	c.Baseline = e.Baseline.Clone()
	c.LatestEvidence = e.LatestEvidence.Clone()
	c.CohortOrder = slices.Clone(e.CohortOrder)
	c.Phases = slices.Clone(e.Phases)
	for i := range c.Phases {
		c.Phases[i].Evidence = e.Phases[i].Evidence.Clone()
	}
	return &c
}

// CurrentPhase returns a pointer to the last phase, or nil before review.
func (e *Experiment) CurrentPhase() *Phase {
	if len(e.Phases) == 0 {
		return nil
	}
	return &e.Phases[len(e.Phases)-1]
}

// Target is the number of treated hosts in the current phase, zero before review.
func (e *Experiment) Target() int {
	if p := e.CurrentPhase(); p != nil {
		return int(p.TargetHosts)
	}
	return 0
}
