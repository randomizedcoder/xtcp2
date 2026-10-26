// Package controller implements the treatment control plane's gRPC services
// and its background loops over the ports in package store. Handlers decode
// wire messages into domain values at the boundary (package wire), apply the
// pure rules in package experiment, and persist through a store transaction
// that serializes every control-plane mutation.
package controller

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/experiment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/identity"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/wire"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// agentWindow is how recently an agent must have checked in to count as
// present for eligibility, observation and lease renewal.
const agentWindow = 2 * time.Minute

type Server struct {
	pb.UnimplementedAgentControlServiceServer
	pb.UnimplementedOperatorServiceServer
	Store          store.Store
	Hub            *NotificationHub
	EvidenceSource EvidenceSource
	Now            func() time.Time
	// Zero preserves the production one-hour candidate discovery window.
	DiscoveryWindow time.Duration
}

var errExperimentBudget = errors.New("data-center experiment budget exhausted (one active experiment)")
var errDiscoverySuppressed = errors.New("destination already treated, held, proposed, or in cooldown")

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// failure maps store and domain errors onto gRPC codes. Unknown errors are
// precondition failures: the request was well formed but the state refused it.
func failure(e error) error {
	if e == nil {
		return nil
	}
	if status.Code(e) != codes.Unknown {
		return e
	}
	if errors.Is(e, store.ErrNotFound) || errors.Is(e, store.ErrNotEnrolled) {
		return status.Error(codes.NotFound, "not found")
	}
	if errors.Is(e, context.Canceled) {
		return status.Error(codes.Canceled, "cancelled")
	}
	return status.Error(codes.FailedPrecondition, e.Error())
}

func invalid(e error) error { return status.Error(codes.InvalidArgument, e.Error()) }

func validRequest(id, reason string) error {
	if !treatment.ValidID(id) || strings.HasSuffix(id, ".result") || reason == "" || len(reason) > 4096 {
		return status.Error(codes.InvalidArgument, "request ID and bounded reason required")
	}
	return nil
}

func jsonMessage(m proto.Message) ([]byte, error) { return protojson.Marshal(m) }

// audit appends one event whose payload is the protojson of a wire message.
func audit(ctx context.Context, rp store.Repos, actor, requestID, kind string, m proto.Message) error {
	b, e := jsonMessage(m)
	if e != nil {
		return e
	}
	return rp.Audit.Append(ctx, actor, requestID, kind, b)
}

// replay answers a repeated request ID with the original response. The
// repeat must be the same operation with the same body; anything else is a
// conflict rather than a second mutation.
func replay(ctx context.Context, rp store.Repos, id, kind string, request, response proto.Message) (bool, error) {
	ev, e := rp.Audit.Lookup(ctx, id)
	if errors.Is(e, store.ErrNotFound) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	if ev.Kind != kind {
		return false, fmt.Errorf("%w for another operation", store.ErrRequestConflict)
	}
	want, e := jsonMessage(request)
	if e != nil {
		return false, e
	}
	stored := request.ProtoReflect().New().Interface()
	if e = protojson.Unmarshal(ev.Payload, stored); e != nil {
		return false, e
	}
	got, e := jsonMessage(stored)
	if e != nil {
		return false, e
	}
	if string(got) != string(want) {
		return false, fmt.Errorf("%w with different body", store.ErrRequestConflict)
	}
	result, e := rp.Audit.Lookup(ctx, id+".result")
	if e != nil {
		return false, fmt.Errorf("missing idempotent response: %w", e)
	}
	return true, protojson.Unmarshal(result.Payload, response)
}

func holdScopes(location, agentID string) []string {
	return []string{"fleet", "location:" + location, "agent:" + agentID}
}

