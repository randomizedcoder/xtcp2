package baseline

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// Store serializes storage activity and retains its lock until Close. Close
// waits for any active operation; callers must not free resources on timeout.
type Store struct {
	mu     sync.Mutex
	path   string
	ops    operations
	lock   io.Closer
	closed bool
}

// New validates a path without I/O. Directory creation occurs in LoadAndLock.
func New(path string) (*Store, error) {
	if path == "" || strings.ContainsRune(path, 0) {
		return nil, fmt.Errorf("invalid baseline path")
	}
	return &Store{path: filepath.Clean(path), ops: filesystem()}, nil
}

// LoadAndLock creates missing directories and acquires a sibling .lock before
// loading the record. Missing is distinct from invalid. Failure releases the
// acquired lock and includes any cleanup error.
func (s *Store) LoadAndLock(ctx context.Context) (model.Baseline, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return model.Baseline{}, false, err
	}
	if s.closed || s.lock != nil {
		return model.Baseline{}, false, fmt.Errorf("baseline store already loaded or closed")
	}
	if err := s.ops.mkdirAll(filepath.Dir(s.path), 0750); err != nil {
		return model.Baseline{}, false, err
	}
	lock, err := s.ops.lock(s.path + ".lock")
	if err != nil {
		return model.Baseline{}, false, err
	}
	record, present, err := s.load(ctx)
	if err != nil {
		return model.Baseline{}, false, errors.Join(err, lock.Close())
	}
	s.lock = lock
	return record, present, nil
}

func (s *Store) load(ctx context.Context) (model.Baseline, bool, error) {
	if err := ctx.Err(); err != nil {
		return model.Baseline{}, false, err
	}
	file, err := s.ops.open(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return model.Baseline{}, false, nil
	}
	if err != nil {
		return model.Baseline{}, false, err
	}
	data, readErr := io.ReadAll(io.LimitReader(file, MaxFileBytes+1))
	if err = errors.Join(readErr, file.Close(), ctx.Err()); err != nil {
		return model.Baseline{}, false, err
	}
	if len(data) > MaxFileBytes {
		return model.Baseline{}, false, fmt.Errorf("baseline exceeds %d bytes", MaxFileBytes)
	}
	record, err := decodeRecord(data)
	return record, err == nil, err
}

// Save writes one intended record. Durable success is the only outcome that
// authorizes the caller to replace its in-memory baseline. Indeterminate writes
// may already be visible on disk and must be retried with the same record.
func (s *Store) Save(ctx context.Context, record model.Baseline) model.SaveResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return model.SaveResult{Err: err}
	}
	if s.closed || s.lock == nil {
		return model.SaveResult{Err: fmt.Errorf("baseline store is not locked")}
	}
	data, err := encodeRecord(record)
	if err != nil {
		return model.SaveResult{Err: err}
	}
	return s.save(ctx, data)
}

func (s *Store) save(ctx context.Context, data []byte) (result model.SaveResult) {
	dir := filepath.Dir(s.path)
	file, err := s.ops.temporary(dir, ".baseline-*")
	if err != nil {
		return model.SaveResult{Err: err}
	}
	closed, renamed := false, false
	defer func() {
		if !closed {
			result.Err = errors.Join(result.Err, file.Close())
		}
		if !renamed {
			result.Err = errors.Join(result.Err, s.ops.remove(file.Name()))
		}
	}()
	if err := writeAndSync(ctx, file, data); err != nil {
		return model.SaveResult{Err: err}
	}
	err = file.Close()
	closed = true
	if err != nil {
		return model.SaveResult{Err: err}
	}
	if err := ctx.Err(); err != nil {
		return model.SaveResult{Err: err}
	}
	if err := s.ops.rename(file.Name(), s.path); err != nil {
		return model.SaveResult{Err: err}
	}
	renamed = true
	// After rename, always attempt directory sync, even if context was canceled.
	// Returning cancellation here cannot establish what survived a reboot.
	if err := s.syncDirectory(dir); err != nil {
		return model.SaveResult{Outcome: model.SaveIndeterminate, Err: err}
	}
	return model.SaveResult{Outcome: model.SaveDurable}
}

func writeAndSync(ctx context.Context, file syncedFile, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	n, err := file.Write(data)
	if n != len(data) {
		err = errors.Join(err, io.ErrShortWrite)
	}
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return file.Sync()
}

func (s *Store) syncDirectory(dir string) error {
	file, err := s.ops.directory(dir)
	if err != nil {
		return err
	}
	return errors.Join(file.Sync(), file.Close())
}

// Close releases the lifetime lock after outstanding storage operations finish.
// The lock file remains in place; unlinking it could create competing locks.
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.lock != nil {
		return s.lock.Close()
	}
	return nil
}

var _ model.BaselineStore = (*Store)(nil)
