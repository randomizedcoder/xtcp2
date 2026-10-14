package xtcp

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	linkmonitorpb "github.com/randomizedcoder/xtcp2/gen/go/linkmonitor"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	"golang.org/x/sys/unix"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestLinkMonitorSettings(t *testing.T) {
	defaults := linkmonitor.DefaultConfig()
	tests := []struct {
		category, description, expected string
		config                          *xtcp_config.LinkMonitorConfig
		check                           func(linkmonitor.Config) error
		exposure                        string // "summary", "all" or "filter"
		core, bulk                      bool   // expected filter results
		wantErr                         string
	}{
		{
			category: "positive", description: "nil config keeps library defaults with the xtcp2 baseline",
			expected: "defaults, xtcp2 baseline, summary exposure",
			check: func(c linkmonitor.Config) error {
				if c.BaselineFile != DefaultLinkMonitorBaseline || c.Settle != defaults.Settle ||
					c.Resync != defaults.Resync || c.StatsInterval != defaults.StatsInterval ||
					c.StatsInclude != defaults.StatsInclude || c.NetstatFields != defaults.NetstatFields {
					return fmt.Errorf("got %+v", c)
				}
				return nil
			},
			exposure: "summary",
		},
		{
			category: "positive", description: "every set field overrides its default",
			expected: "overrides applied",
			config: &xtcp_config.LinkMonitorConfig{
				BaselineFile: "/tmp/b.json", Settle: durationpb.New(5 * time.Second), Resync: durationpb.New(time.Minute),
				StatsInterval: durationpb.New(2 * time.Second), StatsInclude: "^mlx", StatsExclude: "^lo$",
				NetstatFields: "^Tcp", MaxSpeedExceptions: []string{"eth9"},
			},
			check: func(c linkmonitor.Config) error {
				if c.BaselineFile != "/tmp/b.json" || c.Settle != 5*time.Second || c.Resync != time.Minute ||
					c.StatsInterval != 2*time.Second || c.StatsInclude != "^mlx" || c.StatsExclude != "^lo$" ||
					c.NetstatFields != "^Tcp" || len(c.MaxSpeedExceptions) != 1 || c.MaxSpeedExceptions[0] != "eth9" {
					return fmt.Errorf("got %+v", c)
				}
				return nil
			},
			exposure: "summary",
		},
		{
			category: "positive", description: "metrics_all registers the unfiltered collector",
			expected: "all exposure, pattern ignored",
			config:   &xtcp_config.LinkMonitorConfig{MetricsAll: true, MetricsCoreInclude: "("},
			exposure: "all",
		},
		{
			category: "negative", description: "an invalid core include pattern is rejected",
			expected: "regexp error",
			config:   &xtcp_config.LinkMonitorConfig{MetricsCoreInclude: "("},
			wantErr:  "missing closing )",
		},
		{
			category: "negative", description: "an invalid core exclude pattern is rejected",
			expected: "regexp error",
			config:   &xtcp_config.LinkMonitorConfig{MetricsCoreExclude: "["},
			wantErr:  "missing closing ]",
		},
		{
			category: "boundary", description: "a zero settle duration is applied, not treated as unset",
			expected: "settle 0",
			config:   &xtcp_config.LinkMonitorConfig{Settle: durationpb.New(0)},
			check: func(c linkmonitor.Config) error {
				if c.Settle != 0 {
					return fmt.Errorf("settle = %s", c.Settle)
				}
				return nil
			},
			exposure: "summary",
		},
		{
			category: "corner", description: "a custom include replaces the anchored core pattern",
			expected: "bulk metric selected, core metric not",
			config:   &xtcp_config.LinkMonitorConfig{MetricsCoreInclude: "go_link_monitor_collector_errors_.*"},
			exposure: "filter", bulk: true,
		},
		{
			category: "corner", description: "the exclude pattern removes a core metric",
			expected: "core metric rejected",
			config:   &xtcp_config.LinkMonitorConfig{MetricsCoreExclude: "^go_link_monitor_collection_healthy$"},
			exposure: "filter",
		},
		{
			category: "boundary", description: "an exclude alone filters the core audit set instead of the summary",
			expected: "core metric selected, bulk metric not",
			config:   &xtcp_config.LinkMonitorConfig{MetricsCoreExclude: "^go_link_monitor_up_links$"},
			exposure: "filter", core: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			cfg, exposure, err := linkMonitorSettings(tc.config)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.check != nil {
				if err := tc.check(cfg); err != nil {
					t.Fatal(err)
				}
			}
			got := "filter"
			switch {
			case exposure.all:
				got = "all"
			case exposure.match == nil:
				got = "summary"
			}
			if got != tc.exposure {
				t.Fatalf("exposure = %s, want %s", got, tc.exposure)
			}
			if got != "filter" {
				return
			}
			match := exposure.match
			if got := match("go_link_monitor_collection_healthy"); got != tc.core {
				t.Errorf("core metric selected = %t, want %t", got, tc.core)
			}
			if got := match("go_link_monitor_collector_errors_total"); got != tc.bulk {
				t.Errorf("bulk metric selected = %t, want %t", got, tc.bulk)
			}
		})
	}
}

