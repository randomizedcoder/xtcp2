package main

import (
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestEnvOverrideLinkMonitor(t *testing.T) {
	flagValue := func() *xtcp_config.XtcpConfig {
		return &xtcp_config.XtcpConfig{LinkMonitor: &xtcp_config.LinkMonitorConfig{
			BaselineFile: "/flag.json", Resync: durationpb.New(time.Hour),
		}}
	}
	tests := []struct {
		category, description, expected string
		env                             map[string]string
		start                           *xtcp_config.XtcpConfig
		check                           func(*xtcp_config.LinkMonitorConfig) bool
	}{
		{
			category: "positive", description: "LINK_MONITOR enables the monitor",
			expected: "Enable true", env: map[string]string{"LINK_MONITOR": "true"}, start: flagValue(),
			check: func(m *xtcp_config.LinkMonitorConfig) bool { return m.GetEnable() },
		},
		{
			category: "positive", description: "string, list and duration variables override the flags",
			expected: "all fields replaced",
			env: map[string]string{
				"LINK_MONITOR_BASELINE_FILE": "/env.json", "LINK_MONITOR_STATS_INCLUDE": "^mlx",
				"LINK_MONITOR_METRICS_CORE_EXCLUDE": "x", "LINK_MONITOR_MAX_SPEED_EXCEPTIONS": "eth0,eth1",
				"LINK_MONITOR_RESYNC": "5m", "LINK_MONITOR_SETTLE": "0s", "LINK_MONITOR_METRICS_ALL": "1",
			},
			start: flagValue(),
			check: func(m *xtcp_config.LinkMonitorConfig) bool {
				return m.GetBaselineFile() == "/env.json" && m.GetStatsInclude() == "^mlx" &&
					m.GetMetricsCoreExclude() == "x" && len(m.GetMaxSpeedExceptions()) == 2 &&
					m.GetResync().AsDuration() == 5*time.Minute && m.GetSettle() != nil &&
					m.GetSettle().AsDuration() == 0 && m.GetMetricsAll()
			},
		},
		{
			category: "positive", description: "the on-demand variables override the flags",
			expected: "mode, watch, slow and TTL replaced",
			env: map[string]string{
				"LINK_MONITOR_COLLECTION_MODE": "on-demand", "LINK_MONITOR_WATCH_INTERVAL": "20s",
				"LINK_MONITOR_SLOW_INTERVAL": "2m", "LINK_MONITOR_ON_DEMAND_TTL": "0s",
			},
			start: flagValue(),
			check: func(m *xtcp_config.LinkMonitorConfig) bool {
				return m.GetCollectionMode() == "on-demand" && m.GetWatchInterval().AsDuration() == 20*time.Second &&
					m.GetSlowInterval().AsDuration() == 2*time.Minute && m.GetOnDemandTtl() != nil &&
					m.GetOnDemandTtl().AsDuration() == 0
			},
		},
		{
			category: "positive", description: "the problem and path variables override the flags",
			expected: "windows, classes, exclude, categories, interface paths, all path, ip paths and max bytes replaced",
			env: map[string]string{
				"LINK_MONITOR_ON_DEMAND_WAIT": "2s", "LINK_MONITOR_ERRORS_WINDOW": "10m", "LINK_MONITOR_FLAP_WINDOW": "1h",
				"LINK_MONITOR_ERRORS_CLASSES": "drop", "LINK_MONITOR_ERRORS_EXCLUDE": "^eth9/.*",
				"LINK_MONITOR_METRICS_CATEGORIES": "link", "LINK_MONITOR_METRICS_INTERFACE_PATHS": "false",
				"LINK_MONITOR_METRICS_ALL_PATH": "/all", "LINK_MONITOR_IP_PATHS": "true", "LINK_MONITOR_IP_MAX_BYTES": "4096",
			},
			start: flagValue(),
			check: func(m *xtcp_config.LinkMonitorConfig) bool {
				return m.GetOnDemandWait().AsDuration() == 2*time.Second && m.GetErrorWindow().AsDuration() == 10*time.Minute &&
					m.GetFlapWindow().AsDuration() == time.Hour && m.GetErrorClasses() == "drop" && m.GetErrorExclude() == "^eth9/.*" &&
					m.GetMetricsCategories() == "link" && m.MetricsInterfacePaths != nil && !m.GetMetricsInterfacePaths() &&
					m.GetMetricsAllPath() == "/all" && m.GetIpPaths() && m.GetIpMaxBytes() == 4096
			},
		},
		{
			category: "negative", description: "LINK_MONITOR=false turns off the default-on monitor",
			expected: "Enable false", env: map[string]string{"LINK_MONITOR": "false"},
			start: &xtcp_config.XtcpConfig{LinkMonitor: &xtcp_config.LinkMonitorConfig{Enable: true}},
			check: func(m *xtcp_config.LinkMonitorConfig) bool { return !m.GetEnable() },
		},
		{
			category: "negative", description: "unparsable bool and duration values leave the flag values",
			expected: "unchanged", env: map[string]string{"LINK_MONITOR": "maybe", "LINK_MONITOR_RESYNC": "soon"},
			start: flagValue(),
			check: func(m *xtcp_config.LinkMonitorConfig) bool {
				return !m.GetEnable() && m.GetResync().AsDuration() == time.Hour
			},
		},
		{
			category: "boundary", description: "an empty exception list clears the exceptions",
			expected: "nil exceptions", env: map[string]string{"LINK_MONITOR_MAX_SPEED_EXCEPTIONS": ""},
			start: &xtcp_config.XtcpConfig{LinkMonitor: &xtcp_config.LinkMonitorConfig{MaxSpeedExceptions: []string{"eth0"}}},
			check: func(m *xtcp_config.LinkMonitorConfig) bool { return m.GetMaxSpeedExceptions() == nil },
		},
		{
			category: "boundary", description: "set but empty optional variables mean none, not the default",
			expected: "error classes, categories and all path present and empty",
			env:      map[string]string{"LINK_MONITOR_ERRORS_CLASSES": "", "LINK_MONITOR_METRICS_CATEGORIES": "", "LINK_MONITOR_METRICS_ALL_PATH": ""},
			start:    flagValue(),
			check: func(m *xtcp_config.LinkMonitorConfig) bool {
				return m.ErrorClasses != nil && m.GetErrorClasses() == "" && m.MetricsCategories != nil &&
					m.GetMetricsCategories() == "" && m.MetricsAllPath != nil && m.GetMetricsAllPath() == ""
			},
		},
		{
			category: "boundary", description: "an IP max bytes over uint32 is ignored",
			expected: "flag value kept", env: map[string]string{"LINK_MONITOR_IP_MAX_BYTES": "4294967296"},
			start: &xtcp_config.XtcpConfig{LinkMonitor: &xtcp_config.LinkMonitorConfig{IpMaxBytes: 7}},
			check: func(m *xtcp_config.LinkMonitorConfig) bool { return m.GetIpMaxBytes() == 7 },
		},
		{
			category: "corner", description: "a config without link_monitor gains one",
			expected: "block created and enabled", env: map[string]string{"LINK_MONITOR": "true"},
			start: &xtcp_config.XtcpConfig{},
			check: func(m *xtcp_config.LinkMonitorConfig) bool { return m.GetEnable() },
		},
		{
			category: "corner", description: "no variables leave the flag values untouched",
			expected: "unchanged", start: flagValue(),
			check: func(m *xtcp_config.LinkMonitorConfig) bool {
				return m.GetBaselineFile() == "/flag.json" && !m.GetEnable() && m.GetSettle() == nil &&
					m.ErrorClasses == nil && m.MetricsAllPath == nil && !m.GetIpPaths()
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			for k, v := range tc.env {
				t.Setenv(k, v)
			}
			envOverrideLinkMonitor(tc.start, 0)
			if !tc.check(tc.start.GetLinkMonitor()) {
				t.Fatalf("link_monitor = %v", tc.start.GetLinkMonitor())
			}
		})
	}
}
