//go:build dest_kafka

package xtcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/twmb/franz-go/pkg/kgo"
	"google.golang.org/protobuf/types/known/durationpb"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// destinations_kafka_test.go exercises pkg/xtcp/destinations_kafka.go
// — only built under the `dest_kafka` build tag, which the
// `nix build .#test-go-flavor-kafka` target sets explicitly. The
// default `go test ./...` skips this file entirely (matches the
// production behaviour: only kafka-flavor builds compile it in).
//
// Scope: tests the helpers that DON'T require a real Kafka broker
// (schema-registry HTTP exchange, init-time registration in the
// destinations dispatch table, struct constants). The broker-bound
// helpers (newKafkaDest end-to-end, Send, Close, pingKafkaWithRetries
// against a real client) need a real broker and are covered by the
// microvm lifecycle harness later — out of scope here.

// ───────────────────────────────────────────────────────────────────────
// init() side effect — dispatch table contains "kafka" with this build
// tag set. Pin so a future RegisterDestination rename in
// destinations_core.go fails this test loudly.
// ───────────────────────────────────────────────────────────────────────

func TestKafkaDest_initRegistersScheme(t *testing.T) {
	if !IsKnownScheme(schemeKafka) {
		t.Errorf("scheme %q should be registered under build tag dest_kafka", schemeKafka)
	}
	_, status := lookupDestinationFactory(schemeKafka)
	if status != destLookupFound {
		t.Errorf("lookupDestinationFactory(%q) status = %d, want destLookupFound", schemeKafka, status)
	}
}

// ───────────────────────────────────────────────────────────────────────
// KafkaHeaderSizeCst — constant that the wire-format depends on. Pin
// against accidental tweaks.
// ───────────────────────────────────────────────────────────────────────

func TestKafkaHeaderSizeCst(t *testing.T) {
	// Confluent's protobuf wire format prefixes records with a 1-byte
	// magic + 4-byte schema ID + 1-byte first-message-index varint = 6.
	// See https://docs.confluent.io/platform/current/schema-registry/fundamentals/serdes-develop/index.html#wire-format
	if KafkaHeaderSizeCst != 6 {
		t.Errorf("KafkaHeaderSizeCst = %d, want 6", KafkaHeaderSizeCst)
	}
}

// ───────────────────────────────────────────────────────────────────────
// newKafkaDestFixture — assembles an XTCP whose KafkaSchemaUrl points
// at the given httptest.Server and whose XtcpProtoFile is a tempfile
// containing the supplied proto-source bytes. Reusable across the
// schema-registry tests below.
// ───────────────────────────────────────────────────────────────────────

func newKafkaDestFixture(t *testing.T, schemaSrv *httptest.Server, protoSrc string) *XTCP {
	t.Helper()
	x := newTestXTCP(t, "kafka:127.0.0.1:9092")
	x.config.Topic = "xtcp-test"
	x.config.KafkaSchemaUrl = schemaSrv.URL

	tmp := filepath.Join(t.TempDir(), "x.proto")
	if err := os.WriteFile(tmp, []byte(protoSrc), 0o600); err != nil {
		t.Fatalf("write tmp proto: %v", err)
	}
	x.config.XtcpProtoFile = tmp
	return x
}

// ───────────────────────────────────────────────────────────────────────
// getLatestSchemaID — table-driven against an httptest.Server. Covers
// the four meaningful response shapes: 200/ok, 404, other 5xx, and
// malformed JSON.
// ───────────────────────────────────────────────────────────────────────

