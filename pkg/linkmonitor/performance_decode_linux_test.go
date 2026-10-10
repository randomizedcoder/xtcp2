package linkmonitor

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func BenchmarkPerformanceDecode(b *testing.B) {
	for _, kind := range []string{"route", "ethtool"} {
		b.Run(kind, func(b *testing.B) {
			data := productionRouteFixture()
			if kind == "ethtool" {
				data = productionEnvelope(42, []byte{5, 1, 0, 0, 12, 0, 1, 128, 8, 0, 1, 0, 1, 0, 0, 0})
			}
			count := 0
			emit := func(model.Event) bool { count++; return true }
			b.ReportAllocs()
			for b.Loop() {
				var err error
				if kind == "route" {
					err = decodeRouteEvents(data, 1, model.Stamp{}, emit)
				} else {
					err = decodeEthtoolEvents(data, 42, 1, emit)
				}
				if err != nil {
					b.Fatal(err)
				}
			}
			if count != b.N {
				b.Fatal("decoder lost events")
			}
		})
	}
}
