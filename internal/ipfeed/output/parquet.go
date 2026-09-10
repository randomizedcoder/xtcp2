// Package output writes the combined dataset to a timestamped Parquet file.
package output

import (
	"fmt"
	"os"
	"time"

	"github.com/parquet-go/parquet-go"
	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
)

// Timestamp formats t as the YYYY-MM-DD-HH-MM basename (UTC), matching the
// required S3 object naming.
func Timestamp(t time.Time) string { return t.UTC().Format("2006-01-02-15-04") }

// Filename returns the Parquet filename for time t, e.g. 2026-09-09-14-30.parquet.
func Filename(t time.Time) string { return Timestamp(t) + ".parquet" }

// WriteParquet writes records to path and returns the file size in bytes.
func WriteParquet(path string, records []model.Record) (int64, error) {
	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	w := parquet.NewGenericWriter[model.Record](f)
	if len(records) > 0 {
		if _, err := w.Write(records); err != nil {
			_ = f.Close() // error path: best-effort close, surfacing the write error
			return 0, fmt.Errorf("write parquet rows: %w", err)
		}
	}
	if err := w.Close(); err != nil {
		_ = f.Close() // error path: best-effort close, surfacing the writer error
		return 0, fmt.Errorf("close parquet writer: %w", err)
	}
	fi, err := f.Stat()
	if err != nil {
		_ = f.Close() // error path: best-effort close, surfacing the stat error
		return 0, err
	}
	size := fi.Size()
	if err := f.Close(); err != nil {
		return 0, err
	}
	return size, nil
}
