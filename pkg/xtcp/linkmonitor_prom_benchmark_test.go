package xtcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_config"
)

// BenchmarkPromScrapeLinkMonitor measures a full promPath scrape of xtcp2's
// registry, gather and text encoding included, with and without the embedded
// monitor. The monitor adds the problem summary, the collection mode gauge and
// the up gauge; a running monitor publishes this host's interfaces, so the
// running case varies with the host. The detail paths are not on promPath and
// are benchmarked in pkg/linkmonitor.
func BenchmarkPromScrapeLinkMonitor(b *testing.B) {
	tests := []struct {
		name, category, description, expectedOutcome string
		enable, run                                  bool
	}{
		{"monitor_off", "baseline", "-linkMonitor=false: xtcp2's own metrics only", "the reference cost", false, false},
		{"monitor_built", "positive", "the monitor is built but not yet running, as before its first publication", "summary from the empty snapshot", true, false},
		{"monitor_running", "positive", "the default on-demand monitor running on this host", "summary of this host's interfaces; no refresh on promPath", true, true},
	}
	for _, tc := range tests {
		b.Run(tc.name, func(b *testing.B) {
			b.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			registry := prometheus.NewRegistry()
			x := &XTCP{registry: registry, config: &xtcp_config.XtcpConfig{LinkMonitor: &xtcp_config.LinkMonitorConfig{
				Enable: tc.enable, BaselineFile: b.TempDir() + "/baseline.json", CollectionMode: "on-demand",
			}}}
			x.fatalf = b.Fatalf
			var promWG sync.WaitGroup
			promWG.Add(1)
			x.InitPromethus(&promWG)
			x.initLinkMonitor()
			if tc.run {
				ctx, cancel := context.WithCancel(context.Background())
				var wg sync.WaitGroup
				wg.Add(1)
				go x.runLinkMonitor(ctx, &wg)
				b.Cleanup(func() { cancel(); wg.Wait() })
				deadline := time.Now().Add(10 * time.Second)
				for !x.linkMonitor.Health().Running {
					if x.linkMonitorDown.Load() || time.Now().After(deadline) {
						b.Skip("link monitor did not start on this host")
					}
					time.Sleep(10 * time.Millisecond)
				}
			}
			h := promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
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
