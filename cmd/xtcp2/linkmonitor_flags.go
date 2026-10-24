package main

import (
	"flag"
	"fmt"
	"strings"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/internal/goip/goiphttp"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

// defineLinkMonitorFlags registers the embedded link monitor's flags
// (XtcpConfig.link_monitor, field 250). The monitor is on by default, and the
// defaults are the library's and go-link-monitor's, except the baseline
// file, which is xtcp2's own so a standalone go-link-monitor can run beside it.
func defineLinkMonitorFlags(f *mainFlags) {
	d := linkmonitor.DefaultConfig()
	f.linkMonitor = flag.Bool("linkMonitor", true, "run the embedded link monitor: link counts against a baseline, per-interface and RDMA checks and collector health on the Prometheus listener, its detail paths beside -promPath, plus LinkMonitorService on the gRPC listener. Falls back to LINK_MONITOR env.")
	f.linkMonitorBaselineFile = flag.String("linkMonitorBaselineFile", xtcp.DefaultLinkMonitorBaseline, "link monitor baseline file; locked, so it must differ from any standalone go-link-monitor's. Falls back to LINK_MONITOR_BASELINE_FILE env.")
	f.linkMonitorSettle = flag.Duration("linkMonitorSettle", d.Settle, "how long link state must hold before the first baseline is recorded. Falls back to LINK_MONITOR_SETTLE env.")
	f.linkMonitorResync = flag.Duration("linkMonitorResync", d.Resync, "full link resync interval (positive). Falls back to LINK_MONITOR_RESYNC env.")
	f.linkMonitorStatsInterval = flag.Duration("linkMonitorStatsInterval", d.StatsInterval, "interface statistics collection interval (positive). Falls back to LINK_MONITOR_STATS_INTERVAL env.")
	f.linkMonitorStatsInclude = flag.String("linkMonitorStatsInclude", d.StatsInclude, "regexp of interfaces whose statistics are collected. Falls back to LINK_MONITOR_STATS_INCLUDE env.")
	f.linkMonitorStatsExclude = flag.String("linkMonitorStatsExclude", d.StatsExclude, "regexp of interfaces excluded from statistics. Falls back to LINK_MONITOR_STATS_EXCLUDE env.")
	f.linkMonitorNetstatFields = flag.String("linkMonitorNetstatFields", d.NetstatFields, "regexp of host protocol (netstat) fields collected. Falls back to LINK_MONITOR_NETSTAT_FIELDS env.")
	f.linkMonitorMaxSpeedExceptions = flag.String("linkMonitorMaxSpeedExceptions", "", "comma-separated exact selectors exempt from the max-speed check. Falls back to LINK_MONITOR_MAX_SPEED_EXCEPTIONS env.")
	f.linkMonitorMetricsCoreInclude = flag.String("linkMonitorMetricsCoreInclude", "", "anchored regexp of link monitor metric names registered on xtcp2's registry; empty with an empty exclude = the alerting summary, otherwise the core audit set. Falls back to LINK_MONITOR_METRICS_CORE_INCLUDE env.")
	f.linkMonitorMetricsCoreExclude = flag.String("linkMonitorMetricsCoreExclude", "", "anchored regexp of link monitor metric names removed from the selection. Falls back to LINK_MONITOR_METRICS_CORE_EXCLUDE env.")
	f.linkMonitorMetricsAll = flag.Bool("linkMonitorMetricsAll", false, "register every link monitor metric, including bulk driver, RDMA and netstat counters, instead of the alerting summary. Falls back to LINK_MONITOR_METRICS_ALL env.")
	f.linkMonitorCollectionMode = flag.String("linkMonitorCollectionMode", string(linkmonitor.CollectionOnDemand), "link monitor collection mode: poll collects everything each stats interval; on-demand polls errors and state each watch interval and settings each slow interval, and collects the rest when xtcp2 serves it. Falls back to LINK_MONITOR_COLLECTION_MODE env.")
	f.linkMonitorWatchInterval = flag.Duration("linkMonitorWatchInterval", d.WatchInterval, "on demand: error and state polling interval (positive). Falls back to LINK_MONITOR_WATCH_INTERVAL env.")
	f.linkMonitorSlowInterval = flag.Duration("linkMonitorSlowInterval", d.SlowInterval, "on demand: settings and RDMA capability polling interval (positive). Falls back to LINK_MONITOR_SLOW_INTERVAL env.")
	f.linkMonitorOnDemandTTL = flag.Duration("linkMonitorOnDemandTTL", d.OnDemandTTL, "on demand: how long collected detail is reused; 0 collects on every scrape. Falls back to LINK_MONITOR_ON_DEMAND_TTL env.")
	f.linkMonitorOnDemandWait = flag.Duration("linkMonitorOnDemandWait", d.OnDemandWait, "on demand: longest a detail scrape waits for collection before serving what it has (under 10s). Falls back to LINK_MONITOR_ON_DEMAND_WAIT env.")
	f.linkMonitorErrorsWindow = flag.Duration("linkMonitorErrorsWindow", d.ErrorWindow, "how long an error counter increase stays a problem; 0 disables. Falls back to LINK_MONITOR_ERRORS_WINDOW env.")
	f.linkMonitorFlapWindow = flag.Duration("linkMonitorFlapWindow", d.FlapWindow, "how long a link flap stays a problem; 0 disables. Falls back to LINK_MONITOR_FLAP_WINDOW env.")
	f.linkMonitorErrorsClasses = flag.String("linkMonitorErrorsClasses", strings.Join(d.ErrorClasses, ","), "comma-separated error classes that count as problems: link, rdma, drop. Falls back to LINK_MONITOR_ERRORS_CLASSES env.")
	f.linkMonitorErrorsExclude = flag.String("linkMonitorErrorsExclude", "", "anchored regexp of error series keys never counted as problems. Falls back to LINK_MONITOR_ERRORS_EXCLUDE env.")
	f.linkMonitorMetricsCategories = flag.String("linkMonitorMetricsCategories", strings.Join(monitorprom.Categories(), ","), "comma-separated detail categories served at -promPath/<category>; empty serves none. Falls back to LINK_MONITOR_METRICS_CATEGORIES env.")
	f.linkMonitorInterfacePaths = flag.Bool("linkMonitorMetricsInterfacePaths", true, "serve -promPath/<category>/<interface> and -linkMonitorMetricsAllPath/<interface>. Falls back to LINK_MONITOR_METRICS_INTERFACE_PATHS env.")
	f.linkMonitorMetricsAllPath = flag.String("linkMonitorMetricsAllPath", xtcp.DefaultLinkMonitorAllPath, "path serving every link monitor metric on the Prometheus listener; empty serves none. Falls back to LINK_MONITOR_METRICS_ALL_PATH env.")
	f.linkMonitorIPPaths = flag.Bool("linkMonitorIPPaths", false, "serve the read-only `ip ... show` paths under /ip and /i on the Prometheus listener, or on -linkMonitorIPListen; they expose host addresses. Falls back to LINK_MONITOR_IP_PATHS env.")
	f.linkMonitorIPMaxBytes = flag.Uint("linkMonitorIPMaxBytes", goiphttp.DefaultMaxBytes, "largest /ip response body in bytes. Falls back to LINK_MONITOR_IP_MAX_BYTES env.")
	f.linkMonitorIPListen = flag.String("linkMonitorIPListen", "", "loopback host:port, such as 127.0.0.1:9089, serving /ip and /i on a listener of their own, without listener auth, instead of on the Prometheus listener; needs -linkMonitorIPPaths. Connections from non-loopback addresses are closed. Empty keeps one listener. Falls back to LINK_MONITOR_IP_LISTEN env.")
}

// buildLinkMonitorConfig maps the flags onto XtcpConfig.link_monitor.
func buildLinkMonitorConfig(f *mainFlags) *xtcp_config.LinkMonitorConfig {
	return &xtcp_config.LinkMonitorConfig{
		Enable:                *f.linkMonitor,
		BaselineFile:          *f.linkMonitorBaselineFile,
		Settle:                durationpb.New(*f.linkMonitorSettle),
		Resync:                durationpb.New(*f.linkMonitorResync),
		StatsInterval:         durationpb.New(*f.linkMonitorStatsInterval),
		StatsInclude:          *f.linkMonitorStatsInclude,
		StatsExclude:          *f.linkMonitorStatsExclude,
		NetstatFields:         *f.linkMonitorNetstatFields,
		MaxSpeedExceptions:    splitCSV(*f.linkMonitorMaxSpeedExceptions),
		MetricsCoreInclude:    *f.linkMonitorMetricsCoreInclude,
		MetricsCoreExclude:    *f.linkMonitorMetricsCoreExclude,
		MetricsAll:            *f.linkMonitorMetricsAll,
		CollectionMode:        *f.linkMonitorCollectionMode,
		WatchInterval:         durationpb.New(*f.linkMonitorWatchInterval),
		SlowInterval:          durationpb.New(*f.linkMonitorSlowInterval),
		OnDemandTtl:           durationpb.New(*f.linkMonitorOnDemandTTL),
		OnDemandWait:          durationpb.New(*f.linkMonitorOnDemandWait),
		ErrorWindow:           durationpb.New(*f.linkMonitorErrorsWindow),
		FlapWindow:            durationpb.New(*f.linkMonitorFlapWindow),
		ErrorClasses:          proto.String(*f.linkMonitorErrorsClasses),
		ErrorExclude:          *f.linkMonitorErrorsExclude,
		MetricsCategories:     proto.String(*f.linkMonitorMetricsCategories),
		MetricsInterfacePaths: proto.Bool(*f.linkMonitorInterfacePaths),
		MetricsAllPath:        proto.String(*f.linkMonitorMetricsAllPath),
		IpPaths:               *f.linkMonitorIPPaths,
		IpMaxBytes:            uint32(min(*f.linkMonitorIPMaxBytes, maxLinkMonitorIPMaxBytes)),
		IpListen:              *f.linkMonitorIPListen,
	}
}

// maxLinkMonitorIPMaxBytes is the ip_max_bytes bound in the proto; a larger
// flag is clamped to it, so config validation sees the bound rather than a
// truncated value.
const maxLinkMonitorIPMaxBytes = 1 << 30

// printLinkMonitorFlags prints the link monitor run, linkMonitor through
// linkMonitorIPListen, and pairs with defineLinkMonitorFlags.
func printLinkMonitorFlags(f *mainFlags) {
	fmt.Println("*linkMonitor:", *f.linkMonitor)
	fmt.Println("*linkMonitorBaselineFile:", *f.linkMonitorBaselineFile)
	fmt.Println("*linkMonitorSettle:", *f.linkMonitorSettle)
	fmt.Println("*linkMonitorResync:", *f.linkMonitorResync)
	fmt.Println("*linkMonitorStatsInterval:", *f.linkMonitorStatsInterval)
	fmt.Println("*linkMonitorStatsInclude:", *f.linkMonitorStatsInclude)
	fmt.Println("*linkMonitorStatsExclude:", *f.linkMonitorStatsExclude)
	fmt.Println("*linkMonitorNetstatFields:", *f.linkMonitorNetstatFields)
	fmt.Println("*linkMonitorMaxSpeedExceptions:", *f.linkMonitorMaxSpeedExceptions)
	fmt.Println("*linkMonitorMetricsCoreInclude:", *f.linkMonitorMetricsCoreInclude)
	fmt.Println("*linkMonitorMetricsCoreExclude:", *f.linkMonitorMetricsCoreExclude)
	fmt.Println("*linkMonitorMetricsAll:", *f.linkMonitorMetricsAll)
	fmt.Println("*linkMonitorCollectionMode:", *f.linkMonitorCollectionMode)
	fmt.Println("*linkMonitorWatchInterval:", *f.linkMonitorWatchInterval)
	fmt.Println("*linkMonitorSlowInterval:", *f.linkMonitorSlowInterval)
	fmt.Println("*linkMonitorOnDemandTTL:", *f.linkMonitorOnDemandTTL)
	fmt.Println("*linkMonitorOnDemandWait:", *f.linkMonitorOnDemandWait)
	fmt.Println("*linkMonitorErrorsWindow:", *f.linkMonitorErrorsWindow)
	fmt.Println("*linkMonitorFlapWindow:", *f.linkMonitorFlapWindow)
	fmt.Println("*linkMonitorErrorsClasses:", *f.linkMonitorErrorsClasses)
	fmt.Println("*linkMonitorErrorsExclude:", *f.linkMonitorErrorsExclude)
	fmt.Println("*linkMonitorMetricsCategories:", *f.linkMonitorMetricsCategories)
	fmt.Println("*linkMonitorMetricsInterfacePaths:", *f.linkMonitorInterfacePaths)
	fmt.Println("*linkMonitorMetricsAllPath:", *f.linkMonitorMetricsAllPath)
	fmt.Println("*linkMonitorIPPaths:", *f.linkMonitorIPPaths)
	fmt.Println("*linkMonitorIPMaxBytes:", *f.linkMonitorIPMaxBytes)
	fmt.Println("*linkMonitorIPListen:", *f.linkMonitorIPListen)
}

// envOverrideLinkMonitor applies the LINK_MONITOR_* variables. Like the other
// overrides, an unparsable value is ignored and the flag value stands.
func envOverrideLinkMonitor(c *xtcp_config.XtcpConfig, debugLevel uint) {
	if c.LinkMonitor == nil {
		c.LinkMonitor = &xtcp_config.LinkMonitorConfig{}
	}
	m := c.LinkMonitor
	if v, ok := envBool("LINK_MONITOR"); ok {
		m.Enable = v
		logEnv("LINK_MONITOR", fmt.Sprintf("c.LinkMonitor.Enable:%t", v), debugLevel)
	}
	for _, b := range []struct {
		key    string
		target *bool
	}{
		{"LINK_MONITOR_METRICS_ALL", &m.MetricsAll},
		{"LINK_MONITOR_IP_PATHS", &m.IpPaths},
	} {
		if v, ok := envBool(b.key); ok {
			*b.target = v
			logEnv(b.key, fmt.Sprintf("value:%t", v), debugLevel)
		}
	}
	if v, ok := envBool("LINK_MONITOR_METRICS_INTERFACE_PATHS"); ok {
		m.MetricsInterfacePaths = proto.Bool(v)
		logEnv("LINK_MONITOR_METRICS_INTERFACE_PATHS", fmt.Sprintf("value:%t", v), debugLevel)
	}
	if v, ok := envUint32("LINK_MONITOR_IP_MAX_BYTES"); ok {
		m.IpMaxBytes = v
		logEnv("LINK_MONITOR_IP_MAX_BYTES", fmt.Sprintf("value:%d", v), debugLevel)
	}
	for _, s := range []struct {
		key    string
		target *string
	}{
		{"LINK_MONITOR_BASELINE_FILE", &m.BaselineFile},
		{"LINK_MONITOR_STATS_INCLUDE", &m.StatsInclude},
		{"LINK_MONITOR_STATS_EXCLUDE", &m.StatsExclude},
		{"LINK_MONITOR_NETSTAT_FIELDS", &m.NetstatFields},
		{"LINK_MONITOR_METRICS_CORE_INCLUDE", &m.MetricsCoreInclude},
		{"LINK_MONITOR_METRICS_CORE_EXCLUDE", &m.MetricsCoreExclude},
		{"LINK_MONITOR_COLLECTION_MODE", &m.CollectionMode},
		{"LINK_MONITOR_ERRORS_EXCLUDE", &m.ErrorExclude},
		{"LINK_MONITOR_IP_LISTEN", &m.IpListen},
	} {
		if v, ok := envString(s.key); ok {
			*s.target = v
			logEnv(s.key, fmt.Sprintf("value:%s", v), debugLevel)
		}
	}
	// Optional fields: set, even to empty, overrides the default.
	for _, s := range []struct {
		key    string
		target **string
	}{
		{"LINK_MONITOR_ERRORS_CLASSES", &m.ErrorClasses},
		{"LINK_MONITOR_METRICS_CATEGORIES", &m.MetricsCategories},
		{"LINK_MONITOR_METRICS_ALL_PATH", &m.MetricsAllPath},
	} {
		if v, ok := envString(s.key); ok {
			*s.target = proto.String(v)
			logEnv(s.key, fmt.Sprintf("value:%s", v), debugLevel)
		}
	}
	if v, ok := envString("LINK_MONITOR_MAX_SPEED_EXCEPTIONS"); ok {
		m.MaxSpeedExceptions = splitCSV(v)
		logEnv("LINK_MONITOR_MAX_SPEED_EXCEPTIONS", fmt.Sprintf("c.LinkMonitor.MaxSpeedExceptions:%v", m.MaxSpeedExceptions), debugLevel)
	}
	for _, d := range []struct {
		key    string
		target **durationpb.Duration
	}{
		{"LINK_MONITOR_SETTLE", &m.Settle},
		{"LINK_MONITOR_RESYNC", &m.Resync},
		{"LINK_MONITOR_STATS_INTERVAL", &m.StatsInterval},
		{"LINK_MONITOR_WATCH_INTERVAL", &m.WatchInterval},
		{"LINK_MONITOR_SLOW_INTERVAL", &m.SlowInterval},
		{"LINK_MONITOR_ON_DEMAND_TTL", &m.OnDemandTtl},
		{"LINK_MONITOR_ON_DEMAND_WAIT", &m.OnDemandWait},
		{"LINK_MONITOR_ERRORS_WINDOW", &m.ErrorWindow},
		{"LINK_MONITOR_FLAP_WINDOW", &m.FlapWindow},
	} {
		if v, ok := envDuration(d.key); ok {
			*d.target = durationpb.New(v)
			logEnv(d.key, fmt.Sprintf("value:%s", v), debugLevel)
		}
	}
}

// printLinkMonitorConfig prints XtcpConfig.link_monitor after overrides.
func printLinkMonitorConfig(c *xtcp_config.XtcpConfig) {
	m := c.GetLinkMonitor()
	fmt.Println("c.LinkMonitor.Enable:", m.GetEnable())
	fmt.Println("c.LinkMonitor.BaselineFile:", m.GetBaselineFile())
	fmt.Println("c.LinkMonitor.Settle:", m.GetSettle().AsDuration().String())
	fmt.Println("c.LinkMonitor.Resync:", m.GetResync().AsDuration().String())
	fmt.Println("c.LinkMonitor.StatsInterval:", m.GetStatsInterval().AsDuration().String())
	fmt.Println("c.LinkMonitor.MaxSpeedExceptions:", m.GetMaxSpeedExceptions())
	fmt.Println("c.LinkMonitor.MetricsAll:", m.GetMetricsAll())
	fmt.Println("c.LinkMonitor.CollectionMode:", m.GetCollectionMode())
	fmt.Println("c.LinkMonitor.WatchInterval:", m.GetWatchInterval().AsDuration().String())
	fmt.Println("c.LinkMonitor.SlowInterval:", m.GetSlowInterval().AsDuration().String())
	fmt.Println("c.LinkMonitor.OnDemandTtl:", m.GetOnDemandTtl().AsDuration().String())
	fmt.Println("c.LinkMonitor.OnDemandWait:", m.GetOnDemandWait().AsDuration().String())
	fmt.Println("c.LinkMonitor.ErrorWindow:", m.GetErrorWindow().AsDuration().String())
	fmt.Println("c.LinkMonitor.FlapWindow:", m.GetFlapWindow().AsDuration().String())
	fmt.Println("c.LinkMonitor.ErrorClasses:", m.GetErrorClasses())
	fmt.Println("c.LinkMonitor.MetricsCategories:", m.GetMetricsCategories())
	fmt.Println("c.LinkMonitor.MetricsInterfacePaths:", m.GetMetricsInterfacePaths())
	fmt.Println("c.LinkMonitor.MetricsAllPath:", m.GetMetricsAllPath())
	fmt.Println("c.LinkMonitor.IpPaths:", m.GetIpPaths())
	fmt.Println("c.LinkMonitor.IpMaxBytes:", m.GetIpMaxBytes())
	fmt.Println("c.LinkMonitor.IpListen:", m.GetIpListen())
}
