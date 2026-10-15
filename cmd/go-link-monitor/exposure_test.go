package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
)

func environment(values map[string]string) func(string) string {
	return func(name string) string { return values[name] }
}

func TestExposureOptions(t *testing.T) {
	all := monitorprom.Categories()
	// exposure fills the summary and category defaults the cases do not vary.
	exposure := func(include, exclude, allPath, allAddress string) exposureOptions {
		return exposureOptions{coreInclude: include, coreExclude: exclude, allPath: allPath, allAddress: allAddress, summary: summaryProblems, categories: all, interfacePaths: true}
	}
	defaults := exposure(defaultCoreInclude, "", defaultAllPath, "")
	with := func(o exposureOptions, mutate func(*exposureOptions)) exposureOptions { mutate(&o); return o }
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		args                                         []string
		env                                          map[string]string
		want                                         exposureOptions
	}{
		{"defaults", "positive", "no flags or environment", "core pattern, nothing excluded, shared /metrics-all", nil, nil, defaults},
		{"flags", "positive", "every exposure flag", "flag values used", []string{"-metrics.core-include=a|b", "-metrics.core-exclude=b", "-metrics-all-path=/full", "-metrics-all-addr=127.0.0.1:9102"}, nil,
			exposure("a|b", "b", "/full", "127.0.0.1:9102")},
		{"environment", "positive", "every exposure variable", "environment values used", nil, map[string]string{
			coreIncludeEnvironment: "a", coreExcludeEnvironment: "b", allPathEnvironment: "/env", allAddressEnvironment: ":9102"},
			exposure("a", "b", "/env", ":9102")},
		{"flag beats environment", "corner", "flag and variable both set", "flag wins per field", []string{"-metrics.core-include=flag", "-metrics-all-path=/flag"}, map[string]string{
			coreIncludeEnvironment: "env", coreExcludeEnvironment: "env", allPathEnvironment: "/env"},
			exposure("flag", "env", "/flag", "")},
		{"explicit empty flag", "corner", "empty flag with nonempty variable", "empty flag wins and disables /metrics-all", []string{"-metrics-all-path=", "-metrics.core-include="}, map[string]string{
			allPathEnvironment: "/env", coreIncludeEnvironment: "env"},
			exposure("", "", "", "")},
		{"empty environment", "boundary", "variables set to empty", "defaults kept", nil, map[string]string{
			coreIncludeEnvironment: "", allPathEnvironment: "", allAddressEnvironment: ""}, defaults},
		{"unrelated environment", "negative", "only other variables set", "defaults kept", nil, map[string]string{"GO_LINK_MONITOR_OTHER": "x"}, defaults},
		{"summary core", "positive", "-metrics.summary=core", "core summary", []string{"-metrics.summary=core"}, nil,
			with(defaults, func(o *exposureOptions) { o.summary = summaryCore })},
		{"summary environment", "positive", "summary variable", "core summary", nil, map[string]string{summaryEnvironment: "core"},
			with(defaults, func(o *exposureOptions) { o.summary = summaryCore })},
		{"categories subset", "positive", "-metrics.categories=link,rdma", "two categories in flag order", []string{"-metrics.categories=link,rdma"}, nil,
			with(defaults, func(o *exposureOptions) { o.categories = []string{"link", "rdma"} })},
		{"categories empty", "boundary", "explicit empty categories", "no categories", []string{"-metrics.categories="}, nil,
			with(defaults, func(o *exposureOptions) { o.categories = []string{} })},
		{"categories env vs flag", "corner", "variable sets categories, explicit empty flag", "flag wins: none", []string{"-metrics.categories="}, map[string]string{categoriesEnvironment: "netstat"},
			with(defaults, func(o *exposureOptions) { o.categories = []string{} })},
		{"categories environment", "positive", "variable sets categories", "netstat only", nil, map[string]string{categoriesEnvironment: "netstat"},
			with(defaults, func(o *exposureOptions) { o.categories = []string{"netstat"} })},
		{"interface paths off", "positive", "-metrics.interface-paths=false", "interface paths disabled", []string{"-metrics.interface-paths=false"}, nil,
			with(defaults, func(o *exposureOptions) { o.interfacePaths = false })},
		{"interface paths environment", "positive", "variable set to false", "interface paths disabled", nil, map[string]string{interfacePathsEnviron: "false"},
			with(defaults, func(o *exposureOptions) { o.interfacePaths = false })},
		{"interface paths flag beats environment", "corner", "bare flag with variable false", "flag wins: enabled", []string{"-metrics.interface-paths"}, map[string]string{interfacePathsEnviron: "false"}, defaults},
		{"interface paths empty environment", "boundary", "variable set to empty", "default enabled", nil, map[string]string{interfacePathsEnviron: ""}, defaults},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			var out bytes.Buffer
			opts, err := parseOptions(tc.args, &out, environment(tc.env))
			if err != nil || !reflect.DeepEqual(opts.exposure, tc.want) {
				t.Fatalf("exposure=%+v err=%v, want %+v", opts.exposure, err, tc.want)
			}
		})
	}
}

