package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
)

// fakeViews serves a marker per path for the known names, so a probe can
// tell which view and which decoded name answered.
func fakeViews(categories []string, known ...string) *views {
	view := func(path string) interfaceView {
		return func(name string) (prometheus.Gatherer, error) {
			for _, k := range known {
				if name == k {
					return markedRegistry(path + "_iface_marker"), nil
				}
			}
			return nil, monitorprom.ErrUnknownInterface
		}
	}
	v := &views{All: view("all"), Categories: make(map[string]interfaceView)}
	for _, category := range categories {
		if category != monitorprom.CategoryNetstat {
			v.Categories[category] = view(category)
		}
	}
	return v
}

func TestHTTPInterfaceRoutes(t *testing.T) {
	const address = ":9101"
	long := strings.Repeat("a", maxInterfaceSegment)
	categories := []string{"link", "rdma", "netstat"}
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		path                                         string
		disabled                                     bool
		status                                       int
		marker                                       string
	}{
		{"link-one-interface", "positive", "/metrics/link/eth1", "link view", "/metrics/link/eth1", false, 200, "link_iface_marker"},
		{"all-one-interface", "positive", "/metrics-all/eth1", "all view", "/metrics-all/eth1", false, 200, "all_iface_marker"},
		{"native-ib", "positive", "/metrics/rdma/rdma:mlx5_0:1", "rdma view", "/metrics/rdma/rdma:mlx5_0:1", false, 200, "rdma_iface_marker"},
		{"trailing-slash", "boundary", "/metrics/link/eth1/", "same view as without the slash", "/metrics/link/eth1/", false, 200, "link_iface_marker"},
		{"all-trailing-slash", "boundary", "/metrics-all/eth1/", "all view", "/metrics-all/eth1/", false, 200, "all_iface_marker"},
		{"percent-encoded", "boundary", "rdma%3Amlx5_0%3A1", "decoded once to the native name", "/metrics/rdma/rdma%3Amlx5_0%3A1", false, 200, "rdma_iface_marker"},
		{"name-128", "boundary", "128-byte name", "served", "/metrics/link/" + long, false, 200, "link_iface_marker"},
		{"category-named-interface", "corner", "interface named link", "served as the interface", "/metrics/link/link", false, 200, "link_iface_marker"},
		{"parent-unchanged", "positive", "/metrics/link with interface paths", "category registry", "/metrics/link", false, 200, "link_marker"},
		{"unknown-interface", "negative", "/metrics/link/eth9", "404 unknown interface", "/metrics/link/eth9", false, 404, "unknown interface"},
		{"netstat-interface", "negative", "/metrics/netstat/eth1", "404: host scope has no interface form", "/metrics/netstat/eth1", false, 404, ""},
		{"disabled-category", "negative", "/metrics/traffic/eth1 with traffic disabled", "404", "/metrics/traffic/eth1", false, 404, ""},
		{"name-129", "negative", "129-byte name", "400", "/metrics/link/" + long + "a", false, 400, "invalid interface"},
		{"encoded-slash", "negative", "eth%2F1", "400", "/metrics/link/eth%2F1", false, 400, "invalid interface"},
		{"encoded-dot", "negative", "%2E", "400", "/metrics/link/%2E", false, 400, "invalid interface"},
		{"encoded-dotdot", "negative", "%2E%2E", "400", "/metrics-all/%2E%2E", false, 400, "invalid interface"},
		{"extra-segment", "negative", "/metrics/link/eth1/x", "404", "/metrics/link/eth1/x", false, 404, ""},
		{"empty-segment", "boundary", "/metrics/link/", "404: no subtree route", "/metrics/link/", false, 404, ""},
		{"disabled", "negative", "-metrics.interface-paths=false", "404", "/metrics/link/eth1", true, 404, ""},
		{"disabled-all", "negative", "-metrics.interface-paths=false", "404", "/metrics-all/eth1", true, 404, ""},
		{"disabled-parent", "positive", "-metrics.interface-paths=false", "parent path unchanged", "/metrics/link", true, 200, "link_marker"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			o := exposureOptions{allPath: defaultAllPath, categories: categories, interfacePaths: !tc.disabled}
			gatherers := make(map[string]prometheus.Gatherer)
			for _, category := range categories {
				gatherers[category] = markedRegistry(category + "_marker")
			}
			var v *views
			if o.interfacePaths {
				v = fakeViews(categories, "eth1", "link", "rdma:mlx5_0:1", long)
			}
			m := &fakeMonitor{}
			var stop atomic.Bool
			served := endpoints(address, o, markedRegistry("core_marker"), markedRegistry("all_marker"), gatherers, v)
			handler := httpServer(m, &stop, served[0].routes...).Handler
			r := httptest.NewRecorder()
			handler.ServeHTTP(r, httptest.NewRequestWithContext(t.Context(), "GET", tc.path, nil))
			if r.Code != tc.status || (tc.marker != "" && !strings.Contains(r.Body.String(), tc.marker)) {
				t.Fatalf("%s: %d %q", tc.path, r.Code, r.Body.String())
			}
			if tc.status == 200 && !strings.Contains(r.Header().Get("Content-Type"), "text/plain") {
				t.Fatalf("content type %q", r.Header().Get("Content-Type"))
			}
		})
	}
}

