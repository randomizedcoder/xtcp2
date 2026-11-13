package xtcp

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/protobuf/types/known/durationpb"
)

// procWithCgroups is a fake /proc: pid → (netns inode, cgroup file).
func procWithCgroups(t *testing.T, pids map[int]struct {
	ino    uint64
	cgroup string
}) string {
	t.Helper()
	proc := t.TempDir()
	for pid, p := range pids {
		dir := filepath.Join(proc, strconv.Itoa(pid))
		if err := os.MkdirAll(filepath.Join(dir, "ns"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("net:["+strconv.FormatUint(p.ino, 10)+"]", filepath.Join(dir, "ns", "net")); err != nil {
			t.Fatal(err)
		}
		if p.cgroup != "" {
			if err := os.WriteFile(filepath.Join(dir, "cgroup"), []byte(p.cgroup), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
	return proc
}

func cgroupOf(id string) string { return "0::/system.slice/docker-" + id + ".scope\n" }

// TestReconcileLimits runs reconcile over a fake /proc with the scan cap,
// owner quota and add rate set.
func TestReconcileLimits(t *testing.T) {
	type pidNS = struct {
		ino    uint64
		cgroup string
	}
	const base = uint64(4026533000)
	idA, idB := strings.Repeat("a", 64), strings.Repeat("b", 64)

	tests := []struct {
		name        string
		category    string
		description string
		pids        map[int]pidNS
		tracked     map[uint64]int // tracked inode → its pid
		maxNetns    uint32
		perOwner    uint32
		adds        uint32
		timeout     time.Duration // 0: a minute
		// expected
		expectedDeleted []uint64
		expectedKept    []uint64
		expectedStores  int
		expectedOwners  map[uint64]string // added inode → owner
		expectedNames   map[uint64]string
		expectedReason  string // reconcile_incomplete reason, "" for none
	}{
		{name: "complete_deletes", category: "P", description: "complete scan: a tracked namespace no longer live is deleted",
			pids: map[int]pidNS{100: {base, ""}}, tracked: map[uint64]int{base: 100, base + 1: 101}, maxNetns: 100,
			expectedDeleted: []uint64{base + 1}, expectedKept: []uint64{base}},
		{name: "capped_keeps_tracked", category: "C", description: "scan cap reached before a tracked namespace in /proc order: still seen (Keep), kept",
			pids: func() map[int]pidNS {
				m := map[int]pidNS{}
				for i := range 20 {
					m[100+i] = pidNS{base + uint64(i), ""}
				}
				return m
			}(),
			tracked: map[uint64]int{base + 19: 119}, maxNetns: 1, // scan cap 4: base+19 unseen
			expectedKept: []uint64{base + 19}, expectedReason: reconcileIncompleteScanCapCst},
		{name: "capped_deletes_gone", category: "N", description: "a capped scan walked every pid: a gone tracked namespace is deleted (no slot leak under a flood), its slot goes to a waiter",
			pids: func() map[int]pidNS {
				m := map[int]pidNS{}
				for i := range 20 {
					m[100+i] = pidNS{base + uint64(i), ""}
				}
				return m
			}(),
			tracked: map[uint64]int{base + 99: 999}, maxNetns: 1,
			expectedDeleted: []uint64{base + 99}, expectedStores: 1, expectedReason: reconcileIncompleteScanCapCst},
		{name: "nested_cgroup_owner", category: "C", description: "a tenant sub-cgroup named like a container: the owner is the outer runtime component",
			pids: map[int]pidNS{
				100: {base, "0::/system.slice/docker-" + idA + ".scope/docker-" + idB + ".scope\n"},
			}, maxNetns: 100, perOwner: 64,
			expectedStores: 1, expectedOwners: map[uint64]string{base: "/system.slice/docker-" + idA + ".scope"}, expectedNames: map[uint64]string{base: idA}},
		{name: "timeout_no_delete", category: "C", description: "a timed-out scan deletes nothing, even a gone namespace",
			pids: func() map[int]pidNS {
				m := map[int]pidNS{}
				for i := range 300 {
					m[100+i] = pidNS{base + uint64(i), ""}
				}
				return m
			}(),
			tracked: map[uint64]int{base + 999: 9999}, maxNetns: 4096, timeout: time.Nanosecond,
			expectedKept: []uint64{base + 999}, expectedStores: -1, expectedReason: reconcileIncompleteTimeoutCst},
		{name: "unreadable_owner_deferred", category: "N", description: "a candidate whose cgroup can't be read is deferred, not filed under an owner",
			pids: map[int]pidNS{100: {base, ""}}, maxNetns: 100, perOwner: 1},
		{name: "owner_quota_reconcile", category: "N", description: "owner A's 5 namespaces against quota 2, B's 1: A gets 2, B 1",
			pids: map[int]pidNS{
				100: {base, cgroupOf(idA)}, 101: {base + 1, cgroupOf(idA)}, 102: {base + 2, cgroupOf(idA)},
				103: {base + 3, cgroupOf(idA)}, 104: {base + 4, cgroupOf(idA)}, 105: {base + 5, cgroupOf(idB)},
			}, maxNetns: 100, perOwner: 2, expectedStores: 3},
		{name: "names_for_admitted_only", category: "P", description: "add rate 1: one name resolved, the waiting ones keep no name",
			pids: map[int]pidNS{100: {base, cgroupOf(idA)}, 101: {base + 1, cgroupOf(idB)}}, maxNetns: 100, adds: 1,
			expectedStores: 1, expectedNames: map[uint64]string{base: idA}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			x := newReconcileTestXTCP(t, procWithCgroups(t, tc.pids))
			x.config.MaxNetns = tc.maxNetns
			x.config.MaxNetnsPerOwner = tc.perOwner
			x.config.MaxNetnsAddsPerReconcile = tc.adds
			timeout := tc.timeout
			if timeout == 0 {
				timeout = time.Minute
			}
			x.config.ReconcileTimeout = durationpb.New(timeout)
			x.pReconcileIncomplete = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "ri"}, []string{"reason"})
			x.pNetnsDropped = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "nd"}, []string{"reason"})
			for ino, pid := range tc.tracked {
				_, cancel := context.WithCancel(context.Background())
				name := "tracked"
				x.nsMap.Store(ino, netNSitem{inode: ino, pid: pid, name: &name, cancel: cancel, socketFD: 5})
			}

			fake := openAndSetnsSyscallsT{
				open:  func(string, int, uint32) (int, error) { return -1, syscall.ENOENT },
				setns: func(int, int) error { return nil },
				close: func(int) error { return nil },
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			var stores int
			withFakeSyscalls(t, fake, func() {
				_, stores = x.reconcile(ctx)
				waitForNsInstanceEnd(t, x, float64(stores))
			})

			if tc.expectedStores >= 0 && stores != tc.expectedStores {
				t.Errorf("%s: stores=%d, expected %d", tc.description, stores, tc.expectedStores)
			}
			for _, ino := range tc.expectedDeleted {
				if _, ok := x.nsMap.Load(ino); ok {
					t.Errorf("%s: %d not deleted", tc.description, ino)
				}
			}
			for _, ino := range tc.expectedKept {
				if _, ok := x.nsMap.Load(ino); !ok {
					t.Errorf("%s: %d deleted", tc.description, ino)
				}
			}
			for ino, owner := range tc.expectedOwners {
				v, ok := x.nsMap.Load(ino)
				if !ok || v.(netNSitem).owner != owner {
					t.Errorf("%s: %d owner %v, expected %q", tc.description, ino, v, owner)
				}
			}
			for ino, name := range tc.expectedNames {
				v, ok := x.nsMap.Load(ino)
				if !ok || *v.(netNSitem).name != name {
					t.Errorf("%s: %d name %v, expected %q", tc.description, ino, v, name)
				}
			}
			if tc.expectedReason != "" {
				if got := testutil.ToFloat64(x.pReconcileIncomplete.WithLabelValues(tc.expectedReason)); got != 1 {
					t.Errorf("%s: reconcile_incomplete{%s}=%v, expected 1", tc.description, tc.expectedReason, got)
				}
			}
			if tc.name == "owner_quota_reconcile" {
				if got := testutil.ToFloat64(x.pNetnsDropped.WithLabelValues(netnsDropOwnerQuotaCst)); got != 3 {
					t.Errorf("%s: owner_quota drops %v, expected 3", tc.description, got)
				}
			}
		})
	}
}