func TestInitLinkMonitor(t *testing.T) {
	baseline := func(t *testing.T) string { return t.TempDir() + "/baseline.json" }
	tests := []struct {
		category, description, expected string
		config                          func(*testing.T) *xtcp_config.LinkMonitorConfig
		wantMonitor, wantProblems       bool
		wantFatal                       string
	}{
		{
			category: "positive", description: "disabled leaves no monitor",
			expected: "no monitor, no fatal",
			config:   func(*testing.T) *xtcp_config.LinkMonitorConfig { return &xtcp_config.LinkMonitorConfig{} },
		},
		{
			category: "positive", description: "enabled builds the monitor and registers the summary",
			expected: "monitor, collection_healthy and problems",
			config: func(t *testing.T) *xtcp_config.LinkMonitorConfig {
				return &xtcp_config.LinkMonitorConfig{Enable: true, BaselineFile: baseline(t)}
			},
			wantMonitor: true, wantProblems: true,
		},
		{
			category: "positive", description: "a custom include registers only the filtered collector",
			expected: "monitor and collection_healthy, no problems",
			config: func(t *testing.T) *xtcp_config.LinkMonitorConfig {
				return &xtcp_config.LinkMonitorConfig{Enable: true, BaselineFile: baseline(t), MetricsCoreInclude: "go_link_monitor_collection_healthy"}
			},
			wantMonitor: true,
		},
		{
			category: "negative", description: "a bad metric pattern is fatal",
			expected: "fatal",
			config: func(t *testing.T) *xtcp_config.LinkMonitorConfig {
				return &xtcp_config.LinkMonitorConfig{Enable: true, BaselineFile: baseline(t), MetricsCoreInclude: "("}
			},
			wantFatal: "link monitor config",
		},
		{
			category: "negative", description: "a bad stats pattern is rejected by the library",
			expected: "fatal",
			config: func(t *testing.T) *xtcp_config.LinkMonitorConfig {
				return &xtcp_config.LinkMonitorConfig{Enable: true, BaselineFile: baseline(t), StatsInclude: "["}
			},
			wantFatal: "link monitor config",
		},
		{
			category: "boundary", description: "a zero resync interval is rejected",
			expected: "fatal",
			config: func(t *testing.T) *xtcp_config.LinkMonitorConfig {
				return &xtcp_config.LinkMonitorConfig{Enable: true, BaselineFile: baseline(t), Resync: durationpb.New(0)}
			},
			wantFatal: "link monitor config",
		},
		{
			category: "corner", description: "metrics_all with an invalid core pattern ignores the pattern",
			expected: "monitor, no fatal",
			config: func(t *testing.T) *xtcp_config.LinkMonitorConfig {
				return &xtcp_config.LinkMonitorConfig{Enable: true, BaselineFile: baseline(t), MetricsAll: true, MetricsCoreInclude: "("}
			},
			wantMonitor: true, wantProblems: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			r := prometheus.NewRegistry()
			var fatal string
			x := &XTCP{registry: r, config: &xtcp_config.XtcpConfig{LinkMonitor: tc.config(t)}}
			x.fatalf = func(format string, args ...any) { fatal = fmt.Sprintf(format, args...) }
			x.initLinkMonitor()
			if tc.wantFatal != "" {
				if !strings.Contains(fatal, tc.wantFatal) {
					t.Fatalf("fatal = %q, want %q", fatal, tc.wantFatal)
				}
				if x.linkMonitor != nil {
					t.Fatal("monitor set after a fatal error")
				}
				return
			}
			if fatal != "" {
				t.Fatalf("unexpected fatal: %s", fatal)
			}
			if (x.linkMonitor != nil) != tc.wantMonitor {
				t.Fatalf("monitor = %v, want present %t", x.linkMonitor, tc.wantMonitor)
			}
			families, err := r.Gather()
			if err != nil {
				t.Fatal(err)
			}
			found := map[string]bool{}
			for _, f := range families {
				found[f.GetName()] = true
			}
			if found["go_link_monitor_collection_healthy"] != tc.wantMonitor {
				t.Errorf("core metric gathered = %t, want %t", found["go_link_monitor_collection_healthy"], tc.wantMonitor)
			}
			if found["go_link_monitor_problems"] != tc.wantProblems {
				t.Errorf("problems gathered = %t, want %t", found["go_link_monitor_problems"], tc.wantProblems)
			}
		})
	}
}

