package reconcile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/internal/fixtures"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type fakeBackend struct {
	routes     map[string]bool
	enabled    bool
	failVerify bool
	failRemove bool
	installs   int
	onInstall  func()
}

func (f *fakeBackend) BeginReconcile() {}

func (f *fakeBackend) Plan(_ context.Context, t *pb.Treatment) ([]byte, error) {
	return []byte(t.Destination + ":" + t.Settings.GetCongestionControl()), nil
}
func (f *fakeBackend) Install(_ context.Context, b []byte) error {
	f.installs++
	if f.onInstall != nil {
		f.onInstall()
	}
	f.routes[string(b)] = true
	return nil
}
func (f *fakeBackend) Verify(_ context.Context, b []byte) error {
	if f.failVerify || !f.routes[string(b)] {
		return errors.New("readback failed")
	}
	return nil
}
func (f *fakeBackend) Remove(_ context.Context, b []byte) error {
	if f.failRemove {
		return errors.New("withdrawal failed")
	}
	delete(f.routes, string(b))
	return nil
}

func TestFailedWithdrawalDisablesOwnedLookup(t *testing.T) {
	now := time.Now()
	b := &fakeBackend{routes: map[string]bool{}}
	r := &Reconciler{Journal: Journal{Dir: t.TempDir()}, Backend: b, AgentID: "host-1", BootID: "boot-1", Now: func() time.Time { return now }}
	if _, e := r.Run(context.Background(), fixtures.Desired(now), ModeFull); e != nil {
		t.Fatal(e)
	}
	if !b.enabled {
		t.Fatal("lookup never enabled")
	}
	now = now.Add(61 * time.Minute)
	b.failRemove = true
	if _, e := r.Run(context.Background(), nil, ModeExpiryOnly); e == nil {
		t.Fatal("withdrawal failure hidden")
	}
	if b.enabled {
		t.Fatal("expired route still selected after failed withdrawal")
	}
	b.failRemove = false
	if _, e := r.Run(context.Background(), nil, ModeFull); e != nil {
		t.Fatal(e)
	}
	if len(b.routes) != 0 || b.enabled {
		t.Fatal("failed withdrawal did not recover")
	}
}

func TestStableRouteSurvivesOfflineReboot(t *testing.T) {
	now := time.Now()
	b := &fakeBackend{routes: map[string]bool{}}
	r := &Reconciler{Journal: Journal{Dir: t.TempDir()}, Backend: b, AgentID: "host-1", BootID: "boot-1", Now: func() time.Time { return now }}
	d := fixtures.Desired(now)
	d.Assignments[0].Mode = pb.AssignmentMode_ASSIGNMENT_MODE_STABLE
	d.Assignments[0].LeaseExpiresAt = nil
	if _, e := r.Run(context.Background(), d, ModeFull); e != nil {
		t.Fatal(e)
	}
	now = now.Add(48 * time.Hour)
	r.BootID = "boot-2"
	b.routes = map[string]bool{}
	b.enabled = false
	s, e := r.Run(context.Background(), nil, ModeFull)
	if e != nil || len(b.routes) != 1 || !b.enabled || s.Records[0].InstalledBootId != "boot-2" {
		t.Fatal("offline reboot did not restore stable treatment", e)
	}
}
func (f *fakeBackend) Enable(context.Context) error  { f.enabled = true; return nil }
func (f *fakeBackend) Disable(context.Context) error { f.enabled = false; return nil }

func TestJournalIntentPrecedesKernelAndCrashRecovery(t *testing.T) {
	now := time.Now()
	dir := t.TempDir()
	b := &fakeBackend{routes: map[string]bool{}, failVerify: true}
	r := &Reconciler{Journal: Journal{Dir: dir}, Backend: b, AgentID: "host-1", BootID: "boot-1", Now: func() time.Time { return now }}
	b.onInstall = func() {
		raw, e := os.ReadFile(filepath.Join(dir, "state.textproto"))
		if e != nil || len(raw) == 0 {
			t.Fatal("intent not durable before install", e)
		}
	}
	s, e := r.Run(context.Background(), fixtures.Desired(now), ModeFull)
	if e == nil || s.Records[0].Status != pb.ApplyStatus_APPLY_STATUS_FAILED {
		t.Fatal("must report readback failure", s, e)
	}
	// The kernel changed before success was persisted. A fresh reconciler uses
	// the journal to recover without needing a controller or another command.
	b.failVerify = false
	r2 := *r
	s, e = r2.Run(context.Background(), nil, ModeFull)
	if e != nil || s.Records[0].Status != pb.ApplyStatus_APPLY_STATUS_VERIFIED || len(b.routes) != 1 {
		t.Fatal("restart did not converge", s, e)
	}
}

