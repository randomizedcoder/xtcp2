package reconcile

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/internal/fixtures"
)

func ptr[T any](v T) *T { return &v }

type fakeBackend struct {
	routes     map[string]bool
	enabled    bool
	failVerify bool
	failRemove bool
	installs   int
	onInstall  func()
}

func (f *fakeBackend) BeginReconcile() {}

func (f *fakeBackend) Plan(_ context.Context, t *treatment.Treatment) ([]byte, error) {
	return []byte(t.Destination.String() + ":" + t.Settings.Algorithm()), nil
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
func (f *fakeBackend) Enable(context.Context) error  { f.enabled = true; return nil }
func (f *fakeBackend) Disable(context.Context) error { f.enabled = false; return nil }

func newReconciler(t *testing.T, b Backend, now *time.Time) *Reconciler {
	t.Helper()
	r := &Reconciler{Journal: Journal{Dir: t.TempDir()}, Backend: b, AgentID: "host-1", Location: "dc1", BootID: "boot-1"}
	if now != nil {
		r.Now = func() time.Time { return *now }
	}
	return r
}

func TestFailedWithdrawalDisablesOwnedLookup(t *testing.T) {
	now := time.Now()
	b := &fakeBackend{routes: map[string]bool{}}
	r := newReconciler(t, b, &now)
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
	r := newReconciler(t, b, &now)
	d := fixtures.Desired(now)
	d.Assignments[0].Mode = treatment.ModeStable
	d.Assignments[0].LeaseExpiresAt = time.Time{}
	if _, e := r.Run(context.Background(), d, ModeFull); e != nil {
		t.Fatal(e)
	}
	now = now.Add(48 * time.Hour)
	r.BootID = "boot-2"
	b.routes = map[string]bool{}
	b.enabled = false
	s, e := r.Run(context.Background(), nil, ModeFull)
	if e != nil || len(b.routes) != 1 || !b.enabled || s.Records[0].InstalledBootID != "boot-2" || s.BootID != "boot-2" {
		t.Fatal("offline reboot did not restore stable treatment", e)
	}
}

func TestJournalIntentPrecedesKernelAndCrashRecovery(t *testing.T) {
	now := time.Now()
	b := &fakeBackend{routes: map[string]bool{}, failVerify: true}
	r := newReconciler(t, b, &now)
	dir := r.Journal.Dir
	b.onInstall = func() {
		raw, e := os.ReadFile(filepath.Join(dir, SnapshotFile))
		if e != nil || len(raw) == 0 {
			t.Fatal("intent not durable before install", e)
		}
	}
	s, e := r.Run(context.Background(), fixtures.Desired(now), ModeFull)
	if e == nil || s.Records[0].Status != treatment.ApplyFailed {
		t.Fatal("must report readback failure", s, e)
	}
	// The kernel changed before success was persisted. A fresh reconciler uses
	// the journal to recover without needing a controller or another command.
	b.failVerify = false
	r2 := *r
	s, e = r2.Run(context.Background(), nil, ModeFull)
	if e != nil || s.Records[0].Status != treatment.ApplyVerified || len(b.routes) != 1 {
		t.Fatal("restart did not converge", s, e)
	}
}

func TestTrialExpiryCannotBeResurrectedByRenewalOrRestart(t *testing.T) {
	now := time.Now()
	b := &fakeBackend{routes: map[string]bool{}}
	r := newReconciler(t, b, &now)
	d := fixtures.Desired(now)
	if _, e := r.Run(context.Background(), d, ModeFull); e != nil {
		t.Fatal(e)
	}
	now = now.Add(61 * time.Minute)
	s, e := r.Run(context.Background(), nil, ModeExpiryOnly)
	if e != nil || len(b.routes) != 0 || s.Records[0].Status != treatment.ApplyExpired {
		t.Fatal("watchdog expiry", s, e)
	}
	d.Revision++
	d.Assignments[0].LeaseExpiresAt = now.Add(time.Hour)
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
	r := newReconciler(t, b, nil)
	d := fixtures.Desired(now)
	d.Assignments[0].Mode = treatment.ModeStable
	if _, e := r.Run(context.Background(), d, ModeFull); e != nil {
		t.Fatal(e)
	}
	if e := r.SuspendLocal(context.Background(), true, "incident test"); e != nil {
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
	if e := r.SuspendLocal(context.Background(), false, "test complete"); e != nil {
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
	if e := os.WriteFile(filepath.Join(dir, SnapshotFile), []byte("invalid!!"), 0600); e != nil {
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
	r := newReconciler(t, b, &now)
	d := fixtures.Desired(now)
	a := &d.Assignments[0]
	a.ExperimentID = "state"
	a.Generation = 2
	pred := a.Treatment
	pred.Settings.CongestionControl = ptr("bbr")
	a.Predecessor = &pred
	if _, e := r.Run(context.Background(), d, ModeFull); e != nil {
		t.Fatal(e)
	}
	now = now.Add(61 * time.Minute)
	s, e := r.Run(context.Background(), nil, ModeExpiryOnly)
	if e != nil || !b.routes["192.0.2.1/32:bbr"] || s.Records[0].Status != treatment.ApplyExpired {
		t.Fatal("predecessor not restored", e)
	}
	installs := b.installs
	if _, e = r.Run(context.Background(), nil, ModeExpiryOnly); e != nil {
		t.Fatal(e)
	}
	if b.installs != installs {
		t.Fatal("watchdog repeatedly reinstalled predecessor")
	}
	bad := d.Clone()
	bad.Revision++
	bad.Assignments[0].Generation--
	if _, e = r.Run(context.Background(), bad, ModeFull); e == nil {
		t.Fatal("stale generation accepted")
	}
	if e = r.Journal.WithLock(context.Background(), func(s *State, _ func() error) error {
		if s.Desired.Revision != d.Revision {
			t.Fatal("rejected generation poisoned persisted desired state")
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	d.Revision++
	a.Mode = treatment.ModeRetired
	if _, e = r.Run(context.Background(), d, ModeFull); e != nil {
		t.Fatal(e)
	}
	if len(b.routes) != 0 {
		t.Fatal("retired assignment restored predecessor")
	}
}

func TestJournalHeaderIdentifiesWriter(t *testing.T) {
	now := time.Unix(1700000000, 0)
	b := &fakeBackend{routes: map[string]bool{}}
	r := newReconciler(t, b, &now)
	s, e := r.Run(context.Background(), fixtures.Desired(now), ModeFull)
	if e != nil || s.AgentID != "host-1" || s.Location != "dc1" || s.BootID != "boot-1" || !s.UpdatedAt.Equal(now) {
		t.Fatalf("header not written: %+v %v", s, e)
	}
	other := *r
	other.AgentID = "host-2"
	if _, e = other.Run(context.Background(), nil, ModeFull); e == nil || !strings.Contains(e.Error(), "another agent") {
		t.Fatalf("foreign journal accepted: %v", e)
	}
	if e = other.SuspendLocal(context.Background(), true, "x"); e == nil || !strings.Contains(e.Error(), "another agent") {
		t.Fatalf("foreign journal suspended: %v", e)
	}
	if b.installs != 1 {
		t.Fatal("foreign reconciler touched the kernel")
	}
}

// TestJournalCollectionIDOwnership: the agent's reconciler writes its nonce
// into the header; a reconciler without one (the watchdog) keeps whatever
// the journal holds, so only the owning agent process changes it.
func TestJournalCollectionIDOwnership(t *testing.T) {
	const other = "fedcba9876543210fedcba9876543210"
	for _, tc := range []struct {
		name, description string
		first, second     string // CollectionID of the first and second writer
		expected          string
	}{
		{"agent-writes", "positive: the agent's nonce lands in the header", fixtures.CollectionID, fixtures.CollectionID, fixtures.CollectionID},
		{"watchdog-preserves", "negative: a watchdog pass leaves the agent's nonce alone", fixtures.CollectionID, "", fixtures.CollectionID},
		{"agent-restart", "corner: a restarted agent replaces the nonce", fixtures.CollectionID, other, other},
		{"never-set", "boundary: a journal only the watchdog wrote has no nonce", "", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Unix(1700000000, 0)
			b := &fakeBackend{routes: map[string]bool{}}
			r := newReconciler(t, b, &now)
			r.CollectionID = tc.first
			if _, e := r.Run(context.Background(), fixtures.Desired(now), ModeFull); e != nil {
				t.Fatal(e)
			}
			second := *r
			second.CollectionID = tc.second
			if _, e := second.Run(context.Background(), nil, ModeExpiryOnly); e != nil {
				t.Fatal(e)
			}
			s, e := ReadSnapshot(r.Journal.Dir)
			if e != nil {
				t.Fatal(e)
			}
			if s.CollectionID != tc.expected {
				t.Fatalf("%s: header nonce %q, want %q", tc.description, s.CollectionID, tc.expected)
			}
		})
	}
}
