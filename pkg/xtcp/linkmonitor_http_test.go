package xtcp

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
)

// TestLinkMonitorSettingsDetail covers the go-link-monitor settings xtcp2
// gained with the detail paths: problem windows and classes, the on-demand
// wait and the path layout.
func TestLinkMonitorSettingsDetail(t *testing.T) {
	defaults := linkmonitor.DefaultConfig()
	tests := []struct {
		category, description, expected string
		config                          *xtcp_config.LinkMonitorConfig
		check                           func(linkmonitor.Config, linkMonitorPaths) error
		wantErr                         string
	}{
		{
			category: "positive", description: "unset fields keep the go-link-monitor defaults",
			expected: "link,rdma classes, 180s windows, 5s wait, every category, interface paths, /metrics-all, no /ip",
			config:   &xtcp_config.LinkMonitorConfig{},
			check: func(c linkmonitor.Config, p linkMonitorPaths) error {
				if !slices.Equal(c.ErrorClasses, defaults.ErrorClasses) || c.ErrorWindow != defaults.ErrorWindow ||
					c.FlapWindow != defaults.FlapWindow || c.OnDemandWait != defaults.OnDemandWait || c.ErrorExclude != "" {
					return errors.New("problem settings differ from the library defaults")
				}
				if !slices.Equal(p.categories, monitorprom.Categories()) || !p.interfacePaths || p.allPath != DefaultLinkMonitorAllPath || p.ipPaths || p.ipMaxBytes != 0 {
					return errors.New("path layout differs from the defaults")
				}
				return nil
			},
		},
		{
			category: "positive", description: "every new field overrides its default",
			expected: "overrides applied",
			config: &xtcp_config.LinkMonitorConfig{
				OnDemandWait: durationpb.New(2 * time.Second), ErrorWindow: durationpb.New(10 * time.Minute),
				FlapWindow: durationpb.New(time.Hour), ErrorClasses: proto.String("drop"), ErrorExclude: "^eth9/.*",
				MetricsCategories: proto.String("link,rdma"), MetricsInterfacePaths: proto.Bool(false),
				MetricsAllPath: proto.String("/all"), IpPaths: true, IpMaxBytes: 4096,
			},
			check: func(c linkmonitor.Config, p linkMonitorPaths) error {
				if c.OnDemandWait != 2*time.Second || c.ErrorWindow != 10*time.Minute || c.FlapWindow != time.Hour ||
					!slices.Equal(c.ErrorClasses, []string{"drop"}) || c.ErrorExclude != "^eth9/.*" {
					return errors.New("problem settings not applied")
				}
				if !slices.Equal(p.categories, []string{"link", "rdma"}) || p.interfacePaths || p.allPath != "/all" || !p.ipPaths || p.ipMaxBytes != 4096 {
					return errors.New("path layout not applied")
				}
				return nil
			},
		},
		{
			category: "negative", description: "an on-demand wait of the full detail timeout is rejected",
			expected: "error naming the timeout",
			config:   &xtcp_config.LinkMonitorConfig{OnDemandWait: durationpb.New(10 * time.Second)},
			wantErr:  "detail path timeout",
		},
		{
			category: "boundary", description: "an on-demand wait just under the detail timeout is accepted",
			expected: "9.999s wait",
			config:   &xtcp_config.LinkMonitorConfig{OnDemandWait: durationpb.New(9999 * time.Millisecond)},
			check: func(c linkmonitor.Config, _ linkMonitorPaths) error {
				if c.OnDemandWait != 9999*time.Millisecond {
					return errors.New("wait not applied")
				}
				return nil
			},
		},
		{
			category: "boundary", description: "set but empty lists and path mean none",
			expected: "no error classes, no categories, no all-metrics path",
			config:   &xtcp_config.LinkMonitorConfig{ErrorClasses: proto.String(""), MetricsCategories: proto.String(""), MetricsAllPath: proto.String("")},
			check: func(c linkmonitor.Config, p linkMonitorPaths) error {
				if len(c.ErrorClasses) != 0 || len(p.categories) != 0 || p.allPath != "" {
					return errors.New("empty values not applied")
				}
				return nil
			},
		},
		{
			category: "corner", description: "metrics_all keeps the path layout",
			expected: "all exposure and the default paths",
			config:   &xtcp_config.LinkMonitorConfig{MetricsAll: true},
			check: func(_ linkmonitor.Config, p linkMonitorPaths) error {
				if p.allPath != DefaultLinkMonitorAllPath || len(p.categories) == 0 {
					return errors.New("path layout lost with metrics_all")
				}
				return nil
			},
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
			if err := tc.check(cfg, exposure.paths); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestLinkMonitorHandler serves the detail paths from a monitor that has not
// run: the registries answer from the empty snapshot, so only path routing,
// validation and the down and on-demand states are exercised here. The
// linkmonitorhttp tests cover the route behavior itself.
func TestLinkMonitorHandler(t *testing.T) {
	type request struct {
		path string
		code int
	}
	tests := []struct {
		category, description, expected string
		config                          *xtcp_config.LinkMonitorConfig
		disabled, down                  bool
		promPath                        string
		requests                        []request
		wantErr                         string
	}{
		{
			category: "positive", description: "the default layout serves categories and the all-metrics path",
			expected: "200 for /metrics/link, /metrics/rdma, /metrics-all",
			config:   &xtcp_config.LinkMonitorConfig{CollectionMode: "poll"},
			requests: []request{{"/metrics/link", http.StatusOK}, {"/metrics/rdma", http.StatusOK}, {"/metrics-all", http.StatusOK}},
		},
		{
			category: "positive", description: "a custom promPath moves the category paths",
			expected: "200 under /prom/, 404 under /metrics/",
			config:   &xtcp_config.LinkMonitorConfig{CollectionMode: "poll"}, promPath: "/prom",
			requests: []request{{"/prom/link", http.StatusOK}, {"/metrics/link", http.StatusNotFound}},
		},
		{
			category: "negative", description: "a disabled monitor has no handler",
			expected: "nil handler, no error",
			disabled: true,
		},
		{
			category: "negative", description: "unknown paths and interfaces are not found",
			expected: "404",
			config:   &xtcp_config.LinkMonitorConfig{CollectionMode: "poll"},
			requests: []request{{"/metrics/nosuch", http.StatusNotFound}, {"/metrics/link/nosuch0", http.StatusNotFound}, {"/", http.StatusNotFound}},
		},
		{
			category: "negative", description: "the /ip paths are off by default",
			expected: "404 for /ip/link and /i/l",
			config:   &xtcp_config.LinkMonitorConfig{CollectionMode: "poll"},
			requests: []request{{"/ip/link", http.StatusNotFound}, {"/i/l", http.StatusNotFound}},
		},
		{
			category: "negative", description: "the all-metrics path cannot take promPath",
			expected: "path collision error",
			config:   &xtcp_config.LinkMonitorConfig{MetricsAllPath: proto.String("/metrics")},
			wantErr:  `collides with "/metrics"`,
		},
		{
			category: "negative", description: "the all-metrics path cannot take a health path",
			expected: "path collision error",
			config:   &xtcp_config.LinkMonitorConfig{MetricsAllPath: proto.String("/readyz")},
			wantErr:  `collides with "/readyz"`,
		},
		{
			category: "negative", description: "the all-metrics path cannot sit under pprof",
			expected: "path collision error",
			config:   &xtcp_config.LinkMonitorConfig{MetricsAllPath: proto.String("/debug/pprof/x")},
			wantErr:  `collides with "/debug/pprof/"`,
		},
		{
			category: "negative", description: "the all-metrics path cannot take a category path",
			expected: "path collision error",
			config:   &xtcp_config.LinkMonitorConfig{MetricsAllPath: proto.String("/metrics/link")},
			wantErr:  "collides with a category path",
		},
		{
			category: "negative", description: "an unknown category is rejected",
			expected: "category error",
			config:   &xtcp_config.LinkMonitorConfig{MetricsCategories: proto.String("link,bogus")},
			wantErr:  "bogus",
		},
		{
			category: "boundary", description: "pprof without its trailing slash is reserved too",
			expected: "path collision error",
			config:   &xtcp_config.LinkMonitorConfig{MetricsAllPath: proto.String("/debug/pprof")},
			wantErr:  `collides with "/debug/pprof/"`,
		},
		{
			category: "boundary", description: "no categories and no all-metrics path serve nothing",
			expected: "404 everywhere",
			config:   &xtcp_config.LinkMonitorConfig{CollectionMode: "poll", MetricsCategories: proto.String(""), MetricsAllPath: proto.String("")},
			requests: []request{{"/metrics/link", http.StatusNotFound}, {"/metrics-all", http.StatusNotFound}},
		},
		{
			category: "boundary", description: "a category subset serves only its categories",
			expected: "200 for link, 404 for rdma",
			config:   &xtcp_config.LinkMonitorConfig{CollectionMode: "poll", MetricsCategories: proto.String("link")},
			requests: []request{{"/metrics/link", http.StatusOK}, {"/metrics/rdma", http.StatusNotFound}},
		},
		{
			category: "corner", description: "a failed monitor answers 503 on every path",
			expected: "503, even for unknown paths",
			config:   &xtcp_config.LinkMonitorConfig{CollectionMode: "poll"}, down: true,
			requests: []request{{"/metrics/link", http.StatusServiceUnavailable}, {"/metrics-all", http.StatusServiceUnavailable}, {"/nosuch", http.StatusServiceUnavailable}},
		},
		{
			category: "corner", description: "on demand, a detail scrape before the monitor runs cannot refresh",
			expected: "503 from the refresh",
			config:   &xtcp_config.LinkMonitorConfig{CollectionMode: "on-demand"},
			requests: []request{{"/metrics/link", http.StatusServiceUnavailable}, {"/metrics-all", http.StatusServiceUnavailable}},
		},
		{
			category: "corner", description: "the /ip paths cannot take promPath when enabled",
			expected: "path collision error",
			config:   &xtcp_config.LinkMonitorConfig{IpPaths: true}, promPath: "/ip",
			wantErr: "collides with the /ip paths",
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			c := tc.config
			if c == nil {
				c = &xtcp_config.LinkMonitorConfig{}
			}
			c.Enable, c.BaselineFile = !tc.disabled, t.TempDir()+"/baseline.json"
			x := &XTCP{registry: prometheus.NewRegistry(), config: &xtcp_config.XtcpConfig{LinkMonitor: c}}
			x.fatalf = t.Fatalf
			x.initLinkMonitor()
			x.linkMonitorDown.Store(tc.down)
			promPath := tc.promPath
			if promPath == "" {
				promPath = "/metrics"
			}
			h, err := x.LinkMonitorHandler(promPath)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.disabled {
				if h != nil {
					t.Fatal("handler without a monitor")
				}
				return
			}
			for _, r := range tc.requests {
				rec := httptest.NewRecorder()
				h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, r.path, nil))
				if rec.Code != r.code {
					t.Errorf("GET %s = %d, want %d", r.path, rec.Code, r.code)
				}
			}
		})
	}
}

