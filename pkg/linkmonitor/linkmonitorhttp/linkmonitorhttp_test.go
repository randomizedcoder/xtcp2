package linkmonitorhttp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
	"github.com/randomizedcoder/xtcp2/pkg/listenerauth"
)

// marked is a registry holding one gauge named name.
func marked(name string) *prometheus.Registry {
	r := prometheus.NewRegistry()
	g := prometheus.NewGauge(prometheus.GaugeOpts{Name: name, Help: "marker"})
	g.Set(1)
	r.MustRegister(g)
	return r
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRecorder()
	h.ServeHTTP(r, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
	return r
}

func TestValidInterfaceSegment(t *testing.T) {
	for _, tc := range []struct {
		name, category, description string
		segment                     string
		expected                    bool
	}{
		{"netdev", "positive", "a netdev name", "eth0", true},
		{"rdma", "positive", "a native RDMA identity", "mlx5_0/1", false},
		{"empty", "negative", "an empty segment", "", false},
		{"dot", "negative", "the current directory", ".", false},
		{"dotdot", "negative", "the parent directory", "..", false},
		{"slash", "negative", "a nested path", "a/b", false},
		{"max", "boundary", "exactly MaxInterfaceSegment bytes", strings.Repeat("a", MaxInterfaceSegment), true},
		{"over", "boundary", "one byte over MaxInterfaceSegment", strings.Repeat("a", MaxInterfaceSegment+1), false},
		{"dots", "corner", "three dots is a name, not a traversal", "...", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %t", tc.category, tc.description, tc.expected)
			if got := ValidInterfaceSegment(tc.segment); got != tc.expected {
				t.Fatalf("ValidInterfaceSegment(%q) = %t", tc.segment, got)
			}
		})
	}
}

func TestRouteRegister(t *testing.T) {
	view := func(name string) (prometheus.Gatherer, error) {
		switch name {
		case "eth0":
			return marked("eth0_marker"), nil
		case "broken0":
			return nil, errors.New("view failed")
		}
		return nil, monitorprom.ErrUnknownInterface
	}
	mux := http.NewServeMux()
	for _, r := range DetailRoutes("/metrics/", []string{"link"}, "/metrics-all", marked("all_marker"),
		map[string]prometheus.Gatherer{"link": marked("link_marker")}, &Views{All: view, Categories: map[string]InterfaceView{"link": view}}) {
		r.Register(mux)
	}
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		path                                         string
		status                                       int
		body                                         string
	}{
		{"category", "positive", "a category path serves its registry", "200 with the link marker", "/metrics/link", 200, "link_marker"},
		{"all", "positive", "the all path serves the full registry", "200 with the all marker", "/metrics-all", 200, "all_marker"},
		{"interface", "positive", "an interface of the all path", "200 with that interface only", "/metrics-all/eth0", 200, "eth0_marker"},
		{"category_interface", "positive", "an interface of a category path", "200 with that interface only", "/metrics/link/eth0", 200, "eth0_marker"},
		{"trailing_slash", "boundary", "an interface path with one trailing slash", "same as without", "/metrics-all/eth0/", 200, "eth0_marker"},
		{"unknown", "negative", "an interface the snapshot does not have", "404", "/metrics-all/nosuch0", 404, "unknown interface"},
		{"view_error", "negative", "a view that fails", "500 with the error", "/metrics-all/broken0", 500, "view failed"},
		{"unserved_category", "negative", "a category that was not configured", "404", "/metrics/rdma", 404, ""},
		{"escaped_slash", "corner", "an escaped slash decodes into the segment", "400 invalid interface", "/metrics-all/a%2Fb", 400, "invalid interface"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			r := get(t, mux, tc.path)
			if r.Code != tc.status || !strings.Contains(r.Body.String(), tc.body) {
				t.Fatalf("%s: %d %q", tc.path, r.Code, r.Body.String())
			}
		})
	}
}

