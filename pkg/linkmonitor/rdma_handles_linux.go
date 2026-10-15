package linkmonitor

import (
	"bytes"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
	"golang.org/x/sys/unix"
)

// rdmaFiles opens and reads RDMA counter files. Descriptors are raw: an
// *os.File would register each sysfs file with the runtime poller (two fcntl
// and two epoll_ctl calls per open) for files that never block that way.
// Tests substitute errors a temporary directory cannot produce.
type rdmaFiles interface {
	open(path string) (int, error)
	pread(fd int, buf []byte) (int, error)
	close(fd int) error
}

type sysfsFiles struct{}

func (sysfsFiles) open(path string) (int, error) {
	for {
		fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC, 0)
		if err == nil {
			return fd, nil
		}
		if err != unix.EINTR {
			return -1, &fs.PathError{Op: "open", Path: path, Err: err}
		}
	}
}

// pread at offset 0 rereads the attribute: each sysfs read from offset 0
// calls the attribute's show again, so a held descriptor sees fresh values.
func (sysfsFiles) pread(fd int, buf []byte) (int, error) {
	for {
		n, err := unix.Pread(fd, buf, 0)
		if err != unix.EINTR {
			return n, err
		}
	}
}

func (sysfsFiles) close(fd int) error { return unix.Close(fd) }

const (
	// rdmaHandleLimit bounds the descriptors held across polls; ports beyond
	// it open, read and close each poll, as before descriptors were held.
	rdmaHandleLimit = 4096
	// Slot states besides an open descriptor.
	rdmaSlotClosed = -1
	rdmaSlotAbsent = -2 // an optional file the port does not have
)

// rdmaDeviceFiles are the device attributes the info owner reads, relative
// to the port directory.
var rdmaDeviceFiles = [...]string{"../../board_id", "../../fw_ver", "../../hca_type"}

// rdmaHandleKey has the collector kind because on demand the error counters
// and the rest of a port's files are separate jobs with schemas of their own;
// one entry for both would reopen every descriptor as they alternate.
type rdmaHandleKey struct {
	device, hardware string
	port             uint32
	kind             model.CollectorKind
}

// rdmaHandles holds counter file descriptors across polls, shared by every
// worker so a port has one set whichever worker polls it. Entries live for
// one inventory generation: a resync, which every device replacement causes,
// closes the older ones.
type rdmaHandles struct {
	files  rdmaFiles
	limit  int64
	held   atomic.Int64
	mu     sync.Mutex
	ports  map[rdmaHandleKey]*rdmaPortFiles
	newest uint64
	refs   int
	closed bool
}

func newRDMAHandles(files rdmaFiles) *rdmaHandles {
	return &rdmaHandles{files: files, limit: rdmaHandleLimit, ports: make(map[rdmaHandleKey]*rdmaPortFiles)}
}

// rdmaPortFiles is one port's descriptors, one slot per file a poll reads:
// the port's schema entries in schema order, then, when the entry carries
// the port's metadata, rate and the device attributes if the port owns the
// device's info series.
type rdmaPortFiles struct {
	mu      sync.Mutex
	handles *rdmaHandles
	// cached entries keep their descriptors after a poll; retired ones were
	// closed by a sweep or Close and must not be reopened.
	cached, retired, transient bool
	generation                 uint64
	schema                     *model.StatisticSchema
	owner, metadata            bool
	names                      []string
	fds                        []int
	// counters is the number of schema slots; hw is whether any of them is a
	// counter STAT_GET can replace.
	counters int
	hw       bool
	// netlinkOff is a STAT_GET failure that will not heal by itself; it
	// lasts for the schema, so a resync tries netlink again.
	netlinkOff bool
}

func (h *rdmaHandles) retain() *rdmaHandles {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.refs++
	return h
}

