package xtcp

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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
			expected: "defaults, xtcp2 baseline, summary exposure, poll mode (no detail path refreshes)",
			check: func(c linkmonitor.Config) error {
				if c.CollectionMode != linkmonitor.CollectionPoll {
					return fmt.Errorf("collection mode %q, want poll", c.CollectionMode)
				}
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
			category: "positive", description: "collection_mode on-demand with its intervals",
			expected: "on-demand mode; watch, slow and TTL applied",
			config: &xtcp_config.LinkMonitorConfig{
				CollectionMode: "on-demand", WatchInterval: durationpb.New(20 * time.Second),
				SlowInterval: durationpb.New(2 * time.Minute), OnDemandTtl: durationpb.New(5 * time.Second),
			},
			check: func(c linkmonitor.Config) error {
				if c.CollectionMode != linkmonitor.CollectionOnDemand || c.WatchInterval != 20*time.Second ||
					c.SlowInterval != 2*time.Minute || c.OnDemandTTL != 5*time.Second {
					return fmt.Errorf("got %+v", c)
				}
				return nil
			},
			exposure: "summary",
		},
		{
			category: "negative", description: "an unknown collection mode passes through for New to reject",
			expected: "mode kept; linkmonitor.New fails naming it",
			config:   &xtcp_config.LinkMonitorConfig{CollectionMode: "lazy"},
			check: func(c linkmonitor.Config) error {
				if _, err := linkmonitor.New(c, linkmonitor.Options{}); err == nil || !strings.Contains(err.Error(), `"lazy"`) {
					return fmt.Errorf("New err = %v, want invalid collection mode", err)
				}
				return nil
			},
			exposure: "summary",
		},
		{
			category: "boundary", description: "a zero on-demand TTL is applied, not treated as unset",
			expected: "TTL 0; intervals unset keep the library defaults",
			config:   &xtcp_config.LinkMonitorConfig{CollectionMode: "on-demand", OnDemandTtl: durationpb.New(0)},
			check: func(c linkmonitor.Config) error {
				if c.OnDemandTTL != 0 || c.WatchInterval != defaults.WatchInterval || c.SlowInterval != defaults.SlowInterval {
					return fmt.Errorf("got %+v", c)
				}
				return nil
			},
			exposure: "summary",
		},
		{
			category: "corner", description: "on-demand intervals set in poll mode are kept but unused",
			expected: "poll mode; watch interval stored",
			config:   &xtcp_config.LinkMonitorConfig{CollectionMode: "poll", WatchInterval: durationpb.New(time.Second)},
			check: func(c linkmonitor.Config) error {
				if c.CollectionMode != linkmonitor.CollectionPoll || c.WatchInterval != time.Second {
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

type fakeRefresher struct {
	calls int
	scope linkmonitor.RefreshScope
	err   error
}

func (f *fakeRefresher) Refresh(_ context.Context, scope linkmonitor.RefreshScope) (linkmonitor.RefreshResult, error) {
	f.calls++
	f.scope = scope
	return linkmonitor.RefreshResult{}, f.err
}

func TestLinkMonitorRefreshing(t *testing.T) {
	m, err := linkmonitor.New(linkmonitor.DefaultConfig(), linkmonitor.Options{})
	if err != nil {
		t.Fatal(err)
	}
	match := func(string) bool { return true }
	tests := []struct {
		category, description, expected string
		exposure                        linkMonitorExposure
		err                             error
		wrapped                         int // collectors wrapped in refreshing
		collects                        int // refresh calls after one Collect of each
	}{
		{category: "positive", description: "metrics_all refreshes before the detail collector",
			expected: "detail collector wrapped, summary not; one refresh of every collector",
			exposure: linkMonitorExposure{all: true}, wrapped: 1, collects: 1},
		{category: "positive", description: "a name filter refreshes before it collects",
			expected: "one wrapped collector; one refresh", exposure: linkMonitorExposure{match: match}, wrapped: 1, collects: 1},
		{category: "negative", description: "a refresh error still serves the snapshot",
			expected: "Collect returns after the failed refresh", exposure: linkMonitorExposure{match: match},
			err: linkmonitor.ErrUnknownInterface, wrapped: 1, collects: 1},
		{category: "boundary", description: "the summary never refreshes",
			expected: "nothing wrapped; no refresh", exposure: linkMonitorExposure{}},
		{category: "corner", description: "a monitor that is not running answers Refresh at once",
			expected: "Collect returns; poll and unstarted monitors do not block", exposure: linkMonitorExposure{all: true}, wrapped: 1},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			fake := &fakeRefresher{err: tc.err}
			wrapped := 0
			for _, c := range tc.exposure.collectors(m) {
				r, ok := c.(refreshing)
				if !ok {
					continue
				}
				wrapped++
				if tc.category != "corner" {
					r.monitor = fake
				}
				ch := make(chan prometheus.Metric, 1024)
				r.Collect(ch)
			}
			if wrapped != tc.wrapped {
				t.Errorf("wrapped %d collectors, want %d", wrapped, tc.wrapped)
			}
			if fake.calls != tc.collects {
				t.Errorf("%d refreshes, want %d", fake.calls, tc.collects)
			}
			if fake.calls > 0 && fake.scope != (linkmonitor.RefreshScope{Collectors: linkmonitor.AllCollectors()}) {
				t.Errorf("scope %+v, want every collector on every device", fake.scope)
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

// netstatSuccess is the host netstat collector's last success in the
// monitor's latest snapshot, read without a refresh; zero when absent.
func netstatSuccess(m *linkmonitor.Monitor) time.Time {
	at, _ := m.Snapshot().HostCollector().LastSuccess()
	return at
}

// netstatQuiet is the latest resync, when netstat's collections for it are
// done. Netstat is collected when its job registers and again after each
// full reconciliation (resyncHost), and startup runs more than one of those,
// so a gather can race a follow-up collection. It's quiet once the last
// success started no earlier than the last resync: a success that only
// finished after it can be the registration's, with the follow-up to come.
func netstatQuiet(m *linkmonitor.Monitor) (time.Time, bool) {
	s := m.Snapshot()
	resync, ok := s.LastSuccessfulResync()
	at, succeeded := s.HostCollector().LastSuccess()
	took, timed := s.HostCollector().Duration()
	return resync, ok && succeeded && timed && !at.Add(-took).Before(resync)
}

// netSysfsAbsent reports whether the sysfs network class directory is missing,
// as in the Nix build sandbox. Anything else that stops it being a readable
// directory is an error, so a broken host fails rather than skips.
func netSysfsAbsent(path string) (bool, error) {
	info, err := os.Stat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !info.IsDir() {
		return false, fmt.Errorf("%s is not a directory", path)
	}
	return false, nil
}

// requireNetSysfs skips a test that runs the monitor against real links when
// the host has no sysfs.
func requireNetSysfs(t *testing.T) {
	t.Helper()
	absent, err := netSysfsAbsent("/sys/class/net")
	if err != nil {
		t.Fatal(err)
	}
	if absent {
		t.Skip("needs /sys/class/net: the embedded monitor reconciles real links; test-linkmonitor-embedding-vm runs this test")
	}
}

func TestNetSysfsAbsent(t *testing.T) {
	dir := t.TempDir()
	file := dir + "/file"
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	locked := dir + "/locked"
	if err := os.Mkdir(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name, category, description, expected string
		path                                  string
		absent, wantErr, needsNonRoot         bool
	}{
		{name: "dir_present", category: "positive", description: "the path is an existing directory",
			expected: "present: the test runs", path: dir},
		{name: "missing", category: "negative", description: "the path does not exist",
			expected: "absent: the test skips", path: dir + "/net", absent: true},
		{name: "missing_parent", category: "boundary", description: "the parent is missing too, as /sys is in the sandbox",
			expected: "absent: the test skips", path: dir + "/sys/class/net", absent: true},
		{name: "file_not_dir", category: "corner", description: "the path is a regular file",
			expected: "an error: the test fails", path: file, wantErr: true},
		{name: "unreadable_parent", category: "corner", description: "the parent cannot be searched, so stat gets EACCES",
			expected: "an error: the test fails", path: locked + "/net", wantErr: true, needsNonRoot: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			if tc.needsNonRoot {
				if os.Geteuid() == 0 {
					t.Skip("root ignores directory permissions")
				}
				if err := os.Chmod(locked, 0); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
			}
			absent, err := netSysfsAbsent(tc.path)
			if (err != nil) != tc.wantErr || absent != tc.absent {
				t.Fatalf("netSysfsAbsent(%q) = %t, %v; want %t, error %t", tc.path, absent, err, tc.absent, tc.wantErr)
			}
		})
	}
}

// TestLinkMonitorOnDemandScrape runs the embedded monitor and gathers xtcp2's
// registry twice: an on-demand detail exposure collects netstat again when its
// data is older than the TTL, and only then. It needs real links, so it skips
// in the build sandbox and runs in test-linkmonitor-embedding-vm.
func TestLinkMonitorOnDemandScrape(t *testing.T) {
	requireNetSysfs(t)
	tests := []struct {
		category, description, expected string
		config                          *xtcp_config.LinkMonitorConfig
		advances                        bool
	}{
		{category: "positive", description: "on demand with a zero TTL collects on every gather",
			expected: "netstat last success advances", advances: true,
			config: &xtcp_config.LinkMonitorConfig{CollectionMode: "on-demand", OnDemandTtl: durationpb.New(0), MetricsAll: true}},
		{category: "positive", description: "a core-pattern exposure refreshes too",
			expected: "netstat last success advances", advances: true,
			config: &xtcp_config.LinkMonitorConfig{CollectionMode: "on-demand", OnDemandTtl: durationpb.New(0), MetricsCoreInclude: "go_link_monitor_collector_.*"}},
		{category: "negative", description: "poll mode never collects on a gather",
			expected: "netstat keeps its startup collection",
			config:   &xtcp_config.LinkMonitorConfig{CollectionMode: "poll", StatsInterval: durationpb.New(2 * time.Minute), MetricsAll: true}},
		{category: "boundary", description: "an empty mode is poll",
			expected: "netstat keeps its startup collection",
			config:   &xtcp_config.LinkMonitorConfig{StatsInterval: durationpb.New(2 * time.Minute), MetricsAll: true}},
		{category: "corner", description: "on demand within the TTL reuses the startup collection",
			expected: "netstat last success unchanged",
			config:   &xtcp_config.LinkMonitorConfig{CollectionMode: "on-demand", OnDemandTtl: durationpb.New(time.Hour), MetricsAll: true}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			tc.config.Enable, tc.config.BaselineFile = true, t.TempDir()+"/baseline.json"
			registry := prometheus.NewRegistry()
			x := &XTCP{registry: registry, config: &xtcp_config.XtcpConfig{LinkMonitor: tc.config}}
			x.fatalf = func(format string, args ...any) { t.Errorf(format, args...) }
			x.initLinkMonitor()
			if x.linkMonitor == nil {
				t.Fatal("monitor not built")
			}
			ctx, cancel := context.WithCancel(context.Background())
			var wg sync.WaitGroup
			wg.Add(1)
			go x.runLinkMonitor(ctx, &wg)
			t.Cleanup(func() { cancel(); wg.Wait() })

			// Gather while netstat is quiet. A resync during the gather can
			// collect netstat by itself, so measure again.
			var first, second time.Time
			for deadline := time.Now().Add(20 * time.Second); ; time.Sleep(50 * time.Millisecond) {
				if time.Now().After(deadline) {
					t.Fatal("netstat never quiet between resyncs")
				}
				resync, quiet := netstatQuiet(x.linkMonitor)
				if !quiet {
					continue
				}
				first = netstatSuccess(x.linkMonitor)
				if _, err := registry.Gather(); err != nil {
					t.Fatal(err)
				}
				second = netstatSuccess(x.linkMonitor)
				if after, _ := x.linkMonitor.Snapshot().LastSuccessfulResync(); after.Equal(resync) {
					break
				}
			}
			if advanced := second.After(first); advanced != tc.advances {
				t.Errorf("netstat last success %v then %v: advanced %t, want %t", first, second, advanced, tc.advances)
			}
		})
	}
}
