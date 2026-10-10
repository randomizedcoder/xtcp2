package linkmonitor_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	client "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
)

type performanceScenario struct {
	Ports, Fields, Rate, Scrapers, Burst int
	DelayMS, WarmupMS, MeasureMS         int
	Mode                                 string
}

func performanceWait(t *testing.T, m *linkmonitor.Monitor, count uint64) {
	t.Helper()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if current, known := m.Snapshot().Counts().Current(); known && current == count {
			return
		}
		select {
		case <-timer.C:
			t.Fatal("synthetic source failed to converge")
		case <-tick.C:
		}
	}
}

type performanceScrapeResult struct {
	Times           []int64
	Bytes, Failures int64
}

type performanceWriter struct {
	*httptest.ResponseRecorder
	mode string
}

func (w performanceWriter) Write(data []byte) (int, error) {
	if w.mode == "slow-client" {
		time.Sleep(time.Millisecond)
	}
	if w.mode == "disconnected" {
		return 0, errors.New("scripted disconnected writer")
	}
	return w.ResponseRecorder.Write(data)
}

func performanceScrapes(ctx context.Context, r *client.Registry, count int, mode string) func() performanceScrapeResult {
	var wg sync.WaitGroup
	var mu sync.Mutex
	var result performanceScrapeResult
	h := promhttp.HandlerFor(r, promhttp.HandlerOpts{})
	for range count {
		wg.Go(func() {
			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			for ctx.Err() == nil {
				start := time.Now()
				w := httptest.NewRecorder()
				h.ServeHTTP(performanceWriter{ResponseRecorder: w, mode: mode}, req)
				mu.Lock()
				if len(result.Times) < 100000 {
					result.Times = append(result.Times, time.Since(start).Nanoseconds())
				}
				result.Bytes += int64(w.Body.Len())
				if w.Code != http.StatusOK {
					result.Failures++
				}
				mu.Unlock()
				if mode == "cadence" {
					select {
					case <-ctx.Done():
						return
					case <-time.After(15 * time.Second):
					}
				}
			}
		})
	}
	return func() performanceScrapeResult { wg.Wait(); return result }
}

func performancePercentiles(values []int64) []int64 {
	if len(values) == 0 {
		return nil
	}
	slices.Sort(values)
	return []int64{values[(len(values)*50+99)/100-1], values[(len(values)*95+99)/100-1], values[(len(values)*99+99)/100-1], values[len(values)-1]}
}

func TestPerformanceScenario(t *testing.T) {
	cfg := performanceScenario{Ports: 2, Fields: 4, Rate: 100, Scrapers: 1, WarmupMS: 10, MeasureMS: 50}
	if raw := os.Getenv("LINKMONITOR_BENCH_SCENARIO"); raw != "" {
		if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
			t.Fatal(err)
		}
	}
	if cfg.Ports < 1 || cfg.Ports > 256 || cfg.Fields < 0 || cfg.Fields > 65536 || cfg.Ports*cfg.Fields > 256*1024 || cfg.Rate < 0 || cfg.Rate > 10000 || cfg.Scrapers < 0 || cfg.Scrapers > 10 || cfg.MeasureMS <= 0 || cfg.MeasureMS > 120000 || cfg.WarmupMS < 0 || cfg.WarmupMS > 30000 || cfg.DelayMS < 0 || cfg.DelayMS > 5000 || cfg.Burst < 0 || cfg.Burst > 8192 {
		t.Fatal("invalid bounded scenario")
	}
	runPerformanceScenario(t, cfg)
}

