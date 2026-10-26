package reconcile

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	jpb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment_journal"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/internal/fixtures"
	"google.golang.org/protobuf/encoding/prototext"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// legacySnapshot is the pre-journal-package format: prototext of the RPC
// LocalState message. It must fail closed, never half-load.
const legacySnapshot = `desired: {
  agent_id: "host-1"
  revision: 1
  issued_at: { seconds: 1700000000 }
}
records: {
  assignment: { experiment_id: "trial-1" }
}
`

func TestJournalRejectsInvalidSnapshots(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		change            func(*jpb.Journal)
		raw               string
		want              string
	}{
		{"valid", "positive: valid snapshot can be reopened", func(*jpb.Journal) {}, "", ""},
		{"bad-desired", "negative: invalid desired state is not recovered", func(j *jpb.Journal) { j.Desired.Revision = 0 }, "", "invalid persisted"},
		{"missing-assignment", "negative: records require an assignment", func(j *jpb.Journal) { j.Records[0].Assignment = nil }, "", "missing assignment"},
		{"bad-id", "negative: projection names cannot contain paths", func(j *jpb.Journal) { j.Records[0].Assignment.ExperimentId = "../escape"; j.Desired = nil }, "", "invalid journal record"},
		{"duplicate", "negative: duplicate records cannot shadow each other", func(j *jpb.Journal) { j.Records = append(j.Records, j.Records[0]) }, "", "duplicate"},
		{"bad-treatment", "negative: invalid treatment cannot be recovered", func(j *jpb.Journal) { j.Records[0].Assignment.Treatment = nil; j.Desired = nil }, "", "invalid journal treatment"},
		{"unknown-mode", "negative: a mode from a newer writer is refused, not zeroed", func(j *jpb.Journal) { j.Records[0].Assignment.Mode = 99; j.Desired = nil }, "", "unknown assignment mode"},
		{"unknown-status", "negative: an apply status from a newer writer is refused", func(j *jpb.Journal) { j.Records[0].Status = 42 }, "", "unknown apply status"},
		{"bad-timestamp", "negative: an out-of-range timestamp is a parse error", func(j *jpb.Journal) { j.UpdatedAt = &timestamppb.Timestamp{Seconds: 1 << 60} }, "", "updated_at"},
		{"bad-identity", "negative: a path-like agent id in the header is refused", func(j *jpb.Journal) { j.AgentId = "../x" }, "", "invalid journal identity"},
		{"legacy-format", "corner: the pre-journal-package LocalState text fails closed", nil, legacySnapshot, "invalid treatment journal"},
		{"garbage", "corner: non-prototext bytes fail closed", nil, "\x00\x01 not text", "invalid treatment journal"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := Journal{Dir: t.TempDir()}
			raw := []byte(tc.raw)
			if tc.change != nil {
				d := fixtures.Desired(time.Unix(1700000000, 0))
				s := &State{AgentID: "host-1", Location: "dc1", BootID: "boot-1", Desired: d, Records: []Record{{ApplicationRecord: treatment.ApplicationRecord{Assignment: d.Assignments[0]}}}}
				p := toJournal(s)
				tc.change(p)
				var err error
				if raw, err = prototext.Marshal(p); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.WriteFile(filepath.Join(j.Dir, SnapshotFile), raw, 0600); err != nil {
				t.Fatal(err)
			}
			called := false
			err := j.WithLock(context.Background(), func(*State, func() error) error { called = true; return nil })
			if tc.want == "" {
				if err != nil || !called {
					t.Fatalf("%s: %v", tc.description, err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) || called {
				t.Fatalf("%s: got %v, callback=%t; want %q without callback", tc.description, err, called, tc.want)
			}
			if _, rerr := ReadSnapshot(j.Dir); rerr == nil || !strings.Contains(rerr.Error(), tc.want) {
				t.Fatalf("%s: lock-free read disagrees: %v", tc.description, rerr)
			}
		})
	}
}

// TestJournalLockWaits is the lock's contract: a free lock is taken at once,
// a held one is waited for, and the wait ends at the context's deadline with
// ErrJournalBusy and nothing read or written.
func TestJournalLockWaits(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		hold              time.Duration // how long a competing holder keeps the lock; 0 is none
		timeout           time.Duration
		wantBusy          bool
	}{
		{"uncontended", "positive: a free lock runs the callback immediately", 0, time.Second, false},
		{"contended-waits", "positive: a lock held 100 ms is waited for within a 1 s budget, then the callback runs", 100 * time.Millisecond, 5 * time.Second, false},
		{"ctx-deadline", "negative: a lock held past a 150 ms budget returns ErrJournalBusy wrapping the deadline, callback never called", 400 * time.Millisecond, 150 * time.Millisecond, true},
		{"zero-budget", "boundary: an already expired context fails busy without touching the journal when the lock is held", 400 * time.Millisecond, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := Journal{Dir: t.TempDir()}
			if err := j.WithLock(context.Background(), func(s *State, save func() error) error {
				s.SuspensionReason = "first writer"
				return save()
			}); err != nil {
				t.Fatal(err)
			}
			held, released := make(chan struct{}), make(chan struct{})
			if tc.hold > 0 {
				go func() {
					defer close(released)
					_ = j.WithLock(context.Background(), func(*State, func() error) error {
						close(held)
						time.Sleep(tc.hold)
						return nil
					})
				}()
				<-held
			} else {
				close(released)
			}
			ctx, cancel := context.WithTimeout(context.Background(), tc.timeout)
			defer cancel()
			called := false
			start := time.Now()
			err := j.WithLock(ctx, func(s *State, save func() error) error {
				called = true
				s.SuspensionReason = "second writer"
				return save()
			})
			waited := time.Since(start)
			<-released
			if tc.wantBusy {
				if !errors.Is(err, ErrJournalBusy) || !errors.Is(err, context.DeadlineExceeded) || called {
					t.Fatalf("%s: err=%v called=%t", tc.description, err, called)
				}
			} else if err != nil || !called {
				t.Fatalf("%s: err=%v called=%t", tc.description, err, called)
			}
			if tc.hold > 0 && !tc.wantBusy && waited < tc.hold/2 {
				t.Fatalf("%s: ran after %v without waiting for the holder", tc.description, waited)
			}
			want := "second writer"
			if tc.wantBusy {
				want = "first writer"
			}
			s, err := ReadSnapshot(j.Dir)
			if err != nil || s.SuspensionReason != want {
				t.Fatalf("%s: snapshot %+v err=%v, want reason %q", tc.description, s, err, want)
			}
		})
	}
}

