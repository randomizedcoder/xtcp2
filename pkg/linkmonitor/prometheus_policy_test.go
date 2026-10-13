package linkmonitor_test

import (
	"testing"

	dto "github.com/prometheus/client_model/go"

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

func TestPrometheusExpectedSpeedException(t *testing.T) {
	const absent = -1.0
	for _, tc := range []struct {
		name, category, description, expected string
		selector                              string
		up                                    bool
		activeMbps, maximumMbps               uint64
		check                                 string
		exception, expectedBits               float64
	}{
		{"at expected", "positive", "1G link on a 10G port, exception eth0=1G", "max_speed fails, exempt, expected speed 1e9",
			"eth0=1G", true, 1000, 10000, "fail", 1, 1e9},
		{"below expected", "negative", "the same port fallen to 100M", "max_speed fails and is no longer exempt",
			"eth0=1G", true, 100, 10000, "fail", 0, 1e9},
		{"above expected", "negative", "2.5G, above the expected 1G but under the 10G maximum", "not exempt: the expected speed is stale",
			"eth0=1G", true, 2500, 10000, "fail", 0, 1e9},
		{"at maximum", "boundary", "link at its 10G maximum, exception eth0=10G", "max_speed passes, exempt",
			"eth0=10G", true, 10000, 10000, "pass", 1, 1e10},
		{"maximum not expected", "corner", "link at its 10G maximum, exception eth0=1G", "max_speed passes, so nothing alerts; not exempt",
			"eth0=1G", true, 10000, 10000, "pass", 0, 1e9},
		{"Mb/s granularity", "boundary", "2.5G link, exception eth0=2500M", "exempt at exactly 2.5e9",
			"eth0=2500M", true, 2500, 10000, "fail", 1, 2.5e9},
		{"bare name", "positive", "100M link, bare exception eth0", "exempt at any speed, no expected-speed series (unchanged)",
			"eth0", true, 100, 10000, "fail", 1, absent},
		{"down", "corner", "link down, exception eth0=1G", "max_speed not_applicable; no measured speed, so not exempt",
			"eth0=1G", false, 1000, 10000, "not_applicable", 0, 1e9},
		{"down bare", "corner", "link down, bare exception eth0", "exempt as before",
			"eth0", false, 1000, 10000, "not_applicable", 1, absent},
		{"unmatched", "negative", "exception for an interface that is not present", "no exemption, no expected-speed series",
			"eth9=1G", true, 1000, 10000, "fail", 0, absent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			f := linkmonitor.NewPrometheusFixture(t)
			f.Device(1, "eth0", tc.up)
			f.Speed(1, tc.activeMbps, tc.maximumMbps)
			f.Exceptions(tc.selector)
			f.Publish(1)
			families := gathered(t, monitorRegistry(t, f.Monitor))
			check := ""
			for _, metric := range families["go_link_monitor_interface_check"].GetMetric() {
				if labelValue(metric, "check") == "max_speed" && metricValue(metric) == 1 {
					check = labelValue(metric, "status")
				}
			}
			exception := seriesValue(families["go_link_monitor_interface_max_speed_exception"], absent)
			expectedBits := seriesValue(families["go_link_monitor_interface_expected_speed_bits_per_second"], absent)
			if check != tc.check || exception != tc.exception || expectedBits != tc.expectedBits {
				t.Fatalf("got max_speed %s, exception %v, expected speed %v; want %s, %v, %v",
					check, exception, expectedBits, tc.check, tc.exception, tc.expectedBits)
			}
		})
	}
}

func labelValue(metric *dto.Metric, name string) string {
	for _, label := range metric.GetLabel() {
		if label.GetName() == name {
			return label.GetValue()
		}
	}
	return ""
}

// seriesValue returns the one eth0 series of a family, or absent.
func seriesValue(family *dto.MetricFamily, absent float64) float64 {
	for _, metric := range family.GetMetric() {
		if labelValue(metric, "interface") == "eth0" {
			return metricValue(metric)
		}
	}
	return absent
}
