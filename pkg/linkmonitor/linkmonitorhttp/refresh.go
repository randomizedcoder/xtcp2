package linkmonitorhttp

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
)

// RefreshHeader marks a detail response served after the on-demand wait
// ended, before every collection it asked for had finished.
const RefreshHeader = "X-Link-Monitor-Refresh"

// Refresher is the part of *linkmonitor.Monitor the refresh wrapper uses.
type Refresher interface {
	Refresh(context.Context, linkmonitor.RefreshScope) (linkmonitor.RefreshResult, error)
}

// RefreshMetrics counts on-demand refreshes by outcome: cached when every
// job was fresh, collected when any ran, timeout when the wait ended first.
type RefreshMetrics struct {
	cached, collected, timeout prometheus.Counter
}

// NewRefreshMetrics registers go_link_monitor_on_demand_refreshes_total on r.
func NewRefreshMetrics(r prometheus.Registerer) *RefreshMetrics {
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "go_link_monitor_on_demand_refreshes_total",
		Help: "On-demand detail scrapes by refresh outcome: cached, collected or timeout.",
	}, []string{"result"})
	r.MustRegister(counter)
	return &RefreshMetrics{cached: counter.WithLabelValues("cached"), collected: counter.WithLabelValues("collected"), timeout: counter.WithLabelValues("timeout")}
}

func (m *RefreshMetrics) observe(result linkmonitor.RefreshResult) {
	switch {
	case result.TimedOut:
		m.timeout.Inc()
	case result.Collected != 0:
		m.collected.Inc()
	default:
		m.cached.Inc()
	}
}

// RegisterMode exposes the collection mode as go_link_monitor_collection_mode.
func RegisterMode(r prometheus.Registerer, mode linkmonitor.CollectionMode) {
	if mode == "" {
		mode = linkmonitor.CollectionPoll
	}
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "go_link_monitor_collection_mode", Help: "Collection mode of this monitor, always 1.",
		ConstLabels: prometheus.Labels{"mode": string(mode)},
	})
	gauge.Set(1)
	r.MustRegister(gauge)
}

// RefreshRoutes makes every detail route refresh the collectors it serves
// before it gathers: a category path under categoryPrefix its category's,
// the all-metrics path every collector. Other routes, such as the core
// /metrics, never refresh: link state comes from events and errors from the
// watch interval.
func RefreshRoutes(routes []Route, source Refresher, categoryPrefix, allPath string, metrics *RefreshMetrics) error {
	for i := range routes {
		r := &routes[i]
		var set linkmonitor.CollectorSet
		switch category, ok := strings.CutPrefix(r.Path, categoryPrefix); {
		case r.Mount != nil:
		case r.Path == allPath:
			set = linkmonitor.AllCollectors()
		case ok:
			var err error
			if set, err = linkmonitor.Collectors(monitorprom.CategoryCollectors(category)...); err != nil {
				return err
			}
		}
		if set != 0 {
			r.Refresh = func(h http.Handler, view bool) http.Handler { return Refreshing(source, set, view, metrics, h) }
		}
	}
	return nil
}

// Refreshing refreshes collectors, on the {interface} of an interface view,
// then serves h from the snapshot that follows. A timed-out refresh still
// serves, marked with RefreshHeader.
func Refreshing(source Refresher, collectors linkmonitor.CollectorSet, view bool, metrics *RefreshMetrics, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope := linkmonitor.RefreshScope{Collectors: collectors}
		if view {
			// An invalid segment is rejected by the view itself.
			if scope.Interface = r.PathValue("interface"); !ValidInterfaceSegment(scope.Interface) {
				h.ServeHTTP(w, r)
				return
			}
		}
		result, err := source.Refresh(r.Context(), scope)
		switch {
		case errors.Is(err, linkmonitor.ErrUnknownInterface):
			http.Error(w, "unknown interface", http.StatusNotFound)
			return
		case err != nil && r.Context().Err() != nil:
			return // the client is gone; the collection completes for the next scrape
		case err != nil:
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}
		metrics.observe(result)
		if result.TimedOut {
			w.Header().Set(RefreshHeader, "timeout")
		}
		h.ServeHTTP(w, r)
	})
}
