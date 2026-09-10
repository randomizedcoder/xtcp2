package combine

import (
	"fmt"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
)

// genRecords builds n unique, canonical /24 records. The first octet spans
// 10..73 and the next two span 0..255, giving ~4.19M distinct /24s so no
// dedup collisions occur at the sizes benchmarked.
func genRecords(n int) []model.Record {
	recs := make([]model.Record, n)
	for i := range n {
		recs[i] = model.Record{
			Prefix:     fmt.Sprintf("%d.%d.%d.0/24", 10+((i>>16)&0x3f), (i>>8)&0xff, i&0xff),
			SourceName: "bench",
			Provider:   "bench",
		}
	}
	return recs
}

// BenchmarkValidate measures the combine hot path (netip.ParsePrefix +
// Masked() canonicalization + dedup map) across dataset sizes. A fresh seen
// map is allocated per iteration since Validate mutates it.
func BenchmarkValidate(b *testing.B) {
	for _, size := range []int{100, 10_000, 100_000} {
		recs := genRecords(size)
		b.Run(fmt.Sprintf("n=%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				seen := make(map[string]struct{}, size)
				res := Validate(recs, seen)
				if res.ValidCount() != size {
					b.Fatalf("valid=%d, want %d", res.ValidCount(), size)
				}
			}
		})
	}
}
