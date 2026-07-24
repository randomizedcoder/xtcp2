package nsdiscover

import (
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"
)

// Resolver maps a network-namespace inode to a best-effort human name for record
// labeling. Since Method B keys namespaces by inode (not by a filesystem path),
// the friendly name is reconstructed here:
//
//  1. a bind-mount name — if the namespace happens to be named under one of the
//     configured dirs (/run/netns, /run/docker/netns), that basename;
//  2. else a container id parsed from /proc/<pid>/cgroup;
//  3. else the synthetic "netns:[<inode>]".
//
// Not safe for concurrent use. Call Refresh once per discovery scan (it rebuilds
// the inode→bind-name index), then Name per namespace.
type Resolver struct {
	procRoot  string
	netnsDirs []string
	byInode   map[uint64]string // bind-mount name index, rebuilt by Refresh
}

// NewResolver builds a resolver reading pids under procRoot (typically "/proc")
// and looking for bind-mount names under netnsDirs (e.g. /run/netns,
// /run/docker/netns). netnsDirs may be empty (then names come from cgroup or the
// synthetic fallback only).
func NewResolver(procRoot string, netnsDirs []string) *Resolver {
	return &Resolver{
		procRoot:  procRoot,
		netnsDirs: netnsDirs,
		byInode:   make(map[uint64]string),
	}
}

// Refresh rebuilds the inode→bind-name index by statting every entry under each
// configured netns dir. Cheap (O(named namespaces)) and reuses the map. Call it
// once per discovery scan before any Name lookups so freshly-named namespaces
// are picked up.
func (r *Resolver) Refresh() {
	clear(r.byInode)
	for _, dir := range r.netnsDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // a missing dir (e.g. no docker) is normal
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			var st unix.Stat_t
			if err := unix.Stat(filepath.Join(dir, e.Name()), &st); err != nil {
				continue
			}
			// First name wins for a given inode (deterministic across dirs).
			if _, ok := r.byInode[st.Ino]; !ok {
				r.byInode[st.Ino] = e.Name()
			}
		}
	}
}

// Name returns a best-effort human name for the namespace (inode, pid), using
// the index from the last Refresh, then the cgroup of pid, then a synthetic id.
func (r *Resolver) Name(inode uint64, pid int) string {
	if name, ok := r.byInode[inode]; ok {
		return name
	}
	if cid := r.containerID(pid); cid != "" {
		return cid
	}
	return "netns:[" + strconv.FormatUint(inode, 10) + "]"
}

// containerID reads /proc/<pid>/cgroup and returns the first container id found
// (a 64-char lowercase-hex run), or "" if none. Covers the common docker /
// containerd / cri-o / kubernetes cgroup layouts, whose paths embed the 64-hex
// container id (e.g. ".../docker-<id>.scope", ".../cri-containerd-<id>.scope",
// ".../kubepods-.../<id>").
func (r *Resolver) containerID(pid int) string {
	data, err := os.ReadFile(filepath.Join(r.procRoot, strconv.Itoa(pid), "cgroup"))
	if err != nil {
		return ""
	}
	return parseContainerID(data)
}

// parseContainerID finds the first maximal lowercase-hex run of length >= 64 in
// the cgroup file and returns its first 64 chars (the container id). Runtimes
// sometimes suffix the id (e.g. "<id>.scope"); taking exactly 64 hex chars
// normalizes that.
func parseContainerID(cgroup []byte) string {
	run := 0
	start := 0
	for i := 0; i <= len(cgroup); i++ {
		if i < len(cgroup) && isHexLower(cgroup[i]) {
			if run == 0 {
				start = i
			}
			run++
			continue
		}
		if run >= 64 {
			return string(cgroup[start : start+64])
		}
		run = 0
	}
	return ""
}

func isHexLower(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
}
