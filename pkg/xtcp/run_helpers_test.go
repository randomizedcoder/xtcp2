package xtcp

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

func newRunFixture(t *testing.T) *XTCP {
	t.Helper()
	x := &XTCP{
		nsMap:     &sync.Map{},
		fdToNsMap: &sync.Map{},
		netNsDirs: &sync.Map{},
	}
	reg := prometheus.NewRegistry()
	x.pC = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{Subsystem: "xtcp_run_test",
			Name: promNameCounts, Help: "test counts"},
		promLabels,
	)
	x.pH = promauto.With(reg).NewSummaryVec(
		prometheus.SummaryOpts{
			Subsystem: "xtcp_run_test", Name: promNameHistograms, Help: "test",
			Objectives: map[float64]float64{0.5: quantileError},
			MaxAge:     summaryVecMaxAge,
		},
		promLabels,
	)
	return x
}

// ───────────────────────────────────────────────────────────────────────
// checkDoneNonBlocking — branch on whether ctx is cancelled
// ───────────────────────────────────────────────────────────────────────

func TestCheckDoneNonBlocking_open(t *testing.T) {
	x := &XTCP{}
	ctx := context.Background()
	if x.checkDoneNonBlocking(ctx) {
		t.Error("uncancelled ctx should report not-done")
	}
}

func TestCheckDoneNonBlocking_cancelled(t *testing.T) {
	x := &XTCP{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if !x.checkDoneNonBlocking(ctx) {
		t.Error("cancelled ctx should report done")
	}
}

// ───────────────────────────────────────────────────────────────────────
// closeDestination — three branches: nil, ok, err
// ───────────────────────────────────────────────────────────────────────

type fakeCloseDest struct {
	closed   bool
	closeErr error
}

func (f *fakeCloseDest) Send(_ context.Context, _ *[]byte) (int, error) { return 0, nil }
func (f *fakeCloseDest) Close() error {
	f.closed = true
	return f.closeErr
}

func TestCloseDestination_nil(t *testing.T) {
	x := &XTCP{}
	x.closeDestination() // no panic, nothing to close
}

func TestCloseDestination_ok(t *testing.T) {
	d := &fakeCloseDest{}
	x := &XTCP{dest: d}
	x.closeDestination()
	if !d.closed {
		t.Error("Close should have been called")
	}
}

func TestCloseDestination_errorLogged(t *testing.T) {
	d := &fakeCloseDest{closeErr: errors.New("boom")}
	x := &XTCP{dest: d, debugLevel: 11} // hit the log.Printf branch
	x.closeDestination()
	if !d.closed {
		t.Error("Close should have been called even on error")
	}
}

// ───────────────────────────────────────────────────────────────────────
// reconcile + mapReconciler — drive the WG via context cancellation
// ───────────────────────────────────────────────────────────────────────

// newReconcileFixture extends newRunFixture with at least one netNsDir
// entry pointing to a real (empty) tempdir, so x.discoverAllNamespaces
// doesn't panic on nsMaps[0].
func newReconcileFixture(t *testing.T) *XTCP {
	t.Helper()
	x := newRunFixture(t)
	x.nsMap = &sync.Map{}
	x.netNsDirs.Store(t.TempDir(), "/")
	return x
}

func TestReconcile_emptyMaps(t *testing.T) {
	x := newReconcileFixture(t)
	dels, stores := x.reconcile(context.Background())
	if dels != 0 || stores != 0 {
		t.Errorf("empty reconcile: dels=%d stores=%d, want 0/0", dels, stores)
	}
}

func TestMapReconciler_cancelExits(t *testing.T) {
	x := newReconcileFixture(t)
	x.fatalf = t.Fatalf // mapReconciler may eventually call nsAdd which uses fatalf
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	done := make(chan struct{})
	go func() {
		x.mapReconciler(ctx, &wg)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("mapReconciler did not exit on ctx cancel")
	}
	wg.Wait()
}

// ───────────────────────────────────────────────────────────────────────
// nsMapCountReporter — periodic ticker that reads MapCount + LenSyncMap
// and publishes to a gauge. Exit via ctx.Done().
// ───────────────────────────────────────────────────────────────────────

func TestNsMapCountReporter_cancelExits(t *testing.T) {
	x := newRunFixture(t)
	x.nsMap = &sync.Map{}
	// pG is required by nsMapCountReporter (gauge sink); newRunFixture
	// doesn't set it, but the ticker-driven update requires it. Use a
	// fresh gauge registered on the test's private registry.
	reg := prometheus.NewRegistry()
	x.pG = promauto.With(reg).NewGauge(prometheus.GaugeOpts{
		Subsystem: "xtcp_runhelpers_test", Name: promNameGauge, Help: "test gauge",
	})
	x.debugLevel = 200 // exercise the log branch on tick

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	done := make(chan struct{})
	go func() {
		x.nsMapCountReporter(ctx, &wg)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("nsMapCountReporter did not exit on cancel")
	}
	wg.Wait()
}

// watchNsNamespace: fsnotify-based ns watcher. With a tempdir as netNsDir
// (not linuxNetNSDirCst, so the createNetworkNamespace branch is skipped),
// it should set up the watcher and exit on ctx.Done().
func TestWatchNsNamespace_cancelExits(t *testing.T) {
	x := newRunFixture(t)
	x.nsMap = &sync.Map{}
	dir := t.TempDir()

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(1)
	done := make(chan error, 1)
	go func() {
		done <- x.watchNsNamespace(ctx, &wg, dir)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("watchNsNamespace did not exit on cancel")
	}
	wg.Wait()
}

// ───────────────────────────────────────────────────────────────────────
// checkDirectoryExists — three branches: exists+dir, exists+file, missing
// ───────────────────────────────────────────────────────────────────────

func TestCheckDirectoryExists_isDir(t *testing.T) {
	dir := t.TempDir()
	if !checkDirectoryExists(dir) {
		t.Errorf("tempdir %q should report exists", dir)
	}
}

func TestCheckDirectoryExists_missing(t *testing.T) {
	if checkDirectoryExists("/no/such/path/probably") {
		t.Error("missing path should report not-exists")
	}
}

func TestCheckDirectoryExists_isFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "file")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// Regular file → info.IsDir() returns false.
	if checkDirectoryExists(f) {
		t.Error("regular file should not report as directory")
	}
}
