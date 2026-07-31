// discovery-bench compares network-namespace *discovery* methods head to head,
// so we can decide (and, later, re-confirm) which one xtcp2 should use to build
// its set of namespaces to poll.
//
// Two methods, because they scale with different things:
//
//	Method A — directory scan (what xtcp2 does today, pkg/xtcp/ns_discover.go):
//	    os.ReadDir("/run/netns") [+ "/run/docker/netns"], filter, collect names.
//	    Cost is O(named-namespaces). Only sees namespaces that have a bind-mount
//	    under one of those dirs — anonymous container/`unshare -n` netns are
//	    INVISIBLE to it.
//
//	Method B — /proc/<pid>/ns/net inode scan (the strong-correctness candidate):
//	    os.ReadDir("/proc"), then for each numeric pid read its net-namespace
//	    identity and dedup by inode. Cost is O(processes). Sees EVERY namespace
//	    that has at least one live process — including anonymous ones. Two
//	    sub-variants differ only in the per-pid syscall:
//	      B-readlink: readlink(/proc/<pid>/ns/net) → parse "net:[<inode>]"
//	      B-stat:     stat(/proc/<pid>/ns/net) → Stat_t.Ino
//	    Both are gated by ptrace-access to the target process, so an unprivileged
//	    run skips other users' pids — the tool counts those skips so the numbers
//	    are interpretable. Production xtcp2 runs with CAP_SYS_ADMIN and sees all.
//
// Modes:
//
//	-mode measure   Time each method against the live system (default /proc and
//	                /run/netns,/run/docker/netns) and report a coverage diff
//	                (which inodes each method found; B-only = A's blind spots)
//	                plus per-method skip counts (pids that errored: EACCES/gone).
//	-mode grid      Root-only. For each (N namespaces × P processes) cell: create
//	                the namespaces, spawn the processes, run measure, tear down.
//	                Emits one JSON line per cell for the harness to collect.
//	                Processes are cheap `sleep infinity` (bulk, host netns) and
//	                `ip netns exec <ns> sleep infinity` (the in-namespace ones) so
//	                P can reach thousands without the RSS a fleet of Go binaries
//	                would cost.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const (
	defaultProcRoot = "/proc"
	// The two standard bind-mount dirs xtcp2 watches today.
	defaultNetnsDirs = "/run/netns,/run/docker/netns"
	// How many timed repetitions per method in measure mode. Discovery is a
	// once-per-poll operation, so we care about a stable single-scan cost, not
	// throughput — a handful of iters with a min/median is plenty.
	defaultIters = 25
	// settleDelay lets freshly spawned sleeper children finish their setns
	// before we scan, in grid mode.
	settleDelay = 200 * time.Millisecond
)

func main() {
	os.Exit(runMain(context.Background(), os.Args[1:], os.Stdout, os.Stderr))
}

// runMain is the flag-parsing entrypoint, extracted so tests can drive it with
// synthetic args + a cancellable ctx without subprocessing.
func runMain(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("discovery-bench", flag.ContinueOnError)
	fs.SetOutput(stderr)
	mode := fs.String("mode", "measure", "measure | grid | sleeper")
	procRoot := fs.String("proc", defaultProcRoot, "procfs root to scan for Method B")
	netnsDirs := fs.String("netnsDirs", defaultNetnsDirs, "comma-separated dirs for Method A")
	iters := fs.Int("iters", defaultIters, "timed repetitions per method (measure)")
	asJSON := fs.Bool("json", false, "emit one machine-readable JSON line per result")
	nsGrid := fs.String("nsGrid", "1,10,50,100", "grid mode: namespace counts to sweep")
	pidGrid := fs.String("pidGrid", "100,500,1000,5000", "grid mode: process counts to sweep")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	switch *mode {
	case "measure":
		dirs := splitNonEmpty(*netnsDirs)
		res := measure(*procRoot, dirs, *iters)
		report(stdout, res, *asJSON)
		return 0
	case "grid":
		return runGrid(ctx, *procRoot, splitNonEmpty(*netnsDirs), *iters, parseInts(*nsGrid), parseInts(*pidGrid), *asJSON, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "unknown -mode %q\n", *mode)
		return 2
	}
}

// ───────────────────────── Method A: directory scan ─────────────────────────

// scanDirNames mirrors pkg/xtcp/ns_discover.go discoverNamespaces: ReadDir each
// dir, skip subdirectories, return the full namespace paths. This is the timed
// production-equivalent scan — no per-entry stat, matching today's behavior.
func scanDirNames(dirs []string) []string {
	var out []string
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue // a missing dir (e.g. no docker) is normal, not an error
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	return out
}

// ───────────────────────── Method B: /proc inode scan ───────────────────────

