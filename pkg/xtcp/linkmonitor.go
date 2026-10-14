package xtcp

import (
	"context"
	"log"
	"log/slog"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/linkmonitorgrpc"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
)

// DefaultLinkMonitorBaseline is xtcp2's baseline file for the embedded link
// monitor. It differs from go-link-monitor's, so the two can run side by side.
const DefaultLinkMonitorBaseline = "/var/lib/xtcp2/link-monitor-baseline.json"

// linkMonitorExposure selects the link monitor collectors xtcp2 registers.
type linkMonitorExposure struct {
	all   bool              // every metric plus the sparse problem series
	match func(string) bool // a name-filtered collector; nil with !all is the summary
}

// collectors returns the collectors for m. The default is the alerting
// summary, which costs the same however many interfaces the host has.
func (e linkMonitorExposure) collectors(m *linkmonitor.Monitor) []prometheus.Collector {
	switch {
	case e.all:
		return []prometheus.Collector{monitorprom.NewCollector(m), monitorprom.NewSummaryCollector(m, monitorprom.WithoutScalars())}
	case e.match != nil:
		return []prometheus.Collector{monitorprom.NewCollector(m, monitorprom.WithMetricFilter(e.match))}
	default:
		return []prometheus.Collector{monitorprom.NewSummaryCollector(m)}
	}
}

// linkMonitorSettings translates the config into library settings and the
// metric exposure, once at startup. Unset fields keep the library defaults.
// Empty include and exclude patterns serve the summary; either pattern
// selects names, with an empty include meaning the core audit set.
func linkMonitorSettings(c *xtcp_config.LinkMonitorConfig) (linkmonitor.Config, linkMonitorExposure, error) {
	cfg := linkmonitor.DefaultConfig()
	cfg.BaselineFile = DefaultLinkMonitorBaseline
	for _, s := range []struct {
		value  string
		target *string
	}{
		{c.GetBaselineFile(), &cfg.BaselineFile},
		{c.GetStatsInclude(), &cfg.StatsInclude},
		{c.GetStatsExclude(), &cfg.StatsExclude},
		{c.GetNetstatFields(), &cfg.NetstatFields},
	} {
		if s.value != "" {
			*s.target = s.value
		}
	}
	if c.GetSettle() != nil {
		cfg.Settle = c.GetSettle().AsDuration()
	}
	if c.GetResync() != nil {
		cfg.Resync = c.GetResync().AsDuration()
	}
	if c.GetStatsInterval() != nil {
		cfg.StatsInterval = c.GetStatsInterval().AsDuration()
	}
	cfg.MaxSpeedExceptions = append([]string(nil), c.GetMaxSpeedExceptions()...)
	if c.GetMetricsAll() {
		return cfg, linkMonitorExposure{all: true}, nil
	}
	include, exclude := c.GetMetricsCoreInclude(), c.GetMetricsCoreExclude()
	if include == "" && exclude == "" {
		return cfg, linkMonitorExposure{}, nil
	}
	if include == "" {
		include = monitorprom.CoreMetricsPattern
	}
	match, err := monitorprom.NameMatcher(include, exclude)
	if err != nil {
		return linkmonitor.Config{}, linkMonitorExposure{}, err
	}
	return cfg, linkMonitorExposure{match: match}, nil
}

// promRegisterer is x.registry, or the default registerer when unset, as in
// InitPromethus.
func (x *XTCP) promRegisterer() prometheus.Registerer {
	if x.registry == nil {
		return prometheus.DefaultRegisterer
	}
	return x.registry
}

// initLinkMonitor builds the embedded link monitor when enabled and registers
// its collector. Invalid configuration is fatal at startup, like the rest of
// XtcpConfig; the monitor acquires nothing until Run.
func (x *XTCP) initLinkMonitor() {
	c := x.config.GetLinkMonitor()
	if !c.GetEnable() {
		return
	}
	cfg, exposure, err := linkMonitorSettings(c)
	if err != nil {
		x.callFatalf("link monitor config: %v", err)
		return
	}
	m, err := linkmonitor.New(cfg, linkmonitor.Options{Logger: slog.Default()})
	if err != nil {
		x.callFatalf("link monitor config: %v", err)
		return
	}
	for _, collector := range exposure.collectors(m) {
		if err := x.promRegisterer().Register(collector); err != nil {
			x.callFatalf("link monitor metrics: %v", err)
			return
		}
	}
	x.linkMonitor = m
	if x.debugLevel > 10 {
		log.Printf("initLinkMonitor: enabled baseline:%s all_metrics:%t summary:%t", cfg.BaselineFile, exposure.all, !exposure.all && exposure.match == nil)
	}
}

// newLinkMonitorService builds LinkMonitorService. It is always registered;
// without an embedded monitor every call returns FailedPrecondition. Request
// metrics exist only when the monitor does.
func (x *XTCP) newLinkMonitorService() (*linkmonitorgrpc.Server, error) {
	var monitor linkmonitorgrpc.Monitor // a nil *Monitor would be a non-nil interface
	opts := []linkmonitorgrpc.Option{linkmonitorgrpc.WithLogger(slog.Default())}
	if x.linkMonitor != nil {
		monitor = x.linkMonitor
		opts = append(opts, linkmonitorgrpc.WithRegisterer(x.promRegisterer()))
	}
	return linkmonitorgrpc.NewServer(monitor, opts...)
}

// runLinkMonitor runs the monitor for the daemon's lifetime. A failure while
// the daemon is running, such as a baseline file locked by another monitor,
// is fatal so it is not mistaken for a healthy daemon without link metrics.
func (x *XTCP) runLinkMonitor(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	// A clean cancellation returns nil.
	err := x.linkMonitor.Run(ctx)
	switch {
	case err == nil:
	case ctx.Err() == nil:
		x.callFatalf("link monitor stopped: %v", err)
	default:
		log.Printf("link monitor shutdown: %v", err)
	}
}
