package xtcp

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

// netns bind mounts appear under /run/netns and /run/docker/netns as pods
// start. When xtcp2 runs in a container, those dirs are bind mounts too, and
// with Docker's default rprivate propagation the container sees only the
// namespaces that existed when it started: newer ones are not collected
// (setns EINVAL on the stale file) and their container name/image enrichment
// is empty. Prod v2.0.0 ran this way (one host saw 10 of 17). The fix is
// deployment side (`:ro,rslave`); this check makes it visible.

// mountPropagation reports whether the mount holding dir receives mounts made
// elsewhere: true for shared or slave (shared:N / master:N in mountinfo),
// false for private. found is false when no mount in mountinfo holds dir.
//
// mountinfo lines are: id parent major:minor root mountpoint options
// [optional fields...] - fstype source superoptions. The mount holding dir is
// the one with the longest mount point that is dir or a parent of it; of
// equal mount points the last listed is on top.
func mountPropagation(mountinfo []byte, dir string) (receives, found bool) {
	dir = filepath.Clean(dir)
	best := -1
	for _, line := range strings.Split(string(mountinfo), "\n") {
		f := strings.Fields(line)
		if len(f) < 7 {
			continue
		}
		point := unescapeMountinfo(f[4])
		if !pathWithin(dir, point) || len(point) < best {
			continue
		}
		best = len(point)
		found = true
		receives = false
		for _, opt := range f[6:] {
			if opt == "-" {
				break
			}
			if strings.HasPrefix(opt, "shared:") || strings.HasPrefix(opt, "master:") {
				receives = true
			}
		}
	}
	return receives, found
}

// pathWithin reports whether dir is point or below it.
func pathWithin(dir, point string) bool {
	if point == "/" || dir == point {
		return true
	}
	return strings.HasPrefix(dir, point+"/")
}

// unescapeMountinfo undoes the kernel's octal escapes (\040 space, \011 tab,
// \012 newline, \134 backslash) in a mountinfo path.
func unescapeMountinfo(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && isOctal(s[i+1]) && isOctal(s[i+2]) && isOctal(s[i+3]) {
			b.WriteByte((s[i+1]-'0')<<6 | (s[i+2]-'0')<<3 | (s[i+3] - '0'))
			i += 3
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

func isOctal(c byte) bool { return c >= '0' && c <= '7' }

// checkNetnsPropagation warns, and sets
// xtcp_gauges{function="checkNetnsPropagation",variable="private",type=<dir>}
// to 1, for each netns dir on a private mount. Runs after InitPromethus.
func (x *XTCP) checkNetnsPropagation() {
	mountinfo, err := os.ReadFile(procRoot + "/self/mountinfo")
	if err != nil {
		if x.debugLevel > 10 {
			log.Printf("checkNetnsPropagation: %v", err)
		}
		return
	}
	for _, dir := range netNsCandidateDirs {
		if _, ok := x.netNsDirs.Load(dir); !ok {
			continue
		}
		clean := filepath.Clean(dir)
		receives, found := mountPropagation(mountinfo, clean)
		private := found && !receives
		value := 0.0
		if private {
			value = 1
			log.Printf("WARN: netns dir %s is a private mount: network namespaces created after xtcp2 started are invisible to it (not collected, no container name/image); mount it with rslave propagation, e.g. docker -v %s:%s:ro,rslave", clean, clean, clean)
		}
		x.pGV.WithLabelValues("checkNetnsPropagation", "private", clean).Set(value)
	}
}
