package linkmonitor

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/linuxio"
	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

// TestRDMAScalarTable covers the raw-descriptor attribute reader every RDMA
// inventory, state and association file goes through.
func TestRDMAScalarTable(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		// setup returns the path to read.
		setup    func(t *testing.T, dir string) string
		canceled bool
		want     string
		wantErr  error
		// empty is whether emptyGIDSlot accepts the error, as a GID slot
		// without a GID returns.
		empty bool
	}{
		{name: "value", category: "positive", description: "state attribute with a newline", expectedOutcome: "text without the newline",
			setup: rdmaScalarFile("4: ACTIVE\n"), want: "4: ACTIVE"},
		{name: "trim", category: "positive", description: "GID type padded with spaces", expectedOutcome: "trimmed text",
			setup: rdmaScalarFile("  RoCE v2 \n"), want: "RoCE v2"},
		{name: "empty", category: "boundary", description: "empty attribute", expectedOutcome: "empty text, no error",
			setup: rdmaScalarFile("")},
		{name: "limit", category: "boundary", description: "attribute of exactly 4096 bytes", expectedOutcome: "read whole",
			setup: rdmaScalarFile(strings.Repeat("x", rdmaScalarLimit)), want: strings.Repeat("x", rdmaScalarLimit)},
		{name: "over-limit", category: "boundary", description: "attribute of 4097 bytes", expectedOutcome: "ErrLimit",
			setup: rdmaScalarFile(strings.Repeat("x", rdmaScalarLimit+1)), wantErr: linuxio.ErrLimit},
		{name: "large", category: "corner", description: "1 MiB file, larger than any sysfs page", expectedOutcome: "ErrLimit from one bounded read",
			setup: rdmaScalarFile(strings.Repeat("x", 1<<20)), wantErr: linuxio.ErrLimit},
		{name: "missing", category: "negative", description: "file that does not exist", expectedOutcome: "fs.ErrNotExist, an empty GID slot",
			setup: func(_ *testing.T, dir string) string { return filepath.Join(dir, "absent") }, wantErr: fs.ErrNotExist, empty: true},
		{name: "directory", category: "negative", description: "a directory, whose read fails with EISDIR", expectedOutcome: "read *fs.PathError naming the path; not an empty slot",
			setup: func(_ *testing.T, dir string) string { return dir }, wantErr: syscall.EISDIR},
		{name: "permission", category: "negative", description: "file without read permission", expectedOutcome: "fs.ErrPermission",
			setup: func(t *testing.T, dir string) string {
				if os.Geteuid() == 0 {
					t.Skip("root reads any file")
				}
				path := rdmaScalarFile("secret")(t, dir)
				if err := os.Chmod(path, 0); err != nil {
					t.Fatal(err)
				}
				return path
			}, wantErr: fs.ErrPermission},
		{name: "canceled", category: "corner", description: "context canceled before the read, path missing", expectedOutcome: "context.Canceled: nothing is opened",
			setup: func(_ *testing.T, dir string) string { return filepath.Join(dir, "absent") }, canceled: true, wantErr: context.Canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			path := tc.setup(t, t.TempDir())
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			got, err := rdmaScalar(ctx, path)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Fatalf("%s: %v", tc.expectedOutcome, err)
			}
			if got != tc.want {
				t.Fatalf("%s: got %q", tc.expectedOutcome, got)
			}
			if err != nil && emptyGIDSlot(err) != tc.empty {
				t.Fatalf("%s: emptyGIDSlot(%v) = %t", tc.expectedOutcome, err, !tc.empty)
			}
			var pathErr *fs.PathError
			if tc.name == "directory" && (!errors.As(err, &pathErr) || pathErr.Op != "read" || pathErr.Path != path) {
				t.Fatalf("%s: %#v", tc.expectedOutcome, err)
			}
		})
	}
}

