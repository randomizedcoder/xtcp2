package reconcile

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"golang.org/x/sys/unix"
)

// Journal serializes the daemon, watchdog and local administrative commands.
// The single atomic snapshot is authoritative. Per-assignment files are human
// readable projections only, never an input to recovery.
type Journal struct{ Dir string }

// ErrJournalBusy is returned, wrapping the context's error, when the lock
// was still held by another process or goroutine at the context's deadline.
var ErrJournalBusy = errors.New("treatment journal busy")

// lockPollMaxCst bounds the retry interval while waiting for the lock. The
// kernel's blocking flock cannot be interrupted by a context, so the wait is
// a non-blocking attempt repeated with a doubling delay from 1 ms to this.
const lockPollMaxCst = 50 * time.Millisecond

// pendingPrefix names atomicFile's temporary files. One is only ever created
// under the lock, so any found by a new lock holder belongs to a writer that
// died mid-write and is swept before the snapshot is read.
const pendingPrefix = ".pending-"

// WithLock loads the snapshot under the exclusive journal lock, runs fn with
// a save callback that persists the current state atomically, and refreshes
// the per-assignment projections if anything was saved. A snapshot that
// fails validation never reaches fn.
//
// The lock waits: the daemon's reconcile pass, the watchdog's expiry pass and
// a local suspend/resume each take it in turn rather than one skipping its
// pass because another held it. The wait is bounded by ctx; at its deadline
// WithLock returns ErrJournalBusy wrapping ctx.Err() without having read or
// written anything. `treatmentctl status` reads the snapshot without the lock
// and never waits.
func (j Journal) WithLock(ctx context.Context, fn func(*State, func() error) error) (err error) {
	if err = os.MkdirAll(j.Dir, 0700); err != nil {
		return err
	}
	fd, err := unix.Open(filepath.Join(j.Dir, ".lock"), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := unix.Close(fd); cerr != nil && err == nil {
			err = cerr
		}
	}()
	if err = lock(ctx, fd); err != nil {
		return err
	}
	defer func() {
		if uerr := unix.Flock(fd, unix.LOCK_UN); uerr != nil && err == nil {
			err = uerr
		}
	}()
	if err = j.sweepPending(); err != nil {
		return err
	}
	s := new(State)
	f, err := os.OpenFile(filepath.Join(j.Dir, SnapshotFile), os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err == nil {
		b, rerr := readLimited(f)
		cerr := f.Close()
		if rerr != nil {
			return rerr
		}
		if cerr != nil {
			return cerr
		}
		if s, err = UnmarshalText(b); err != nil {
			return err
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dirty := false
	save := func() error {
		b, err := MarshalText(s)
		if err != nil {
			return err
		}
		if len(b) > treatment.MaxStateBytes {
			return errors.New("journal exceeds size limit")
		}
		if err = atomicFile(j.Dir, SnapshotFile, b); err != nil {
			return err
		}
		dirty = true
		return nil
	}
	result := fn(s, save)
	if dirty {
		for i := range s.Records {
			r := &s.Records[i]
			b, err := recordText(r)
			if err != nil {
				return errors.Join(result, err)
			}
			name := "assignment-" + r.Assignment.ExperimentID + ".textproto"
			old, rerr := os.ReadFile(filepath.Join(j.Dir, name))
			if rerr == nil && bytes.Equal(old, b) {
				continue
			}
			if err = atomicFile(j.Dir, name, b); err != nil {
				return errors.Join(result, err)
			}
		}
	}
	return result
}

// lock takes the exclusive flock, waiting until ctx is done.
func lock(ctx context.Context, fd int) error {
	delay := time.Millisecond
	for {
		err := unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			return err
		}
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return fmt.Errorf("%w: %w", ErrJournalBusy, ctx.Err())
		case <-timer.C:
		}
		delay = min(2*delay, lockPollMaxCst)
	}
}

// sweepPending removes temporary files left by a writer that died between
// CreateTemp and Rename. The caller holds the lock, so none is in use.
func (j Journal) sweepPending() error {
	entries, err := os.ReadDir(j.Dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), pendingPrefix) {
			continue
		}
		if err = os.Remove(filepath.Join(j.Dir, e.Name())); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func readLimited(f *os.File) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(f, treatment.MaxStateBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > treatment.MaxStateBytes {
		return nil, errors.New("journal too large")
	}
	return b, nil
}

// atomicFile writes b to a temporary file in dir, syncs it, renames it over
// name and syncs the directory. On failure the temporary file is removed; a
// crash before that point leaves one for sweepPending.
func atomicFile(dir, name string, b []byte) (err error) {
	f, err := os.CreateTemp(dir, pendingPrefix)
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() {
		if err == nil {
			return
		}
		if rerr := os.Remove(tmp); rerr != nil && !errors.Is(rerr, os.ErrNotExist) {
			err = errors.Join(err, rerr)
		}
	}()
	if _, err = f.Write(b); err != nil {
		return errors.Join(err, f.Close())
	}
	if err = f.Sync(); err != nil {
		return errors.Join(err, f.Close())
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(tmp, filepath.Join(dir, name)); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err = d.Sync(); err != nil {
		return errors.Join(err, d.Close())
	}
	return d.Close()
}
