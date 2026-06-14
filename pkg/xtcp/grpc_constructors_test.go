package xtcp

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
)

// With the constructor's new `reg` parameter, tests can pass a fresh
// prometheus.NewRegistry() to avoid the default-registry duplicate
// registration panic that used to be possible when the same constructor
// ran twice in one process.

func TestNewXtcpFlatRecordService_freshRegistry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := make(chan struct{}, 1)
	got := NewXtcpFlatRecordService(ctx, prometheus.NewRegistry(), &ch, 0)
	if got == nil {
		t.Fatal("NewXtcpFlatRecordService returned nil")
	}
}

func TestNewXtcpFlatRecordService_nilFallsBackToDefault(t *testing.T) {
	// nil reg → falls back to prometheus.DefaultRegisterer. If a prior
	// test in this package already registered on the default registry
	// with the same metric names this would panic; recover and skip in
	// that case.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := make(chan struct{}, 1)
	defer func() {
		if r := recover(); r != nil {
			t.Skipf("recovered from re-registration on default registry: %v", r)
		}
	}()
	got := NewXtcpFlatRecordService(ctx, nil, &ch, 0)
	if got == nil {
		t.Fatal("NewXtcpFlatRecordService returned nil")
	}
}

func TestNewXtcpConfigService_freshRegistry(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := make(chan time.Duration, 1)
	cfg := &xtcp_config.XtcpConfig{PollFrequency: nil}
	got := NewXtcpConfigService(ctx, prometheus.NewRegistry(), cfg, &ch, 0)
	if got == nil {
		t.Fatal("NewXtcpConfigService returned nil")
	}
	if got.config != cfg {
		t.Error("config not stored")
	}
}

func TestNewXtcpConfigService_nilFallsBackToDefault(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := make(chan time.Duration, 1)
	cfg := &xtcp_config.XtcpConfig{PollFrequency: nil}
	defer func() {
		if r := recover(); r != nil {
			t.Skipf("recovered from re-registration on default registry: %v", r)
		}
	}()
	got := NewXtcpConfigService(ctx, nil, cfg, &ch, 0)
	if got == nil {
		t.Fatal("NewXtcpConfigService returned nil")
	}
}

// InitPromethus with a fresh registry: x.pC, x.pH, x.pG should all be
// non-nil; calling it twice with two different registries should not
// panic (the duplicate-collector check is per-registry).
func TestInitPromethus_freshRegistry(t *testing.T) {
	x := &XTCP{registry: prometheus.NewRegistry()}
	var wg sync.WaitGroup
	wg.Add(1)
	x.InitPromethus(&wg)
	wg.Wait()
	if x.pC == nil || x.pH == nil || x.pG == nil {
		t.Error("InitPromethus did not populate all metric handles")
	}

	// Run a second time with a different registry — should also succeed.
	x2 := &XTCP{registry: prometheus.NewRegistry()}
	var wg2 sync.WaitGroup
	wg2.Add(1)
	x2.InitPromethus(&wg2)
	wg2.Wait()
}

func TestInitPromethus_nilFallsBackToDefault(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Skipf("recovered from re-registration on default registry: %v", r)
		}
	}()
	x := &XTCP{} // x.registry zero-value → nil → falls back
	var wg sync.WaitGroup
	wg.Add(1)
	x.InitPromethus(&wg)
	wg.Wait()
}
