package linkmonitor_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	client "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func performanceRegistry(t testing.TB, ports, fields int) *client.Registry {
	t.Helper()
	f := linkmonitor.NewPrometheusFixture(t)
	samples := linkmonitor.PerformanceSamples(fields)
	for i := 1; i <= ports; i++ {
		f.Device(uint32(i), fmt.Sprintf("eth%d", i), true)
		if err := f.Samples(uint32(i), model.CollectorDriver, samples...); err != nil {
			t.Fatal(err)
		}
	}
	f.Publish(uint64(ports))
	r := monitorRegistry(t, f.Monitor)
	gathered(t, r)
	return r
}

func BenchmarkPerformanceExporter(b *testing.B) {
	for _, size := range [][2]int{{2, 64}, {8, 64}, {32, 0}, {32, 64}, {32, 1024}, {128, 64}, {256, 64}, {2, 8192}, {1, 65536}} {
		for _, encoding := range []string{"gather", "http"} {
			b.Run(fmt.Sprintf("ports=%d/fields=%d/%s", size[0], size[1], encoding), func(b *testing.B) {
				r := performanceRegistry(b, size[0], size[1])
				h := promhttp.HandlerFor(r, promhttp.HandlerOpts{})
				req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
				b.ReportAllocs()
				for b.Loop() {
					if encoding == "gather" {
						if _, err := r.Gather(); err != nil {
							b.Fatal(err)
						}
					} else {
						w := httptest.NewRecorder()
						h.ServeHTTP(w, req)
						if w.Code != http.StatusOK {
							b.Fatal(w.Body.String())
						}
						b.SetBytes(int64(w.Body.Len()))
					}
				}
			})
		}
	}
}

func BenchmarkPerformanceMixedGather(b *testing.B) {
	f := linkmonitor.NewPrometheusFixture(b)
	f.Device(1, "eth0", true)
	f.Device(2, "eth2", true)
	f.RDMA(false)
	f.PerformanceNative()
	f.Publish(4)
	r := monitorRegistry(b, f.Monitor)
	gathered(b, r)
	b.ReportAllocs()
	for b.Loop() {
		if _, err := r.Gather(); err != nil {
			b.Fatal(err)
		}
	}
}