func (s *Server) CheckIn(ctx context.Context, r *pb.CheckInRequest) (*pb.CheckInResponse, error) {
	id, e := identity.Agent(ctx, r.GetAgentId())
	if e != nil {
		return nil, e
	}
	report, e := wire.ParseReport(r)
	if e != nil {
		return nil, invalid(e)
	}
	if id.Location != report.Location || r.Capabilities == nil {
		return nil, status.Error(codes.InvalidArgument, "invalid agent report")
	}
	if e = treatment.ValidateReport(report); e != nil {
		return nil, invalid(e)
	}
	now := s.now()
	repos := s.Store.Repos()
	// New enrollment and hold inheritance are serialized against operator
	// controls. The overwhelmingly common heartbeat is one independent update.
	if _, e = repos.Agents.Get(ctx, report.AgentID); errors.Is(e, store.ErrNotFound) {
		e = s.Store.Tx(ctx, func(rp store.Repos) error {
			held, err := rp.Holds.Held(ctx, holdScopes(report.Location, report.AgentID)...)
			if err != nil {
				return err
			}
			return rp.Agents.Enroll(ctx, report.AgentID, report.Location, &treatment.DesiredState{AgentID: report.AgentID, Revision: 1, Suspended: held, IssuedAt: now})
		})
	}
	if e != nil {
		return nil, failure(e)
	}
	if e = repos.Agents.Observe(ctx, report, now); errors.Is(e, store.ErrNotEnrolled) {
		return nil, status.Error(codes.PermissionDenied, "agent location cannot change during enrollment")
	} else if e != nil {
		return nil, failure(e)
	}
	d, e := repos.Agents.Desired(ctx, report.AgentID)
	if e != nil {
		return nil, failure(e)
	}
	if d.Revision == report.DesiredRevision {
		return &pb.CheckInResponse{}, nil
	}
	return &pb.CheckInResponse{Desired: wire.Desired(d)}, nil
}

func (s *Server) GetDesiredState(ctx context.Context, r *pb.GetDesiredStateRequest) (*pb.GetDesiredStateResponse, error) {
	if e := s.authorizeEnrolledAgent(ctx, r.GetAgentId()); e != nil {
		return nil, e
	}
	d, e := s.Store.Repos().Agents.Desired(ctx, r.AgentId)
	if e != nil {
		return nil, failure(e)
	}
	return &pb.GetDesiredStateResponse{Desired: wire.Desired(d)}, nil
}

func (s *Server) WatchDesiredState(r *pb.WatchDesiredStateRequest, stream grpc.ServerStreamingServer[pb.WatchDesiredStateResponse]) error {
	if e := s.authorizeEnrolledAgent(stream.Context(), r.GetAgentId()); e != nil {
		return e
	}
	if s.Hub == nil {
		return status.Error(codes.Unavailable, "notification service unavailable; use check-in")
	}
	ch, unsubscribe := s.Hub.subscribe(r.AgentId)
	defer unsubscribe()
	last := r.AfterRevision
	d, e := s.Store.Repos().Agents.Desired(stream.Context(), r.AgentId)
	if e != nil {
		return failure(e)
	}
	if d.Revision != last {
		if e = stream.Send(&pb.WatchDesiredStateResponse{Revision: d.Revision}); e != nil {
			return e
		}
		last = d.Revision
	}
	for {
		select {
		case <-stream.Context().Done():
			return stream.Context().Err()
		case revision := <-ch:
			if revision > last {
				if e = stream.Send(&pb.WatchDesiredStateResponse{Revision: revision}); e != nil {
					return e
				}
				last = revision
			}
		}
	}
}

func (s *Server) authorizeEnrolledAgent(ctx context.Context, want string) error {
	id, e := identity.Agent(ctx, want)
	if e != nil {
		return e
	}
	a, e := s.Store.Repos().Agents.Get(ctx, want)
	if e != nil {
		return failure(e)
	}
	if a.Location != id.Location {
		return status.Error(codes.PermissionDenied, "agent location differs from enrollment")
	}
	return nil
}

func (s *Server) Propose(ctx context.Context, r *pb.ProposeRequest) (out *pb.ProposeResponse, err error) {
	actor, e := identity.Operator(ctx)
	if e != nil {
		return nil, e
	}
	out, err = s.propose(ctx, r, actor, false)
	return out, failure(err)
}