// TestRDMAScalarDescriptors checks no row of reads, failed or not, leaves a
// descriptor open.
func TestRDMAScalarDescriptors(t *testing.T) {
	t.Log("corner: 1000 reads of present, missing, oversized and directory paths; expected: /proc/self/fd count unchanged")
	dir := t.TempDir()
	big := filepath.Join(dir, "big")
	rdmaWrite(t, big, strings.Repeat("x", rdmaScalarLimit+1))
	paths := []string{rdmaScalarFile("1")(t, dir), filepath.Join(dir, "absent"), dir, big}
	before := rdmaOpenDescriptors(t)
	for i := range 1000 {
		_, _ = rdmaScalar(t.Context(), paths[i%len(paths)])
	}
	if after := rdmaOpenDescriptors(t); after != before {
		t.Fatalf("descriptors %d before, %d after", before, after)
	}
}

func rdmaScalarFile(value string) func(*testing.T, string) string {
	return func(t *testing.T, dir string) string {
		path := filepath.Join(dir, "value")
		rdmaWrite(t, path, value)
		return path
	}
}

func rdmaOpenDescriptors(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

// TestRDMAGIDScanCostTable guards the resync's GID scan at one read system
// call per file read, counted by /proc/self/io syscr: io.ReadAll would read
// each file twice, once more for EOF.
func TestRDMAGIDScanCostTable(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		// slots GID type files, the first populated of them with an ndev.
		slots, populated int
		noTypes          bool
		reads            int64
		versions         []string
		aliases          int
	}{
		{name: "one-slot", category: "boundary", description: "one populated GID slot", expectedOutcome: "2 reads: its type and its ndev",
			slots: 1, populated: 1, reads: 2, versions: []string{"v2"}, aliases: 1},
		{name: "mlx5-table", category: "positive", description: "256 slots, 12 with an ndev, as on host l", expectedOutcome: "268 reads; a missing ndev costs an open, no read",
			slots: 256, populated: 12, reads: 268, versions: []string{"v2"}, aliases: 1},
		{name: "full-table", category: "positive", description: "256 slots, all with an ndev", expectedOutcome: "512 reads",
			slots: 256, populated: 256, reads: 512, versions: []string{"v2"}, aliases: 1},
		{name: "no-ndevs", category: "corner", description: "256 slots, none with an ndev (native InfiniBand)", expectedOutcome: "256 reads, no aliases",
			slots: 256, reads: 256, versions: []string{"v2"}},
		{name: "empty-dir", category: "boundary", description: "gid_attrs/types with no slots", expectedOutcome: "no reads, version unknown",
			versions: []string{unknownSetting}},
		{name: "no-types", category: "negative", description: "port without gid_attrs/types", expectedOutcome: "no reads, version unknown",
			noTypes: true, versions: []string{unknownSetting}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			port := t.TempDir()
			if !tc.noTypes {
				if err := os.MkdirAll(filepath.Join(port, "gid_attrs", "types"), 0750); err != nil {
					t.Fatal(err)
				}
			}
			for i := range tc.slots {
				rdmaWrite(t, filepath.Join(port, "gid_attrs", "types", strconv.Itoa(i)), "RoCE v2")
				if i < tc.populated {
					rdmaWrite(t, filepath.Join(port, "gid_attrs", "ndevs", strconv.Itoa(i)), "eth1")
				}
			}
			var p model.RDMAPort
			overhead := rdmaReadCalls(t)
			overhead = rdmaReadCalls(t) - overhead
			before := rdmaReadCalls(t)
			err := rdmaFilesystem{}.gids(t.Context(), port, &p)
			reads := rdmaReadCalls(t) - before - overhead
			if err != nil {
				t.Fatal(err)
			}
			// Other threads of the test binary may read too; none of them
			// can make the count smaller.
			if reads < tc.reads || reads > tc.reads+2 {
				t.Fatalf("%s: %d read system calls", tc.expectedOutcome, reads)
			}
			if !slices.Equal(p.Versions, tc.versions) || len(p.Aliases) != tc.aliases || p.Records != tc.slots {
				t.Fatalf("%s: %+v", tc.expectedOutcome, p)
			}
		})
	}
}

// rdmaReadCalls is the process's read system calls so far.
func rdmaReadCalls(t *testing.T) int64 {
	t.Helper()
	b, err := os.ReadFile("/proc/self/io")
	if err != nil {
		t.Skip(err)
	}
	for line := range strings.Lines(string(b)) {
		if value, ok := strings.CutPrefix(line, "syscr: "); ok {
			n, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			return n
		}
	}
	t.Fatal("no syscr in /proc/self/io")
	return 0
}
