package telemetry

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// TestSetupPrometheus covers the Prometheus bridge: the handler exists only
// when asked for, every instrument the collector records into is exposed under
// its rendered Prometheus name, gauges show the latest value, and two Setups in
// one process do not collide.
//
// go test ./internal/ipfeed/telemetry/ -run TestSetupPrometheus
func TestSetupPrometheus(t *testing.T) {
	src := metric.WithAttributes(attribute.String("source", "gcp-goog"), attribute.String("provider", "gcp"))
	okOutcome := metric.WithAttributes(attribute.String("outcome", "success"))

	tests := []struct {
		description string
		opts        Options
		record      func(ctx context.Context, tel *Telemetry)
		wantHandler bool
		// wantLines are regexps that must each match one line of the /metrics
		// body (label order and the exporter's otel_scope_* labels are not
		// pinned, hence [^}]*).
		wantLines []string
		// wantAbsent are regexps that must match no line.
		wantAbsent []string
	}{
		// negative — default keeps the OTLP-only behaviour
		{"zero Options: no Prometheus handler", Options{}, nil, false, nil, nil},

		// positive — every new instrument renders under its Prometheus name
		{"lookup-table size and build/upload durations are exposed", Options{Prometheus: true},
			func(ctx context.Context, tel *Telemetry) {
				tel.ArtifactRecords.Record(ctx, 42)
				tel.ArtifactBytes.Record(ctx, 4096)
				tel.WriteDuration.Record(ctx, 0.25)
				tel.UploadDuration.Record(ctx, 1.5)
				tel.CycleDuration.Record(ctx, 2.0, okOutcome)
				tel.Cycles.Add(ctx, 1, okOutcome)
			}, true,
			[]string{
				`^ipfeed_artifact_records\{[^}]*\} 42$`,
				`^ipfeed_artifact_size_bytes\{[^}]*\} 4096$`,
				`^ipfeed_write_duration_seconds_count\{[^}]*\} 1$`,
				`^ipfeed_write_duration_seconds_sum\{[^}]*\} 0\.25$`,
				`^ipfeed_upload_duration_seconds_count\{[^}]*\} 1$`,
				`^ipfeed_cycle_duration_seconds_count\{[^}]*outcome="success"[^}]*\} 1$`,
				`^ipfeed_cycles_total\{[^}]*outcome="success"[^}]*\} 1$`,
			},
			[]string{`^ipfeed_artifact_bytes_bytes`}},
		{"per-source record gauge carries source and provider labels", Options{Prometheus: true},
			func(ctx context.Context, tel *Telemetry) {
				tel.SourceRecords.Record(ctx, 3, src)
				tel.FetchDuration.Record(ctx, 0.5, src)
				tel.FetchBytes.Add(ctx, 1234, src)
			}, true,
			[]string{
				`^ipfeed_source_records\{[^}]*provider="gcp"[^}]*source="gcp-goog"[^}]*\} 3$`,
				`^ipfeed_fetch_duration_seconds_count\{[^}]*source="gcp-goog"[^}]*\} 1$`,
				`^ipfeed_fetch_bytes_total\{[^}]*source="gcp-goog"[^}]*\} 1234$`,
			}, nil},

		// boundary — a gauge is last-value, a counter accumulates
		{"gauge shows the latest value, counter the running total", Options{Prometheus: true},
			func(ctx context.Context, tel *Telemetry) {
				tel.ArtifactRecords.Record(ctx, 42)
				tel.ArtifactRecords.Record(ctx, 7)
				tel.Cycles.Add(ctx, 1, okOutcome)
				tel.Cycles.Add(ctx, 1, okOutcome)
			}, true,
			[]string{
				`^ipfeed_artifact_records\{[^}]*\} 7$`,
				`^ipfeed_cycles_total\{[^}]*\} 2$`,
			},
			[]string{`^ipfeed_artifact_records\{[^}]*\} 42$`}},

		// corner — a zero gauge is still a series (a failed source shows 0, not absence)
		{"zero recorded into the source gauge is exposed as 0", Options{Prometheus: true},
			func(ctx context.Context, tel *Telemetry) { tel.SourceRecords.Record(ctx, 0, src) }, true,
			[]string{`^ipfeed_source_records\{[^}]*source="gcp-goog"[^}]*\} 0$`}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			// Never try to reach a real OTLP endpoint from a unit test.
			t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
			ctx := context.Background()
			tel, err := Setup(ctx, "ipfeed-collector-test", tc.opts)
			if err != nil {
				t.Fatalf("Setup: %v", err)
			}
			t.Cleanup(func() {
				if err := tel.Shutdown(context.Background()); err != nil {
					t.Errorf("Shutdown: %v", err)
				}
			})
			if (tel.PrometheusHandler != nil) != tc.wantHandler {
				t.Fatalf("PrometheusHandler != nil = %v, want %v", tel.PrometheusHandler != nil, tc.wantHandler)
			}
			if tc.record != nil {
				tc.record(ctx, tel)
			}
			if !tc.wantHandler {
				return
			}

			rec := httptest.NewRecorder()
			tel.PrometheusHandler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
			if rec.Code != http.StatusOK {
				t.Fatalf("GET /metrics status = %d, want 200", rec.Code)
			}
			body, err := io.ReadAll(rec.Body)
			if err != nil {
				t.Fatal(err)
			}
			for _, pat := range tc.wantLines {
				if !regexp.MustCompile("(?m)" + pat).Match(body) {
					t.Errorf("no line matches %s\n--- body ---\n%s", pat, body)
				}
			}
			for _, pat := range tc.wantAbsent {
				if regexp.MustCompile("(?m)" + pat).Match(body) {
					t.Errorf("a line matches %s but must not\n--- body ---\n%s", pat, body)
				}
			}
		})
	}
}

// TestSetupTwicePrivateRegistry pins the private-registry choice: two
// Prometheus-enabled Setups in one process (as the tests above do) must not
// fail with duplicate-registration errors and must not see each other's data.
func TestSetupTwicePrivateRegistry(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	ctx := context.Background()
	a, err := Setup(ctx, "a", Options{Prometheus: true})
	if err != nil {
		t.Fatalf("first Setup: %v", err)
	}
	b, err := Setup(ctx, "b", Options{Prometheus: true})
	if err != nil {
		t.Fatalf("second Setup: %v", err)
	}
	t.Cleanup(func() { _ = a.Shutdown(ctx); _ = b.Shutdown(ctx) })

	a.ArtifactRecords.Record(ctx, 11)
	rec := httptest.NewRecorder()
	b.PrometheusHandler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if regexp.MustCompile(`(?m)^ipfeed_artifact_records\{[^}]*\} 11$`).Match(rec.Body.Bytes()) {
		t.Error("second Telemetry's /metrics shows the first Telemetry's gauge (registries are shared)")
	}
}

// TestShutdownNil pins the documented nil no-op so callers can defer Shutdown
// unconditionally.
func TestShutdownNil(t *testing.T) {
	var tel *Telemetry
	if err := tel.Shutdown(context.Background()); err != nil {
		t.Errorf("nil Telemetry Shutdown = %v, want nil", err)
	}
}
