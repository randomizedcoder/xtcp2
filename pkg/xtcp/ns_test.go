package xtcp

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// newNsFixture returns an XTCP shape that the various ns_* + poller
// helpers need: Prometheus counter/histogram registries, empty sync.Maps,
// and a fake fdToNsMap.
func newNsFixture(t *testing.T) *XTCP {
	t.Helper()
	x := &XTCP{
		nsMap:     &sync.Map{},
		fdToNsMap: &sync.Map{},
		netNsDirs: &sync.Map{},
	}
	reg := prometheus.NewRegistry()
	x.pC = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{Subsystem: "xtcp_ns_test",
			Name: promNameCounts, Help: "test counts"},
		promLabels,
	)
	x.pH = promauto.With(reg).NewSummaryVec(
		prometheus.SummaryOpts{
			Subsystem: "xtcp_ns_test", Name: promNameHistograms, Help: "test",
			Objectives: map[float64]float64{0.5: quantileError},
			MaxAge:     summaryVecMaxAge,
		},
		promLabels,
	)
	x.pG = promauto.With(reg).NewGauge(prometheus.GaugeOpts{
		Subsystem: "xtcp_ns_test", Name: promNameGauge, Help: "test gauge",
	})
	return x
}

// ───────────────────────────────────────────────────────────────────────
// MapCount / LenSyncMap / lenSyncMap / GetNetlinkSocketFDs
// ───────────────────────────────────────────────────────────────────────

func TestMapCount(t *testing.T) {
	x := newNsFixture(t)
	if got := x.MapCount(); got != 0 {
		t.Errorf("MapCount empty = %d, want 0", got)
	}
	x.storeCount.Add(5)
	x.deleteCount.Add(2)
	if got := x.MapCount(); got != 3 {
		t.Errorf("MapCount = %d, want 3", got)
	}
}

func TestLenSyncMap(t *testing.T) {
	m := &sync.Map{}
	if got := lenSyncMap(m); got != 0 {
		t.Errorf("empty map len = %d, want 0", got)
	}
	m.Store("a", 1)
	m.Store("b", 2)
	if got := lenSyncMap(m); got != 2 {
		t.Errorf("map len = %d, want 2", got)
	}
}

func TestXTCP_LenSyncMap(t *testing.T) {
	x := newNsFixture(t)
	x.nsMap.Store("a", "x")
	if got := x.LenSyncMap(); got != 1 {
		t.Errorf("LenSyncMap = %d, want 1", got)
	}
}

func TestGetNetlinkSocketFDs(t *testing.T) {
	x := newNsFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	x.nsMap.Store("ns1", netNSitem{
		ctx:      ctx,
		cancel:   cancel,
		socketFD: 7,
	})
	x.nsMap.Store("ns2", netNSitem{
		ctx:      ctx,
		cancel:   cancel,
		socketFD: 11,
	})
	got := x.GetNetlinkSocketFDs()
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	// Order is map-iteration, so just check membership.
	seen := map[int]bool{got[0]: true, got[1]: true}
	if !seen[7] || !seen[11] {
		t.Errorf("got %v, want {7,11}", got)
	}
}

// ───────────────────────────────────────────────────────────────────────
// discoverNamespaces — scans a tempdir + returns map keyed by file name.
// ───────────────────────────────────────────────────────────────────────

func TestDiscoverNamespaces_emptyDir(t *testing.T) {
	x := newNsFixture(t)
	dir := t.TempDir() + "/"
	m := x.discoverNamespaces(dir)
	count := 0
	m.Range(func(k, v interface{}) bool { count++; return true })
	if count != 0 {
		t.Errorf("empty dir should yield 0 entries; got %d", count)
	}
}

