// Package output writes the combined dataset to a timestamped Parquet file.
package output

import (
	"errors"
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
//
// The write is atomic with respect to readers of path: rows go to a temporary
// file in the same directory (path + ".tmp"), which is fsync'ed, closed and
// then renamed over path. A consumer such as xtcp2's pkg/ipasn that reloads the
// artifact on a timer therefore only ever opens a complete file — never a
// half-written one — and a crash mid-write leaves the previous artifact in
// place. On any error the temporary file is removed.
func WriteParquet(path string, records []model.Record) (n int64, err error) {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, err
	}
	// Until the rename succeeds, every failure path discards the temp file. A
	// failure to remove it is folded into the returned error so a stale .tmp
	// never goes unnoticed (the next run overwrites it anyway).
	committed := false
	defer func() {
		if committed {
			return
		}
		if rerr := os.Remove(tmp); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove %s: %w", tmp, rerr))
		}
	}()

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
	if err := f.Sync(); err != nil {
		_ = f.Close() // error path: best-effort close, surfacing the sync error
		return 0, fmt.Errorf("sync parquet file: %w", err)
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
	if err := os.Rename(tmp, path); err != nil {
		return 0, fmt.Errorf("rename parquet file into place: %w", err)
	}
	committed = true
	return size, nil
}
