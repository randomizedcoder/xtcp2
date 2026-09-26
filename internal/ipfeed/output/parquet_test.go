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
			if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
				t.Errorf("%s: temp file %s.tmp left behind (stat err=%v)", tc.desc, path, err)
			}
		})
	}
}

// TestWriteParquetAtomic covers the temp-file + rename contract: the target is
// either the previous complete artifact or the new complete artifact, and no
// temp file survives either outcome.
func TestWriteParquetAtomic(t *testing.T) {
	two := []model.Record{{Prefix: "1.2.3.0/24", IPVersion: 4}, {Prefix: "2600::/16", IPVersion: 6}}
	five := []model.Record{
		{Prefix: "10.0.0.0/8", IPVersion: 4}, {Prefix: "10.1.0.0/16", IPVersion: 4}, {Prefix: "10.2.0.0/16", IPVersion: 4},
		{Prefix: "fd00::/8", IPVersion: 6}, {Prefix: "fd01::/16", IPVersion: 6},
	}

	tests := []struct {
		description string
		setup       func(t *testing.T, dir string) string // returns the target path
		records     []model.Record
		wantErr     bool
		wantRows    int64 // rows readable at the target afterwards (-1 = target must not exist)
	}{
		// positive
		{"fresh path: file created, no temp left", func(t *testing.T, dir string) string {
			return filepath.Join(dir, "a.parquet")
		}, two, false, 2},
		{"existing artifact is replaced by the new one (rename over)", func(t *testing.T, dir string) string {
			p := filepath.Join(dir, "b.parquet")
			if _, err := WriteParquet(p, two); err != nil {
				t.Fatal(err)
			}
			return p
		}, five, false, 5},
		// corner
		{"stale temp file from an earlier crash is overwritten, not an error", func(t *testing.T, dir string) string {
			p := filepath.Join(dir, "c.parquet")
			if err := os.WriteFile(p+".tmp", []byte("garbage"), 0o600); err != nil {
				t.Fatal(err)
			}
			return p
		}, two, false, 2},
		// negative
		{"missing directory: error, nothing created", func(t *testing.T, dir string) string {
			return filepath.Join(dir, "missing", "d.parquet")
		}, two, true, -1},
		{"target path is a directory: rename fails, temp removed", func(t *testing.T, dir string) string {
			p := filepath.Join(dir, "e.parquet")
			if err := os.Mkdir(p, 0o755); err != nil {
				t.Fatal(err)
			}
			return p
		}, two, true, -1},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := t.TempDir()
			path := tc.setup(t, dir)

			_, err := WriteParquet(path, tc.records)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tc.wantErr)
			}
			if _, serr := os.Stat(path + ".tmp"); !os.IsNotExist(serr) {
				t.Errorf("temp file left behind (stat err=%v)", serr)
			}
			if tc.wantRows < 0 {
				if st, serr := os.Stat(path); serr == nil && !st.IsDir() {
					t.Errorf("target file exists after a failed write")
				}
				return
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			st, _ := f.Stat()
			pf, err := parquet.OpenFile(f, st.Size())
			if err != nil {
				t.Fatalf("reopen: %v", err)
			}
			if got := pf.NumRows(); got != tc.wantRows {
				t.Errorf("NumRows = %d, want %d", got, tc.wantRows)
			}
		})
	}
}
