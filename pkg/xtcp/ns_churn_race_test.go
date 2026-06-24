//go:build linux

package xtcp

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
)

// TestNsChurn_deleteDuringInit_doesNotLeakThread is the regression test for the
// thread-exhaustion crash found in the 10 h soak: a namespace deleted *while*
// its netNamespaceInstance goroutine is still doing setns/socket init.
//
// The bug: the per-ns cancel was created deep inside netNamespaceInstance
// (after the setns + socket setup). A delete arriving during that window found
// no nsMap entry, so nsDelete never called cancel(); the instance then blocked
// forever on <-nsCtx.Done() holding a locked OS thread. Under churn these
// leaked threads accumulate to the SetMaxThreads(2000) cap and crash the daemon
// with "fatal error: thread exhaustion".
//
// The fix creates+stores the cancel in nsAdd before launching the goroutine, so
// nsDelete can always reach it, and netNamespaceInstance aborts its init when
// the context is already cancelled. This test forces the race deterministically
// by blocking the openAndSetnsSyscalls.open seam mid-init, deleting the ns, then
// releasing the seam, and asserts the goroutine actually exits (no leak).
func TestNsChurn_deleteDuringInit_doesNotLeakThread(t *testing.T) {
	// Block the open seam mid-init so we can delete the ns while the
	// instance goroutine is still initializing.
	openEntered := make(chan struct{})
	releaseOpen := make(chan struct{})
	var once sync.Once

	devNull, err := unix.Open("/dev/null", unix.O_RDONLY|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Skipf("open /dev/null: %v", err)
	}
	t.Cleanup(func() { _ = unix.Close(devNull) })

	origSyscalls := openAndSetnsSyscalls
	origRestore := restoreNsSetns
	openAndSetnsSyscalls = openAndSetnsSyscallsT{
		open: func(_ string, _ int, _ uint32) (int, error) {
			once.Do(func() { close(openEntered) })
			<-releaseOpen // block until the test deletes the ns
			return devNull, nil
		},
		setns: func(_ int, _ int) error { return nil },
		close: func(_ int) error { return nil },
	}
	restoreNsSetns = func(_ int, _ int) error { return nil } // clean UnlockOSThread
	t.Cleanup(func() {
		openAndSetnsSyscalls = origSyscalls
		restoreNsSetns = origRestore
	})

	x := newNsExtraFixture(t)
	x.config = &xtcp_config.XtcpConfig{Netlinkers: 0}
	x.Netlinker = func(_ context.Context, _ *sync.WaitGroup, _ *string, _ int, _ uint32) {}

	name := "race-ns"

	// Make checkMountInfo pass (it just scans mountInfoDir for the name), so
	// init reaches the (blocked) open seam rather than bailing early.
	mi := filepath.Join(t.TempDir(), "mountinfo")
	if werr := os.WriteFile(mi, []byte("36 35 0:32 / /run/netns/"+name+" rw - nsfs nsfs rw\n"), 0o600); werr != nil {
		t.Fatal(werr)
	}
	origMountInfoDir := mountInfoDir
	mountInfoDir = mi
	t.Cleanup(func() { mountInfoDir = origMountInfoDir })

	// nsAdd reserves the cancel in nsMap and launches netNamespaceInstance,
	// which blocks in the open seam.
	x.nsAdd(context.Background(), &name)

	select {
	case <-openEntered:
	case <-time.After(5 * time.Second):
		close(releaseOpen)
		t.Fatal("netNamespaceInstance never reached the open seam")
	}

	// The fix's key property: the cancel is reachable *during* init.
	if _, ok := x.nsMap.Load(name); !ok {
		close(releaseOpen)
		t.Fatal("nsAdd must store the per-ns entry (with cancel) before init completes")
	}

	// Delete while the instance is still initializing — this is the race.
	x.nsDelete(&name)

	// Let init proceed; the instance must observe the cancellation and exit
	// (abort-during-init), not block forever on <-nsCtx.Done().
	close(releaseOpen)

	deadline := time.After(5 * time.Second)
	for {
		ended := testutil.ToFloat64(x.pC.WithLabelValues("netNamespaceInstance", "end", "counter"))
		if ended >= 1 {
			break // goroutine exited — no leaked, forever-blocked thread
		}
		select {
		case <-deadline:
			t.Fatal("netNamespaceInstance did not exit after delete-during-init: the per-ns cancel was unreachable and the goroutine is blocked forever on <-nsCtx.Done() (the thread-leak regression)")
		case <-time.After(10 * time.Millisecond):
		}
	}

	// And it took the abort path rather than fully setting up a doomed ns.
	if aborted := testutil.ToFloat64(x.pC.WithLabelValues("netNamespaceInstance", "abortedDuringInit", "count")); aborted < 1 {
		t.Errorf("expected abortedDuringInit counter ≥1, got %v", aborted)
	}
}
