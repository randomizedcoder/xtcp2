package attribution

import (
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unsafe"

	"github.com/randomizedcoder/xtcp2/pkg/treatment/reconcile"
	"golang.org/x/sys/unix"
)

// WatchConfig tells a Watcher where the standalone agent writes its journal.
type WatchConfig struct {
	// Dir is the journal directory (local.Config.Directory of the agent).
	Dir string
	// Location is this daemon's own telemetry label. When it is set the
	// journal's location is never written onto records and a difference is
	// logged once. When it is empty (no -location/LOCATION) the journal's
	// location is adopted so evidence queries can find the host.
	Location string
	// Poll bounds staleness when inotify is unavailable, the directory does
	// not exist yet or an event was missed. Zero means 30s.
	Poll time.Duration
	// NetnsInode is the namespace the agent's routes live in. Zero means this
	// process's own namespace, which is right whenever daemon and agent run
	// in the host namespace, as the agent requires.
	NetnsInode uint64
	// Log receives diagnostics; nil means log.Printf.
	Log func(format string, args ...any)
}

// Watcher feeds a Stamper from a journal directory written by another
// process. It reads the snapshot without the journal lock (the file is
// replaced atomically), on an inotify event for the snapshot's name, on
// Wake, and on the poll interval as a fallback. A missing or invalid
// snapshot publishes a nil index: with no trustworthy journal there is no
// agent to speak for, and the daemon degrades to an observation-only
// producer rather than attributing from stale state.
type Watcher struct {
	cfg         WatchConfig
	stamper     *Stamper
	wake        chan struct{}
	mu          sync.Mutex
	lastModTime time.Time
	lastSize    int64
	lastErr     string
	warnedFor   string
	// warnedUnbound is set once a journal without a collection nonce has
	// been logged, so the warning is not repeated on every refresh.
	warnedUnbound bool
}

// NewWatcher validates the configuration and resolves the namespace. It
// does not read the journal; Run or Refresh does.
func NewWatcher(cfg WatchConfig, s *Stamper) (*Watcher, error) {
	if cfg.Dir == "" || s == nil {
		return nil, errors.New("attribution: journal directory and stamper are required")
	}
	if cfg.Poll <= 0 {
		cfg.Poll = 30 * time.Second
	}
	if cfg.Log == nil {
		cfg.Log = log.Printf
	}
	if cfg.NetnsInode == 0 {
		var st unix.Stat_t
		if err := unix.Stat("/proc/self/ns/net", &st); err != nil {
			return nil, fmt.Errorf("attribution: network namespace: %w", err)
		}
		cfg.NetnsInode = st.Ino
	}
	return &Watcher{cfg: cfg, stamper: s, wake: make(chan struct{}, 1)}, nil
}

// Wake asks Run to re-read the snapshot now; concurrent wakes coalesce.
func (w *Watcher) Wake() {
	select {
	case w.wake <- struct{}{}:
	default:
	}
}

// Refresh re-reads the snapshot and publishes the resulting index. force
// re-reads even when size and modification time are unchanged. The error
// is the load failure, if any; it is also logged once per distinct message.
func (w *Watcher) Refresh(force bool) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	path := filepath.Join(w.cfg.Dir, reconcile.SnapshotFile)
	info, err := os.Lstat(path)
	if err == nil && !force && info.ModTime().Equal(w.lastModTime) && info.Size() == w.lastSize && w.stamper.Current() != nil {
		return nil
	}
	if err != nil {
		w.lastModTime, w.lastSize = time.Time{}, 0
		w.stamper.Publish(nil)
		if errors.Is(err, os.ErrNotExist) {
			w.note("")
			return nil
		}
		w.note(err.Error())
		return err
	}
	s, err := reconcile.ReadSnapshot(w.cfg.Dir)
	if err != nil {
		w.lastModTime, w.lastSize = time.Time{}, 0
		w.stamper.Publish(nil)
		w.note(err.Error())
		return err
	}
	w.lastModTime, w.lastSize = info.ModTime(), info.Size()
	w.note("")
	if s.Location != "" && w.cfg.Location != "" && s.Location != w.cfg.Location && w.warnedFor != s.Location {
		w.warnedFor = s.Location
		w.cfg.Log("treatment attribution: journal location %q differs from this daemon's -location %q; records keep %q", s.Location, w.cfg.Location, w.cfg.Location)
	}
	adoptLocation := w.cfg.Location == ""
	if adoptLocation && s.Location != "" && w.warnedFor != s.Location {
		w.warnedFor = s.Location
		w.cfg.Log("treatment attribution: this daemon has no -location; records take the journal's %q", s.Location)
	}
	if s.CollectionID == "" && !w.warnedUnbound {
		w.warnedUnbound = true
		w.cfg.Log("treatment attribution: the journal carries no collection nonce; records are stamped without one and the controller will not count them as evidence until the agent rewrites the journal")
	}
	// The nonce is the agent's, read from the journal, so the controller
	// can bind these records to the session the agent reported.
	w.stamper.Publish(Build(s, Identity{AgentID: s.AgentID, Location: s.Location, BootID: s.BootID, CollectionID: s.CollectionID, NetnsInode: w.cfg.NetnsInode}, adoptLocation))
	return nil
}

