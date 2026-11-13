package nsdiscover

import (
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
		{name: "scan_cap", category: "N", description: "100 namespaces, cap 10",
			pids: pidsN(100, 100), lim: ScanLimits{MaxNamespaces: 10}, expectedStop: ScanCap, expectedFound: 10},
		{name: "cap_exact", category: "B", description: "10 namespaces, cap 10: complete",
			pids: pidsN(50, 10), lim: ScanLimits{MaxNamespaces: 10}, expectedStop: ScanComplete, expectedFound: 10},
		{name: "under_deadline_check", category: "B", description: "255 pids never reach a deadline check",
			pids: pidsN(255, 255), lim: ScanLimits{Deadline: time.Now().Add(-time.Second)}, expectedStop: ScanComplete, expectedFound: 255},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := NewScanner(writeProc(t, tc.pids))
			defer s.Close()
			found := 0
			_, stop := s.ScanLimited(tc.lim, func(Namespace) { found++ })
			if stop != tc.expectedStop || (tc.expectedFound >= 0 && found != tc.expectedFound) {
				t.Errorf("%s: stop=%d found=%d, expected stop=%d found=%d", tc.description, stop, found, tc.expectedStop, tc.expectedFound)
			}
		})
	}
}

// TestPidInNetns: the direct check reconcile makes after an incomplete
// scan.
func TestPidInNetns(t *testing.T) {
	s := NewScanner(writeProc(t, map[int]uint64{42: 4026531999}))
	defer s.Close()
	tests := []struct {
		name        string
		category    string
		description string
		pid         int
		inode       uint64
		expected    bool
	}{
		{name: "in_it", category: "P", description: "pid still in the namespace", pid: 42, inode: 4026531999, expected: true},
		{name: "moved", category: "N", description: "pid now in another namespace", pid: 42, inode: 4026531998},
		{name: "gone", category: "N", description: "pid exited", pid: 43, inode: 4026531999},
		{name: "pid_zero", category: "B", description: "pid 0 (bind mount only) is never confirmed this way", pid: 0, inode: 4026531999},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := s.PidInNetns(tc.pid, tc.inode); got != tc.expected {
				t.Errorf("%s: %v, expected %v", tc.description, got, tc.expected)
			}
		})
	}
}