// procResult is what a /proc scan returns: the deduped inode→handle set and the
// number of pids skipped (readlink/stat error — EACCES for other users' pids
// when unprivileged, or the pid exited mid-scan).
type procResult struct {
	seen    map[uint64]string
	skipped int
}

// procMapHint pre-sizes the dedup map. Most hosts have well under this many
// distinct network namespaces; overshoot just avoids a couple of early grows.
const procMapHint = 64

// scanProcReadlink walks procRoot, reads each numeric pid's ns/net symlink into
// a reused buffer, parses the "net:[<inode>]" target straight from the bytes
// (no result-string alloc), and dedups by inode. The handle is
// /proc/<pid>/ns/net for one representative pid (the enterable fd source).
func scanProcReadlink(procRoot string) procResult {
	r := procResult{seen: make(map[uint64]string, procMapHint)}
	buf := make([]byte, 64) // "net:[<inode>]" is short; reused across pids
	forEachPid(procRoot, func(name string) {
		link := procRoot + "/" + name + "/ns/net"
		n, err := unix.Readlink(link, buf)
		if err != nil || n <= 0 {
			r.skipped++
			return
		}
		ino, ok := parseNetInode(buf[:n])
		if !ok {
			r.skipped++
			return
		}
		if _, dup := r.seen[ino]; !dup {
			r.seen[ino] = link
		}
	})
	return r
}

// scanProcStat is the same walk but stats ns/net for its inode instead of
// readlink+parse. stat follows the magic symlink to the nsfs inode, so
// Stat_t.Ino IS the namespace identity — no readlink buffer, no parsing.
func scanProcStat(procRoot string) procResult {
	r := procResult{seen: make(map[uint64]string, procMapHint)}
	var st unix.Stat_t
	forEachPid(procRoot, func(name string) {
		link := procRoot + "/" + name + "/ns/net"
		if err := unix.Stat(link, &st); err != nil {
			r.skipped++
			return
		}
		if _, dup := r.seen[st.Ino]; !dup {
			r.seen[st.Ino] = link
		}
	})
	return r
}

// forEachPid calls fn(name) for every numeric-named entry directly under
// procRoot, reading the directory in batches. Readdirnames avoids os.ReadDir's
// two big costs on /proc: sorting thousands of entries we don't need ordered,
// and allocating an os.DirEntry per entry. The first-byte digit check skips the
// non-pid entries (cpuinfo, sys, self, …) without a syscall each.
func forEachPid(procRoot string, fn func(name string)) {
	f, err := os.Open(procRoot)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	for {
		names, err := f.Readdirnames(512)
		for _, name := range names {
			if len(name) == 0 || name[0] < '0' || name[0] > '9' {
				continue
			}
			fn(name)
		}
		if err != nil {
			return // io.EOF (done) or a real read error
		}
	}
}

// parseNetInode extracts the inode from a "net:[4026531840]" symlink target,
// operating on bytes so the readlink hot path needs no result-string alloc.
func parseNetInode(target []byte) (uint64, bool) {
	l := bytes.IndexByte(target, '[')
	r := bytes.IndexByte(target, ']')
	if l < 0 || r <= l+1 { // r<=l+1 also rejects empty brackets "[]"
		return 0, false
	}
	var ino uint64
	for _, c := range target[l+1 : r] {
		if c < '0' || c > '9' {
			return 0, false
		}
		ino = ino*10 + uint64(c-'0')
	}
	return ino, true
}

// resolveDirInodes stats each Method-A path to get its nsfs inode, so A's
// findings can be compared to B's in the same inode space. This is a COVERAGE
// helper, deliberately outside the timed path (production Method A never stats).
func resolveDirInodes(paths []string) map[uint64]string {
	out := make(map[uint64]string, len(paths))
	for _, p := range paths {
		var st unix.Stat_t
		if err := unix.Stat(p, &st); err != nil {
			continue
		}
		out[st.Ino] = p
	}
	return out
}

// ───────────────────────────── measurement ──────────────────────────────────

type methodResult struct {
	Name    string `json:"method"`
	Count   int    `json:"found"`   // namespaces discovered
	Skipped int    `json:"skipped"` // pids that errored (EACCES/gone); 0 for dir
	MinNs   int64  `json:"min_ns"`  // fastest of iters
	MedNs   int64  `json:"med_ns"`  // median of iters
	MeanNs  int64  `json:"mean_ns"` // mean of iters
	Iters   int    `json:"iters"`
}

type measureResult struct {
	ProcRoot   string         `json:"proc"`
	NetnsDirs  []string       `json:"netns_dirs"`
	Methods    []methodResult `json:"methods"`
	CoverProcN int            `json:"coverage_proc_inodes"` // distinct inodes B saw
	CoverDirN  int            `json:"coverage_dir_inodes"`  // distinct inodes A saw
	BOnlyN     int            `json:"b_only_inodes"`        // A's blind spots
	BOnly      []uint64       `json:"b_only_inode_list"`
}

