package agent

import (
	"context"
	"errors"
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type lifecycleClient struct {
	pb.AgentControlServiceClient
	t                *testing.T
	cancel           context.CancelFunc
	fail             bool
	badDesired       bool
	checked, watched bool // inspected only after Run has joined all workers
}

func (c *lifecycleClient) CheckIn(_ context.Context, r *pb.CheckInRequest, _ ...grpc.CallOption) (*pb.CheckInResponse, error) {
	c.checked = true
	if r.AgentId != "host-1" || r.Location != "dc" || r.Capabilities.BootId != "boot-1" || r.Capabilities.ActuatorEnabled {
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
	if c.badDesired {
		c.cancel()
		return &pb.CheckInResponse{Desired: &pb.DesiredState{AgentId: "host-1", Revision: 7, IssuedAt: &timestamppb.Timestamp{Nanos: -1}}}, nil
	}
	return &pb.CheckInResponse{Desired: &pb.DesiredState{AgentId: "host-1", Revision: 7, IssuedAt: timestamppb.Now()}}, nil
}
func (c *lifecycleClient) WatchDesiredState(_ context.Context, r *pb.WatchDesiredStateRequest, _ ...grpc.CallOption) (grpc.ServerStreamingClient[pb.WatchDesiredStateResponse], error) {
	c.watched = true
	if r.AgentId != "host-1" || r.AfterRevision != 7 {
		c.t.Error("watch did not resume from reconciled revision")
	}
	c.cancel()
	return nil, errors.New("stream disconnected")
}

func TestAgentRunCancellation(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		fail, badDesired  bool
		wantWatch         bool
		wantRevision      uint64
	}{
		{"connected", "positive: a successful check-in reconciles and starts the watch", false, false, true, 7},
		{"offline", "negative: an offline controller leaves the journal untouched and never watches", true, false, false, 0},
		{"undecodable", "corner: a desired state that fails to decode is logged, not reconciled", false, true, false, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testAgent(t)
			defer a.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			c := &lifecycleClient{t: t, cancel: cancel, fail: tc.fail, badDesired: tc.badDesired}
			a.client = c
			a.Run(ctx)
			if !c.checked || c.watched != tc.wantWatch {
				t.Fatalf("%s: checked=%t watched=%t", tc.description, c.checked, c.watched)
			}
			if got := a.State().Revision(); got != tc.wantRevision {
				t.Fatalf("%s: revision %d, want %d", tc.description, got, tc.wantRevision)
			}
		})
	}
}

func TestLocalIdentityFailure(t *testing.T) {
	a, err := newLocal(Config{AgentID: "../host", Location: "dc"}, testIdentity)
	if a != nil || err == nil {
		t.Fatalf("invalid identity must prevent construction: %v %v", a, err)
	}
}
