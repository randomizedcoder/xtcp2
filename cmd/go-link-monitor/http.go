package main

import (
	"net/http"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
)

const httpShutdownGrace = 5 * time.Second

type healthSource interface {
	Health() linkmonitor.Health
}

func httpServer(source healthSource, gatherer prometheus.Gatherer, stopping *atomic.Bool) *http.Server {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(gatherer, promhttp.HandlerOpts{
		MaxRequestsInFlight: 10, Timeout: 10 * time.Second,
		ErrorHandling: promhttp.HTTPErrorOnError,
	}))
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
