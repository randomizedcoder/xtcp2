package reconcile

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	pb "github.com/randomizedcoder/xtcp2/gen/go/xtcp_controller"
	"github.com/randomizedcoder/xtcp2/pkg/treatment"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/encoding/prototext"
)

// Journal serializes the daemon, watchdog and local administrative commands.
// The single atomic snapshot is authoritative. Per-assignment files are human
// readable projections only, never an input to recovery.
type Journal struct{ Dir string }

func (j Journal) WithLock(fn func(*pb.LocalState, func() error) error) error {
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
	s := new(pb.LocalState)
	f, err := os.OpenFile(filepath.Join(j.Dir, "state.textproto"), os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if err == nil {
		b, rerr := io.ReadAll(io.LimitReader(f, treatment.MaxStateBytes+1))
		cerr := f.Close()
		if rerr != nil {
			return rerr
		}
		if cerr != nil {
			return cerr
		}
		if len(b) > treatment.MaxStateBytes {
			return errors.New("journal too large")
		}
		if err = prototext.Unmarshal(b, s); err != nil {
			return fmt.Errorf("invalid treatment journal: %w", err)
		}
		if s.Desired != nil {
			if err = treatment.ValidateDesired(s.Desired); err != nil {
				return fmt.Errorf("invalid persisted desired state: %w", err)
			}
		}
		if len(s.Records) > treatment.MaxAssignments*2 {
			return errors.New("too many journal records")
		}
		seen := map[string]bool{}
		for _, r := range s.Records {
			if r.Assignment == nil || !treatment.ValidID(r.Assignment.Id) {
				return errors.New("invalid journal record")
			}
			if seen[r.Assignment.Id] {
				return errors.New("duplicate journal record")
			}
			seen[r.Assignment.Id] = true
			if err = treatment.ValidateTreatment(r.Assignment.Treatment); err != nil {
				return fmt.Errorf("invalid journal treatment: %w", err)
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	dirty := false
	save := func() error {
		b, err := prototext.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(s)
		if err != nil {
			return err
		}
		if len(b) > treatment.MaxStateBytes {
			return errors.New("journal exceeds size limit")
		}
		if err = atomicFile(j.Dir, "state.textproto", b); err != nil {
			return err
		}
		dirty = true
		return nil
	}
	result := fn(s, save)
	if dirty {
		for _, r := range s.Records {
			b, err := prototext.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(r)
			if err != nil {
				return errors.Join(result, err)
			}
			name := "assignment-" + r.Assignment.Id + ".textproto"
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