func TestTrialExpiryCannotBeResurrectedByRenewalOrRestart(t *testing.T) {
	now := time.Now()
	b := &fakeBackend{routes: map[string]bool{}}
	r := &Reconciler{Journal: Journal{Dir: t.TempDir()}, Backend: b, AgentID: "host-1", BootID: "boot-1", Now: func() time.Time { return now }}
	d := fixtures.Desired(now)
	if _, e := r.Run(context.Background(), d, ModeFull); e != nil {
		t.Fatal(e)
	}
	now = now.Add(61 * time.Minute)
	s, e := r.Run(context.Background(), nil, ModeExpiryOnly)
	if e != nil || len(b.routes) != 0 || s.Records[0].Status != pb.ApplyStatus_APPLY_STATUS_EXPIRED {
		t.Fatal("watchdog expiry", s, e)
	}
	d.Revision++
	d.Assignments[0].LeaseExpiresAt = timestamppb.New(now.Add(time.Hour))
	if _, e = r.Run(context.Background(), d, ModeFull); e != nil {
		t.Fatal(e)
	}
	if len(b.routes) != 0 {
		t.Fatal("lease renewal resurrected expired generation")
	}
	d.Revision++
	d.Assignments[0].Generation++
	if _, e = r.Run(context.Background(), d, ModeFull); e != nil {
		t.Fatal(e)
	}
	if len(b.routes) != 1 {
		t.Fatal("explicit approved generation did not activate")
	}
}

func TestPersistentHoldAndTombstones(t *testing.T) {
	now := time.Now()
	b := &fakeBackend{routes: map[string]bool{}}
	r := &Reconciler{Journal: Journal{Dir: t.TempDir()}, Backend: b, AgentID: "host-1", BootID: "boot-1"}
	d := fixtures.Desired(now)
	d.Assignments[0].Mode = pb.AssignmentMode_ASSIGNMENT_MODE_STABLE
	if _, e := r.Run(context.Background(), d, ModeFull); e != nil {
		t.Fatal(e)
	}
	if e := r.SuspendLocal(true, "incident test"); e != nil {
		t.Fatal(e)
	}
	if _, e := r.Run(context.Background(), nil, ModeFull); e != nil {
		t.Fatal(e)
	}
	if b.enabled || len(b.routes) != 0 {
		t.Fatal("hold did not restore baseline")
	}
	d.Revision++
	if _, e := r.Run(context.Background(), d, ModeFull); e != nil {
		t.Fatal(e)
	}
	if len(b.routes) != 0 {
		t.Fatal("controller update cleared local hold")
	}
	if e := r.SuspendLocal(false, "test complete"); e != nil {
		t.Fatal(e)
	}
	d.Revision++
	d.Assignments = nil
	if _, e := r.Run(context.Background(), d, ModeFull); e != nil {
		t.Fatal(e)
	}
	if _, e := r.Run(context.Background(), nil, ModeFull); e != nil {
		t.Fatal(e)
	}
	if len(b.routes) != 0 {
		t.Fatal("orphan projection resurrected treatment")
	}
	d.Revision--
	if _, e := r.Run(context.Background(), d, ModeFull); e == nil {
		t.Fatal("accepted stale desired state")
	}
}

func TestCorruptJournalDoesNotInstall(t *testing.T) {
	dir := t.TempDir()
	if e := os.WriteFile(filepath.Join(dir, "state.textproto"), []byte("invalid!!"), 0600); e != nil {
		t.Fatal(e)
	}
	b := &fakeBackend{routes: map[string]bool{}}
	r := &Reconciler{Journal: Journal{Dir: dir}, Backend: b, AgentID: "host-1"}
	if _, e := r.Run(context.Background(), fixtures.Desired(time.Now()), ModeFull); e == nil {
		t.Fatal("corrupt journal overwritten")
	}
	if b.installs != 0 {
		t.Fatal("kernel touched with corrupt state")
	}
}

func TestPredecessorExpiryAndGenerationRejectionPreserveJournal(t *testing.T) {
	now := time.Now()
	b := &fakeBackend{routes: map[string]bool{}}
	r := &Reconciler{Journal: Journal{Dir: t.TempDir()}, Backend: b, AgentID: "host-1", BootID: "boot-1", Now: func() time.Time { return now }}
	d := fixtures.Desired(now)
	a := d.Assignments[0]
	a.ExperimentId = "state"
	a.Generation = 2
	a.Predecessor = proto.Clone(a.Treatment).(*pb.Treatment)
	a.Predecessor.Settings.CongestionControl = proto.String("bbr")
	if _, e := r.Run(context.Background(), d, ModeFull); e != nil {
		t.Fatal(e)
	}
	now = now.Add(61 * time.Minute)
	s, e := r.Run(context.Background(), nil, ModeExpiryOnly)
	if e != nil || !b.routes["192.0.2.1/32:bbr"] || s.Records[0].Status != pb.ApplyStatus_APPLY_STATUS_EXPIRED {
		t.Fatal("predecessor not restored", e)
	}
	installs := b.installs
	if _, e = r.Run(context.Background(), nil, ModeExpiryOnly); e != nil {
		t.Fatal(e)
	}
	if b.installs != installs {
		t.Fatal("watchdog repeatedly reinstalled predecessor")
	}
	bad := proto.Clone(d).(*pb.DesiredState)
	bad.Revision++
	bad.Assignments[0].Generation--
	if _, e = r.Run(context.Background(), bad, ModeFull); e == nil {
		t.Fatal("stale generation accepted")
	}
	if e = r.Journal.WithLock(func(s *pb.LocalState, _ func() error) error {
		if s.Desired.Revision != d.Revision {
			t.Fatal("rejected generation poisoned persisted desired state")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	d.Revision++
	a.Mode = pb.AssignmentMode_ASSIGNMENT_MODE_RETIRED
	if _, e = r.Run(context.Background(), d, ModeFull); e != nil {
		t.Fatal(e)
	}
	if len(b.routes) != 0 {
		t.Fatal("retired assignment restored predecessor")
	}
}
