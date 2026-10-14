package main

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
)

const (
	httpShutdownGrace = 5 * time.Second
	coreInFlight      = 10
	// Category paths are moderate and scraped by slower detail jobs.
	categoryInFlight = 4
	// The full exposition is large and meant for slow or on-demand collection.
	allInFlight = 2
	// One interface's full view is small; it keeps the full view's slots free.
	allInterfaceInFlight = 4
	metricsTimeout       = 10 * time.Second
	// maxInterfaceSegment bounds a decoded interface path segment. Netdev
	// names are at most 15 bytes; native RDMA identities are longer.
	maxInterfaceSegment = 128
)

type healthSource interface {
	Health() linkmonitor.Health
}

// interfaceView returns the gatherer for one interface of the current
// snapshot, or monitorprom.ErrUnknownInterface.
type interfaceView func(name string) (prometheus.Gatherer, error)

// route serves one gatherer on one exact path and, when view is set, one
// interface of it on path/{interface}.
type route struct {
	path     string
	gatherer prometheus.Gatherer
	inFlight int
	view     interfaceView
	// viewInFlight limits interface requests separately; zero shares inFlight.
	viewInFlight int
	// protect, when set, wraps the metrics handler, e.g. with read auth.
	protect func(http.Handler) http.Handler
}

// views holds the interface view of each detail path; a nil views serves no
// interface paths.
type views struct {
	all        interfaceView
	categories map[string]interfaceView
}

// endpoint is one listener and the metric routes it serves beside health checks.
type endpoint struct {
	address string
	routes  []route
}

// endpoints places /metrics on the main address, and the category paths
// (in o.categories order) with the all-metrics path on either the same
// listener or the all-metrics listener.
func endpoints(address string, o exposureOptions, core, all prometheus.Gatherer, categories map[string]prometheus.Gatherer, v *views) []endpoint {
	main := endpoint{address: address, routes: []route{{path: metricsPath, gatherer: core, inFlight: coreInFlight}}}
	var detail []route
	for _, category := range o.categories {
		r := route{path: categoryPathPrefix + category, gatherer: categories[category], inFlight: categoryInFlight}
		if v != nil {
			r.view = v.categories[category]
		}
		detail = append(detail, r)
	}
	if o.allPath != "" {
		r := route{path: o.allPath, gatherer: all, inFlight: allInFlight}
		if v != nil {
			r.view, r.viewInFlight = v.all, allInterfaceInFlight
		}
		detail = append(detail, r)
	}
	if o.shared(address) {
		main.routes = append(main.routes, detail...)
		return []endpoint{main}
	}
	return []endpoint{main, {address: o.allAddress, routes: detail}}
}

func httpServer(source healthSource, stopping *atomic.Bool, routes ...route) *http.Server {
	mux := http.NewServeMux()
	for _, r := range routes {
		slots := make(chan struct{}, r.inFlight)
		mux.Handle(r.path, r.wrap(limited(slots, metricsHandler(r.gatherer)), metricsTimeout))
		if r.view == nil {
			continue
		}
		if r.viewInFlight != 0 {
			slots = make(chan struct{}, r.viewInFlight)
		}
		h := r.wrap(limited(slots, interfaceHandler(r.view)), metricsTimeout)
		mux.Handle(r.path+"/{interface}", h)
		mux.Handle(r.path+"/{interface}/{$}", h)
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

func metricsHandler(g prometheus.Gatherer) http.Handler {
	return promhttp.HandlerFor(g, promhttp.HandlerOpts{ErrorHandling: promhttp.HTTPErrorOnError})
}

// wrap applies the timeout outside the in-flight limit, so a timed-out
// gather keeps its slot until it returns, as promhttp does, then protect.
func (r route) wrap(h http.Handler, timeout time.Duration) http.Handler {
	h = http.TimeoutHandler(h, timeout, fmt.Sprintf("Exceeded configured timeout of %v.\n", timeout))
	if r.protect != nil {
		h = r.protect(h)
	}
	return h
}

// limited admits at most cap(slots) concurrent requests, rejecting the rest
// with 503 and promhttp's message.
func limited(slots chan struct{}, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			http.Error(w, fmt.Sprintf("Limit of concurrent requests reached (%d), try again later.", cap(slots)), http.StatusServiceUnavailable)
			return
		}
		h.ServeHTTP(w, r)
	})
}

// interfaceHandler resolves the decoded {interface} segment against one
// snapshot and serves only that interface. An absent name is 404, never an
// empty exposition.
func interfaceHandler(view interfaceView) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("interface")
		if !validInterfaceSegment(name) {
			http.Error(w, "invalid interface", http.StatusBadRequest)
			return
		}
		g, err := view(name)
		switch {
		case errors.Is(err, monitorprom.ErrUnknownInterface):
			http.Error(w, "unknown interface", http.StatusNotFound)
		case err != nil:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		default:
			metricsHandler(g).ServeHTTP(w, r)
		}
	})
}

func validInterfaceSegment(name string) bool {
	return name != "" && name != "." && name != ".." && len(name) <= maxInterfaceSegment && !strings.Contains(name, "/")
}

func writeHealth(w http.ResponseWriter, healthy bool) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if !healthy {
		w.WriteHeader(http.StatusServiceUnavailable)
		return
	}
	w.WriteHeader(http.StatusOK)
}
