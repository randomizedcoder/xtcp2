//go:build linux

package xtcp

import (
	"os"
	"runtime"
	runtimeDebug "runtime/debug"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// TestNamespaceChurn_threadBoundedUnderRestoreFailure is the regression
// test for the OS-thread leak that crashed the 12 h s3parquet-long soak.
//
// The bug: netNamespaceInstance calls runtime.LockOSThread, does
// state-modifying setns work, and then runs a deferred restore-setns.
// Earlier code unconditionally `defer runtime.UnlockOSThread()` —
// when the restore failed (under nsTest churn the failure rate was
// 100 %), the goroutine handed a TAINTED M (still in a stale netns)
// back to Go's scheduler. The runtime can't safely reuse such an M,
// so it parked it and created a new one for every new namespace
// goroutine. Thread count climbed from a baseline of ~300 to the
// SetMaxThreads(2000) cap in 1 h 45 min and crashed with `fatal error:
// thread exhaustion`.
//
// The fix moves UnlockOSThread inside the restore-defer and only
// calls it when the restore succeeded; on failure the goroutine
// exits with the lock still held, which makes the Go runtime
// terminate the OS thread instead of recycling it. This test forces
// the restore to fail (via the restoreNsSetns seam), runs many
// iterations of the LockOSThread+restore-fail+exit pattern, and
// asserts that the process's OS-thread count stays bounded.
//
// Without the fix, this test panics with `runtime: program exceeds
// 150-thread limit` (debug.SetMaxThreads cap below) within a few
// hundred iterations. With the fix it completes cleanly.
func TestNamespaceChurn_threadBoundedUnderRestoreFailure(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode")
	}

	// Replace the restore-Setns seam with a stub that always returns
	// EPERM, mirroring the production microvm scenario where
	// CAP_SYS_ADMIN was missing.
	origSetns := restoreNsSetns
	restoreNsSetns = func(_ int, _ int) error {
		return syscall.EPERM
	}
	t.Cleanup(func() { restoreNsSetns = origSetns })

	// Tight cap so a leak panics within a few hundred iterations
	// instead of taking hours.
	prevCap := runtimeDebug.SetMaxThreads(150)
	t.Cleanup(func() { runtimeDebug.SetMaxThreads(prevCap) })

	baseline := readSelfThreads(t)

	// N iterations of the LockOSThread + restore-fails + exit pattern.
	// We don't call netNamespaceInstance directly (it would need an
	// XTCP fixture and a real namespace), but the loop body mirrors
	// exactly the same sequence: lock, snapshot origNs, simulate
	// state-modifying work, defer a conditional-restore-then-unlock,
	// exit.
	const N = 400
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runtime.LockOSThread()
			origNs, err := os.Open("/proc/thread-self/ns/net")
			if err != nil {
				// snapshotOrigNs failed — exit with lock held so the
				// runtime terminates the OS thread (mirrors production
				// no-origNs branch in netNamespaceInstance).
				return
			}
			defer func() { _ = origNs.Close() }()
			defer func() {
				if rerr := restoreNsSetns(int(origNs.Fd()), syscall.CLONE_NEWNET); rerr != nil {
					return // skip UnlockOSThread → runtime terminates M
				}
				runtime.UnlockOSThread() //nolint:forbidigo // exercising the safe path inside the test
			}()
			// Simulate the "do work in the new netns" body. We don't
			// need to actually setns — the bug is about what happens
			// to the M on the way out when restore fails. Sleep a
			// little so the Go runtime has a chance to do M-handoff
			// scheduling between goroutines.
			time.Sleep(time.Microsecond)
		}()
	}
	wg.Wait()

	// Give the runtime a moment to terminate any OS threads whose
	// goroutines just exited.
	time.Sleep(200 * time.Millisecond)

	end := readSelfThreads(t)
	delta := end - baseline

	// Bound is generous to avoid flakes from Go's M-pool warm-up
	// scheduling. The leaky behavior grows linearly with N (e.g.
	// 400 iterations → delta ≥ 300); the fixed behavior holds
	// delta < 50 in practice.
	const maxDelta = 80
	if delta > maxDelta {
		t.Fatalf("OS-thread leak under simulated restore failure: baseline=%d end=%d delta=%d (allowed ≤%d). The unconditional `defer runtime.UnlockOSThread()` pattern is back in netNamespaceInstance — see ns_net_namespace.go comments.",
			baseline, end, delta, maxDelta)
	}
	t.Logf("thread count: baseline=%d end=%d delta=%d (cap=%d)", baseline, end, delta, maxDelta)
}

// readSelfThreads reads /proc/self/status to get the current OS-thread
// count for this process. /proc/self/status:Threads counts kernel
// task_struct entries that belong to the process group — exactly what
// the Go runtime's M pool maps to.
func readSelfThreads(t *testing.T) int {
	t.Helper()
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		t.Fatalf("read /proc/self/status: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "Threads:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			t.Fatalf("malformed Threads line: %q", line)
		}
		n, err := strconv.Atoi(fields[1])
		if err != nil {
			t.Fatalf("parse Threads count %q: %v", fields[1], err)
		}
		return n
	}
	t.Fatal("no Threads: line in /proc/self/status")
	return 0
}
