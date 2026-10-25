package agent

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_controller"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type lifecycleClient struct {
	pb.AgentControlServiceClient
	t                *testing.T
	cancel           context.CancelFunc
	fail             bool
	checked, watched bool // inspected only after Run has joined all workers
}

func (c *lifecycleClient) CheckIn(_ context.Context, r *pb.CheckInRequest, _ ...grpc.CallOption) (*pb.CheckInResponse, error) {
	c.checked = true
	if r.AgentId != "host" || r.Location != "dc" || r.Capabilities.BootId == "" || r.Capabilities.ActuatorEnabled {
		c.t.Error("incorrect check-in identity/capabilities")
	}
	for _, v := range r.Records {
		if len(v.InstalledRoute) != 0 || v.InstalledBootId != "" {
			c.t.Error("local kernel state leaked into RPC")
		}
	}
	if c.fail {
		c.cancel()
		return nil, errors.New("controller offline")
	}
	return &pb.CheckInResponse{Desired: &pb.DesiredState{AgentId: "host", Revision: 7, IssuedAt: timestamppb.Now()}}, nil
}
func (c *lifecycleClient) WatchDesiredState(_ context.Context, r *pb.WatchDesiredStateRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[pb.WatchDesiredStateResponse], error) {
	c.watched = true
	if r.AgentId != "host" || r.AfterRevision != 7 {
		c.t.Error("watch did not resume from reconciled revision")
	}
	c.cancel()
	return nil, errors.New("stream disconnected")
}

func TestLocalAgentContracts(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		change            func(*Config)
		wantError         string
	}{
		{"offline", "offline control does not require TLS or DNS", func(*Config) {}, ""},
		{"identity", "invalid agent identity fails before initialization", func(c *Config) { c.AgentID = "../host" }, "identity"},
		{"bad-prefix", "malformed protected prefixes fail closed", func(c *Config) { c.ProtectedPrefixes = []string{"bad"} }, "ParsePrefix"},
		{"no-exclusions", "application requires management exclusions", func(c *Config) { c.EnableApply = true }, "protected_prefixes"},
		{"protected", "explicit exclusions allow backend construction without writes", func(c *Config) { c.EnableApply = true; c.ProtectedPrefixes = []string{"192.0.2.0/24"} }, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := Config{AgentID: "host", Location: "dc", Directory: t.TempDir(), Table: 10002, RulePriority: 30000, Protocol: 242}
			tc.change(&c)
			a, err := newLocal(c, testLocalIdentity)
			if tc.wantError != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantError) {
					t.Fatalf("%s: got %v want %q", tc.description, err, tc.wantError)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			if a.State() != nil || a.namespace == 0 || len(a.collectionID) != 32 || a.Reconciler.BootID == "" {
				t.Fatal("invalid initial identity/state")
			}
			if !strings.Contains(c.String(), "agent=host") {
				t.Fatal("config diagnostic missing identity")
			}
		})
	}
}

func TestAgentRunCancellation(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "connected"
		if fail {
			name = "offline"
		}
		t.Run(name, func(t *testing.T) {
			a, err := newLocal(Config{AgentID: "host", Location: "dc", Directory: t.TempDir(), Table: 10002, RulePriority: 30000, Protocol: 242}, testLocalIdentity)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			c := &lifecycleClient{t: t, cancel: cancel, fail: fail}
			a.client = c
			a.Run(ctx)
			if !c.checked || c.watched == fail {
				t.Fatalf("check-in/watch lifecycle: checked=%t watched=%t", c.checked, c.watched)
			}
			if !fail && a.State().GetDesired().GetRevision() != 7 {
				t.Fatal("desired state was not reconciled")
			}
		})
	}
}

func testLocalIdentity() (string, uint64, error) { return "boot", 123, nil }

func TestLocalIdentityFailure(t *testing.T) {
	sentinel := errors.New("identity unavailable")
	a, err := newLocal(Config{AgentID: "host", Location: "dc"}, func() (string, uint64, error) { return "", 0, sentinel })
	if a != nil || !errors.Is(err, sentinel) {
		t.Fatalf("identity failure must prevent construction: %v %v", a, err)
	}
}
