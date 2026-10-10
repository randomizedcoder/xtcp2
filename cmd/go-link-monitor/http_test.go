package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
)

type fakeMonitor struct {
	ready      atomic.Bool
	run        func(context.Context) error
	rebaseline func() error
}

func (m *fakeMonitor) Health() linkmonitor.Health    { return linkmonitor.Health{Ready: m.ready.Load()} }
func (m *fakeMonitor) Run(ctx context.Context) error { return m.run(ctx) }
func (m *fakeMonitor) RequestRebaseline() error      { return m.rebaseline() }

func TestHTTPHealth(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		ready, stop                                  bool
		path                                         string
		status                                       int
	}{
		{"starting", "boundary", "before first publication", "not ready", false, false, "/readyz", 503},
		{"live", "positive", "process active before readiness", "alive", false, false, "/healthz", 200},
		{"ready", "positive", "baseline and required collection healthy", "ready", true, false, "/readyz", 200},
		{"failed source", "negative", "required source unhealthy", "not ready but process remains live", false, false, "/readyz", 503},
		{"stopping", "corner", "shutdown with old ready snapshot", "not ready", true, true, "/readyz", 503},
		{"stopped health", "boundary", "shutdown liveness", "unavailable", true, true, "/healthz", 503},
		{"unknown", "negative", "unregistered path", "not found", true, false, "/debug/pprof", 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			m := &fakeMonitor{}
			m.ready.Store(tc.ready)
			var stop atomic.Bool
			stop.Store(tc.stop)
			server := httpServer(m, prometheus.NewRegistry(), &stop)
			response := httptest.NewRecorder()
			server.Handler.ServeHTTP(response, httptest.NewRequestWithContext(t.Context(), "GET", tc.path, nil))
			if response.Code != tc.status || server.WriteTimeout != 15*time.Second || server.ReadHeaderTimeout != 5*time.Second || server.IdleTimeout != time.Minute {
				t.Fatal(response.Code, server)
			}
		})
	}
}

type gatherFunc func() ([]*dto.MetricFamily, error)

func (f gatherFunc) Gather() ([]*dto.MetricFamily, error) { return f() }

func TestHTTPProductionLimit(t *testing.T) {
	t.Log("boundary: ten active production scrapes; expected: eleventh rejected and accepted scrapes finish after release")
	entered, release := make(chan struct{}, 10), make(chan struct{})
	g := gatherFunc(func() ([]*dto.MetricFamily, error) {
		entered <- struct{}{}
		<-release
		return nil, nil
	})
	var stop atomic.Bool
	handler := httpServer(&fakeMonitor{}, g, &stop).Handler
	responses := make(chan int, 10)
	for range 10 {
		go func() {
			r := httptest.NewRecorder()
			handler.ServeHTTP(r, httptest.NewRequestWithContext(t.Context(), "GET", "/metrics", nil))
			responses <- r.Code
		}()
	}
	for range 10 {
		<-entered
	}
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequestWithContext(t.Context(), "GET", "/metrics", nil))
	if r.Code != http.StatusServiceUnavailable || !strings.Contains(r.Body.String(), "Limit of concurrent") {
		t.Error("production limit", r.Code, r.Body.String())
	}
	close(release)
	for range 10 {
		if code := <-responses; code != http.StatusOK {
			t.Errorf("accepted scrape: %d", code)
		}
	}
}

func TestHTTPMetrics(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		fail                                         bool
		accept                                       string
		status                                       int
	}{
		{"text", "positive", "repeat cached exposition", "build identity and cached value", false, "text/plain", 200},
		{"negotiation", "corner", "unsupported encoding", "supported fallback", false, "application/unknown", 200},
		{"failure", "negative", "gather fails", "visible HTTP failure", true, "text/plain", 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			metric := prometheus.NewGauge(prometheus.GaugeOpts{Name: "cached_value", Help: "Cached test value."})
			metric.Set(42)
			var gatherer prometheus.Gatherer = commandRegistry(metric)
			if tc.fail {
				gatherer = gatherFunc(func() ([]*dto.MetricFamily, error) { return nil, errors.New("gather failed") })
			}
			var stop atomic.Bool
			server := httpServer(&fakeMonitor{}, gatherer, &stop)
			for range 2 {
				response := httptest.NewRecorder()
				request := httptest.NewRequestWithContext(t.Context(), "GET", "/metrics", nil)
				request.Header.Set("Accept", tc.accept)
				server.Handler.ServeHTTP(response, request)
				body := response.Body.String()
				if response.Code != tc.status || (!tc.fail && (!strings.Contains(body, "cached_value 42") || !strings.Contains(body, `go_link_monitor_build_info{revision="`+commit+`",version="`+version+`"} 1`))) {
					t.Fatal(response.Code, body)
				}
			}
		})
	}
}

func TestHTTPTimeoutRetainsSlots(t *testing.T) {
	t.Log("boundary/corner: ten blocked gathers time out; expected: eleventh rejected until gathers actually return")
	entered := make(chan struct{}, 10)
	release := make(chan struct{})
	finished := make(chan struct{}, 10)
	g := gatherFunc(func() ([]*dto.MetricFamily, error) {
		entered <- struct{}{}
		<-release
		defer func() { finished <- struct{}{} }()
		return nil, nil
	})
	// Shortened timeout exercises the same promhttp behavior without a ten-second test.
	handler := promhttp.HandlerFor(g, promhttp.HandlerOpts{MaxRequestsInFlight: 10, Timeout: 100 * time.Millisecond})
	responses := make(chan int, 10)
	for range 10 {
		go func() {
			r := httptest.NewRecorder()
			handler.ServeHTTP(r, httptest.NewRequestWithContext(t.Context(), "GET", "/metrics", nil))
			responses <- r.Code
		}()
	}
	for range 10 {
		<-entered
	}
	for range 10 {
		if code := <-responses; code != http.StatusServiceUnavailable {
			t.Errorf("timeout: %d", code)
		}
	}
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequestWithContext(t.Context(), "GET", "/metrics", nil))
	if r.Code != http.StatusServiceUnavailable || !strings.Contains(r.Body.String(), "Limit of concurrent") {
		t.Error("timed-out gathers released slots prematurely", r.Code, r.Body.String())
	}
	close(release)
	for range 10 {
		<-finished
	}
}
