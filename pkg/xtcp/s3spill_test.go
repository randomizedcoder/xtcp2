//go:build dest_s3parquet

package xtcp

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// spillObj is one object to spill in a test.
type spillObj struct {
	key  string
	body string
	rows int
}

// spillSize is the on-disk size of o.
func spillSize(o spillObj) int64 {
	return int64(len(spillHeader(o.key, o.rows)) + len(o.body))
}

// readOldest opens, reads and closes the oldest spilled object.
func readOldest(t *testing.T, s *s3Spill) (spillObj, uint64, bool) {
	t.Helper()
	obj, ok := s.oldest()
	if !ok {
		return spillObj{}, 0, false
	}
	defer func() { _ = obj.Close() }()
	b, err := io.ReadAll(obj.Body)
	if err != nil {
		t.Fatalf("read spilled body: %v", err)
	}
	if int64(len(b)) != obj.Size {
		t.Fatalf("body %d bytes, Size %d", len(b), obj.Size)
	}
	return spillObj{key: obj.Key, body: string(b), rows: obj.Rows}, obj.seq, true
}

// drainKeys pops every spilled object, oldest first, and returns their keys.
func drainKeys(t *testing.T, s *s3Spill) []string {
	t.Helper()
	var keys []string
	for {
		o, seq, ok := readOldest(t, s)
		if !ok {
			return keys
		}
		keys = append(keys, o.key)
		s.remove(seq)
	}
}

func TestS3Spill(t *testing.T) {
	a := spillObj{key: "p/host=h/date=2026-11-22/hour=01/1_aa.parquet", body: "PAR1-aaaa", rows: 3}
	b := spillObj{key: "p/host=h/date=2026-11-22/hour=01/2_bb.parquet", body: "PAR1-bbbb", rows: 4}
	c := spillObj{key: "p/host=h/date=2026-11-22/hour=01/3_cc.parquet", body: "PAR1-cccc", rows: 5}

	tests := []struct {
		name        string
		category    string
		description string
		maxBytes    int64
		prepare     func(t *testing.T, dir string) // files present before open
		writes      []spillObj
		writeErr    error // expected from the last write
		reopenMax   int64 // > 0: close and reopen with this bound
		expected    []string
		evicted     float64
		errors      float64
	}{
		{name: "roundtrip", category: "P", description: "one object comes back with its key, rows and body",
			maxBytes: 1 << 20, writes: []spillObj{a}, expected: []string{a.key}},
		{name: "fifo", category: "P", description: "objects come back oldest first",
			maxBytes: 1 << 20, writes: []spillObj{a, b, c}, expected: []string{a.key, b.key, c.key}},
		{name: "evict_oldest", category: "N", description: "room for two: the third write evicts the first",
			maxBytes: spillSize(a) + spillSize(b), writes: []spillObj{a, b, c},
			expected: []string{b.key, c.key}, evicted: 1},
		{name: "exact_fit", category: "B", description: "an object exactly maxBytes is kept",
			maxBytes: spillSize(a), writes: []spillObj{a}, expected: []string{a.key}},
		{name: "too_large", category: "B", description: "an object one byte over maxBytes is refused, nothing evicted",
			maxBytes: spillSize(a) - 1, writes: []spillObj{a}, writeErr: errSpillTooLarge},
		{name: "empty_key", category: "N", description: "an empty key is refused",
			maxBytes: 1 << 20, writes: []spillObj{{key: "", body: "x"}}, writeErr: errors.New("any"), errors: 1},
		{name: "reopen_recovers", category: "P", description: "objects survive a restart, in order",
			maxBytes: 1 << 20, writes: []spillObj{a, b}, reopenMax: 1 << 20, expected: []string{a.key, b.key}},
		{name: "reopen_smaller_bound", category: "B", description: "a restart with a smaller bound evicts the oldest",
			maxBytes: 1 << 20, writes: []spillObj{a, b, c}, reopenMax: spillSize(c),
			expected: []string{c.key}, evicted: 2},
		{name: "tmp_leftover_removed", category: "C", description: "a .tmp from a crash mid-write is deleted at open",
			maxBytes: 1 << 20,
			prepare: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, spillName(7, s3SpillTmpExt)), "partial")
			},
			expected: nil},
		{name: "corrupt_skipped", category: "N", description: "a file with a bad header is deleted and counted",
			maxBytes: 1 << 20,
			prepare: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, spillName(1, s3SpillExt)), "not a spill file")
			},
			writes: []spillObj{a}, expected: []string{a.key}, errors: 1},
		{name: "bad_key_skipped", category: "N", description: "a spilled key with a control character is rejected",
			maxBytes: 1 << 20,
			prepare: func(t *testing.T, dir string) {
				h := spillHeader("evil\x1b[2J", 1)
				writeTestFile(t, filepath.Join(dir, spillName(1, s3SpillExt)), string(h)+"PAR1")
			},
			expected: nil, errors: 1},
		{name: "foreign_ignored", category: "C", description: "other files and a symlink named like a spill file are left alone",
			maxBytes: 1 << 20,
			prepare: func(t *testing.T, dir string) {
				writeTestFile(t, filepath.Join(dir, "notes.txt"), "keep")
				target := filepath.Join(t.TempDir(), "outside")
				writeTestFile(t, target, "outside")
				if err := os.Symlink(target, filepath.Join(dir, spillName(2, s3SpillExt))); err != nil {
					t.Fatal(err)
				}
			},
			writes: []spillObj{a}, expected: []string{a.key}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "spill")
			if tc.prepare != nil {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				tc.prepare(t, dir)
			}
			m := newS3SpillMetrics(prometheus.NewRegistry())
			s, err := openS3Spill(dir, tc.maxBytes, m)
			if err != nil {
				t.Fatalf("openS3Spill: %v", err)
			}
			var lastErr error
			for _, o := range tc.writes {
				lastErr = s.write(o.key, []byte(o.body), o.rows)
			}
			switch {
			case tc.writeErr == nil && lastErr != nil:
				t.Fatalf("%s: write error %v", tc.description, lastErr)
			case tc.writeErr != nil && lastErr == nil:
				t.Fatalf("%s: write succeeded, expected an error", tc.description)
			case errors.Is(tc.writeErr, errSpillTooLarge) && !errors.Is(lastErr, errSpillTooLarge):
				t.Fatalf("%s: write error %v, expected errSpillTooLarge", tc.description, lastErr)
			}
			if tc.reopenMax > 0 {
				if err := s.close(); err != nil {
					t.Fatal(err)
				}
				if s, err = openS3Spill(dir, tc.reopenMax, m); err != nil {
					t.Fatalf("reopen: %v", err)
				}
			}
			defer func() { _ = s.close() }()
			if got := drainKeys(t, s); !slices.Equal(got, tc.expected) {
				t.Errorf("%s: keys %q, expected %q", tc.description, got, tc.expected)
			}
			if n, by := s.stats(); n != 0 || by != 0 {
				t.Errorf("%s: after drain %d objects, %d bytes; expected 0, 0", tc.description, n, by)
			}
			if got := s3CounterValue(t, m.total.WithLabelValues(s3SpillEvicted)); got != tc.evicted {
				t.Errorf("%s: evicted %v, expected %v", tc.description, got, tc.evicted)
			}
			if got := s3CounterValue(t, m.total.WithLabelValues(s3SpillError)); got != tc.errors {
				t.Errorf("%s: errors %v, expected %v", tc.description, got, tc.errors)
			}
			if _, err := os.Stat(filepath.Join(dir, spillName(7, s3SpillTmpExt))); !os.IsNotExist(err) {
				t.Errorf("%s: .tmp leftover still present (%v)", tc.description, err)
			}
		})
	}
}

