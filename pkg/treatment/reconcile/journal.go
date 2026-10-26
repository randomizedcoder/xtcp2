package reconcile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"golang.org/x/sys/unix"
)

// Journal serializes the daemon, watchdog and local administrative commands.
// The single atomic snapshot is authoritative. Per-assignment files are human
// readable projections only, never an input to recovery.
type Journal struct{ Dir string }

// WithLock loads the snapshot under the exclusive journal lock, runs fn with
// a save callback that persists the current state atomically, and refreshes
// the per-assignment projections if anything was saved. A snapshot that
// fails validation never reaches fn.
func (j Journal) WithLock(fn func(*State, func() error) error) error {
	if err := os.MkdirAll(j.Dir, 0700); err != nil {
		return err
	}
	fd, err := unix.Open(filepath.Join(j.Dir, ".lock"), unix.O_CREAT|unix.O_RDWR|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	if err = unix.Flock(fd, unix.LOCK_EX|unix.LOCK_NB); err != nil {
		return fmt.Errorf("treatment journal busy: %w", err)
	}
	defer unix.Flock(fd, unix.LOCK_UN)
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
			old, _ := os.ReadFile(filepath.Join(j.Dir, name))
			if bytes.Equal(old, b) {
				continue
			}
			if err = atomicFile(j.Dir, name, b); err != nil {
				return errors.Join(result, err)
			}
		}
	}
	return result
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

func atomicFile(dir, name string, b []byte) error {
	f, err := os.CreateTemp(dir, ".pending-")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
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
	defer d.Close()
	return d.Sync()
}
