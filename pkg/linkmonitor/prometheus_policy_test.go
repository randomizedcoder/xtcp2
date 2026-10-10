package linkmonitor_test

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
)

func TestPrometheusRDMAScope(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		native                                       bool
		links                                        float64
	}{
		{"native", "positive", "two native InfiniBand ports", "two canonical links with transport duplex", true, 2},
		{"roce", "corner", "two RoCE ports sharing Ethernet", "one link and two port series", false, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			f := linkmonitor.NewPrometheusFixture(t)
			f.RDMA(tc.native)
			f.Publish(uint64(tc.links))
			families := gathered(t, monitorRegistry(t, f.Monitor))
			if metricValue(families["go_link_monitor_up_links"].Metric[0]) != tc.links {
				t.Fatal("duplicate or missing link count")
			}
			port := families["go_link_monitor_rdma_port_up"]
			if port == nil || len(port.Metric) != 2 {
				t.Fatal("port identities merged")
			}
			for _, metric := range port.Metric {
				if len(metric.Label) != 3 || metricValue(metric) != 1 {
					t.Fatal("wrong RDMA port labels/state")
				}
			}
			duplex := families["go_link_monitor_interface_duplex_info"]
			if tc.native {
				if duplex == nil || len(duplex.Metric) != 2 {
					t.Fatal("native duplex missing")
				}
				for _, metric := range duplex.Metric {
					for _, label := range metric.Label {
						if label.GetName() == "source" && label.GetValue() != "transport" {
							t.Fatal("fabricated ethtool evidence")
						}
					}
				}
			} else if duplex != nil {
				t.Fatal("RDMA fabricated Ethernet duplex")
			}
		})
	}
}

func TestPrometheusExceptionAndOneHot(t *testing.T) {
	t.Log("positive/corner: matched and missing selectors; expected: complete one-hot states without changing unknown raw checks")
	f := linkmonitor.NewPrometheusFixture(t)
	f.Device(1, "eth0", true)
	f.Exceptions("eth0", "missing")
	f.Publish(1)
	families := gathered(t, monitorRegistry(t, f.Monitor))
	for _, name := range []string{"interface_check", "collector_support", "max_speed_exception_match", "interface_classification"} {
		family := families["go_link_monitor_"+name]
		if family == nil {
			t.Fatalf("missing %s", name)
		}
		groups := make(map[string]float64)
		counts := make(map[string]int)
		for _, metric := range family.Metric {
			key := ""
			for _, label := range metric.Label {
				if label.GetName() != "status" {
					key += label.GetName() + "=" + label.GetValue() + ";"
				}
			}
			groups[key] += metricValue(metric)
			counts[key]++
		}
		want := 4
		if name == "max_speed_exception_match" || name == "interface_classification" {
			want = 3
		}
		for key, value := range groups {
			if value != 1 || counts[key] != want {
				t.Fatalf("%s/%s invalid one-hot %v/%d", name, key, value, counts[key])
			}
		}
	}
}

func TestPrometheusBeforeInventoryAndDeletion(t *testing.T) {
	t.Log("boundary/corner: unstarted monitor, known-empty inventory and deletion; expected: unknown counts omitted, then known zero and no deleted series")
	f := linkmonitor.NewPrometheusFixture(t)
	r := monitorRegistry(t, f.Monitor)
	if gathered(t, r)["go_link_monitor_up_links"] != nil {
		t.Fatal("unknown count fabricated")
	}
	f.Publish(0)
	if metricValue(gathered(t, r)["go_link_monitor_up_links"].Metric[0]) != 0 {
		t.Fatal("known zero omitted")
	}
	f.Device(1, "eth0", true)
	f.Publish(1)
	if gathered(t, r)["go_link_monitor_interface_up"] == nil {
		t.Fatal("device missing")
	}
	f.Remove(1)
	f.Publish(1)
	if gathered(t, r)["go_link_monitor_interface_up"] != nil {
		t.Fatal("deleted series retained")
	}
}