func TestGetLatestSchemaID_table(t *testing.T) {
	cases := []struct {
		name     string
		category string
		handler  http.HandlerFunc
		wantID   int
		wantErr  bool
	}{
		{
			name:     "positive_200_with_id",
			category: "positive",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]int{"id": 42})
			},
			wantID: 42,
		},
		{
			name:     "positive_200_id_zero",
			category: "positive",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]int{"id": 0})
			},
			wantID: 0,
		},
		{
			name:     "negative_404_returns_err",
			category: "negative",
			handler:  func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNotFound) },
			wantErr:  true,
		},
		{
			name:     "negative_500_returns_err",
			category: "negative",
			handler:  func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
			wantErr:  true,
		},
		{
			name:     "boundary_300_redirect_unexpected_status",
			category: "boundary",
			handler:  func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusMultipleChoices) },
			wantErr:  true,
		},
		{
			name:     "corner_malformed_json",
			category: "corner",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("not json"))
			},
			wantErr: true,
		},
		{
			name:     "corner_empty_body_200",
			category: "corner",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			wantErr: true, // empty body fails json.Decode
		},
		{
			name:     "adversarial_giant_id_int_overflow_safe",
			category: "adversarial",
			handler: func(w http.ResponseWriter, r *http.Request) {
				// Very large JSON number; Go's int64 fits anything < 2^63.
				_ = json.NewEncoder(w).Encode(map[string]int64{"id": 1 << 62})
			},
			wantID: 1 << 62,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()
			x := newKafkaDestFixture(t, srv, "syntax = \"proto3\";")
			d := &kafkaDest{x: x}
			gotID, err := d.getLatestSchemaID(context.Background())
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if !tc.wantErr && gotID != tc.wantID {
				t.Errorf("id = %d, want %d", gotID, tc.wantID)
			}
		})
	}
}

// TestGetLatestSchemaID_buildsURL pins the URL shape so a refactor of
// the schema-registry endpoint pattern fails this test loudly.
func TestGetLatestSchemaID_buildsURL(t *testing.T) {
	var sawPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]int{"id": 1})
	}))
	defer srv.Close()
	x := newKafkaDestFixture(t, srv, "")
	x.config.Topic = "my-topic"
	d := &kafkaDest{x: x}
	if _, err := d.getLatestSchemaID(context.Background()); err != nil {
		t.Fatal(err)
	}
	wantPath := "/subjects/my-topic-value/versions/latest"
	if sawPath != wantPath {
		t.Errorf("URL path = %q, want %q", sawPath, wantPath)
	}
}

// TestGetLatestSchemaID_ctxCancel verifies the 10s ceiling honours
// caller ctx cancel.
func TestGetLatestSchemaID_ctxCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Sleep longer than the test's ctx timeout so the request is
		// aborted via ctx.Done rather than reaching us.
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	x := newKafkaDestFixture(t, srv, "")
	d := &kafkaDest{x: x}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := d.getLatestSchemaID(ctx)
	if err == nil {
		t.Error("expected error on ctx-cancel")
	}
	if elapsed := time.Since(start); elapsed > 1*time.Second {
		t.Errorf("returned in %v; ctx-cancel should be < 1s", elapsed)
	}
}

// ───────────────────────────────────────────────────────────────────────
// registerProtobufSchema — table-driven via sr.Client → httptest.Server.
// The franz-go sr package POSTs to /subjects/<sub>/versions and decodes
// the {"id": N} response.
// ───────────────────────────────────────────────────────────────────────

