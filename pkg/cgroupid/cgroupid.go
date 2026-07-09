// Package cgroupid resolves a socket's cgroup v2 id — the value the kernel
// returns in INET_DIAG_CGROUP_ID, which on cgroup v2 equals the inode number of
// the cgroup directory — into the owning container's id and runtime, parsed
// from the cgroup path.
//
// Why a cache: xtcp2's hot loop sees 10k–100k sockets per poll cycle, so a
// per-socket filesystem walk is out of the question. Instead a snapshot of
// cgroup-id -> container is built by walking /sys/fs/cgroup once, kept in an
// atomically-swapped map, and rebuilt (debounced) when a lookup misses — a miss
// means a container started since the last build, so the next cycle picks it up.
//
// cgroup v2 / modern kernels only: on cgroup v2 the cgroup id from
// INET_DIAG_CGROUP_ID is the cgroup directory inode, so we key the map on
// os.Stat(dir).Ino. On cgroup v1 (or if /sys/fs/cgroup isn't the unified
// hierarchy) resolution simply misses and container_id stays empty.
package cgroupid

import (
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// DefaultRoot is the standard cgroup v2 unified-hierarchy mount point.
const DefaultRoot = "/sys/fs/cgroup"

// rebuildDebounce bounds how often a cache miss can trigger a rebuild, so a
// burst of misses (many sockets in a just-started container) coalesces into one
// walk.
const rebuildDebounce = time.Second

// Entry is the resolved container identity for a cgroup.
type Entry struct {
	ContainerID string
	Runtime     string
}

// Resolver holds an atomically-swapped snapshot of cgroup id -> Entry. The zero
// value is not usable; call New.
type Resolver struct {
	root string

	snapshot atomic.Pointer[map[uint64]Entry]

	rebuildMu     sync.Mutex // serializes rebuilds
	lastBuildNano atomic.Int64
	rebuilding    atomic.Bool
}

// New returns a Resolver rooted at root (use DefaultRoot in production) and does
// an initial build so the first poll cycle already has data.
func New(root string) *Resolver {
	if root == "" {
		root = DefaultRoot
	}
	r := &Resolver{root: root}
	r.rebuild()
	return r
}

// Resolve returns the container id + runtime for a socket's cgroup id, or empty
// strings when the cgroup isn't a recognised container (host process, cgroup v1,
// or a container that appeared after the last build). On a miss it schedules a
// debounced rebuild so subsequent cycles resolve newly-started containers. It is
// safe for concurrent use and never blocks on the filesystem.
func (r *Resolver) Resolve(cgroupID uint64) (containerID, runtime string) {
	if m := r.snapshot.Load(); m != nil {
		if e, ok := (*m)[cgroupID]; ok {
			return e.ContainerID, e.Runtime
		}
	}
	r.scheduleRebuild()
	return "", ""
}

// scheduleRebuild kicks off at most one background rebuild per rebuildDebounce,
// without blocking the caller (the hot path).
func (r *Resolver) scheduleRebuild() {
	last := r.lastBuildNano.Load()
	if time.Duration(time.Now().UnixNano()-last) < rebuildDebounce {
		return
	}
	if !r.rebuilding.CompareAndSwap(false, true) {
		return // a rebuild is already in flight
	}
	go func() {
		defer r.rebuilding.Store(false)
		r.rebuild()
	}()
}

// rebuild walks the cgroup tree and atomically swaps in a fresh snapshot.
func (r *Resolver) rebuild() {
	r.rebuildMu.Lock()
	defer r.rebuildMu.Unlock()

	m := make(map[uint64]Entry)
	_ = filepath.WalkDir(r.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable subtrees, keep walking
		}
		if !d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(r.root, path)
		if rerr != nil {
			return nil
		}
		id, runtime := parseContainerFromPath(rel)
		if id == "" {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		st, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			return nil
		}
		m[st.Ino] = Entry{ContainerID: id, Runtime: runtime}
		return nil
	})

	r.snapshot.Store(&m)
	r.lastBuildNano.Store(time.Now().UnixNano())
}

// parseContainerFromPath finds the container a cgroup belongs to by scanning the
// path components from deepest to shallowest and returning the first that names
// a container. Scanning deepest-first means a socket in a sub-cgroup of a
// container (e.g. docker-<id>.scope/init) still resolves to that container.
func parseContainerFromPath(rel string) (containerID, runtime string) {
	parts := strings.Split(rel, string(filepath.Separator))
	for i := len(parts) - 1; i >= 0; i-- {
		if id, rt := parseLeaf(parts[i]); id != "" {
			return id, rt
		}
	}
	return "", ""
}

// parseLeaf extracts a container id + runtime from a single cgroup path
// component. It recognises the common systemd-driver scope names and the
// cgroupfs-driver bare-hex leaf. Returns "" when the component isn't a
// container cgroup.
func parseLeaf(name string) (containerID, runtime string) {
	// systemd cgroup driver: "<runtime>-<id>.scope".
	if scope, ok := strings.CutSuffix(name, ".scope"); ok {
		var rt, hexPart string
		switch {
		case strings.HasPrefix(scope, "docker-"):
			rt, hexPart = "docker", strings.TrimPrefix(scope, "docker-")
		case strings.HasPrefix(scope, "cri-containerd-"):
			rt, hexPart = "containerd", strings.TrimPrefix(scope, "cri-containerd-")
		case strings.HasPrefix(scope, "crio-"):
			rt, hexPart = "crio", strings.TrimPrefix(scope, "crio-")
		case strings.HasPrefix(scope, "libpod-"):
			rt, hexPart = "podman", strings.TrimPrefix(scope, "libpod-")
		default:
			return "", ""
		}
		// Only a valid 64-hex id counts; otherwise it's not a container scope
		// we recognise (don't report a runtime with an empty id).
		if id := validHex(hexPart); id != "" {
			return id, rt
		}
		return "", ""
	}
	// cgroupfs driver: a bare 64-hex directory (Docker "/docker/<id>",
	// Kubernetes "/kubepods/.../<id>"). Runtime isn't encoded in the leaf, so
	// report the generic "cgroupfs".
	if id := validHex(name); id != "" {
		return id, "cgroupfs"
	}
	return "", ""
}

// validHex returns s if it's a 64-char lowercase-hex container id, else "".
// Container ids are always 64 hex chars; requiring that avoids matching
// unrelated cgroup directories.
func validHex(s string) string {
	if len(s) != 64 {
		return ""
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return ""
		}
	}
	return s
}
