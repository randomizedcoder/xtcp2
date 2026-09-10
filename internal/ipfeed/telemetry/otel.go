// Package telemetry wires OpenTelemetry (OTLP over HTTP) metrics and traces
// for the collector. If no OTLP endpoint is configured (the standard
// OTEL_EXPORTER_OTLP_ENDPOINT env var is empty), providers are still created
// but without exporters, so the tool runs fine with no collector attached.
package telemetry

import (
	"context"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

// Telemetry holds the tracer and metric instruments used across the pipeline.
type Telemetry struct {
	Tracer trace.Tracer

	FetchBytes       metric.Int64Counter
	FetchAttempts    metric.Int64Counter
	FetchFailures    metric.Int64Counter
	RecordsValid     metric.Int64Counter
	RecordsInvalid   metric.Int64Counter
	UploadBytes      metric.Int64Counter
	Cycles           metric.Int64Counter // collection cycles, attr outcome=success|failure
	FetchDuration    metric.Float64Histogram
	ParseDuration    metric.Float64Histogram
	SourcesSucceeded metric.Int64Gauge

	shutdown []func(context.Context) error
}

// Setup builds trace and metric providers for serviceName and returns a
// Telemetry with all instruments created. Call Telemetry.Shutdown to flush.
func Setup(ctx context.Context, serviceName string) (*Telemetry, error) {
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
	if t.UploadBytes, err = m.Int64Counter("ipfeed.upload.bytes"); err != nil {
		return nil, err
	}
	if t.Cycles, err = m.Int64Counter("ipfeed.cycles"); err != nil {
		return nil, err
	}
	if t.FetchDuration, err = m.Float64Histogram("ipfeed.fetch.duration", metric.WithUnit("s")); err != nil {
		return nil, err
	}
	if t.ParseDuration, err = m.Float64Histogram("ipfeed.parse.duration", metric.WithUnit("s")); err != nil {
		return nil, err
	}
	if t.SourcesSucceeded, err = m.Int64Gauge("ipfeed.sources.succeeded"); err != nil {
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
