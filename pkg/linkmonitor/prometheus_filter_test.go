package linkmonitor_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	client "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
)

func filteredRegistry(t testing.TB, m *linkmonitor.Monitor, opts ...monitorprom.Option) *client.Registry {
	t.Helper()
	r := client.NewPedanticRegistry()
	if err := r.Register(monitorprom.NewCollector(m, opts...)); err != nil {
		t.Fatal(err)
	}
	return r
}

func exposition(t testing.TB, r *client.Registry) string {
	t.Helper()
	w := httptest.NewRecorder()
	promhttp.HandlerFor(r, promhttp.HandlerOpts{}).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code != http.StatusOK {
		t.Fatal(w.Code, w.Body.String())
	}
	return w.Body.String()
}

func coreMatcher(t testing.TB) func(string) bool {
	t.Helper()
	match, err := monitorprom.NameMatcher(monitorprom.CoreMetricsPattern, "")
	if err != nil {
		t.Fatal(err)
	}
	return match
}

func deployedHost(t testing.TB) *linkmonitor.Monitor {
	f := linkmonitor.NewPrometheusFixture(t)
	f.PerformanceDeployedHost()
	f.Publish(3)
	return f.Monitor
}

func TestPrometheusFilterFamilies(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		build                                        func(testing.TB) *linkmonitor.Monitor
		include, exclude                             string
		mustHave, mustNotHave                        []string
	}{
		{"core deployed", "positive", "default core on the deployed-host shape", "audit families only, identical to the full values",
			deployedHost, monitorprom.CoreMetricsPattern, "",
			[]string{"go_link_monitor_up_links", "go_link_monitor_interface_up", "go_link_monitor_interface_check", "go_link_monitor_interface_speed_bits_per_second", "go_link_monitor_interface_duplex_info", "go_link_monitor_interface_receive_errors_total"},
			[]string{"go_link_monitor_ethtool_statistic", "go_link_monitor_netstat_TcpExt_Field0", "go_link_monitor_collector_support", "go_link_monitor_interface_receive_bytes_total"}},
		{"core fleet", "positive", "default core with RoCE ports and excluded bridges", "classification and bulk statistics stay out",
			func(t testing.TB) *linkmonitor.Monitor {
				f := linkmonitor.NewPrometheusFixture(t)
				f.PerformanceRealisticFleet()
				f.Publish(4)
				return f.Monitor
			}, monitorprom.CoreMetricsPattern, "",
			[]string{"go_link_monitor_interface_up", "go_link_monitor_interface_max_speed_bits_per_second"},
			[]string{"go_link_monitor_interface_classification", "go_link_monitor_ethtool_statistic"}},
		{"reject all", "negative", "include matches nothing", "zero families without a Gather error",
			deployedHost, "", "", nil, []string{"go_link_monitor_up_links"}},
		{"zero devices", "boundary", "empty inventory", "only policy families from core",
			func(t testing.TB) *linkmonitor.Monitor {
				f := linkmonitor.NewPrometheusFixture(t)
				f.Publish(0)
				return f.Monitor
			}, monitorprom.CoreMetricsPattern, "",
			[]string{"go_link_monitor_up_links", "go_link_monitor_baseline_up_links"}, []string{"go_link_monitor_interface_up"}},
		{"one device", "boundary", "single up interface", "per-interface core families present",
			func(t testing.TB) *linkmonitor.Monitor {
				f := linkmonitor.NewPrometheusFixture(t)
				f.Device(1, "eth0", true)
				f.Publish(1)
				return f.Monitor
			}, monitorprom.CoreMetricsPattern, "",
			[]string{"go_link_monitor_interface_up", "go_link_monitor_interface_check"}, nil},
		{"exclude wins", "corner", "include everything except driver statistics", "all but the excluded family",
			deployedHost, ".*", "go_link_monitor_ethtool_statistic",
			[]string{"go_link_monitor_netstat_TcpExt_Field0", "go_link_monitor_collector_support"}, []string{"go_link_monitor_ethtool_statistic"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			match, err := monitorprom.NameMatcher(tc.include, tc.exclude)
			if err != nil {
				t.Fatal(err)
			}
			m := tc.build(t)
			all := gathered(t, filteredRegistry(t, m))
			got := gathered(t, filteredRegistry(t, m, monitorprom.WithMetricFilter(match)))
			// Filtering is exact: the subset is precisely the matching full families,
			// with identical series and values.
			for name, family := range all {
				if !match(name) {
					continue
				}
				if got[name] == nil || got[name].String() != family.String() {
					t.Errorf("family %s differs from the full collector", name)
				}
			}
			for name := range got {
				if !match(name) || all[name] == nil {
					t.Errorf("unexpected family %s", name)
				}
			}
			for _, name := range tc.mustHave {
				if got[name] == nil {
					t.Errorf("missing %s", name)
				}
			}
			for _, name := range tc.mustNotHave {
				if got[name] != nil {
					t.Errorf("unexpected %s", name)
				}
			}
		})
	}
}

