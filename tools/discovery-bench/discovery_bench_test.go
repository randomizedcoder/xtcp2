package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// ───────────────────────── synthetic-tree builders ──────────────────────────

// buildNetnsDir creates n regular files under a fresh dir, mimicking /run/netns.
func buildNetnsDir(tb testing.TB, n int) string {
	tb.Helper()
	dir := tb.TempDir()
	for i := range n {
		f, err := os.Create(filepath.Join(dir, "ns"+strconv.Itoa(i)))
		if err != nil {
			tb.Fatal(err)
		}
		_ = f.Close()
	}
	return dir
}

// buildProcTree creates p pid dirs each with an ns/net symlink to net:[inode],
// inode drawn from `distinct` namespaces, plus non-pid noise like real /proc.
func buildProcTree(tb testing.TB, p, distinct int) string {
	tb.Helper()
	proc := tb.TempDir()
	for _, name := range []string{"cpuinfo", "meminfo", "sys", "self", "stat", "uptime"} {
		if err := os.Mkdir(filepath.Join(proc, name), 0o755); err != nil {
			tb.Fatal(err)
		}
	}
	const base = 4026531840
	for i := range p {
		pidDir := filepath.Join(proc, strconv.Itoa(1000+i))
		if err := os.MkdirAll(filepath.Join(pidDir, "ns"), 0o755); err != nil {
			tb.Fatal(err)
		}
		target := "net:[" + strconv.Itoa(base+(i%distinct)) + "]"
		if err := os.Symlink(target, filepath.Join(pidDir, "ns", "net")); err != nil {
			tb.Fatal(err)
		}
	}
	return proc
}

// ───────────────────────────── unit tests ───────────────────────────────────

func TestParseNetInode(t *testing.T) {
	tests := []struct {
		name   string
		target string
		want   uint64
		ok     bool
	}{
		{"positive typical", "net:[4026531840]", 4026531840, true},
		{"positive min", "net:[1]", 1, true},
		{"negative no brackets", "net:4026531840", 0, false},
		{"negative empty brackets", "net:[]", 0, false},
		{"negative reversed", "net:]123[", 0, false},
		{"negative non-numeric", "net:[abc]", 0, false},
		{"corner other nstype", "pid:[4026531836]", 4026531836, true}, // parser is nstype-agnostic
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := parseNetInode([]byte(tt.target))
			if ok != tt.ok || got != tt.want {
				t.Fatalf("parseNetInode(%q) = (%d,%v), want (%d,%v)", tt.target, got, ok, tt.want, tt.ok)
			}
		})
	}
}

func TestScanDirNames(t *testing.T) {
	dir := buildNetnsDir(t, 50)
	if got := len(scanDirNames([]string{dir})); got != 50 {
		t.Fatalf("scanDirNames = %d, want 50", got)
	}
	// Missing dirs are tolerated (no docker netns dir is normal).
	if got := len(scanDirNames([]string{dir, filepath.Join(dir, "does-not-exist")})); got != 50 {
		t.Fatalf("scanDirNames with a missing dir = %d, want 50", got)
	}
}

func TestScanProcReadlink_dedup(t *testing.T) {
	proc := buildProcTree(t, 1000, 100)
	got := scanProcReadlink(proc)
	if len(got.seen) != 100 {
		t.Fatalf("scanProcReadlink distinct = %d, want 100", len(got.seen))
	}
	if got.skipped != 0 {
		t.Fatalf("scanProcReadlink skipped = %d, want 0 (all synthetic links are readable)", got.skipped)
	}
}

// ───────────────────────── hermetic microbench (Exp 1) ──────────────────────
// Real-procfs numbers come from `-mode grid` in the microVM; these isolate the
// O(namespaces) vs O(processes) algorithmic shape. tmpfs understates the /proc
// method (no per-access procfs generation cost), so treat B here as a floor.

func BenchmarkDirScan(b *testing.B) {
	for _, n := range []int{1, 10, 50, 100, 500} {
		b.Run("ns="+strconv.Itoa(n), func(b *testing.B) {
			dir := buildNetnsDir(b, n)
			dirs := []string{dir}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = scanDirNames(dirs)
			}
		})
	}
}