// schemaRegistryHandler returns a path-aware HTTP handler that mimics
// the three endpoints franz-go's sr.CreateSchema touches:
//
//	POST /subjects/<sub>/versions          → returns {"id": N}
//	GET  /schemas/ids/<N>/versions         → returns [{subject, version}]
//	GET  /subjects/<sub>/versions/<ver>    → returns full SubjectSchema
//
// The `createStatus` arg lets a test override the POST response code
// to drive error paths; the GET endpoints stay well-formed so the
// happy-path tests see exactly the failure surface they're aiming at.
//
// The matching uses substring/prefix checks since franz-go varies
// minor path details across versions.
func schemaRegistryHandler(id int, createStatus int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/subjects/") && strings.HasSuffix(r.URL.Path, "/versions"):
			if createStatus != 0 && createStatus != http.StatusOK {
				w.WriteHeader(createStatus)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/schemas/ids/"):
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"subject": "xtcp-test-value", "version": 1},
			})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/subjects/"):
			// GET /subjects/<sub>/versions/<ver> → full SubjectSchema
			_ = json.NewEncoder(w).Encode(map[string]any{
				"subject":    "xtcp-test-value",
				"version":    1,
				"id":         id,
				"schema":     `syntax = "proto3"; message M {}`,
				"schemaType": "PROTOBUF",
			})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func TestRegisterProtobufSchema_table(t *testing.T) {
	const protoSrc = `syntax = "proto3";
package test;
message M { string a = 1; }`
	cases := []struct {
		name     string
		category string
		handler  http.HandlerFunc
		wantErr  bool
		wantID   int
	}{
		{
			name:     "positive_register_returns_id",
			category: "positive",
			handler:  schemaRegistryHandler(7, 0),
			wantID:   7,
		},
		{
			name:     "negative_4xx_error",
			category: "negative",
			handler:  schemaRegistryHandler(0, http.StatusBadRequest),
			wantErr:  true,
		},
		{
			name:     "negative_5xx_error",
			category: "negative",
			handler:  schemaRegistryHandler(0, http.StatusServiceUnavailable),
			wantErr:  true,
		},
		{
			name:     "boundary_id_zero",
			category: "boundary",
			handler:  schemaRegistryHandler(0, 0),
			wantID:   0,
		},
		{
			name:     "corner_malformed_json_response",
			category: "corner",
			handler: func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("garbage"))
			},
			wantErr: true,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()
			x := newKafkaDestFixture(t, srv, protoSrc)
			d := &kafkaDest{x: x}
			err := d.registerProtobufSchema(context.Background())
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if !tc.wantErr && d.schemaID != tc.wantID {
				t.Errorf("schemaID = %d, want %d", d.schemaID, tc.wantID)
			}
		})
	}
}

// TestRegisterProtobufSchema_missingProtoFile pins the disk-read error
// branch: a non-existent XtcpProtoFile must surface as an err, not a
// panic or silent zero-schema-id.
func TestRegisterProtobufSchema_missingProtoFile(t *testing.T) {
	srv := httptest.NewServer(schemaRegistryHandler(1, 0))
	defer srv.Close()
	x := newKafkaDestFixture(t, srv, "")
	x.config.XtcpProtoFile = "/no/such/file/proto/xtcp.proto"
	d := &kafkaDest{x: x}
	err := d.registerProtobufSchema(context.Background())
	if err == nil {
		t.Error("expected error on missing proto file")
	}
	if !strings.Contains(err.Error(), "read proto") {
		t.Errorf("err = %v, want substring 'read proto'", err)
	}
}

// pingKafkaWithRetries — retry loop with ctx-cancel awareness — is
// tightly coupled to d.client.Ping() (a method on a kgo.Client built
// against a real broker). Without an interface seam we can't drive it
// cleanly in unit tests; the lifecycle microvm harness covers it
// against a real broker. A future refactor that extracts a pingFunc
// seam would let us cover the retry + ctx-cancel logic here.

// ───────────────────────────────────────────────────────────────────────
// Race — drive the pure HTTP helpers concurrently.
// ───────────────────────────────────────────────────────────────────────

func TestKafkaSchemaRegistryHelpers_concurrent(t *testing.T) {
	const protoSrc = `syntax = "proto3"; package test; message M {}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 1})
	}))
	defer srv.Close()

	const goroutines = 16
	var wg sync.WaitGroup
	var calls atomic.Int64
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			x := newKafkaDestFixture(t, srv, protoSrc)
			d := &kafkaDest{x: x}
			for j := 0; j < 20; j++ {
				_, _ = d.getLatestSchemaID(context.Background())
				calls.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := calls.Load(); got != goroutines*20 {
		t.Errorf("calls = %d, want %d", got, goroutines*20)
	}
}

// ───────────────────────────────────────────────────────────────────────
// Benchmarks
// ───────────────────────────────────────────────────────────────────────

func BenchmarkGetLatestSchemaID(b *testing.B) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"id":42}`)
	}))
	defer srv.Close()
	x := newKafkaDestFixture(&testing.T{}, srv, "")
	d := &kafkaDest{x: x}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = d.getLatestSchemaID(ctx)
	}
}

