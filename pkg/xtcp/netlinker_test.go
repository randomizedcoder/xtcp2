package xtcp

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
)

// netlinkerSyscall: drive the early-exit path with an already-canceled
// ctx. The loop's first checkDoneNonBlocking returns true and the function
// cleans up + returns without ever calling Recvfrom.

func TestNetlinkerSyscall_earlyExit(t *testing.T) {
	x := &XTCP{
		config: &xtcp_config.XtcpConfig{WriteFiles: 0},
	}
	reg := prometheus.NewRegistry()
	x.pC = promauto.With(reg).NewCounterVec(
		prometheus.CounterOpts{Subsystem: "xtcp_netlinker_test",
			Name: promNameCounts, Help: "test counts"},
		promLabels,
	)
	x.pH = promauto.With(reg).NewSummaryVec(
		prometheus.SummaryOpts{
			Subsystem: "xtcp_netlinker_test", Name: promNameHistograms, Help: "test",
			Objectives: map[float64]float64{0.5: quantileError},
			MaxAge:     summaryVecMaxAge,
		},
		promLabels,
	)
	x.packetBufferPool.Init(func() *[]byte { b := make([]byte, 4096); return &b })

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-canceled → first checkDoneNonBlocking returns true

	wg := new(sync.WaitGroup)
	wg.Add(1)
	name := "test-ns"
	done := make(chan struct{})
	go func() {
		x.netlinkerSyscall(ctx, wg, &name, -1, 0)
		close(done)
	}()
	wg.Wait()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("netlinkerSyscall did not exit on pre-canceled ctx")
	}
}
