package main

import (
	"net/http"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/linkmonitorhttp"
)

const (
	httpShutdownGrace    = 5 * time.Second
	coreInFlight         = linkmonitorhttp.CoreInFlight
	categoryInFlight     = linkmonitorhttp.CategoryInFlight
	allInFlight          = linkmonitorhttp.AllInFlight
	allInterfaceInFlight = linkmonitorhttp.AllInterfaceInFlight
	metricsTimeout       = linkmonitorhttp.Timeout
	maxInterfaceSegment  = linkmonitorhttp.MaxInterfaceSegment
)

type healthSource interface {
	Health() linkmonitor.Health
}

// The route model is the library's; see linkmonitorhttp.
type (
	interfaceView = linkmonitorhttp.InterfaceView
	route         = linkmonitorhttp.Route
	views         = linkmonitorhttp.Views
)

// endpoint is one listener and the metric routes it serves beside health checks.
type endpoint struct {
	address string
	routes  []route
}

// endpoints places /metrics on the main address, and the category paths
// (in o.categories order) with the all-metrics path on either the same
// listener or the all-metrics listener.
func endpoints(address string, o exposureOptions, core, all prometheus.Gatherer, categories map[string]prometheus.Gatherer, v *views) []endpoint {
	main := endpoint{address: address, routes: []route{{Path: metricsPath, Gatherer: core, InFlight: coreInFlight}}}
	detail := linkmonitorhttp.DetailRoutes(categoryPathPrefix, o.categories, o.allPath, all, categories, v)
	if o.shared(address) {
		main.routes = append(main.routes, detail...)
		return []endpoint{main}
	}
	return []endpoint{main, {address: o.allAddress, routes: detail}}
}

func httpServer(source healthSource, stopping *atomic.Bool, routes ...route) *http.Server {
	mux := http.NewServeMux()
	for _, r := range routes {
		r.Register(mux)
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
