package prometheus_test

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	client "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
)

// ExampleNewCollector shows wiring inside a host that already owns HTTP and
// lifecycle supervision. The host calls shutdown and waits before exiting.
func ExampleNewCollector() {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cfg := linkmonitor.DefaultConfig()
	cfg.BaselineFile = "/var/lib/example-host/link-baseline.json"
	m, err := linkmonitor.New(cfg, linkmonitor.Options{Logger: slog.Default()})
	if err != nil {
		panic(err)
	}
	registry := client.NewRegistry()
	if err := registry.Register(monitorprom.NewCollector(m)); err != nil {
		panic(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(registry, promhttp.HandlerOpts{
		MaxRequestsInFlight: 10, Timeout: 10 * time.Second,
		ErrorHandling: promhttp.HTTPErrorOnError,
	}))
	// Attach mux to the host's server with WriteTimeout longer than the scrape
	// timeout. Bind before Run. This example does not start an HTTP listener.
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()
	// The host selects on done alongside its other services, and forwards
	// explicit controls using m.RequestResync or m.RequestRebaseline.
	cancel()
	if err := <-done; err != nil && err != context.Canceled {
		slog.Error("monitor shutdown", "error", err)
	}
}
