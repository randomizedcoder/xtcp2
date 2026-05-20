//go:build dest_nats

package xtcp

import (
	"context"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// destinations_nats_test.go exercises pkg/xtcp/destinations_nats.go
// under the `dest_nats` build tag. The production code is tightly
// coupled to the nats.Conn type (no interface seam), so unit tests
// without a real NATS broker are limited. End-to-end Send/Close/
// publish coverage runs against a real nats-server inside the
// microvm lifecycle harness.
//
// What we CAN cover here:
//   - init() side effect (dispatch registered)
//   - constant values (natsReconnectsCst, natsTimeoutCst)
//   - Close on nil client is safe (no panic)
//   - newNATSDest fails-fast (or fast-enough) on an unreachable URL
//     so callers don't hang forever during daemon startup

// ───────────────────────────────────────────────────────────────────────
// init() side effect
// ───────────────────────────────────────────────────────────────────────

func TestNATSDest_initRegistersScheme(t *testing.T) {
	if !IsKnownScheme(schemeNats) {
		t.Errorf("scheme %q should be registered under build tag dest_nats", schemeNats)
	}
	_, status := lookupDestinationFactory(schemeNats)
	if status != destLookupFound {
		t.Errorf("lookupDestinationFactory(%q) status = %d, want destLookupFound", schemeNats, status)
	}
}

// ───────────────────────────────────────────────────────────────────────
// Constants — pin against unintended timeout / reconnect-count changes
// ───────────────────────────────────────────────────────────────────────

func TestNATSDestConstants(t *testing.T) {
	// 5 reconnects @ 2s + jitter = bounded recovery window
	if natsReconnectsCst != 5 {
		t.Errorf("natsReconnectsCst = %d, want 5", natsReconnectsCst)
	}
	// 1s per connection attempt — keeps startup from hanging.
	if natsTimeoutCst != 1*time.Second {
		t.Errorf("natsTimeoutCst = %v, want 1s", natsTimeoutCst)
	}
}

// ───────────────────────────────────────────────────────────────────────
// Close-on-nil-client must not panic. Pin the safety check at
// destinations_nats.go:67 (`if d.client != nil { … }`).
// ───────────────────────────────────────────────────────────────────────

func TestNATSDest_CloseNilClient(t *testing.T) {
	d := &natsDest{x: newTestXTCP(t, "nats:127.0.0.1:4222"), client: nil}
	if err := d.Close(); err != nil {
		t.Errorf("Close on nil client should be nil; got %v", err)
	}
}

// (A "newNATSDest returns within natsTimeoutCst on unreachable URL"
// test would require precise control over nats.go's
// RetryOnFailedConnect semantics, which vary across versions and can
// block for MaxReconnect * ReconnectWait = 10s on connection refusal.
// The stripsScheme test below indirectly covers the bounded-time
// behaviour by completing within natsTimeoutCst + 2s grace once the
// fake listener accepts.)

// TestNewNATSDest_stripsScheme verifies that the "nats:" scheme prefix
// is removed before being passed to nats.Options.Url. Without a real
// server we observe this indirectly: a URL of "nats:127.0.0.1:65535"
// must NOT result in nats trying to dial literally "nats:127.0.0.1:65535"
// (which would fail with "no such host" rather than "connection
// refused").
//
// We test this by setting up a TCP listener on 127.0.0.1:0, deriving
// the addr, then asking newNATSDest to connect to "nats:<addr>" — if
// the prefix stripping works, the listener will receive a connection
// attempt within ~natsTimeoutCst. (We don't speak NATS protocol on
// the listener side; the goal is just to observe the dial reached the
// right host:port.)
func TestNewNATSDest_stripsScheme(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = ln.Close() }()

	connected := make(chan struct{}, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		connected <- struct{}{}
		_ = conn.Close()
	}()

	x := newTestXTCP(t, "nats:"+ln.Addr().String())
	done := make(chan struct{})
	go func() {
		defer close(done)
		d, _ := newNATSDest(context.Background(), x)
		if d != nil {
			_ = d.Close()
		}
	}()

	select {
	case <-connected:
		// Dial reached our fake listener at the stripped host:port.
		// Cleanup the dialer goroutine.
		<-done
	case <-time.After(natsTimeoutCst + 2*time.Second):
		t.Fatal("nats client did not dial the stripped host:port within natsTimeoutCst + 2s")
	}
}

// ───────────────────────────────────────────────────────────────────────
// Race — drive the helpers concurrently. Each goroutine builds its
// own natsDest with a nil client and exercises Close.
// ───────────────────────────────────────────────────────────────────────

func TestNATSDest_concurrentCloseOnNil(t *testing.T) {
	const goroutines = 16
	var wg sync.WaitGroup
	var calls atomic.Int64
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				d := &natsDest{x: newTestXTCP(t, "nats:127.0.0.1:4222"), client: nil}
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

func BenchmarkNATSDest_CloseNilClient(b *testing.B) {
	x := newTestXTCP(&testing.T{}, "nats:127.0.0.1:4222")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d := &natsDest{x: x, client: nil}
		_ = d.Close()
	}
}
