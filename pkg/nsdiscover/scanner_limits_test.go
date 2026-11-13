package nsdiscover

import (
	"strings"
	"testing"
	"time"
)

// TestScanLimits: the deadline and namespace cap stop a scan and report
// why; the result is complete only when every pid was seen.
func TestScanLimits(t *testing.T) {
	pidsN := func(pids, distinct int) map[int]uint64 {
		m := make(map[int]uint64, pids)
		for i := range pids {
			m[1000+i] = 4026531000 + uint64(i%distinct)
		}
		return m
	}
	tests := []struct {
		name          string
		category      string
		description   string
		pids          map[int]uint64
		lim           ScanLimits
		expectedStop  ScanStop
		expectedFound int // -1: at most the cap, unchecked
	}{
		{name: "completes", category: "P", description: "1000 pids, generous limits",
			pids: pidsN(1000, 100), lim: ScanLimits{Deadline: time.Now().Add(time.Hour), MaxNamespaces: 1000},
			expectedStop: ScanComplete, expectedFound: 100},
		{name: "unlimited", category: "P", description: "zero limits: unlimited",
			pids: pidsN(300, 300), expectedStop: ScanComplete, expectedFound: 300},
		{name: "deadline", category: "N", description: "deadline already passed: stops at the first check (256 pids)",
			pids: pidsN(1000, 1000), lim: ScanLimits{Deadline: time.Now().Add(-time.Second)},
			expectedStop: ScanTimeout, expectedFound: 255},
		{name: "scan_cap", category: "N", description: "100 namespaces, cap 10: 10 reported, every pid still walked",
			pids: pidsN(100, 100), lim: ScanLimits{MaxNamespaces: 10}, expectedStop: ScanCap, expectedFound: 10},
		{name: "keep_past_cap", category: "C", description: "past the cap, tracked/waiting namespaces (Keep) are still reported",
			pids: pidsN(100, 100), lim: ScanLimits{MaxNamespaces: 10, Keep: func(ino uint64) bool { return ino >= 4026531090 }},
			expectedStop: ScanCap, expectedFound: -1},
		{name: "timeout_wins", category: "C", description: "deadline and cap both hit: timeout is reported",
			pids: pidsN(1000, 1000), lim: ScanLimits{Deadline: time.Now().Add(-time.Second), MaxNamespaces: 10},
			expectedStop: ScanTimeout, expectedFound: 10},
		{name: "cap_exact", category: "B", description: "10 namespaces, cap 10: complete",
			pids: pidsN(50, 10), lim: ScanLimits{MaxNamespaces: 10}, expectedStop: ScanComplete, expectedFound: 10},
		{name: "under_deadline_check", category: "B", description: "255 pids never reach a deadline check",
			pids: pidsN(255, 255), lim: ScanLimits{Deadline: time.Now().Add(-time.Second)}, expectedStop: ScanComplete, expectedFound: 255},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewScanner(writeProc(t, tc.pids))
			defer s.Close()
			found, kept, other := 0, 0, 0
			_, stop := s.ScanLimited(tc.lim, func(ns Namespace) {
				found++
				if tc.lim.Keep != nil && tc.lim.Keep(ns.Inode) {
					kept++
				} else {
					other++
				}
			})
			if stop != tc.expectedStop || (tc.expectedFound >= 0 && found != tc.expectedFound) {
				t.Errorf("%s: stop=%d found=%d, expected stop=%d found=%d", tc.description, stop, found, tc.expectedStop, tc.expectedFound)
			}
			if tc.lim.Keep != nil && (kept != 10 || other > tc.lim.MaxNamespaces) {
				t.Errorf("%s: %d Keep reported (expected all 10), %d others (expected <= %d)", tc.description, kept, other, tc.lim.MaxNamespaces)
			}
		})
	}
}

// TestOwnerFromCgroup: the quota owner a cgroup path maps to.
func TestOwnerFromCgroup(t *testing.T) {
	id := strings.Repeat("a", 64)
	fake := strings.Repeat("b", 64)
	tests := []struct {
		name        string
		category    string
		description string
		cgroup      string
		expected    string
	}{
		{name: "docker_systemd", category: "P", description: "docker with the systemd driver",
			cgroup: "0::/system.slice/docker-" + id + ".scope\n", expected: "/system.slice/docker-" + id + ".scope"},
		{name: "docker_cgroupfs", category: "P", description: "docker with the cgroupfs driver",
			cgroup: "0::/docker/" + id + "\n", expected: "/docker/" + id},
		{name: "kubepods", category: "P", description: "kubernetes, containerd, systemd driver",
			cgroup:   "0::/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod1234.slice/cri-containerd-" + id + ".scope\n",
			expected: "/kubepods.slice/kubepods-burstable.slice/kubepods-burstable-pod1234.slice/cri-containerd-" + id + ".scope"},
		{name: "kubepods_cgroupfs", category: "P", description: "kubernetes with the cgroupfs driver",
			cgroup: "0::/kubepods/burstable/pod1234/" + id + "\n", expected: "/kubepods/burstable/pod1234/" + id},
		{name: "nested_fake_id", category: "C", description: "a tenant sub-cgroup named like a container: the outer runtime component wins",
			cgroup: "0::/system.slice/docker-" + id + ".scope/docker-" + fake + ".scope/x\n", expected: "/system.slice/docker-" + id + ".scope"},
		{name: "user_slice", category: "C", description: "no runtime: first two components, so one user's sub-cgroups share an owner",
			cgroup: "0::/user.slice/user-1000.slice/user@1000.service/app.slice/" + fake + "\n", expected: "/user.slice/user-1000.slice"},
		{name: "hex_not_runtime", category: "N", description: "a bare 64-hex component not under docker/kubepods/pod is not a runtime",
			cgroup: "0::/" + fake + "/x/y\n", expected: "/" + fake + "/x"},
		{name: "root", category: "B", description: "the root cgroup",
			cgroup: "0::/\n", expected: "/"},
		{name: "v1_hybrid", category: "B", description: "v1 lines then the v2 line: v2 is used",
			cgroup: "12:pids:/docker/" + fake + "\n0::/system.slice/docker-" + id + ".scope\n", expected: "/system.slice/docker-" + id + ".scope"},
		{name: "v1_only", category: "B", description: "no v2 line: the first line",
			cgroup: "12:pids:/docker/" + id + "\n11:memory:/docker/" + id + "\n", expected: "/docker/" + id},
		{name: "short_id", category: "N", description: "63 hex chars is not an id",
			cgroup: "0::/system.slice/docker-" + id[:63] + ".scope\n", expected: "/system.slice/docker-" + id[:63] + ".scope"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ownerFromCgroup([]byte(tc.cgroup)); got != tc.expected {
				t.Errorf("%s: %q, expected %q", tc.description, got, tc.expected)
			}
		})
	}
}

// TestOwnerUnreadable: a pid whose cgroup can't be read has no owner yet.
func TestOwnerUnreadable(t *testing.T) {
	r := NewResolver(t.TempDir(), nil)
	for _, pid := range []int{0, -1, 12345} {
		if o, ok := r.Owner(pid); ok {
			t.Errorf("pid %d: owner %q ok, expected not ok", pid, o)
		}
	}
}