// timeScan runs scan iters times and returns min/median/mean ns plus the count
// and skip total from the last run (all runs see the same live set at this
// cadence). scan returns (found, skipped).
func timeScan(name string, iters int, scan func() (found, skipped int)) methodResult {
	runs := make([]int64, 0, iters)
	var lastCount, lastSkip int
	for range iters {
		start := time.Now()
		lastCount, lastSkip = scan()
		runs = append(runs, time.Since(start).Nanoseconds())
	}
	sorted := slices.Clone(runs)
	slices.Sort(sorted)
	var sum int64
	for _, v := range sorted {
		sum += v
	}
	mr := methodResult{Name: name, Count: lastCount, Skipped: lastSkip, Iters: iters}
	if len(sorted) > 0 {
		mr.MinNs = sorted[0]
		mr.MedNs = sorted[len(sorted)/2]
		mr.MeanNs = sum / int64(len(sorted))
	}
	return mr
}

// measure times all three methods against the live system and computes the
// coverage diff (which namespaces Method B sees that Method A cannot).
func measure(procRoot string, dirs []string, iters int) measureResult {
	dirRes := timeScan("dir", iters, func() (int, int) { return len(scanDirNames(dirs)), 0 })
	rlRes := timeScan("proc-readlink", iters, func() (int, int) {
		r := scanProcReadlink(procRoot)
		return len(r.seen), r.skipped
	})
	stRes := timeScan("proc-stat", iters, func() (int, int) {
		r := scanProcStat(procRoot)
		return len(r.seen), r.skipped
	})
	// proc-reuse: the reused, zero-allocation nsScanner. Created once and re-run
	// across iters, so it measures the long-running-daemon steady state.
	sc := newNsScanner(procRoot)
	reuseRes := timeScan("proc-reuse", iters, func() (int, int) { return sc.scan() })
	if cerr := sc.Close(); cerr != nil {
		fmt.Fprintf(os.Stderr, "nsScanner close: %v\n", cerr)
	}

	// Coverage: resolve A's paths → inodes, compare to B's inode set.
	dirInodes := resolveDirInodes(scanDirNames(dirs))
	procInodes := scanProcStat(procRoot).seen
	var bOnly []uint64
	for ino := range procInodes {
		if _, seenByA := dirInodes[ino]; !seenByA {
			bOnly = append(bOnly, ino)
		}
	}
	slices.Sort(bOnly)

	return measureResult{
		ProcRoot:   procRoot,
		NetnsDirs:  dirs,
		Methods:    []methodResult{dirRes, rlRes, stRes, reuseRes},
		CoverProcN: len(procInodes),
		CoverDirN:  len(dirInodes),
		BOnlyN:     len(bOnly),
		BOnly:      bOnly,
	}
}

func report(w io.Writer, r measureResult, asJSON bool) {
	if asJSON {
		b, err := json.Marshal(r)
		if err != nil {
			fmt.Fprintf(w, "DISCOBENCH_ERR %v\n", err)
			return
		}
		fmt.Fprintf(w, "DISCOBENCH %s\n", b)
		return
	}
	fmt.Fprintf(w, "proc=%s netnsDirs=%v\n", r.ProcRoot, r.NetnsDirs)
	fmt.Fprintf(w, "%-16s %8s %8s %10s %10s %10s\n", "method", "found", "skipped", "min_us", "med_us", "mean_us")
	for _, m := range r.Methods {
		fmt.Fprintf(w, "%-16s %8d %8d %10.1f %10.1f %10.1f\n",
			m.Name, m.Count, m.Skipped, us(m.MinNs), us(m.MedNs), us(m.MeanNs))
	}
	fmt.Fprintf(w, "coverage: proc saw %d distinct netns, dir saw %d; B-only (A blind spots)=%d\n",
		r.CoverProcN, r.CoverDirN, r.BOnlyN)
}

func us(ns int64) float64 { return float64(ns) / 1000.0 }

// ───────────────────────────── grid (root) ──────────────────────────────────

type gridRow struct {
	NS      int           `json:"ns"`
	PIDs    int           `json:"pids"`
	Measure measureResult `json:"measure"`
}