// release closes every descriptor once the last collector is done.
func (h *rdmaHandles) release() error {
	h.mu.Lock()
	h.refs--
	if h.refs > 0 || h.closed {
		h.mu.Unlock()
		return nil
	}
	h.closed = true
	entries := make([]*rdmaPortFiles, 0, len(h.ports))
	for key, e := range h.ports {
		entries = append(entries, e)
		delete(h.ports, key)
	}
	h.mu.Unlock()
	return retireRDMAPorts(entries)
}

// retireRDMAPorts waits for each entry's poll to finish, then closes it.
func retireRDMAPorts(entries []*rdmaPortFiles) error {
	var err error
	for _, e := range entries {
		e.mu.Lock()
		err = errors.Join(err, e.closeAll())
		e.retired = true
		e.mu.Unlock()
	}
	return err
}

// acquire returns the entry of p's files read by kind, locked. A port from an older generation than one
// already seen, or any port after Close, gets a transient entry that is
// closed on release.
func (h *rdmaHandles) acquire(p model.RDMAPort, kind model.CollectorKind) (*rdmaPortFiles, error) {
	key := rdmaHandleKey{device: p.Device, hardware: p.Hardware, port: p.Port, kind: kind}
	var stale []*rdmaPortFiles
	h.mu.Lock()
	if p.Generation > h.newest {
		h.newest = p.Generation
		for k, e := range h.ports {
			if e.generation < h.newest {
				stale = append(stale, e)
				delete(h.ports, k)
			}
		}
	}
	e := h.ports[key]
	if e != nil && e.generation != p.Generation {
		e = nil
	}
	if e == nil && !h.closed && p.Generation == h.newest {
		e = &rdmaPortFiles{handles: h, cached: true, generation: p.Generation}
		h.ports[key] = e
	}
	h.mu.Unlock()
	err := retireRDMAPorts(stale)
	if e != nil {
		e.mu.Lock()
		if !e.retired {
			return e, err
		}
		e.mu.Unlock()
	}
	e = &rdmaPortFiles{handles: h, generation: p.Generation}
	e.mu.Lock()
	return e, err
}

// release ends a poll. Transient entries, and cached ones over the limit,
// close their descriptors.
func (e *rdmaPortFiles) release() error {
	var err error
	if !e.cached || e.transient {
		err = e.closeAll()
		e.schema, e.transient = nil, false
	}
	e.mu.Unlock()
	return err
}

// remap lays the slots out for schema's entries lo to hi, then the metadata
// files when metadata is set. Descriptors of files still named are kept, so
// pruning one unreadable file reopens nothing.
func (e *rdmaPortFiles) remap(schema *model.StatisticSchema, lo, hi int, owner, metadata bool) error {
	owner = owner && metadata
	if e.schema == schema && e.owner == owner && e.metadata == metadata && e.names != nil {
		return nil
	}
	names := make([]string, 0, hi-lo+1+len(rdmaDeviceFiles))
	names = append(names, schema.Names[lo:hi]...)
	if metadata {
		names = append(names, "rate")
	}
	if owner {
		names = append(names, rdmaDeviceFiles[:]...)
	}
	old := make(map[string]int, len(e.names))
	for i, name := range e.names {
		old[name] = e.fds[i]
	}
	fds := make([]int, len(names))
	for i, name := range names {
		fds[i] = rdmaSlotClosed
		if fd, ok := old[name]; ok {
			fds[i] = fd
			delete(old, name)
		}
	}
	var err error
	for _, fd := range old {
		err = errors.Join(err, e.closeFD(fd))
	}
	if e.schema != schema {
		e.netlinkOff = false
	}
	e.schema, e.owner, e.metadata, e.names, e.fds, e.counters = schema, owner, metadata, names, fds, hi-lo
	e.hw = slices.ContainsFunc(names[:hi-lo], rdmaStatCounter)
	if e.cached && e.handles.held.Load()+int64(len(names)) > e.handles.limit {
		e.transient = true
	}
	return err
}

