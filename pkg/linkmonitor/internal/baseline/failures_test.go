package baseline

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/testkit"
)

type faultyFile struct {
	syncedFile
	writeErr, syncErr, closeErr error
	short                       bool
	trace                       *[]string
}

func (f *faultyFile) Write(data []byte) (int, error) {
	*f.trace = append(*f.trace, "write")
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	if f.short {
		return f.syncedFile.Write(data[:len(data)-1])
	}
	return f.syncedFile.Write(data)
}
func (f *faultyFile) Sync() error {
	*f.trace = append(*f.trace, "sync")
	if f.syncErr != nil {
		return f.syncErr
	}
	return f.syncedFile.Sync()
}
func (f *faultyFile) Close() error {
	*f.trace = append(*f.trace, "close")
	return errors.Join(f.syncedFile.Close(), f.closeErr)
}

type faultyDirectory struct {
	syncedDirectory
	syncErr, closeErr error
	trace             *[]string
}

func (f *faultyDirectory) Sync() error {
	*f.trace = append(*f.trace, "directory sync")
	if f.syncErr != nil {
		return f.syncErr
	}
	return f.syncedDirectory.Sync()
}
func (f *faultyDirectory) Close() error {
	*f.trace = append(*f.trace, "directory close")
	return errors.Join(f.syncedDirectory.Close(), f.closeErr)
}

func injectSaveFailure(store *Store, stage string, failure error, trace *[]string) {
	base := filesystem()
	store.ops.temporary = func(dir, pattern string) (syncedFile, error) {
		*trace = append(*trace, "create")
		if stage == "create" {
			return nil, failure
		}
		file, err := base.temporary(dir, pattern)
		if err != nil {
			return nil, err
		}
		wrapped := &faultyFile{syncedFile: file, trace: trace, short: stage == "short write"}
		switch stage {
		case "write", "cleanup":
			wrapped.writeErr = failure
		case "sync":
			wrapped.syncErr = failure
		case "close":
			wrapped.closeErr = failure
		}
		return wrapped, nil
	}
	store.ops.rename = func(from, to string) error {
		*trace = append(*trace, "rename")
		if filepath.Dir(from) != filepath.Dir(to) {
			return errors.New("temporary file was not colocated")
		}
		if stage == "rename" {
			return failure
		}
		return base.rename(from, to)
	}
	store.ops.remove = func(path string) error {
		*trace = append(*trace, "remove")
		if stage == "cleanup" {
			return fs.ErrPermission
		}
		return base.remove(path)
	}
	store.ops.directory = func(path string) (syncedDirectory, error) {
		*trace = append(*trace, "directory open")
		if stage == "directory open" {
			return nil, failure
		}
		file, err := base.directory(path)
		if err != nil {
			return nil, err
		}
		wrapped := &faultyDirectory{syncedDirectory: file, trace: trace}
		if stage == "directory sync" {
			wrapped.syncErr = failure
		}
		if stage == "directory close" {
			wrapped.closeErr = failure
		}
		return wrapped, nil
	}
}

