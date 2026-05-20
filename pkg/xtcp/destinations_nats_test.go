//go:build dest_nats

package xtcp

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	nats "github.com/nats-io/nats.go"
	"github.com/prometheus/client_golang/prometheus/testutil"
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

// ───────────────────────────────────────────────────────────────────────
// fakeNATSPublisher + Send/Close tests via the natsPublisher interface
// seam. Same shape as fakeKafkaProducer in destinations_kafka_test.go.
// ───────────────────────────────────────────────────────────────────────

type fakeNATSPublisher struct {
	publishErr error
	flushErr   error
	publishes  atomic.Int64
	flushes    atomic.Int64
	closes     atomic.Int64
	lastSubj   string
	lastData   []byte
}

func (f *fakeNATSPublisher) Publish(subj string, data []byte) error {
	f.publishes.Add(1)
	f.lastSubj = subj
	f.lastData = append(f.lastData[:0], data...)
	return f.publishErr
}
func (f *fakeNATSPublisher) FlushTimeout(_ time.Duration) error {
	f.flushes.Add(1)
	return f.flushErr
}
func (f *fakeNATSPublisher) Close() { f.closes.Add(1) }

func newNATSDestForTest(t *testing.T, fake *fakeNATSPublisher) *natsDest {
	t.Helper()
	x := newTestXTCP(t, "nats:127.0.0.1:4222")
	x.config.Topic = "xtcp-test"
	return &natsDest{x: x, client: fake}
}

// ───────────────────────────────────────────────────────────────────────
// Send
// ───────────────────────────────────────────────────────────────────────

func TestNATSDest_Send_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name           string
		category       string
		publishErr     error
		wantN          int
		wantErr        bool
		wantOKCounter  float64
		wantErrCounter float64
	}{
		{"positive_clean_publish", "positive", nil, 1, false, 1, 0},
		{"negative_publish_err", "negative", errors.New("no servers"), 0, true, 0, 1},
		{"boundary_publish_returns_eof", "boundary", errors.New("EOF"), 0, true, 0, 1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeNATSPublisher{publishErr: tc.publishErr}
			d := newNATSDestForTest(t, fake)
			payload := []byte("payload")
			n, err := d.Send(context.Background(), &payload)
			if n != tc.wantN {
				t.Errorf("n = %d, want %d", n, tc.wantN)
			}
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if fake.publishes.Load() != 1 {
				t.Errorf("Publish calls = %d, want 1", fake.publishes.Load())
			}
			if fake.lastSubj != "xtcp-test" {
				t.Errorf("subj = %q, want xtcp-test", fake.lastSubj)
			}
			gotOK := testutil.ToFloat64(d.x.pC.WithLabelValues("destNATS", "Publish", "count"))
			gotErr := testutil.ToFloat64(d.x.pC.WithLabelValues("destNATS", "Publish", "error"))
			if gotOK != tc.wantOKCounter {
				t.Errorf("OK counter = %v, want %v", gotOK, tc.wantOKCounter)
			}
			if gotErr != tc.wantErrCounter {
				t.Errorf("Err counter = %v, want %v", gotErr, tc.wantErrCounter)
			}
		})
	}
}

// TestNATSDest_Send_debugLog covers the debugLevel>10 branch.
func TestNATSDest_Send_debugLog(t *testing.T) {
	fake := &fakeNATSPublisher{}
	d := newNATSDestForTest(t, fake)
	d.x.debugLevel = 11
	payload := []byte("x")
	_, _ = d.Send(context.Background(), &payload)
}

// ───────────────────────────────────────────────────────────────────────
// Close
// ───────────────────────────────────────────────────────────────────────

func TestNATSDest_Close_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		flushErr error
	}{
		{"positive_clean_close", "positive", nil},
		{"negative_flush_err_still_closes", "negative", errors.New("flush timeout")},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeNATSPublisher{flushErr: tc.flushErr}
			d := newNATSDestForTest(t, fake)
			if err := d.Close(); err != nil {
				t.Errorf("Close err = %v, want nil", err)
			}
			if fake.flushes.Load() != 1 {
				t.Errorf("FlushTimeout calls = %d, want 1", fake.flushes.Load())
			}
			if fake.closes.Load() != 1 {
				t.Errorf("Close calls = %d, want 1", fake.closes.Load())
			}
		})
	}
}

// TestNATSDest_Close_debugLog covers the debug-log branch on flush err.
func TestNATSDest_Close_debugLog(t *testing.T) {
	fake := &fakeNATSPublisher{flushErr: errors.New("err")}
	d := newNATSDestForTest(t, fake)
	d.x.debugLevel = 11
	_ = d.Close()
}

// TestNewNATSDest_happy drives the full constructor path via the
// newNATSConnFn factory seam.
func TestNewNATSDest_happy(t *testing.T) {
	fake := &fakeNATSPublisher{}
	orig := newNATSConnFn
	newNATSConnFn = func(_ nats.Options) (natsPublisher, error) { return fake, nil }
	defer func() { newNATSConnFn = orig }()

	x := newTestXTCP(t, "nats:127.0.0.1:4222")
	d, err := newNATSDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newNATSDest err = %v", err)
	}
	if d == nil {
		t.Fatal("dest nil")
	}
	_ = d.Close()
}

// TestNewNATSDest_factoryErr drives the constructor's error-wrap path.
func TestNewNATSDest_factoryErr(t *testing.T) {
	orig := newNATSConnFn
	newNATSConnFn = func(_ nats.Options) (natsPublisher, error) {
		return nil, errors.New("synthetic")
	}
	defer func() { newNATSConnFn = orig }()

	x := newTestXTCP(t, "nats:127.0.0.1:4222")
	d, err := newNATSDest(context.Background(), x)
	if err == nil {
		t.Error("expected err")
	}
	if d != nil {
		t.Error("dest should be nil")
	}
	if !strings.Contains(err.Error(), "opts.Connect") {
		t.Errorf("err = %q, want substring 'opts.Connect'", err)
	}
}

// TestNewNATSDest_debugLog covers the debug-log gate in newNATSDest.
func TestNewNATSDest_debugLog(t *testing.T) {
	fake := &fakeNATSPublisher{}
	orig := newNATSConnFn
	newNATSConnFn = func(_ nats.Options) (natsPublisher, error) { return fake, nil }
	defer func() { newNATSConnFn = orig }()

	x := newTestXTCP(t, "nats:127.0.0.1:4222")
	x.debugLevel = 11
	d, err := newNATSDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newNATSDest err = %v", err)
	}
	_ = d.Close()
}
