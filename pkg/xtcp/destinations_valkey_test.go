//go:build dest_valkey

package xtcp

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
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

// ───────────────────────────────────────────────────────────────────────
// fakeValkeyPublisher + Send/Close tests via the valkeyPublisher
// interface seam. Same shape as fakeKafkaProducer / fakeNATSPublisher
// / fakeNSQProducer above.
// ───────────────────────────────────────────────────────────────────────

type fakeValkeyPublisher struct {
	publishErr error
	pingErr    error
	closeErr   error
	publishes  atomic.Int64
	pings      atomic.Int64
	closes     atomic.Int64
	lastChan   string
	lastMsg    []byte
}

func (f *fakeValkeyPublisher) Publish(_ context.Context, channel string, msg []byte) error {
	f.publishes.Add(1)
	f.lastChan = channel
	f.lastMsg = append(f.lastMsg[:0], msg...)
	return f.publishErr
}
func (f *fakeValkeyPublisher) Ping(_ context.Context) error { f.pings.Add(1); return f.pingErr }
func (f *fakeValkeyPublisher) Close() error                  { f.closes.Add(1); return f.closeErr }

func newValkeyDestForTest(t *testing.T, fake *fakeValkeyPublisher) *valkeyDest {
	t.Helper()
	x := newTestXTCP(t, "valkey:127.0.0.1:6379")
	x.config.Topic = "xtcp-test"
	return &valkeyDest{x: x, client: fake}
}

func TestValkeyDest_Send_table(t *testing.T) {
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
		{"negative_publish_err", "negative", errors.New("connection refused"), 0, true, 0, 1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeValkeyPublisher{publishErr: tc.publishErr}
			d := newValkeyDestForTest(t, fake)
			payload := []byte("payload")
			n, err := d.Send(context.Background(), &payload)
			if n != tc.wantN {
				t.Errorf("n = %d, want %d", n, tc.wantN)
			}
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if fake.lastChan != "xtcp-test" {
				t.Errorf("channel = %q, want xtcp-test", fake.lastChan)
			}
			gotOK := testutil.ToFloat64(d.x.pC.WithLabelValues("destValKey", "Publish", "count"))
			gotErr := testutil.ToFloat64(d.x.pC.WithLabelValues("destValKey", "Publish", "error"))
			if gotOK != tc.wantOKCounter {
				t.Errorf("OK counter = %v, want %v", gotOK, tc.wantOKCounter)
			}
			if gotErr != tc.wantErrCounter {
				t.Errorf("Err counter = %v, want %v", gotErr, tc.wantErrCounter)
			}
		})
	}
}

func TestValkeyDest_Send_debugLog(t *testing.T) {
	fake := &fakeValkeyPublisher{}
	d := newValkeyDestForTest(t, fake)
	d.x.debugLevel = 11
	payload := []byte("x")
	_, _ = d.Send(context.Background(), &payload)
}

func TestValkeyDest_Close_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		closeErr error
	}{
		{"positive_clean_close", "positive", nil},
		{"negative_close_err", "negative", errors.New("close failed")},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeValkeyPublisher{closeErr: tc.closeErr}
			d := newValkeyDestForTest(t, fake)
			err := d.Close()
			if (err != nil) != (tc.closeErr != nil) {
				t.Errorf("Close err = %v, want non-nil=%v", err, tc.closeErr != nil)
			}
			if fake.closes.Load() != 1 {
				t.Errorf("Close calls = %d, want 1", fake.closes.Load())
			}
		})
	}
}

// TestValkeyDest_RedisClientAdapter pins the production adapter wraps
// a real *redis.Client through the interface. The adapter itself
// can't deeply exercise the real client without a server, but the
// type check + factory call should still work.
func TestValkeyDest_RedisClientAdapter_satisfiesIface(t *testing.T) {
	adapter := &redisClientAdapter{c: nil}
	var _ valkeyPublisher = adapter
}

// TestNewValKeyDest_happy drives the constructor end-to-end via the
// newValkeyClientFn factory seam. Fake Ping succeeds so the
// constructor returns a fully-built dest.
func TestNewValKeyDest_happy(t *testing.T) {
	fake := &fakeValkeyPublisher{}
	orig := newValkeyClientFn
	newValkeyClientFn = func(_ string) valkeyPublisher { return fake }
	defer func() { newValkeyClientFn = orig }()

	x := newTestXTCP(t, "valkey:127.0.0.1:6379")
	d, err := newValKeyDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newValKeyDest err = %v", err)
	}
	if d == nil {
		t.Fatal("dest nil")
	}
	if fake.pings.Load() != 1 {
		t.Errorf("pings = %d, want 1", fake.pings.Load())
	}
	_ = d.Close()
}

// TestNewValKeyDest_pingErr drives the constructor's ping-fails-→
// return-err branch.
func TestNewValKeyDest_pingErr(t *testing.T) {
	fake := &fakeValkeyPublisher{pingErr: errors.New("refused")}
	orig := newValkeyClientFn
	newValkeyClientFn = func(_ string) valkeyPublisher { return fake }
	defer func() { newValkeyClientFn = orig }()

	x := newTestXTCP(t, "valkey:127.0.0.1:6379")
	d, err := newValKeyDest(context.Background(), x)
	if err == nil {
		t.Error("expected ping err")
	}
	if d != nil {
		t.Error("dest should be nil on ping err")
	}
}

// TestNewValKeyDest_debugLog covers the debug-log gate during
// successful construction.
func TestNewValKeyDest_debugLog(t *testing.T) {
	fake := &fakeValkeyPublisher{}
	orig := newValkeyClientFn
	newValkeyClientFn = func(_ string) valkeyPublisher { return fake }
	defer func() { newValkeyClientFn = orig }()

	x := newTestXTCP(t, "valkey:127.0.0.1:6379")
	x.debugLevel = 11
	d, err := newValKeyDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newValKeyDest err = %v", err)
	}
	_ = d.Close()
}
