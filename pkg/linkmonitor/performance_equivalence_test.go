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

func TestPerformanceRealisticFleet(t *testing.T) {
	t.Log("positive/realistic: two 100GE Ethernet and two 400GE RoCEv2 links; expected four links, 1,024 driver statistics, and two RDMA ports")
	f := linkmonitor.NewPrometheusFixture(t)
	f.PerformanceRealisticFleet()
	f.Publish(4)
	families := gathered(t, monitorRegistry(t, f.Monitor))
	if got := metricValue(families["go_link_monitor_up_links"].Metric[0]); got != 4 {
		t.Fatalf("up_links = %v, want 4", got)
	}
	if got := len(families["go_link_monitor_ethtool_statistic"].Metric); got != 4*256 {
		t.Fatalf("ethtool statistic series = %d, want 1024", got)
	}
	if got := len(families["go_link_monitor_rdma_port_up"].Metric); got != 2 {
		t.Fatalf("RoCE port series = %d, want 2", got)
	}
	if got := len(families["go_link_monitor_interface_speed_bits_per_second"].Metric); got != 4 {
		t.Fatalf("speed series = %d, want 4", got)
	}
	classification := families["go_link_monitor_interface_classification"]
	for _, name := range []string{"docker0", "br-0123456789ab"} {
		foundExcluded := false
		for _, metric := range classification.Metric {
			labels := metricLabels(metric)
			if labels["interface"] == name && labels["status"] == "excluded" && metricValue(metric) == 1 {
				foundExcluded = true
			}
		}
		if !foundExcluded {
			t.Fatalf("missing excluded classification for %s", name)
		}
	}
	if _, ok := families["go_link_monitor_interface_up"]; !ok {
		t.Fatal("physical interface metrics unexpectedly absent")
	}
	for _, metric := range families["go_link_monitor_interface_up"].Metric {
		if labels := metricLabels(metric); labels["interface"] == "docker0" || labels["interface"] == "br-0123456789ab" {
			t.Fatal("Docker bridge exported as physical interface", labels["interface"])
		}
	}
}

func TestPerformanceLocalMachine(t *testing.T) {
	t.Log("positive/boundary: three local Ethernet interfaces with zero rtnetlink counters and unknown negotiated settings")
	f := linkmonitor.NewPrometheusFixture(t)
	f.PerformanceLocalMachine()
	f.Publish(3)
	families := gathered(t, monitorRegistry(t, f.Monitor))
	if got := metricValue(families["go_link_monitor_up_links"].Metric[0]); got != 3 {
		t.Fatalf("up_links = %v, want 3", got)
	}
	traffic := families["go_link_monitor_interface_receive_packets_total"]
	if traffic == nil || len(traffic.Metric) != 3 {
		t.Fatalf("receive packet series = %d, want 3", len(traffic.Metric))
	}
	for _, metric := range traffic.Metric {
		if metricValue(metric) != 0 {
			t.Fatalf("zero receive counter = %v", metricValue(metric))
		}
	}
	if _, ok := families["go_link_monitor_interface_speed_bits_per_second"]; ok {
		t.Fatal("speed emitted without negotiated-speed evidence")
	}
	if _, ok := families["go_link_monitor_rdma_port_up"]; ok {
		t.Fatal("RDMA emitted for Ethernet-only fixture")
	}
}

func metricLabels(metric *dto.Metric) map[string]string {
	labels := make(map[string]string, len(metric.Label))
	for _, label := range metric.Label {
		labels[label.GetName()] = label.GetValue()
	}
	return labels
}
