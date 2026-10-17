package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/internal/goip"
	"github.com/randomizedcoder/xtcp2/internal/goip/goiphttp"
	"github.com/randomizedcoder/xtcp2/pkg/listenerauth"
)

const ipTestPcap = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getaddr.pcap"

// countingOpener replays ipTestPcap and counts opens, so a test can prove
// which requests reached netlink. hold, when non-nil, blocks each open on
// entered/release.
type countingOpener struct {
	opens            atomic.Int32
	entered, release chan struct{}
}

func (c *countingOpener) open() (goip.Source, func() error, error) {
	c.opens.Add(1)
	if c.entered != nil {
		c.entered <- struct{}{}
		<-c.release
	}
	src, err := goip.OpenReplay(ipTestPcap)
	if err != nil {
		return nil, func() error { return nil }, err
	}
	return src, func() error { return nil }, nil
}

// ipServer is the main listener's handler with categories, interface paths
// and, when enabled, the /ip paths on c, protected by policy.
func ipServer(t testing.TB, enabled bool, c *countingOpener, inFlight int, policy *listenerauth.Policy) http.Handler {
	t.Helper()
	o := exposureOptions{allPath: defaultAllPath, categories: []string{"link"}, interfacePaths: true}
	served := endpoints(":9101", o, prometheus.NewRegistry(), prometheus.NewRegistry(),
		map[string]prometheus.Gatherer{"link": prometheus.NewRegistry()}, fakeViews(o.categories, "eth1"))
	if enabled {
		var err error
		if served, err = withIPPaths(served, goiphttp.Options{Open: c.open, InFlight: inFlight}); err != nil {
			t.Fatal(err)
		}
	}
	served = protectRoutes(served, policy)
	var stop atomic.Bool
	return httpServer(&fakeMonitor{}, &stop, served[0].routes...).Handler
}

func get(t testing.TB, h http.Handler, path, token string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	r := httptest.NewRecorder()
	h.ServeHTTP(r, req)
	return r
}

func TestIPPathsRoutes(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		enabled                                      bool
		path                                         string
		status                                       int
		opens                                        int32
	}{
		{"enabled addr", "positive", "/ip/addr with ip paths on", "200 from one netlink open", true, "/ip/addr", http.StatusOK, 1},
		{"enabled index", "positive", "/ip with ip paths on", "200, no netlink", true, "/ip", http.StatusOK, 0},
		{"enabled interface", "positive", "/ip/addr/goip0 with ip paths on", "200", true, "/ip/addr/goip0?format=text", http.StatusOK, 1},
		{"default off addr", "negative", "/ip/addr with ip paths off", "404", false, "/ip/addr", http.StatusNotFound, 0},
		{"default off index", "negative", "/ip with ip paths off", "404", false, "/ip", http.StatusNotFound, 0},
		{"bad param", "boundary", "/ip/addr?foo=1", "400 before netlink", true, "/ip/addr?foo=1", http.StatusBadRequest, 0},
		{"metrics beside ip", "corner", "/metrics with ip paths on", "200, no netlink", true, metricsPath, http.StatusOK, 0},
		{"interface beside ip", "corner", "/metrics/link/eth1 with ip paths on", "200, no netlink", true, "/metrics/link/eth1", http.StatusOK, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			var c countingOpener
			r := get(t, ipServer(t, tc.enabled, &c, 0, nil), tc.path, "")
			if r.Code != tc.status || c.opens.Load() != tc.opens {
				t.Fatalf("%s: %d opens=%d %q, want %d opens=%d", tc.path, r.Code, c.opens.Load(), r.Body.String(), tc.status, tc.opens)
			}
		})
	}
}

