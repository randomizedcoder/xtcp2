package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/linkmonitorhttp"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
)

// fakeRefresher records each scope and answers with result and err; when
// block is set each call waits on it.
type fakeRefresher struct {
	mu     sync.Mutex
	scopes []linkmonitor.RefreshScope
	result linkmonitor.RefreshResult
	err    error
	block  chan struct{}
}

func (f *fakeRefresher) Refresh(ctx context.Context, scope linkmonitor.RefreshScope) (linkmonitor.RefreshResult, error) {
	f.mu.Lock()
	f.scopes = append(f.scopes, scope)
	f.mu.Unlock()
	if f.block != nil {
		select {
		case <-f.block:
		case <-ctx.Done():
			return linkmonitor.RefreshResult{}, ctx.Err()
		}
	}
	return f.result, f.err
}

func (f *fakeRefresher) calls() []linkmonitor.RefreshScope {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]linkmonitor.RefreshScope(nil), f.scopes...)
}

func mustCollectors(t *testing.T, names ...string) linkmonitor.CollectorSet {
	t.Helper()
	set, err := linkmonitor.Collectors(names...)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

// onDemandHandler serves every path in categories, the all path and the
// interface views, refreshing through f unless poll is set.
func onDemandHandler(t *testing.T, f *fakeRefresher, poll bool) (http.Handler, *prometheus.Registry) {
	t.Helper()
	categories := monitorprom.Categories()
	o := exposureOptions{allPath: defaultAllPath, categories: categories, interfacePaths: true}
	gatherers := make(map[string]prometheus.Gatherer)
	for _, category := range categories {
		gatherers[category] = markedRegistry(category + "_marker")
	}
	served := endpoints(":9101", o, markedRegistry("core_marker"), markedRegistry("all_marker"), gatherers, fakeViews(categories, "eth1"))
	registry := prometheus.NewRegistry()
	if !poll {
		var err error
		if served, err = refreshRoutes(served, f, o.allPath, linkmonitorhttp.NewRefreshMetrics(registry)); err != nil {
			t.Fatal(err)
		}
	}
	var stop atomic.Bool
	return httpServer(&fakeMonitor{}, &stop, served[0].routes...).Handler, registry
}

func TestHTTPOnDemand(t *testing.T) {
	rdma := mustCollectors(t, monitorprom.CategoryCollectors(monitorprom.CategoryRDMA)...)
	driver := mustCollectors(t, "driver", "phy")
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		path                                         string
		poll                                         bool
		result                                       linkmonitor.RefreshResult
		err                                          error
		status                                       int
		marker, header                               string
		want                                         []linkmonitor.RefreshScope
	}{
		{"summary-no-refresh", "positive", "GET /metrics on demand", "served from the snapshot, no refresh", "/metrics", false, linkmonitor.RefreshResult{}, nil, 200, "core_marker", "", nil},
		{"category-kinds", "positive", "GET /metrics/rdma", "one refresh of the RDMA collectors", "/metrics/rdma", false, linkmonitor.RefreshResult{Collected: 1}, nil, 200, "rdma_marker", "", []linkmonitor.RefreshScope{{Collectors: rdma}}},
		{"all", "positive", "GET /metrics-all", "one refresh of every collector", "/metrics-all", false, linkmonitor.RefreshResult{Cached: 3}, nil, 200, "all_marker", "", []linkmonitor.RefreshScope{{Collectors: linkmonitor.AllCollectors()}}},
		{"interface", "positive", "GET /metrics/driver/eth1", "refresh of driver and phy on eth1", "/metrics/driver/eth1", false, linkmonitor.RefreshResult{Collected: 2}, nil, 200, "driver_iface_marker", "", []linkmonitor.RefreshScope{{Collectors: driver, Interface: "eth1"}}},
		{"collection-no-refresh", "boundary", "GET /metrics/collection", "no refresh: collection health is published state", "/metrics/collection", false, linkmonitor.RefreshResult{}, nil, 200, "collection_marker", "", nil},
		{"timeout-serves", "negative", "the refresh times out", "200 with the refresh header, from the current snapshot", "/metrics/netstat", false, linkmonitor.RefreshResult{TimedOut: true}, nil, 200, "netstat_marker", "timeout", []linkmonitor.RefreshScope{{Collectors: mustCollectors(t, "netstat")}}},
		{"unknown-interface", "negative", "refresh of eth9", "404 as the view would answer", "/metrics/link/eth9", false, linkmonitor.RefreshResult{}, linkmonitor.ErrUnknownInterface, 404, "unknown interface", "", []linkmonitor.RefreshScope{{Collectors: mustCollectors(t, monitorprom.CategoryCollectors(monitorprom.CategoryLink)...), Interface: "eth9"}}},
		{"not-running", "negative", "the monitor stopped", "503", "/metrics/traffic", false, linkmonitor.RefreshResult{}, linkmonitor.ErrNotRunning, 503, "not accepting", "", []linkmonitor.RefreshScope{{Collectors: mustCollectors(t, "standard")}}},
		{"invalid-segment", "boundary", "an invalid interface segment", "400 from the view, no refresh", "/metrics/link/%2E", false, linkmonitor.RefreshResult{}, nil, 400, "invalid interface", "", nil},
		{"poll-mode", "corner", "the same path in poll mode", "no refresh wrapper at all", "/metrics/rdma", true, linkmonitor.RefreshResult{}, nil, 200, "rdma_marker", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			f := &fakeRefresher{result: tc.result, err: tc.err}
			handler, _ := onDemandHandler(t, f, tc.poll)
			r := httptest.NewRecorder()
			handler.ServeHTTP(r, httptest.NewRequestWithContext(t.Context(), "GET", tc.path, nil))
			if r.Code != tc.status || !strings.Contains(r.Body.String(), tc.marker) || r.Header().Get(refreshHeader) != tc.header {
				t.Fatalf("%d %q header %q", r.Code, r.Body.String(), r.Header().Get(refreshHeader))
			}
			got := f.calls()
			if len(got) != len(tc.want) {
				t.Fatalf("refreshes %+v, want %+v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("refresh %+v, want %+v", got[i], tc.want[i])
				}
			}
		})
	}
}

