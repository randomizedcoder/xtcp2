package linkmonitor_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	client "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
)

func BenchmarkPrometheusCollectorCollect(b *testing.B) {
	for _, fixture := range []struct {
		name string
		make func(*testing.B) *linkmonitor.Monitor
	}{
		{name: "local", make: func(b *testing.B) *linkmonitor.Monitor {
			f := linkmonitor.NewPrometheusFixture(b)
			f.PerformanceLocalMachine()
			f.Publish(3)
			return f.Monitor
		}},
		{name: "fleet", make: func(b *testing.B) *linkmonitor.Monitor {
			f := linkmonitor.NewPrometheusFixture(b)
			f.PerformanceRealisticFleet()
			f.Publish(4)
			return f.Monitor
		}},
	} {
		b.Run(fixture.name, func(b *testing.B) {
			collector := monitorprom.NewCollector(fixture.make(b))
			out := make(chan client.Metric, 1024)
			done := make(chan struct{})
			go func() {
				for range out {
				}
				close(done)
			}()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				collector.Collect(out)
			}
			b.StopTimer()
			close(out)
			<-done
		})
	}
}

func BenchmarkPrometheusCachedGather(b *testing.B) {
	f := linkmonitor.NewPrometheusFixture(b)
	f.Device(1, "eth0", true)
	if err := f.Samples(0, model.CollectorNetstat, model.Sample{Descriptor: "netstat_Tcp_ActiveOpens", Kind: model.SampleUntyped, Number: model.Unsigned(42)}); err != nil {
		b.Fatal(err)
	}
	f.Publish(1)
	r := monitorRegistry(b, f.Monitor)
	gathered(b, r)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := r.Gather(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPrometheusSchemaChange(b *testing.B) {
	f := linkmonitor.NewPrometheusFixture(b)
	r := monitorRegistry(b, f.Monitor)
	keys := [...]string{"netstat_Tcp_ActiveOpens", "netstat_MPTcpExt_MPCapableSYNRX"}
	i := 0
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := f.Samples(0, model.CollectorNetstat, model.Sample{Descriptor: keys[i%2], Kind: model.SampleUntyped, Number: model.Unsigned(42)}); err != nil {
			b.Fatal(err)
		}
		f.Publish(1)
		if _, err := r.Gather(); err != nil {
			b.Fatal(err)
		}
		i++
	}
}

func filterBenchmarkCases(b *testing.B) []struct {
	name string
	opts []monitorprom.Option
} {
	core, err := monitorprom.NameMatcher(monitorprom.CoreMetricsPattern, "")
	if err != nil {
		b.Fatal(err)
	}
	return []struct {
		name string
		opts []monitorprom.Option
	}{
		{name: "unfiltered"},
		{name: "core", opts: []monitorprom.Option{monitorprom.WithMetricFilter(core)}},
		{name: "all", opts: []monitorprom.Option{monitorprom.WithMetricFilter(func(string) bool { return true })}},
		{name: "reject-all", opts: []monitorprom.Option{monitorprom.WithMetricFilter(func(string) bool { return false })}},
	}
}

func filterBenchmarkFixtures() []struct {
	name string
	make func(*testing.B) *linkmonitor.Monitor
} {
	return []struct {
		name string
		make func(*testing.B) *linkmonitor.Monitor
	}{
		{name: "local", make: func(b *testing.B) *linkmonitor.Monitor {
			f := linkmonitor.NewPrometheusFixture(b)
			f.PerformanceLocalMachine()
			f.Publish(3)
			return f.Monitor
		}},
		{name: "deployed", make: func(b *testing.B) *linkmonitor.Monitor {
			f := linkmonitor.NewPrometheusFixture(b)
			f.PerformanceDeployedHost()
			f.Publish(3)
			return f.Monitor
		}},
		{name: "fleet", make: func(b *testing.B) *linkmonitor.Monitor {
			f := linkmonitor.NewPrometheusFixture(b)
			f.PerformanceRealisticFleet()
			f.Publish(4)
			return f.Monitor
		}},
	}
}

// BenchmarkPrometheusFilteredCollect compares exposure tiers on one snapshot.
// "unfiltered" must match BenchmarkPrometheusCollectorCollect from before
// filter support; "core" must scale with the emitted subset.
func BenchmarkPrometheusFilteredCollect(b *testing.B) {
	for _, fixture := range filterBenchmarkFixtures() {
		for _, filter := range filterBenchmarkCases(b) {
			b.Run(fixture.name+"/"+filter.name, func(b *testing.B) {
				collector := monitorprom.NewCollector(fixture.make(b), filter.opts...)
				out := make(chan client.Metric, 1024)
				done := make(chan struct{})
				go func() {
					for range out {
					}
					close(done)
				}()
				collector.Collect(out)
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					collector.Collect(out)
				}
				b.StopTimer()
				close(out)
				<-done
			})
		}
	}
}

// BenchmarkPrometheusFilteredHTTP measures full scrape cost, including
// gathering, sorting and text encoding, per exposure tier.
func BenchmarkPrometheusFilteredHTTP(b *testing.B) {
	for _, fixture := range filterBenchmarkFixtures() {
		for _, filter := range filterBenchmarkCases(b) {
			b.Run(fixture.name+"/"+filter.name, func(b *testing.B) {
				r := client.NewRegistry()
				if err := r.Register(monitorprom.NewCollector(fixture.make(b), filter.opts...)); err != nil {
					b.Fatal(err)
				}
				h := promhttp.HandlerFor(r, promhttp.HandlerOpts{})
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
}
