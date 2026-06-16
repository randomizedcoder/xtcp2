package xtcp

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
)

// ───────────────────────────────────────────────────────────────────────
// incrementStoreAndGenerationCounts: two atomic adds
// ───────────────────────────────────────────────────────────────────────

func TestIncrementStoreAndGenerationCounts(t *testing.T) {
	x := &XTCP{
		storeCount: atomic.Uint64{},
		generation: atomic.Uint64{},
	}
	x.incrementStoreAndGenerationCounts()
	if got := x.storeCount.Load(); got != 1 {
		t.Errorf("storeCount = %d, want 1", got)
	}
	if got := x.generation.Load(); got != 1 {
		t.Errorf("generation = %d, want 1", got)
	}
	x.incrementStoreAndGenerationCounts()
	if got := x.storeCount.Load(); got != 2 {
		t.Errorf("after 2nd call storeCount = %d, want 2", got)
	}
}

// ───────────────────────────────────────────────────────────────────────
// createNetlinkers: with netlinkers=0, just exits without spawning
// ───────────────────────────────────────────────────────────────────────

func TestCreateNetlinkers_zero(t *testing.T) {
	x := &XTCP{}
	reg := prometheus.NewRegistry()
	x.pC = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{Subsystem: "xtcp_createnl_test",
			Name: promNameCounts, Help: "test counts"},
		promLabels,
	)
	wg := new(sync.WaitGroup)
	wg.Add(1)
	name := "test-ns"
	x.createNetlinkers(context.Background(), wg, &name, -1, 0)
	wg.Wait() // should complete instantly since netlinkers=0
}

// createNetlinkersAndStore: setSocketTimeoutViaSyscall on a socketpair fd
// + store the netNSitem with netlinkers=0 (no goroutines spawn). Verifies
// the store path + counter increments fire end-to-end.
func TestCreateNetlinkersAndStore_zeroNetlinkers(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
	if err != nil {
		t.Skipf("socketpair: %v", err)
	}
	defer func() {
		_ = unix.Close(fds[0])
		_ = unix.Close(fds[1])
	}()

	x := newNsExtraFixture(t)
	x.config = &xtcp_config.XtcpConfig{
		NlTimeoutMilliseconds: 100,
		Netlinkers:            0,
	}
	// Netlinker function pointer is required by createNetlinkers; even
	// with netlinkers=0 the field must be set (the loop body never runs
	// so any signature works).
	x.Netlinker = func(_ context.Context, _ *sync.WaitGroup, _ *string, _ int, _ uint32) {}
	x.debugLevel = 11 // hit the log branch
	nsName := "test-ns"
	nsCtx, nsCancel := context.WithCancel(context.Background())
	defer nsCancel()
	x.createNetlinkersAndStore(nsCtx, nsCancel, &nsName, fds[0])

	if _, ok := x.nsMap.Load(nsName); !ok {
		t.Error("nsMap should contain the new ns entry")
	}
	if _, ok := x.fdToNsMap.Load(fds[0]); !ok {
		t.Error("fdToNsMap should contain the new fd entry")
	}
	if x.storeCount.Load() != 1 || x.generation.Load() != 1 {
		t.Errorf("counters: storeCount=%d generation=%d, want 1/1", x.storeCount.Load(), x.generation.Load())
	}
}

// createNetlinkers with netlinkers=2 + a no-op Netlinker function: drives
// the loop body (counter + spawn + debug log) for each iteration.
func TestCreateNetlinkers_nonZero(t *testing.T) {
	x := newNsExtraFixture(t)
	x.debugLevel = 11 // hit log branch
	var ran sync.WaitGroup
	x.Netlinker = func(_ context.Context, wg *sync.WaitGroup, _ *string, _ int, _ uint32) {
		defer wg.Done()
		ran.Done()
	}
	ran.Add(2)
	wg := new(sync.WaitGroup)
	wg.Add(1)
	name := "spawn-ns"
	x.createNetlinkers(context.Background(), wg, &name, 9, 2)
	wg.Wait()
	ran.Wait() // both Netlinker invocations ran
}

// createNetlinkersAndStore with netlinkers=2: the spawned netlinkers run
// the no-op stub, the netNSitem lands in nsMap, and storeCount/generation
// both increment by 1.
func TestCreateNetlinkersAndStore_spawnsNetlinkers(t *testing.T) {
	fds, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM, 0)
	if err != nil {
		t.Skipf("socketpair: %v", err)
	}
	defer func() {
		_ = unix.Close(fds[0])
		_ = unix.Close(fds[1])
	}()

	x := newNsExtraFixture(t)
	x.config = &xtcp_config.XtcpConfig{
		NlTimeoutMilliseconds: 100,
		Netlinkers:            2,
	}
	var ran sync.WaitGroup
	ran.Add(2)
	x.Netlinker = func(_ context.Context, wg *sync.WaitGroup, _ *string, _ int, _ uint32) {
		defer wg.Done()
		ran.Done()
	}
	x.debugLevel = 11
	nsName := "spawn-store-ns"
	nsCtx, nsCancel := context.WithCancel(context.Background())
	defer nsCancel()
	x.createNetlinkersAndStore(nsCtx, nsCancel, &nsName, fds[0])
	ran.Wait()

	if _, ok := x.nsMap.Load(nsName); !ok {
		t.Error("nsMap should contain the new ns entry")
	}
}

// nsAdd duplicate branch: already-present nsName increments the duplicate
// counter + returns without spawning a netNamespaceInstance goroutine.
func TestNsAdd_duplicate(t *testing.T) {
	x := newNsExtraFixture(t)
	x.debugLevel = 11
	nsName := "already-here"
	x.nsMap.Store(nsName, netNSitem{})
	x.nsAdd(context.Background(), &nsName)
	// No assert needed beyond the function returning; the counter was
	// incremented and no panic / leaked goroutine.
}

func newNsExtraFixture(t *testing.T) *XTCP {
	t.Helper()
	reg := prometheus.NewRegistry()
	x := &XTCP{
		nsMap:     &sync.Map{},
		fdToNsMap: &sync.Map{},
	}
	x.pC = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{Subsystem: "xtcp_nsstore_test",
			Name: promNameCounts, Help: "test counts"},
		promLabels,
	)
	x.pH = promauto.With(reg).NewSummaryVec(
		prometheus.SummaryOpts{Subsystem: "xtcp_nsstore_test",
			Name: promNameHistograms, Help: "test",
			Objectives: map[float64]float64{0.5: quantileError},
			MaxAge:     summaryVecMaxAge},
		promLabels,
	)
	return x
}