// TestS3Spill_contentAndModes checks one object byte for byte and the
// directory and file permissions.
func TestS3Spill_contentAndModes(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spill")
	s, err := openS3Spill(dir, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.close() }()
	if s.maxBytes != s3SpillMaxBytesDefaultCst {
		t.Errorf("maxBytes %d, expected the 1 GiB default for 0", s.maxBytes)
	}
	want := spillObj{key: "k/1.parquet", body: "PAR1\x00\xffbinary", rows: 42}
	if err := s.write(want.key, []byte(want.body), want.rows); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %v (%v), expected 0700", info.Mode().Perm(), err)
	}
	info, err = os.Stat(filepath.Join(dir, spillName(1, s3SpillExt)))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("file mode %v (%v), expected 0600", info.Mode().Perm(), err)
	}
	got, _, ok := readOldest(t, s)
	if !ok || got != want {
		t.Errorf("got %+v, expected %+v", got, want)
	}
}

func writeTestFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// s3WaitFor polls cond for up to 10 s.
func s3WaitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// TestS3ParquetDest_spill drives the destination through an S3 outage with
// a spill dir: the worker spills after one attempt, and the objects are
// uploaded once S3 is back.
func TestS3ParquetDest_spill(t *testing.T) {
	tests := []struct {
		name          string
		category      string
		description   string
		spillMax      int64
		preexisting   bool // an object from an earlier run is in the dir
		outageObjects int  // objects finalized while S3 fails
		expectedSpill int  // objects spilled during the outage
		expectedDrop  bool // uploadRowsDropped counted
	}{
		{name: "outage_spills_then_uploads", category: "P",
			description: "objects that fail during an outage are spilled after one attempt and uploaded when S3 returns",
			spillMax:    1 << 20, outageObjects: 3, expectedSpill: 3},
		{name: "earlier_run_uploaded", category: "P",
			description: "an object left by an earlier run is uploaded at start",
			spillMax:    1 << 20, preexisting: true},
		{name: "spill_too_small_drops", category: "N",
			description: "an object larger than the spill bound is dropped and counted",
			spillMax:    16, outageObjects: 1, expectedDrop: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "spill")
			const earlierKey = "earlier/run.parquet"
			if tc.preexisting {
				pre, err := openS3Spill(dir, 0, nil)
				if err != nil {
					t.Fatal(err)
				}
				if err := pre.write(earlierKey, []byte("PAR1-earlier"), 1); err != nil {
					t.Fatal(err)
				}
				_ = pre.close()
			}
			var failing atomic.Bool
			failing.Store(tc.outageObjects > 0)
			injectErr := func(int) error {
				if failing.Load() {
					return errors.New("dial tcp: connection refused")
				}
				return nil
			}
			reg := prometheus.NewRegistry()
			sm := newS3SpillMetrics(reg)
			d, upl, x := newS3ParquetFixtureCustom(t, 1, injectErr, func(d *s3ParquetDest) {
				spill, err := openS3Spill(dir, tc.spillMax, sm)
				if err != nil {
					t.Fatal(err)
				}
				d.startSpill(context.Background(), spill)
			})

			if tc.preexisting {
				s3WaitFor(t, "earlier object uploaded", func() bool { return hasKey(upl.Calls(), earlierKey) })
			}
			for i := 0; i < tc.outageObjects; i++ {
				before := len(upl.Calls())
				if _, err := d.Send(context.Background(), marshalEnvelopeBuf(t, x, mkEnvelope(1))); err != nil {
					t.Fatal(err)
				}
				// threshold 1: every envelope finalizes one object, tried once.
				s3WaitFor(t, "outage upload attempt", func() bool { return len(upl.Calls()) == before+1 })
			}
			s3WaitFor(t, "spill settles", func() bool {
				n, _ := d.spill.stats()
				return n == tc.expectedSpill
			})
			if got := s3CounterValue(t, sm.total.WithLabelValues(s3SpillWritten)); int(got) != tc.expectedSpill {
				t.Errorf("%s: written %v, expected %d", tc.description, got, tc.expectedSpill)
			}
			if got := s3CounterValue(t, x.pC.WithLabelValues("destS3Parquet", "uploadRowsDropped", "error")); (got > 0) != tc.expectedDrop {
				t.Errorf("%s: uploadRowsDropped %v, expected dropped=%v", tc.description, got, tc.expectedDrop)
			}
			outageKeys := uploadKeys(upl.Calls())

			// S3 is back: the next object's upload succeeds and wakes the
			// re-upload, which empties the spill.
			failing.Store(false)
			if _, err := d.Send(context.Background(), marshalEnvelopeBuf(t, x, mkEnvelope(1))); err != nil {
				t.Fatal(err)
			}
			s3WaitFor(t, "spill empty", func() bool { n, _ := d.spill.stats(); return n == 0 })
			if got := s3CounterValue(t, sm.total.WithLabelValues(s3SpillUploaded)); int(got) < tc.expectedSpill {
				t.Errorf("%s: uploaded %v, expected ≥ %d", tc.description, got, tc.expectedSpill)
			}
			if tc.expectedSpill > 0 {
				calls := upl.Calls()
				for _, k := range outageKeys {
					if n := countKey(calls, k); n != 2 {
						t.Errorf("%s: key %s put %d times, expected 2 (failed, then from the spill)", tc.description, k, n)
					}
				}
				// The re-upload sends the same Parquet bytes the worker built.
				if first, last := bodyOf(calls, outageKeys[0], 0), bodyOf(calls, outageKeys[0], 1); !bytes.Equal(first, last) {
					t.Errorf("%s: spilled body differs from the original", tc.description)
				}
			}
			if err := d.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// TestS3ParquetDest_spillKeptOnClose: objects still spilled at Close stay
// on disk for the next start.
func TestS3ParquetDest_spillKeptOnClose(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "spill")
	d, upl, x := newS3ParquetFixtureCustom(t, 1, func(int) error { return errors.New("refused") }, func(d *s3ParquetDest) {
		spill, err := openS3Spill(dir, 0, nil)
		if err != nil {
			t.Fatal(err)
		}
		d.startSpill(context.Background(), spill)
	})
	if _, err := d.Send(context.Background(), marshalEnvelopeBuf(t, x, mkEnvelope(1))); err != nil {
		t.Fatal(err)
	}
	s3WaitFor(t, "spilled", func() bool { n, _ := d.spill.stats(); return n == 1 })
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	if got := len(upl.Calls()); got != 1 {
		t.Errorf("uploads %d, expected 1 (one attempt, then spill)", got)
	}
	s, err := openS3Spill(dir, 0, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = s.close() }()
	if n, _ := s.stats(); n != 1 {
		t.Errorf("after restart %d objects, expected 1", n)
	}
}

func hasKey(calls []fakeUploadCall, key string) bool { return countKey(calls, key) > 0 }

func countKey(calls []fakeUploadCall, key string) int {
	n := 0
	for _, c := range calls {
		if c.key == key {
			n++
		}
	}
	return n
}

func uploadKeys(calls []fakeUploadCall) []string {
	keys := make([]string, 0, len(calls))
	for _, c := range calls {
		if strings.HasSuffix(c.key, ".parquet") {
			keys = append(keys, c.key)
		}
	}
	return keys
}

// bodyOf returns the body of the nth put of key.
func bodyOf(calls []fakeUploadCall, key string, nth int) []byte {
	for _, c := range calls {
		if c.key == key {
			if nth == 0 {
				return c.body
			}
			nth--
		}
	}
	return nil
}