func (s *Server) propose(ctx context.Context, r *pb.ProposeRequest, actor string, discovered bool) (out *pb.ProposeResponse, err error) {
	if e := validRequest(r.GetRequestId(), "proposal"); e != nil {
		return nil, e
	}
	proposal, e := wire.ParseExperiment(r.Experiment)
	if e != nil {
		return nil, invalid(e)
	}
	if e = treatment.ValidateProposal(proposal); e != nil {
		return nil, invalid(e)
	}
	if proposal.Treatment.Settings.Algorithm() == "" {
		return nil, status.Error(codes.InvalidArgument, "v1 experiments require congestion_control to measure actual socket exposure")
	}
	if len(proposal.CohortOrder) < int(proposal.Policy.MinimumTreatedHosts)+2 {
		return nil, status.Error(codes.InvalidArgument, "population must leave at least two controls after minimum treated hosts")
	}
	out = new(pb.ProposeResponse)
	err = s.Store.Tx(ctx, func(rp store.Repos) error {
		// Discovery's earlier lookup is only an optimization. Recheck under
		// the controller mutation lock after warehouse analysis. Explicit
		// operator proposals may intentionally replace a stable predecessor.
		if discovered {
			blocked, err := rp.Experiments.DestinationBlocked(ctx, proposal.Location, proposal.Treatment.Destination, s.now().Add(-24*time.Hour))
			if err != nil {
				return err
			}
			if blocked {
				return errDiscoverySuppressed
			}
		}
		if yes, e := replay(ctx, rp, r.RequestId, "propose", r, out); yes || e != nil {
			return e
		}
		exists, e := rp.Experiments.Exists(ctx, proposal.ID)
		if e != nil {
			return e
		}
		if exists {
			return errors.New("experiment already exists")
		}
		n := proposal.Clone()
		n.Revision = 1
		n.Status = treatment.StatusProposed
		n.ProposedAt = s.now()
		n.Phases = nil
		n.LatestEvidence = nil
		n.LatestOutcome = treatment.OutcomeUnspecified
		active, e := rp.Experiments.HasActive(ctx, n.Location)
		if e != nil {
			return e
		}
		if active {
			return errExperimentBudget
		}
		// Randomized, reproducible allocation within kernel strata. The full
		// returned order is reviewed and persisted before any treatment occurs.
		strata := map[string][]string{}
		for _, id := range n.CohortOrder {
			a, e := rp.Agents.Get(ctx, id)
			if e != nil {
				return e
			}
			if a.Location != n.Location || !a.Seen(s.now(), agentWindow) || !a.Report.Capabilities.SimpleHostRouting || !a.Report.Capabilities.ActuatorEnabled {
				return fmt.Errorf("agent %s is not eligible", id)
			}
			if algo := n.Treatment.Settings.Algorithm(); algo != "" && !a.Report.Capabilities.Supports(algo) {
				return fmt.Errorf("agent %s lacks %s", id, algo)
			}
			strata[a.Report.Capabilities.KernelRelease] = append(strata[a.Report.Capabilities.KernelRelease], id)
		}
		keys := make([]string, 0, len(strata))
		for k, ids := range strata {
			keys = append(keys, k)
			sort.Slice(ids, func(i, j int) bool {
				a := sha256.Sum256([]byte(n.ID + "/" + ids[i]))
				b := sha256.Sum256([]byte(n.ID + "/" + ids[j]))
				return string(a[:]) < string(b[:])
			})
		}
		sort.Strings(keys)
		n.CohortOrder = nil
		for i := 0; ; i++ {
			added := false
			for _, k := range keys {
				if i < len(strata[k]) {
					n.CohortOrder = append(n.CohortOrder, strata[k][i])
					added = true
				}
			}
			if !added {
				break
			}
		}
		if e := s.save(ctx, rp, n); e != nil {
			return e
		}
		out.Experiment = wire.Experiment(n)
		if e := audit(ctx, rp, actor, r.RequestId, "propose", r); e != nil {
			return e
		}
		return audit(ctx, rp, actor, r.RequestId+".result", "result", out)
	})
	return out, err
}