func TestExposureValidation(t *testing.T) {
	const address = ":9101"
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		exposure                                     exposureOptions
		wantErr                                      string
	}{
		{"defaults", "positive", "default exposure", "valid", exposureOptions{coreInclude: defaultCoreInclude, allPath: defaultAllPath}, ""},
		{"separate", "positive", "own listener", "valid", exposureOptions{coreInclude: ".*", allPath: "/metrics-all", allAddress: ":9102"}, ""},
		{"separate /metrics", "corner", "all-path /metrics on its own listener", "valid: no collision", exposureOptions{coreInclude: ".*", allPath: "/metrics", allAddress: ":9102"}, ""},
		{"disabled", "boundary", "empty all-path", "valid", exposureOptions{coreInclude: ".*"}, ""},
		{"empty include", "boundary", "empty core include", "valid: core serves nothing", exposureOptions{allPath: "/metrics-all"}, ""},
		{"nested path", "boundary", "multi-segment path", "valid", exposureOptions{coreInclude: ".*", allPath: "/a/b"}, ""},
		{"address without path", "negative", "all-addr with empty path", "error", exposureOptions{coreInclude: ".*", allAddress: ":9102"}, "requires a nonempty"},
		{"relative", "negative", "path without leading slash", "error", exposureOptions{coreInclude: ".*", allPath: "metrics-all"}, "exact absolute path"},
		{"trailing slash", "negative", "subtree pattern", "error", exposureOptions{coreInclude: ".*", allPath: "/metrics-all/"}, "exact absolute path"},
		{"root", "boundary", "path is /", "error: subtree of everything", exposureOptions{coreInclude: ".*", allPath: "/"}, "exact absolute path"},
		{"space", "corner", "space would become a method pattern", "error", exposureOptions{coreInclude: ".*", allPath: "/a b"}, "exact absolute path"},
		{"wildcard", "corner", "brace wildcard", "error", exposureOptions{coreInclude: ".*", allPath: "/{x}"}, "exact absolute path"},
		{"health", "negative", "collides with /healthz", "error", exposureOptions{coreInclude: ".*", allPath: "/healthz"}, "health endpoint"},
		{"ready", "negative", "collides with /readyz", "error", exposureOptions{coreInclude: ".*", allPath: "/readyz"}, "health endpoint"},
		{"shared /metrics", "negative", "all-path /metrics on the core listener", "error", exposureOptions{coreInclude: ".*", allPath: "/metrics"}, "core endpoint"},
		{"same address /metrics", "corner", "all-addr equal to metrics-addr", "treated as shared, so error", exposureOptions{coreInclude: ".*", allPath: "/metrics", allAddress: address}, "core endpoint"},
		{"bad include", "negative", "malformed include", "error", exposureOptions{coreInclude: "[", allPath: "/metrics-all"}, "include regexp"},
		{"bad exclude", "negative", "malformed exclude", "error", exposureOptions{coreInclude: ".*", coreExclude: "(", allPath: "/metrics-all"}, "exclude regexp"},
		{"summary invalid", "negative", "summary bogus", "error", exposureOptions{summary: "bogus", allPath: "/metrics-all"}, "metrics.summary"},
		{"summary core", "positive", "summary core", "valid", exposureOptions{summary: summaryCore, coreInclude: ".*", allPath: "/metrics-all"}, ""},
		{"categories all", "positive", "every category", "valid", exposureOptions{allPath: "/metrics-all", categories: monitorprom.Categories()}, ""},
		{"category unknown", "negative", "link,bogus", "error", exposureOptions{allPath: "/metrics-all", categories: []string{"link", "bogus"}}, "unknown metric category"},
		{"category duplicate", "negative", "link,link", "error", exposureOptions{allPath: "/metrics-all", categories: []string{"link", "link"}}, "duplicate metric category"},
		{"category summary", "negative", "summary is not a detail category", "error", exposureOptions{allPath: "/metrics-all", categories: []string{"summary"}}, "unknown metric category"},
		{"all-path collides", "negative", "all-path /metrics/link with link enabled", "error", exposureOptions{allPath: "/metrics/link", categories: []string{"link"}}, "category path"},
		{"all-path collides elsewhere", "corner", "all-path /metrics/link on its own listener", "error: categories share that listener", exposureOptions{allPath: "/metrics/link", allAddress: ":9102", categories: []string{"link"}}, "category path"},
		{"all-path disabled category", "corner", "all-path /metrics/link with link disabled", "valid", exposureOptions{allPath: "/metrics/link", categories: []string{"rdma"}}, ""},
		{"all-path under metrics", "corner", "all-path /metrics/everything", "valid", exposureOptions{allPath: "/metrics/everything", categories: monitorprom.Categories()}, ""},
		{"interface paths /metrics", "negative", "separate all-path /metrics with categories and interface paths", "error: /metrics/{interface} shadows categories",
			exposureOptions{allPath: "/metrics", allAddress: ":9102", categories: []string{"link"}, interfacePaths: true}, "category prefix"},
		{"interface paths /metrics no categories", "boundary", "separate all-path /metrics, interface paths, no categories", "valid",
			exposureOptions{allPath: "/metrics", allAddress: ":9102", categories: []string{}, interfacePaths: true}, ""},
		{"interface paths off /metrics", "corner", "separate all-path /metrics with categories, interface paths off", "valid",
			exposureOptions{allPath: "/metrics", allAddress: ":9102", categories: []string{"link"}}, ""},
		{"all-path under interface", "negative", "all-path /metrics/link/all with link interface paths", "error: shadowed by /metrics/link/{interface}",
			exposureOptions{allPath: "/metrics/link/all", categories: []string{"link"}, interfacePaths: true}, "interface paths"},
		{"all-path under interface off", "corner", "all-path /metrics/link/all, interface paths off", "valid",
			exposureOptions{allPath: "/metrics/link/all", categories: []string{"link"}}, ""},
		{"all-path under other category", "boundary", "all-path /metrics/linkx/all with link interface paths", "valid: prefix is per segment",
			exposureOptions{allPath: "/metrics/linkx/all", categories: []string{"link"}, interfacePaths: true}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			if tc.exposure.summary == "" {
				tc.exposure.summary = summaryProblems
			}
			match, err := tc.exposure.coreMatcher(address)
			if tc.wantErr == "" {
				if err != nil || match == nil {
					t.Fatalf("err=%v, want valid matcher", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err=%v, want containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestCommandExposureValidation(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		args                                         []string
		env                                          map[string]string
		reason                                       string
	}{
		{"flag regexp", "negative", "malformed core include flag", "exit 2 before listening", []string{"-metrics.core-include=["}, nil, "include regexp"},
		{"environment regexp", "negative", "malformed core exclude variable", "exit 2 before listening", nil, map[string]string{coreExcludeEnvironment: "("}, "exclude regexp"},
		{"address without path", "negative", "all-addr with disabled path", "exit 2 before listening", []string{"-metrics-all-path=", "-metrics-all-addr=127.0.0.1:0"}, nil, "requires a nonempty"},
		{"collision", "corner", "all-path /metrics on the shared listener", "exit 2 before listening", []string{"-metrics-all-path=/metrics"}, nil, "core endpoint"},
		{"summary invalid", "negative", "-metrics.summary=bogus", "exit 2 before listening", []string{"-metrics.summary=bogus"}, nil, "metrics.summary"},
		{"category unknown", "negative", "-metrics.categories=link,bogus", "exit 2 before listening", []string{"-metrics.categories=link,bogus"}, nil, "unknown metric category"},
		{"window below stats", "negative", "-flap-window=5s with 15s stats", "exit 2 before listening", []string{"-flap-window=5s"}, nil, "flap window"},
		{"window negative", "negative", "-errors.window=-1s", "exit 2 before listening", []string{"-errors.window=-1s"}, nil, "error window"},
		{"window malformed", "negative", "-errors.window=soon", "exit 2 before listening", []string{"-errors.window=soon"}, nil, "errors.window"},
		{"window environment", "negative", "flap window variable below stats", "exit 2 before listening", nil, map[string]string{flapWindowEnvironment: "1s"}, "flap window"},
		{"class unknown", "negative", "-errors.classes=link,bogus", "exit 2 before listening", []string{"-errors.classes=link,bogus"}, nil, "error class"},
		{"exclude malformed", "negative", "-errors.exclude=(", "exit 2 before listening", []string{"-errors.exclude=("}, nil, "error exclude"},
		{"interface paths environment", "negative", "interface paths variable not a boolean", "exit 2 before listening", nil, map[string]string{interfacePathsEnviron: "maybe"}, interfacePathsEnviron},
		{"interface paths flag", "negative", "-metrics.interface-paths=maybe", "exit 2 before listening", []string{"-metrics.interface-paths=maybe"}, nil, "metrics.interface-paths"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			// An unbindable address proves validation fails before any listener exists.
			args := append([]string{"-metrics-addr=127.0.0.1:99999"}, tc.args...)
			var out, diagnostic bytes.Buffer
			if code := command(args, &out, &diagnostic, environment(tc.env)); code != 2 || !strings.Contains(diagnostic.String(), tc.reason) {
				t.Fatal(code, diagnostic.String())
			}
		})
	}
}

// layout is the address and paths of one endpoint.
type layout struct {
	address string
	paths   []string
}

func TestEndpoints(t *testing.T) {
	const address = ":9101"
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		exposure                                     exposureOptions
		want                                         []layout
	}{
		{"shared", "positive", "default exposure", "one listener serving both paths",
			exposureOptions{allPath: defaultAllPath}, []layout{{address, []string{metricsPath, defaultAllPath}}}},
		{"separate", "positive", "own all-metrics address", "two listeners, one path each",
			exposureOptions{allPath: defaultAllPath, allAddress: ":9102"}, []layout{{address, []string{metricsPath}}, {":9102", []string{defaultAllPath}}}},
		{"disabled", "boundary", "empty all-path", "core listener only",
			exposureOptions{}, []layout{{address, []string{metricsPath}}}},
		{"same address", "corner", "all-addr equal to metrics-addr", "shared, not a second bind of the same address",
			exposureOptions{allPath: "/full", allAddress: address}, []layout{{address, []string{metricsPath, "/full"}}}},
		{"separate /metrics", "corner", "all-path /metrics on its own listener", "each listener has its own /metrics",
			exposureOptions{allPath: metricsPath, allAddress: ":9102"}, []layout{{address, []string{metricsPath}}, {":9102", []string{metricsPath}}}},
		{"categories shared", "positive", "default categories on one listener", "summary, every category in order, then all",
			exposureOptions{allPath: defaultAllPath, categories: monitorprom.Categories()}, []layout{{address, []string{metricsPath,
				"/metrics/link", "/metrics/traffic", "/metrics/ethtool", "/metrics/driver", "/metrics/rdma", "/metrics/netstat", "/metrics/collection", defaultAllPath}}}},
		{"categories separate", "positive", "categories with own all-metrics address", "alerting listener serves only /metrics",
			exposureOptions{allPath: defaultAllPath, allAddress: ":9102", categories: []string{"link", "rdma"}},
			[]layout{{address, []string{metricsPath}}, {":9102", []string{"/metrics/link", "/metrics/rdma", defaultAllPath}}}},
		{"categories without all", "boundary", "categories with empty all-path", "categories on the main listener",
			exposureOptions{categories: []string{"netstat"}}, []layout{{address, []string{metricsPath, "/metrics/netstat"}}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			core, all := prometheus.NewRegistry(), prometheus.NewRegistry()
			categories := make(map[string]prometheus.Gatherer)
			for _, category := range tc.exposure.categories {
				categories[category] = prometheus.NewRegistry()
			}
			served := endpoints(address, tc.exposure, core, all, categories, nil)
			got := make([]layout, 0, len(served))
			for _, e := range served {
				l := layout{address: e.address}
				for _, r := range e.routes {
					l.paths = append(l.paths, r.path)
					wantGatherer, wantLimit := prometheus.Gatherer(all), allInFlight
					if r.path == metricsPath && e.address == address {
						wantGatherer, wantLimit = core, coreInFlight
					}
					if category, ok := strings.CutPrefix(r.path, categoryPathPrefix); ok && categories[category] != nil {
						wantGatherer, wantLimit = categories[category], categoryInFlight
					}
					if r.gatherer != wantGatherer || r.inFlight != wantLimit {
						t.Errorf("%s%s: wrong gatherer or limit %d", e.address, r.path, r.inFlight)
					}
				}
				got = append(got, l)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("endpoints=%v, want %v", got, tc.want)
			}
		})
	}
}

// markedRegistry serves one gauge whose name identifies the registry.
func markedRegistry(name string) *prometheus.Registry {
	r := prometheus.NewRegistry()
	g := prometheus.NewGauge(prometheus.GaugeOpts{Name: name, Help: "Route marker."})
	g.Set(1)
	r.MustRegister(g)
	return r
}

func TestHTTPRoutes(t *testing.T) {
	const address = ":9101"
	type probe struct {
		endpoint int
		path     string
		status   int
		marker   string
	}
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		exposure                                     exposureOptions
		probes                                       []probe
	}{
		{"shared", "positive", "default exposure", "core on /metrics, all on /metrics-all, health beside them",
			exposureOptions{allPath: defaultAllPath}, []probe{
				{0, metricsPath, 200, "core_marker"}, {0, defaultAllPath, 200, "all_marker"}, {0, "/healthz", 200, ""}, {0, "/readyz", 200, ""}}},
		{"disabled", "negative", "empty all-path", "all-metrics path not found",
			exposureOptions{}, []probe{{0, metricsPath, 200, "core_marker"}, {0, defaultAllPath, 404, ""}}},
		{"separate", "positive", "own all-metrics listener", "each listener serves only its path plus health",
			exposureOptions{allPath: defaultAllPath, allAddress: ":9102"}, []probe{
				{0, metricsPath, 200, "core_marker"}, {0, defaultAllPath, 404, ""},
				{1, defaultAllPath, 200, "all_marker"}, {1, metricsPath, 404, ""}, {1, "/healthz", 200, ""}, {1, "/readyz", 200, ""}}},
		{"exact path", "boundary", "subpath of the all-metrics path", "not found",
			exposureOptions{allPath: defaultAllPath}, []probe{{0, defaultAllPath + "/x", 404, ""}, {0, metricsPath + "/x", 404, ""}}},
		{"category", "positive", "link category enabled", "link marker on /metrics/link; disabled category not found",
			exposureOptions{allPath: defaultAllPath, categories: []string{"link"}}, []probe{
				{0, "/metrics/link", 200, "link_marker"}, {0, "/metrics/rdma", 404, ""}, {0, "/metrics/link/", 404, ""}}},
		{"categories empty", "boundary", "no categories", "category paths not found",
			exposureOptions{allPath: defaultAllPath, categories: []string{}}, []probe{{0, "/metrics/link", 404, ""}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			m := &fakeMonitor{}
			m.ready.Store(true)
			var stop atomic.Bool
			categories := make(map[string]prometheus.Gatherer)
			for _, category := range tc.exposure.categories {
				categories[category] = markedRegistry(category + "_marker")
			}
			served := endpoints(address, tc.exposure, markedRegistry("core_marker"), markedRegistry("all_marker"), categories, nil)
			handlers := make([]http.Handler, 0, len(served))
			for _, e := range served {
				handlers = append(handlers, httpServer(m, &stop, e.routes...).Handler)
			}
			for _, p := range tc.probes {
				r := httptest.NewRecorder()
				handlers[p.endpoint].ServeHTTP(r, httptest.NewRequestWithContext(t.Context(), "GET", p.path, nil))
				body := r.Body.String()
				if r.Code != p.status || (p.marker != "" && !strings.Contains(body, p.marker+" 1")) {
					t.Errorf("endpoint %d %s: %d %q", p.endpoint, p.path, r.Code, body)
				}
				// Neither registry may leak onto the other's route.
				if p.marker == "core_marker" && strings.Contains(body, "all_marker") || p.marker == "all_marker" && strings.Contains(body, "core_marker") {
					t.Errorf("endpoint %d %s mixed registries: %q", p.endpoint, p.path, body)
				}
			}
		})
	}
}

// failingListener accepts nothing and reports err from Accept.
type failingListener struct {
	net.Listener
	err error
}

func (l failingListener) Accept() (net.Conn, error) { return nil, l.err }

func TestServeEndpoints(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		failBind, failServe, wantStarted             bool
	}{
		{"both", "positive", "two listeners, host cancels", "monitor runs and both listeners close", false, false, true},
		{"second bind", "negative", "second listener cannot bind", "first listener closed and monitor never starts", true, false, false},
		{"second dies", "negative", "second server fails after start", "process stops and first listener closes", false, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			var started atomic.Bool
			m := &fakeMonitor{run: func(runCtx context.Context) error {
				started.Store(true)
				if !tc.failServe {
					cancel()
				}
				<-runCtx.Done()
				return nil
			}}
			failure := errors.New("injected failure")
			var opened []net.Listener
			listen := func(ctx context.Context, network, address string) (net.Listener, error) {
				if len(opened) == 1 && tc.failBind {
					return nil, failure
				}
				var lc net.ListenConfig
				listener, err := lc.Listen(ctx, network, address)
				if err != nil {
					return nil, err
				}
				opened = append(opened, listener)
				if len(opened) == 2 && tc.failServe {
					return failingListener{listener, failure}, nil
				}
				return listener, nil
			}
			served := []endpoint{
				{address: "127.0.0.1:0", routes: []route{coreRoute(prometheus.NewRegistry())}},
				{address: "127.0.0.1:0", routes: []route{{path: defaultAllPath, gatherer: prometheus.NewRegistry(), inFlight: allInFlight}}},
			}
			err := serve(ctx, m, quietLogger(), nil, listen, served)
			wantFailure := tc.failBind || tc.failServe
			if (err != nil) != wantFailure || (wantFailure && !errors.Is(err, failure)) || started.Load() != tc.wantStarted {
				t.Fatal(err, started.Load())
			}
			for i, listener := range opened {
				if _, err := listener.Accept(); !errors.Is(err, net.ErrClosed) {
					t.Errorf("listener %d remains open: %v", i, err)
				}
			}
		})
	}
}

