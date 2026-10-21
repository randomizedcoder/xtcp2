// Package linkmonitorhttp serves a link monitor's detail metrics over HTTP:
// one exact path per metric category, the full exposition, one interface of
// each, and the read-only `ip … show` paths. Each route has its own in-flight
// limit and a timeout, and on demand refreshes what it serves before it
// gathers.
//
// The host owns the listener, the core /metrics path, health checks and
// shutdown; go-link-monitor and xtcp2 mount the same routes on their own
// muxes.
package linkmonitorhttp

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
)

const (
	CoreInFlight = 10
	// Category paths are moderate and scraped by slower detail jobs.
	CategoryInFlight = 4
	// The full exposition is large and meant for slow or on-demand collection.
	AllInFlight = 2
	// One interface's full view is small; it keeps the full view's slots free.
	AllInterfaceInFlight = 4
	// Timeout bounds one metrics request, including an on-demand wait.
	Timeout = 10 * time.Second
	// MaxInterfaceSegment bounds a decoded interface path segment. Netdev
	// names are at most 15 bytes; native RDMA identities are longer.
	MaxInterfaceSegment = 128
)

// InterfaceView returns the gatherer for one interface of the current
// snapshot, or monitorprom.ErrUnknownInterface.
type InterfaceView func(name string) (prometheus.Gatherer, error)

// Route serves one gatherer on one exact path and, when View is set, one
// interface of it on Path/{interface}.
type Route struct {
	Path     string
	Gatherer prometheus.Gatherer
	InFlight int
	View     InterfaceView
	// ViewInFlight limits interface requests separately; zero shares InFlight.
	ViewInFlight int
	// Protect, when set, wraps the metrics handler, e.g. with read auth.
	Protect func(http.Handler) http.Handler
	// Refresh, set on demand, wraps the gather, inside the in-flight limit,
	// with the refresh of what the route serves; view marks the interface
	// handler.
	Refresh func(h http.Handler, view bool) http.Handler
	// Mount, when set, registers a whole route family on the mux in place of
	// a gatherer, each handler passed through the same protect and timeout
	// wrap; the family limits its own concurrency. Path names it in logs.
	Mount func(mux *http.ServeMux, wrap func(http.Handler) http.Handler)
}

// Views holds the interface view of each detail path; a nil *Views serves no
// interface paths.
type Views struct {
	All        InterfaceView
	Categories map[string]InterfaceView
}

// Register puts the route on mux.
func (r Route) Register(mux *http.ServeMux) {
	if r.Mount != nil {
		r.Mount(mux, func(h http.Handler) http.Handler { return r.wrap(h, Timeout) })
		return
	}
	slots := make(chan struct{}, r.InFlight)
	mux.Handle(r.Path, r.wrap(Limited(slots, r.refreshed(MetricsHandler(r.Gatherer), false)), Timeout))
	if r.View == nil {
		return
	}
	if r.ViewInFlight != 0 {
		slots = make(chan struct{}, r.ViewInFlight)
	}
	h := r.wrap(Limited(slots, r.refreshed(InterfaceHandler(r.View), true)), Timeout)
	mux.Handle(r.Path+"/{interface}", h)
	mux.Handle(r.Path+"/{interface}/{$}", h)
}

func (r Route) refreshed(h http.Handler, view bool) http.Handler {
	if r.Refresh == nil {
		return h
	}
	return r.Refresh(h, view)
}

// wrap applies the timeout outside the in-flight limit, so a timed-out
// gather keeps its slot until it returns, as promhttp does, then protect.
func (r Route) wrap(h http.Handler, timeout time.Duration) http.Handler {
	h = http.TimeoutHandler(h, timeout, fmt.Sprintf("Exceeded configured timeout of %v.\n", timeout))
	if r.Protect != nil {
		h = r.Protect(h)
	}
	return h
}

// MetricsHandler serves g, failing the request on a gather error.
func MetricsHandler(g prometheus.Gatherer) http.Handler {
	return promhttp.HandlerFor(g, promhttp.HandlerOpts{ErrorHandling: promhttp.HTTPErrorOnError})
}

// Limited admits at most cap(slots) concurrent requests, rejecting the rest
// with 503 and promhttp's message.
func Limited(slots chan struct{}, h http.Handler) http.Handler {
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

// InterfaceHandler resolves the decoded {interface} segment against one
// snapshot and serves only that interface. An absent name is 404, never an
// empty exposition.
func InterfaceHandler(view InterfaceView) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("interface")
		if !ValidInterfaceSegment(name) {
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
			MetricsHandler(g).ServeHTTP(w, r)
		}
	})
}

// ValidInterfaceSegment reports whether name can be one interface path
// segment.
func ValidInterfaceSegment(name string) bool {
	return name != "" && name != "." && name != ".." && len(name) <= MaxInterfaceSegment && !strings.Contains(name, "/")
}

// DetailRoutes returns one route per category, under categoryPrefix in
// categories order, then the all-metrics route when allPath is set. v adds
// their interface views.
func DetailRoutes(categoryPrefix string, categories []string, allPath string, all prometheus.Gatherer, gatherers map[string]prometheus.Gatherer, v *Views) []Route {
	var detail []Route
	for _, category := range categories {
		r := Route{Path: categoryPrefix + category, Gatherer: gatherers[category], InFlight: CategoryInFlight}
		if v != nil {
			r.View = v.Categories[category]
		}
		detail = append(detail, r)
	}
	if allPath != "" {
		r := Route{Path: allPath, Gatherer: all, InFlight: AllInFlight}
		if v != nil {
			r.View, r.ViewInFlight = v.All, AllInterfaceInFlight
		}
		detail = append(detail, r)
	}
	return detail
}