func (s *Server) Review(ctx context.Context, r *pb.ReviewRequest) (out *pb.ReviewResponse, err error) {
	actor, e := identity.Operator(ctx)
	if e != nil {
		return nil, e
	}
	if e = validRequest(r.GetRequestId(), r.GetReason()); e != nil {
		return nil, e
	}
	evidence, e := wire.ParseEvidence(r.Evidence)
	if e != nil {
		return nil, invalid(e)
	}
	out = new(pb.ReviewResponse)
	err = s.Store.Tx(ctx, func(rp store.Repos) error {
		if yes, e := replay(ctx, rp, r.RequestId, "review", r, out); yes || e != nil {
			return e
		}
		n, e := rp.Experiments.Get(ctx, r.ExperimentId)
		if e != nil {
			return e
		}
		if n.Revision != r.ExpectedRevision {
			return errors.New("experiment revision changed; review fresh evidence")
		}
		if !r.Approve {
			if n.Status, e = experiment.Transition(n.Status, experiment.EventReject); e != nil {
				return e
			}
			n.Revision++
		} else {
			if len(n.Phases) == 0 {
				if evidence != nil {
					if e = saveEvidence(ctx, rp, n.ID, evidence); e != nil {
						return e
					}
					n.Baseline = evidence.Clone()
				}
				if e = treatment.ValidateProposal(n); e != nil {
					return e
				}
				if n.Baseline.AnalyzedAt.After(s.now().Add(30*time.Second)) || s.now().Sub(n.Baseline.WindowEnd) > n.Policy.MaximumEvidenceAge {
					return errors.New("baseline stale/future; supply refreshed baseline evidence with review")
				}
			}
			if len(n.Phases) > 0 {
				if e = s.startObservation(ctx, rp, n); e != nil {
					return e
				}
				if n.LatestOutcome == treatment.OutcomeAwaitingApplication {
					return errors.New("cohort is not currently verified")
				}
				if e = saveEvidence(ctx, rp, n.ID, evidence); e != nil {
					return e
				}
			}
			n, e = experiment.Advance(n, evidence, s.now(), actor, r.Reason)
			if e != nil {
				return e
			}
		}
		if e = s.assign(ctx, rp, n, false); e != nil {
			return e
		}
		if e = s.save(ctx, rp, n); e != nil {
			return e
		}
		out.Experiment = wire.Experiment(n)
		if e = audit(ctx, rp, actor, r.RequestId, "review", r); e != nil {
			return e
		}
		return audit(ctx, rp, actor, r.RequestId+".result", "result", out)
	})
	return out, failure(err)
}

// save stamps the controller's clock on the experiment and persists it.
func (s *Server) save(ctx context.Context, rp store.Repos, e *treatment.Experiment) error {
	e.UpdatedAt = s.now()
	return rp.Experiments.Save(ctx, e)
}

func saveEvidence(ctx context.Context, rp store.Repos, experimentID string, v *treatment.Evidence) error {
	if e := treatment.ValidateEvidence(v); e != nil {
		return e
	}
	return rp.Evidence.Put(ctx, experimentID, v)
}

// startObservation records when the whole current cohort verifiably applied
// the treatment, or marks the experiment awaiting application.
func (s *Server) startObservation(ctx context.Context, rp store.Repos, e *treatment.Experiment) error {
	p := e.CurrentPhase()
	if p == nil {
		return nil
	}
	latest := p.IssuedAt
	notReady := func() error { e.LatestOutcome = treatment.OutcomeAwaitingApplication; return nil }
	for _, id := range e.CohortOrder[:p.TargetHosts] {
		a, err := rp.Agents.Get(ctx, id)
		if err != nil {
			return err
		}
		if !a.Seen(s.now(), agentWindow) || a.Report.LocallySuspended {
			return notReady()
		}
		d, err := rp.Agents.Desired(ctx, id)
		if err != nil {
			return err
		}
		if d.Suspended || a.Report.DesiredRevision != d.Revision {
			return notReady()
		}
		expected := d.Find(e.ID)
		if expected == nil || expected.Mode != treatment.ModeTrial || expected.LeaseExpiresAt.IsZero() || !expected.LeaseExpiresAt.After(s.now()) {
			return notReady()
		}
		found := false
		for i := range a.Report.Records {
			rec := &a.Report.Records[i]
			if rec.Assignment.ExperimentID == expected.ExperimentID && rec.Assignment.Generation == expected.Generation && rec.Status == treatment.ApplyVerified && !rec.ExpiredLatched && !rec.AppliedAt.IsZero() && rec.Assignment.Treatment.Equal(expected.Treatment) {
				found = true
				if rec.AppliedAt.After(s.now().Add(30 * time.Second)) {
					return errors.New("agent clock ahead")
				}
				if rec.AppliedAt.After(latest) {
					latest = rec.AppliedAt
				}
			}
		}
		if !found {
			return notReady()
		}
	}
	if p.ObservationStartedAt.IsZero() {
		p.ObservationStartedAt = latest
	}
	if e.LatestOutcome == treatment.OutcomeAwaitingApplication {
		e.LatestOutcome = treatment.OutcomeObserving
	}
	return nil
}

