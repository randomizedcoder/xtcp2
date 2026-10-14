package linkmonitor_test

import (
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	client "github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
)

func summaryRegistry(t testing.TB, m *linkmonitor.Monitor, opts ...monitorprom.SummaryOption) *client.Registry {
	t.Helper()
	r := client.NewPedanticRegistry()
	if err := r.Register(monitorprom.NewSummaryCollector(m, opts...)); err != nil {
		t.Fatal(err)
	}
	return r
}

// series renders gathered metrics as sorted "name{k=v,...} value" lines.
func series(t testing.TB, r client.Gatherer) []string {
	t.Helper()
	families, err := r.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, family := range families {
		for _, m := range family.Metric {
			lines = append(lines, seriesLine(family.GetName(), m))
		}
	}
	sort.Strings(lines)
	return lines
}

func seriesLine(name string, m *dto.Metric) string {
	labels := make([]string, 0, len(m.Label))
	for _, pair := range m.Label {
		labels = append(labels, pair.GetName()+"="+pair.GetValue())
	}
	return fmt.Sprintf("%s{%s} %g", name, strings.Join(labels, ","), metricValue(m))
}

const (
	problemsName  = "go_link_monitor_problems"
	interfaceName = "go_link_monitor_interface_problem"
	hostName      = "go_link_monitor_host_problem"
)

func healthyFixture(t testing.TB, ports int) *linkmonitor.PrometheusFixture {
	f := linkmonitor.NewPrometheusFixture(t)
	for i := 1; i <= ports; i++ {
		f.Device(uint32(i), fmt.Sprintf("eth%d", i), true)
		f.Speed(uint32(i), 10000, 10000)
	}
	f.Publish(uint64(ports))
	return f
}

func fixedClock(at time.Time) monitorprom.SummaryOption {
	return monitorprom.WithClock(func() time.Time { return at })
}

