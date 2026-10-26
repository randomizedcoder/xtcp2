package controller

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/controller/store/postgres"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TestCheckInValidation runs without a store: every row must be rejected
// before the handler touches persistence.
func TestCheckInValidation(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		change            func(*pb.CheckInRequest)
		expected          codes.Code
	}{
		{"wrong-host", "negative: certificate identity cannot report another host", func(r *pb.CheckInRequest) { r.AgentId = "other" }, codes.PermissionDenied},
		{"wrong-location", "negative: certificate location is authoritative", func(r *pb.CheckInRequest) { r.Location = "other" }, codes.InvalidArgument},
		{"missing-capabilities", "negative: capabilities must be present", func(r *pb.CheckInRequest) { r.Capabilities = nil }, codes.InvalidArgument},
		{"invalid-boot", "negative: boot identity is required", func(r *pb.CheckInRequest) { r.Capabilities.BootId = "" }, codes.InvalidArgument},
		{"long-kernel", "boundary: kernel version is bounded at 256 bytes", func(r *pb.CheckInRequest) { r.Capabilities.KernelRelease = strings.Repeat("a", 257) }, codes.InvalidArgument},
		{"long-algorithm", "boundary: algorithm names fit the 15-byte kernel ABI", func(r *pb.CheckInRequest) { r.Capabilities.CongestionAlgorithms = []string{strings.Repeat("a", 16)} }, codes.InvalidArgument},
		{"nil-record", "corner: a nil application record fails closed", func(r *pb.CheckInRequest) { r.Records = []*pb.ApplicationRecord{nil} }, codes.InvalidArgument},
		{"kernel-bytes", "corner: kernel route bytes must remain local", func(r *pb.CheckInRequest) {
			r.Records = []*pb.ApplicationRecord{{Assignment: &pb.Assignment{ExperimentId: "assignment"}, InstalledRoute: []byte{1}}}
		}, codes.InvalidArgument},
		{"unknown-status", "corner: an apply status from a newer agent is rejected, not defaulted", func(r *pb.CheckInRequest) {
			r.Records = []*pb.ApplicationRecord{{Assignment: &pb.Assignment{ExperimentId: "assignment", Treatment: &pb.Treatment{Id: "cubic", Revision: 1, Destination: "192.0.2.1/32", Settings: &pb.TCPSettings{}}}, Status: pb.ApplyStatus(200)}}
		}, codes.InvalidArgument},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := &pb.CheckInRequest{AgentId: "host", Location: "dc1", Capabilities: &pb.Capabilities{BootId: "boot", CongestionAlgorithms: []string{"cubic"}}}
			tc.change(r)
			_, err := (&Server{}).CheckIn(testIdentity("agent/dc1/host"), r)
			if status.Code(err) != tc.expected {
				t.Fatalf("%s: got %v expected %v", tc.description, err, tc.expected)
			}
		})
	}
}

func TestStatusAuthorization(t *testing.T) {
	for _, tc := range []struct {
		description, method, path, identity string
		expected                            int
	}{
		{"negative: mTLS is mandatory", "GET", "/status", "", 401},
		{"negative: agents cannot inspect fleet status", "GET", "/status", "agent/dc1/host", 403},
		{"negative: status has no mutation endpoint", "POST", "/status", "operator/alice", 405},
		{"negative: unknown paths return not found", "GET", "/unknown", "operator/alice", 404},
	} {
		t.Run(tc.description, func(t *testing.T) {
			w := httptest.NewRecorder()
			(&Server{}).StatusHandler().ServeHTTP(w, operatorHTTP(tc.method, tc.path, tc.identity))
			if w.Code != tc.expected {
				t.Fatalf("got %d expected %d", w.Code, tc.expected)
			}
		})
	}
}

func TestStatusAndDesiredAuthorization(t *testing.T) {
	eachStore(t, func(t *testing.T, st store.Store) {
		now := time.Now().UTC()
		s := testServer(st, &now)
		enroll(t, s, "host")
		for _, tc := range []struct {
			description, identity string
			expected              codes.Code
		}{
			{"positive: enrolled identity can fetch its desired state", "agent/dc1/host", codes.OK},
			{"negative: enrollment location cannot change", "agent/other/host", codes.PermissionDenied},
			{"negative: a different host cannot fetch desired state", "agent/dc1/other", codes.PermissionDenied},
			{"negative: operator identity is not an agent", "operator/alice", codes.PermissionDenied},
		} {
			t.Run(tc.description, func(t *testing.T) {
				got, err := s.GetDesiredState(testIdentity(tc.identity), &pb.GetDesiredStateRequest{AgentId: "host"})
				if status.Code(err) != tc.expected {
					t.Fatalf("got %v expected %v", err, tc.expected)
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
				if path == "/metrics" && !strings.Contains(w.Body.String(), "xtcp_controller_agents_connected 1") {
					t.Fatalf("connected agent missing from metrics: %s", w.Body.String())
				}
			})
		}
		pg, ok := st.(*postgres.Store)
		if !ok {
			return
		}
		if err := pg.Close(); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		s.StatusHandler().ServeHTTP(w, operatorHTTP(http.MethodGet, "/readyz", "operator/alice"))
		if w.Code != 503 {
			t.Fatalf("closed database reported ready: %d", w.Code)
		}
	})
}

func TestWatchLifecycle(t *testing.T) {
	eachStore(t, func(t *testing.T, st store.Store) {
		now := time.Now().UTC()
		s := testServer(st, &now)
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
	})
}
