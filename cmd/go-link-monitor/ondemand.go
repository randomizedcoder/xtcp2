package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
)

// refreshHeader marks a detail response served after the on-demand wait
// ended, before every collection it asked for had finished.
const refreshHeader = "X-Link-Monitor-Refresh"

type refresher interface {
	Refresh(context.Context, linkmonitor.RefreshScope) (linkmonitor.RefreshResult, error)
}

// refreshMetrics counts on-demand refreshes by outcome: cached when every
// job was fresh, collected when any ran, timeout when the wait ended first.
type refreshMetrics struct {
	cached, collected, timeout prometheus.Counter
}

func newRefreshMetrics(r prometheus.Registerer) *refreshMetrics {
	counter := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "go_link_monitor_on_demand_refreshes_total",
		Help: "On-demand detail scrapes by refresh outcome: cached, collected or timeout.",
	}, []string{"result"})
	r.MustRegister(counter)
	return &refreshMetrics{cached: counter.WithLabelValues("cached"), collected: counter.WithLabelValues("collected"), timeout: counter.WithLabelValues("timeout")}
}

func (m *refreshMetrics) observe(result linkmonitor.RefreshResult) {
	switch {
	case result.TimedOut:
		m.timeout.Inc()
	case result.Collected != 0:
		m.collected.Inc()
	default:
		m.cached.Inc()
	}
}

// collectionMode registers the collection mode metric on r and, on demand,
// the refresh counters, and makes the detail routes refresh.
func collectionMode(served []endpoint, m refresher, opts commandOptions, logger *slog.Logger, r prometheus.Registerer) ([]endpoint, error) {
	registerMode(r, opts.monitor.CollectionMode)
	if opts.monitor.CollectionMode != linkmonitor.CollectionOnDemand {
		return served, nil
	}
	if opts.statsIgnored {
		logger.Warn("stats-interval is unused on demand, the default collection mode; set -collection.mode=poll to poll every collector, or tune -watch.interval and -watch.slow-interval")
	}
	return refreshRoutes(served, m, opts.exposure.allPath, newRefreshMetrics(r))
}

// registerMode exposes the collection mode as go_link_monitor_collection_mode.
func registerMode(r prometheus.Registerer, mode linkmonitor.CollectionMode) {
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

// refreshRoutes makes every detail route refresh the collectors it serves
// before it gathers: a category path its category's, the all-metrics path
// every collector. /metrics never refreshes: link state comes from events
// and errors from the watch interval.
func refreshRoutes(served []endpoint, source refresher, allPath string, metrics *refreshMetrics) ([]endpoint, error) {
	for i := range served {
		for j := range served[i].routes {
			r := &served[i].routes[j]
			var set linkmonitor.CollectorSet
			switch category, ok := strings.CutPrefix(r.path, categoryPathPrefix); {
			case r.path == allPath:
				set = linkmonitor.AllCollectors()
			case ok:
				var err error
				if set, err = linkmonitor.Collectors(monitorprom.CategoryCollectors(category)...); err != nil {
					return nil, err
				}
			}
			if set != 0 {
				r.refresh = func(h http.Handler, view bool) http.Handler { return refreshing(source, set, view, metrics, h) }
			}
		}
	}
	return served, nil
}

// refreshing refreshes collectors, on the {interface} of an interface view,
// then serves h from the snapshot that follows. A timed-out refresh still
// serves, marked with refreshHeader.
func refreshing(source refresher, collectors linkmonitor.CollectorSet, view bool, metrics *refreshMetrics, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		scope := linkmonitor.RefreshScope{Collectors: collectors}
		if view {
			// An invalid segment is rejected by the view itself.
			if scope.Interface = r.PathValue("interface"); !validInterfaceSegment(scope.Interface) {
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
			w.Header().Set(refreshHeader, "timeout")
		}
		h.ServeHTTP(w, r)
	})
}
