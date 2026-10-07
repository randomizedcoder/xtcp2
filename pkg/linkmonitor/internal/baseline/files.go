// Package baseline stores the versioned expected-link count under a lifetime
// advisory lock. It owns persistence, not the policy for learning a count.
package baseline

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

// ErrLocked means another owner holds the baseline's sibling lock file.
var ErrLocked = errors.New("baseline already locked")

type syncedFile interface {
	io.Writer
	Sync() error
	Close() error
	Name() string
}

type syncedDirectory interface {
	Sync() error
	Close() error
}

// operations permits failure injection without replacing process-wide hooks.
type operations struct {
	mkdirAll  func(string, fs.FileMode) error
	lock      func(string) (io.Closer, error)
	open      func(string) (io.ReadCloser, error)
	temporary func(string, string) (syncedFile, error)
	rename    func(string, string) error
	remove    func(string) error
	directory func(string) (syncedDirectory, error)
}

func filesystem() operations {
	return operations{
		mkdirAll:  os.MkdirAll,
		lock:      lockFile,
		open:      func(path string) (io.ReadCloser, error) { return os.Open(path) },
		temporary: func(dir, pattern string) (syncedFile, error) { return os.CreateTemp(dir, pattern) },
		rename:    os.Rename,
		remove:    os.Remove,
		directory: func(path string) (syncedDirectory, error) { return os.Open(path) },
	}
}

func lockFile(path string) (io.Closer, error) {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(file.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		if errors.Is(err, unix.EWOULDBLOCK) {
			err = fmt.Errorf("%w: %w", ErrLocked, err)
		}
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}