func runPerformanceScenario(t *testing.T, cfg performanceScenario) {
	t.Helper()
	m, source := linkmonitor.NewPerformanceMonitor(t, cfg.Ports, cfg.Fields, time.Duration(cfg.DelayMS)*time.Millisecond)
	source.Configure(cfg.Mode)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()
	defer func() {
		source.Release()
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
	}()
	performanceWait(t, m, uint64(cfg.Ports))
	r := monitorRegistry(t, m)
	warmup := performanceWarmup(m, cfg)
	t.Logf("warmup population: %v", warmup)
	scrapeCtx, stopScrapes := context.WithCancel(ctx)
	join := performanceScrapes(scrapeCtx, r, cfg.Scrapers, cfg.Mode)
	defer func() { stopScrapes(); join() }()
	eventStart := time.Now()
	latencies, delivered, dropped := performanceEvents(t, cfg, m, source)
	eventWindow := time.Since(eventStart)
	t.Logf("event measurement completed in %s; joining scrapers", eventWindow)
	stopScrapes()
	scrapes := join()
	if scrapes.Failures != 0 {
		t.Fatal("HTTP scrape failures", scrapes.Failures)
	}
	start := time.Now()
	source.Recover()
	for i := range cfg.Ports {
		source.Change(i, true)
	}
	beforeResync, _ := m.Snapshot().LastSuccessfulResync()
	if err := m.RequestResync(); err != nil {
		t.Fatal(err)
	}
	performanceRecovery(t, m, beforeResync, uint64(cfg.Ports))
	recovery := time.Since(start)
	t.Logf("fresh reconciliation recovered in %s", recovery)
	gathered(t, r)
	performancePresentSamples(t, m.Snapshot(), cfg.Fields)
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	fds, fdErr := os.ReadDir("/proc/self/fd")
	threads, threadErr := os.ReadDir("/proc/self/task")
	if fdErr != nil || threadErr != nil {
		t.Fatal("process resource sampling", fdErr, threadErr)
	}
	resyncs := make(map[string]uint64)
	m.Snapshot().RangeResyncs(func(reason, result string, count uint64) bool { resyncs[reason+"/"+result] = count; return true })
	calls, workers := source.Work()
	queryNS, dispatchNS := source.Timings()
	if workers > 4 {
		t.Fatal("unbounded worker growth", workers)
	}
	report := map[string]any{"scenario": cfg, "events": delivered, "producer_drops": dropped,
		"warmup": warmup, "present_samples_after": performanceSampleCount(m.Snapshot()),
		"event_window_ns": eventWindow.Nanoseconds(), "latency_probe_events": len(latencies),
		"publication_ns_p50_p95_p99_max": performancePercentiles(latencies), "scrape_ns_p50_p95_p99_max": performancePercentiles(scrapes.Times), "scrape_bytes": scrapes.Bytes,
		"recovery_ns": recovery.Nanoseconds(), "heap_alloc_bytes": memory.HeapAlloc, "goroutines": runtime.NumGoroutine(),
		"source_queries": calls, "peak_source_workers": workers,
		"source_query_ns_total": queryNS, "dispatch_to_worker_ns_total": dispatchNS, "scheduler_pending_ns": "unavailable",
		"fd_count": len(fds), "os_threads": len(threads), "resync_outcomes": resyncs, "producer_queue_peak": source.QueuePeak(), "internal_queue_peak": "unavailable; boundedness covered by scheduler tests",
		"latency_scope": "sampled source delivery to observed publication; excludes provider/kernel", "provider_kernel": "unavailable"}
	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Printf("PERFORMANCE_RESULT %s\n", encoded)
}

func performanceRecovery(t *testing.T, m *linkmonitor.Monitor, before time.Time, ports uint64) {
	t.Helper()
	// Recovery duration is a measurement, not a ten-second latency promise.
	// Large cold schemas can occupy the owner well after producers stop. Keep
	// a finite bound within the runner's ten-minute scenario deadline, and still
	// require a fresh complete reconciliation with the exact expected count.
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		snapshot := m.Snapshot()
		at, complete := snapshot.LastSuccessfulResync()
		count, known := snapshot.Counts().Current()
		if complete && at.After(before) && known && count == ports {
			return
		}
		time.Sleep(time.Millisecond)
	}
	snapshot := m.Snapshot()
	count, known := snapshot.Counts().Current()
	at, complete := snapshot.LastSuccessfulResync()
	t.Fatalf("fresh complete reconciliation did not recover expected count: got=%d known=%t want=%d before=%s last=%s complete=%t health=%+v", count, known, ports, before, at, complete, snapshot.Health())
}

