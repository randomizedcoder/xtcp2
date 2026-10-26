package controller

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/experiment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/identity"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type Server struct {
	pb.UnimplementedAgentControlServiceServer
	pb.UnimplementedOperatorServiceServer
	Store          *Store
	Hub            *NotificationHub
	EvidenceSource EvidenceSource
	Now            func() time.Time
	// Zero preserves the production one-hour candidate discovery window.
	DiscoveryWindow time.Duration
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}
func failure(e error) error {
	if e == nil {
		return nil
	}
	if status.Code(e) != codes.Unknown {
		return e
	}
	if errors.Is(e, sql.ErrNoRows) {
		return status.Error(codes.NotFound, "not found")
	}
	if errors.Is(e, context.Canceled) {
		return status.Error(codes.Canceled, "cancelled")
	}
	return status.Error(codes.FailedPrecondition, e.Error())
}
func validRequest(id, reason string) error {
	if !treatment.ValidID(id) || strings.HasSuffix(id, ".result") || reason == "" || len(reason) > 4096 {
		return status.Error(codes.InvalidArgument, "request ID and bounded reason required")
	}
	return nil
}

func (s *Server) CheckIn(ctx context.Context, r *pb.CheckInRequest) (*pb.CheckInResponse, error) {
	id, e := identity.Agent(ctx, r.GetAgentId())
	if e != nil {
		return nil, e
	}
	if id.Location != r.Location || r.Capabilities == nil || !treatment.ValidID(r.Capabilities.BootId) || len(r.Capabilities.KernelRelease) > 256 || len(r.Capabilities.CongestionAlgorithms) > 64 || len(r.Records) > treatment.MaxAssignments*2 {
		return nil, status.Error(codes.InvalidArgument, "invalid agent report")
	}
	for _, a := range r.Capabilities.CongestionAlgorithms {
		if !treatment.ValidID(a) || len(a) > 15 {
			return nil, status.Error(codes.InvalidArgument, "invalid algorithm capability")
		}
	}
	seen := map[string]bool{}
	for _, v := range r.Records {
		if v == nil || v.Assignment == nil || !treatment.ValidID(v.Assignment.ExperimentId) || seen[v.Assignment.ExperimentId] || len(v.Error) > 4096 || len(v.InstalledRoute) > 0 {
			return nil, status.Error(codes.InvalidArgument, "invalid application record (kernel bytes are local only)")
		}
		seen[v.Assignment.ExperimentId] = true
		if e := treatment.ValidateTreatment(v.Assignment.Treatment); e != nil {
			return nil, status.Error(codes.InvalidArgument, e.Error())
		}
	}
	now := s.now()
	initial := &pb.DesiredState{AgentId: r.AgentId, Revision: 1, IssuedAt: timestamppb.New(now)}
	// New enrollment and hold inheritance are serialized against operator
	// controls. The overwhelmingly common heartbeat is one independent update.
	var exists bool
	e = s.Store.DB.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM xtcp_control_agents WHERE id=$1)", r.AgentId).Scan(&exists)
	if e != nil {
		return nil, failure(e)
	}
	if !exists {
		e = s.Store.mutate(ctx, func(tx *sql.Tx) error {
			if err := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM xtcp_control_holds WHERE scope IN ('fleet',$1,$2))", "location:"+r.Location, "agent:"+r.AgentId).Scan(&initial.Suspended); err != nil {
				return err
			}
			b, err := jsonMessage(initial)
			if err != nil {
				return err
			}
			_, err = tx.ExecContext(ctx, "INSERT INTO xtcp_control_agents(id,location,desired) VALUES($1,$2,$3) ON CONFLICT(id) DO NOTHING", r.AgentId, r.Location, b)
			return err
		})
		if e != nil {
			return nil, failure(e)
		}
	}
	b, e := jsonMessage(r)
	if e != nil {
		return nil, failure(e)
	}
	res, e := s.Store.DB.ExecContext(ctx, "UPDATE xtcp_control_agents SET observed=$2,last_seen=$3 WHERE id=$1 AND location=$4", r.AgentId, b, now, r.Location)
	if e != nil {
		return nil, failure(e)
	}
	n, e := res.RowsAffected()
	if e != nil {
		return nil, failure(e)
	}
	if n != 1 {
		return nil, status.Error(codes.PermissionDenied, "agent location cannot change during enrollment")
	}
	d, e := desired(ctx, s.Store.DB, r.AgentId)
	if e != nil {
		return nil, failure(e)
	}
	if d.Revision == r.DesiredRevision {
		return &pb.CheckInResponse{}, nil
	}
	return &pb.CheckInResponse{Desired: d}, nil
}
func (s *Server) GetDesiredState(ctx context.Context, r *pb.GetDesiredStateRequest) (*pb.GetDesiredStateResponse, error) {
	if e := s.authorizeEnrolledAgent(ctx, r.GetAgentId()); e != nil {
		return nil, e
	}
	d, e := desired(ctx, s.Store.DB, r.AgentId)
	return &pb.GetDesiredStateResponse{Desired: d}, failure(e)
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
	d, e := desired(stream.Context(), s.Store.DB, r.AgentId)
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
	var location string
	if e = s.Store.DB.QueryRowContext(ctx, "SELECT location FROM xtcp_control_agents WHERE id=$1", want).Scan(&location); e != nil {
		return failure(e)
	}
	if location != id.Location {
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
	var e error
	if e = validRequest(r.GetRequestId(), "proposal"); e != nil {
		return nil, e
	}
	if e = treatment.ValidateProposal(r.Experiment); e != nil {
		return nil, status.Error(codes.InvalidArgument, e.Error())
	}
	if r.Experiment.Treatment.Settings.GetCongestionControl() == "" {
		return nil, status.Error(codes.InvalidArgument, "v1 experiments require congestion_control to measure actual socket exposure")
	}
	if len(r.Experiment.CohortOrder) < int(r.Experiment.Policy.MinimumTreatedHosts)+2 {
		return nil, status.Error(codes.InvalidArgument, "population must leave at least two controls after minimum treated hosts")
	}
	out = new(pb.ProposeResponse)
	err = s.Store.mutate(ctx, func(tx *sql.Tx) error {
		// Discovery's earlier lookup is only an optimization. Recheck under
		// the controller mutation lock after warehouse analysis. Explicit
		// operator proposals may intentionally replace a stable predecessor.
		if discovered {
			blocked, err := discoveryDestinationBlocked(ctx, tx, r.Experiment.Location, r.Experiment.Treatment.Destination, s.now())
			if err != nil {
				return err
			}
			if blocked {
				return errDiscoverySuppressed
			}
		}
		if yes, e := replay(ctx, tx, r.RequestId, "propose", r, out); yes || e != nil {
			return e
		}
		var exists bool
		if e := tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM xtcp_control_experiments WHERE id=$1)", r.Experiment.Id).Scan(&exists); e != nil {
			return e
		}
		if exists {
			return errors.New("experiment already exists")
		}
		n := proto.Clone(r.Experiment).(*pb.Experiment)
		n.Revision = 1
		n.Status = pb.ExperimentStatus_EXPERIMENT_STATUS_PROPOSED
		n.ProposedAt = timestamppb.New(s.now())
		n.Phases = nil
		n.LatestEvidence = nil
		n.LatestOutcome = pb.Outcome_OUTCOME_UNSPECIFIED
		active, e := hasActiveExperiment(ctx, tx, n.Location)
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
			var location string
			var observed []byte
			var seen time.Time
			if e := tx.QueryRowContext(ctx, "SELECT location,observed,last_seen FROM xtcp_control_agents WHERE id=$1", id).Scan(&location, &observed, &seen); e != nil {
				return e
			}
			o := new(pb.CheckInRequest)
			if e := protojson.Unmarshal(observed, o); e != nil {
				return e
			}
			if location != n.Location || s.now().Sub(seen) > 2*time.Minute || !o.GetCapabilities().GetSimpleHostRouting() || !o.GetCapabilities().GetActuatorEnabled() {
				return fmt.Errorf("agent %s is not eligible", id)
			}
			if algo := n.Treatment.Settings.GetCongestionControl(); algo != "" {
				found := false
				for _, v := range o.Capabilities.CongestionAlgorithms {
					found = found || v == algo
				}
				if !found {
					return fmt.Errorf("agent %s lacks %s", id, algo)
				}
			}
			strata[o.Capabilities.KernelRelease] = append(strata[o.Capabilities.KernelRelease], id)
		}
		keys := make([]string, 0, len(strata))
		for k, ids := range strata {
			keys = append(keys, k)
			sort.Slice(ids, func(i, j int) bool {
				a := sha256.Sum256([]byte(n.Id + "/" + ids[i]))
				b := sha256.Sum256([]byte(n.Id + "/" + ids[j]))
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
		if e := saveExperiment(ctx, tx, n); e != nil {
			return e
		}
		out.Experiment = n
		if e := audit(ctx, tx, actor, r.RequestId, "propose", r); e != nil {
			return e
		}
		return audit(ctx, tx, actor, r.RequestId+".result", "result", out)
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
	out = new(pb.ReviewResponse)
	err = s.Store.mutate(ctx, func(tx *sql.Tx) error {
		if yes, e := replay(ctx, tx, r.RequestId, "review", r, out); yes || e != nil {
			return e
		}
		n, e := loadExperiment(ctx, tx, r.ExperimentId)
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
				if r.Evidence != nil {
					if e = saveEvidence(ctx, tx, n.Id, r.Evidence); e != nil {
						return e
					}
					n.Baseline = proto.Clone(r.Evidence).(*pb.Evidence)
				}
				if e = treatment.ValidateProposal(n); e != nil {
					return e
				}
				if n.Baseline.AnalyzedAt.AsTime().After(s.now().Add(30*time.Second)) || s.now().Sub(n.Baseline.WindowEnd.AsTime()) > n.Policy.MaximumEvidenceAge.AsDuration() {
					return errors.New("baseline stale/future; supply refreshed baseline evidence with review")
				}
			}
			if len(n.Phases) > 0 {
				if e = s.startObservation(ctx, tx, n); e != nil {
					return e
				}
				if n.LatestOutcome == pb.Outcome_OUTCOME_AWAITING_APPLICATION {
					return errors.New("cohort is not currently verified")
				}
				if e = saveEvidence(ctx, tx, n.Id, r.Evidence); e != nil {
					return e
				}
			}
			n, e = experiment.Advance(n, r.Evidence, s.now(), actor, r.Reason)
			if e != nil {
				return e
			}
		}
		if e = s.assign(ctx, tx, n, false); e != nil {
			return e
		}
		if e = saveExperiment(ctx, tx, n); e != nil {
			return e
		}
		out.Experiment = n
		if e = audit(ctx, tx, actor, r.RequestId, "review", r); e != nil {
			return e
		}
		return audit(ctx, tx, actor, r.RequestId+".result", "result", out)
	})
	return out, failure(err)
}

func saveEvidence(ctx context.Context, tx *sql.Tx, id string, v *pb.Evidence) error {
	if e := treatment.ValidateEvidence(v); e != nil {
		return e
	}
	b, e := jsonMessage(v)
	if e != nil {
		return e
	}
	var old []byte
	var owner string
	e = tx.QueryRowContext(ctx, "SELECT state,experiment_id FROM xtcp_control_evidence WHERE id=$1", v.Id).Scan(&old, &owner)
	if e == nil {
		p := new(pb.Evidence)
		if e = protojson.Unmarshal(old, p); e != nil {
			return e
		}
		if owner != id || !proto.Equal(p, v) {
			return errors.New("evidence IDs are immutable and experiment-specific")
		}
		return nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO xtcp_control_evidence(id,experiment_id,state) VALUES($1,$2,$3)", v.Id, id, b)
	return e
}

func (s *Server) startObservation(ctx context.Context, tx *sql.Tx, e *pb.Experiment) error {
	if len(e.Phases) == 0 {
		return nil
	}
	p := e.Phases[len(e.Phases)-1]
	latest := p.IssuedAt.AsTime()
	notReady := func() error { e.LatestOutcome = pb.Outcome_OUTCOME_AWAITING_APPLICATION; return nil }
	for _, id := range e.CohortOrder[:p.TargetHosts] {
		var raw []byte
		var seen time.Time
		if err := tx.QueryRowContext(ctx, "SELECT observed,last_seen FROM xtcp_control_agents WHERE id=$1", id).Scan(&raw, &seen); err != nil {
			return err
		}
		o := new(pb.CheckInRequest)
		if err := protojson.Unmarshal(raw, o); err != nil {
			return err
		}
		if s.now().Sub(seen) > 2*time.Minute || o.LocallySuspended {
			return notReady()
		}
		d, err := desired(ctx, tx, id)
		if err != nil {
			return err
		}
		if d.Suspended || o.DesiredRevision != d.Revision {
			return notReady()
		}
		var expected *pb.Assignment
		for _, a := range d.Assignments {
			if a.ExperimentId == e.Id {
				expected = a
			}
		}
		if expected == nil || expected.Mode != pb.AssignmentMode_ASSIGNMENT_MODE_TRIAL || expected.LeaseExpiresAt == nil || !expected.LeaseExpiresAt.AsTime().After(s.now()) {
			return notReady()
		}
		found := false
		for _, a := range o.Records {
			if a.Assignment.ExperimentId == expected.ExperimentId && a.Assignment.Generation == expected.Generation && a.Status == pb.ApplyStatus_APPLY_STATUS_VERIFIED && !a.ExpiredLatched && a.AppliedAt != nil && a.AppliedAt.CheckValid() == nil && proto.Equal(a.Assignment.Treatment, expected.Treatment) {
				found = true
				t := a.AppliedAt.AsTime()
				if t.After(s.now().Add(30 * time.Second)) {
					return errors.New("agent clock ahead")
				}
				if t.After(latest) {
					latest = t
				}
			}
		}
		if !found {
			return notReady()
		}
	}
	if p.ObservationStartedAt == nil {
		p.ObservationStartedAt = timestamppb.New(latest)
	}
	if e.LatestOutcome == pb.Outcome_OUTCOME_AWAITING_APPLICATION {
		e.LatestOutcome = pb.Outcome_OUTCOME_OBSERVING
	}
	return nil
}

func (s *Server) assign(ctx context.Context, tx *sql.Tx, e *pb.Experiment, reactivate bool) error {
	target := 0
	if len(e.Phases) > 0 {
		target = int(e.Phases[len(e.Phases)-1].TargetHosts)
	}
	for i, id := range e.CohortOrder {
		d, err := desired(ctx, tx, id)
		if err != nil {
			return err
		}
		var a *pb.Assignment
		for _, x := range d.Assignments {
			if x.ExperimentId == e.Id {
				a = x
				break
			}
		}
		if i >= target && a == nil {
			continue
		}
		if a == nil {
			var report []byte
			var seen time.Time
			if err = tx.QueryRowContext(ctx, "SELECT observed,last_seen FROM xtcp_control_agents WHERE id=$1", id).Scan(&report, &seen); err != nil {
				return err
			}
			o := new(pb.CheckInRequest)
			if err = protojson.Unmarshal(report, o); err != nil {
				return err
			}
			if d.Suspended || o.LocallySuspended || s.now().Sub(seen) > 2*time.Minute || !o.GetCapabilities().GetSimpleHostRouting() || !o.GetCapabilities().GetActuatorEnabled() {
				return fmt.Errorf("agent %s no longer eligible", id)
			}
			available := false
			for _, cc := range o.GetCapabilities().GetCongestionAlgorithms() {
				available = available || cc == e.Treatment.Settings.GetCongestionControl()
			}
			if !available {
				return fmt.Errorf("agent %s no longer has required algorithm", id)
			}
			var predecessor *pb.Treatment
			for _, x := range d.Assignments {
				if x.Mode != pb.AssignmentMode_ASSIGNMENT_MODE_RETIRED && x.Treatment.Destination == e.Treatment.Destination {
					if x.Mode != pb.AssignmentMode_ASSIGNMENT_MODE_STABLE {
						return errors.New("destination already has an active trial/hold")
					}
					predecessor = proto.Clone(x.Treatment).(*pb.Treatment)
					x.Mode = pb.AssignmentMode_ASSIGNMENT_MODE_RETIRED
				}
			}
			a = &pb.Assignment{ExperimentId: e.Id, Treatment: proto.Clone(e.Treatment).(*pb.Treatment), Generation: 1}
			d.Assignments = append(d.Assignments, a)
			a.Predecessor = predecessor
		}
		switch e.Status {
		case pb.ExperimentStatus_EXPERIMENT_STATUS_COMPLETE:
			a.Mode = pb.AssignmentMode_ASSIGNMENT_MODE_STABLE
			a.LeaseExpiresAt = nil
		case pb.ExperimentStatus_EXPERIMENT_STATUS_SUSPENDED:
			a.Mode = pb.AssignmentMode_ASSIGNMENT_MODE_SUSPENDED
		case pb.ExperimentStatus_EXPERIMENT_STATUS_RETIRED, pb.ExperimentStatus_EXPERIMENT_STATUS_ROLLED_BACK:
			a.Mode = pb.AssignmentMode_ASSIGNMENT_MODE_RETIRED
			if e.Status == pb.ExperimentStatus_EXPERIMENT_STATUS_ROLLED_BACK && a.Predecessor != nil {
				a.Treatment = proto.Clone(a.Predecessor).(*pb.Treatment)
				a.Mode = pb.AssignmentMode_ASSIGNMENT_MODE_STABLE
				a.Generation++
			}
		default:
			a.Mode = pb.AssignmentMode_ASSIGNMENT_MODE_TRIAL
			if a.LeaseExpiresAt == nil || reactivate {
				a.LeaseExpiresAt = timestamppb.New(s.now().Add(treatment.TrialLease))
			}
		}
		if reactivate {
			a.Generation++
		}
		d.Revision++
		d.IssuedAt = timestamppb.New(s.now())
		if err = treatment.ValidateDesired(d); err != nil {
			return err
		}
		if err = saveDesired(ctx, tx, d); err != nil {
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
	err = s.Store.mutate(ctx, func(tx *sql.Tx) error {
		if yes, e := replay(ctx, tx, r.RequestId, "control", r, out); yes || e != nil {
			return e
		}
		if r.ExperimentId != "" {
			n, e := loadExperiment(ctx, tx, r.ExperimentId)
			if e != nil {
				return e
			}
			if experiment.Terminal(n.Status) {
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
				p := n.Phases[len(n.Phases)-1]
				n.Phases = append(n.Phases, &pb.Phase{TargetHosts: p.TargetHosts, IssuedAt: timestamppb.New(s.now()), Reviewer: actor, Reason: r.Reason})
			}
			n.Revision++
			if e = s.assign(ctx, tx, n, r.Action == pb.ControlAction_CONTROL_ACTION_RESUME); e != nil {
				return e
			}
			if e = saveExperiment(ctx, tx, n); e != nil {
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
				_, e = tx.ExecContext(ctx, "INSERT INTO xtcp_control_holds(scope,reason) VALUES($1,$2) ON CONFLICT(scope) DO UPDATE SET reason=EXCLUDED.reason", scope, r.Reason)
			} else {
				_, e = tx.ExecContext(ctx, "DELETE FROM xtcp_control_holds WHERE scope=$1", scope)
			}
			if e != nil {
				return e
			}
			rows, e := tx.QueryContext(ctx, "SELECT id,location FROM xtcp_control_agents WHERE ($1='' OR id=$1) AND ($2='' OR location=$2)", r.AgentId, r.Location)
			if e != nil {
				return e
			}
			type host struct{ id, loc string }
			var hosts []host
			for rows.Next() {
				var h host
				if e = rows.Scan(&h.id, &h.loc); e != nil {
					rows.Close()
					return e
				}
				hosts = append(hosts, h)
			}
			e = rows.Err()
			rows.Close()
			if e != nil {
				return e
			}
			for _, h := range hosts {
				d, e := desired(ctx, tx, h.id)
				if e != nil {
					return e
				}
				if e = tx.QueryRowContext(ctx, "SELECT EXISTS(SELECT 1 FROM xtcp_control_holds WHERE scope IN ('fleet',$1,$2))", "location:"+h.loc, "agent:"+h.id).Scan(&d.Suspended); e != nil {
					return e
				}
				d.Revision++
				d.IssuedAt = timestamppb.New(s.now())
				if e = saveDesired(ctx, tx, d); e != nil {
					return e
				}
			}
			out.AffectedAgents = uint32(len(hosts))
		}
		if e := audit(ctx, tx, actor, r.RequestId, "control", r); e != nil {
			return e
		}
		return audit(ctx, tx, actor, r.RequestId+".result", "result", out)
	})
	return out, failure(err)
}

// controlEvent maps an operator action onto a state-machine event. Resume
// needs the experiment: a held completed treatment resumes as stable, a held
// trial resumes with a fresh phase, and a held proposal cannot resume at all.
func controlEvent(n *pb.Experiment, action pb.ControlAction) (experiment.Event, error) {
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
		if len(n.Phases) == 0 {
			return 0, errors.New("proposal requires initial review")
		}
		if n.Phases[len(n.Phases)-1].Outcome == pb.Outcome_OUTCOME_FINAL_SAFETY {
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
	out := new(pb.InspectResponse)
	rows, e := s.Store.DB.QueryContext(ctx, "SELECT state FROM xtcp_control_experiments WHERE ($1='' OR id=$1) ORDER BY id LIMIT 1000", r.ExperimentId)
	if e != nil {
		return nil, failure(e)
	}
	for rows.Next() {
		var b []byte
		if e = rows.Scan(&b); e != nil {
			break
		}
		v := new(pb.Experiment)
		if e = protojson.Unmarshal(b, v); e != nil {
			break
		}
		out.Experiments = append(out.Experiments, v)
	}
	e = errors.Join(e, rows.Err())
	rows.Close()
	if e != nil {
		return nil, failure(e)
	}
	// Host details are deliberately explicit; a dashboard must not pull the
	// entire fleet's route journals on every refresh.
	if r.AgentId != "" {
		d, e := desired(ctx, s.Store.DB, r.AgentId)
		if e != nil {
			return nil, failure(e)
		}
		out.Desired = append(out.Desired, d)
		var b []byte
		if e = s.Store.DB.QueryRowContext(ctx, "SELECT observed FROM xtcp_control_agents WHERE id=$1", r.AgentId).Scan(&b); e != nil {
			return nil, failure(e)
		}
		if len(b) > 0 {
			o := new(pb.CheckInRequest)
			if e = protojson.Unmarshal(b, o); e != nil {
				return nil, failure(e)
			}
			out.Observed = append(out.Observed, o)
		}
	}
	return out, nil
}
