package xtcp

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

// mountinfo lines as the kernel writes them.
const (
	miRoot       = "22 1 259:2 / / rw,relatime shared:1 - ext4 /dev/root rw\n"
	miRunShared  = "25 22 0:25 / /run rw,nosuid,nodev shared:5 - tmpfs tmpfs rw,mode=755\n"
	miCtrRoot    = "1180 1100 0:120 / / rw,relatime - overlay overlay rw\n"
	miNetnsPriv  = "1203 1180 0:25 /netns /run/netns ro,nosuid,nodev - tmpfs tmpfs rw,mode=755\n"
	miNetnsSlave = "1203 1180 0:25 /netns /run/netns ro,nosuid,nodev master:5 - tmpfs tmpfs rw,mode=755\n"
	miDockerPriv = "1204 1180 0:25 /docker/netns /run/docker/netns ro,nosuid,nodev - tmpfs tmpfs rw,mode=755\n"
	miDockerSlv  = "1204 1180 0:25 /docker/netns /run/docker/netns ro,nosuid,nodev master:5 - tmpfs tmpfs rw,mode=755\n"
)

func TestMountPropagation(t *testing.T) {
	for _, tc := range []struct {
		name, category, description string
		mountinfo, dir              string
		receives, found             bool
	}{
		{"rslave", "positive", "container bind mount with rslave (master:N)", miCtrRoot + miNetnsSlave, "/run/netns", true, true},
		{"host-shared", "positive", "on the host, not a mount itself: the shared /run holds it", miRoot + miRunShared, "/run/docker/netns", true, true},
		{"rprivate", "negative", "container bind mount with Docker's default rprivate, the prod v2.0.0 shape", miCtrRoot + miNetnsPriv + miDockerSlv, "/run/netns", false, true},
		{"none", "negative", "no mounts listed", "", "/run/netns", false, false},
		{"unbindable", "negative", "unbindable alone is not propagation", miCtrRoot + strings.Replace(miNetnsPriv, "nodev -", "nodev unbindable -", 1), "/run/netns", false, true},
		{"sibling-prefix", "boundary", "a mount at /run/net is not a parent of /run/netns: the private root holds it", miCtrRoot + strings.Replace(miNetnsSlave, " /run/netns ", " /run/net ", 1), "/run/netns", false, true},
		{"unclean-dir", "boundary", "a dir with a doubled and a trailing slash still finds its rslave mount", miCtrRoot + miDockerSlv, "/run//docker/netns/", true, true},
		{"stacked-slave-on-top", "corner", "two mounts on one point: the later one is on top", miCtrRoot + miNetnsPriv + miNetnsSlave, "/run/netns", true, true},
		{"stacked-private-on-top", "corner", "the reverse stack", miCtrRoot + miNetnsSlave + miNetnsPriv, "/run/netns", false, true},
		{"escaped", "corner", "a mount point with a space (\\040)", miCtrRoot + strings.Replace(miNetnsSlave, " /run/netns ", ` /run/my\040netns `, 1), "/run/my netns", true, true},
		{"shared-and-slave", "corner", "shared:N master:M both receive", miCtrRoot + strings.Replace(miNetnsSlave, "master:5", "shared:9 master:5", 1), "/run/netns", true, true},
		{"superoptions", "corner", "a shared: after the - separator is not an optional field", miCtrRoot + strings.Replace(miNetnsPriv, "rw,mode=755", "shared:1", 1), "/run/netns", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s", tc.category, tc.description)
			receives, found := mountPropagation([]byte(tc.mountinfo), tc.dir)
			if receives != tc.receives || found != tc.found {
				t.Fatalf("got receives=%v found=%v, expected receives=%v found=%v", receives, found, tc.receives, tc.found)
			}
		})
	}
}

func TestCheckNetnsPropagation(t *testing.T) {
	dirs := []string{"/run/netns/", "/run/docker/netns/"}
	for _, tc := range []struct {
		name, category, description string
		mountinfo                   *string // nil: no mountinfo file
		present                     []string
		expected                    map[string]float64 // gauge per dir; absent: no series
	}{
		{"rslave", "positive", "both dirs rslave", ptr(miCtrRoot + miNetnsSlave + miDockerSlv), dirs, map[string]float64{"/run/netns": 0, "/run/docker/netns": 0}},
		{"rprivate", "negative", "both dirs rprivate, the prod v2.0.0 shape", ptr(miCtrRoot + miNetnsPriv + miDockerPriv), dirs, map[string]float64{"/run/netns": 1, "/run/docker/netns": 1}},
		{"mixed", "negative", "one of each", ptr(miCtrRoot + miNetnsSlave + miDockerPriv), dirs, map[string]float64{"/run/netns": 0, "/run/docker/netns": 1}},
		{"absent-dir", "boundary", "a dir initSyncMaps didn't find gets no series", ptr(miCtrRoot + miNetnsPriv), dirs[:1], map[string]float64{"/run/netns": 1}},
		{"no-mountinfo", "corner", "mountinfo unreadable: no series, no panic", nil, dirs, map[string]float64{}},
		{"unlisted", "corner", "no mount holds the dir: unknown, not private", ptr(""), dirs, map[string]float64{"/run/netns": 0, "/run/docker/netns": 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s", tc.category, tc.description)
			proc := t.TempDir()
			if tc.mountinfo != nil {
				if err := os.MkdirAll(filepath.Join(proc, "self"), 0o750); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(proc, "self", "mountinfo"), []byte(*tc.mountinfo), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			prevProc, prevDirs := procRoot, netNsCandidateDirs
			t.Cleanup(func() { procRoot, netNsCandidateDirs = prevProc, prevDirs })
			procRoot, netNsCandidateDirs = proc, dirs

			x := newMetricsFixture(t, 0)
			x.netNsDirs = &sync.Map{}
			for _, d := range tc.present {
				x.netNsDirs.Store(d, true)
			}
			x.checkNetnsPropagation()

			if n := testutil.CollectAndCount(x.pGV); n != len(tc.expected) {
				t.Fatalf("%d series, expected %d", n, len(tc.expected))
			}
			for dir, want := range tc.expected {
				if got := testutil.ToFloat64(x.pGV.WithLabelValues("checkNetnsPropagation", "private", dir)); got != want {
					t.Fatalf("%s: got %v, expected %v", dir, got, want)
				}
			}
		})
	}
}

func ptr[T any](v T) *T { return &v }
