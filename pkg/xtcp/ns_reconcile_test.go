package xtcp

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/randomizedcoder/xtcp2/pkg/nsdiscover"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
)

// TestBackgroundReconcileFrequency: nil config → package default; a configured
// value is honored; 0 means "disable the background ticker".
func TestBackgroundReconcileFrequency(t *testing.T) {
	x := &XTCP{}
	if got := x.backgroundReconcileFrequency(); got != reconcileFrequency {
		t.Errorf("nil config: got %v, want default %v", got, reconcileFrequency)
	}
	x.config = &xtcp_config.XtcpConfig{ReconcileFrequency: durationpb.New(90 * time.Second)}
	if got := x.backgroundReconcileFrequency(); got != 90*time.Second {
		t.Errorf("configured: got %v, want 90s", got)
	}
	x.config = &xtcp_config.XtcpConfig{ReconcileFrequency: durationpb.New(0)}
	if got := x.backgroundReconcileFrequency(); got != 0 {
		t.Errorf("zero: got %v, want 0 (disabled)", got)
	}
}

// TestReconcileBeforePollEnabled: nil config defaults to true (production flag
// default); otherwise the config bool wins.
func TestReconcileBeforePollEnabled(t *testing.T) {
	x := &XTCP{}
	if !x.reconcileBeforePollEnabled() {
		t.Error("nil config should default to true")
	}
	x.config = &xtcp_config.XtcpConfig{ReconcileBeforePoll: true}
	if !x.reconcileBeforePollEnabled() {
		t.Error("true config should enable")
	}
	x.config = &xtcp_config.XtcpConfig{ReconcileBeforePoll: false}
	if x.reconcileBeforePollEnabled() {
		t.Error("false config should disable")
	}
}

// buildProcTreeForXTCP creates p pid dirs each with an ns/net symlink to
// net:[base+i%distinct] under a temp /proc, so the /proc-scan discovery finds
// `distinct` namespaces with inodes base..base+distinct-1.
func buildProcTreeForXTCP(t *testing.T, p, distinct int, base uint64) string {
	t.Helper()
	proc := t.TempDir()
	for i := 0; i < p; i++ {
		pidDir := filepath.Join(proc, strconv.Itoa(1000+i))
		if err := os.MkdirAll(filepath.Join(pidDir, "ns"), 0o755); err != nil {
			t.Fatal(err)
		}
		ino := base + uint64(i%distinct)
		target := "net:[" + strconv.FormatUint(ino, 10) + "]"
		if err := os.Symlink(target, filepath.Join(pidDir, "ns", "net")); err != nil {
			t.Fatal(err)
		}
	}
	return proc
}

func newReconcileTestXTCP(t *testing.T, proc string) *XTCP {
	t.Helper()
	x := newRunFixture(t)
	x.config = &xtcp_config.XtcpConfig{Netlinkers: 0}
	x.Netlinker = func(_ context.Context, _ *sync.WaitGroup, _ *string, _ uint64, _ int, _ uint32) {}
	x.nsScanner = nsdiscover.NewScanner(proc)
	x.nsResolver = nsdiscover.NewResolver(proc, nil)
	t.Cleanup(func() { _ = x.nsScanner.Close() })
	return x
}

// TestReconcile_deletesGoneNamespaces: nsMap holds inodes that /proc no longer
// shows → reconcile deletes them (cancelling their ctx) and returns the count.
func TestReconcile_deletesGoneNamespaces(t *testing.T) {
	x := newReconcileTestXTCP(t, t.TempDir()) // empty /proc → nothing live

	var mu sync.Mutex
	cancels := 0
	for _, ino := range []uint64{4026531900, 4026531901} {
		_, cancel := context.WithCancel(context.Background())
		wrapped := func() { mu.Lock(); cancels++; mu.Unlock(); cancel() }
		name := "gone"
		x.nsMap.Store(ino, netNSitem{inode: ino, name: &name, cancel: wrapped, socketFD: -1})
	}

	dels, stores := x.reconcile(context.Background())
	if dels != 2 || stores != 0 {
		t.Fatalf("reconcile = (dels=%d, stores=%d), want (2, 0)", dels, stores)
	}
	if n := lenSyncMap(x.nsMap); n != 0 {
		t.Fatalf("nsMap len = %d after delete-all, want 0", n)
	}
	mu.Lock()
	defer mu.Unlock()
	if cancels != 2 {
		t.Fatalf("cancels called = %d, want 2", cancels)
	}
}

