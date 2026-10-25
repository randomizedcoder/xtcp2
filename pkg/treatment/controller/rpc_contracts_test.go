package controller

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_controller"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestCheckInValidation(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		change            func(*pb.CheckInRequest)
		want              codes.Code
	}{
		{"wrong-host", "certificate identity cannot report another host", func(r *pb.CheckInRequest) { r.AgentId = "other" }, codes.PermissionDenied},
		{"wrong-location", "certificate location is authoritative", func(r *pb.CheckInRequest) { r.Location = "other" }, codes.InvalidArgument},
		{"missing-capabilities", "capabilities must be present", func(r *pb.CheckInRequest) { r.Capabilities = nil }, codes.InvalidArgument},
		{"invalid-boot", "boot identity is required", func(r *pb.CheckInRequest) { r.Capabilities.BootId = "" }, codes.InvalidArgument},
		{"long-kernel", "kernel version is bounded", func(r *pb.CheckInRequest) { r.Capabilities.KernelRelease = strings.Repeat("a", 257) }, codes.InvalidArgument},
		{"long-algorithm", "algorithm names fit kernel ABI", func(r *pb.CheckInRequest) { r.Capabilities.CongestionAlgorithms = []string{strings.Repeat("a", 16)} }, codes.InvalidArgument},
		{"nil-record", "nil application record fails closed", func(r *pb.CheckInRequest) { r.Records = []*pb.ApplicationRecord{nil} }, codes.InvalidArgument},
		{"kernel-bytes", "kernel route bytes must remain local", func(r *pb.CheckInRequest) {
			r.Records = []*pb.ApplicationRecord{{Assignment: &pb.Assignment{Id: "assignment"}, InstalledRoute: []byte{1}}}
		}, codes.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &pb.CheckInRequest{AgentId: "host", Location: "dc1", Capabilities: &pb.Capabilities{BootId: "boot", CongestionAlgorithms: []string{"cubic"}}}
			tc.change(r)
			_, err := (&Server{}).CheckIn(testIdentity("agent/dc1/host"), r)
			if status.Code(err) != tc.want {
				t.Fatalf("%s: got %v want %v", tc.description, err, tc.want)
			}
		})
	}
}

func operatorHTTP(method, path, identity string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	if identity != "" {
		u, _ := url.Parse("spiffe://xtcp/" + identity)
		cert := &x509.Certificate{URIs: []*url.URL{u}}
		r.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}, VerifiedChains: [][]*x509.Certificate{{cert}}}
	}
	return r
}
func TestStatusAuthorization(t *testing.T) {
	for _, tc := range []struct {
		description, method, path, identity string
		want                                int
	}{
		{"mTLS is mandatory", "GET", "/status", "", 401},
		{"agents cannot inspect fleet status", "GET", "/status", "agent/dc1/host", 403},
		{"status has no mutation endpoint", "POST", "/status", "operator/alice", 405},
		{"unknown paths return not found", "GET", "/unknown", "operator/alice", 404},
	} {
		t.Run(tc.description, func(t *testing.T) {
			w := httptest.NewRecorder()
			(&Server{}).StatusHandler().ServeHTTP(w, operatorHTTP(tc.method, tc.path, tc.identity))
			if w.Code != tc.want {
				t.Fatalf("got %d want %d", w.Code, tc.want)
			}
		})
	}
}

func TestPostgresStatusAndDesiredAuthorization(t *testing.T) {
	s := &Server{Store: testStore(t)}
	enroll(t, s, "host")
	for _, tc := range []struct {
		description, identity string
		want                  codes.Code
	}{
		{"enrolled identity can fetch its desired state", "agent/dc1/host", codes.OK},
		{"enrollment location cannot change", "agent/other/host", codes.PermissionDenied},
		{"different host cannot fetch desired state", "agent/dc1/other", codes.PermissionDenied},
		{"operator identity is not an agent", "operator/alice", codes.PermissionDenied},
	} {
		t.Run(tc.description, func(t *testing.T) {
			got, err := s.GetDesiredState(testIdentity(tc.identity), &pb.GetDesiredStateRequest{AgentId: "host"})
			if status.Code(err) != tc.want {
				t.Fatalf("got %v want %v", err, tc.want)
			}
			if err == nil && (got.Desired.AgentId != "host" || got.Desired.Revision != 1) {
				t.Fatal("wrong desired state")
			}
		})
	}
	for _, path := range []string{"/status", "/metrics", "/readyz"} {
		t.Run(path, func(t *testing.T) {
			w := httptest.NewRecorder()
			s.StatusHandler().ServeHTTP(w, operatorHTTP(http.MethodGet, path, "operator/alice"))
			if w.Code != 200 || w.Body.Len() == 0 {
				t.Fatalf("got %d %s", w.Code, w.Body.String())
			}
			if path == "/status" && !strings.Contains(w.Body.String(), `"agents":1`) {
				t.Fatal("enrolled agent missing from status")
			}
		})
	}
	if err := s.Store.DB.Close(); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.StatusHandler().ServeHTTP(w, operatorHTTP(http.MethodGet, "/readyz", "operator/alice"))
	if w.Code != 503 {
		t.Fatalf("closed DB reported ready: %d", w.Code)
	}
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

func TestPostgresWatchLifecycle(t *testing.T) {
	s := &Server{Store: testStore(t), Hub: NewNotificationHub()}
	enroll(t, s, "host")
	ctx, cancel := context.WithTimeout(testIdentity("agent/dc1/host"), 5*time.Second)
	defer cancel()
	stream := &desiredTestStream{ctx: ctx, sent: make(chan uint64, 4)}
	done := make(chan error, 1)
	go func() { done <- s.WatchDesiredState(&pb.WatchDesiredStateRequest{AgentId: "host"}, stream) }()
	receive := func(want uint64) {
		t.Helper()
		select {
		case got := <-stream.sent:
			if got != want {
				t.Fatalf("revision=%d want %d", got, want)
			}
		case <-ctx.Done():
			t.Fatal("watch timed out")
		}
	}
	receive(1)
	s.Hub.publish("host", 1)
	s.Hub.publish("host", 2)
	receive(2)
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("watch cancellation: %v", err)
	}
	s.Hub.mu.Lock()
	defer s.Hub.mu.Unlock()
	if len(s.Hub.subs) != 0 {
		t.Fatal("watch leaked subscription")
	}
}