func performanceSampleCount(snapshot linkmonitor.Snapshot) int {
	count := 0
	snapshot.RangeSamples(func(s linkmonitor.SampleView) bool {
		if s.DescriptorKey() == "ethtool_statistic" || s.DescriptorKey() == "phy_statistic" {
			count++
		}
		return true
	})
	return count
}

func performanceWarmup(m *linkmonitor.Monitor, cfg performanceScenario) map[string]any {
	time.Sleep(time.Duration(cfg.WarmupMS) * time.Millisecond)
	present, expected := performanceSampleCount(m.Snapshot()), cfg.Ports*cfg.Fields*2
	outcome := "fully-populated"
	if present != expected {
		outcome = "capacity-limited"
	}
	if cfg.Mode != "" || cfg.DelayMS != 0 {
		outcome = "injected-source-behavior"
	}
	if cfg.WarmupMS < 5000 {
		outcome = "smoke-only"
	}
	return map[string]any{"outcome": outcome, "present": present, "expected": expected,
		"steady_state_comparable": outcome == "fully-populated"}
}

func performanceEvents(t *testing.T, cfg performanceScenario, m *linkmonitor.Monitor, source *linkmonitor.PerformanceSource) ([]int64, int, int) {
	t.Helper()
	delivered, dropped := 0, 0
	for i := range cfg.Burst {
		index := 1 + i%max(1, cfg.Ports-1)
		if cfg.Ports == 1 {
			index = 0
		}
		if source.Change(index, (i/max(1, cfg.Ports-1))%2 == 0) {
			delivered++
		} else {
			dropped++
		}
	}
	if cfg.Burst != 0 {
		if err := m.RequestResync(); err != nil {
			t.Fatal(err)
		}
	}
	var latencies []int64
	if cfg.Rate == 0 {
		time.Sleep(time.Duration(cfg.MeasureMS) * time.Millisecond)
		return latencies, delivered, dropped
	}
	end := time.Now().Add(time.Duration(cfg.MeasureMS) * time.Millisecond)
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	sequence := 0
	for time.Now().Before(end) {
		<-tick.C
		batch := cfg.Rate / 1000
		if cfg.Rate == 1 && sequence%1000 == 0 {
			batch = 1
		}
		if cfg.Rate == 100 && sequence%10 == 0 {
			batch = 1
		}
		for range batch {
			index := 1 + delivered%max(1, cfg.Ports-1)
			if cfg.Ports == 1 {
				index = 0
			}
			if source.Change(index, (delivered/max(1, cfg.Ports-1))%2 == 0) {
				delivered++
			} else {
				dropped++
			}
		}
		if batch > 0 && sequence%100 == 0 {
			latencies = append(latencies, performanceProbe(t, m, source))
		}
		if sequence%1000 == 0 && (cfg.Mode == "rename" || cfg.Mode == "hotplug") {
			source.Churn(sequence)
			if err := m.RequestResync(); err != nil {
				t.Fatal(err)
			}
		}
		sequence++
	}
	return latencies, delivered, dropped
}

func performanceProbe(t *testing.T, m *linkmonitor.Monitor, source *linkmonitor.PerformanceSource) int64 {
	t.Helper()
	var before uint64
	var target bool
	m.Snapshot().RangeDevices(func(d linkmonitor.DeviceView) bool {
		if d.Identity() == "netdev:1" {
			up, _ := d.Up()
			target = !up
			a, b := d.ObservedTransitions()
			before = a + b
		}
		return true
	})
	start := time.Now()
	if !source.Change(0, target) {
		if err := m.RequestResync(); err != nil {
			t.Fatal(err)
		}
	}
	for time.Since(start) < 5*time.Minute {
		matched := false
		m.Snapshot().RangeDevices(func(d linkmonitor.DeviceView) bool {
			a, b := d.ObservedTransitions()
			up, known := d.Up()
			if d.Identity() == "netdev:1" && known && up == target && a+b > before {
				matched = true
			}
			return !matched
		})
		if matched {
			return time.Since(start).Nanoseconds()
		}
		runtime.Gosched()
	}
	t.Fatal("latency probe did not publish its transition")
	return 0
}
