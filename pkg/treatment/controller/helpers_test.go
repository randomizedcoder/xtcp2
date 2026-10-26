package controller

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"net/url"
	"strings"
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store/memory"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store/pgtest"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/wire"
	"google.golang.org/grpc"
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

// eachStore runs fn against the in-memory store always and against
// PostgreSQL when XTCP_CONTROLLER_TEST_DSN is set, so every handler test is
// a unit test by default and an integration test in the Nix harness.
func eachStore(t *testing.T, fn func(t *testing.T, s store.Store)) {
	t.Helper()
	t.Run("memory", func(t *testing.T) { fn(t, memory.New()) })
	t.Run("postgres", func(t *testing.T) { fn(t, pgtest.Open(t)) })
}

func testServer(s store.Store, now *time.Time) *Server {
	return &Server{Store: s, Hub: NewNotificationHub(), Now: func() time.Time { return *now }}
}

func capabilities() *pb.Capabilities {
	return &pb.Capabilities{BootId: "boot-1", KernelRelease: "test", CongestionAlgorithms: []string{"cubic", "bbr"}, SimpleHostRouting: true, ActuatorEnabled: true}
}

func enroll(t *testing.T, s *Server, id string) {
	t.Helper()
	if _, e := s.CheckIn(testIdentity("agent/dc1/"+id), &pb.CheckInRequest{AgentId: id, Location: "dc1", Capabilities: capabilities()}); e != nil {
		t.Fatal(e)
	}
}

func enrollFleet(t *testing.T, s *Server, n int) {
	t.Helper()
	for i := range n {
		enroll(t, s, fmt.Sprintf("host-%d", i))
	}
}

func proposal(now time.Time) *pb.ProposeRequest {
	e := &pb.Experiment{Id: "experiment-1", Location: "dc1", Hypothesis: "test cubic", Policy: wire.Policy(treatment.DefaultPolicy()), Treatment: &pb.Treatment{Id: "cubic", Revision: 1, Destination: "192.0.2.1/32", Settings: &pb.TCPSettings{CongestionControl: proto.String("cubic")}}, Baseline: &pb.Evidence{Id: "baseline-1", AnalyzedAt: timestamppb.New(now), WindowStart: timestamppb.New(now.Add(-time.Hour)), WindowEnd: timestamppb.New(now.Add(-time.Second)), QueryVersion: "test", ArtifactSha256: strings.Repeat("a", 64)}}
	for i := range 8 {
		id := fmt.Sprintf("host-%d", i)
		e.CohortOrder = append(e.CohortOrder, id)
		e.Baseline.Hosts = append(e.Baseline.Hosts, &pb.HostEvidence{AgentId: id, Sockets: 100, BytesAcked: 1 << 25, GoodputBps: 1e8, RttP95Ms: 1, CollectionHealthy: true})
	}
	return &pb.ProposeRequest{Experiment: e, RequestId: "proposal-1"}
}

func desiredOf(t *testing.T, s store.Store, id string) *treatment.DesiredState {
	t.Helper()
	d, e := s.Repos().Agents.Desired(context.Background(), id)
	if e != nil {
		t.Fatal(e)
	}
	return d
}

func experimentsOf(t *testing.T, s store.Store) []*treatment.Experiment {
	t.Helper()
	list, e := s.Repos().Experiments.List(context.Background(), "", 1000)
	if e != nil {
		t.Fatal(e)
	}
	return list
}

func fleetAssignments(t *testing.T, s store.Store) int {
	t.Helper()
	agents, e := s.Repos().Agents.List(context.Background(), "")
	if e != nil {
		t.Fatal(e)
	}
	n := 0
	for _, a := range agents {
		n += len(desiredOf(t, s, a.ID).Assignments)
	}
	return n
}

// seedExperiment stores a bare experiment directly, standing in for history
// written by an earlier controller.
func seedExperiment(t *testing.T, s store.Store, id, location, destination string, status treatment.Status, updated time.Time) {
	t.Helper()
	tr := treatment.Treatment{ID: "cubic", Revision: 1, Settings: treatment.TCPSettings{CongestionControl: proto.String("cubic")}}
	if destination != "" {
		tr.Destination = netip.MustParsePrefix(destination)
	}
	e := &treatment.Experiment{ID: id, Location: location, Hypothesis: "seeded", Treatment: tr, Policy: treatment.DefaultPolicy(), Status: status, Revision: 1, ProposedAt: updated, UpdatedAt: updated}
	if err := s.Repos().Experiments.Save(context.Background(), e); err != nil {
		t.Fatal(err)
	}
}

type fixedEvidence struct{ value *treatment.Evidence }

func (f fixedEvidence) Analyze(context.Context, *treatment.Experiment) (*treatment.Evidence, error) {
	return f.value, nil
}

func parseEvidence(t *testing.T, p *pb.Evidence) *treatment.Evidence {
	t.Helper()
	v, e := wire.ParseEvidence(p)
	if e != nil {
		t.Fatal(e)
	}
	return v
}

// acknowledgeFleet reports every cohort host's assignments as verified at
// the latest phase's issue time.
func acknowledgeFleet(t *testing.T, s *Server, n *pb.Experiment) {
	t.Helper()
	for _, id := range n.CohortOrder {
		d := wire.Desired(desiredOf(t, s.Store, id))
		r := &pb.CheckInRequest{AgentId: id, Location: n.Location, DesiredRevision: d.Revision, Capabilities: capabilities()}
		for _, a := range d.Assignments {
			r.Records = append(r.Records, &pb.ApplicationRecord{Assignment: proto.Clone(a).(*pb.Assignment), DesiredRevision: d.Revision, Status: pb.ApplyStatus_APPLY_STATUS_VERIFIED, AppliedAt: n.Phases[len(n.Phases)-1].IssuedAt})
		}
		if _, e := s.CheckIn(testIdentity("agent/dc1/"+id), r); e != nil {
			t.Fatal(e)
		}
	}
}

type discoveryTransport func(*http.Request) (*http.Response, error)

func (f discoveryTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func operatorHTTP(method, path, identity string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	if identity != "" {
		u, _ := url.Parse("spiffe://xtcp/" + identity)
		cert := &x509.Certificate{URIs: []*url.URL{u}}
		r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}
	}
	return r
}

type desiredTestStream struct {
	grpc.ServerStream
	ctx  context.Context
	sent chan uint64
}

func (s *desiredTestStream) Context() context.Context { return s.ctx }
func (s *desiredTestStream) Send(r *pb.WatchDesiredStateResponse) error {
	select {
	case s.sent <- r.Revision:
		return nil
	case <-s.ctx.Done():
		return s.ctx.Err()
	}
}