func TestSaveFailureOutcomes(t *testing.T) {
	failure := errors.New("injected storage error")
	for _, tc := range []struct {
		name, category, description, expected string
		outcome                               model.SaveOutcome
		diskCount                             uint64
	}{
		{"create", "negative", "temporary file creation denied", "failed, old disk state", model.SaveFailed, 4},
		{"write", "negative", "write error", "failed, old disk state", model.SaveFailed, 4},
		{"short write", "boundary", "short write without error", "ErrShortWrite, old disk state", model.SaveFailed, 4},
		{"sync", "negative", "temporary file fsync fails", "failed, old disk state", model.SaveFailed, 4},
		{"close", "negative", "temporary file close fails", "failed, old disk state", model.SaveFailed, 4},
		{"rename", "negative", "atomic rename fails", "failed, old disk state", model.SaveFailed, 4},
		{"directory open", "negative", "parent open fails after rename", "indeterminate, new disk state visible", model.SaveIndeterminate, 6},
		{"directory sync", "negative", "parent fsync fails after rename", "indeterminate, new disk state visible", model.SaveIndeterminate, 6},
		{"directory close", "corner", "parent close fails after fsync", "indeterminate, cleanup failure visible", model.SaveIndeterminate, 6},
		{"cleanup", "corner", "write and temporary removal both fail", "both errors retained", model.SaveFailed, 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expected)
			path := filepath.Join(t.TempDir(), "baseline.json")
			store := newStore(t, path)
			if _, _, err := store.LoadAndLock(t.Context()); err != nil {
				t.Fatal(err)
			}
			requireSave(t, store, testRecord(4))
			trace := make([]string, 0, 9)
			injectSaveFailure(store, tc.name, failure, &trace)
			result := store.Save(t.Context(), testRecord(6))
			wantError := failure
			if tc.name == "short write" {
				wantError = io.ErrShortWrite
			}
			if result.Outcome != tc.outcome || !errors.Is(result.Err, wantError) {
				t.Fatalf("save = %+v, trace %v", result, trace)
			}
			if tc.name == "cleanup" && !errors.Is(result.Err, fs.ErrPermission) {
				t.Fatal("cleanup error swallowed")
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			record, err := decodeRecord(data)
			if err != nil || record.Count != tc.diskCount {
				t.Fatalf("disk = %+v, %v", record, err)
			}
			leftovers, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".baseline-*"))
			if err != nil {
				t.Fatal(err)
			}
			if tc.name != "cleanup" && len(leftovers) != 0 {
				t.Fatalf("temporary files leaked: %v", leftovers)
			}
			// Retry the identical intended record after uncertain durability.
			store.ops = filesystem()
			requireSave(t, store, testRecord(6))
		})
	}
}

func TestDurableSaveOrdering(t *testing.T) {
	store := newStore(t, filepath.Join(t.TempDir(), "baseline.json"))
	if _, _, err := store.LoadAndLock(t.Context()); err != nil {
		t.Fatal(err)
	}
	trace := make([]string, 0, 8)
	injectSaveFailure(store, "", nil, &trace)
	requireSave(t, store, testRecord(4))
	want := []string{"create", "write", "sync", "close", "rename", "directory open", "directory sync", "directory close"}
	if !reflect.DeepEqual(trace, want) {
		t.Fatalf("write order = %v, want %v", trace, want)
	}
}

type gatedFile struct {
	syncedFile
	gate *testkit.Barrier
	t    *testing.T
}

func (f gatedFile) Sync() error {
	if err := f.gate.Wait(f.t.Context()); err != nil {
		return err
	}
	return f.syncedFile.Sync()
}

func await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case value := <-ch:
		return value
	case <-time.After(5 * time.Second):
		t.Fatal("test synchronization timed out")
		var zero T
		return zero
	}
}

func TestCloseRetainsLockDuringSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "baseline.json")
	store, competitor := newStore(t, path), newStore(t, path)
	if _, _, err := store.LoadAndLock(t.Context()); err != nil {
		t.Fatal(err)
	}
	gate := testkit.NewBarrier()
	defer gate.Release()
	base := filesystem()
	store.ops.temporary = func(dir, pattern string) (syncedFile, error) {
		file, err := base.temporary(dir, pattern)
		if err != nil {
			return nil, err
		}
		return gatedFile{file, gate, t}, nil
	}
	saved := make(chan model.SaveResult, 1)
	go func() { saved <- store.Save(t.Context(), testRecord(6)) }()
	await(t, gate.Entered())
	closing, closed := make(chan struct{}), make(chan error, 1)
	go func() { close(closing); closed <- store.Close() }()
	await(t, closing)
	if _, _, err := competitor.LoadAndLock(t.Context()); !errors.Is(err, ErrLocked) {
		t.Fatalf("lock released while write blocked: %v", err)
	}
	select {
	case err := <-closed:
		t.Fatalf("Close returned before active write ended: %v", err)
	default:
	}
	gate.Release()
	if result := await(t, saved); result.Outcome != model.SaveDurable || result.Err != nil {
		t.Fatalf("save = %+v", result)
	}
	if err := await(t, closed); err != nil {
		t.Fatal(err)
	}
	if got, present, err := competitor.LoadAndLock(t.Context()); err != nil || !present || got.Count != 6 {
		t.Fatalf("after cleanup = %+v, %v, %v", got, present, err)
	}
}
