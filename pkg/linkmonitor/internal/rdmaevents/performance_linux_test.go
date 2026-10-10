//go:build linux && rdma && cgo && monitor_bench

package rdmaevents

import (
	"fmt"
	"runtime"
	"testing"
)

func TestPerformanceBatch(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		count, acks                           int
	}{
		{"one", "positive", "one port event", "copy port before one acknowledgement", 1, 1},
		{"empty", "boundary", "empty batch", "no call or acknowledgement", 0, 0},
		{"maximum", "boundary", "64 port events", "ordered ports and 64 acknowledgements", 64, 64},
		{"oversize", "negative", "65 events", "reject without partial acknowledgement", 65, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			ports := make([]uint32, tc.count)
			if got := benchmarkBatch(ports); got != tc.acks {
				t.Fatal(tc.expected, got)
			}
			for i := range tc.acks {
				if ports[i] != uint32(i+1) {
					t.Fatal("copy happened after acknowledgement or order changed")
				}
			}
		})
	}
}

func BenchmarkPerformanceCGO(b *testing.B) {
	for _, size := range []int{1, 8, 32, 64} {
		b.Run(fmt.Sprintf("batch=%d", size), func(b *testing.B) {
			ports := make([]uint32, size)
			calls := runtime.NumCgoCall()
			b.ReportAllocs()
			for b.Loop() {
				if benchmarkBatch(ports) != size {
					b.Fatal("acknowledgement count changed")
				}
			}
			b.ReportMetric(float64(runtime.NumCgoCall()-calls)/float64(b.N*size), "cgo/event")
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*size), "ns/event")
		})
	}
}
