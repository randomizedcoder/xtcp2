package linkmonitor_test

import (
	"fmt"
	"strconv"
	"strings"
	"testing"

	client "github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
)

func performanceEquivalent(r *client.Registry, ports, fields int) error {
	families, err := r.Gather()
	if err != nil {
		return err
	}
	seen := make(map[string]bool, ports*fields)
	for _, family := range families {
		if family.GetName() != "go_link_monitor_ethtool_statistic" {
			continue
		}
		if family.GetType() != dto.MetricType_UNTYPED {
			return fmt.Errorf("wrong statistic type")
		}
		for _, metric := range family.Metric {
			labels := make(map[string]string, len(metric.Label))
			for _, label := range metric.Label {
				labels[label.GetName()] = label.GetValue()
			}
			port, portErr := strconv.Atoi(strings.TrimPrefix(labels["interface"], "eth"))
			field, fieldErr := strconv.Atoi(strings.TrimPrefix(labels["statistic"], "queue_"))
			key := labels["interface"] + "/" + labels["statistic"]
			if portErr != nil || fieldErr != nil || port < 1 || port > ports || field < 0 || field >= fields || metricValue(metric) != float64(field) || labels["encoding"] != "utf8" || seen[key] {
				return fmt.Errorf("unexpected metric %s", key)
			}
			seen[key] = true
		}
	}
	if len(seen) != ports*fields {
		return fmt.Errorf("statistics count %d, expected %d", len(seen), ports*fields)
	}
	return nil
}

func TestPerformanceEquivalence(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		ports, fields, expectedFields         int
		invalid                               bool
	}{
		{"ordinary", "positive", "two ports with deterministic fields", "exact values and labels", 2, 64, 64, false},
		{"empty", "boundary", "no selected statistics", "no invented samples", 2, 0, 0, false},
		{"maximum", "boundary", "one maximum schema", "all fields preserved", 1, 65536, 65536, false},
		{"mismatch", "negative", "expected schema differs", "equivalence rejected", 2, 4, 3, true},
		{"pages", "corner", "inventory crosses page boundary", "all interface labels distinct", 33, 1, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			err := performanceEquivalent(performanceRegistry(t, tc.ports, tc.fields), tc.ports, tc.expectedFields)
			if (err != nil) != tc.invalid {
				t.Fatal(tc.expected, err)
			}
		})
	}
}

func TestPerformanceMixedIdentities(t *testing.T) {
	t.Log("positive/corner: Ethernet, shared RoCE and two native ports; expected four counted links without duplicate RoCE counting")
	f := linkmonitor.NewPrometheusFixture(t)
	f.Device(1, "eth0", true)
	f.Device(2, "eth2", true)
	f.RDMA(false)
	f.PerformanceNative()
	f.Publish(4)
	if count, known := f.Monitor.Snapshot().Counts().Current(); !known || count != 4 {
		t.Fatal("incorrect mixed inventory count", count, known)
	}
	gathered(t, monitorRegistry(t, f.Monitor))
}