func TestSummaryScalars(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		build                                        func(testing.TB) *linkmonitor.Monitor
		opts                                         []monitorprom.SummaryOption
		want                                         []string
	}{
		{"healthy-host", "positive", "three healthy ports, baseline ready", "four scalars, no sparse series", func(t testing.TB) *linkmonitor.Monitor { return healthyFixture(t, 3).Monitor }, nil,
			[]string{"go_link_monitor_baseline_ready{} 1", "go_link_monitor_collection_healthy{} 0", "go_link_monitor_problems{} 0", "go_link_monitor_up_links_delta{} 0"}},
		{"before-publication", "boundary", "monitor without a snapshot", "no delta; problems zero", func(testing.TB) *linkmonitor.Monitor { return new(linkmonitor.Monitor) }, nil,
			[]string{"go_link_monitor_baseline_ready{} 0", "go_link_monitor_collection_healthy{} 0", "go_link_monitor_problems{} 0"}},
		{"without-scalars", "positive", "WithoutScalars for the all registry", "problems only", func(t testing.TB) *linkmonitor.Monitor { return healthyFixture(t, 3).Monitor },
			[]monitorprom.SummaryOption{monitorprom.WithoutScalars()}, []string{"go_link_monitor_problems{} 0"}},
		{"delta-negative", "corner", "one of three baseline links down", "delta -1 and the flap of eth3", func(t testing.TB) *linkmonitor.Monitor {
			f := healthyFixture(t, 3)
			f.Device(3, "eth3", false)
			f.Publish(3)
			return f.Monitor
		}, nil, []string{"go_link_monitor_baseline_ready{} 1", "go_link_monitor_collection_healthy{} 0",
			"go_link_monitor_interface_problem{interface=eth3,reason=flap} 1", "go_link_monitor_problems{} 1", "go_link_monitor_up_links_delta{} -1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			opts := append([]monitorprom.SummaryOption{fixedClock(time.Unix(10, 500000000))}, tc.opts...)
			if got := series(t, summaryRegistry(t, tc.build(t), opts...)); !slices.Equal(got, tc.want) {
				t.Fatalf("series:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}

func TestSummaryNilMonitor(t *testing.T) {
	t.Log("negative: nil monitor; expected: gather error, no panic")
	r := client.NewRegistry()
	r.MustRegister(monitorprom.NewSummaryCollector(nil))
	if _, err := r.Gather(); err == nil {
		t.Fatal("nil monitor gathered without error")
	}
}

func TestSummaryProblems(t *testing.T) {
	const grace = 45 * time.Second
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		setup                                        func(*linkmonitor.PrometheusFixture)
		scrape                                       time.Duration
		want                                         []string
	}{
		{"speed-pass", "negative", "all checks pass", "no sparse series", func(*linkmonitor.PrometheusFixture) {}, 0, nil},
		{"speed-fail", "positive", "eth1 below maximum", "max_speed on eth1", func(f *linkmonitor.PrometheusFixture) {
			f.Speed(1, 1000, 10000)
		}, 0, []string{"interface_problem{interface=eth1,reason=max_speed}"}},
		{"speed-excepted", "negative", "bare-name exception", "no max_speed", func(f *linkmonitor.PrometheusFixture) {
			f.Speed(1, 1000, 10000)
			f.Exceptions("eth1")
		}, 0, nil},
		{"speed-excepted-at-speed", "negative", "NAME=SPEED at exactly SPEED", "no max_speed", func(f *linkmonitor.PrometheusFixture) {
			f.Speed(1, 1000, 10000)
			f.Exceptions("eth1=1G")
		}, 0, nil},
		{"speed-excepted-degraded", "corner", "NAME=SPEED, link below SPEED", "max_speed present", func(f *linkmonitor.PrometheusFixture) {
			f.Speed(1, 100, 10000)
			f.Exceptions("eth1=1G")
		}, 0, []string{"interface_problem{interface=eth1,reason=max_speed}"}},
		{"exception-unmatched", "positive", "selector names a missing interface", "host exception_unmatched", func(f *linkmonitor.PrometheusFixture) {
			f.Exceptions("eth9")
		}, 0, []string{"host_problem{reason=exception_unmatched}"}},
		{"netstat-failed", "positive", "host netstat fails", "host collector, no interface label", func(f *linkmonitor.PrometheusFixture) {
			f.Fail(0, model.CollectorNetstat)
		}, 0, []string{"host_problem{reason=collector}"}},
		{"failed-attempt", "positive", "driver fails on eth2", "collector on eth2", func(f *linkmonitor.PrometheusFixture) {
			f.Fail(2, model.CollectorDriver)
		}, 0, []string{"interface_problem{interface=eth2,reason=collector}"}},
		{"unknown-at-grace", "boundary", "new up eth4 without settings at the grace", "check_unknown on eth4", func(f *linkmonitor.PrometheusFixture) {
			f.Device(4, "eth4", true)
		}, grace, []string{"interface_problem{interface=eth4,reason=check_unknown}"}},
		{"unknown-inside-grace", "boundary", "same, one nanosecond earlier", "none", func(f *linkmonitor.PrometheusFixture) {
			f.Device(4, "eth4", true)
		}, grace - 1, nil},
		{"crc-increase", "positive", "CRC counter 10 to 11", "errors on eth1", func(f *linkmonitor.PrometheusFixture) {
			crc(f, 1, 10)
			crc(f, 1, 11)
		}, 0, []string{"interface_problem{interface=eth1,reason=errors}"}},
		{"window-after", "boundary", "CRC increase, scrape at W+1ns", "none without republishing", func(f *linkmonitor.PrometheusFixture) {
			crc(f, 1, 10)
			crc(f, 1, 11)
		}, 180*time.Second + 1, nil},
		{"multiple-reasons", "corner", "eth1 slow, flapped and driver failed", "three series", func(f *linkmonitor.PrometheusFixture) {
			f.Device(1, "eth1", false)
			f.Device(1, "eth1", true)
			f.Speed(1, 1000, 10000)
			f.Fail(1, model.CollectorDriver)
		}, 0, []string{"interface_problem{interface=eth1,reason=collector}", "interface_problem{interface=eth1,reason=flap}", "interface_problem{interface=eth1,reason=max_speed}"}},
		{"problems-consistency", "corner", "problems on three interfaces and the host", "problems equals sparse series", func(f *linkmonitor.PrometheusFixture) {
			f.Speed(1, 1000, 10000)
			f.Fail(2, model.CollectorDriver)
			f.Device(3, "eth3", false)
			f.Fail(0, model.CollectorNetstat)
			f.Exceptions("eth9")
		}, 0, []string{"host_problem{reason=collector}", "host_problem{reason=exception_unmatched}", "interface_problem{interface=eth1,reason=max_speed}",
			"interface_problem{interface=eth2,reason=collector}", "interface_problem{interface=eth3,reason=flap}"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			f := linkmonitor.NewPrometheusFixture(t)
			for i := uint32(1); i <= 3; i++ {
				f.Device(i, fmt.Sprintf("eth%d", i), true)
				f.Speed(i, 10000, 10000)
			}
			tc.setup(f)
			f.Publish(3)
			got := problemSeries(t, summaryRegistry(t, f.Monitor, fixedClock(f.ScrapeTime(tc.scrape))))
			if !slices.Equal(got, tc.want) {
				t.Fatalf("problems:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}

func crc(f *linkmonitor.PrometheusFixture, index uint32, value uint64) {
	if err := f.Samples(index, model.CollectorNetdev, model.Sample{Descriptor: "go_link_monitor_interface_receive_crc_errors_total", Kind: model.SampleCounter,
		Number: model.Unsigned(value), Counter: model.CounterIdentity{Source: "kernel", Width: 64}}); err != nil {
		panic(err)
	}
}

// problemSeries returns the sparse series without prefix and checks that
// problems equals their count in the same scrape.
func problemSeries(t testing.TB, r client.Gatherer) []string {
	t.Helper()
	var sparse []string
	problems := -1.0
	for _, line := range series(t, r) {
		switch {
		case strings.HasPrefix(line, problemsName+"{"):
			_, _ = fmt.Sscanf(strings.TrimPrefix(line, problemsName+"{} "), "%g", &problems)
		case strings.HasPrefix(line, interfaceName+"{"), strings.HasPrefix(line, hostName+"{"):
			if !strings.HasSuffix(line, " 1") {
				t.Fatalf("sparse series value is not 1: %s", line)
			}
			sparse = append(sparse, strings.TrimSuffix(strings.TrimPrefix(line, "go_link_monitor_"), " 1"))
		}
	}
	if int(problems) != len(sparse) {
		t.Fatalf("problems = %g with %d sparse series", problems, len(sparse))
	}
	return sparse
}

func TestSummaryNativeRDMA(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		samples                                      func(uint64) []model.Sample
		want                                         []string
	}{
		{"ib-link-downed", "positive", "link_downed increases on native port 1", "flap on the canonical port name", func(v uint64) []model.Sample {
			return []model.Sample{ibCounter("link_downed_total", v)}
		}, []string{"interface_problem{interface=rdma:mlx5_0:1,reason=flap}"}},
		{"ib-symbol-error", "positive", "symbol_error increases on native port 1", "errors on the canonical port name", func(v uint64) []model.Sample {
			return []model.Sample{ibCounter("symbol_error_total", v)}
		}, []string{"interface_problem{interface=rdma:mlx5_0:1,reason=errors}"}},
		{"ib-data", "negative", "data counter increases", "none", func(v uint64) []model.Sample {
			return []model.Sample{ibCounter("port_rcv_data_total", v)}
		}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			f := linkmonitor.NewPrometheusFixture(t)
			f.RDMA(true)
			for _, v := range []uint64{1, 2} {
				if err := f.NativeSamples(1, model.CollectorRDMACounters, tc.samples(v)...); err != nil {
					t.Fatal(err)
				}
			}
			f.Publish(2)
			var got []string
			for _, line := range problemSeries(t, summaryRegistry(t, f.Monitor, fixedClock(f.ScrapeTime(0)))) {
				if strings.Contains(line, "reason=errors") || strings.Contains(line, "reason=flap") {
					got = append(got, line)
				}
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("problems = %v, want %v", got, tc.want)
			}
		})
	}
}

func ibCounter(name string, value uint64) model.Sample {
	return model.Sample{Descriptor: "go_link_monitor_infiniband_" + name, Kind: model.SampleCounter, Number: model.Unsigned(value),
		Counter: model.CounterIdentity{Source: "sysfs", Width: 64}, NoInterfaceLabel: true,
		Labels: []model.Label{{Name: "device", Value: "mlx5_0"}, {Name: "port", Value: "1"}}}
}

func TestSummaryLinkTimestamps(t *testing.T) {
	t.Log("positive: flap and CRC evidence; expected: last flap and last error timestamps on /metrics/link, even with windows disabled")
	f := healthyFixture(t, 1)
	f.ProblemConfig(func(c *linkmonitor.Config) { c.ErrorWindow, c.FlapWindow = 0, 0 })
	crc(f, 1, 1)
	crc(f, 1, 2)
	f.Device(1, "eth1", false)
	f.Publish(1)
	match, err := monitorprom.CategoryMatcher(monitorprom.CategoryLink)
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Join(series(t, filteredRegistry(t, f.Monitor, monitorprom.WithMetricFilter(match))), "\n")
	for _, want := range []string{"go_link_monitor_interface_last_flap_timestamp_seconds{interface=eth1} 10.5",
		"go_link_monitor_interface_last_error_timestamp_seconds{class=link,interface=eth1} 10.5"} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in\n%s", want, got)
		}
	}
	if problems := problemSeries(t, summaryRegistry(t, f.Monitor, fixedClock(f.ScrapeTime(0)))); len(problems) != 0 {
		t.Fatalf("windows disabled but problems = %v", problems)
	}
}

func TestCategoryEquivalence(t *testing.T) {
	t.Log("positive: every category on the deployed host and native RDMA; expected: categories partition the unfiltered output; shared families on every path")
	f := linkmonitor.NewPrometheusFixture(t)
	f.PerformanceDeployedHost()
	f.PerformanceNative()
	f.Publish(3)
	all := series(t, monitorRegistry(t, f.Monitor))
	seen := make(map[string]string)
	for _, category := range monitorprom.Categories() {
		match, err := monitorprom.CategoryMatcher(category)
		if err != nil {
			t.Fatal(err)
		}
		lines := series(t, filteredRegistry(t, f.Monitor, monitorprom.WithMetricFilter(match)))
		if !slices.ContainsFunc(lines, func(l string) bool { return strings.HasPrefix(l, "go_link_monitor_collection_healthy{") }) {
			t.Errorf("%s: collection_healthy missing", category)
		}
		for _, line := range lines {
			if !slices.Contains(all, line) {
				t.Errorf("%s: %s not in the unfiltered output with the same value", category, line)
			}
			shared := monitorprom.CategoryOf(line[:strings.IndexByte(line, '{')]) == monitorprom.CategoryShared
			if previous, ok := seen[line]; ok && !shared {
				t.Errorf("%s: %s also on %s", category, line, previous)
			}
			seen[line] = category
		}
	}
	for _, line := range all {
		name := line[:strings.IndexByte(line, '{')]
		if _, ok := seen[line]; !ok && monitorprom.CategoryOf(name) != monitorprom.CategorySummary {
			t.Errorf("%s (category %q) on no category path", line, monitorprom.CategoryOf(name))
		}
	}
}

func TestCategorySourceNames(t *testing.T) {
	t.Log("positive: every fixed source descriptor; expected: exactly one detail category each")
	for _, name := range linkmonitor.FixedSourceNames() {
		category := monitorprom.CategoryOf(name)
		if !slices.Contains(monitorprom.Categories(), category) {
			t.Errorf("%s: category %q", name, category)
		}
	}
}

func TestSummaryAllocationGuards(t *testing.T) {
	collect := func(c client.Collector) float64 {
		out := make(chan client.Metric, 4096)
		return testing.AllocsPerRun(50, func() {
			c.Collect(out)
			for len(out) > 0 {
				<-out
			}
		})
	}
	small := collect(monitorprom.NewSummaryCollector(healthyFixture(t, 3).Monitor, fixedClock(time.Unix(10, 500000000))))
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		ports, broken                                int
	}{
		{"healthy-port-independent", "positive", "1,024 healthy ports", "same allocations as 3 ports", 1024, 0},
		{"broken-one", "boundary", "one slow port of 64", "bounded by scalars plus one problem", 64, 1},
		{"broken-64", "boundary", "64 slow ports", "linear in problems", 64, 64},
		{"broken-512", "boundary", "512 slow ports", "linear in problems", 512, 512},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			got := collect(monitorprom.NewSummaryCollector(speedFixture(t, tc.ports, tc.broken), fixedClock(time.Unix(10, 500000000))))
			// A two-label const metric costs 12 allocations in client_golang
			// (metric, label pairs and their strings); allow 16 per series.
			if limit := small + 16*float64(tc.broken); got > limit || (tc.broken == 0 && got != small) {
				t.Fatalf("allocations = %v, healthy 3-port = %v, limit %v", got, small, limit)
			}
		})
	}
}

func TestRangeSamplesSkipping(t *testing.T) {
	t.Log("positive: skip every schema; expected: no sample visited; nil skip visits the same samples as RangeSamples")
	f := linkmonitor.NewPrometheusFixture(t)
	f.PerformanceDeployedHost()
	f.Publish(3)
	s := f.Monitor.Snapshot()
	visited := 0
	s.RangeSamplesSkipping(func(linkmonitor.SchemaID) bool { return true }, func(linkmonitor.SampleView) bool { visited++; return true })
	all, unskipped := 0, 0
	s.RangeSamples(func(linkmonitor.SampleView) bool { all++; return true })
	s.RangeSamplesSkipping(nil, func(linkmonitor.SampleView) bool { unskipped++; return true })
	schemas := 0
	s.RangeSchemas(func(linkmonitor.SchemaID, func(func(string) bool)) bool { schemas++; return true })
	if visited != 0 || all == 0 || unskipped != all || schemas == 0 {
		t.Fatalf("visited=%d all=%d unskipped=%d schemas=%d", visited, all, unskipped, schemas)
	}
}

func TestSummaryConcurrentScrapePublish(t *testing.T) {
	t.Log("race: 16 scrapers while a publisher flaps, toggles checks and changes schemas; expected: no race, problems always equals sparse series")
	f := healthyFixture(t, 4)
	var clock atomic.Int64
	clock.Store(time.Unix(10, 500000000).UnixNano())
	r := summaryRegistry(t, f.Monitor, monitorprom.WithClock(func() time.Time { return time.Unix(0, clock.Load()) }))
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					problemSeries(t, r)
				}
			}
		})
	}
	for i := range 200 {
		port := uint32(i%4 + 1)
		f.Device(port, fmt.Sprintf("eth%d", port), i%2 == 0)
		f.Speed(port, uint64(1000+9000*(i%2)), 10000)
		_ = f.Samples(port, model.CollectorDriver, linkmonitor.PerformanceSamples(i%7+1)...)
		f.Publish(4)
		clock.Add(int64(time.Second))
	}
	close(stop)
	wg.Wait()
}