func (e *rdmaPortFiles) openSlot(i int, path string) error {
	fd, err := e.handles.files.open(path)
	if err != nil {
		return err
	}
	e.fds[i] = fd
	e.handles.held.Add(1)
	return nil
}

func (e *rdmaPortFiles) closeFD(fd int) error {
	if fd < 0 {
		return nil
	}
	e.handles.held.Add(-1)
	return e.handles.files.close(fd)
}

// closeAll closes every descriptor; the next poll reopens them under the
// identity guard.
func (e *rdmaPortFiles) closeAll() error {
	var err error
	for i, fd := range e.fds {
		err = errors.Join(err, e.closeFD(fd))
		e.fds[i] = rdmaSlotClosed
	}
	return err
}

// rdmaSlotRead is one slot's result for a poll. Counter slots keep the parsed
// value; the rate and device attributes keep their text.
type rdmaSlotRead struct {
	text   string
	value  uint64
	parsed bool
	// skip is a counter this poll takes from STAT_GET instead.
	skip bool
	err  error
}

// needsOpen reports a closed slot the poll needs among those only selects;
// nil selects every slot.
func (e *rdmaPortFiles) needsOpen(reads []rdmaSlotRead, only func(string) bool) bool {
	for i, fd := range e.fds {
		if (only == nil || only(e.names[i])) && fd == rdmaSlotClosed && !reads[i].skip && reads[i].err == nil {
			return true
		}
	}
	return false
}

// open opens the closed slots under path, keeping each error for its slot so
// the build keeps the old per-file semantics. A missing rate or device
// attribute stays absent for the entry's life.
func (e *rdmaPortFiles) open(path string, reads []rdmaSlotRead, only func(string) bool) {
	for i, fd := range e.fds {
		if only != nil && !only(e.names[i]) || fd != rdmaSlotClosed || reads[i].skip || reads[i].err != nil {
			continue
		}
		err := e.openSlot(i, filepath.Join(path, e.names[i]))
		if i >= e.counters && errors.Is(err, fs.ErrNotExist) {
			e.fds[i] = rdmaSlotAbsent
			continue
		}
		reads[i].err = err
	}
}

// read rereads slot i into buf with one pread. A sysfs attribute's show
// fills at most a page, which a single read at offset 0 returns whole.
func (e *rdmaPortFiles) read(i int, buf []byte, r *rdmaSlotRead) {
	if r.skip || r.err != nil {
		return
	}
	if e.fds[i] == rdmaSlotAbsent {
		r.err = fs.ErrNotExist
		return
	}
	n, err := e.handles.files.pread(e.fds[i], buf)
	if err != nil {
		r.err = &fs.PathError{Op: "read", Path: e.names[i], Err: err}
		return
	}
	if n > rdmaScalarLimit {
		r.err = linuxio.ErrLimit
		return
	}
	text := bytes.TrimSpace(buf[:n])
	if i >= e.counters {
		r.text = string(text)
		return
	}
	r.value, err = strconv.ParseUint(string(text), 10, 64)
	r.parsed = err == nil
}

const (
	rdmaHWGroup = "hw_counters/"
	// rdmaIndexZero is hw_counters index 0 on every mlx5 device. Reading it
	// first takes the driver's full refresh, which stamps the 10 ms cache
	// the rest of the files then hit; other drivers refresh on any index.
	rdmaIndexZero = rdmaHWGroup + "rx_write_requests"
)

// rdmaStatCounter reports a schema entry STAT_GET reports: every hw_counters
// file except lifespan, which is the cache setting, not a counter.
func rdmaStatCounter(name string) bool {
	return strings.HasPrefix(name, rdmaHWGroup) && name != rdmaHWGroup+"lifespan"
}

// rdmaPMACounter reports a PortCounters file. Each costs a firmware command
// on mlx5 and none is cached, so these are read in parallel.
func rdmaPMACounter(name string) bool {
	return strings.HasPrefix(name, "counters/") || strings.HasPrefix(name, "counters_ext/")
}
