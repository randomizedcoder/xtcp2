//go:build dest_valkey

package xtcp

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// destinations_valkey_test.go exercises pkg/xtcp/destinations_valkey.go
// under the `dest_valkey` build tag.
//
// Scope notes: newValKeyDest calls client.Ping() at construction
// time, and go-redis v9 does a multi-step RESP3 negotiation (HELLO,
// CLIENT SETINFO …) before PING. Faking that handshake in-process is
// hard to keep robust across go-redis upgrades, so this file only
// covers what's testable without a real Valkey/Redis server:
//
//   - init() side effect (dispatch registered)
//   - constants (pool size + ping/IO timeouts)
//   - Close-on-nil-client safety
//   - newValKeyDest against an unreachable URL: returns an err within
//     valkeyPingTimeoutCst + grace, never hangs
//
// The happy-path Publish flow runs against a real Valkey inside the
// microvm lifecycle harness (where the kgo/sr / NATS / NSQ / Valkey
// integration tests all share an actual service).

// ───────────────────────────────────────────────────────────────────────
// init() side effect
// ───────────────────────────────────────────────────────────────────────

func TestValkeyDest_initRegistersScheme(t *testing.T) {
	if !IsKnownScheme(schemeValkey) {
		t.Errorf("scheme %q should be registered under build tag dest_valkey", schemeValkey)
	}
	_, status := lookupDestinationFactory(schemeValkey)
	if status != destLookupFound {
		t.Errorf("lookupDestinationFactory(%q) status = %d, want destLookupFound", schemeValkey, status)
	}
}

// ───────────────────────────────────────────────────────────────────────
// Constants
// ───────────────────────────────────────────────────────────────────────

func TestValkeyDestConstants(t *testing.T) {
	if valkeyPingTimeoutCst != 2*time.Second {
		t.Errorf("valkeyPingTimeoutCst = %v, want 2s", valkeyPingTimeoutCst)
	}
	if valkeyTimeoutCst != 1*time.Second {
		t.Errorf("valkeyTimeoutCst = %v, want 1s", valkeyTimeoutCst)
	}
	if valkeyMaxIdleConnsCst != 20 {
		t.Errorf("valkeyMaxIdleConnsCst = %d, want 20", valkeyMaxIdleConnsCst)
	}
}

// ───────────────────────────────────────────────────────────────────────
// Close on nil client must not panic.
// ───────────────────────────────────────────────────────────────────────

func TestValkeyDest_CloseNilClient(t *testing.T) {
	d := &valkeyDest{x: newTestXTCP(t, "valkey:127.0.0.1:6379"), client: nil}
	if err := d.Close(); err != nil {
		t.Errorf("Close on nil client should be nil; got %v", err)
	}
}

// ───────────────────────────────────────────────────────────────────────
// newValKeyDest against an unreachable URL — must surface an err
// within valkeyPingTimeoutCst (2s) + 1s grace, never hang.
// ───────────────────────────────────────────────────────────────────────

func TestNewValKeyDest_unreachableURL(t *testing.T) {
	x := newTestXTCP(t, "valkey:127.0.0.1:1") // port 1 → connection refused
	start := time.Now()
	d, err := newValKeyDest(context.Background(), x)
	if err == nil {
		t.Error("expected err on unreachable URL")
	}
	if d != nil {
		_ = d.Close()
	}
	if elapsed := time.Since(start); elapsed > valkeyPingTimeoutCst+1*time.Second {
		t.Errorf("returned in %v; expected ≤ %v", elapsed, valkeyPingTimeoutCst+1*time.Second)
	}
}

func TestNewValKeyDest_emptyAddrAfterPrefix(t *testing.T) {
	x := newTestXTCP(t, "valkey:")
	d, err := newValKeyDest(context.Background(), x)
	if err == nil {
		t.Error("expected err on empty addr")
	}
	if d != nil {
		_ = d.Close()
	}
}

// ───────────────────────────────────────────────────────────────────────
// Race — concurrent Close-on-nil
// ───────────────────────────────────────────────────────────────────────

func TestValkeyDest_concurrentCloseOnNil(t *testing.T) {
	const goroutines = 16
	var wg sync.WaitGroup
	var calls atomic.Int64
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				d := &valkeyDest{x: newTestXTCP(t, "valkey:127.0.0.1:6379"), client: nil}
				if err := d.Close(); err != nil {
					return
				}
				calls.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := calls.Load(); got != goroutines*100 {
		t.Errorf("calls = %d, want %d", got, goroutines*100)
	}
}

// ───────────────────────────────────────────────────────────────────────
// Benchmark
// ───────────────────────────────────────────────────────────────────────

func BenchmarkValkeyDest_CloseNilClient(b *testing.B) {
	x := newTestXTCP(&testing.T{}, "valkey:127.0.0.1:6379")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d := &valkeyDest{x: x, client: nil}
		_ = d.Close()
	}
}
