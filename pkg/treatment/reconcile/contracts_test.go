package reconcile

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/internal/fixtures"
)

type faultBackend struct {
	*fakeBackend
	fail string
}

func (b *faultBackend) BeginReconcile() {}

func (b *faultBackend) Plan(ctx context.Context, t *treatment.Treatment) ([]byte, error) {
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
			r := newReconciler(t, b, &now)
			d := fixtures.Desired(now)
			if tc.name == "plan" {
				if _, err := r.Run(context.Background(), d, ModeFull); err != nil {
					t.Fatal(err)
				}
			}
			b.fail = tc.name
			s, err := r.Run(context.Background(), d, ModeFull)
			if err == nil || !strings.Contains(err.Error(), tc.name+" failed") || s.Records[0].Status != treatment.ApplyFailed || b.enabled || len(b.routes) != tc.wantRoutes {
				t.Fatalf("%s: err=%v enabled=%t routes=%d state=%v", tc.description, err, b.enabled, len(b.routes), s)
			}
			b.fail = ""
			s, err = r.Run(context.Background(), nil, ModeFull)
			if err != nil || !b.enabled || len(b.routes) != 1 || s.Records[0].Status != treatment.ApplyVerified {
				t.Fatalf("retry did not recover: %v %v", s, err)
			}
		})
	}
}

func TestReconcileRevisionContracts(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		change            func(*treatment.DesiredState)
		mode              Mode
		wantError         string
	}{
		{"idempotent", "identical state verifies without reinstall", func(*treatment.DesiredState) {}, ModeFull, ""},
		{"conflict", "same revision cannot carry different contents", func(d *treatment.DesiredState) { d.Suspended = true }, ModeFull, "conflicting"},
		{"wrong-host", "another agent's state is rejected", func(d *treatment.DesiredState) { d.AgentID = "other" }, ModeFull, "another agent"},
		{"watchdog", "watchdog cannot accept controller instructions", func(*treatment.DesiredState) {}, ModeExpiryOnly, "watchdog"},
		{"invalid", "an invalid desired state is rejected before the journal changes", func(d *treatment.DesiredState) { d.Revision = 0 }, ModeFull, "desired-state header"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Unix(1700000000, 0)
			b := &fakeBackend{routes: map[string]bool{}}
			r := newReconciler(t, b, &now)
			d := fixtures.Desired(now)
			s, err := r.Run(context.Background(), d, ModeFull)
			if err != nil {
				t.Fatal(err)
			}
			original := s.Desired.Clone()
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
			if err = r.Journal.WithLock(func(s *State, _ func() error) error {
				if !s.Desired.Equal(original) {
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
		want        treatment.ApplyStatus
		routes      int
	}{
		{"one nanosecond before expiry is active", time.Nanosecond, treatment.ApplyVerified, 1},
		{"exact expiry is withdrawn", 0, treatment.ApplyExpired, 0},
		{"one nanosecond after expiry is withdrawn", -time.Nanosecond, treatment.ApplyExpired, 0},
	} {
		t.Run(tc.description, func(t *testing.T) {
			now := time.Unix(1700000000, 0)
			b := &fakeBackend{routes: map[string]bool{}}
			r := newReconciler(t, b, &now)
			d := fixtures.Desired(now)
			d.Assignments[0].LeaseExpiresAt = now.Add(tc.offset)
			s, err := r.Run(context.Background(), d, ModeFull)
			if err != nil || s.Records[0].Status != tc.want || len(b.routes) != tc.routes {
				t.Fatalf("got %v %v routes=%d; want %v routes=%d", s, err, len(b.routes), tc.want, tc.routes)
			}
			record := s.Records[0]
			if !record.ReceivedAt.Equal(now) {
				t.Fatal("receipt timestamp missing")
			}
			if tc.routes == 1 && !record.AppliedAt.Equal(now) {
				t.Fatal("application timestamp missing")
			}
		})
	}
}

// Reports strips the kernel-local fields, which is how the check-in path
// guarantees route bytes never cross the wire.
func TestReportsStripLocalFields(t *testing.T) {
	now := time.Unix(1700000000, 0)
	b := &fakeBackend{routes: map[string]bool{}}
	r := newReconciler(t, b, &now)
	s, err := r.Run(context.Background(), fixtures.Desired(now), ModeFull)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Records[0].InstalledRoute) == 0 || s.Records[0].InstalledBootID != "boot-1" {
		t.Fatal("fixture did not install a route")
	}
	reports := s.Reports()
	if len(reports) != 1 || reports[0].Status != treatment.ApplyVerified || reports[0].Assignment.ExperimentID != "trial-1" {
		t.Fatalf("report mismatch: %+v", reports)
	}
	reports[0].Assignment.Treatment.ID = "mutated"
	if s.Records[0].Assignment.Treatment.ID == "mutated" {
		t.Fatal("report aliases the snapshot")
	}
	if (*State)(nil).Reports() != nil || (*State)(nil).Held() || (*State)(nil).Revision() != 0 {
		t.Fatal("nil state accessors must be safe")
	}
}
