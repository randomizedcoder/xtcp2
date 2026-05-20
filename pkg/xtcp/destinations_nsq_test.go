//go:build dest_nsq

package xtcp

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	nsq "github.com/nsqio/go-nsq"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// destinations_nsq_test.go exercises pkg/xtcp/destinations_nsq.go
// under the `dest_nsq` build tag. Scope mirrors P2.1/P2.2: cover
// what's testable without a real nsqd. The end-to-end Publish flow
// runs against a real nsqd inside the microvm lifecycle harness.
//
// Conveniently, nsq.NewProducer is lazy — it validates the addr
// format but does not connect until Publish() is called — so the
// constructor and Close path are unit-testable. Send is testable
// only in the failure direction (no broker → returns error).

// ───────────────────────────────────────────────────────────────────────
// init() side effect
// ───────────────────────────────────────────────────────────────────────

func TestNSQDest_initRegistersScheme(t *testing.T) {
	if !IsKnownScheme(schemeNsq) {
		t.Errorf("scheme %q should be registered under build tag dest_nsq", schemeNsq)
	}
	_, status := lookupDestinationFactory(schemeNsq)
	if status != destLookupFound {
		t.Errorf("lookupDestinationFactory(%q) status = %d, want destLookupFound", schemeNsq, status)
	}
}

// ───────────────────────────────────────────────────────────────────────
// newNSQDest — happy path constructor + error path
// ───────────────────────────────────────────────────────────────────────

func TestNewNSQDest_table(t *testing.T) {
	cases := []struct {
		name     string
		category string
		dest     string
	}{
		{"positive_host_port", "positive", "nsq:127.0.0.1:4150"},
		{"positive_localhost", "positive", "nsq:localhost:4150"},
		{"boundary_high_port", "boundary", "nsq:127.0.0.1:65535"},
		// Documented permissive behaviour: nsq.NewProducer doesn't dial
		// at construction time and accepts almost any addr string;
		// errors surface later via Publish. Pin that — a future
		// NewProducer that pre-validates would catch these rows.
		{"corner_empty_after_prefix", "corner", "nsq:"},
		{"corner_addr_without_port", "corner", "nsq:127.0.0.1"},
		{"adversarial_only_colon", "adversarial", "nsq::"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			x := newTestXTCP(t, tc.dest)
			d, err := newNSQDest(context.Background(), x)
			if err != nil {
				t.Errorf("newNSQDest err = %v; current NSQ behaviour is permissive at construction time", err)
			}
			if d != nil {
				_ = d.Close()
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// Close-on-nil-producer must not panic.
// ───────────────────────────────────────────────────────────────────────

func TestNSQDest_CloseNilProducer(t *testing.T) {
	d := &nsqDest{x: newTestXTCP(t, "nsq:127.0.0.1:4150"), producer: nil}
	if err := d.Close(); err != nil {
		t.Errorf("Close on nil producer should be nil; got %v", err)
	}
}

// ───────────────────────────────────────────────────────────────────────
// Send against an unreachable broker. The Publish call is synchronous
// — it dials, fails, returns the error. We pin: the metric counter
// for the error path is incremented exactly once.
// ───────────────────────────────────────────────────────────────────────

func TestNSQDest_SendUnreachableIncrementsErrCounter(t *testing.T) {
	x := newTestXTCP(t, "nsq:127.0.0.1:1") // port 1 → refused
	x.config.Topic = "xtcp-test"
	d, err := newNSQDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newNSQDest: %v", err)
	}
	defer func() { _ = d.Close() }()

	payload := []byte("hello")
	n, sendErr := d.Send(context.Background(), &payload)
	if sendErr == nil {
		t.Error("expected Send to err against unreachable broker")
	}
	if n != 0 {
		t.Errorf("n = %d on error, want 0", n)
	}
	got := testutil.ToFloat64(x.pC.WithLabelValues("destNSQ", "Publish", "error"))
	if got != 1 {
		t.Errorf("err counter = %v, want 1", got)
	}
}

// ───────────────────────────────────────────────────────────────────────
// "nsq:" scheme stripping — verified via a fake listener that accepts
// the TCP connection NSQ producer attempts when Publish is called.
// ───────────────────────────────────────────────────────────────────────

func TestNewNSQDest_stripsScheme(t *testing.T) {
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

	x := newTestXTCP(t, "nsq:"+ln.Addr().String())
	x.config.Topic = "xtcp-test"
	d, err := newNSQDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newNSQDest: %v", err)
	}
	defer func() { _ = d.Close() }()

	// Trigger an actual dial by calling Publish; we don't care if it
	// completes — we only want to see the listener accept.
	payload := []byte("ping")
	go func() { _, _ = d.Send(context.Background(), &payload) }()

	select {
	case <-connected:
		// dial reached the stripped host:port
	case <-time.After(3 * time.Second):
		t.Fatal("nsq producer did not dial the stripped host:port within 3s")
	}
}

// ───────────────────────────────────────────────────────────────────────
// Race
// ───────────────────────────────────────────────────────────────────────

func TestNSQDest_concurrentCloseOnNil(t *testing.T) {
	const goroutines = 16
	var wg sync.WaitGroup
	var calls atomic.Int64
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				d := &nsqDest{x: newTestXTCP(t, "nsq:127.0.0.1:4150"), producer: nil}
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

func BenchmarkNSQDest_NewAndClose(b *testing.B) {
	x := newTestXTCP(&testing.T{}, "nsq:127.0.0.1:4150")
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		d, _ := newNSQDest(context.Background(), x)
		if d != nil {
			_ = d.Close()
		}
	}
}

// ───────────────────────────────────────────────────────────────────────
// fakeNSQProducer + Send/Close tests via the nsqProducer interface seam.
// ───────────────────────────────────────────────────────────────────────

type fakeNSQProducer struct {
	publishErr error
	publishes  atomic.Int64
	stops      atomic.Int64
	lastTopic  string
	lastBody   []byte
}

func (f *fakeNSQProducer) Publish(topic string, body []byte) error {
	f.publishes.Add(1)
	f.lastTopic = topic
	f.lastBody = append(f.lastBody[:0], body...)
	return f.publishErr
}
func (f *fakeNSQProducer) Stop() { f.stops.Add(1) }

func newNSQDestForTest(t *testing.T, fake *fakeNSQProducer) *nsqDest {
	t.Helper()
	x := newTestXTCP(t, "nsq:127.0.0.1:4150")
	x.config.Topic = "xtcp-test"
	return &nsqDest{x: x, producer: fake}
}

func TestNSQDest_Send_table(t *testing.T) {
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
		{"negative_publish_err", "negative", errors.New("broker EOF"), 0, true, 0, 1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeNSQProducer{publishErr: tc.publishErr}
			d := newNSQDestForTest(t, fake)
			payload := []byte("payload")
			n, err := d.Send(context.Background(), &payload)
			if n != tc.wantN {
				t.Errorf("n = %d, want %d", n, tc.wantN)
			}
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if fake.lastTopic != "xtcp-test" {
				t.Errorf("topic = %q, want xtcp-test", fake.lastTopic)
			}
			gotOK := testutil.ToFloat64(d.x.pC.WithLabelValues("destNSQ", "Publish", "count"))
			gotErr := testutil.ToFloat64(d.x.pC.WithLabelValues("destNSQ", "Publish", "error"))
			if gotOK != tc.wantOKCounter {
				t.Errorf("OK counter = %v, want %v", gotOK, tc.wantOKCounter)
			}
			if gotErr != tc.wantErrCounter {
				t.Errorf("Err counter = %v, want %v", gotErr, tc.wantErrCounter)
			}
		})
	}
}

