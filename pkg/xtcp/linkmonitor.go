package xtcp

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/internal/goip/goiphttp"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/linkmonitorgrpc"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/linkmonitorhttp"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
	"google.golang.org/protobuf/types/known/durationpb"
)

// DefaultLinkMonitorBaseline is xtcp2's baseline file for the embedded link
// monitor. It differs from go-link-monitor's, so the two can run side by side.
const DefaultLinkMonitorBaseline = "/var/lib/xtcp2/link-monitor-baseline.json"

// DefaultLinkMonitorAllPath is the all-metrics path, as go-link-monitor's.
const DefaultLinkMonitorAllPath = "/metrics-all"

// ErrLinkMonitorDisabled is returned for a link monitor request when xtcp2
// runs without one.
var ErrLinkMonitorDisabled = errors.New("link monitor is disabled")

// linkMonitorExposure selects the link monitor collectors xtcp2 registers,
// and the detail paths it serves beside promPath.
type linkMonitorExposure struct {
	all   bool              // every metric plus the sparse problem series
	match func(string) bool // a name-filtered collector; nil with !all is the summary
	paths linkMonitorPaths
}

// linkMonitorPaths is the detail path layout from the config.
type linkMonitorPaths struct {
	categories     []string
	interfacePaths bool
	allPath        string
	ipPaths        bool
	ipMaxBytes     int
}

// newLinkMonitorPaths applies the defaults: every category, interface paths
// and /metrics-all on, /ip off.
func newLinkMonitorPaths(c *xtcp_config.LinkMonitorConfig) linkMonitorPaths {
	p := linkMonitorPaths{
		categories: monitorprom.Categories(), interfacePaths: true, allPath: DefaultLinkMonitorAllPath,
		ipPaths: c.GetIpPaths(), ipMaxBytes: int(c.GetIpMaxBytes()),
	}
	if c == nil {
		return p
	}
	if c.MetricsCategories != nil {
		p.categories = splitLinkMonitorList(c.GetMetricsCategories())
	}
	if c.MetricsInterfacePaths != nil {
		p.interfacePaths = c.GetMetricsInterfacePaths()
	}
	if c.MetricsAllPath != nil {
		p.allPath = c.GetMetricsAllPath()
	}
	return p
}

// splitLinkMonitorList splits a comma-separated list; empty is an empty list.
func splitLinkMonitorList(value string) []string {
	if value == "" {
		return []string{}
	}
	return strings.Split(value, ",")
}

// collectors returns the collectors for m. The default is the alerting
// summary, which costs the same however many interfaces the host has. The
// detail collectors refresh first, which in poll mode returns at once.
func (e linkMonitorExposure) collectors(m *linkmonitor.Monitor) []prometheus.Collector {
	switch {
	case e.all:
		return []prometheus.Collector{refreshing{monitorprom.NewCollector(m), m}, monitorprom.NewSummaryCollector(m, monitorprom.WithoutScalars())}
	case e.match != nil:
		return []prometheus.Collector{refreshing{monitorprom.NewCollector(m, monitorprom.WithMetricFilter(e.match)), m}}
	default:
		return []prometheus.Collector{monitorprom.NewSummaryCollector(m)}
	}
}

// linkMonitorRefresher is the part of *linkmonitor.Monitor refreshing uses.
type linkMonitorRefresher interface {
	Refresh(context.Context, linkmonitor.RefreshScope) (linkmonitor.RefreshResult, error)
}

// refreshing brings every collector up to date before each collection, so an
// on-demand monitor serves current detail on xtcp2's registry. Refresh bounds
// its own wait by OnDemandWait; on a timeout or an error, such as a monitor
// not yet running, the last published snapshot is served.
type refreshing struct {
	prometheus.Collector
	monitor linkMonitorRefresher
}

