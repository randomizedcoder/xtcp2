package main

import (
	"log/slog"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/linkmonitorhttp"
)

const refreshHeader = linkmonitorhttp.RefreshHeader

// collectionMode registers the collection mode metric on r and, on demand,
// the refresh counters, and makes the detail routes refresh.
func collectionMode(served []endpoint, m linkmonitorhttp.Refresher, opts commandOptions, logger *slog.Logger, r prometheus.Registerer) ([]endpoint, error) {
	linkmonitorhttp.RegisterMode(r, opts.monitor.CollectionMode)
	if opts.monitor.CollectionMode != linkmonitor.CollectionOnDemand {
		return served, nil
	}
	if opts.statsIgnored {
		logger.Warn("stats-interval is unused on demand, the default collection mode; set -collection.mode=poll to poll every collector, or tune -watch.interval and -watch.slow-interval")
	}
	return refreshRoutes(served, m, opts.exposure.allPath, linkmonitorhttp.NewRefreshMetrics(r))
}

// refreshRoutes makes every detail route refresh the collectors it serves
// before it gathers: a category path its category's, the all-metrics path
// every collector. /metrics never refreshes: link state comes from events
// and errors from the watch interval.
func refreshRoutes(served []endpoint, source linkmonitorhttp.Refresher, allPath string, metrics *linkmonitorhttp.RefreshMetrics) ([]endpoint, error) {
	for i := range served {
		if err := linkmonitorhttp.RefreshRoutes(served[i].routes, source, categoryPathPrefix, allPath, metrics); err != nil {
			return nil, err
		}
	}
	return served, nil
}