func TestServeDrainsEveryServer(t *testing.T) {
	t.Log("corner: slow scrape on each of two listeners during shutdown; expected: serve waits for both and both responses complete")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	entered, release := make(chan struct{}, 2), make(chan struct{})
	slow := gatherFunc(func() ([]*dto.MetricFamily, error) { entered <- struct{}{}; <-release; return nil, nil })
	m := &fakeMonitor{run: func(runCtx context.Context) error { <-runCtx.Done(); return nil }}
	addresses := make(chan string, 2)
	listen := func(ctx context.Context, network, address string) (net.Listener, error) {
		var lc net.ListenConfig
		listener, err := lc.Listen(ctx, network, address)
		if err == nil {
			addresses <- listener.Addr().String()
		}
		return listener, err
	}
	serveCtx, stop := context.WithCancel(ctx)
	defer stop()
	served := make(chan error, 1)
	go func() {
		served <- serve(serveCtx, m, quietLogger(), nil, listen, []endpoint{
			{address: "127.0.0.1:0", routes: []route{coreRoute(slow)}},
			{address: "127.0.0.1:0", routes: []route{{path: defaultAllPath, gatherer: slow, inFlight: allInFlight}}},
		})
	}()
	responses := make(chan error, 2)
	for _, path := range []string{metricsPath, defaultAllPath} {
		address := <-addresses
		go func() {
			_, err := fetchPath(ctx, address, path)
			responses <- err
		}()
	}
	<-entered
	<-entered
	stop()
	select {
	case err := <-served:
		t.Fatal("serve returned before scrapes drained", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	for range 2 {
		if err := <-responses; err != nil {
			t.Error(err)
		}
	}
	if err := <-served; err != nil {
		t.Fatal(err)
	}
}
