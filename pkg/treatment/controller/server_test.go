package controller

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/identity"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func testIdentity(path string) context.Context {
	u, _ := url.Parse("spiffe://xtcp/" + path)
	cert := &x509.Certificate{URIs: []*url.URL{u}}
	return peer.NewContext(context.Background(), &peer.Peer{AuthInfo: credentials.TLSInfo{State: tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}}})
}

func testStore(t *testing.T) *Store {
	t.Helper()
	dsn := os.Getenv("XTCP_CONTROLLER_TEST_DSN")
	if dsn == "" {
		t.Skip("set XTCP_CONTROLLER_TEST_DSN for isolated PostgreSQL integration")
	}
	db, e := sql.Open("postgres", dsn)
	if e != nil {
		t.Fatal(e)
	}
	schema := fmt.Sprintf("xtcp_test_%d", time.Now().UnixNano())
	if _, e = db.Exec("CREATE SCHEMA " + schema); e != nil {
		db.Close()
		t.Fatal(e)
	}
	if strings.HasPrefix(dsn, "postgres://") || strings.HasPrefix(dsn, "postgresql://") {
		u, e := url.Parse(dsn)
		if e != nil {
			t.Fatal(e)
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		dsn = u.String()
	} else {
		dsn += " search_path=" + schema
	}
	s, e := Open(context.Background(), dsn)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.DB.Close(); _, _ = db.Exec("DROP SCHEMA " + schema + " CASCADE"); db.Close() })
	if e = s.Migrate(context.Background()); e != nil {
		t.Fatal(e)
	}
	return s
}

func enroll(t *testing.T, s *Server, id string) {
	t.Helper()
	_, e := s.CheckIn(testIdentity("agent/dc1/"+id), &pb.CheckInRequest{AgentId: id, Location: "dc1", Capabilities: &pb.Capabilities{BootId: "boot-1", KernelRelease: "test", CongestionAlgorithms: []string{"cubic", "bbr"}, SimpleHostRouting: true, ActuatorEnabled: true}})
	if e != nil {
		t.Fatal(e)
	}
}
func proposal(now time.Time) *pb.ProposeRequest {
	e := &pb.Experiment{Id: "experiment-1", Location: "dc1", Hypothesis: "test cubic", Policy: treatment.DefaultPolicy(), Treatment: &pb.Treatment{Id: "cubic", Revision: 1, Destination: "192.0.2.1/32", Settings: &pb.TCPSettings{CongestionControl: proto.String("cubic")}}, Baseline: &pb.Evidence{Id: "baseline-1", AnalyzedAt: timestamppb.New(now), WindowStart: timestamppb.New(now.Add(-time.Hour)), WindowEnd: timestamppb.New(now.Add(-time.Second)), QueryVersion: "test", ArtifactSha256: strings.Repeat("a", 64)}}
	for i := range 8 {
		id := fmt.Sprintf("host-%d", i)
		e.CohortOrder = append(e.CohortOrder, id)
		e.Baseline.Hosts = append(e.Baseline.Hosts, &pb.HostEvidence{AgentId: id, Sockets: 100, BytesAcked: 1 << 25, GoodputBps: 1e8, RttP95Ms: 1, CollectionHealthy: true})
	}
	return &pb.ProposeRequest{Experiment: e, RequestId: "proposal-1"}
}

func TestPostgresReviewedRolloutAndPersistentFleetHold(t *testing.T) {
	now := time.Now().UTC()
	s := &Server{Store: testStore(t), Now: func() time.Time { return now }}
	operator := testIdentity("operator/alice")
	for i := range 8 {
		enroll(t, s, fmt.Sprintf("host-%d", i))
	}
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
	var assignments int
	if e = s.Store.DB.QueryRow("SELECT sum(jsonb_array_length(coalesce(desired->'assignments','[]'))) FROM xtcp_control_agents").Scan(&assignments); e != nil {
		t.Fatal(e)
	}
	if assignments != 1 {
		t.Fatal("incorrect fleet exposure", assignments)
	}
	if _, e = s.Control(operator, &pb.ControlRequest{Fleet: true, Action: pb.ControlAction_CONTROL_ACTION_SUSPEND, Reason: "incident", RequestId: "hold-1"}); e != nil {
		t.Fatal(e)
	}
	enroll(t, s, "new-host")
	d, e := desired(context.Background(), s.Store.DB, "new-host")
	if e != nil || !d.Suspended {
		t.Fatal("new host escaped persistent fleet hold", e)
	}
	// A narrower resume cannot clear the fleet-wide hold.
	if _, e = s.Control(operator, &pb.ControlRequest{AgentId: "new-host", Action: pb.ControlAction_CONTROL_ACTION_RESUME, Reason: "host resume", RequestId: "resume-1"}); e != nil {
		t.Fatal(e)
	}
	d, e = desired(context.Background(), s.Store.DB, "new-host")
	if e != nil || !d.Suspended {
		t.Fatal("narrower resume overrode fleet hold")
	}
	if _, e = s.Review(testIdentity("agent/dc1/host-0"), r); e == nil {
		t.Fatal("agent obtained operator authority")
	}
}