func TestHTTPInterfaceViewError(t *testing.T) {
	t.Log("negative: the view fails for another reason; expected: 500, not 404")
	o := exposureOptions{allPath: defaultAllPath, interfacePaths: true}
	v := &views{All: func(string) (prometheus.Gatherer, error) { return nil, errors.New("view failed") }}
	var stop atomic.Bool
	handler := httpServer(&fakeMonitor{}, &stop, endpoints(":9101", o, prometheus.NewRegistry(), prometheus.NewRegistry(), nil, v)[0].routes...).Handler
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequestWithContext(t.Context(), "GET", "/metrics-all/eth1", nil))
	if r.Code != http.StatusInternalServerError || !strings.Contains(r.Body.String(), "view failed") {
		t.Fatal(r.Code, r.Body.String())
	}
}

func TestHTTPInterfaceSeparateListener(t *testing.T) {
	t.Log("positive: -metrics-all-addr set; expected: interface routes move with the detail paths")
	o := exposureOptions{allPath: defaultAllPath, allAddress: ":9102", categories: []string{"link"}, interfacePaths: true}
	served := endpoints(":9101", o, markedRegistry("core_marker"), markedRegistry("all_marker"),
		map[string]prometheus.Gatherer{"link": markedRegistry("link_marker")}, fakeViews(o.categories, "eth1"))
	var stop atomic.Bool
	main := httpServer(&fakeMonitor{}, &stop, served[0].routes...).Handler
	detail := httpServer(&fakeMonitor{}, &stop, served[1].routes...).Handler
	for _, p := range []struct {
		handler http.Handler
		path    string
		status  int
	}{{main, "/metrics/link/eth1", 404}, {main, "/metrics-all/eth1", 404}, {detail, "/metrics/link/eth1", 200}, {detail, "/metrics-all/eth1", 200}} {
		r := httptest.NewRecorder()
		p.handler.ServeHTTP(r, httptest.NewRequestWithContext(t.Context(), "GET", p.path, nil))
		if r.Code != p.status {
			t.Errorf("%s: %d, want %d", p.path, r.Code, p.status)
		}
	}
}

