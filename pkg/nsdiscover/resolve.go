package nsdiscover

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

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
	pathByIno map[uint64]string // bind-mount path index, rebuilt by Refresh
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
		pathByIno: make(map[uint64]string),
	}
}

// Refresh rebuilds the inode→bind-name and inode→bind-path indexes by statting
// every entry under each configured netns dir. Cheap (O(named namespaces)) and
// reuses the maps. Call it once per discovery scan before any Name / EachBindMount
// lookups so freshly-named namespaces are picked up.
func (r *Resolver) Refresh() {
	clear(r.byInode)
	clear(r.pathByIno)
	for _, dir := range r.netnsDirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // a missing dir (e.g. no docker) is normal
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			path := filepath.Join(dir, e.Name())
			var st unix.Stat_t
			if err := unix.Stat(path, &st); err != nil {
				continue
			}
			// First name/path wins for a given inode (deterministic across dirs).
			if _, ok := r.byInode[st.Ino]; !ok {
				r.byInode[st.Ino] = e.Name()
				r.pathByIno[st.Ino] = path
			}
		}
	}
}

// EachBindMount calls fn once per distinct network-namespace bind mount found by
// the last Refresh, with the namespace inode and the bind-mount file path. The
// path can be opened + setns'd into WITHOUT a live pid (no pid=host requirement),
// which is how xtcp2 discovers container/pod namespaces that /proc-scan (Method B)
// cannot see. Must be called after Refresh; not safe for concurrent use.
func (r *Resolver) EachBindMount(fn func(inode uint64, path string)) {
	for ino, path := range r.pathByIno {
		fn(ino, path)
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

// Owner is the quota owner of pid's namespace: the cgroup v2 path of pid
// (the "0::" line; the first line on a v1-only host), cut just after the
// first component a container runtime names by container id:
//
//	docker-<id>.scope, cri-containerd-<id>.scope, crio-<id>.scope,
//	libpod-<id>.scope, or a bare <id> under /docker or a kubepods slice
//
// and with no such component, its first two components (user-1000.slice
// under user.slice, one service under system.slice). Components are
// matched from the root: a process can create sub-cgroups, named anything,
// only below its own, so it can't change where its path is cut once a
// runtime component is above it. Under a runtime that doesn't name
// cgroups by container id, a process can make new owners by making
// sub-cgroups at the second level.
//
// ok is false when pid's cgroup can't be read (it exited): the caller
// should try again later rather than file it under any owner.
func (r *Resolver) Owner(pid int) (owner string, ok bool) {
	if pid <= 0 {
		return "", false
	}
	data, err := os.ReadFile(filepath.Join(r.procRoot, strconv.Itoa(pid), "cgroup"))
	if err != nil {
		return "", false
	}
	return ownerFromCgroup(data), true
}

// ownerFromCgroup is Owner on a /proc/<pid>/cgroup file.
func ownerFromCgroup(data []byte) string {
	path := cgroupPath(data)
	comps := strings.Split(strings.Trim(path, "/"), "/")
	for i, c := range comps {
		if runtimeComponent(c, i, comps) {
			return "/" + strings.Join(comps[:i+1], "/")
		}
	}
	if len(comps) > 2 {
		comps = comps[:2]
	}
	return "/" + strings.Join(comps, "/")
}

// cgroupPath returns the v2 ("0::") path, else the first line's path.
func cgroupPath(data []byte) string {
	var first string
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) != 3 {
			continue
		}
		if parts[0] == "0" && parts[1] == "" {
			return parts[2]
		}
		if first == "" {
			first = parts[2]
		}
	}
	return first
}

var runtimeScopePrefixes = []string{"docker-", "cri-containerd-", "crio-", "libpod-"}

// runtimeComponent reports whether comps[i] is a runtime's container
// cgroup.
func runtimeComponent(c string, i int, comps []string) bool {
	for _, p := range runtimeScopePrefixes {
		if id, ok := strings.CutPrefix(c, p); ok {
			if id, ok = strings.CutSuffix(id, ".scope"); ok && isContainerID(id) {
				return true
			}
		}
	}
	if i > 0 && isContainerID(c) {
		parent := comps[i-1]
		return parent == "docker" || strings.HasPrefix(parent, "kubepods") || strings.HasPrefix(parent, "pod")
	}
	return false
}

func isContainerID(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := range len(s) {
		if !isHexLower(s[i]) {
			return false
		}
	}
	return true
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
