package xtcp

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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
