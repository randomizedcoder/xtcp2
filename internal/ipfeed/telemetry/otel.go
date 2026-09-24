// Package telemetry wires OpenTelemetry metrics and traces for the collector.
//
// Metrics reach operators two ways, both driven by the same instruments:
//
//   - OTLP over HTTP when the standard OTEL_EXPORTER_OTLP_ENDPOINT env var is
//     set (traces go the same way);
//   - the Prometheus text format through Telemetry.PrometheusHandler when
//     Options.Prometheus is set — the daemon mounts it as /metrics on its
//     health server so one port serves probes and scrapes.
//
// With neither configured, providers are still created but without exporters,
// so the tool runs fine with no collector attached.
package telemetry

import (
	"context"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	otelprom "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Options tunes Setup. The zero value keeps the OTLP-only behavior.
type Options struct {
	// Prometheus additionally exposes every instrument in the Prometheus text
	// format through Telemetry.PrometheusHandler (nil when false). Meant for the
	// daemon's health server; a one-shot run has nowhere to be scraped from and
	// reports its numbers in the end-of-run summary instead.
	Prometheus bool
}

// Telemetry holds the tracer and metric instruments used across the pipeline.
//
// Naming: OTel dotted names; the Prometheus exporter renders them with
// underscores plus the conventional suffixes, e.g. ipfeed.fetch.duration (unit
// s) → ipfeed_fetch_duration_seconds, ipfeed.cycles → ipfeed_cycles_total,
// ipfeed.artifact.size (unit By) → ipfeed_artifact_size_bytes. Every series also
// carries the exporter's otel_scope_name/otel_scope_version labels.
type Telemetry struct {
	Tracer trace.Tracer

	// Per-source (attributes source, provider). Counters accumulate across
	// cycles; the gauge is the latest cycle's view.
	FetchBytes     metric.Int64Counter
	FetchAttempts  metric.Int64Counter
	FetchFailures  metric.Int64Counter
	RecordsValid   metric.Int64Counter
	RecordsInvalid metric.Int64Counter
	FetchDuration  metric.Float64Histogram // discover + download, seconds
	ParseDuration  metric.Float64Histogram // parse of the downloaded body, seconds
	SourceRecords  metric.Int64Gauge       // valid records (prefix entries) from the source in the latest cycle

	// Per-cycle (no attributes unless noted).
	Cycles           metric.Int64Counter     // attr outcome=success|failure
	CycleDuration    metric.Float64Histogram // whole cycle, seconds; attr outcome
	SourcesSucceeded metric.Int64Gauge
	ArtifactRecords  metric.Int64Gauge       // prefix entries in the artifact just written (the lookup table size)
	ArtifactBytes    metric.Int64Gauge       // size of that Parquet file
	WriteDuration    metric.Float64Histogram // combine + Parquet write (building the lookup artifact), seconds
	UploadBytes      metric.Int64Counter
	UploadDuration   metric.Float64Histogram // S3 PUT, seconds

	// PrometheusHandler serves the instruments above in the Prometheus text
	// format. nil unless Options.Prometheus was set.
	PrometheusHandler http.Handler

	shutdown []func(context.Context) error
}

// Setup builds trace and metric providers for serviceName and returns a
// Telemetry with all instruments created. Call Telemetry.Shutdown to flush.
func Setup(ctx context.Context, serviceName string, opts Options) (*Telemetry, error) {
	// Use a schemaless resource for the service.name attribute so it merges
	// cleanly with resource.Default() regardless of the SDK's schema version.
	res, err := resource.Merge(resource.Default(),
		resource.NewSchemaless(attribute.String("service.name", serviceName)))
	if err != nil {
		return nil, err
	}

	t := &Telemetry{}
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")

	// Traces.
	var tpOpts []sdktrace.TracerProviderOption
	tpOpts = append(tpOpts, sdktrace.WithResource(res))
	if endpoint != "" {
		texp, err := otlptracehttp.New(ctx)
		if err != nil {
			return nil, err
		}
		tpOpts = append(tpOpts, sdktrace.WithBatcher(texp))
	}
	tp := sdktrace.NewTracerProvider(tpOpts...)
	otel.SetTracerProvider(tp)
	t.shutdown = append(t.shutdown, tp.Shutdown)

	// Metrics.
	var mpOpts []sdkmetric.Option
	mpOpts = append(mpOpts, sdkmetric.WithResource(res))
	if endpoint != "" {
		mexp, err := otlpmetrichttp.New(ctx)
		if err != nil {
			return nil, err
		}
		mpOpts = append(mpOpts, sdkmetric.WithReader(sdkmetric.NewPeriodicReader(mexp)))
	}
	if opts.Prometheus {
		// A private registry: the exporter is a pull reader, and keeping it off
		// the default registry means a second Setup in one process (tests) does
		// not collide on already-registered collectors.
		reg := prometheus.NewRegistry()
		pexp, err := otelprom.New(otelprom.WithRegisterer(reg))
		if err != nil {
			return nil, err
		}
		mpOpts = append(mpOpts, sdkmetric.WithReader(pexp))
		t.PrometheusHandler = promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	}
	mp := sdkmetric.NewMeterProvider(mpOpts...)
	otel.SetMeterProvider(mp)
	t.shutdown = append(t.shutdown, mp.Shutdown)

	t.Tracer = tp.Tracer("ipfeed-collector")
	m := mp.Meter("ipfeed-collector")

	if t.FetchBytes, err = m.Int64Counter("ipfeed.fetch.bytes"); err != nil {
		return nil, err
	}
	if t.FetchAttempts, err = m.Int64Counter("ipfeed.fetch.attempts"); err != nil {
		return nil, err
	}
	if t.FetchFailures, err = m.Int64Counter("ipfeed.fetch.failures"); err != nil {
		return nil, err
	}
	if t.RecordsValid, err = m.Int64Counter("ipfeed.records.valid"); err != nil {
		return nil, err
	}
	if t.RecordsInvalid, err = m.Int64Counter("ipfeed.records.invalid"); err != nil {
		return nil, err
	}
	if t.FetchDuration, err = m.Float64Histogram("ipfeed.fetch.duration", metric.WithUnit("s"),
		metric.WithDescription("Discover + download of one source's feed, per attempt sequence")); err != nil {
		return nil, err
	}
	if t.ParseDuration, err = m.Float64Histogram("ipfeed.parse.duration", metric.WithUnit("s"),
		metric.WithDescription("Parse of one source's downloaded body")); err != nil {
		return nil, err
	}
	if t.SourceRecords, err = m.Int64Gauge("ipfeed.source.records",
		metric.WithDescription("Valid prefix records the source contributed in the latest cycle")); err != nil {
		return nil, err
	}

	if t.Cycles, err = m.Int64Counter("ipfeed.cycles"); err != nil {
		return nil, err
	}
	if t.CycleDuration, err = m.Float64Histogram("ipfeed.cycle.duration", metric.WithUnit("s"),
		metric.WithDescription("One full collection cycle: fetch all sources, combine, write, upload")); err != nil {
		return nil, err
	}
	if t.SourcesSucceeded, err = m.Int64Gauge("ipfeed.sources.succeeded"); err != nil {
		return nil, err
	}
	if t.ArtifactRecords, err = m.Int64Gauge("ipfeed.artifact.records",
		metric.WithDescription("Prefix records in the Parquet artifact written by the latest cycle (lookup-table entries)")); err != nil {
		return nil, err
	}
	// Named "size" not "bytes": the Prometheus exporter appends the unit, so this
	// renders as ipfeed_artifact_size_bytes rather than ipfeed_artifact_bytes_bytes.
	if t.ArtifactBytes, err = m.Int64Gauge("ipfeed.artifact.size", metric.WithUnit("By"),
		metric.WithDescription("Size of the Parquet artifact written by the latest cycle")); err != nil {
		return nil, err
	}
	if t.WriteDuration, err = m.Float64Histogram("ipfeed.write.duration", metric.WithUnit("s"),
		metric.WithDescription("Sort + Parquet write of the combined records (building the lookup artifact)")); err != nil {
		return nil, err
	}
	if t.UploadBytes, err = m.Int64Counter("ipfeed.upload.bytes"); err != nil {
		return nil, err
	}
	if t.UploadDuration, err = m.Float64Histogram("ipfeed.upload.duration", metric.WithUnit("s"),
		metric.WithDescription("S3 PUT of the artifact")); err != nil {
		return nil, err
	}
	return t, nil
}

// Shutdown flushes and stops all providers. Errors are joined into the first
// non-nil result; a nil Telemetry is a no-op.
func (t *Telemetry) Shutdown(ctx context.Context) error {
	if t == nil {
		return nil
	}
	var firstErr error
	for _, fn := range t.shutdown {
		if err := fn(ctx); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
