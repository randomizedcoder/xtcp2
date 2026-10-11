package linkmonitor

import (
	"fmt"
	"regexp"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func BenchmarkPerformanceFilters(b *testing.B) {
	for b.Loop() {
		_ = regexp.MustCompile("^(rx|tx)_queue_[0-9]+$")
	}
}

// PerformanceSamples constructs deterministic driver statistics outside timing.
// This bridge exists only in the test binary, alongside PrometheusFixture.
func PerformanceSamples(count int) []model.Sample {
	samples := make([]model.Sample, count)
	for i := range samples {
		samples[i] = model.Sample{Descriptor: "ethtool_statistic", Kind: model.SampleUntyped,
			Number: model.Unsigned(uint64(i)), Labels: []model.Label{{Name: "statistic", Value: fmt.Sprintf("queue_%d", i)}, {Name: "encoding", Value: "utf8"}}}
	}
	return samples
}

// PerformanceNative adds distinct native ports beside the RoCE fixture HCA.
func (f *PrometheusFixture) PerformanceNative() {
	for port := uint32(1); port <= 2; port++ {
		key := model.DeviceKey{Namespace: 1, Kind: model.DeviceNativeRDMA, RDMADevice: "mlx5_1", Port: port}
		d := model.Device{Key: key, Token: model.Token{SourceEpoch: f.r.epoch}, Eligibility: model.Eligible, Up: presentValue(true), RDMA: true}
		if _, err := f.r.observe(model.Observation{Device: d}); err != nil {
			f.t.Fatal(err)
		}
		p := model.RDMAPort{Device: "mlx5_1", Port: port, Layer: rdmaNativeLayer, State: presentValue(uint8(4)), Physical: presentValue(uint8(5))}
		samples, _ := rdmaSamples([]model.RDMAPort{p}, presentValue(true))
		if err := f.samples(model.JobKey{Namespace: 1, Device: key, Collector: model.CollectorRDMAState}, samples); err != nil {
			f.t.Fatal(err)
		}
	}
}

// PerformanceRealisticFleet installs two 100G Ethernet links and two
// independent 400G RoCEv2 Ethernet links. Each link carries a bounded driver
// statistic schema so the exporter benchmark exercises mixed identity and
// settings/RDMA projections used by a representative host.
func (f *PrometheusFixture) PerformanceRealisticFleet() {
	stats := PerformanceSamples(256)
	for index := uint32(1); index <= 4; index++ {
		name := fmt.Sprintf("eth%d", index)
		f.Device(index, name, true)
		if err := f.Samples(index, model.CollectorDriver, stats...); err != nil {
			f.t.Fatal(err)
		}
		speed := uint64(100_000_000_000)
		if index > 2 {
			speed = 400_000_000_000
		}
		settings := []model.Sample{
			interfaceGauge("max_speed_bits_per_second", speed),
			interfaceGauge("speed_bits_per_second", speed),
			interfaceGauge("duplex_info", 1, model.Label{Name: "duplex", Value: "full"}, model.Label{Name: "source", Value: "ethtool"}),
		}
		if err := f.Samples(index, model.CollectorSettings, settings...); err != nil {
			f.t.Fatal(err)
		}
	}
	for index := uint32(3); index <= 4; index++ {
		key := model.DeviceKey{Namespace: 1, Kind: model.DeviceEthernet, Index: index}
		d := model.Device{Key: key, Name: fmt.Sprintf("eth%d", index), Token: model.Token{SourceEpoch: f.r.epoch}, Eligibility: model.Eligible, Up: presentValue(true), RDMA: true}
		if _, err := f.r.observe(model.Observation{Device: d}); err != nil {
			f.t.Fatal(err)
		}
		port := model.RDMAPort{Device: fmt.Sprintf("mlx5_%d", index-3), Port: 1, Layer: rdmaEthernetLayer, State: presentValue(uint8(4)), Physical: presentValue(uint8(5)), Aliases: []string{fmt.Sprintf("eth%d", index)}, Versions: []string{"v2"}}
		samples, _ := rdmaSamples([]model.RDMAPort{port}, presentValue(true))
		if err := f.samples(model.JobKey{Namespace: 1, Device: key, Collector: model.CollectorRDMAState}, samples); err != nil {
			f.t.Fatal(err)
		}
	}
	for index, name := range map[uint32]string{5: "docker0", 6: "br-0123456789ab"} {
		key := model.DeviceKey{Namespace: 1, Kind: model.DeviceEthernet, Index: index}
		d := model.Device{Key: key, Name: name, Token: model.Token{SourceEpoch: f.r.epoch}, Eligibility: model.Excluded, Up: presentValue(true)}
		if _, err := f.r.observe(model.Observation{Device: d}); err != nil {
			f.t.Fatal(err)
		}
	}
}

// PerformanceLocalMachine installs the three Ethernet interfaces shown in the
// local-machine example. The supplied ifconfig data reports zero counters but
// does not establish negotiated speed or duplex, so settings remain unknown.
func (f *PrometheusFixture) PerformanceLocalMachine() {
	for index, name := range map[uint32]string{1: "enp1s0", 2: "enp35s0f0np0", 3: "enp35s0f1np1"} {
		f.Device(index, name, true)
		record := model.LinkStatistics{Key: model.DeviceKey{Namespace: 1, Kind: model.DeviceEthernet, Index: index}, Fields: 25, Width: 64}
		record.Carrier[0] = model.Optional[uint64]{Value: 1, Present: true}
		if err := f.samples(model.JobKey{Namespace: 1, Device: record.Key, Collector: model.CollectorNetdev}, trafficSamples(&record, f.r.generation)); err != nil {
			f.t.Fatal(err)
		}
	}
}

func performanceFixture(b testing.TB, ports, fields int) *PrometheusFixture {
	b.Helper()
	f := NewPrometheusFixture(b)
	samples := PerformanceSamples(fields)
	for i := 1; i <= ports; i++ {
		f.Device(uint32(i), fmt.Sprintf("eth%d", i), true)
		if err := f.Samples(uint32(i), model.CollectorDriver, samples...); err != nil {
			b.Fatal(err)
		}
	}
	f.Publish(uint64(ports))
	return f
}

func BenchmarkPerformancePublication(b *testing.B) {
	for _, fields := range []int{0, 64, 1024, 8192, 65536} {
		b.Run(fmt.Sprintf("fields=%d", fields), func(b *testing.B) {
			f := performanceFixture(b, 2, fields)
			before := f.Monitor.root.Load().pages[0][0].collectors[model.CollectorDriver].block
			b.ReportAllocs()
			for b.Loop() {
				f.Device(1, "eth1", false)
				f.Publish(2)
				f.Device(1, "eth1", true)
				f.Publish(2)
			}
			if after := f.Monitor.root.Load().pages[0][0].collectors[model.CollectorDriver].block; before != after {
				b.Fatal("event publication copied numeric storage")
			}
			b.ReportMetric(2, "events/op")
		})
	}
}

func BenchmarkPerformanceReducer(b *testing.B) {
	for _, ports := range []int{2, 8, 32, 128, 256} {
		b.Run(fmt.Sprintf("ports=%d", ports), func(b *testing.B) {
			f := performanceFixture(b, ports, 0)
			b.ReportAllocs()
			for b.Loop() {
				f.Device(1, "eth1", false)
				f.Device(1, "eth1", true)
			}
			if f.r.upCount != uint64(ports) {
				b.Fatal("reducer changed final count")
			}
			b.ReportMetric(2, "events/op")
		})
	}
}

func BenchmarkPerformanceRefresh(b *testing.B) {
	for _, fields := range []int{0, 64, 1024, 8192, 65536} {
		b.Run(fmt.Sprintf("fields=%d", fields), func(b *testing.B) {
			f := performanceFixture(b, 2, fields)
			samples := PerformanceSamples(fields)
			b.ReportAllocs()
			for b.Loop() {
				if err := f.Samples(1, model.CollectorDriver, samples...); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