func TestHTTPOnDemandLimits(t *testing.T) {
	t.Log("boundary: 3 concurrent /metrics-all with a limit of 2; expected: the third gets 503 and never refreshes")
	f := &fakeRefresher{block: make(chan struct{})}
	handler, _ := onDemandHandler(t, f, false)
	var wg sync.WaitGroup
	for range allInFlight {
		wg.Go(func() {
			r := httptest.NewRecorder()
			handler.ServeHTTP(r, httptest.NewRequestWithContext(t.Context(), "GET", "/metrics-all", nil))
			if r.Code != 200 {
				t.Errorf("admitted request %d", r.Code)
			}
		})
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(f.calls()) != allInFlight && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequestWithContext(t.Context(), "GET", "/metrics-all", nil))
	close(f.block)
	wg.Wait()
	if r.Code != http.StatusServiceUnavailable || len(f.calls()) != allInFlight {
		t.Fatalf("third %d, refreshes %d", r.Code, len(f.calls()))
	}
}

func TestHTTPOnDemandMetrics(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		result                                       linkmonitor.RefreshResult
		label                                        string
	}{
		{"cached", "positive", "every job fresh", "cached counted", linkmonitor.RefreshResult{Cached: 4}, "cached"},
		{"collected", "positive", "one job collected", "collected counted", linkmonitor.RefreshResult{Cached: 3, Collected: 1}, "collected"},
		{"timeout", "negative", "the wait ended", "timeout counted, not collected", linkmonitor.RefreshResult{Collected: 1, TimedOut: true}, "timeout"},
		{"empty", "boundary", "nothing in scope", "cached counted", linkmonitor.RefreshResult{}, "cached"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			handler, registry := onDemandHandler(t, &fakeRefresher{result: tc.result}, false)
			handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), "GET", "/metrics/netstat", nil))
			families, err := registry.Gather()
			if err != nil || len(families) != 1 {
				t.Fatal(families, err)
			}
			for _, metric := range families[0].GetMetric() {
				want := 0.0
				if metric.GetLabel()[0].GetValue() == tc.label {
					want = 1
				}
				if metric.GetCounter().GetValue() != want {
					t.Fatalf("%s = %v, want %v", metric.GetLabel()[0].GetValue(), metric.GetCounter().GetValue(), want)
				}
			}
		})
	}
}

func TestHTTPOnDemandAuth(t *testing.T) {
	t.Log("corner: read auth on demand; expected: an unauthenticated detail scrape is rejected before any refresh")
	token := secretFile(t, "secret")
	opts, err := parseOptions([]string{"-auth.read-mode=raw", "-auth.read-token-file=" + token}, &strings.Builder{}, environment(nil))
	if err != nil {
		t.Fatal(err)
	}
	access, err := opts.admin.resolve()
	if err != nil {
		t.Fatal(err)
	}
	f := &fakeRefresher{}
	o := exposureOptions{allPath: defaultAllPath, categories: []string{"link"}, interfacePaths: true}
	served := endpoints(":9101", o, prometheus.NewRegistry(), prometheus.NewRegistry(), map[string]prometheus.Gatherer{"link": prometheus.NewRegistry()}, fakeViews(o.categories, "eth1"))
	if served, err = refreshRoutes(served, f, o.allPath, linkmonitorhttp.NewRefreshMetrics(prometheus.NewRegistry())); err != nil {
		t.Fatal(err)
	}
	var stop atomic.Bool
	handler := httpServer(&fakeMonitor{}, &stop, protectRoutes(served, access.policy)[0].routes...).Handler
	for _, path := range []string{"/metrics/link", "/metrics-all/eth1"} {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequestWithContext(t.Context(), "GET", path, nil))
		if r.Code != http.StatusUnauthorized {
			t.Fatalf("%s: %d", path, r.Code)
		}
	}
	if len(f.calls()) != 0 {
		t.Fatalf("unauthenticated scrapes refreshed %d times", len(f.calls()))
	}
}

