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
