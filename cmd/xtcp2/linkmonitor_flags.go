package main

import (
	"flag"
	"fmt"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp"
	"google.golang.org/protobuf/types/known/durationpb"
)

// defineLinkMonitorFlags registers the embedded link monitor's flags
// (XtcpConfig.link_monitor, field 250). The defaults are the library's, except
// the baseline file, which is xtcp2's own so a standalone go-link-monitor can
// run beside it.
func defineLinkMonitorFlags(f *mainFlags) {
	d := linkmonitor.DefaultConfig()
	f.linkMonitor = flag.Bool("linkMonitor", false, "run the embedded link monitor: link counts against a baseline, per-interface and RDMA checks and collector health on the Prometheus listener, plus LinkMonitorService on the gRPC listener. Falls back to LINK_MONITOR env.")
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
}

// buildLinkMonitorConfig maps the flags onto XtcpConfig.link_monitor.
func buildLinkMonitorConfig(f *mainFlags) *xtcp_config.LinkMonitorConfig {
	return &xtcp_config.LinkMonitorConfig{
		Enable:             *f.linkMonitor,
		BaselineFile:       *f.linkMonitorBaselineFile,
		Settle:             durationpb.New(*f.linkMonitorSettle),
		Resync:             durationpb.New(*f.linkMonitorResync),
		StatsInterval:      durationpb.New(*f.linkMonitorStatsInterval),
		StatsInclude:       *f.linkMonitorStatsInclude,
		StatsExclude:       *f.linkMonitorStatsExclude,
		NetstatFields:      *f.linkMonitorNetstatFields,
		MaxSpeedExceptions: splitCSV(*f.linkMonitorMaxSpeedExceptions),
		MetricsCoreInclude: *f.linkMonitorMetricsCoreInclude,
		MetricsCoreExclude: *f.linkMonitorMetricsCoreExclude,
		MetricsAll:         *f.linkMonitorMetricsAll,
	}
}

// printLinkMonitorFlags prints the link monitor run, linkMonitor through
// linkMonitorMetricsAll, and pairs with defineLinkMonitorFlags.
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
	if v, ok := envBool("LINK_MONITOR_METRICS_ALL"); ok {
		m.MetricsAll = v
		logEnv("LINK_MONITOR_METRICS_ALL", fmt.Sprintf("c.LinkMonitor.MetricsAll:%t", v), debugLevel)
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
	} {
		if v, ok := envString(s.key); ok {
			*s.target = v
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
}