func (r refreshing) Collect(ch chan<- prometheus.Metric) {
	if _, err := r.monitor.Refresh(context.Background(), linkmonitor.RefreshScope{Collectors: linkmonitor.AllCollectors()}); err != nil {
		slog.Debug("link monitor refresh; serving the last snapshot", "err", err)
	}
	r.Collector.Collect(ch)
}

// linkMonitorSettings translates the config into library settings and the
// metric exposure, once at startup. Unset fields keep the library defaults.
// Empty include and exclude patterns serve the summary; either pattern
// selects names, with an empty include meaning the core audit set.
func linkMonitorSettings(c *xtcp_config.LinkMonitorConfig) (linkmonitor.Config, linkMonitorExposure, error) {
	cfg := linkmonitor.DefaultConfig()
	cfg.BaselineFile = DefaultLinkMonitorBaseline
	// An empty collection mode keeps the embedded monitor polling, as it did
	// before the mode existed; the OCI images select on-demand.
	cfg.CollectionMode = linkmonitor.CollectionPoll
	if mode := c.GetCollectionMode(); mode != "" {
		cfg.CollectionMode = linkmonitor.CollectionMode(mode)
	}
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
	for _, d := range []struct {
		value  *durationpb.Duration
		target *time.Duration
	}{
		{c.GetStatsInterval(), &cfg.StatsInterval},
		{c.GetWatchInterval(), &cfg.WatchInterval},
		{c.GetSlowInterval(), &cfg.SlowInterval},
		{c.GetOnDemandTtl(), &cfg.OnDemandTTL},
		{c.GetOnDemandWait(), &cfg.OnDemandWait},
		{c.GetErrorWindow(), &cfg.ErrorWindow},
		{c.GetFlapWindow(), &cfg.FlapWindow},
	} {
		if d.value != nil {
			*d.target = d.value.AsDuration()
		}
	}
	// A detail scrape must be answered inside the path timeout after its wait.
	if cfg.OnDemandWait >= linkmonitorhttp.Timeout {
		return linkmonitor.Config{}, linkMonitorExposure{}, fmt.Errorf("on-demand wait %v must be under the %v detail path timeout", cfg.OnDemandWait, linkmonitorhttp.Timeout)
	}
	if c != nil && c.ErrorClasses != nil {
		cfg.ErrorClasses = splitLinkMonitorList(c.GetErrorClasses())
	}
	cfg.ErrorExclude = c.GetErrorExclude()
	cfg.MaxSpeedExceptions = append([]string(nil), c.GetMaxSpeedExceptions()...)
	paths := newLinkMonitorPaths(c)
	if c.GetMetricsAll() {
		return cfg, linkMonitorExposure{all: true, paths: paths}, nil
	}
	include, exclude := c.GetMetricsCoreInclude(), c.GetMetricsCoreExclude()
	if include == "" && exclude == "" {
		return cfg, linkMonitorExposure{paths: paths}, nil
	}
	if include == "" {
		include = monitorprom.CoreMetricsPattern
	}
	match, err := monitorprom.NameMatcher(include, exclude)
	if err != nil {
		return linkmonitor.Config{}, linkMonitorExposure{}, err
	}
	return cfg, linkMonitorExposure{match: match, paths: paths}, nil
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
	// The mode and, on demand, the detail path refresh outcomes are on
	// promPath, as on go-link-monitor's /metrics.
	linkmonitorhttp.RegisterMode(x.promRegisterer(), cfg.CollectionMode)
	if cfg.CollectionMode == linkmonitor.CollectionOnDemand {
		x.linkMonitorRefresh = linkmonitorhttp.NewRefreshMetrics(x.promRegisterer())
	}
	x.linkMonitorPaths = exposure.paths
	x.linkMonitor = m
	if x.debugLevel > 10 {
		log.Printf("initLinkMonitor: enabled baseline:%s mode:%s all_metrics:%t summary:%t", cfg.BaselineFile, cfg.CollectionMode, exposure.all, !exposure.all && exposure.match == nil)
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
// the daemon is running, such as a corrupt baseline file or one locked by
// another monitor, stops only the monitor: TCP collection and /readyz carry
// on, xtcp_gauges{function="linkMonitor",variable="up"} drops to 0 and the
// detail paths answer 503. The baseline file is left as it is; the operator
// repairs or removes it and restarts xtcp2.
func (x *XTCP) runLinkMonitor(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	x.setLinkMonitorUp(true)
	defer x.setLinkMonitorUp(false)
	// A clean cancellation returns nil.
	err := x.linkMonitor.Run(ctx)
	switch {
	case err == nil:
	case ctx.Err() == nil:
		x.linkMonitorDown.Store(true)
		if x.pC != nil {
			x.pC.WithLabelValues("linkMonitor", "failed", "error").Inc()
		}
		log.Printf("link monitor stopped; xtcp2 continues without link monitoring: %v", err)
	default:
		log.Printf("link monitor shutdown: %v", err)
	}
}

// setLinkMonitorUp sets xtcp_gauges{function="linkMonitor",variable="up"}.
func (x *XTCP) setLinkMonitorUp(up bool) {
	if x.pGV == nil {
		return
	}
	v := 0.0
	if up {
		v = 1
	}
	x.pGV.WithLabelValues("linkMonitor", "up", "gauge").Set(v)
}

// LinkMonitorHandler builds the embedded link monitor's detail paths for the
// listener that serves promPath: promPath/<category> with
// promPath/<category>/<interface>, the all-metrics path with its interface
// views and, when enabled, the /ip and /i paths. It is nil without a
// monitor. Unknown paths are 404, and every path is 503 once the monitor has
// failed. Call it once per daemon; the listener's own auth policy applies.
func (x *XTCP) LinkMonitorHandler(promPath string) (http.Handler, error) {
	if x.linkMonitor == nil {
		return nil, nil
	}
	p := x.linkMonitorPaths
	prefix := strings.TrimSuffix(promPath, "/") + "/"
	layout := linkmonitorhttp.Paths{
		CategoryPrefix: prefix, Categories: p.categories, AllPath: p.allPath,
		InterfacePaths: p.interfacePaths, IPPaths: p.ipPaths,
		Reserved: []string{promPath, "/healthz", "/readyz", "/debug/pprof/"},
	}
	if err := layout.Validate(); err != nil {
		return nil, fmt.Errorf("link monitor paths: %w", err)
	}
	detail, err := linkmonitorhttp.NewDetail(x.linkMonitor, p.categories, p.interfacePaths, linkmonitorhttp.Identity{})
	if err != nil {
		return nil, fmt.Errorf("link monitor paths: %w", err)
	}
	routes := detail.Routes(prefix, p.categories, p.allPath)
	if x.linkMonitorRefresh != nil {
		if err := linkmonitorhttp.RefreshRoutes(routes, x.linkMonitor, prefix, p.allPath, x.linkMonitorRefresh); err != nil {
			return nil, fmt.Errorf("link monitor paths: %w", err)
		}
	}
	if p.ipPaths {
		ip, err := linkmonitorhttp.IPRoute(goiphttp.Options{MaxBytes: p.ipMaxBytes})
		if err != nil {
			return nil, fmt.Errorf("link monitor ip paths: %w", err)
		}
		routes = append(routes, ip)
	}
	mux := http.NewServeMux()
	for _, r := range routes {
		r.Register(mux)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if x.linkMonitorDown.Load() {
			http.Error(w, "link monitor down", http.StatusServiceUnavailable)
			return
		}
		mux.ServeHTTP(w, r)
	}), nil
}

// RequestLinkMonitorRebaseline asks the embedded link monitor to relearn the
// expected up-link count, as SIGUSR1 does for go-link-monitor. The new
// baseline is written once the links have settled.
func (x *XTCP) RequestLinkMonitorRebaseline() error {
	if x.linkMonitor == nil {
		return ErrLinkMonitorDisabled
	}
	return x.linkMonitor.RequestRebaseline()
}