func TestNSQDest_Send_debugLog(t *testing.T) {
	fake := &fakeNSQProducer{}
	d := newNSQDestForTest(t, fake)
	d.x.debugLevel = 11
	payload := []byte("x")
	_, _ = d.Send(context.Background(), &payload)
}

func TestNSQDest_Close_stopsProducer(t *testing.T) {
	fake := &fakeNSQProducer{}
	d := newNSQDestForTest(t, fake)
	if err := d.Close(); err != nil {
		t.Errorf("Close err = %v, want nil", err)
	}
	if fake.stops.Load() != 1 {
		t.Errorf("Stop calls = %d, want 1", fake.stops.Load())
	}
}

// TestNewNSQDest_happy drives the constructor via the newNSQProducerFn
// factory seam.
func TestNewNSQDest_happy(t *testing.T) {
	fake := &fakeNSQProducer{}
	orig := newNSQProducerFn
	newNSQProducerFn = func(_ string, _ *nsq.Config) (nsqProducer, error) { return fake, nil }
	defer func() { newNSQProducerFn = orig }()

	x := newTestXTCP(t, "nsq:127.0.0.1:4150")
	d, err := newNSQDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newNSQDest err = %v", err)
	}
	if d == nil {
		t.Fatal("dest nil")
	}
	_ = d.Close()
}

// TestNewNSQDest_factoryErr drives the error-wrap branch.
func TestNewNSQDest_factoryErr(t *testing.T) {
	orig := newNSQProducerFn
	newNSQProducerFn = func(_ string, _ *nsq.Config) (nsqProducer, error) {
		return nil, errors.New("synthetic")
	}
	defer func() { newNSQProducerFn = orig }()

	x := newTestXTCP(t, "nsq:127.0.0.1:4150")
	d, err := newNSQDest(context.Background(), x)
	if err == nil {
		t.Error("expected err")
	}
	if d != nil {
		t.Error("dest should be nil")
	}
}

// TestNewNSQDest_debugLog covers the debug-log gate.
func TestNewNSQDest_debugLog(t *testing.T) {
	fake := &fakeNSQProducer{}
	orig := newNSQProducerFn
	newNSQProducerFn = func(_ string, _ *nsq.Config) (nsqProducer, error) { return fake, nil }
	defer func() { newNSQProducerFn = orig }()

	x := newTestXTCP(t, "nsq:127.0.0.1:4150")
	x.debugLevel = 11
	d, err := newNSQDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newNSQDest err = %v", err)
	}
	_ = d.Close()
}