func BenchmarkProcScanReadlink(b *testing.B) {
	const distinct = 100
	for _, p := range []int{100, 500, 1000, 5000, 10000} {
		b.Run("pids="+strconv.Itoa(p), func(b *testing.B) {
			proc := buildProcTree(b, p, distinct)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_ = scanProcReadlink(proc)
			}
		})
	}
}

// BenchmarkProcScanReuse measures the reused nsScanner in steady state (the
// scanner is created + warmed once, then re-run) — the long-running-daemon case.
// Allocs/op should be ~0 regardless of process count.
func BenchmarkProcScanReuse(b *testing.B) {
	const distinct = 100
	for _, p := range []int{100, 1000, 10000} {
		b.Run("pids="+strconv.Itoa(p), func(b *testing.B) {
			proc := buildProcTree(b, p, distinct)
			s := newNsScanner(proc)
			b.Cleanup(func() {
				if err := s.Close(); err != nil {
					b.Error(err)
				}
			})
			s.scan() // warm buffers + map
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				s.scan()
			}
		})
	}
}

// TestNsScanner_matchesReadlink guards the unsafe fast path against the plain
// implementation: same distinct inode set, same skip count.
func TestNsScanner_matchesReadlink(t *testing.T) {
	proc := buildProcTree(t, 500, 50)
	want := scanProcReadlink(proc)
	s := newNsScanner(proc)
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	found, skipped := s.scan()
	if found != len(want.seen) {
		t.Fatalf("nsScanner found %d, scanProcReadlink found %d", found, len(want.seen))
	}
	if skipped != want.skipped {
		t.Fatalf("nsScanner skipped %d, scanProcReadlink skipped %d", skipped, want.skipped)
	}
	for ino := range want.seen {
		if _, ok := s.seen[ino]; !ok {
			t.Fatalf("nsScanner missing inode %d that scanProcReadlink found", ino)
		}
	}
}

// ─────────────────────── coverage proof (Exp 3, root) ───────────────────────

// TestCoverage_anonymousNetnsFoundOnlyByProc creates an anonymous network
// namespace (no /run/netns bind mount) via `unshare -n`, then asserts Method B
// (proc scan) finds its inode while Method A (dir scan of /run/netns) does not.
// This is the security-audit correctness argument, made executable. Root-only;
// skips cleanly otherwise and in CI.
func TestCoverage_anonymousNetnsFoundOnlyByProc(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("needs root to unshare a network namespace")
	}
	unshareBin, err := exec.LookPath("unshare")
	if err != nil {
		t.Skip("unshare not available")
	}

	// Baseline: inodes visible to the proc scan before we add the anon netns.
	before := scanProcStat(defaultProcRoot).seen

	// `unshare -n sleep 30` holds an anonymous netns alive in a child process.
	cmd := exec.Command(unshareBin, "-n", "sleep", "30")
	if err := cmd.Start(); err != nil {
		t.Fatalf("start unshare child: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	// Give the child a moment to enter the new netns.
	time.Sleep(300 * time.Millisecond)

	// The child's netns inode (from its own /proc/<pid>/ns/net).
	var childSt unix.Stat_t
	childLink := filepath.Join(defaultProcRoot, strconv.Itoa(cmd.Process.Pid), "ns", "net")
	if err := unix.Stat(childLink, &childSt); err != nil {
		t.Fatalf("stat child ns/net: %v", err)
	}

	// Method B must now see an inode it didn't see before — the anon netns.
	after := scanProcStat(defaultProcRoot).seen
	if _, ok := after[childSt.Ino]; !ok {
		t.Fatalf("Method B (proc) did not find the anonymous netns inode %d", childSt.Ino)
	}
	if _, existedBefore := before[childSt.Ino]; existedBefore {
		t.Fatalf("test setup invalid: inode %d already existed before unshare", childSt.Ino)
	}

	// Method A must NOT see it — there is no /run/netns bind mount for it.
	dirInodes := resolveDirInodes(scanDirNames([]string{"/run/netns", "/run/docker/netns"}))
	if _, seenByA := dirInodes[childSt.Ino]; seenByA {
		t.Fatalf("Method A unexpectedly saw the anonymous netns inode %d", childSt.Ino)
	}
	t.Logf("confirmed: anon netns inode %d found by proc scan, invisible to dir scan", childSt.Ino)
}