func TestIPPathsAuth(t *testing.T) {
	token := secretFile(t, "secret")
	opts, err := parseOptions([]string{"-auth.read-mode=raw", "-auth.read-token-file=" + token}, &strings.Builder{}, environment(nil))
	if err != nil {
		t.Fatal(err)
	}
	access, err := opts.admin.resolve()
	if err != nil {
		t.Fatal(err)
	}
	var c countingOpener
	h := ipServer(t, true, &c, 0, access.policy)
	for _, tc := range []struct {
		name, category, description string
		path, token                 string
		expected                    int
	}{
		{"addr token", "positive", "/ip/addr with the read token", "/ip/addr", "secret", http.StatusOK},
		{"health", "positive", "liveness stays open", "/healthz", "", http.StatusOK},
		{"addr missing", "negative", "/ip/addr without a token", "/ip/addr", "", http.StatusUnauthorized},
		{"addr wrong", "negative", "/ip/addr with a wrong token", "/ip/addr", "nope", http.StatusUnauthorized},
		{"index missing", "boundary", "the index needs read access too", "/ip", "", http.StatusUnauthorized},
		{"interface missing", "corner", "/ip/link/eth0 without a token", "/ip/link/eth0", "", http.StatusUnauthorized},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %d", tc.category, tc.description, tc.expected)
			if r := get(t, h, tc.path, tc.token); r.Code != tc.expected {
				t.Fatalf("%s: %d %q, want %d", tc.path, r.Code, r.Body.String(), tc.expected)
			}
		})
	}
	if n := c.opens.Load(); n != 1 {
		t.Errorf("opens = %d, want 1: only the authorized request reaches netlink", n)
	}
}

// TestIPPathsSeparateSlots holds every /ip slot and probes the rest of the
// listener: the /ip limit must not reach the metrics routes, nor theirs /ip.
func TestIPPathsSeparateSlots(t *testing.T) {
	c := countingOpener{entered: make(chan struct{}), release: make(chan struct{})}
	h := ipServer(t, true, &c, 1, nil)
	done := make(chan int)
	go func() { done <- get(t, h, "/ip/link", "").Code }()
	<-c.entered
	for _, tc := range []struct {
		name, category, description string
		path                        string
		expected                    int
	}{
		{"metrics", "positive", "/metrics while /ip is full", metricsPath, http.StatusOK},
		{"all", "positive", "/metrics-all while /ip is full", defaultAllPath, http.StatusOK},
		{"index", "corner", "the index takes no slot", "/ip", http.StatusOK},
		{"ip full", "boundary", "a second /ip request at InFlight=1", "/ip/addr", http.StatusServiceUnavailable},
		{"ip other object", "negative", "the slot is shared by every object", "/ip/route", http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %d", tc.category, tc.description, tc.expected)
			if r := get(t, h, tc.path, ""); r.Code != tc.expected {
				t.Fatalf("%s: %d %q, want %d", tc.path, r.Code, r.Body.String(), tc.expected)
			}
		})
	}
	close(c.release)
	if code := <-done; code != http.StatusOK {
		t.Errorf("held request: %d", code)
	}
}

// TestIPPathsNoNetlinkOnMetrics guards the scrape path: with /ip mounted,
// no metrics path ever opens a netlink socket for it.
func TestIPPathsNoNetlinkOnMetrics(t *testing.T) {
	var c countingOpener
	h := ipServer(t, true, &c, 0, nil)
	paths := []string{metricsPath, defaultAllPath, "/metrics/link", "/metrics/link/eth1", "/metrics-all/eth1", "/healthz"}
	for range 100 {
		for _, p := range paths {
			if r := get(t, h, p, ""); r.Code != http.StatusOK {
				t.Fatalf("%s: %d", p, r.Code)
			}
		}
	}
	if n := c.opens.Load(); n != 0 {
		t.Fatalf("netlink opens across metrics scrapes = %d, want 0", n)
	}
}

// BenchmarkMetricsWithIPPaths is the scrape path with /ip unmounted and
// mounted; the two must match, since mounting adds routes and nothing to a
// metrics request.
//
// go test ./cmd/go-link-monitor/ -run x -bench MetricsWithIPPaths -count=10
func BenchmarkMetricsWithIPPaths(b *testing.B) {
	for _, bc := range []struct {
		name    string
		enabled bool
	}{{"off", false}, {"on", true}} {
		b.Run(bc.name, func(b *testing.B) {
			var c countingOpener
			h := ipServer(b, bc.enabled, &c, 0, nil)
			req := httptest.NewRequestWithContext(b.Context(), http.MethodGet, metricsPath, nil)
			b.ReportAllocs()
			for b.Loop() {
				r := httptest.NewRecorder()
				h.ServeHTTP(r, req)
				if r.Code != http.StatusOK {
					b.Fatal(r.Code)
				}
			}
			if c.opens.Load() != 0 {
				b.Fatal("metrics scrape opened netlink")
			}
		})
	}
}
