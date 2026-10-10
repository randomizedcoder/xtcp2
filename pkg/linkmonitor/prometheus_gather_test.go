package linkmonitor_test

import (
	"math"
	"sync"
	"testing"
	"time"

	client "github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	monitorprom "github.com/randomizedcoder/xtcp2/pkg/linkmonitor/prometheus"
)

func monitorRegistry(t testing.TB, m *linkmonitor.Monitor) *client.Registry {
	t.Helper()
	r := client.NewPedanticRegistry()
	if err := r.Register(monitorprom.NewCollector(m)); err != nil {
		t.Fatal(err)
	}
	return r
}

func gathered(t testing.TB, r *client.Registry) map[string]*dto.MetricFamily {
	t.Helper()
	families, err := r.Gather()
	if err != nil {
		t.Fatal(err)
	}
	result := make(map[string]*dto.MetricFamily, len(families))
	for _, family := range families {
		result[family.GetName()] = family
	}
	return result
}

func metricValue(m *dto.Metric) float64 {
	if m.Gauge != nil {
		return m.Gauge.GetValue()
	}
	if m.Counter != nil {
		return m.Counter.GetValue()
	}
	return m.Untyped.GetValue()
}

func TestPrometheusNumericContract(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		number                                       model.Number
		want                                         float64
	}{
		{"zero", "positive", "known zero", "export zero", model.Unsigned(0), 0},
		{"32 bit", "boundary", "maximum 32-bit integer", "exact float", model.Unsigned(math.MaxUint32), math.MaxUint32},
		{"53 bit", "boundary", "integer above float precision", "round only at exposition", model.Unsigned(1<<53 + 1), float64(uint64(1<<53 + 1))},
		{"64 bit", "boundary", "maximum unsigned integer", "finite rounded float", model.Unsigned(math.MaxUint64), float64(uint64(math.MaxUint64))},
		{"sentinel", "corner", "negative TCP sentinel", "signed untyped minus one", model.Signed(-1), -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			f := linkmonitor.NewPrometheusFixture(t)
			if err := f.Samples(0, model.CollectorNetstat, model.Sample{Descriptor: "netstat_Tcp_MaxConn", Kind: model.SampleUntyped, Number: tc.number}); err != nil {
				t.Fatal(err)
			}
			f.Publish(1)
			families := gathered(t, monitorRegistry(t, f.Monitor))
			family := families["go_link_monitor_netstat_Tcp_MaxConn"]
			if family == nil || family.GetType() != dto.MetricType_UNTYPED || len(family.Metric) != 1 {
				t.Fatal("missing/untyped family")
			}
			if m := family.Metric[0]; metricValue(m) != tc.want || len(m.Label) != 0 || m.TimestampMs != nil {
				t.Fatalf("unexpected sample %v", m)
			}
			if v := metricValue(families["go_link_monitor_up_links_delta"].Metric[0]); v != -1 {
				t.Fatalf("delta %v", v)
			}
		})
	}
}

func TestPrometheusFamilies(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome, key, metric string
		kind                                                      model.CollectorKind
		typeOf                                                    model.SampleKind
		want                                                      dto.MetricType
	}{
		{"traffic", "positive", "raw cumulative traffic", "counter without accumulation", "go_link_monitor_interface_receive_bytes_total", "go_link_monitor_interface_receive_bytes_total", model.CollectorNetdev, model.SampleCounter, dto.MetricType_COUNTER},
		{"carrier", "positive", "carrier state", "gauge", "go_link_monitor_interface_carrier", "go_link_monitor_interface_carrier", model.CollectorCarrier, model.SampleGauge, dto.MetricType_GAUGE},
		{"settings", "positive", "negotiated speed", "gauge in bits per second", "go_link_monitor_interface_speed_bits_per_second", "go_link_monitor_interface_speed_bits_per_second", model.CollectorSettings, model.SampleGauge, dto.MetricType_GAUGE},
		{"identity", "positive", "known driver identity", "boolean gauge", "go_link_monitor_interface_driver_known", "go_link_monitor_interface_driver_known", model.CollectorInventory, model.SampleGauge, dto.MetricType_GAUGE},
		{"channels", "positive", "channel count", "channel gauge", "go_link_monitor_interface_channels", "go_link_monitor_interface_channels", model.CollectorChannels, model.SampleGauge, dto.MetricType_GAUGE},
		{"rings", "positive", "ring entries", "entry gauge", "go_link_monitor_interface_ring_entries", "go_link_monitor_interface_ring_entries", model.CollectorRings, model.SampleGauge, dto.MetricType_GAUGE},
		{"driver", "positive", "abbreviated driver key", "contracted untyped name", "ethtool_statistic", "go_link_monitor_ethtool_statistic", model.CollectorDriver, model.SampleUntyped, dto.MetricType_UNTYPED},
		{"phy", "positive", "abbreviated PHY key", "contracted untyped name", "phy_statistic", "go_link_monitor_phy_statistic", model.CollectorPHY, model.SampleUntyped, dto.MetricType_UNTYPED},
		{"rdma", "positive", "abbreviated RDMA key", "contracted gauge name", "rdma_port_up", "go_link_monitor_rdma_port_up", model.CollectorRDMAState, model.SampleGauge, dto.MetricType_GAUGE},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			f := linkmonitor.NewPrometheusFixture(t)
			f.Device(1, "eth0", true)
			labels := familyLabels(tc.kind)
			value := uint64(42)
			if tc.kind == model.CollectorInventory || tc.kind == model.CollectorCarrier || tc.kind == model.CollectorRDMAState {
				value = 1
			}
			if err := f.Samples(1, tc.kind, model.Sample{Descriptor: tc.key, Kind: tc.typeOf, Number: model.Unsigned(value), Labels: labels}); err != nil {
				t.Fatal(err)
			}
			f.Publish(1)
			r := monitorRegistry(t, f.Monitor)
			for range 2 {
				family := gathered(t, r)[tc.metric]
				if family == nil || family.GetType() != tc.want || len(family.Metric) != 1 {
					t.Fatal("wrong family")
				}
				m := family.Metric[0]
				if metricValue(m) != float64(value) || len(m.Label) != len(labels)+1 {
					t.Fatalf("wrong sample %v", m)
				}
				wantLabels := append([]model.Label{{Name: "interface", Value: "eth0"}}, labels...)
				for _, wanted := range wantLabels {
					found := false
					for _, actual := range m.Label {
						if actual.GetName() == wanted.Name && actual.GetValue() == wanted.Value {
							found = true
						}
					}
					if !found {
						t.Fatalf("missing label %+v in %v", wanted, m)
					}
				}
			}
		})
	}
}

