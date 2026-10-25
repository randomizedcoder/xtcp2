package reconcile

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_controller"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/internal/fixtures"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type faultBackend struct {
	*fakeBackend
	fail string
}

func (b *faultBackend) BeginReconcile() {}

func (b *faultBackend) Plan(ctx context.Context, t *pb.Treatment) ([]byte, error) {
	if b.fail == "plan" {
		return nil, errors.New("plan failed")
	}
	return b.fakeBackend.Plan(ctx, t)
}
func (b *faultBackend) Install(ctx context.Context, raw []byte) error {
	if b.fail == "install" {
		return errors.New("install failed")
	}
	return b.fakeBackend.Install(ctx, raw)
}
func (b *faultBackend) Enable(ctx context.Context) error {
	if b.fail == "enable" {
		return errors.New("enable failed")
	}
	return b.fakeBackend.Enable(ctx)
}

func TestReconcileFailureRecovery(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		wantRoutes        int
	}{
		{"plan", "forwarding changes withdraw an existing override", 0},
		{"install", "failed installation cannot enable lookup", 0},
		{"enable", "failed rule installation isolates installed routes", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Unix(1700000000, 0)
			b := &faultBackend{fakeBackend: &fakeBackend{routes: map[string]bool{}}}
			r := &Reconciler{Journal: Journal{Dir: t.TempDir()}, Backend: b, AgentID: "host-1", BootID: "boot", Now: func() time.Time { return now }}
			d := fixtures.Desired(now)
			if tc.name == "plan" {
				if _, err := r.Run(context.Background(), d, ModeFull); err != nil {
					t.Fatal(err)
				}
			}
			b.fail = tc.name
			s, err := r.Run(context.Background(), d, ModeFull)
			if err == nil || !strings.Contains(err.Error(), tc.name+" failed") || s.Records[0].Status != pb.ApplyStatus_APPLY_STATUS_FAILED || b.enabled || len(b.routes) != tc.wantRoutes {
				t.Fatalf("%s: err=%v enabled=%t routes=%d state=%v", tc.description, err, b.enabled, len(b.routes), s)
			}
			b.fail = ""
			s, err = r.Run(context.Background(), nil, ModeFull)
			if err != nil || !b.enabled || len(b.routes) != 1 || s.Records[0].Status != pb.ApplyStatus_APPLY_STATUS_VERIFIED {
				t.Fatalf("retry did not recover: %v %v", s, err)
			}
		})
	}
}

func TestReconcileRevisionContracts(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		change            func(*pb.DesiredState)
		mode              Mode
		wantError         string
	}{
		{"idempotent", "identical state verifies without reinstall", func(*pb.DesiredState) {}, ModeFull, ""},
		{"conflict", "same revision cannot carry different contents", func(d *pb.DesiredState) { d.Suspended = true }, ModeFull, "conflicting"},
		{"wrong-host", "another agent's state is rejected", func(d *pb.DesiredState) { d.AgentId = "other" }, ModeFull, "another agent"},
		{"watchdog", "watchdog cannot accept controller instructions", func(*pb.DesiredState) {}, ModeExpiryOnly, "watchdog"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Unix(1700000000, 0)
			b := &fakeBackend{routes: map[string]bool{}}
			r := &Reconciler{Journal: Journal{Dir: t.TempDir()}, Backend: b, AgentID: "host-1", BootID: "boot", Now: func() time.Time { return now }}
			d := fixtures.Desired(now)
			s, err := r.Run(context.Background(), d, ModeFull)
			if err != nil {
				t.Fatal(err)
			}
			original := proto.Clone(s.Desired)
			tc.change(d)
			_, err = r.Run(context.Background(), d, tc.mode)
			if tc.wantError == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("%s: expected %q got %v", tc.description, tc.wantError, err)
			}
			if b.installs != 1 {
				t.Fatal("rejected/idempotent state reinstalled route")
			}
			if err = r.Journal.WithLock(func(s *pb.LocalState, _ func() error) error {
				if !proto.Equal(s.Desired, original) {
					t.Fatal("rejected request changed journal")
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReconcileLeaseBoundary(t *testing.T) {
	for _, tc := range []struct {
		description string
		offset      time.Duration
		want        pb.ApplyStatus
		routes      int
	}{
		{"one nanosecond before expiry is active", time.Nanosecond, pb.ApplyStatus_APPLY_STATUS_VERIFIED, 1},
		{"exact expiry is withdrawn", 0, pb.ApplyStatus_APPLY_STATUS_EXPIRED, 0},
		{"one nanosecond after expiry is withdrawn", -time.Nanosecond, pb.ApplyStatus_APPLY_STATUS_EXPIRED, 0},
	} {
		t.Run(tc.description, func(t *testing.T) {
			now := time.Unix(1700000000, 0)
			b := &fakeBackend{routes: map[string]bool{}}
			r := &Reconciler{Journal: Journal{Dir: t.TempDir()}, Backend: b, AgentID: "host-1", BootID: "boot", Now: func() time.Time { return now }}
			d := fixtures.Desired(now)
			d.Assignments[0].LeaseExpiresAt = timestamppb.New(now.Add(tc.offset))
			s, err := r.Run(context.Background(), d, ModeFull)
			if err != nil || s.Records[0].Status != tc.want || len(b.routes) != tc.routes {
				t.Fatalf("got %v %v routes=%d; want %v routes=%d", s, err, len(b.routes), tc.want, tc.routes)
			}
			record := s.Records[0]
			if !record.ReceivedAt.AsTime().Equal(now) {
				t.Fatal("receipt timestamp missing")
			}
			if tc.routes == 1 && !record.AppliedAt.AsTime().Equal(now) {
				t.Fatal("application timestamp missing")
			}
		})
	}
}
