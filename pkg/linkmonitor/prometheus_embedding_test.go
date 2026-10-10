package linkmonitor_test

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	client "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
)

type embeddingMetric struct{ metric client.Metric }

func (c embeddingMetric) Describe(chan<- *client.Desc)     {}
func (c embeddingMetric) Collect(out chan<- client.Metric) { out <- c.metric }

func TestEmbeddingDynamicCollisions(t *testing.T) {
	const name = "go_link_monitor_netstat_Tcp_Embedding"
	const help = "Cached source value for " + name + "."
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		help                                         string
		kind                                         client.ValueType
		label                                        string
		wantError                                    bool
	}{
		{"distinct series", "boundary", "compatible schema with distinct label value", "both series gather", help, client.UntypedValue, "host", false},
		{"duplicate", "negative", "dynamic series appears after successful scrape", "duplicate rejected at Gather", help, client.UntypedValue, "monitor", true},
		{"type", "negative", "dynamic family conflicts with host gauge", "type mismatch at Gather", help, client.GaugeValue, "host", true},
		{"help", "corner", "dynamic family conflicts with host help", "help mismatch at Gather", "Host value.", client.UntypedValue, "host", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			f := linkmonitor.NewPrometheusFixture(t)
			f.Publish(0)
			r := client.NewRegistry()
			collector := monitorprom.NewCollector(f.Monitor)
			if err := r.Register(collector); err != nil {
				t.Fatal(err)
			}
			metric, err := client.NewConstMetric(client.NewDesc(name, tc.help, []string{"origin"}, nil), tc.kind, 1, tc.label)
			if err != nil {
				t.Fatal(err)
			}
			if err := r.Register(embeddingMetric{metric: metric}); err != nil {
				t.Fatal(err)
			}
			if _, err := r.Gather(); err != nil {
				t.Fatal("initial gather", err)
			}
			if err := f.Samples(0, model.CollectorNetstat, model.Sample{Descriptor: "netstat_Tcp_Embedding", Kind: model.SampleUntyped, Number: model.Unsigned(1), Labels: []model.Label{{Name: "origin", Value: "monitor"}}}); err != nil {
				t.Fatal(err)
			}
			f.Publish(0)
			if _, err := r.Gather(); (err != nil) != tc.wantError {
				t.Fatalf("Gather error=%v", err)
			}
			response := httptest.NewRecorder()
			promhttp.HandlerFor(r, promhttp.HandlerOpts{ErrorHandling: promhttp.HTTPErrorOnError}).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
			want := http.StatusOK
			if tc.wantError {
				want = http.StatusInternalServerError
			}
			if response.Code != want {
				t.Fatalf("HTTP %d, want %d", response.Code, want)
			}
			if r.Unregister(collector) {
				t.Fatal("unchecked collector unexpectedly unregistered")
			}
		})
	}
}

func TestEmbeddingBlockedCollection(t *testing.T) {
	t.Log("corner: inventory held at barrier while concurrent scrapes and controls run; expected: cached Gather completes without waiting for inventory, then shutdown joins")
	linkmonitor.WithBlockedEmbedding(t, func(m *linkmonitor.Monitor) {
		r := monitorRegistry(t, m)
		finished := make(chan struct{})
		go func() {
			defer close(finished)
			var scrapes sync.WaitGroup
			for range 10 {
				scrapes.Go(func() {
					if _, err := r.Gather(); err != nil {
						t.Error(err)
					}
					if err := m.RequestResync(); err != nil {
						t.Error(err)
					}
				})
			}
			scrapes.Wait()
		}()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Fatal("scrapes blocked on collection")
		}
	})
}