// TestReconcile_keepsTrackedNamespaces: an inode present in both /proc and nsMap
// is left alone (no delete, no re-add).
func TestReconcile_keepsTrackedNamespaces(t *testing.T) {
	const base = uint64(4026532000)
	proc := buildProcTreeForXTCP(t, 3, 3, base) // inodes base, base+1, base+2
	x := newReconcileTestXTCP(t, proc)

	for i := uint64(0); i < 3; i++ {
		_, cancel := context.WithCancel(context.Background())
		name := "tracked"
		x.nsMap.Store(base+i, netNSitem{inode: base + i, name: &name, cancel: cancel, socketFD: 5})
	}

	dels, stores := x.reconcile(context.Background())
	if dels != 0 || stores != 0 {
		t.Fatalf("reconcile = (dels=%d, stores=%d), want (0, 0) when all tracked", dels, stores)
	}
	if n := lenSyncMap(x.nsMap); n != 3 {
		t.Fatalf("nsMap len = %d, want 3", n)
	}
}

// TestReconcile_addsNewNamespaces: /proc shows namespaces not in nsMap →
// reconcile calls nsAdd for each, reserving an entry keyed by inode. The ctx is
// pre-cancelled so each spawned netNamespaceInstance aborts right after the
// (faked) open; nsAdd still reserves the inode slot synchronously, which is what
// we assert. We join the goroutines (via the "end" counter) before the fake seam
// is restored so they never read it concurrently with the restore.
func TestReconcile_addsNewNamespaces(t *testing.T) {
	const base = uint64(4026532100)
	proc := buildProcTreeForXTCP(t, 4, 2, base) // 2 distinct namespaces
	x := newReconcileTestXTCP(t, proc)

	fake := openAndSetnsSyscallsT{
		open:  func(string, int, uint32) (int, error) { return -1, syscall.ENOENT },
		setns: func(int, int) error { return nil },
		close: func(int) error { return nil },
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // each netNamespaceInstance aborts during init

	var dels, stores int
	withFakeSyscalls(t, fake, func() {
		dels, stores = x.reconcile(ctx)
		// Both aborting goroutines must finish before withFakeSyscalls restores
		// the global open seam (else -race sees a read/write on it).
		waitForNsInstanceEnd(t, x, 2)
	})

	if dels != 0 || stores != 2 {
		t.Fatalf("reconcile = (dels=%d, stores=%d), want (0, 2)", dels, stores)
	}
	for i := uint64(0); i < 2; i++ {
		if _, ok := x.nsMap.Load(base + i); !ok {
			t.Fatalf("reconcile did not reserve inode %d", base+i)
		}
	}
}

// waitForNsInstanceEnd blocks until at least `want` netNamespaceInstance
// goroutines have run to completion (their deferred "end" counter fired).
func waitForNsInstanceEnd(t *testing.T, x *XTCP, want float64) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if testutil.ToFloat64(x.pC.WithLabelValues("netNamespaceInstance", "end", "counter")) >= want {
			return
		}
		select {
		case <-deadline:
			t.Fatalf("netNamespaceInstance end counter did not reach %v", want)
		case <-time.After(5 * time.Millisecond):
		}
	}
}

// TestReconcile_emptyProc: no live namespaces and an empty nsMap → 0/0.
func TestReconcile_emptyProc(t *testing.T) {
	x := newReconcileTestXTCP(t, t.TempDir())
	if dels, stores := x.reconcile(context.Background()); dels != 0 || stores != 0 {
		t.Fatalf("empty reconcile = (%d, %d), want (0, 0)", dels, stores)
	}
}
