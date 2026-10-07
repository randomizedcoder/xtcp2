package baseline

import (
	"context"
	"errors"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

const validJSON = `{"version":1,"expected_up_links":4,"recorded_at":"2026-10-06T12:00:00Z"}`

func testRecord(count uint64) model.Baseline {
	return model.Baseline{Version: 1, Count: count, RecordedAt: time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)}
}

func newStore(t *testing.T, path string) *Store {
	t.Helper()
	store, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	return store
}

func requireSave(t *testing.T, store *Store, record model.Baseline) {
	t.Helper()
	if result := store.Save(t.Context(), record); result.Outcome != model.SaveDurable || result.Err != nil {
		t.Fatalf("save = %+v", result)
	}
}

func TestDirectoryPermissionsAndRotation(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected string
		parent                                string
		existing, blocked                     bool
	}{
		{"missing", "positive", "create a missing parent", "restrictive new directory and readback", "new", false, false},
		{"nested", "boundary", "create multiple missing parents", "each new directory excludes world bits", "one/two/three", false, false},
		{"existing", "corner", "existing directory requested 0755 subject to umask", "preserve its actual mode through rotation", "existing", true, false},
		{"file component", "negative", "required directory component is a file", "error and original file unchanged", "file/child", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			root := t.TempDir()
			dir := filepath.Join(root, tc.parent)
			var originalMode fs.FileMode
			if tc.existing {
				if err := os.Mkdir(dir, 0755); err != nil {
					t.Fatal(err)
				}
				info, err := os.Stat(dir)
				if err != nil {
					t.Fatal(err)
				}
				originalMode = info.Mode().Perm()
			}
			if tc.blocked {
				if err := os.WriteFile(filepath.Join(root, "file"), []byte("preserved"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			path := filepath.Join(dir, "baseline.json")
			store := newStore(t, path)
			_, present, err := store.LoadAndLock(t.Context())
			if tc.blocked {
				if err == nil {
					t.Fatal("file component accepted as directory")
				}
				if data, readErr := os.ReadFile(filepath.Join(root, "file")); readErr != nil || string(data) != "preserved" {
					t.Fatal("existing file changed")
				}
				return
			}
			if err != nil || present {
				t.Fatalf("first installation = %v, %v", present, err)
			}
			for _, count := range []uint64{4, 0, 6} {
				requireSave(t, store, testRecord(count))
			}
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			reopened := newStore(t, path)
			got, present, err := reopened.LoadAndLock(t.Context())
			if err != nil || !present || got != testRecord(6) {
				t.Fatalf("readback = %+v, %v, %v", got, present, err)
			}
			assertPermissions(t, root, dir, path, tc.existing, originalMode)
			leftovers, err := filepath.Glob(filepath.Join(dir, ".baseline-*"))
			if err != nil || len(leftovers) != 0 {
				t.Fatalf("temporary files remain: %v, %v", leftovers, err)
			}
		})
	}
}

func assertPermissions(t *testing.T, root, dir, path string, existing bool, originalMode fs.FileMode) {
	t.Helper()
	for _, file := range []string{path, path + ".lock"} {
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() & ^fs.FileMode(0600) != 0 {
			t.Fatalf("file permissions = %o", info.Mode().Perm())
		}
	}
	for current := dir; current != root; current = filepath.Dir(current) {
		info, err := os.Stat(current)
		if err != nil {
			t.Fatal(err)
		}
		if existing {
			if info.Mode().Perm() != originalMode {
				t.Fatalf("existing permissions changed: %o", info.Mode().Perm())
			}
		} else if info.Mode().Perm() & ^fs.FileMode(0750) != 0 {
			t.Fatalf("new directory permissions = %o", info.Mode().Perm())
		}
	}
}

func TestLoadValidation(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expected, data string
		wantError                                   bool
	}{
		{"valid", "positive", "versioned baseline", "load count four", validJSON, false},
		{"zero", "boundary", "zero baseline is valid", "present zero", strings.Replace(validJSON, ":4,", ":0,", 1), false},
		{"maximum", "boundary", "uint64 maximum count", "retain exact integer", strings.Replace(validJSON, ":4,", ":18446744073709551615,", 1), false},
		{"size limit", "boundary", "exactly 64 KiB including whitespace", "load", validJSON + strings.Repeat(" ", MaxFileBytes-len(validJSON)), false},
		{"oversize", "boundary", "64 KiB plus one byte", "startup error", validJSON + strings.Repeat(" ", MaxFileBytes-len(validJSON)+1), true},
		{"negative", "negative", "negative count", "startup error", strings.Replace(validJSON, ":4,", ":-1,", 1), true},
		{"fraction", "negative", "fractional count", "startup error", strings.Replace(validJSON, ":4,", ":1.5,", 1), true},
		{"overflow", "boundary", "count exceeds uint64", "startup error", strings.Replace(validJSON, ":4,", ":18446744073709551616,", 1), true},
		{"version", "negative", "unsupported version", "startup error", strings.Replace(validJSON, ":1,", ":2,", 1), true},
		{"timestamp", "negative", "invalid timestamp", "startup error", strings.Replace(validJSON, "2026-10-06T12:00:00Z", "tomorrow", 1), true},
		{"zero timestamp", "negative", "zero timestamp", "startup error", strings.Replace(validJSON, "2026-10-06T12:00:00Z", "0001-01-01T00:00:00Z", 1), true},
		{"missing", "negative", "absent fields", "startup error", `{"version":1}`, true},
		{"null", "negative", "null is not an absent installation", "startup error", `null`, true},
		{"duplicate", "corner", "duplicate count keys", "startup error", strings.Replace(validJSON, `"expected_up_links":4`, `"expected_up_links":4,"expected_up_links":7`, 1), true},
		{"trailing", "negative", "two records", "startup error", validJSON + validJSON, true},
		{"extra key", "negative", "unexpected field", "startup error", strings.Replace(validJSON, `"version":1`, `"extra":0,"version":1`, 1), true},
		{"truncated", "negative", "incomplete JSON", "startup error", validJSON[:len(validJSON)-1], true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			path := filepath.Join(t.TempDir(), "baseline.json")
			if err := os.WriteFile(path, []byte(tc.data), 0600); err != nil {
				t.Fatal(err)
			}
			store := newStore(t, path)
			record, present, err := store.LoadAndLock(t.Context())
			if (err != nil) != tc.wantError || present == tc.wantError {
				t.Fatalf("load = %+v, %v, %v", record, present, err)
			}
			if tc.name == "zero" && record.Count != 0 {
				t.Fatal("zero baseline changed")
			}
			if tc.name == "maximum" && record.Count != math.MaxUint64 {
				t.Fatal("large baseline lost integer precision")
			}
			if data, readErr := os.ReadFile(path); readErr != nil || string(data) != tc.data {
				t.Fatal("load replaced existing state")
			}
			if tc.wantError {
				lock, lockErr := lockFile(path + ".lock")
				if lockErr != nil {
					t.Fatalf("failed load retained lock: %v", lockErr)
				}
				if closeErr := lock.Close(); closeErr != nil {
					t.Fatal(closeErr)
				}
			}
		})
	}
}

func TestLifetimeLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	first, second := newStore(t, path), newStore(t, path)
	if _, _, err := first.LoadAndLock(t.Context()); err != nil {
		t.Fatal(err)
	}
	requireSave(t, first, testRecord(4))
	if _, _, err := second.LoadAndLock(t.Context()); !errors.Is(err, ErrLocked) {
		t.Fatalf("competing lock = %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if got, present, err := second.LoadAndLock(t.Context()); err != nil || !present || got != testRecord(4) {
		t.Fatalf("lock after release = %+v, %v, %v", got, present, err)
	}
	if result := first.Save(t.Context(), testRecord(6)); result.Err == nil {
		t.Fatal("closed store wrote baseline")
	}
}

func TestCanceledBeforeIO(t *testing.T) {
	store := newStore(t, filepath.Join(t.TempDir(), "baseline.json"))
	store.ops.mkdirAll = func(string, fs.FileMode) error { t.Fatal("I/O after cancellation"); return nil }
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := store.LoadAndLock(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("load = %v", err)
	}
	if result := store.Save(ctx, testRecord(4)); !errors.Is(result.Err, context.Canceled) {
		t.Fatalf("save = %+v", result)
	}
}
