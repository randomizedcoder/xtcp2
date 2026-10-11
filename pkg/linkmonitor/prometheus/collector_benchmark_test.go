package prometheus

import (
	"fmt"
	"sync"
	"testing"

	client "github.com/prometheus/client_golang/prometheus"
)

func TestPrometheusScratchTables(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		initialCap, length                           int
		wantCapAtLeast                               int
	}{
		{"empty", "boundary", "zero-label scratch", "zero length without allocation", 0, 0, 0},
		{"reuse", "positive", "shorter request reuses capacity", "length follows request and capacity is retained", 8, 3, 8},
		{"grow", "positive", "larger request grows scratch", "length and capacity cover all labels", 1, 8, 8},
		{"maximum", "boundary", "large supported label scratch", "all positions remain addressable", 32, 32, 32},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			values := make([]string, tc.initialCap)
			values = resizeStrings(values[:0], tc.length)
			seen := make([]bool, tc.initialCap)
			seen = resizeBools(seen[:0], tc.length)
			if len(values) != tc.length || len(seen) != tc.length {
				t.Fatalf("lengths values=%d seen=%d, want %d", len(values), len(seen), tc.length)
			}
			if cap(values) < tc.wantCapAtLeast || cap(seen) < tc.wantCapAtLeast {
				t.Fatalf("capacities values=%d seen=%d, want at least %d", cap(values), cap(seen), tc.wantCapAtLeast)
			}
			for i := range values {
				values[i] = "stale"
				seen[i] = true
			}
			clear(values)
			clear(seen)
			for i := range values {
				if values[i] != "" || seen[i] {
					t.Fatalf("scratch slot %d was not cleared", i)
				}
			}
		})
	}
}

func TestPrometheusDefinitionLabelPositions(t *testing.T) {
	t.Log("positive/corner: compile ordered labels and resolve every name; expected: stable positions and descriptor reuse")
	labels := []string{"interface", "statistic", "encoding"}
	d := newDefinition(client.NewDesc("go_link_monitor_test", "test", labels, nil), labels, client.UntypedValue)
	for want, name := range labels {
		if got := d.labelIndex[name]; got != want {
			t.Fatalf("label %q index=%d, want %d", name, got, want)
		}
	}
	if len(d.labelIndex) != len(labels) {
		t.Fatalf("label index size=%d, want %d", len(d.labelIndex), len(labels))
	}
}

func TestPrometheusLabelBindingTables(t *testing.T) {
	d := newDefinition(client.NewDesc("go_link_monitor_test", "test", []string{"a", "b"}, nil), []string{"a", "b"}, client.GaugeValue)
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		labels                                       [][2]string
		valid                                        bool
	}{
		{"ordered", "positive", "labels in descriptor order", "bind succeeds", [][2]string{{"a", "one"}, {"b", "two"}}, true},
		{"reordered", "corner", "labels arrive in reverse order", "bind succeeds with canonical values", [][2]string{{"b", "two"}, {"a", "one"}}, true},
		{"missing", "negative", "one required label is absent", "bind is rejected", [][2]string{{"a", "one"}}, false},
		{"duplicate", "negative", "one label is repeated", "bind is rejected", [][2]string{{"a", "one"}, {"a", "again"}, {"b", "two"}}, false},
		{"unknown", "negative", "unregistered label is present", "bind is rejected", [][2]string{{"a", "one"}, {"b", "two"}, {"c", "three"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			e := emitter{}
			valid := e.bindLabels(d, func(visit func(string, string) bool) {
				for _, label := range tc.labels {
					if !visit(label[0], label[1]) {
						return
					}
				}
			})
			if valid != tc.valid {
				t.Fatalf("valid=%t, want %t", valid, tc.valid)
			}
			if tc.valid && (e.values[0] != "one" || e.values[1] != "two") {
				t.Fatalf("canonical values=%q, want [one two]", e.values)
			}
		})
	}
}

func BenchmarkPrometheusLabelPlan(b *testing.B) {
	for _, count := range []int{0, 1, 3, 8, 32} {
		b.Run(fmt.Sprintf("labels=%d", count), func(b *testing.B) {
			labels := make([]string, count)
			for i := range labels {
				labels[i] = fmt.Sprintf("label_%d", i)
			}
			desc := client.NewDesc("go_link_monitor_benchmark", "benchmark", labels, nil)
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				_ = newDefinition(desc, labels, client.GaugeValue)
			}
		})
	}
}

func BenchmarkPrometheusLabelScratch(b *testing.B) {
	for _, count := range []int{0, 1, 3, 8, 32} {
		b.Run(fmt.Sprintf("labels=%d", count), func(b *testing.B) {
			b.ReportAllocs()
			values := make([]string, 0, count)
			seen := make([]bool, 0, count)
			b.ResetTimer()
			for b.Loop() {
				values = resizeStrings(values, count)
				seen = resizeBools(seen, count)
				clear(values)
				clear(seen)
			}
		})
	}
}

func BenchmarkPrometheusLabelScratchPool(b *testing.B) {
	for _, count := range []int{0, 1, 3, 8, 32} {
		b.Run(fmt.Sprintf("labels=%d", count), func(b *testing.B) {
			type scratch struct {
				values []string
				seen   []bool
			}
			pool := sync.Pool{New: func() any { return new(scratch) }}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				s := pool.Get().(*scratch)
				s.values = resizeStrings(s.values, count)
				s.seen = resizeBools(s.seen, count)
				clear(s.values)
				clear(s.seen)
				pool.Put(s)
			}
		})
	}
}