// ───────────────────────────────────────────────────────────────────────
// fakeKafkaProducer — implements the kafkaProducer interface so Send /
// Close / pingKafkaWithRetries run without a real broker.
// ───────────────────────────────────────────────────────────────────────

type fakeKafkaProducer struct {
	produceErr  error
	pingErr     error
	flushErr    error
	produces    atomic.Int64
	flushes     atomic.Int64
	closes      atomic.Int64
	pings       atomic.Int64
	allowRebals atomic.Int64
	// failFirstNPings makes the first N Ping calls return pingErr,
	// then subsequent calls succeed. Lets tests drive the
	// pingKafkaWithRetries retry path then recovery.
	failFirstNPings int
}

func (f *fakeKafkaProducer) Produce(_ context.Context, r *kgo.Record, cb func(*kgo.Record, error)) {
	f.produces.Add(1)
	if cb != nil {
		cb(r, f.produceErr)
	}
}
func (f *fakeKafkaProducer) Flush(_ context.Context) error { f.flushes.Add(1); return f.flushErr }
func (f *fakeKafkaProducer) Close()                        { f.closes.Add(1) }
func (f *fakeKafkaProducer) Ping(_ context.Context) error {
	n := f.pings.Add(1)
	if f.failFirstNPings > 0 && int(n) <= f.failFirstNPings {
		return f.pingErr
	}
	return nil
}
func (f *fakeKafkaProducer) AllowRebalance() { f.allowRebals.Add(1) }

// newKafkaDestForTest assembles a kafkaDest with the fake producer +
// a sync.Pool of kgo.Record and a populated x.destBytesPool so Send's
// pool-return path runs cleanly.
func newKafkaDestForTest(t *testing.T, fake *fakeKafkaProducer) *kafkaDest {
	t.Helper()
	x := newTestXTCP(t, "kafka:127.0.0.1:9092")
	x.config.Topic = "xtcp-test"
	x.destBytesPool = sync.Pool{New: func() any { b := make([]byte, 0, 128); return &b }}
	d := &kafkaDest{
		x:      x,
		client: fake,
		recordPool: sync.Pool{
			New: func() any { return new(kgo.Record) },
		},
	}
	return d
}

// ───────────────────────────────────────────────────────────────────────
// Send
// ───────────────────────────────────────────────────────────────────────

