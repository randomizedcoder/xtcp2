package controller

import (
	"context"
	"crypto/x509"
	"fmt"
	"net/url"
	"sync"
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store/pgtest"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/identity"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/wire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestReviewedRolloutAndPersistentFleetHold(t *testing.T) {
	eachStore(t, func(t *testing.T, st store.Store) {
		now := time.Now().UTC().Truncate(time.Microsecond)
		s := testServer(st, &now)
		operator := testIdentity("operator/alice")
		enrollFleet(t, s, 8)
		p, e := s.Propose(operator, proposal(now))
		if e != nil {
			t.Fatal(e)
		}
		if len(p.Experiment.Phases) != 0 {
			t.Fatal("proposal applied without approval")
		}
		r := &pb.ReviewRequest{ExperimentId: p.Experiment.Id, ExpectedRevision: 1, Approve: true, Reason: "reviewed", RequestId: "review-1"}
		v, e := s.Review(operator, r)
		if e != nil {
			t.Fatal(e)
		}
		if len(v.Experiment.Phases) != 1 || v.Experiment.Phases[0].TargetHosts != 1 {
			t.Fatal("wrong initial canary")
		}
		replay, e := s.Review(operator, r)
		if e != nil || !proto.Equal(replay, v) {
			t.Fatal("review retry was not idempotent", e)
		}
		bad := proto.Clone(r).(*pb.ReviewRequest)
		bad.Reason = "changed"
		if _, e = s.Review(operator, bad); e == nil {
			t.Fatal("accepted reused request ID with changed body")
		}
		if n := fleetAssignments(t, st); n != 1 {
			t.Fatal("incorrect fleet exposure", n)
		}
		if _, e = s.Control(operator, &pb.ControlRequest{Fleet: true, Action: pb.ControlAction_CONTROL_ACTION_SUSPEND, Reason: "incident", RequestId: "hold-1"}); e != nil {
			t.Fatal(e)
		}
		enroll(t, s, "new-host")
		if !desiredOf(t, st, "new-host").Suspended {
			t.Fatal("new host escaped persistent fleet hold")
		}
		// A narrower resume cannot clear the fleet-wide hold.
		if _, e = s.Control(operator, &pb.ControlRequest{AgentId: "new-host", Action: pb.ControlAction_CONTROL_ACTION_RESUME, Reason: "host resume", RequestId: "resume-1"}); e != nil {
			t.Fatal(e)
		}
		if !desiredOf(t, st, "new-host").Suspended {
			t.Fatal("narrower resume overrode fleet hold")
		}
		if _, e = s.Review(testIdentity("agent/dc1/host-0"), r); e == nil {
			t.Fatal("agent obtained operator authority")
		}
	})
}

// TestPostgresConcurrentReviewFencesReplicas is PostgreSQL-only: the fence
// under test is the advisory lock two controller replicas share.
func TestPostgresConcurrentReviewFencesReplicas(t *testing.T) {
	// Deliberately not truncated: the baseline stored with a nanosecond
	// clock must still re-save after a microsecond-precision read back.
	now := time.Now().UTC()
	s := testServer(pgtest.Open(t), &now)
	operator := testIdentity("operator/alice")
	enrollFleet(t, s, 8)
	if _, e := s.Propose(operator, proposal(now)); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for i := range 2 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			replica := &Server{Store: s.Store, Now: s.Now}
			_, e := replica.Review(operator, &pb.ReviewRequest{ExperimentId: "experiment-1", ExpectedRevision: 1, Approve: true, Reason: "reviewed", RequestId: fmt.Sprintf("concurrent-%d", i)})
			errs <- e
		}(i)
	}
	wg.Wait()
	close(errs)
	success := 0
	for e := range errs {
		if e == nil {
			success++
		} else {
			t.Log(e)
		}
	}
	if success != 1 {
		t.Fatal("concurrent reviewers both advanced", success)
	}
}

func TestCertificateIdentityIsolation(t *testing.T) {
	if _, e := identity.Agent(testIdentity("agent/dc1/host-1"), "host-2"); e == nil {
		t.Fatal("cross-agent identity accepted")
	}
	if _, e := identity.Operator(context.Background()); e == nil {
		t.Fatal("missing TLS accepted")
	}
	u, _ := url.Parse("spiffe://xtcp/operator/alice")
	v, _ := url.Parse("spiffe://xtcp/agent/dc1/host-1")
	if _, e := identity.FromCertificate(&x509.Certificate{URIs: []*url.URL{u, v}}); e == nil {
		t.Fatal("ambiguous role accepted")
	}
}

