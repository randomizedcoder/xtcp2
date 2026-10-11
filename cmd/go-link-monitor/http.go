package main

import (
	"net/http"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
)

const (
	httpShutdownGrace = 5 * time.Second
	coreInFlight      = 10
	// The full exposition is large and meant for slow or on-demand collection.
	allInFlight = 2
)

type healthSource interface {
	Health() linkmonitor.Health
}

// route serves one gatherer on one exact path.
type route struct {
	path     string
	gatherer prometheus.Gatherer
	inFlight int
	// protect, when set, wraps the metrics handler, e.g. with read auth.
	protect func(http.Handler) http.Handler
}

// endpoint is one listener and the metric routes it serves beside health checks.
type endpoint struct {
	address string
	routes  []route
}

// endpoints places /metrics on the main address and the all-metrics path on
// either the same listener or its own.
func endpoints(address string, o exposureOptions, core, all prometheus.Gatherer) []endpoint {
	main := endpoint{address: address, routes: []route{{path: metricsPath, gatherer: core, inFlight: coreInFlight}}}
	if o.allPath == "" {
		return []endpoint{main}
	}
	full := route{path: o.allPath, gatherer: all, inFlight: allInFlight}
	if o.shared(address) {
		main.routes = append(main.routes, full)
		return []endpoint{main}
	}
	return []endpoint{main, {address: o.allAddress, routes: []route{full}}}
}

func httpServer(source healthSource, stopping *atomic.Bool, routes ...route) *http.Server {
	mux := http.NewServeMux()
	for _, r := range routes {
		h := promhttp.HandlerFor(r.gatherer, promhttp.HandlerOpts{
			MaxRequestsInFlight: r.inFlight, Timeout: 10 * time.Second,
			ErrorHandling: promhttp.HTTPErrorOnError,
		})
		if r.protect != nil {
			h = r.protect(h)
		}
		mux.Handle(r.path, h)
	}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeHealth(w, !stopping.Load())
	})
	mux.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		writeHealth(w, !stopping.Load() && source.Health().Ready)
	})
	return &http.Server{
		Handler: mux, WriteTimeout: 15 * time.Second,
		ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 60 * time.Second,
	}
}

func writeHealth(w http.ResponseWriter, healthy bool) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if !healthy {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}
