package linkmonitor_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	client "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
)

// interfaceBenchmarkCases compare one interface's view with its whole path.
// An empty iface measures the whole path.
var interfaceBenchmarkCases = []struct {
	name, category, description, expectedOutcome string
	fixture                                      func(testing.TB) *linkmonitor.Monitor
	path, iface                                  string
}{
	{"deployed/driver", "path", "driver category, three ports", "baseline", deployedMonitor, monitorprom.CategoryDriver, ""},
	{"deployed/driver/one", "interface", "driver category, one of three ports", "about a third of deployed/driver", deployedMonitor, monitorprom.CategoryDriver, "enp1s0"},
	{"deployed/all", "path", "all path, three ports", "baseline", deployedMonitor, "all", ""},
	{"deployed/all/one", "interface", "all path, one of three ports", "below a third of deployed/all: no netstat", deployedMonitor, "all", "enp1s0"},
	{"healthy-1024/all/one", "interface", "all path, one of 1,024 ports", "same as one port of three", func(t testing.TB) *linkmonitor.Monitor { return speedFixture(t, 1024, 0) }, "all", "eth2"},
	{"healthy-3/all/one", "interface", "all path, one of three ports", "baseline for healthy-1024/all/one", func(t testing.TB) *linkmonitor.Monitor { return speedFixture(t, 3, 0) }, "all", "eth2"},
}

func deployedMonitor(t testing.TB) *linkmonitor.Monitor {
	f := linkmonitor.NewPrometheusFixture(t)
	f.PerformanceDeployedHost()
	f.Publish(3)
	return f.Monitor
}

func benchmarkCollectors(b *testing.B, m *linkmonitor.Monitor, path string) []client.Collector {
	if path == "all" {
		return allCollectors(m, time.Unix(10, 500000000))
	}
	return []client.Collector{categoryCollector(b, m, path)}
}

// BenchmarkInterfaceCollect measures Collect of one interface view, lookup
// included, against its whole path on the same snapshot.
func BenchmarkInterfaceCollect(b *testing.B) {
	for _, tc := range interfaceBenchmarkCases {
		b.Run(tc.name, func(b *testing.B) {
			b.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			m := tc.fixture(b)
			parts := benchmarkCollectors(b, m, tc.path)
			out, stop := drain()
			collect := func() {
				if tc.iface == "" {
					for _, c := range parts {
						c.Collect(out)
					}
					return
				}
				c, err := monitorprom.InterfaceCollector(m.Snapshot(), tc.iface, parts...)
				if err != nil {
					b.Fatal(err)
				}
				c.Collect(out)
			}
			collect()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				collect()
			}
			b.StopTimer()
			stop()
		})
	}
}

// BenchmarkInterfaceHTTP measures a full interface request as the command
// serves it: lookup, a request-scoped registry and promhttp handler,
// gathering and text encoding. Whole-path rows reuse one handler.
func BenchmarkInterfaceHTTP(b *testing.B) {
	for _, tc := range interfaceBenchmarkCases {
		b.Run(tc.name, func(b *testing.B) {
			b.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			m := tc.fixture(b)
			parts := benchmarkCollectors(b, m, tc.path)
			whole := client.NewRegistry()
			whole.MustRegister(parts...)
			wholeHandler := promhttp.HandlerFor(whole, promhttp.HandlerOpts{})
			handler := func() http.Handler {
				if tc.iface == "" {
					return wholeHandler
				}
				c, err := monitorprom.InterfaceCollector(m.Snapshot(), tc.iface, parts...)
				if err != nil {
					b.Fatal(err)
				}
				r := client.NewRegistry()
				r.MustRegister(c)
				return promhttp.HandlerFor(r, promhttp.HandlerOpts{})
			}
			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			b.ReportAllocs()
			for b.Loop() {
				w := httptest.NewRecorder()
				handler().ServeHTTP(w, req)
				if w.Code != http.StatusOK {
					b.Fatal(w.Body.String())
				}
				b.SetBytes(int64(w.Body.Len()))
			}
		})
	}
}