func TestPostgresConcurrentReviewFencesReplicas(t *testing.T) {
	now := time.Now().UTC()
	s := &Server{Store: testStore(t), Now: func() time.Time { return now }}
	operator := testIdentity("operator/alice")
	for i := range 8 {
		enroll(t, s, fmt.Sprintf("host-%d", i))
	}
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
	var channels []chan uint64
	var cleanup []func()
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

type fixedEvidence struct{ value *pb.Evidence }

func (f fixedEvidence) Analyze(context.Context, *pb.Experiment) (*pb.Evidence, error) {
	return f.value, nil
}

func acknowledgeFleet(t *testing.T, s *Server, n *pb.Experiment) {
	t.Helper()
	for _, id := range n.CohortOrder {
		d, e := desired(context.Background(), s.Store.DB, id)
		if e != nil {
			t.Fatal(e)
		}
		r := &pb.CheckInRequest{AgentId: id, Location: n.Location, DesiredRevision: d.Revision, Capabilities: &pb.Capabilities{BootId: "boot-1", KernelRelease: "test", CongestionAlgorithms: []string{"cubic", "bbr"}, SimpleHostRouting: true, ActuatorEnabled: true}}
		for _, a := range d.Assignments {
			r.Records = append(r.Records, &pb.ApplicationRecord{Assignment: proto.Clone(a).(*pb.Assignment), DesiredRevision: d.Revision, Status: pb.ApplyStatus_APPLY_STATUS_VERIFIED, AppliedAt: n.Phases[len(n.Phases)-1].IssuedAt})
		}
		if _, e = s.CheckIn(testIdentity("agent/dc1/"+id), r); e != nil {
			t.Fatal(e)
		}
	}
}

func TestPostgresCompleteReviewedRolloutAndStableWithdrawal(t *testing.T) {
	now := time.Now().UTC()
	s := &Server{Store: testStore(t), Now: func() time.Time { return now }}
	operator := testIdentity("operator/alice")
	for i := range 8 {
		enroll(t, s, fmt.Sprintf("host-%d", i))
	}
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
		if e = s.Maintain(context.Background(), fixedEvidence{ev}); e != nil {
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
		d, e := desired(context.Background(), s.Store.DB, id)
		if e != nil {
			t.Fatal(e)
		}
		if d.Assignments[0].Mode != pb.AssignmentMode_ASSIGNMENT_MODE_STABLE || d.Assignments[0].LeaseExpiresAt != nil {
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
}

func TestPostgresFastSafetyWithdrawalBeforePromotionWindow(t *testing.T) {
	now := time.Now().UTC()
	s := &Server{Store: testStore(t), Now: func() time.Time { return now }}
	operator := testIdentity("operator/alice")
	for i := range 8 {
		enroll(t, s, fmt.Sprintf("host-%d", i))
	}
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
	if e = s.Maintain(context.Background(), fixedEvidence{ev}); e != nil {
		t.Fatal(e)
	}
	v, e := s.Inspect(operator, &pb.InspectRequest{ExperimentId: r.Experiment.Id})
	if e != nil {
		t.Fatal(e)
	}
	if v.Experiments[0].Status != pb.ExperimentStatus_EXPERIMENT_STATUS_ROLLED_BACK {
		t.Fatal("harm was not withdrawn before thirty minutes")
	}
}

func TestPostgresAnalyzeRefreshesWithoutApproval(t *testing.T) {
	now := time.Now().UTC()
	s := &Server{Store: testStore(t), Now: func() time.Time { return now }}
	operator := testIdentity("operator/alice")
	for i := range 8 {
		enroll(t, s, fmt.Sprintf("host-%d", i))
	}
	p := proposal(now)
	if _, e := s.Propose(operator, p); e != nil {
		t.Fatal(e)
	}
	ev := proto.Clone(p.Experiment.Baseline).(*pb.Evidence)
	ev.Id = "refresh-1"
	s.EvidenceSource = fixedEvidence{ev}
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
}