// runGrid sweeps (N namespaces × P processes). For each cell it creates the
// namespaces (ip netns add), spawns P `sleep infinity` children (the first N via
// `ip netns exec` so they live inside the created namespaces and give Method B
// distinct inodes to dedup, the rest in the host netns to build the O(P) load),
// runs measure, then tears everything down.
func runGrid(ctx context.Context, procRoot string, dirs []string, iters int, nsCounts, pidCounts []int, asJSON bool, stdout, stderr io.Writer) int {
	if os.Geteuid() != 0 {
		fmt.Fprintln(stderr, "grid mode requires root (creates namespaces + processes)")
		return 1
	}
	for _, n := range nsCounts {
		for _, p := range pidCounts {
			row, err := runGridCell(ctx, procRoot, dirs, iters, n, p, stderr)
			if err != nil {
				fmt.Fprintf(stderr, "cell ns=%d pids=%d failed: %v\n", n, p, err)
				continue
			}
			if asJSON {
				b, jerr := json.Marshal(row)
				if jerr != nil {
					fmt.Fprintf(stderr, "marshal grid row: %v\n", jerr)
					continue
				}
				fmt.Fprintf(stdout, "DISCOBENCH_GRID %s\n", b)
			} else {
				fmt.Fprintf(stdout, "\n== grid cell: ns=%d pids=%d ==\n", n, p)
				report(stdout, row.Measure, false)
			}
		}
	}
	return 0
}

func runGridCell(ctx context.Context, procRoot string, dirs []string, iters, n, p int, stderr io.Writer) (gridRow, error) {
	names := make([]string, 0, n)
	for i := range n {
		name := fmt.Sprintf("disco%d", i)
		if out, err := exec.CommandContext(ctx, "ip", "netns", "add", name).CombinedOutput(); err != nil {
			cleanupNetns(ctx, names, stderr)
			return gridRow{}, fmt.Errorf("ip netns add %s: %v (%s)", name, err, out)
		}
		names = append(names, name)
	}
	// Spawn p cheap `sleep infinity` children. The first n run via `ip netns
	// exec` so they live inside the created namespaces (distinct inodes for B to
	// dedup); the rest stay in the host netns but still cost B one readlink/stat
	// each — the O(P) we're measuring.
	held := make([]*exec.Cmd, 0, p)
	for i := range p {
		var cmd *exec.Cmd
		var err error
		if i < n {
			cmd, err = spawnHeld(ctx, stderr, "ip", "netns", "exec", names[i], "sleep", "infinity")
		} else {
			cmd, err = spawnHeld(ctx, stderr, "sleep", "infinity")
		}
		if err != nil {
			killAll(held, stderr)
			cleanupNetns(ctx, names, stderr)
			return gridRow{}, fmt.Errorf("spawn process %d: %w", i, err)
		}
		held = append(held, cmd)
	}
	// Small settle so the children have entered their namespaces before we scan.
	time.Sleep(settleDelay)

	res := measure(procRoot, dirs, iters)

	killAll(held, stderr)
	cleanupNetns(ctx, names, stderr)
	return gridRow{NS: n, PIDs: p, Measure: res}, nil
}

// spawnHeld starts a long-lived child in its own process group (Setpgid) so the
// whole group can be signaled at teardown — `ip netns exec` forks, so killing
// just the leader would orphan the actual `sleep`.
func spawnHeld(ctx context.Context, stderr io.Writer, name string, args ...string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmd, nil
}

func killAll(procs []*exec.Cmd, stderr io.Writer) {
	for _, c := range procs {
		if c.Process == nil {
			continue
		}
		// Kill the whole process group (negative pid). Falls back to the
		// single process if the group lookup fails.
		if pgid, err := syscall.Getpgid(c.Process.Pid); err == nil {
			if kerr := syscall.Kill(-pgid, syscall.SIGKILL); kerr != nil && !errors.Is(kerr, syscall.ESRCH) {
				fmt.Fprintf(stderr, "kill group %d: %v\n", pgid, kerr)
			}
		} else if kerr := c.Process.Kill(); kerr != nil && !errors.Is(kerr, os.ErrProcessDone) {
			fmt.Fprintf(stderr, "kill pid %d: %v\n", c.Process.Pid, kerr)
		}
	}
	for _, c := range procs {
		// A killed child exits via signal → *exec.ExitError; that's the
		// expected teardown path, not a failure worth reporting.
		var ee *exec.ExitError
		if err := c.Wait(); err != nil && !errors.As(err, &ee) {
			fmt.Fprintf(stderr, "wait child: %v\n", err)
		}
	}
}

func cleanupNetns(ctx context.Context, names []string, stderr io.Writer) {
	for _, name := range names {
		if out, err := exec.CommandContext(ctx, "ip", "netns", "del", name).CombinedOutput(); err != nil {
			fmt.Fprintf(stderr, "ip netns del %s: %v (%s)\n", name, err, out)
		}
	}
}

// ───────────────────────────── small helpers ────────────────────────────────

func splitNonEmpty(csv string) []string {
	var out []string
	for s := range strings.SplitSeq(csv, ",") {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func parseInts(csv string) []int {
	var out []int
	for _, s := range splitNonEmpty(csv) {
		if v, err := strconv.Atoi(s); err == nil {
			out = append(out, v)
		}
	}
	return out
}
