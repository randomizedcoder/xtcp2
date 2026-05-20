//go:build dest_kafka

package xtcp

import (
	"context"
	"encoding/json"
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