// refreshRetryCst bounds the poll interval while the journal is unreadable.
const refreshRetryCst = 5 * time.Second

// pollAfter is the wait before the next unprompted refresh: the configured
// period, or sooner while the last refresh failed so a journal that becomes
// readable again is noticed without waiting a full period. Refresh has
// already logged the failure once.
func (w *Watcher) pollAfter(lastErr error) time.Duration {
	if lastErr != nil {
		return min(w.cfg.Poll, refreshRetryCst)
	}
	return w.cfg.Poll
}

// note logs a load failure once per distinct message and a recovery once.
func (w *Watcher) note(msg string) {
	if msg == w.lastErr {
		return
	}
	if msg == "" {
		w.cfg.Log("treatment attribution: journal %s readable again", w.cfg.Dir)
	} else {
		w.cfg.Log("treatment attribution: %s; records are not attributed until the journal is valid", msg)
	}
	w.lastErr = msg
}

// Run watches until ctx is done. It never returns an error: attribution is
// best effort and a broken watch degrades to polling.
func (w *Watcher) Run(ctx interface {
	Done() <-chan struct{}
	Err() error
}) {
	refreshed := w.Refresh(true)
	fd, err := unix.InotifyInit1(unix.IN_CLOEXEC | unix.IN_NONBLOCK)
	if err != nil {
		w.cfg.Log("treatment attribution: inotify unavailable (%v); polling every %s", err, w.cfg.Poll)
		fd = -1
	} else {
		defer func() {
			if cerr := unix.Close(fd); cerr != nil {
				w.cfg.Log("treatment attribution: inotify close: %v", cerr)
			}
		}()
	}
	watching := false
	addWatch := func() {
		if fd < 0 || watching {
			return
		}
		_, err := unix.InotifyAddWatch(fd, w.cfg.Dir, unix.IN_MOVED_TO|unix.IN_CLOSE_WRITE|unix.IN_CREATE|unix.IN_DELETE|unix.IN_DELETE_SELF|unix.IN_MOVE_SELF|unix.IN_ONLYDIR)
		watching = err == nil
	}
	addWatch()
	buf := make([]byte, 64<<10)
	timer := time.NewTimer(w.pollAfter(refreshed))
	defer timer.Stop()
	events := make(chan bool, 1) // true: the snapshot changed; false: the directory itself went away
	if fd >= 0 {
		go w.readEvents(ctx, fd, buf, events)
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-w.wake:
			refreshed = w.Refresh(true)
		case changed := <-events:
			if !changed {
				watching = false
			}
			refreshed = w.Refresh(true)
		case <-timer.C:
			addWatch()
			refreshed = w.Refresh(false)
		}
		if !timer.Stop() {
			select {
			case <-timer.C:
			default:
			}
		}
		timer.Reset(w.pollAfter(refreshed))
	}
}

// readEvents turns inotify events for the snapshot into notifications.
func (w *Watcher) readEvents(ctx interface{ Err() error }, fd int, buf []byte, events chan<- bool) {
	for ctx.Err() == nil {
		p := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
		if _, err := unix.Poll(p, 1000); err != nil && err != unix.EINTR {
			return
		}
		if p[0].Revents == 0 {
			continue
		}
		n, err := unix.Read(fd, buf)
		if err == unix.EAGAIN || err == unix.EINTR {
			continue
		}
		if err != nil {
			return
		}
		changed, gone := false, false
		for off := 0; off+unix.SizeofInotifyEvent <= n; {
			ev := (*unix.InotifyEvent)(unsafe.Pointer(&buf[off]))
			nameLen := int(ev.Len)
			name := ""
			if nameLen > 0 && off+unix.SizeofInotifyEvent+nameLen <= n {
				name = unix.ByteSliceToString(buf[off+unix.SizeofInotifyEvent : off+unix.SizeofInotifyEvent+nameLen])
			}
			switch {
			case ev.Mask&(unix.IN_DELETE_SELF|unix.IN_MOVE_SELF|unix.IN_IGNORED) != 0:
				gone = true
			case ev.Mask&unix.IN_Q_OVERFLOW != 0 || name == reconcile.SnapshotFile:
				changed = true
			}
			off += unix.SizeofInotifyEvent + nameLen
		}
		if gone {
			select {
			case events <- false:
			default:
			}
		} else if changed {
			select {
			case events <- true:
			default:
			}
		}
	}
}