func TestLinkMonitorService(t *testing.T) {
	tests := []struct {
		category, description, expected string
		enable                          bool
		expectedCode                    codes.Code
	}{
		{"positive", "an embedded monitor answers GetStatus", "OK, not running before Run", true, codes.OK},
		{"negative", "without a monitor the service reports it is disabled", "FailedPrecondition", false, codes.FailedPrecondition},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			x := &XTCP{registry: prometheus.NewRegistry(), config: &xtcp_config.XtcpConfig{
				LinkMonitor: &xtcp_config.LinkMonitorConfig{Enable: tc.enable, BaselineFile: t.TempDir() + "/baseline.json"},
			}}
			x.fatalf = t.Fatalf
			x.initLinkMonitor()
			svc, err := x.newLinkMonitorService()
			if err != nil {
				t.Fatal(err)
			}
			resp, err := svc.GetStatus(context.Background(), &linkmonitorpb.GetStatusRequest{})
			if got := status.Code(err); got != tc.expectedCode {
				t.Fatalf("code = %s (%v), want %s", got, err, tc.expectedCode)
			}
			if tc.expectedCode == codes.OK && resp.GetHealth().GetRunning() {
				t.Fatal("running before Run")
			}
		})
	}
}

// TestRunLinkMonitorBaselineLocked covers the shared-baseline case: another
// monitor (here, a held flock on the sibling lock file) owns the baseline, so
// the daemon fails with the lock error instead of running without link metrics.
func TestRunLinkMonitorBaselineLocked(t *testing.T) {
	path := t.TempDir() + "/baseline.json"
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lock.Close() })
	if err := unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		category, description, expected string
		canceled                        bool
		wantFatal                       string
	}{
		{"negative", "a baseline locked by another monitor is fatal while the daemon runs", "fatal naming the lock", false, "baseline already locked"},
		{"corner", "the same failure after daemon shutdown is only logged", "no fatal", true, ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			var fatal string
			x := &XTCP{registry: prometheus.NewRegistry(), config: &xtcp_config.XtcpConfig{
				LinkMonitor: &xtcp_config.LinkMonitorConfig{Enable: true, BaselineFile: path},
			}}
			x.fatalf = func(format string, args ...any) { fatal = fmt.Sprintf(format, args...) }
			x.initLinkMonitor()
			if x.linkMonitor == nil {
				t.Fatalf("monitor not built: %s", fatal)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if tc.canceled {
				cancel()
			}
			var wg sync.WaitGroup
			wg.Add(1)
			x.runLinkMonitor(ctx, &wg)
			wg.Wait()
			if tc.wantFatal == "" {
				if fatal != "" {
					t.Fatalf("unexpected fatal: %s", fatal)
				}
				return
			}
			if !strings.Contains(fatal, "link monitor stopped") || !strings.Contains(fatal, tc.wantFatal) {
				t.Fatalf("fatal = %q, want %q", fatal, tc.wantFatal)
			}
		})
	}
}
