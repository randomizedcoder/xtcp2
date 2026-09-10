package output

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
)

func TestTimestampAndFilename(t *testing.T) {
	tm := time.Date(2026, 9, 9, 14, 30, 5, 0, time.UTC)
	tests := []struct {
		name string
		desc string
		got  string
		want string
	}{
		{"positive_timestamp", "positive: minute-granularity UTC stamp, seconds dropped",
			Timestamp(tm), "2026-09-09-14-30"},
		{"positive_filename", "positive: filename appends .parquet",
			Filename(tm), "2026-09-09-14-30.parquet"},
		{"boundary_utc_conversion", "boundary: a non-UTC time is converted to UTC before formatting",
			Timestamp(tm.In(time.FixedZone("x", 3600))), "2026-09-09-14-30"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.got != tc.want {
				t.Errorf("%s: got %q, want %q", tc.desc, tc.got, tc.want)
			}
		})
	}
}

func TestWriteParquet(t *testing.T) {
	tests := []struct {
		name    string
		desc    string
		class   string
		records []model.Record
	}{
		{"positive_rows", "positive: writes rows and reports a positive size", "positive",
			[]model.Record{{Prefix: "1.2.3.0/24", IPVersion: 4}, {Prefix: "2600::/16", IPVersion: 6}}},
		{"boundary_empty", "boundary: zero records still produces a valid parquet file", "boundary",
			nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "out.parquet")
			size, err := WriteParquet(path, tc.records)
			if err != nil {
				t.Fatalf("%s: %v", tc.desc, err)
			}
			if size <= 0 {
				t.Fatalf("%s: size = %d, want > 0", tc.desc, size)
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			st, _ := f.Stat()
			pf, err := parquet.OpenFile(f, st.Size())
			if err != nil {
				t.Fatalf("%s: reopen: %v", tc.desc, err)
			}
			if got := pf.NumRows(); got != int64(len(tc.records)) {
				t.Errorf("%s: NumRows = %d, want %d", tc.desc, got, len(tc.records))
			}
		})
	}
}