func TestPrometheusFilterPassThroughIsIdentical(t *testing.T) {
	dotStar, err := monitorprom.NameMatcher(".*", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		opts                                         []monitorprom.Option
	}{
		{"nil filter", "positive", "explicit nil match", "byte-identical exposition", []monitorprom.Option{monitorprom.WithMetricFilter(nil)}},
		{"accept all", "positive", "filter accepting every name", "byte-identical exposition", []monitorprom.Option{monitorprom.WithMetricFilter(func(string) bool { return true })}},
		{"dot star", "corner", "compiled .* matcher", "byte-identical exposition", []monitorprom.Option{monitorprom.WithMetricFilter(dotStar)}},
		{"no options", "boundary", "variadic call without options", "byte-identical exposition", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			m := deployedHost(t)
			want := exposition(t, filteredRegistry(t, m))
			if got := exposition(t, filteredRegistry(t, m, tc.opts...)); got != want {
				t.Fatalf("exposition differs: %d bytes vs %d bytes", len(got), len(want))
			}
		})
	}
}

type countingMatcher struct {
	mu    sync.Mutex
	calls map[string]int
	total atomic.Int64
	match func(string) bool
}

func newCountingMatcher(match func(string) bool) *countingMatcher {
	return &countingMatcher{calls: make(map[string]int), match: match}
}

func (c *countingMatcher) Match(name string) bool {
	c.mu.Lock()
	c.calls[name]++
	c.mu.Unlock()
	c.total.Add(1)
	return c.match(name)
}

func netstatSample(name string) model.Sample {
	return model.Sample{Descriptor: name, Kind: model.SampleUntyped, Number: model.Unsigned(1)}
}

func TestPrometheusFilterNeverMatchesOnScrape(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		schemas                                      [][]string // netstat descriptor set published before each gather round
		gathers                                      int
		wantNew                                      []int // new match calls caused by each schema
	}{
		{"steady", "positive", "unchanged schema scraped repeatedly", "no match calls after the first gather",
			[][]string{{"netstat_Tcp_ActiveOpens"}}, 5, []int{1}},
		{"new name", "positive", "schema gains a field", "exactly one call for the new name",
			[][]string{{"netstat_Tcp_ActiveOpens"}, {"netstat_Tcp_ActiveOpens", "netstat_Tcp_PassiveOpens"}}, 3, []int{1, 1}},
		{"remove readd", "corner", "field disappears then returns", "returning name is a cache hit",
			[][]string{{"netstat_Tcp_ActiveOpens"}, {"netstat_Tcp_PassiveOpens"}, {"netstat_Tcp_ActiveOpens"}}, 2, []int{1, 1, 0}},
		{"selected and rejected", "corner", "core-selected and rejected names", "both cached once",
			[][]string{{"netstat_Tcp_ActiveOpens", "go_link_monitor_never"}}, 4, []int{2}},
		{"single gather", "boundary", "one scrape per schema", "calls equal distinct new names",
			[][]string{{"netstat_A"}, {"netstat_B"}}, 1, []int{1, 1}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			counter := newCountingMatcher(coreMatcher(t))
			f := linkmonitor.NewPrometheusFixture(t)
			f.Device(1, "eth0", true)
			f.Publish(1)
			r := filteredRegistry(t, f.Monitor, monitorprom.WithMetricFilter(counter.Match))
			gathered(t, r)
			previous := counter.total.Load()
			for i, schema := range tc.schemas {
				samples := make([]model.Sample, 0, len(schema))
				for _, name := range schema {
					samples = append(samples, netstatSample(name))
				}
				if err := f.Samples(0, model.CollectorNetstat, samples...); err != nil {
					t.Fatal(err)
				}
				f.Publish(1)
				for range tc.gathers {
					gathered(t, r)
				}
				if delta := counter.total.Load() - previous; delta != int64(tc.wantNew[i]) {
					t.Fatalf("schema %d caused %d match calls, want %d", i, delta, tc.wantNew[i])
				}
				previous = counter.total.Load()
			}
			for name, calls := range counter.calls {
				if calls != 1 {
					t.Errorf("%s matched %d times, want once", name, calls)
				}
			}
		})
	}
}