func TestDiscoverNamespaces_withFiles(t *testing.T) {
	x := newNsFixture(t)
	dir := t.TempDir() + "/"
	for _, name := range []string{"alice", "bob", "charlie"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte{}, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// And one subdirectory that should be skipped.
	if err := os.Mkdir(filepath.Join(dir, "subdir"), 0o700); err != nil {
		t.Fatal(err)
	}
	m := x.discoverNamespaces(dir)
	count := 0
	m.Range(func(k, v interface{}) bool { count++; return true })
	if count != 3 {
		t.Errorf("3 files + 1 dir → expected 3 entries; got %d", count)
	}
}

func TestDiscoverNamespaces_missingDir(t *testing.T) {
	x := newNsFixture(t)
	m := x.discoverNamespaces("/no/such/path/")
	count := 0
	m.Range(func(k, v interface{}) bool { count++; return true })
	if count != 0 {
		t.Errorf("missing dir should yield 0 entries; got %d", count)
	}
}

// ───────────────────────────────────────────────────────────────────────
// discoverAllNamespaces — merges discoverNamespaces results across
// every entry in netNsDirs.
// ───────────────────────────────────────────────────────────────────────

func TestDiscoverAllNamespaces_singleDir(t *testing.T) {
	x := newNsFixture(t)
	dir := t.TempDir() + "/"
	if err := os.WriteFile(filepath.Join(dir, "ns1"), []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	x.netNsDirs.Store(dir, true)
	m := x.discoverAllNamespaces()
	count := 0
	m.Range(func(k, v interface{}) bool { count++; return true })
	if count != 1 {
		t.Errorf("single dir 1 file → 1 entry; got %d", count)
	}
}

func TestDiscoverAllNamespaces_mergedDirs(t *testing.T) {
	x := newNsFixture(t)
	dir1 := t.TempDir() + "/"
	dir2 := t.TempDir() + "/"
	if err := os.WriteFile(filepath.Join(dir1, "ns1"), []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir2, "ns2"), []byte{}, 0o600); err != nil {
		t.Fatal(err)
	}
	x.netNsDirs.Store(dir1, true)
	x.netNsDirs.Store(dir2, true)
	m := x.discoverAllNamespaces()
	count := 0
	m.Range(func(k, v interface{}) bool { count++; return true })
	if count != 2 {
		t.Errorf("merged 2 dirs → 2 entries; got %d", count)
	}
}

// ───────────────────────────────────────────────────────────────────────
// mergeMaps — used by discoverAllNamespaces; map1 + map2 union into new.
// ───────────────────────────────────────────────────────────────────────

func TestMergeMaps(t *testing.T) {
	m1 := &sync.Map{}
	m1.Store("a", 1)
	m1.Store("b", 2)
	m2 := &sync.Map{}
	m2.Store("b", 99) // overlap: map2 wins
	m2.Store("c", 3)
	merged := mergeMaps(m1, m2)
	got := map[string]int{}
	merged.Range(func(k, v interface{}) bool {
		got[k.(string)] = v.(int)
		return true
	})
	want := map[string]int{"a": 1, "b": 99, "c": 3}
	for k, wv := range want {
		if got[k] != wv {
			t.Errorf("merged[%s] = %d, want %d", k, got[k], wv)
		}
	}
}

// ───────────────────────────────────────────────────────────────────────
// nsDelete — removes a netNSitem from nsMap + cancels its context.
// ───────────────────────────────────────────────────────────────────────

func TestNsDelete(t *testing.T) {
	x := newNsFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// nsDelete will call cancel() on the item's stored CancelFunc.
	var canceled bool
	storedCancel := func() { canceled = true }
	x.nsMap.Store("ns1", netNSitem{
		ctx:      ctx,
		cancel:   storedCancel,
		socketFD: 7,
	})
	x.fdToNsMap.Store(7, "ns1")
	name := "ns1"
	x.nsDelete(&name)
	if _, ok := x.nsMap.Load("ns1"); ok {
		t.Error("nsDelete should remove the entry from nsMap")
	}
	if _, ok := x.fdToNsMap.Load(7); ok {
		t.Error("nsDelete should remove the fd→ns binding")
	}
	if !canceled {
		t.Error("nsDelete should call cancel() on the stored item")
	}
	if x.deleteCount.Load() != 1 {
		t.Errorf("deleteCount = %d, want 1", x.deleteCount.Load())
	}
}

func TestNsDelete_missingKey(t *testing.T) {
	x := newNsFixture(t)
	name := "no-such-ns"
	// Should not panic, should be a no-op.
	x.nsDelete(&name)
	if x.deleteCount.Load() != 0 {
		t.Errorf("deleteCount should stay 0 for missing key; got %d",
			x.deleteCount.Load())
	}
}

// ───────────────────────────────────────────────────────────────────────
// observeNetlinkerDone — Poller helper, channel-driven, pure metric ops.
// ───────────────────────────────────────────────────────────────────────

func TestObserveNetlinkerDone_missingFd(t *testing.T) {
	x := newNsFixture(t)
	// No pollTime entry for this fd → early return.
	x.observeNetlinkerDone(netlinkerDone{fd: 99}, 1)
	// No panic = success.
}

// ───────────────────────────────────────────────────────────────────────
// handlePollRequest — Poller's pollRequestCh case body.
// ───────────────────────────────────────────────────────────────────────

func TestHandlePollRequest_alreadyPolling(t *testing.T) {
	x := newNsFixture(t)
	// pollAllNetlinkSockets requires more state; skip by checking the
	// already-polling early return.
	_, polled := x.handlePollRequest(1, 5 /* count > 0 */, x.pollStartTime)
	if polled {
		t.Error("handlePollRequest should return polled=false when count>0")
	}
}