// The lock-free reader sees the committed snapshot while the lock is held:
// `treatmentctl status` never waits.
func TestJournalLockFreeReadUnderLock(t *testing.T) {
	j := Journal{Dir: t.TempDir()}
	if err := j.WithLock(context.Background(), func(s *State, save func() error) error {
		s.SuspensionReason = "committed"
		return save()
	}); err != nil {
		t.Fatal(err)
	}
	hold, held, released := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(released)
		_ = j.WithLock(context.Background(), func(s *State, save func() error) error {
			close(held)
			<-hold
			return nil
		})
	}()
	<-held
	s, err := ReadSnapshot(j.Dir)
	close(hold)
	<-released
	if err != nil || s.SuspensionReason != "committed" {
		t.Fatalf("lock-free read under lock: %v %+v", err, s)
	}
}

// TestJournalLockHolderDies kills a process holding the lock mid-write: the
// kernel drops its flock, the next holder acquires at once and sweeps the
// temporary file the dead writer left, and the committed snapshot is intact.
func TestJournalLockHolderDies(t *testing.T) {
	dir := t.TempDir()
	j := Journal{Dir: dir}
	if err := j.WithLock(context.Background(), func(s *State, save func() error) error {
		s.SuspensionReason = "committed"
		return save()
	}); err != nil {
		t.Fatal(err)
	}
	// /proc/self/exe re-executes this test binary.
	cmd := exec.Command("/proc/self/exe", "-test.run=^TestJournalLockHolderHelper$")
	cmd.Env = append(os.Environ(), "XTCP_JOURNAL_HOLD_DIR="+dir)
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	line := make([]byte, 5)
	if _, err = io.ReadFull(out, line); err != nil || string(line) != "held\n" {
		_ = cmd.Process.Kill()
		t.Fatalf("holder did not report the lock: %q %v", line, err)
	}
	// While the child holds the lock, a short wait fails busy.
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	err = j.WithLock(ctx, func(*State, func() error) error { t.Fatal("entered under a live holder"); return nil })
	cancel()
	if !errors.Is(err, ErrJournalBusy) {
		t.Fatalf("live holder: %v", err)
	}
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	ctx, cancel = context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err = j.WithLock(ctx, func(s *State, save func() error) error {
		if s.SuspensionReason != "committed" {
			t.Fatalf("dead writer corrupted the snapshot: %+v", s)
		}
		return nil
	}); err != nil {
		t.Fatalf("lock not released by the dead holder: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), pendingPrefix) {
			t.Fatalf("dead writer's temporary file survived: %s", e.Name())
		}
	}
}