func TestDetailRoutes(t *testing.T) {
	all := marked("all")
	view := func(string) (prometheus.Gatherer, error) { return nil, monitorprom.ErrUnknownInterface }
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		categories                                   []string
		allPath                                      string
		views                                        *Views
		paths                                        []string
		limits                                       []int
		withViews                                    []bool
	}{
		{"all_and_categories", "positive", "categories in order, then the all path", "three routes with their limits",
			[]string{"traffic", "link"}, "/metrics-all", nil, []string{"/metrics/traffic", "/metrics/link", "/metrics-all"}, []int{CategoryInFlight, CategoryInFlight, AllInFlight}, []bool{false, false, false}},
		{"views", "positive", "views attach to the categories that have one and the all path", "netstat-like category without a view",
			[]string{"link", "netstat"}, "/metrics-all", &Views{All: view, Categories: map[string]InterfaceView{"link": view}}, []string{"/metrics/link", "/metrics/netstat", "/metrics-all"}, []int{CategoryInFlight, CategoryInFlight, AllInFlight}, []bool{true, false, true}},
		{"no_all", "negative", "an empty all path", "no all route",
			[]string{"link"}, "", nil, []string{"/metrics/link"}, []int{CategoryInFlight}, []bool{false}},
		{"nothing", "boundary", "no categories and no all path", "no routes", nil, "", nil, nil, nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			routes := DetailRoutes("/metrics/", tc.categories, tc.allPath, all, map[string]prometheus.Gatherer{}, tc.views)
			if len(routes) != len(tc.paths) {
				t.Fatalf("%d routes, want %d", len(routes), len(tc.paths))
			}
			for i, r := range routes {
				if r.Path != tc.paths[i] || r.InFlight != tc.limits[i] || (r.View != nil) != tc.withViews[i] {
					t.Errorf("route %d: %s limit %d view %t", i, r.Path, r.InFlight, r.View != nil)
				}
			}
			if tc.allPath != "" && tc.views != nil && routes[len(routes)-1].ViewInFlight != AllInterfaceInFlight {
				t.Errorf("all path interface limit %d", routes[len(routes)-1].ViewInFlight)
			}
		})
	}
}

func TestLimited(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		capacity, concurrent                         int
		rejected                                     int
	}{
		{"under", "positive", "fewer requests than slots", "all served", 2, 1, 0},
		{"at", "boundary", "exactly as many requests as slots", "all served", 2, 2, 0},
		{"over", "boundary", "one request more than slots", "one 503", 2, 3, 1},
		{"one_slot", "corner", "a single slot under four requests", "three 503", 1, 4, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			release, entered := make(chan struct{}), make(chan struct{}, tc.concurrent)
			var blocking atomic.Bool
			blocking.Store(true)
			h := Limited(make(chan struct{}, tc.capacity), http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
				if !blocking.Load() {
					return
				}
				entered <- struct{}{}
				<-release
			}))
			codes := make(chan int, tc.concurrent)
			var wg sync.WaitGroup
			for range tc.concurrent {
				wg.Go(func() { codes <- get(t, h, "/").Code })
			}
			for range tc.concurrent - tc.rejected {
				<-entered
			}
			// Every rejection answers without entering the handler.
			for range tc.rejected {
				if code := <-codes; code != http.StatusServiceUnavailable {
					t.Errorf("rejected request: %d", code)
				}
			}
			blocking.Store(false)
			close(release)
			wg.Wait()
			close(codes)
			for code := range codes {
				if code != http.StatusOK {
					t.Errorf("admitted request: %d", code)
				}
			}
			// Every slot is free again.
			for range tc.capacity {
				if code := get(t, h, "/").Code; code != http.StatusOK {
					t.Errorf("after release: %d", code)
				}
			}
		})
	}
}

func TestRegistryIdentity(t *testing.T) {
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{Name: "test_build_info", Help: "identity"})
	identity := Identity{Name: "test_build_info", Collector: gauge}
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		match                                        func(string) bool
		id                                           Identity
		expected                                     bool
	}{
		{"kept", "positive", "a nil match keeps the identity", "identity served", nil, identity, true},
		{"matched", "positive", "a match accepting the name", "identity served", func(string) bool { return true }, identity, true},
		{"rejected", "negative", "a match rejecting the name", "identity omitted", func(string) bool { return false }, identity, false},
		{"none", "corner", "the zero identity", "nothing beside the collector", nil, Identity{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			families, err := Registry(prometheus.NewGauge(prometheus.GaugeOpts{Name: "collector", Help: "c"}), tc.match, tc.id).Gather()
			if err != nil {
				t.Fatal(err)
			}
			got := false
			for _, f := range families {
				got = got || f.GetName() == "test_build_info"
			}
			if got != tc.expected {
				t.Fatalf("identity served %t", got)
			}
		})
	}
}

