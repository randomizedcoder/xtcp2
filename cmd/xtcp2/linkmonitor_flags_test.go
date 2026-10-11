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
			category: "corner", description: "a config without link_monitor gains one",
			expected: "block created and enabled", env: map[string]string{"LINK_MONITOR": "true"},
			start: &xtcp_config.XtcpConfig{},
			check: func(m *xtcp_config.LinkMonitorConfig) bool { return m.GetEnable() },
		},
		{
			category: "corner", description: "no variables leave the flag values untouched",
			expected: "unchanged", start: flagValue(),
			check: func(m *xtcp_config.LinkMonitorConfig) bool {
				return m.GetBaselineFile() == "/flag.json" && !m.GetEnable() && m.GetSettle() == nil
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
