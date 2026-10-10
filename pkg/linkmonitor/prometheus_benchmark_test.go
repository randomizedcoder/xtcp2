package linkmonitor_test

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func BenchmarkPrometheusCachedGather(b *testing.B) {
	f := linkmonitor.NewPrometheusFixture(b)
	f.Device(1, "eth0", true)
	if err := f.Samples(0, model.CollectorNetstat, model.Sample{Descriptor: "netstat_Tcp_ActiveOpens", Kind: model.SampleUntyped, Number: model.Unsigned(42)}); err != nil {
		b.Fatal(err)
	}
	f.Publish(1)
	r := monitorRegistry(b, f.Monitor)
	gathered(b, r)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := r.Gather(); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkPrometheusSchemaChange(b *testing.B) {
	f := linkmonitor.NewPrometheusFixture(b)
	r := monitorRegistry(b, f.Monitor)
	keys := [...]string{"netstat_Tcp_ActiveOpens", "netstat_MPTcpExt_MPCapableSYNRX"}
	i := 0
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if err := f.Samples(0, model.CollectorNetstat, model.Sample{Descriptor: keys[i%2], Kind: model.SampleUntyped, Number: model.Unsigned(42)}); err != nil {
			b.Fatal(err)
		}
		f.Publish(1)
		if _, err := r.Gather(); err != nil {
			b.Fatal(err)
		}
		i++
	}
}