func TestOnDemandOptions(t *testing.T) {
	defaults := linkmonitor.DefaultConfig()
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		args                                         []string
		env                                          map[string]string
		wantErr                                      string
		mode                                         linkmonitor.CollectionMode
		watch, slow, ttl, wait                       time.Duration
		statsIgnored                                 bool
	}{
		{"defaults", "positive", "no flags", "on demand, the default, with its default intervals", nil, nil, "", linkmonitor.CollectionOnDemand, defaults.WatchInterval, defaults.SlowInterval, defaults.OnDemandTTL, defaults.OnDemandWait, false},
		{"flags", "positive", "every on-demand flag", "exact values", []string{"-collection.mode=on-demand", "-watch.interval=30s", "-watch.slow-interval=10m", "-on-demand.ttl=0", "-on-demand.wait=9s"}, nil, "",
			linkmonitor.CollectionOnDemand, 30 * time.Second, 10 * time.Minute, 0, 9 * time.Second, false},
		{"environment", "positive", "every on-demand variable", "environment values", nil, map[string]string{modeEnvironment: "on-demand", watchEnvironment: "2m", slowEnvironment: "1h", ttlEnvironment: "1m", waitEnvironment: "1s"}, "",
			linkmonitor.CollectionOnDemand, 2 * time.Minute, time.Hour, time.Minute, time.Second, false},
		{"flag-wins", "corner", "flag and variable both set", "flag value", []string{"-watch.interval=90s"}, map[string]string{watchEnvironment: "2m"}, "", linkmonitor.CollectionOnDemand, 90 * time.Second, defaults.SlowInterval, defaults.OnDemandTTL, defaults.OnDemandWait, false},
		{"wait-at-timeout", "boundary", "on demand, wait equal to the 10 s metrics timeout", "rejected", []string{"-collection.mode=on-demand", "-on-demand.wait=10s"}, nil, "metrics timeout", "", 0, 0, 0, 0, false},
		{"poll", "positive", "-collection.mode=poll", "poll mode with the on-demand defaults kept", []string{"-collection.mode=poll"}, nil, "", linkmonitor.CollectionPoll, defaults.WatchInterval, defaults.SlowInterval, defaults.OnDemandTTL, defaults.OnDemandWait, false},
		{"wait-poll", "boundary", "poll mode, wait over the timeout", "accepted: unused in poll mode", []string{"-collection.mode=poll", "-on-demand.wait=1m"}, nil, "", linkmonitor.CollectionPoll, defaults.WatchInterval, defaults.SlowInterval, defaults.OnDemandTTL, time.Minute, false},
		{"bad-duration", "negative", "watch interval soon", "parse error naming the flag", []string{"-watch.interval=soon"}, nil, "watch.interval", "", 0, 0, 0, 0, false},
		{"stats-ignored", "negative", "explicit -stats-interval on demand", "noted so the command can warn", []string{"-collection.mode=on-demand", "-stats-interval=5s"}, nil, "", linkmonitor.CollectionOnDemand, defaults.WatchInterval, defaults.SlowInterval, defaults.OnDemandTTL, defaults.OnDemandWait, true},
		{"stats-only", "corner", "-stats-interval alone, no mode", "still on demand and noted: the interval does not select poll", []string{"-stats-interval=5s"}, nil, "", linkmonitor.CollectionOnDemand, defaults.WatchInterval, defaults.SlowInterval, defaults.OnDemandTTL, defaults.OnDemandWait, true},
		{"stats-poll", "boundary", "-stats-interval with -collection.mode=poll", "poll, not noted", []string{"-collection.mode=poll", "-stats-interval=5s"}, nil, "", linkmonitor.CollectionPoll, defaults.WatchInterval, defaults.SlowInterval, defaults.OnDemandTTL, defaults.OnDemandWait, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			opts, err := parseOptions(tc.args, &strings.Builder{}, environment(tc.env))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %v, want %q", err, tc.wantErr)
				}
				return
			}
			m := opts.monitor
			if err != nil || m.CollectionMode != tc.mode || m.WatchInterval != tc.watch || m.SlowInterval != tc.slow || m.OnDemandTTL != tc.ttl || m.OnDemandWait != tc.wait || opts.statsIgnored != tc.statsIgnored {
				t.Fatalf("%v: %+v ignored %v", err, m, opts.statsIgnored)
			}
			if _, err := linkmonitor.New(m, linkmonitor.Options{}); err != nil {
				t.Fatal(err)
			}
		})
	}
}