func familyLabels(kind model.CollectorKind) []model.Label {
	switch kind {
	case model.CollectorDriver, model.CollectorPHY:
		return []model.Label{{Name: "statistic", Value: "rx_queue_0_packets"}, {Name: "encoding", Value: "utf8"}}
	case model.CollectorChannels, model.CollectorRings:
		return []model.Label{{Name: "kind", Value: "rx"}}
	case model.CollectorRDMAState:
		return []model.Label{{Name: "device", Value: "mlx5_0"}, {Name: "port", Value: "1"}}
	default:
		return nil
	}
}

func TestPrometheusExpiryAndRename(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		at                                           time.Duration
		present                                      bool
	}{
		{"before", "boundary", "one ns before expiry", "sample remains", 45*time.Second - time.Nanosecond, true},
		{"at", "boundary", "exact expiry", "sample omitted", 45 * time.Second, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			f := linkmonitor.NewPrometheusFixture(t)
			f.Device(1, "old", true)
			if err := f.Samples(1, model.CollectorDriver, model.Sample{Descriptor: "ethtool_statistic", Kind: model.SampleUntyped, Number: model.Unsigned(9)}); err != nil {
				t.Fatal(err)
			}
			f.Publish(1)
			old := f.Monitor.Snapshot()
			f.Device(1, "new", true)
			f.Expire(tc.at)
			f.Publish(1)
			families := gathered(t, monitorRegistry(t, f.Monitor))
			family := families["go_link_monitor_ethtool_statistic"]
			if (family != nil) != tc.present {
				t.Fatal("expiry mismatch")
			}
			if family != nil && family.Metric[0].Label[0].GetValue() != "new" {
				t.Fatal("old label retained")
			}
			count := 0
			old.RangeSamples(func(linkmonitor.SampleView) bool { count++; return true })
			if count != 1 {
				t.Fatal("old root mutated")
			}
		})
	}
}

func TestPrometheusConcurrentGather(t *testing.T) {
	t.Log("concurrency: ten gathers while owner publishes counts and schemas; expected: coherent roots and race-free samples")
	f := linkmonitor.NewPrometheusFixture(t)
	f.Device(1, "eth0", true)
	f.Publish(1)
	r := monitorRegistry(t, f.Monitor)
	var group sync.WaitGroup
	for range 10 {
		group.Go(func() {
			for range 50 {
				values := gathered(t, r)
				current := metricValue(values["go_link_monitor_up_links"].Metric[0])
				expected := metricValue(values["go_link_monitor_baseline_up_links"].Metric[0])
				delta := metricValue(values["go_link_monitor_up_links_delta"].Metric[0])
				if current-expected != delta {
					t.Error("mixed snapshot roots")
				}
			}
		})
	}
	for i := range 100 {
		f.Device(1, "eth0", i%2 == 0)
		key := "netstat_Tcp_ActiveOpens"
		if i%2 == 0 {
			key = "netstat_MPTcpExt_MPCapableSYNRX"
		}
		if err := f.Samples(0, model.CollectorNetstat, model.Sample{Descriptor: key, Kind: model.SampleUntyped, Number: model.Unsigned(uint64(i))}); err != nil {
			t.Fatal(err)
		}
		f.Publish(uint64(i % 3))
	}
	group.Wait()
}