func TestPrometheusFilterConcurrentScrapes(t *testing.T) {
	t.Log("corner: concurrent first scrapes race to build the catalog; expected: every name matched exactly once")
	counter := newCountingMatcher(coreMatcher(t))
	r := filteredRegistry(t, deployedHost(t), monitorprom.WithMetricFilter(counter.Match))
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 4 {
				if _, err := r.Gather(); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
	for name, calls := range counter.calls {
		if calls != 1 {
			t.Errorf("%s matched %d times, want once", name, calls)
		}
	}
}

func collectAllocs(t *testing.T, c client.Collector) float64 {
	t.Helper()
	out := make(chan client.Metric, 8192)
	collect := func() {
		c.Collect(out)
		for len(out) > 0 {
			<-out
		}
	}
	collect() // build the catalog outside the measurement
	return testing.AllocsPerRun(20, collect)
}

func TestPrometheusFilterAllocationGuards(t *testing.T) {
	m := deployedHost(t)
	unfiltered := collectAllocs(t, monitorprom.NewCollector(m))
	core := collectAllocs(t, monitorprom.NewCollector(m, monitorprom.WithMetricFilter(coreMatcher(t))))
	none := collectAllocs(t, monitorprom.NewCollector(m, monitorprom.WithMetricFilter(func(string) bool { return false })))
	acceptAll := collectAllocs(t, monitorprom.NewCollector(m, monitorprom.WithMetricFilter(func(string) bool { return true })))
	coreSeries := 0
	for _, family := range gathered(t, filteredRegistry(t, m, monitorprom.WithMetricFilter(coreMatcher(t)))) {
		coreSeries += len(family.Metric)
	}
	t.Logf("allocs/collect unfiltered=%.0f accept-all=%.0f core=%.0f none=%.0f; core series=%d", unfiltered, acceptAll, core, none, coreSeries)
	// Each emitted const metric costs about 15 allocations in client_golang
	// (label pairs, metric and value); 16 per series leaves headroom.
	const perSeries = 16
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		got, limit                                   float64
	}{
		{"core bounded by output", "positive", "core Collect on the deployed-host shape", "allocations scale with emitted core series, not the full snapshot", core, float64(perSeries*coreSeries + 64)},
		{"core far below full", "positive", "core against unfiltered", "at least ten times fewer allocations", core * 10, unfiltered},
		{"reject all", "boundary", "nothing selected", "only fixed per-scrape overhead", none, 64},
		{"filter is no tax", "corner", "accept-all filter against no filter", "selection adds no per-scrape allocation", acceptAll, unfiltered + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			if tc.got > tc.limit {
				t.Fatalf("got %.0f, limit %.0f", tc.got, tc.limit)
			}
		})
	}
}
