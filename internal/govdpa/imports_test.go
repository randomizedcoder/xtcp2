package govdpa

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const (
	modulePath = "github.com/randomizedcoder/xtcp2"
	repoRoot   = "../.."
)

// govdpaPackages are the packages govdpa is made of.
var govdpaPackages = []string{modulePath + "/cmd/govdpa", modulePath + "/internal/govdpa"}

// govdpaMayImport is what they may import beyond the standard library and
// each other, and why: the same hermetic rule internal/gordma's imports_test.go
// explains, so `go test ./internal/govdpa/` runs without nix, root or a VM.
var govdpaMayImport = map[string]string{
	"golang.org/x/sys/unix":        "the NLM_F_*/NLMSG_* constants and the errno names",
	modulePath + "/pkg/xtcpnl":     "the subject under test: the request builders, the pcap reader and ParseVdpa",
	modulePath + "/internal/jsonw": "jsonw_puts, the string escaper shared with the other json_writer ports; stdlib only",
}

// govdpaClosure is the first-party closure of govdpa's non-test files.
var govdpaClosure = []string{
	modulePath + "/cmd/govdpa", modulePath + "/internal/govdpa",
	modulePath + "/pkg/xtcpnl", modulePath + "/internal/jsonw",
	modulePath + "/pkg/cint",
	// Reached through xtcpnl's congestion-name decoder; stdlib only.
	modulePath + "/pkg/strpolicy",
	// Reached through xtcpnl's inet_diag decoders; see internal/gordma.
	modulePath + "/gen/go/xtcp_flat_record",
}

type goImport struct{ path, file string }

func importsOf(t *testing.T, pkg string, tests bool) []goImport {
	t.Helper()
	dir := filepath.Join(repoRoot, strings.TrimPrefix(pkg, modulePath+"/"))
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("%s: %v", pkg, err)
	}
	var out []goImport
	fset := token.NewFileSet()
	for _, e := range entries {
		n := e.Name()
		if e.IsDir() || !strings.HasSuffix(n, ".go") || (!tests && strings.HasSuffix(n, "_test.go")) {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(dir, n), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, s := range f.Imports {
			p, err := strconv.Unquote(s.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, goImport{path: p, file: filepath.Join(dir, n)})
		}
	}
	return out
}

func isStdlib(p string) bool {
	first, _, _ := strings.Cut(p, "/")
	return !strings.Contains(first, ".")
}

func closureOf(t *testing.T, roots []string) []string {
	t.Helper()
	seen := map[string]bool{}
	var walk func(string)
	walk = func(pkg string) {
		if seen[pkg] {
			return
		}
		seen[pkg] = true
		for _, imp := range importsOf(t, pkg, false) {
			if strings.HasPrefix(imp.path, modulePath+"/") {
				walk(imp.path)
			}
		}
	}
	for _, r := range roots {
		walk(r)
	}
	return slices.Sorted(func(yield func(string) bool) {
		for p := range seen {
			if !yield(p) {
				return
			}
		}
	})
}

// TestGovdpaImports checks govdpa's direct imports, test files included,
// and its first-party closure.
func TestGovdpaImports(t *testing.T) {
	closure := closureOf(t, govdpaPackages)
	tests := []struct {
		description string
		check       func(t *testing.T) []string
		expected    int
	}{
		{description: "positive: every direct import is stdlib, govdpa's or allowlisted", check: func(t *testing.T) []string {
			var bad []string
			for _, pkg := range govdpaPackages {
				for _, imp := range importsOf(t, pkg, true) {
					if !isStdlib(imp.path) && !slices.Contains(govdpaPackages, imp.path) && govdpaMayImport[imp.path] == "" {
						bad = append(bad, imp.file+": "+imp.path)
					}
				}
			}
			return bad
		}},
		{description: "negative: no linkmonitor, pkg/xtcp or io_uring package is in the closure", check: func(*testing.T) []string {
			var bad []string
			for _, p := range closure {
				for _, f := range []string{"/pkg/linkmonitor", "/pkg/xtcp", "/pkg/io_uring"} {
					if p == modulePath+f || strings.HasPrefix(p, modulePath+f+"/") {
						bad = append(bad, p)
					}
				}
			}
			return bad
		}},
		{description: "positive: the closure is exactly the expected set", check: func(*testing.T) []string {
			if want := slices.Sorted(slices.Values(govdpaClosure)); !slices.Equal(closure, want) {
				return []string{strings.Join(closure, " ")}
			}
			return nil
		}},
		{description: "corner: every allowlist entry is imported, by a test file at least", check: func(t *testing.T) []string {
			used := map[string]bool{}
			for _, pkg := range govdpaPackages {
				for _, imp := range importsOf(t, pkg, true) {
					used[imp.path] = true
				}
			}
			var bad []string
			for p := range govdpaMayImport {
				if !used[p] {
					bad = append(bad, p)
				}
			}
			return bad
		}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := tc.check(t); len(got) != tc.expected {
				t.Fatalf("%d violations, want %d: %v", len(got), tc.expected, got)
			}
		})
	}
}