func TestNotificationFanoutTenThousandAgents(t *testing.T) {
	h := NewNotificationHub()
	channels := make([]chan uint64, 0, 10000)
	cleanup := make([]func(), 0, 10000)
	for i := range 10000 {
		ch, done := h.subscribe(fmt.Sprint(i))
		channels = append(channels, ch)
		cleanup = append(cleanup, done)
	}
	for i := range 10000 {
		h.publish(fmt.Sprint(i), 1)
		h.publish(fmt.Sprint(i), 2)
	}
	for _, ch := range channels {
		select {
		case v := <-ch:
			if v != 2 {
				t.Fatal("did not coalesce latest revision")
			}
		default:
			t.Fatal("missing suspension notification")
		}
	}
	for _, done := range cleanup {
		done()
	}
}

func TestCompleteReviewedRolloutAndStableWithdrawal(t *testing.T) {
	eachStore(t, func(t *testing.T, st store.Store) {
		now := time.Now().UTC().Truncate(time.Microsecond)
		s := testServer(st, &now)
		operator := testIdentity("operator/alice")
		enrollFleet(t, s, 8)
		if _, e := s.Propose(operator, proposal(now)); e != nil {
			t.Fatal(e)
		}
		approved, e := s.Review(operator, &pb.ReviewRequest{ExperimentId: "experiment-1", ExpectedRevision: 1, Approve: true, Reason: "initial review", RequestId: "approve-initial"})
		if e != nil {
			t.Fatal(e)
		}
		n := approved.Experiment
		for phase, target := range []uint32{1, 2, 4, 6, 8} {
			if n.Phases[len(n.Phases)-1].TargetHosts != target {
				t.Fatal("wrong cohort", n)
			}
			now = now.Add(31 * time.Minute)
			acknowledgeFleet(t, s, n)
			ev := proto.Clone(n.Baseline).(*pb.Evidence)
			ev.Id = fmt.Sprintf("phase-evidence-%d", phase)
			ev.WindowStart = n.Phases[len(n.Phases)-1].IssuedAt
			ev.WindowEnd = timestamppb.New(now.Add(-time.Second))
			ev.AnalyzedAt = timestamppb.New(now)
			for _, h := range ev.Hosts {
				for i, id := range n.CohortOrder {
					if h.AgentId == id && i < int(target) {
						h.GoodputBps *= 1.2
						h.ExposedSockets = h.Sockets
					}
				}
			}
			if e = s.Maintain(context.Background(), fixedEvidence{parseEvidence(t, ev)}); e != nil {
				t.Fatal("maintenance", e)
			}
			// Maintenance may renew leases but must never expand the cohort.
			inspected, e := s.Inspect(operator, &pb.InspectRequest{ExperimentId: n.Id})
			if e != nil {
				t.Fatal(e)
			}
			if inspected.Experiments[0].Revision != n.Revision || len(inspected.Experiments[0].Phases) != len(n.Phases) {
				t.Fatal("maintenance promoted without review")
			}
			acknowledgeFleet(t, s, n)
			approved, e = s.Review(operator, &pb.ReviewRequest{ExperimentId: n.Id, ExpectedRevision: n.Revision, Approve: true, Reason: "evidence reviewed", RequestId: fmt.Sprintf("approve-phase-%d", phase), Evidence: ev})
			if e != nil {
				t.Fatalf("phase %d review: %v", phase, e)
			}
			n = approved.Experiment
		}
		if n.Status != pb.ExperimentStatus_EXPERIMENT_STATUS_COMPLETE {
			t.Fatal("rollout did not complete")
		}
		for _, id := range n.CohortOrder {
			d := desiredOf(t, st, id)
			if d.Assignments[0].Mode != treatment.ModeStable || !d.Assignments[0].LeaseExpiresAt.IsZero() {
				t.Fatal("stable treatment still leased")
			}
		}
		for i, action := range []pb.ControlAction{pb.ControlAction_CONTROL_ACTION_SUSPEND, pb.ControlAction_CONTROL_ACTION_RESUME} {
			if _, e = s.Control(operator, &pb.ControlRequest{ExperimentId: n.Id, Action: action, Reason: "withdrawal comparison", RequestId: fmt.Sprintf("withdrawal-%d", i)}); e != nil {
				t.Fatal(e)
			}
		}
		inspected, e := s.Inspect(operator, &pb.InspectRequest{ExperimentId: n.Id})
		if e != nil {
			t.Fatal(e)
		}
		if inspected.Experiments[0].Status != pb.ExperimentStatus_EXPERIMENT_STATUS_COMPLETE {
			t.Fatal("resume did not retain stable approval")
		}
	})
}