// assign writes the desired state that follows from the experiment's status
// for every cohort host that is, or becomes, part of the current phase.
func (s *Server) assign(ctx context.Context, rp store.Repos, e *treatment.Experiment, reactivate bool) error {
	target := e.Target()
	for i, id := range e.CohortOrder {
		d, err := rp.Agents.Desired(ctx, id)
		if err != nil {
			return err
		}
		a := d.Find(e.ID)
		if i >= target && a == nil {
			continue
		}
		if a == nil {
			agent, err := rp.Agents.Get(ctx, id)
			if err != nil {
				return err
			}
			caps := agent.Report.Capabilities
			if d.Suspended || agent.Report.LocallySuspended || !agent.Seen(s.now(), agentWindow) || !caps.SimpleHostRouting || !caps.ActuatorEnabled {
				return fmt.Errorf("agent %s no longer eligible", id)
			}
			if !caps.Supports(e.Treatment.Settings.Algorithm()) {
				return fmt.Errorf("agent %s no longer has required algorithm", id)
			}
			var predecessor *treatment.Treatment
			for j := range d.Assignments {
				x := &d.Assignments[j]
				if x.Mode != treatment.ModeRetired && x.Treatment.Destination == e.Treatment.Destination {
					if x.Mode != treatment.ModeStable {
						return errors.New("destination already has an active trial/hold")
					}
					t := x.Treatment
					predecessor = &t
					x.Mode = treatment.ModeRetired
				}
			}
			d.Assignments = append(d.Assignments, treatment.Assignment{ExperimentID: e.ID, Treatment: e.Treatment, Generation: 1, Predecessor: predecessor})
			a = &d.Assignments[len(d.Assignments)-1]
		}
		switch e.Status {
		case treatment.StatusComplete:
			a.Mode = treatment.ModeStable
			a.LeaseExpiresAt = time.Time{}
		case treatment.StatusSuspended:
			a.Mode = treatment.ModeSuspended
		case treatment.StatusRetired, treatment.StatusRolledBack:
			a.Mode = treatment.ModeRetired
			if e.Status == treatment.StatusRolledBack && a.Predecessor != nil {
				a.Treatment = *a.Predecessor
				a.Mode = treatment.ModeStable
				a.Generation++
			}
		default:
			a.Mode = treatment.ModeTrial
			if a.LeaseExpiresAt.IsZero() || reactivate {
				a.LeaseExpiresAt = s.now().Add(treatment.TrialLease)
			}
		}
		if reactivate {
			a.Generation++
		}
		d.Revision++
		d.IssuedAt = s.now()
		if err = treatment.ValidateDesired(d); err != nil {
			return err
		}
		if err = rp.Agents.SaveDesired(ctx, d); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) Control(ctx context.Context, r *pb.ControlRequest) (out *pb.ControlResponse, err error) {
	actor, e := identity.Operator(ctx)
	if e != nil {
		return nil, e
	}
	if e = validRequest(r.GetRequestId(), r.GetReason()); e != nil {
		return nil, e
	}
	nsel := 0
	for _, x := range []string{r.ExperimentId, r.AgentId, r.Location} {
		if x != "" {
			if !treatment.ValidID(x) {
				return nil, status.Error(codes.InvalidArgument, "invalid selector")
			}
			nsel++
		}
	}
	if r.Fleet {
		nsel++
	}
	if nsel != 1 || r.Action < pb.ControlAction_CONTROL_ACTION_PAUSE || r.Action > pb.ControlAction_CONTROL_ACTION_RETIRE {
		return nil, status.Error(codes.InvalidArgument, "one scope and a valid action required")
	}
	out = new(pb.ControlResponse)
	err = s.Store.Tx(ctx, func(rp store.Repos) error {
		if yes, e := replay(ctx, rp, r.RequestId, "control", r, out); yes || e != nil {
			return e
		}
		if r.ExperimentId != "" {
			n, e := rp.Experiments.Get(ctx, r.ExperimentId)
			if e != nil {
				return e
			}
			if n.Status.Terminal() {
				return errors.New("terminal experiment cannot be reactivated")
			}
			event, e := controlEvent(n, r.Action)
			if e != nil {
				return e
			}
			if n.Status, e = experiment.Transition(n.Status, event); e != nil {
				return e
			}
			if event == experiment.EventResume {
				p := n.CurrentPhase()
				n.Phases = append(n.Phases, treatment.Phase{TargetHosts: p.TargetHosts, IssuedAt: s.now(), Reviewer: actor, Reason: r.Reason})
			}
			n.Revision++
			if e = s.assign(ctx, rp, n, r.Action == pb.ControlAction_CONTROL_ACTION_RESUME); e != nil {
				return e
			}
			if e = s.save(ctx, rp, n); e != nil {
				return e
			}
			out.AffectedAgents = uint32(len(n.CohortOrder))
		} else {
			if r.Action != pb.ControlAction_CONTROL_ACTION_SUSPEND && r.Action != pb.ControlAction_CONTROL_ACTION_RESUME {
				return errors.New("non-experiment controls support suspend/resume only")
			}
			scope := "fleet"
			if r.AgentId != "" {
				scope = "agent:" + r.AgentId
			}
			if r.Location != "" {
				scope = "location:" + r.Location
			}
			var e error
			if r.Action == pb.ControlAction_CONTROL_ACTION_SUSPEND {
				e = rp.Holds.Set(ctx, scope, r.Reason)
			} else {
				e = rp.Holds.Clear(ctx, scope)
			}
			if e != nil {
				return e
			}
			var hosts []store.AgentRef
			if r.AgentId != "" {
				a, e := rp.Agents.Get(ctx, r.AgentId)
				if e != nil && !errors.Is(e, store.ErrNotFound) {
					return e
				}
				if e == nil {
					hosts = append(hosts, store.AgentRef{ID: a.ID, Location: a.Location})
				}
			} else if hosts, e = rp.Agents.List(ctx, r.Location); e != nil {
				return e
			}
			for _, h := range hosts {
				d, e := rp.Agents.Desired(ctx, h.ID)
				if e != nil {
					return e
				}
				if d.Suspended, e = rp.Holds.Held(ctx, holdScopes(h.Location, h.ID)...); e != nil {
					return e
				}
				d.Revision++
				d.IssuedAt = s.now()
				if e = rp.Agents.SaveDesired(ctx, d); e != nil {
					return e
				}
			}
			out.AffectedAgents = uint32(len(hosts))
		}
		if e := audit(ctx, rp, actor, r.RequestId, "control", r); e != nil {
			return e
		}
		return audit(ctx, rp, actor, r.RequestId+".result", "result", out)
	})
	return out, failure(err)
}

// controlEvent maps an operator action onto a state-machine event. Resume
// needs the experiment: a held completed treatment resumes as stable, a held
// trial resumes with a fresh phase, and a held proposal cannot resume at all.
func controlEvent(n *treatment.Experiment, action pb.ControlAction) (experiment.Event, error) {
	switch action {
	case pb.ControlAction_CONTROL_ACTION_PAUSE:
		return experiment.EventPause, nil
	case pb.ControlAction_CONTROL_ACTION_SUSPEND:
		return experiment.EventSuspend, nil
	case pb.ControlAction_CONTROL_ACTION_RETIRE:
		return experiment.EventRetire, nil
	case pb.ControlAction_CONTROL_ACTION_ROLLBACK:
		return experiment.EventRollback, nil
	case pb.ControlAction_CONTROL_ACTION_RESUME:
		p := n.CurrentPhase()
		if p == nil {
			return 0, errors.New("proposal requires initial review")
		}
		if p.Outcome == treatment.OutcomeFinalSafety {
			return experiment.EventResumeStable, nil
		}
		return experiment.EventResume, nil
	}
	return 0, fmt.Errorf("unknown control action %s", action)
}

func (s *Server) Inspect(ctx context.Context, r *pb.InspectRequest) (*pb.InspectResponse, error) {
	if _, e := identity.Operator(ctx); e != nil {
		return nil, e
	}
	repos := s.Store.Repos()
	out := new(pb.InspectResponse)
	list, e := repos.Experiments.List(ctx, r.ExperimentId, 1000)
	if e != nil {
		return nil, failure(e)
	}
	for _, v := range list {
		out.Experiments = append(out.Experiments, wire.Experiment(v))
	}
	// Host details are deliberately explicit; a dashboard must not pull the
	// entire fleet's route journals on every refresh.
	if r.AgentId != "" {
		d, e := repos.Agents.Desired(ctx, r.AgentId)
		if e != nil {
			return nil, failure(e)
		}
		out.Desired = append(out.Desired, wire.Desired(d))
		a, e := repos.Agents.Get(ctx, r.AgentId)
		if e != nil {
			return nil, failure(e)
		}
		if !a.LastSeen.IsZero() {
			out.Observed = append(out.Observed, wire.Report(&a.Report))
		}
	}
	return out, nil
}