func TestSummaryWindowExpiryRace(t *testing.T) {
	t.Log("race: clock advanced across the window during scrapes; expected: flap never reported once now is past the window")
	f := healthyFixture(t, 1)
	f.Device(1, "eth1", false)
	f.Publish(1)
	base := f.ScrapeTime(0)
	var offset atomic.Int64
	r := summaryRegistry(t, f.Monitor, monitorprom.WithClock(func() time.Time { return base.Add(time.Duration(offset.Load())) }))
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 200 {
				// Read the clock before scraping; the scrape's own now is no earlier.
				before := time.Duration(offset.Load())
				got := problemSeries(t, r)
				if before > 180*time.Second && slices.Contains(got, "interface_problem{interface=eth1,reason=flap}") {
					t.Error("flap reported after the window")
					return
				}
			}
		})
	}
	for step := time.Duration(0); step <= 200*time.Second; step += time.Second {
		offset.Store(int64(step))
	}
	wg.Wait()
}

func TestAllPathsConcurrentScrape(t *testing.T) {
	t.Log("race: summary, every category and all scraped during publication; expected: no race and every response gathers")
	f := linkmonitor.NewPrometheusFixture(t)
	f.PerformanceDeployedHost()
	f.Publish(3)
	registries := make([]client.Gatherer, 0, 2+len(monitorprom.Categories()))
	registries = append(registries, summaryRegistry(t, f.Monitor), monitorRegistry(t, f.Monitor))
	for _, category := range monitorprom.Categories() {
		match, err := monitorprom.CategoryMatcher(category)
		if err != nil {
			t.Fatal(err)
		}
		registries = append(registries, filteredRegistry(t, f.Monitor, monitorprom.WithMetricFilter(match)))
	}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for _, r := range registries {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					if _, err := r.Gather(); err != nil {
						t.Error(err)
						return
					}
				}
			}
		})
	}
	for i := range 50 {
		// Schema churn: the driver statistic count changes every publication.
		_ = f.Samples(1, model.CollectorDriver, linkmonitor.PerformanceSamples(1600+i%15)...)
		f.Device(2, "enp35s0f0np0", i%2 == 0)
		f.Publish(3)
	}
	close(stop)
	wg.Wait()
}