func TestKafkaDest_Send_table(t *testing.T) {
	cases := []struct {
		name           string
		category       string
		produceErr     error
		produceTimeout time.Duration
		wantOKCounter  float64
		wantErrCounter float64
	}{
		{"positive_clean_produce", "positive", nil, 0, 1, 0},
		{"positive_with_produce_timeout", "positive", nil, 100 * time.Millisecond, 1, 0},
		{"negative_produce_err_bumps_error", "negative", errors.New("broker EOF"), 0, 0, 1},
		{"boundary_zero_timeout_uses_ctx_directly", "boundary", nil, 0, 1, 0},
		{"corner_long_produce_timeout", "corner", nil, 24 * time.Hour, 1, 0},
		{"adversarial_huge_produce_error_string", "adversarial",
			errors.New(strings.Repeat("e", 1<<16)), 0, 0, 1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			fake := &fakeKafkaProducer{produceErr: tc.produceErr}
			d := newKafkaDestForTest(t, fake)
			if tc.produceTimeout > 0 {
				d.x.config.KafkaProduceTimeout = durationpb.New(tc.produceTimeout)
			}
			// destBytesPool.Put requires the caller to pass a *[]byte;
			// allocate one here so Send's pool-return path runs.
			buf := d.x.destBytesPool.Get().(*[]byte)
			*buf = append((*buf)[:0], []byte("payload")...)
			n, err := d.Send(context.Background(), buf)
			if err != nil {
				t.Fatalf("Send err = %v (Send itself never errors; only the callback does)", err)
			}
			if n != 1 {
				t.Errorf("n = %d, want 1", n)
			}
			if fake.produces.Load() != 1 {
				t.Errorf("Produce calls = %d, want 1", fake.produces.Load())
			}
			gotOK := testutil.ToFloat64(d.x.pC.WithLabelValues("destKafka", "Produce", "count"))
			gotErr := testutil.ToFloat64(d.x.pC.WithLabelValues("destKafka", "Produce", "error"))
			if gotOK != tc.wantOKCounter {
				t.Errorf("OK counter = %v, want %v", gotOK, tc.wantOKCounter)
			}
			if gotErr != tc.wantErrCounter {
				t.Errorf("Err counter = %v, want %v", gotErr, tc.wantErrCounter)
			}
		})
	}
}

// TestKafkaDest_Send_debugLog covers the debugLevel>10 branch.
func TestKafkaDest_Send_debugLog(t *testing.T) {
	fake := &fakeKafkaProducer{produceErr: errors.New("err")}
	d := newKafkaDestForTest(t, fake)
	d.x.debugLevel = 11
	buf := d.x.destBytesPool.Get().(*[]byte)
	*buf = append((*buf)[:0], []byte("x")...)
	_, _ = d.Send(context.Background(), buf)
}

// ───────────────────────────────────────────────────────────────────────
// Close
// ───────────────────────────────────────────────────────────────────────

func TestKafkaDest_Close_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		flushErr error
	}{
		{"positive_clean_close_flushes_then_closes", "positive", nil},
		{"negative_flush_err_still_closes", "negative", errors.New("flush failed")},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			fake := &fakeKafkaProducer{flushErr: tc.flushErr}
			d := newKafkaDestForTest(t, fake)
			if err := d.Close(); err != nil {
				t.Errorf("Close err = %v, want nil", err)
			}
			if fake.flushes.Load() != 1 {
				t.Errorf("Flush calls = %d, want 1", fake.flushes.Load())
			}
			if fake.closes.Load() != 1 {
				t.Errorf("Close calls = %d, want 1", fake.closes.Load())
			}
		})
	}
}

// TestKafkaDest_CloseNilClient pins the safety check (d.client != nil).
func TestKafkaDest_CloseNilClient(t *testing.T) {
	x := newTestXTCP(t, "kafka:127.0.0.1:9092")
	d := &kafkaDest{x: x, client: nil}
	if err := d.Close(); err != nil {
		t.Errorf("Close on nil client should be nil; got %v", err)
	}
}

// TestKafkaDest_Close_debugLog covers the debugLevel>10 branch in
// the FlushOnClose error path.
func TestKafkaDest_Close_debugLog(t *testing.T) {
	fake := &fakeKafkaProducer{flushErr: errors.New("flush")}
	d := newKafkaDestForTest(t, fake)
	d.x.debugLevel = 11
	_ = d.Close()
}

// ───────────────────────────────────────────────────────────────────────
// pingKafkaWithRetries — drives the retry loop via failFirstNPings.
// ───────────────────────────────────────────────────────────────────────