func TestTeeRegisterer(t *testing.T) {
	collector := func() prometheus.Collector {
		return prometheus.NewGauge(prometheus.GaugeOpts{Name: "tee_gauge", Help: "tee"})
	}
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		taken                                        int // registries already holding tee_gauge
		registries                                   int
		fails                                        bool
	}{
		{"every", "positive", "registers on every registry", "each registry holds it", -1, 3, false},
		{"empty", "boundary", "no registries", "a no-op", -1, 0, false},
		{"last_taken", "negative", "the last registry already holds the name", "error and no registry keeps it", 2, 3, true},
		{"first_taken", "corner", "the first registry already holds the name", "error before any registration", 0, 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			tee := make(TeeRegisterer, 0, tc.registries)
			registries := make([]*prometheus.Registry, tc.registries)
			for i := range registries {
				registries[i] = prometheus.NewRegistry()
				if i == tc.taken {
					registries[i].MustRegister(collector())
				}
				tee = append(tee, registries[i])
			}
			c := collector()
			if err := tee.Register(c); (err != nil) != tc.fails {
				t.Fatalf("Register: %v", err)
			}
			// Unregister matches by descriptor, so the registry that already
			// held the name is skipped.
			for i, r := range registries {
				if i == tc.taken {
					continue
				}
				if held := r.Unregister(c); held == tc.fails {
					t.Errorf("registry %d holds the collector: %t", i, held)
				}
			}
		})
	}
}

// fakeRefresher answers every Refresh with result and err.
type fakeRefresher struct {
	mu     sync.Mutex
	scopes []linkmonitor.RefreshScope
	result linkmonitor.RefreshResult
	err    error
}

func (f *fakeRefresher) Refresh(_ context.Context, scope linkmonitor.RefreshScope) (linkmonitor.RefreshResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.scopes = append(f.scopes, scope)
	return f.result, f.err
}

func TestRefreshRoutes(t *testing.T) {
	link, err := linkmonitor.Collectors(monitorprom.CategoryCollectors(monitorprom.CategoryLink)...)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		path                                         string
		mount                                        bool
		expected                                     linkmonitor.CollectorSet
	}{
		{"category", "positive", "a category path", "its category's collectors", "/metrics/link", false, link},
		{"all", "positive", "the all path", "every collector", "/metrics-all", false, linkmonitor.AllCollectors()},
		{"core", "negative", "the core path", "no refresh", "/metrics", false, 0},
		{"mount", "corner", "a mounted family under the category prefix", "no refresh", "/metrics/x", true, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			r := []Route{{Path: tc.path}}
			if tc.mount {
				r[0].Mount = func(*http.ServeMux, func(http.Handler) http.Handler) {}
			}
			f := &fakeRefresher{}
			if err := RefreshRoutes(r, f, "/metrics/", "/metrics-all", NewRefreshMetrics(prometheus.NewRegistry())); err != nil {
				t.Fatal(err)
			}
			if (r[0].Refresh != nil) != (tc.expected != 0) {
				t.Fatalf("refresh set %t", r[0].Refresh != nil)
			}
			if tc.expected == 0 {
				return
			}
			get(t, r[0].Refresh(http.NotFoundHandler(), false), tc.path)
			if len(f.scopes) != 1 || f.scopes[0].Collectors != tc.expected {
				t.Fatalf("scopes %+v", f.scopes)
			}
		})
	}
	t.Run("unknown_category", func(t *testing.T) {
		t.Logf("negative: a category without collectors; expected: no refresh, no error")
		r := []Route{{Path: "/metrics/nosuch"}}
		if err := RefreshRoutes(r, &fakeRefresher{}, "/metrics/", "", NewRefreshMetrics(prometheus.NewRegistry())); err != nil || r[0].Refresh != nil {
			t.Fatalf("err %v refresh %t", err, r[0].Refresh != nil)
		}
	})
}

