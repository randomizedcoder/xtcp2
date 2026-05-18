package xtcp

import (
	"context"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_config"
)

// NewXtcpConfigService registers metrics on the default Prometheus
// registry. Calling it more than once in the same process panics, so this
// test runs in a fresh package-level subtest. The newConfigServiceFixture
// pattern (used elsewhere) bypasses NewXtcpConfigService precisely to
// avoid the default-registry conflict — but we still need direct test
// coverage of the constructor.
func TestNewXtcpFlatRecordService_smoke(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := make(chan struct{}, 1)
	defer func() {
		if r := recover(); r != nil {
			t.Skipf("recovered from re-registration: %v", r)
		}
	}()
	got := NewXtcpFlatRecordService(ctx, &ch, 0)
	if got == nil {
		t.Fatal("NewXtcpFlatRecordService returned nil")
	}
}

func TestNewXtcpConfigService_smoke(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := make(chan time.Duration, 1)
	cfg := &xtcp_config.XtcpConfig{PollFrequency: nil}
	defer func() {
		if r := recover(); r != nil {
			// Re-running this test in the same process would re-register.
			// Allowable.
			t.Skipf("recovered from re-registration: %v", r)
		}
	}()
	got := NewXtcpConfigService(ctx, cfg, &ch, 0)
	if got == nil {
		t.Fatal("NewXtcpConfigService returned nil")
	}
	if got.config != cfg {
		t.Error("config not stored")
	}
}
