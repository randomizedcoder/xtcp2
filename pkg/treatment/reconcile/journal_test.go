package reconcile

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_controller"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/internal/fixtures"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/encoding/prototext"
)

func TestJournalRejectsInvalidSnapshots(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		change            func(*pb.LocalState)
		want              string
	}{
		{"valid", "valid snapshot can be reopened", func(*pb.LocalState) {}, ""},
		{"bad-desired", "invalid desired state is not recovered", func(s *pb.LocalState) { s.Desired.Revision = 0 }, "invalid persisted"},
		{"missing-assignment", "records require an assignment", func(s *pb.LocalState) { s.Records[0].Assignment = nil }, "invalid journal record"},
		{"bad-id", "projection names cannot contain paths", func(s *pb.LocalState) { s.Records[0].Assignment.Id = "../escape"; s.Desired = nil }, "invalid journal record"},
		{"duplicate", "duplicate records cannot shadow each other", func(s *pb.LocalState) { s.Records = append(s.Records, s.Records[0]) }, "duplicate"},
		{"bad-treatment", "invalid treatment cannot be recovered", func(s *pb.LocalState) { s.Records[0].Assignment.Treatment = nil; s.Desired = nil }, "invalid journal treatment"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			j := Journal{Dir: t.TempDir()}
			d := fixtures.Desired(time.Unix(1700000000, 0))
			s := &pb.LocalState{Desired: d, Records: []*pb.ApplicationRecord{{Assignment: d.Assignments[0]}}}
			tc.change(s)
			raw, err := prototext.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(j.Dir, "state.textproto"), raw, 0600); err != nil {
				t.Fatal(err)
			}
			called := false
			err = j.WithLock(func(*pb.LocalState, func() error) error { called = true; return nil })
			if tc.want == "" {
				if err != nil || !called {
					t.Fatalf("%s: %v", tc.description, err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) || called {
				t.Fatalf("%s: got %v, callback=%t; want %q without callback", tc.description, err, called, tc.want)
			}
		})
	}
}

func TestJournalConcurrentExclusion(t *testing.T) {
	j := Journal{Dir: t.TempDir()}
	entered, release, done := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		done <- j.WithLock(func(s *pb.LocalState, save func() error) error {
			close(entered)
			<-release
			s.SuspensionReason = "first writer"
			return save()
		})
	}()
	<-entered
	called := false
	err := j.WithLock(func(*pb.LocalState, func() error) error { called = true; return nil })
	close(release)
	if first := <-done; first != nil {
		t.Fatal(first)
	}
	if !errors.Is(err, unix.EWOULDBLOCK) || called {
		t.Fatalf("competing writer should fail busy before callback: %v callback=%t", err, called)
	}
	if err = j.WithLock(func(s *pb.LocalState, _ func() error) error {
		if s.SuspensionReason != "first writer" {
			t.Fatal("lost first write")
		}
		return nil
	}); err != nil {
		t.Fatal("lock not released", err)
	}
}

func TestJournalSymlinkProtection(t *testing.T) {
	for _, name := range []string{".lock", "state.textproto"} {
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
			err := (Journal{Dir: dir}).WithLock(func(*pb.LocalState, func() error) error { called = true; return nil })
			if err == nil || called {
				t.Fatal("followed journal symlink")
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
	f, err := os.Create(filepath.Join(dir, "state.textproto"))
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(treatment.MaxStateBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err = f.Close(); err != nil {
		t.Fatal(err)
	}
	err = (Journal{Dir: dir}).WithLock(func(*pb.LocalState, func() error) error { t.Fatal("oversized snapshot reached callback"); return nil })
	if err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("want size limit, got %v", err)
	}
}
