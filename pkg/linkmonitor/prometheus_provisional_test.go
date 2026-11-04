package linkmonitor_test

import (
	"testing"

	dto "github.com/prometheus/client_model/go"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
)

// TestPrometheusBaselineProvisional: baseline_provisional is sparse, 1 only
// while the baseline is the learn-deadline one, in both the summary and the
// general collector, so alerts can tell a guessed baseline from a learned one.
func TestPrometheusBaselineProvisional(t *testing.T) {
	const name = "go_link_monitor_baseline_provisional"
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		publish                                      func(*linkmonitor.PrometheusFixture)
		provisional, ready                           bool
	}{
		{"provisional", "positive", "baseline taken at the learn deadline", "baseline_provisional 1, baseline_ready 1",
			func(f *linkmonitor.PrometheusFixture) { f.PublishProvisional(2, true) }, true, true},
		{"durable", "negative", "a learned (or loaded) baseline", "no baseline_provisional series", func(f *linkmonitor.PrometheusFixture) { f.Publish(2) }, false, true},
		{"unpublished", "boundary", "nothing published yet", "no series, baseline_ready 0", func(*linkmonitor.PrometheusFixture) {}, false, false},
		{"zero-links", "boundary", "provisional baseline of zero up links", "still provisional: zero is a count", func(f *linkmonitor.PrometheusFixture) { f.PublishProvisional(0, true) }, true, true},
		{"flag-without-baseline", "corner", "provisional state with no baseline value", "no series: nothing to be provisional about",
			func(f *linkmonitor.PrometheusFixture) { f.PublishProvisional(0, false) }, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			f := linkmonitor.NewPrometheusFixture(t)
			tc.publish(f)
			if h := f.Monitor.Snapshot().Health(); h.BaselineProvisional != tc.provisional || h.BaselineReady != tc.ready {
				t.Fatalf("%s: health %+v", tc.expectedOutcome, h)
			}
			for collector, families := range map[string]map[string]int{
				"general": familySizes(gathered(t, monitorRegistry(t, f.Monitor))),
				"summary": familySizes(gathered(t, summaryRegistry(t, f.Monitor, fixedClock(f.ScrapeTime(0))))),
			} {
				if got := families[name]; (got == 1) != tc.provisional || got > 1 {
					t.Fatalf("%s: %s collector has %d %s series", tc.expectedOutcome, collector, got, name)
				}
			}
			if tc.provisional {
				families := gathered(t, summaryRegistry(t, f.Monitor, fixedClock(f.ScrapeTime(0))))
				if v := metricValue(families[name].Metric[0]); v != 1 {
					t.Fatalf("%s: value %v", tc.expectedOutcome, v)
				}
			}
		})
	}
}

func familySizes(families map[string]*dto.MetricFamily) map[string]int {
	result := make(map[string]int, len(families))
	for name, family := range families {
		result[name] = len(family.Metric)
	}
	return result
}