// TestLinkMonitorIPHandler covers where the /ip and /i paths are served: on
// the Prometheus listener by default, on their own listener with ip_listen,
// and nowhere with ip_paths off.
func TestLinkMonitorIPHandler(t *testing.T) {
	type request struct {
		path string
		code int
	}
	const loopback = "127.0.0.1:9089"
	tests := []struct {
		category, description, expected string
		config                          *xtcp_config.LinkMonitorConfig
		disabled, down                  bool
		promPath                        string
		prom, ip                        []request // nil ip: no /ip handler
	}{
		{
			category: "positive", description: "ip_listen moves /ip and /i off the Prometheus listener",
			expected: "404 on the Prometheus listener, 200 on the /ip listener",
			config:   &xtcp_config.LinkMonitorConfig{CollectionMode: "poll", IpPaths: true, IpListen: loopback},
			prom:     []request{{"/ip/link", http.StatusNotFound}, {"/i/l", http.StatusNotFound}, {"/metrics/link", http.StatusOK}},
			ip:       []request{{"/ip/link", http.StatusOK}, {"/i/l", http.StatusOK}},
		},
		{
			category: "positive", description: "without ip_listen the Prometheus listener keeps them",
			expected: "200 on the Prometheus listener, no /ip handler",
			config:   &xtcp_config.LinkMonitorConfig{CollectionMode: "poll", IpPaths: true},
			prom:     []request{{"/ip/link", http.StatusOK}, {"/i/l", http.StatusOK}},
		},
		{
			category: "negative", description: "ip_listen without ip_paths serves /ip nowhere",
			expected: "404 on the Prometheus listener, no /ip handler",
			config:   &xtcp_config.LinkMonitorConfig{CollectionMode: "poll", IpListen: loopback},
			prom:     []request{{"/ip/link", http.StatusNotFound}},
		},
		{
			category: "negative", description: "a disabled monitor has no /ip handler",
			expected: "nil handlers",
			config:   &xtcp_config.LinkMonitorConfig{IpPaths: true, IpListen: loopback}, disabled: true,
		},
		{
			category: "boundary", description: "the /ip listener serves only /ip and /i",
			expected: "404 for the detail and health paths there",
			config:   &xtcp_config.LinkMonitorConfig{CollectionMode: "poll", IpPaths: true, IpListen: loopback},
			ip:       []request{{"/metrics/link", http.StatusNotFound}, {"/metrics-all", http.StatusNotFound}, {"/readyz", http.StatusNotFound}, {"/", http.StatusNotFound}},
		},
		{
			category: "corner", description: "with ip_listen, promPath /ip no longer collides",
			expected: "no error; the categories serve under /ip/ on the Prometheus listener",
			config:   &xtcp_config.LinkMonitorConfig{CollectionMode: "poll", IpPaths: true, IpListen: loopback}, promPath: "/ip",
			prom: []request{{"/ip/link", http.StatusOK}},
			ip:   []request{{"/ip/link", http.StatusOK}},
		},
		{
			category: "corner", description: "a failed monitor answers 503 on the /ip listener too",
			expected: "503",
			config:   &xtcp_config.LinkMonitorConfig{CollectionMode: "poll", IpPaths: true, IpListen: loopback}, down: true,
			ip: []request{{"/ip/link", http.StatusServiceUnavailable}},
		},
	}
	serve := func(t *testing.T, name string, h http.Handler, requests []request) {
		t.Helper()
		for _, r := range requests {
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, r.path, nil))
			if rec.Code != r.code {
				t.Errorf("%s: GET %s = %d, want %d", name, r.path, rec.Code, r.code)
			}
		}
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			c := tc.config
			c.Enable, c.BaselineFile = !tc.disabled, t.TempDir()+"/baseline.json"
			x := &XTCP{registry: prometheus.NewRegistry(), config: &xtcp_config.XtcpConfig{LinkMonitor: c}}
			x.fatalf = t.Fatalf
			x.initLinkMonitor()
			x.linkMonitorDown.Store(tc.down)
			promPath := tc.promPath
			if promPath == "" {
				promPath = "/metrics"
			}
			prom, err := x.LinkMonitorHandler(promPath)
			if err != nil {
				t.Fatal(err)
			}
			ip, err := x.LinkMonitorIPHandler()
			if err != nil {
				t.Fatal(err)
			}
			if tc.disabled {
				if prom != nil || ip != nil {
					t.Fatal("handlers without a monitor")
				}
				return
			}
			serve(t, "prometheus listener", prom, tc.prom)
			if (ip != nil) != (tc.ip != nil) {
				t.Fatalf("/ip handler present = %t, want %t", ip != nil, tc.ip != nil)
			}
			if ip != nil {
				serve(t, "/ip listener", ip, tc.ip)
			}
		})
	}
}

