package attribution

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	flat "github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/internal/fixtures"
	"github.com/randomizedcoder/xtcp2/pkg/treatment/reconcile"
)

// writeJournal commits a snapshot the way the agent does: under the lock,
// by atomic rename.
func writeJournal(t *testing.T, dir string, s *reconcile.State) {
	t.Helper()
	err := reconcile.Journal{Dir: dir}.WithLock(func(cur *reconcile.State, save func() error) error {
		*cur = *s
		return save()
	})
	if err != nil {
		t.Fatal(err)
	}
}

func verifiedState(revision uint64) *reconcile.State {
	now := time.Unix(1700000000, 0)
	d := fixtures.Desired(now)
	d.Revision = revision
	d.Assignments[0].Mode = treatment.ModeStable
	d.Assignments[0].LeaseExpiresAt = time.Time{}
	return &reconcile.State{AgentID: "host-1", Location: "dc1", BootID: "boot-1", Desired: d, Records: []reconcile.Record{{ApplicationRecord: treatment.ApplicationRecord{Assignment: d.Assignments[0], Status: treatment.ApplyVerified, AppliedAt: now, DesiredRevision: revision}, InstalledRoute: []byte{1}, InstalledBootID: "boot-1"}}}
}

func record() *flat.XtcpFlatRecord {
	return &flat.XtcpFlatRecord{Location: "dc1", NetnsInode: 7, InetDiagMsgFamily: 2, InetDiagMsgSocketDestination: []byte{192, 0, 2, 1}, InetDiagCong: "cubic"}
}

type logSink struct {
	mu    sync.Mutex
	lines []string
}

func (l *logSink) printf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.lines = append(l.lines, fmt.Sprintf(format, args...))
}

func (l *logSink) count(sub string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, v := range l.lines {
		if strings.Contains(v, sub) {
			n++
		}
	}
	return n
}

func eventually(t *testing.T, what string, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s: condition not met within 5s", what)
}

func TestWatcherRefreshContracts(t *testing.T) {
	for _, tc := range []struct {
		name, description string
		prepare           func(t *testing.T, dir string)
		wantErr           string
		wantAgent         string
		wantAssignment    string
		wantLogged        string
		daemonLocation    string
		wantLocation      string
	}{
		{"journal", "positive: a committed journal attributes records with the journal's agent and the daemon's location", func(t *testing.T, dir string) { writeJournal(t, dir, verifiedState(3)) }, "", "host-1", "trial-1", "", "dc1", "dc1"},
		{"missing-file", "negative: no snapshot yet means identity is not invented", func(*testing.T, string) {}, "", "", "", "", "dc1", "dc1"},
		{"missing-dir", "negative: a directory that does not exist yet is not an error", func(t *testing.T, dir string) { os.RemoveAll(dir) }, "", "", "", "", "dc1", "dc1"},
		{"corrupt", "negative: an invalid snapshot clears attribution and is logged", func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, reconcile.SnapshotFile), []byte("nonsense {"), 0600); err != nil {
				t.Fatal(err)
			}
		}, "invalid treatment journal", "", "", "not attributed", "dc1", "dc1"},
		{"held", "boundary: a journal under a hold stamps identity but no treatment", func(t *testing.T, dir string) {
			s := verifiedState(4)
			s.LocallySuspended = true
			writeJournal(t, dir, s)
		}, "", "host-1", "", "", "dc1", "dc1"},
		{"location-mismatch", "corner: a journal from another location keeps the daemon's label and warns once", func(t *testing.T, dir string) {
			s := verifiedState(5)
			s.Location = "elsewhere"
			writeJournal(t, dir, s)
		}, "", "host-1", "trial-1", "differs from this daemon", "dc1", "dc1"},
		{"adopt-location", "corner: a daemon without its own -location takes the journal's so evidence queries can find it", func(t *testing.T, dir string) {
			s := verifiedState(6)
			s.Location = "journal-dc"
			writeJournal(t, dir, s)
		}, "", "host-1", "trial-1", "records take the journal's", "", "journal-dc"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			tc.prepare(t, dir)
			var sink logSink
			var st Stamper
			w, err := NewWatcher(WatchConfig{Dir: dir, Location: tc.daemonLocation, NetnsInode: 7, Log: sink.printf}, &st)
			if err != nil {
				t.Fatal(err)
			}
			err = w.Refresh(true)
			if tc.wantErr == "" && err != nil || tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)) {
				t.Fatalf("%s: refresh error %v, want %q", tc.description, err, tc.wantErr)
			}
			r := record()
			st.Stamp(r)
			if r.AgentId != tc.wantAgent || r.TreatmentAssignmentId != tc.wantAssignment || r.Location != tc.wantLocation {
				t.Fatalf("%s: agent=%q assignment=%q location=%q", tc.description, r.AgentId, r.TreatmentAssignmentId, r.Location)
			}
			if tc.wantAgent != "" && (r.HostBootId != "boot-1" || len(r.CollectionId) != 32) {
				t.Fatalf("%s: boot/collection not stamped: %+v", tc.description, r)
			}
			// A second refresh logs nothing new: errors and warnings are once per cause.
			_ = w.Refresh(true)
			if tc.wantLogged != "" && sink.count(tc.wantLogged) != 1 {
				t.Fatalf("%s: expected one log line containing %q, got %v", tc.description, tc.wantLogged, sink.lines)
			}
			if tc.wantLogged == "" && sink.count("not attributed")+sink.count("differs") != 0 {
				t.Fatalf("%s: unexpected log lines %v", tc.description, sink.lines)
			}
		})
	}
	if _, err := NewWatcher(WatchConfig{}, nil); err == nil {
		t.Fatal("empty configuration accepted")
	}
}