func TestRefreshing(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		result                                       linkmonitor.RefreshResult
		err                                          error
		view                                         bool
		path                                         string
		status                                       int
		header                                       string
		outcome                                      string
	}{
		{"collected", "positive", "a refresh that collected", "served, collected counted", linkmonitor.RefreshResult{Collected: 1}, nil, false, "/m", 200, "", "collected"},
		{"cached", "positive", "a refresh with every job fresh", "served, cached counted", linkmonitor.RefreshResult{Cached: 2}, nil, false, "/m", 200, "", "cached"},
		{"timeout", "boundary", "the wait ended first", "served with the refresh header", linkmonitor.RefreshResult{TimedOut: true}, nil, false, "/m", 200, "timeout", "timeout"},
		{"unknown", "negative", "an interface the monitor does not have", "404", linkmonitor.RefreshResult{}, linkmonitor.ErrUnknownInterface, true, "/m/nosuch0", 404, "", ""},
		{"stopped", "negative", "a monitor that is not running", "503", linkmonitor.RefreshResult{}, errors.New("monitor stopped"), false, "/m", 503, "", ""},
		{"invalid_view", "corner", "an over-long interface segment skips the refresh", "the view rejects it", linkmonitor.RefreshResult{}, errors.New("unreached"), true, "/m/" + strings.Repeat("a", MaxInterfaceSegment+1), 200, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			registry := prometheus.NewRegistry()
			f := &fakeRefresher{result: tc.result, err: tc.err}
			mux := http.NewServeMux()
			h := Refreshing(f, linkmonitor.AllCollectors(), tc.view, NewRefreshMetrics(registry), http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
			mux.Handle("/m", h)
			mux.Handle("/m/{interface}", h)
			r := get(t, mux, tc.path)
			if r.Code != tc.status || r.Header().Get(RefreshHeader) != tc.header {
				t.Fatalf("%d header %q", r.Code, r.Header().Get(RefreshHeader))
			}
			if tc.outcome != "" && !gathered(t, registry, `result="`+tc.outcome+`"`) {
				t.Fatalf("outcome %s not counted", tc.outcome)
			}
		})
	}
}

// gathered reports whether a counted sample in r carries label.
func gathered(t *testing.T, r *prometheus.Registry, label string) bool {
	t.Helper()
	families, err := r.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range families {
		for _, m := range f.GetMetric() {
			for _, l := range m.GetLabel() {
				if `result="`+l.GetValue()+`"` == label && m.GetCounter().GetValue() == 1 {
					return true
				}
			}
		}
	}
	return false
}

func TestProtect(t *testing.T) {
	enforced, err := listenerauth.NewPolicy(&xtcp_config.ListenerAuth{Mode: xtcp_config.ListenerAuthMode_LISTENER_AUTH_MODE_RAW_TOKEN, RawToken: "read-token"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	open, err := listenerauth.NewPolicy(nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		policy                                       *listenerauth.Policy
		token                                        string
		status                                       int
	}{
		{"token", "positive", "the read token on an enforced policy", "200", enforced, "read-token", 200},
		{"no_token", "negative", "no token on an enforced policy", "401", enforced, "", 401},
		{"open", "corner", "an unenforced policy", "routes unchanged, 200", open, "", 200},
		{"nil", "corner", "a nil policy", "routes unchanged, 200", nil, "", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			routes := []Route{{Path: "/metrics/link", Gatherer: marked("link_marker"), InFlight: 1}}
			Protect(routes, tc.policy)
			mux := http.NewServeMux()
			routes[0].Register(mux)
			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/metrics/link", nil)
			if tc.token != "" {
				req.Header.Set("Authorization", "Bearer "+tc.token)
			}
			r := httptest.NewRecorder()
			mux.ServeHTTP(r, req)
			if r.Code != tc.status {
				t.Fatalf("%d %q", r.Code, r.Body.String())
			}
		})
	}
}