func TestPingKafkaWithRetries_table(t *testing.T) {
	cases := []struct {
		name           string
		category       string
		failFirstN     int
		retries        int
		wantErr        bool
		wantTotalPings int
	}{
		{"positive_first_ping_succeeds", "positive", 0, 3, false, 1},
		{"positive_third_ping_recovers", "positive", 2, 5, false, 3},
		{"negative_all_pings_fail", "negative", 5, 3, true, 3},
		{"boundary_retries_zero", "boundary", 0, 0, false, 0},
		{"corner_exact_match_recovers", "corner", 2, 3, false, 3},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			fake := &fakeKafkaProducer{
				pingErr:         errors.New("connection refused"),
				failFirstNPings: tc.failFirstN,
			}
			d := newKafkaDestForTest(t, fake)
			err := d.pingKafkaWithRetries(context.Background(), tc.retries, 1*time.Microsecond)
			if (err != nil) != tc.wantErr {
				t.Errorf("err = %v, wantErr = %v", err, tc.wantErr)
			}
			if int(fake.pings.Load()) != tc.wantTotalPings {
				t.Errorf("ping calls = %d, want %d", fake.pings.Load(), tc.wantTotalPings)
			}
		})
	}
}

// TestPingKafkaWithRetries_ctxCancelAbortsSleep verifies the
// ctx-cancel-during-sleep branch.
func TestPingKafkaWithRetries_ctxCancelAbortsSleep(t *testing.T) {
	fake := &fakeKafkaProducer{
		pingErr:         errors.New("refused"),
		failFirstNPings: 100, // always fail
	}
	d := newKafkaDestForTest(t, fake)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancelled → first sleep aborts
	err := d.pingKafkaWithRetries(ctx, 10, 100*time.Millisecond)
	if err == nil {
		t.Error("expected ctx-cancel err")
	}
	// First ping fired, then ctx-cancel aborted the sleep before the
	// next ping. Want pings ≤ 2 (some implementations might let the
	// second ping run before checking ctx).
	if got := fake.pings.Load(); got > 2 {
		t.Errorf("pings = %d, want ≤ 2 (ctx-cancel should abort retries)", got)
	}
}

// ───────────────────────────────────────────────────────────────────────
// newKafkaDest end-to-end — registers schema, looks up id, builds the
// (fake) producer via newKafkaProducerFn, then pingKafkaWithRetries
// succeeds against the fake. Exercises the constructor's full happy
// path without a real broker.
// ───────────────────────────────────────────────────────────────────────

func TestNewKafkaDest_happy(t *testing.T) {
	const protoSrc = `syntax = "proto3"; package t; message M {}`
	srv := httptest.NewServer(schemaRegistryHandler(7, 0))
	defer srv.Close()

	x := newTestXTCP(t, "kafka:127.0.0.1:9092")
	x.config.Topic = "xtcp-test"
	x.config.KafkaSchemaUrl = srv.URL
	tmp := filepath.Join(t.TempDir(), "x.proto")
	if err := os.WriteFile(tmp, []byte(protoSrc), 0o600); err != nil {
		t.Fatalf("write tmp proto: %v", err)
	}
	x.config.XtcpProtoFile = tmp

	fake := &fakeKafkaProducer{}
	origFactory := newKafkaProducerFn
	newKafkaProducerFn = func(_ ...kgo.Opt) (kafkaProducer, error) { return fake, nil }
	defer func() { newKafkaProducerFn = origFactory }()

	d, err := newKafkaDest(context.Background(), x)
	if err != nil {
		t.Fatalf("newKafkaDest err = %v", err)
	}
	if d == nil {
		t.Fatal("dest is nil")
	}
	if fake.allowRebals.Load() != 1 {
		t.Errorf("AllowRebalance calls = %d, want 1", fake.allowRebals.Load())
	}
	if fake.pings.Load() < 1 {
		t.Errorf("pings = %d, want ≥1", fake.pings.Load())
	}
	_ = d.Close()
}

