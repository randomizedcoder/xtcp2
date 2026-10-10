package linkmonitor_test

import (
	"slices"
	"testing"
)

func TestPerformancePercentiles(t *testing.T) {
	for _, tc := range []struct {
		name, description, expected string
		values, want                []int64
	}{
		{"empty", "boundary: no completed observations", "absent percentiles", nil, nil},
		{"one", "boundary: one observation", "same value for every percentile", []int64{7}, []int64{7, 7, 7, 7}},
		{"unordered", "positive: unordered observations", "nearest-rank p50/p95/p99/max", []int64{4, 1, 3, 2}, []int64{2, 4, 4, 4}},
		{"duplicates", "corner: equal observations", "duplicates retained", []int64{2, 2, 2}, []int64{2, 2, 2, 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s; expected: %s", tc.description, tc.expected)
			if got := performancePercentiles(tc.values); !slices.Equal(got, tc.want) {
				t.Fatal(tc.expected, got)
			}
		})
	}
}

func TestPerformanceModes(t *testing.T) {
	for _, tc := range []struct{ mode, category, description, expected string }{
		{"", "positive", "concurrent cached scrape and events", "coherent output and final count"},
		{"one-stuck", "negative", "one uncancellable worker", "remaining workers progress; bounded shutdown after release"},
		{"all-stuck", "boundary", "all four workers blocked", "events publish with no replacement workers"},
		{"failed-resync", "negative", "authoritative dump fails", "previous inventory retained and final recovery succeeds"},
		{"schema-churn", "corner", "alternating selected field count", "no duplicate metric families"},
		{"rename", "corner", "interface changes name", "same counted identity"},
		{"hotplug", "corner", "hardware replaced at same index", "fresh generation and final convergence"},
		{"slow-client", "negative", "HTTP writer applies backpressure", "collection progresses during scrape"},
		{"disconnected", "negative", "HTTP writes fail", "collection and subsequent Gather remain usable"},
		{"cadence", "positive", "15-second scrape cadence", "cancellation interrupts cadence wait"},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			runPerformanceScenario(t, performanceScenario{Ports: 2, Fields: 4, Rate: 1000, Scrapers: 1, WarmupMS: 5, MeasureMS: 50, Mode: tc.mode})
		})
	}
}