func TestHTTPInterfaceLimits(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		hold, probe                                  string
		limit                                        int
	}{
		{"all-interface", "boundary", "four active /metrics-all/eth1 requests", "fifth rejected", "/metrics-all/eth1", "/metrics-all/eth1", allInterfaceInFlight},
		{"all-interface-separate", "corner", "two active /metrics-all scrapes", "interface request still admitted: own limit", "/metrics-all", "/metrics-all/eth1", allInFlight},
		{"category-shared", "corner", "four active /metrics/link scrapes", "/metrics/link/eth1 rejected: shares the category limit", "/metrics/link", "/metrics/link/eth1", categoryInFlight},
		{"category-interface", "boundary", "four active /metrics/link/eth1 requests", "/metrics/link rejected", "/metrics/link/eth1", "/metrics/link", categoryInFlight},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			entered, release := make(chan struct{}, tc.limit), make(chan struct{})
			blocking := gatherFunc(func() ([]*dto.MetricFamily, error) {
				entered <- struct{}{}
				<-release
				return nil, nil
			})
			// Only the held path blocks; the probe path answers at once.
			pick := func(path string) prometheus.Gatherer {
				if path == tc.hold {
					return blocking
				}
				return prometheus.NewRegistry()
			}
			view := func(path string) interfaceView {
				return func(name string) (prometheus.Gatherer, error) { return pick(path + "/" + name), nil }
			}
			o := exposureOptions{allPath: defaultAllPath, categories: []string{"link"}, interfacePaths: true}
			v := &views{All: view(defaultAllPath), Categories: map[string]interfaceView{"link": view("/metrics/link")}}
			var stop atomic.Bool
			served := endpoints(":9101", o, prometheus.NewRegistry(), pick(defaultAllPath), map[string]prometheus.Gatherer{"link": pick("/metrics/link")}, v)
			handler := httpServer(&fakeMonitor{}, &stop, served[0].routes...).Handler
			responses := make(chan int, tc.limit)
			for range tc.limit {
				go func() {
					r := httptest.NewRecorder()
					handler.ServeHTTP(r, httptest.NewRequestWithContext(t.Context(), "GET", tc.hold, nil))
					responses <- r.Code
				}()
			}
			for range tc.limit {
				<-entered
			}
			r := httptest.NewRecorder()
			handler.ServeHTTP(r, httptest.NewRequestWithContext(t.Context(), "GET", tc.probe, nil))
			rejected := r.Code == http.StatusServiceUnavailable && strings.Contains(r.Body.String(), "Limit of concurrent")
			if wantRejected := tc.name != "all-interface-separate"; rejected != wantRejected || (!rejected && r.Code != http.StatusOK) {
				t.Errorf("probe %s: %d %q, want rejected=%v", tc.probe, r.Code, r.Body.String(), wantRejected)
			}
			close(release)
			for range tc.limit {
				if code := <-responses; code != http.StatusOK {
					t.Errorf("held request: %d", code)
				}
			}
		})
	}
}

func TestHTTPInterfaceAuth(t *testing.T) {
	t.Log("positive: read auth enabled; expected: interface paths need read access like their parents, health stays open")
	token := secretFile(t, "secret")
	opts, err := parseOptions([]string{"-auth.read-mode=raw", "-auth.read-token-file=" + token}, &strings.Builder{}, environment(nil))
	if err != nil {
		t.Fatal(err)
	}
	access, err := opts.admin.resolve()
	if err != nil {
		t.Fatal(err)
	}
	o := exposureOptions{allPath: defaultAllPath, categories: []string{"link"}, interfacePaths: true}
	served := protectRoutes(endpoints(":9101", o, prometheus.NewRegistry(), prometheus.NewRegistry(),
		map[string]prometheus.Gatherer{"link": prometheus.NewRegistry()}, fakeViews(o.categories, "eth1")), access.policy)
	var stop atomic.Bool
	handler := httpServer(&fakeMonitor{}, &stop, served[0].routes...).Handler
	for _, p := range []struct {
		path, token string
		status      int
	}{
		{"/metrics/link/eth1", "", http.StatusUnauthorized}, {"/metrics-all/eth1/", "", http.StatusUnauthorized},
		{"/metrics/link/eth9", "", http.StatusUnauthorized}, {"/metrics/link/eth1", "secret", http.StatusOK},
		{"/metrics/link/eth9", "secret", http.StatusNotFound},
	} {
		req := httptest.NewRequestWithContext(t.Context(), "GET", p.path, nil)
		if p.token != "" {
			req.Header.Set("Authorization", "Bearer "+p.token)
		}
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, req)
		if r.Code != p.status {
			t.Errorf("%s token=%q: %d, want %d", p.path, p.token, r.Code, p.status)
		}
	}
}