// TestNewKafkaDest_factoryErr drives the `newKafkaProducerFn err →
// fmt.Errorf("newKafkaDest kgo.NewClient: ...")` branch.
func TestNewKafkaDest_factoryErr(t *testing.T) {
	srv := httptest.NewServer(schemaRegistryHandler(1, 0))
	defer srv.Close()
	x := newTestXTCP(t, "kafka:127.0.0.1:9092")
	x.config.Topic = "xtcp-test"
	x.config.KafkaSchemaUrl = srv.URL
	tmp := filepath.Join(t.TempDir(), "x.proto")
	_ = os.WriteFile(tmp, []byte(`syntax = "proto3";`), 0o600)
	x.config.XtcpProtoFile = tmp

	origFactory := newKafkaProducerFn
	newKafkaProducerFn = func(_ ...kgo.Opt) (kafkaProducer, error) {
		return nil, errors.New("factory failed")
	}
	defer func() { newKafkaProducerFn = origFactory }()

	d, err := newKafkaDest(context.Background(), x)
	if err == nil {
		t.Fatal("expected err on factory failure")
	}
	if d != nil {
		t.Error("dest should be nil on factory err")
	}
	if !strings.Contains(err.Error(), "kgo.NewClient") {
		t.Errorf("err = %q, want substring 'kgo.NewClient'", err)
	}
}

// TestNewKafkaDest_pingFailExhaustsRetries drives the
// pingKafkaWithRetries-exhausted branch via a fake that fails every
// ping. Shrinks pingRetrySleep via stubbed retry count.
func TestNewKafkaDest_pingFailExhaustsRetries(t *testing.T) {
	srv := httptest.NewServer(schemaRegistryHandler(1, 0))
	defer srv.Close()
	x := newTestXTCP(t, "kafka:127.0.0.1:9092")
	x.config.Topic = "xtcp-test"
	x.config.KafkaSchemaUrl = srv.URL
	tmp := filepath.Join(t.TempDir(), "x.proto")
	_ = os.WriteFile(tmp, []byte(`syntax = "proto3";`), 0o600)
	x.config.XtcpProtoFile = tmp

	fake := &fakeKafkaProducer{
		pingErr:         errors.New("refused"),
		failFirstNPings: 100, // always fail
	}
	origFactory := newKafkaProducerFn
	newKafkaProducerFn = func(_ ...kgo.Opt) (kafkaProducer, error) { return fake, nil }
	defer func() { newKafkaProducerFn = origFactory }()

	// The constructor uses kafkaPingRetriesCst (5) + kafkaPingRetrySleepCst (1s).
	// With 5 retries + 1s sleep + jitter, the test wall-clocks ~10s.
	// That's acceptable for one focused test.
	d, err := newKafkaDest(context.Background(), x)
	if err == nil {
		t.Fatal("expected ping-exhaust err")
	}
	if d != nil {
		t.Error("dest should be nil on ping exhaust")
	}
	if !strings.Contains(err.Error(), "pingKafka") {
		t.Errorf("err = %q, want substring 'pingKafka'", err)
	}
}

// TestNewKafkaProducerReal_returnsKgoClient pins that the production
// factory returns a usable *kgo.Client. kgo.NewClient is lazy (no
// dial at construction) so this runs without a broker. The return
// value satisfies the kafkaProducer interface via *kgo.Client's
// concrete methods.
func TestNewKafkaProducerReal_returnsKgoClient(t *testing.T) {
	p, err := newKafkaProducerReal(kgo.SeedBrokers("127.0.0.1:9092"))
	if err != nil {
		t.Fatalf("newKafkaProducerReal err = %v", err)
	}
	if p == nil {
		t.Fatal("producer nil")
	}
	defer p.Close()
}

// TestPingKafkaWithRetries_debugLog covers the debug-log branch.
func TestPingKafkaWithRetries_debugLog(t *testing.T) {
	fake := &fakeKafkaProducer{
		pingErr:         errors.New("refused"),
		failFirstNPings: 2,
	}
	d := newKafkaDestForTest(t, fake)
	d.x.debugLevel = 11
	_ = d.pingKafkaWithRetries(context.Background(), 3, 1*time.Microsecond)
}