func TestFastSafetyWithdrawalBeforePromotionWindow(t *testing.T) {
	eachStore(t, func(t *testing.T, st store.Store) {
		now := time.Now().UTC().Truncate(time.Microsecond)
		s := testServer(st, &now)
		operator := testIdentity("operator/alice")
		enrollFleet(t, s, 8)
		if _, e := s.Propose(operator, proposal(now)); e != nil {
			t.Fatal(e)
		}
		r, e := s.Review(operator, &pb.ReviewRequest{ExperimentId: "experiment-1", ExpectedRevision: 1, Approve: true, Reason: "initial", RequestId: "initial"})
		if e != nil {
			t.Fatal(e)
		}
		now = now.Add(5 * time.Minute)
		acknowledgeFleet(t, s, r.Experiment)
		ev := proto.Clone(r.Experiment.Baseline).(*pb.Evidence)
		ev.Id = "harmful"
		ev.WindowStart = r.Experiment.Phases[0].IssuedAt
		ev.WindowEnd = timestamppb.New(now)
		ev.AnalyzedAt = ev.WindowEnd
		ev.Hosts[0].RetransmissionRatio = .1
		if e = s.Maintain(context.Background(), fixedEvidence{parseEvidence(t, ev)}); e != nil {
			t.Fatal(e)
		}
		v, e := s.Inspect(operator, &pb.InspectRequest{ExperimentId: r.Experiment.Id})
		if e != nil {
			t.Fatal(e)
		}
		if v.Experiments[0].Status != pb.ExperimentStatus_EXPERIMENT_STATUS_ROLLED_BACK {
			t.Fatal("harm was not withdrawn before thirty minutes")
		}
		// Withdrawal leaves a retired tombstone so the agent removes the route.
		for _, id := range r.Experiment.CohortOrder[:1] {
			d := desiredOf(t, st, id)
			if len(d.Assignments) != 1 || d.Assignments[0].Mode != treatment.ModeRetired {
				t.Fatalf("withdrawn experiment left %+v", d.Assignments)
			}
		}
	})
}

func TestAnalyzeRefreshesWithoutApproval(t *testing.T) {
	eachStore(t, func(t *testing.T, st store.Store) {
		now := time.Now().UTC().Truncate(time.Microsecond)
		s := testServer(st, &now)
		operator := testIdentity("operator/alice")
		enrollFleet(t, s, 8)
		p := proposal(now)
		if _, e := s.Propose(operator, p); e != nil {
			t.Fatal(e)
		}
		ev := proto.Clone(p.Experiment.Baseline).(*pb.Evidence)
		ev.Id = "refresh-1"
		s.EvidenceSource = fixedEvidence{parseEvidence(t, ev)}
		out, e := s.Analyze(operator, &pb.AnalyzeRequest{ExperimentId: p.Experiment.Id, ExpectedRevision: 1})
		if e != nil || out.Outcome != pb.Outcome_OUTCOME_BASELINE {
			t.Fatal("refresh", out, e)
		}
		inspected, e := s.Inspect(operator, &pb.InspectRequest{ExperimentId: p.Experiment.Id})
		if e != nil {
			t.Fatal(e)
		}
		n := inspected.Experiments[0]
		if n.Status != pb.ExperimentStatus_EXPERIMENT_STATUS_PROPOSED || n.Revision != 1 || len(n.Phases) > 0 || !proto.Equal(n.LatestEvidence, ev) {
			t.Fatal("analysis changed rollout authority")
		}
		if _, e = s.Analyze(operator, &pb.AnalyzeRequest{ExperimentId: n.Id, ExpectedRevision: 2}); e == nil {
			t.Fatal("stale analysis revision accepted")
		}
		if _, e = s.Analyze(testIdentity("agent/dc1/host-0"), &pb.AnalyzeRequest{ExperimentId: n.Id, ExpectedRevision: 1}); e == nil {
			t.Fatal("agent could analyze as operator")
		}
	})
}

// TestExperimentUpdatedAtFollowsControllerClock pins the cooldown input:
// every mutation stamps updated_at from the controller clock, not the database.
func TestExperimentUpdatedAtFollowsControllerClock(t *testing.T) {
	eachStore(t, func(t *testing.T, st store.Store) {
		now := time.Unix(1800000000, 0).UTC()
		s := testServer(st, &now)
		operator := testIdentity("operator/alice")
		enrollFleet(t, s, 8)
		if _, e := s.Propose(operator, proposal(now)); e != nil {
			t.Fatal(e)
		}
		if got := experimentsOf(t, st)[0]; !got.UpdatedAt.Equal(now) || !got.ProposedAt.Equal(now) {
			t.Fatalf("proposal stamped %s / %s, want %s", got.ProposedAt, got.UpdatedAt, now)
		}
		now = now.Add(time.Hour)
		if _, e := s.Control(operator, &pb.ControlRequest{ExperimentId: "experiment-1", Action: pb.ControlAction_CONTROL_ACTION_ROLLBACK, Reason: "test", RequestId: "rollback-1"}); e != nil {
			t.Fatal(e)
		}
		got := experimentsOf(t, st)[0]
		if got.Status != treatment.StatusRolledBack || !got.UpdatedAt.Equal(now) {
			t.Fatalf("rollback stamped %s status %s, want %s rolled_back", got.UpdatedAt, got.Status, now)
		}
		if !proto.Equal(wire.Experiment(got).Treatment, proposal(now).Experiment.Treatment) {
			t.Fatal("rollback changed the treatment")
		}
	})
}