// TestLinkMonitorPromMetrics checks what initLinkMonitor adds to promPath
// beside the summary: the collection mode and, on demand, the refresh
// outcomes.
func TestLinkMonitorPromMetrics(t *testing.T) {
	tests := []struct {
		category, description, expected string
		mode                            string
		wantRefresh                     bool
	}{
		{"positive", "on demand registers the mode and the refresh counter", "mode=on-demand, refresh counter", "on-demand", true},
		{"negative", "poll registers no refresh counter", "mode=poll, no refresh counter", "poll", false},
		{"corner", "an empty mode is poll", "mode=poll, no refresh counter", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			registry := prometheus.NewRegistry()
			x := &XTCP{registry: registry, config: &xtcp_config.XtcpConfig{LinkMonitor: &xtcp_config.LinkMonitorConfig{
				Enable: true, BaselineFile: t.TempDir() + "/baseline.json", CollectionMode: tc.mode,
			}}}
			x.fatalf = t.Fatalf
			x.initLinkMonitor()
			if x.linkMonitor == nil {
				t.Fatal("monitor not built")
			}
			families, err := registry.Gather()
			if err != nil {
				t.Fatal(err)
			}
			mode := ""
			for _, f := range families {
				if f.GetName() == "go_link_monitor_collection_mode" {
					mode = f.GetMetric()[0].GetLabel()[0].GetValue()
				}
			}
			want := tc.mode
			if want == "" {
				want = "poll"
			}
			if mode != want {
				t.Errorf("collection mode = %q, want %q", mode, want)
			}
			// The counter vector has no series until a refresh, so check its
			// registration by registering a duplicate.
			dup := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "go_link_monitor_on_demand_refreshes_total", Help: "On-demand detail scrapes by refresh outcome: cached, collected or timeout."}, []string{"result"})
			registered := errors.As(registry.Register(dup), new(prometheus.AlreadyRegisteredError))
			if registered != tc.wantRefresh {
				t.Errorf("refresh counter registered = %t, want %t", registered, tc.wantRefresh)
			}
		})
	}
}
