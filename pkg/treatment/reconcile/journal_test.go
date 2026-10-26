package reconcile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	jpb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_treatment_journal"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/internal/fixtures"
	"golang.org/x/sys/unix"
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
			err := j.WithLock(func(*State, func() error) error { called = true; return nil })
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

func TestJournalConcurrentExclusion(t *testing.T) {
	j := Journal{Dir: t.TempDir()}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- j.WithLock(func(s *State, save func() error) error {
			close(entered)
			<-release
			s.SuspensionReason = "first writer"
			return save()
		})
	}()
	<-entered
	called := false
	err := j.WithLock(func(*State, func() error) error { called = true; return nil })
	close(release)
	if first := <-done; first != nil {
		t.Fatal(first)
	}
	if !errors.Is(err, unix.EWOULDBLOCK) || called {
		t.Fatalf("competing writer should fail busy before callback: %v callback=%t", err, called)
	}
	if err = j.WithLock(func(s *State, _ func() error) error {
		if s.SuspensionReason != "first writer" {
			t.Fatal("lost first write")
		}
		return nil
	}); err != nil {
		t.Fatal("lock not released", err)
	}
	// The lock-free reader sees the committed snapshot while the lock is held.
	hold := make(chan struct{})
	go func() {
		_ = j.WithLock(func(*State, func() error) error { <-hold; return nil })
	}()
	s, err := ReadSnapshot(j.Dir)
	close(hold)
	if err != nil || s.SuspensionReason != "first writer" {
		t.Fatalf("lock-free read under lock: %v %+v", err, s)
	}
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
			err := (Journal{Dir: dir}).WithLock(func(*State, func() error) error { called = true; return nil })
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
	err = (Journal{Dir: dir}).WithLock(func(*State, func() error) error { t.Fatal("oversized snapshot reached callback"); return nil })
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
	err := j.WithLock(func(s *State, save func() error) error {
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