func TestWatcherFollowsRenames(t *testing.T) {
	dir := t.TempDir()
	writeJournal(t, dir, verifiedState(1))
	var st Stamper
	w, err := NewWatcher(WatchConfig{Dir: dir, NetnsInode: 7, Poll: time.Hour, Log: func(string, ...any) {}}, &st)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); w.Run(ctx) }()
	revision := func() uint64 {
		r := record()
		st.Stamp(r)
		return r.TreatmentStateRevision
	}
	eventually(t, "initial load", func() bool { return revision() == 1 })
	// Atomic rename of a new snapshot: the inotify path, with the hour-long
	// poll ruling out the fallback.
	writeJournal(t, dir, verifiedState(2))
	eventually(t, "rename picked up", func() bool { return revision() == 2 })
	// Removal clears attribution.
	if err := os.Remove(filepath.Join(dir, reconcile.SnapshotFile)); err != nil {
		t.Fatal(err)
	}
	eventually(t, "removal picked up", func() bool { return st.Current() == nil })
	// Re-creation restores it.
	writeJournal(t, dir, verifiedState(3))
	eventually(t, "re-creation picked up", func() bool { return revision() == 3 })
	// A change that leaves size and mtime identical is still visible on Wake.
	w.Wake()
	eventually(t, "wake refreshes", func() bool { return revision() == 3 })
	cancel()
	<-done
}

func TestWatcherPollsWhenDirectoryAppearsLater(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "routes")
	var st Stamper
	w, err := NewWatcher(WatchConfig{Dir: dir, NetnsInode: 7, Poll: 20 * time.Millisecond, Log: func(string, ...any) {}}, &st)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { defer close(done); w.Run(ctx) }()
	time.Sleep(50 * time.Millisecond)
	if st.Current() != nil {
		t.Fatal("index published before any journal existed")
	}
	writeJournal(t, dir, verifiedState(9))
	eventually(t, "late directory", func() bool {
		r := record()
		st.Stamp(r)
		return r.TreatmentStateRevision == 9 && r.AgentId == "host-1"
	})
	// The directory now exists, so the watch is added on the next poll and
	// a rename is seen even if polling were slow.
	writeJournal(t, dir, verifiedState(10))
	eventually(t, "post-attach rename", func() bool {
		r := record()
		st.Stamp(r)
		return r.TreatmentStateRevision == 10
	})
	cancel()
	<-done
}
