package gordma

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

// gordmaPackages are the packages gordma is made of.
var gordmaPackages = []string{modulePath + "/cmd/gordma", modulePath + "/internal/gordma"}

// gordmaMayImport is what they may import beyond the standard library and
// each other, and why: the same hermetic rule internal/goip's
// imports_test.go explains, so `go test ./internal/gordma/` runs without
// nix, root or a VM.
var gordmaMayImport = map[string]string{
	"golang.org/x/sys/unix":      "the NETLINK_RDMA socket, its options and the NLM_F_*/NLMSG_* constants",
	modulePath + "/pkg/xtcpnl":   "the subject under test: every request builder and every reply decoder",
	modulePath + "/pkg/nlstrace": "the strace reader the ReplaySource and the corpus tests replay rdma's captures with",
}

// gordmaClosure is the first-party closure of gordma's non-test files.
var gordmaClosure = []string{
	modulePath + "/cmd/gordma", modulePath + "/internal/gordma",
	modulePath + "/pkg/xtcpnl", modulePath + "/pkg/nlstrace",
	// Reached through xtcpnl's inet_diag decoders; see internal/goip.
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

// TestGordmaImports checks gordma's direct imports, test files included,
// and its first-party closure.
func TestGordmaImports(t *testing.T) {
	closure := closureOf(t, gordmaPackages)
	tests := []struct {
		description string
		check       func(t *testing.T) []string // the violations
		expected    int
	}{
		{description: "positive: every direct import is stdlib, gordma's or allowlisted", check: func(t *testing.T) []string {
			var bad []string
			for _, pkg := range gordmaPackages {
				for _, imp := range importsOf(t, pkg, true) {
					if !isStdlib(imp.path) && !slices.Contains(gordmaPackages, imp.path) && gordmaMayImport[imp.path] == "" {
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
			if want := slices.Sorted(slices.Values(gordmaClosure)); !slices.Equal(closure, want) {
				return []string{strings.Join(closure, " ")}
			}
			return nil
		}},
		{description: "boundary: the stdlib rule", check: func(*testing.T) []string {
			var bad []string
			for p, want := range map[string]bool{"errors": true, "go/parser": true, "golang.org/x/sys/unix": false,
				modulePath + "/pkg/xtcpnl": false, "": true} {
				if isStdlib(p) != want {
					bad = append(bad, p)
				}
			}
			return bad
		}},
		{description: "corner: every allowlist entry is imported, by a test file at least", check: func(t *testing.T) []string {
			used := map[string]bool{}
			for _, pkg := range gordmaPackages {
				for _, imp := range importsOf(t, pkg, true) {
					used[imp.path] = true
				}
			}
			var bad []string
			for p := range gordmaMayImport {
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
