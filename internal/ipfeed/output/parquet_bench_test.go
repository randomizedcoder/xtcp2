package output

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
)

// genRecords builds n fully-populated records so the Parquet writer exercises
// every column, not just the sparse ones.
func genRecords(n int) []model.Record {
	recs := make([]model.Record, n)
	for i := range n {
		recs[i] = model.Record{
			Prefix:          fmt.Sprintf("10.%d.%d.0/24", (i>>8)&0xff, i&0xff),
			IPVersion:       4,
			NetworkOwner:    "bench",
			ServiceOperator: "bench",
			Provider:        "bench",
			Service:         "svc",
			Region:          "us-east-1",
			SourceName:      "bench",
			SourceType:      "provider_feed",
			SourceURL:       "https://example/feed.json",
			RetrievedAt:     "2026-01-01T00:00:00Z",
			Confidence:      "authoritative",
		}
	}
	return recs
}

// BenchmarkWriteParquet measures Parquet write throughput and allocations
// across dataset sizes. It writes to a temp file per run (WriteParquet takes a
// path); SetBytes reports the encoded size so `-benchmem` shows MB/s.
func BenchmarkWriteParquet(b *testing.B) {
	for _, size := range []int{1_000, 100_000} {
		recs := genRecords(size)
		b.Run(fmt.Sprintf("n=%d", size), func(b *testing.B) {
			b.ReportAllocs()
			b.ResetTimer()
			var written int64
			for i := range b.N {
				path := filepath.Join(b.TempDir(), fmt.Sprintf("bench-%d.parquet", i))
				n, err := WriteParquet(path, recs)
				if err != nil {
					b.Fatalf("WriteParquet: %v", err)
				}
				written = n
			}
			b.SetBytes(written)
		})
	}
}
