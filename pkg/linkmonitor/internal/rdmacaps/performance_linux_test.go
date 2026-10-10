//go:build linux && rdma && cgo && monitor_bench

package rdmacaps

import (
	"runtime"
	"testing"
)

func TestPerformanceUMADRoundtrip(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		value                                 byte
	}{
		{"zero", "boundary", "zero-filled 256-byte request", "identical reply and three Go/C calls", 0},
		{"maximum", "positive", "every request byte is 255", "all bytes preserved after buffer cleanup", 255},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			var request [256]byte
			for i := range request {
				request[i] = tc.value
			}
			before := runtime.NumCgoCall()
			got, ok := benchmarkExchange(request)
			if !ok || got != request || runtime.NumCgoCall()-before != 3 {
				t.Fatal(tc.expected)
			}
		})
	}
}

func BenchmarkPerformanceUMAD(b *testing.B) {
	for _, parallel := range []bool{false, true} {
		name := "uncontended"
		if parallel {
			name = "contended"
		}
		b.Run(name, func(b *testing.B) {
			req := request(1)
			if got, ok := benchmarkExchange(req); !ok || got != req {
				b.Fatal("simulated query changed bytes")
			}
			b.ReportAllocs()
			calls := runtime.NumCgoCall()
			if parallel {
				b.ResetTimer()
				b.RunParallel(func(pb *testing.PB) {
					for pb.Next() {
						if _, ok := benchmarkExchange(req); !ok {
							b.Error("simulated allocation failed")
						}
					}
				})
			} else {
				for b.Loop() {
					if _, ok := benchmarkExchange(req); !ok {
						b.Fatal("simulated allocation failed")
					}
				}
			}
			b.ReportMetric(float64(runtime.NumCgoCall()-calls)/float64(b.N), "cgo/query")
		})
	}
}
