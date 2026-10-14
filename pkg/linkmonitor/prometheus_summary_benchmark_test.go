package linkmonitor_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	client "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
)

// speedFixture publishes ports interfaces, the first broken of which run
// below their maximum speed and so carry one speed problem each.
func speedFixture(t testing.TB, ports, broken int) *linkmonitor.Monitor {
	f := linkmonitor.NewPrometheusFixture(t)
	for i := 1; i <= ports; i++ {
		f.Device(uint32(i), fmt.Sprintf("eth%d", i), true)
		active := uint64(10000)
		if i <= broken {
			active = 1000
		}
		f.Speed(uint32(i), active, 10000)
	}
	f.Publish(uint64(ports))
	return f.Monitor
}

var summaryBenchmarkCases = []struct {
	name, category, description, expectedOutcome string
	ports, broken                                int
}{
	{"healthy-3", "healthy", "reference host", "four scalars", 3, 0},
	{"healthy-64", "healthy", "large host", "same cost as healthy-3", 64, 0},
	{"healthy-1024", "healthy", "fleet-sized host", "same cost as healthy-3", 1024, 0},
	{"broken-3", "broken", "one slow port of three", "scalars plus one series", 3, 1},
	{"worst-64", "worst", "every port slow", "linear in problems", 64, 64},
}

func drain() (chan client.Metric, func()) {
	out := make(chan client.Metric, 1024)
	done := make(chan struct{})
	go func() {
		for range out {
		}
		close(done)
	}()
	return out, func() { close(out); <-done }
}

// BenchmarkSummaryCollect measures summary Collect alone; healthy rows must
// not grow with the port count.
func BenchmarkSummaryCollect(b *testing.B) {
	for _, tc := range summaryBenchmarkCases {
		b.Run(tc.name, func(b *testing.B) {
			b.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			collector := monitorprom.NewSummaryCollector(speedFixture(b, tc.ports, tc.broken), fixedClock(time.Unix(10, 500000000)))
			out, stop := drain()
			b.ReportAllocs()
			for b.Loop() {
				collector.Collect(out)
			}
			b.StopTimer()
			stop()
		})
	}
}

// BenchmarkSummaryHTTP measures a full summary scrape, including gathering,
// sorting and text encoding.
func BenchmarkSummaryHTTP(b *testing.B) {
	for _, tc := range summaryBenchmarkCases {
		b.Run(tc.name, func(b *testing.B) {
			b.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			h := promhttp.HandlerFor(summaryRegistry(b, speedFixture(b, tc.ports, tc.broken)), promhttp.HandlerOpts{})
			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			b.ReportAllocs()
			for b.Loop() {
				w := httptest.NewRecorder()
				h.ServeHTTP(w, req)
				if w.Code != http.StatusOK {
					b.Fatal(w.Body.String())
				}
				b.SetBytes(int64(w.Body.Len()))
			}
		})
	}
}

// BenchmarkSummaryParallel gathers the summary from every P at once, as
// several Prometheus replicas scraping one host would.
func BenchmarkSummaryParallel(b *testing.B) {
	b.Log("healthy: 1,024 ports, concurrent gathers; expected: scales with GOMAXPROCS, no shared lock")
	r := summaryRegistry(b, speedFixture(b, 1024, 0))
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if _, err := r.Gather(); err != nil {
				b.Error(err)
				return
			}
		}
	})
}

// BenchmarkCategoryCollect measures each detail path on the reference
// fixtures; together the categories should cost about one unfiltered scrape.
func BenchmarkCategoryCollect(b *testing.B) {
	for _, fixture := range filterBenchmarkFixtures() {
		for _, category := range monitorprom.Categories() {
			b.Run(fixture.name+"/"+category, func(b *testing.B) {
				match, err := monitorprom.CategoryMatcher(category)
				if err != nil {
					b.Fatal(err)
				}
				collector := monitorprom.NewCollector(fixture.make(b), monitorprom.WithMetricFilter(match))
				out, stop := drain()
				collector.Collect(out)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					collector.Collect(out)
				}
				b.StopTimer()
				stop()
			})
		}
	}
}