// TestJournalLockHolderHelper is the child of TestJournalLockHolderDies. It
// takes the lock, leaves a half-written temporary file as a dying writer
// would, reports, and blocks until killed.
func TestJournalLockHolderHelper(t *testing.T) {
	dir := os.Getenv("XTCP_JOURNAL_HOLD_DIR")
	if dir == "" {
		t.Skip("helper for TestJournalLockHolderDies")
	}
	err := Journal{Dir: dir}.WithLock(context.Background(), func(*State, func() error) error {
		f, err := os.CreateTemp(dir, pendingPrefix)
		if err != nil {
			return err
		}
		if _, err = f.WriteString("partial"); err != nil {
			return err
		}
		if err = f.Close(); err != nil {
			return err
		}
		fmt.Println("held")
		select {}
	})
	t.Fatal(err)
}

func TestJournalSymlinkProtection(t *testing.T) {
	for _, name := range []string{".lock", SnapshotFile} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(t.TempDir(), "unrelated")
			if err := os.WriteFile(target, []byte("sentinel"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(dir, name)); err != nil {
				t.Fatal(err)
			}
			called := false
			err := (Journal{Dir: dir}).WithLock(context.Background(), func(*State, func() error) error { called = true; return nil })
			if err == nil || called {
				t.Fatal("followed journal symlink")
			}
			if name == SnapshotFile {
				if _, err = ReadSnapshot(dir); err == nil {
					t.Fatal("lock-free reader followed symlink")
				}
			}
			raw, err := os.ReadFile(target)
			if err != nil || string(raw) != "sentinel" {
				t.Fatal("modified symlink target")
			}
		})
	}
}

func TestJournalSizeLimit(t *testing.T) {
	dir := t.TempDir()
	f, err := os.Create(filepath.Join(dir, SnapshotFile))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(treatment.MaxStateBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	err = (Journal{Dir: dir}).WithLock(context.Background(), func(*State, func() error) error { t.Fatal("oversized snapshot reached callback"); return nil })
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("want size limit, got %v", err)
	}
	if _, err = ReadSnapshot(dir); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("lock-free reader ignored size limit: %v", err)
	}
}

func TestJournalProjectionsFollowSnapshot(t *testing.T) {
	j := Journal{Dir: t.TempDir()}
	d := fixtures.Desired(time.Unix(1700000000, 0))
	err := j.WithLock(context.Background(), func(s *State, save func() error) error {
		s.AgentID = "host-1"
		s.Desired = d
		s.Records = []Record{{ApplicationRecord: treatment.ApplicationRecord{Assignment: d.Assignments[0], Status: treatment.ApplyPending}, InstalledRoute: []byte{1, 2, 3}, InstalledBootID: "boot-1"}}
		return save()
	})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(j.Dir, "assignment-trial-1.textproto"))
	if err != nil {
		t.Fatal(err)
	}
	var p jpb.Record
	if err = prototext.Unmarshal(raw, &p); err != nil || p.Assignment.ExperimentId != "trial-1" || string(p.InstalledRoute) != "\x01\x02\x03" || p.InstalledBootId != "boot-1" {
		t.Fatalf("projection does not mirror the record: %v %+v", err, &p)
	}
	if _, err = os.Lstat(filepath.Join(j.Dir, ".pending-")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("temp file left behind")
	}
}
