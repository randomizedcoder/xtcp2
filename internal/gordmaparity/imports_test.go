package gordmaparity

import (
	"go/ast"
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

// parityPackages are the comparator's packages.
var parityPackages = []string{modulePath + "/cmd/gordma-parity", modulePath + "/internal/gordmaparity"}

// parityMayImport is what they may import beyond the standard library and
// each other. Not pkg/xtcpnl, which can open sockets (DumpRtnetlink), and not
// internal/gordma, whose LiveSource does.
var parityMayImport = map[string]string{
	modulePath + "/pkg/nlstrace": "the strace reader: the captures are what the comparator reads",
}

// parityClosure is the first-party closure of the non-test files.
var parityClosure = []string{
	modulePath + "/cmd/gordma-parity", modulePath + "/internal/gordmaparity", modulePath + "/pkg/nlstrace",
}

// socketOpeners are the calls that would give the comparator a socket.
var socketOpeners = map[string][]string{
	"syscall": {"Socket", "Socketpair"},
	"unix":    {"Socket", "Socketpair"},
	"net":     {"Dial", "DialTimeout", "Listen", "ListenPacket", "DialUnix", "ListenUnix", "DialUDP", "DialTCP", "DialIP"},
	"xtcpnl":  {"DumpRtnetlink"},
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

// TestParityCannotOpenASocket pins that the comparator can never put its own
// traffic into a capture: no socket package in its imports or closure, and
// no socket call in its source.
func TestParityCannotOpenASocket(t *testing.T) {
	closure := closureOf(t, parityPackages)
	tests := []struct {
		description string
		check       func(t *testing.T) []string // the violations
		expected    int
	}{
		{description: "positive: every direct import is stdlib, the comparator's or allowlisted", check: func(t *testing.T) []string {
			var bad []string
			for _, pkg := range parityPackages {
				for _, imp := range importsOf(t, pkg, true) {
					if !isStdlib(imp.path) && !slices.Contains(parityPackages, imp.path) && parityMayImport[imp.path] == "" {
						bad = append(bad, imp.file+": "+imp.path)
					}
				}
			}
			return bad
		}},
		{description: "negative: no socket package is imported, tests included", check: func(t *testing.T) []string {
			var bad []string
			for _, pkg := range parityPackages {
				for _, imp := range importsOf(t, pkg, true) {
					if slices.Contains([]string{"syscall", "net", "golang.org/x/sys/unix"}, imp.path) {
						bad = append(bad, imp.file+": "+imp.path)
					}
				}
			}
			return bad
		}},
		{description: "negative: neither gordma nor xtcpnl is in the closure", check: func(*testing.T) []string {
			var bad []string
			for _, p := range closure {
				if p == modulePath+"/internal/gordma" || p == modulePath+"/pkg/xtcpnl" {
					bad = append(bad, p)
				}
			}
			return bad
		}},
		{description: "boundary: the closure is exactly the expected set", check: func(*testing.T) []string {
			if want := slices.Sorted(slices.Values(parityClosure)); !slices.Equal(closure, want) {
				return []string{strings.Join(closure, " ")}
			}
			return nil
		}},
		{description: "corner: no socket call in the closure's source", check: func(t *testing.T) []string {
			var bad []string
			for _, pkg := range closure {
				dir := filepath.Join(repoRoot, strings.TrimPrefix(pkg, modulePath+"/"))
				files, err := filepath.Glob(filepath.Join(dir, "*.go"))
				if err != nil {
					t.Fatal(err)
				}
				for _, name := range files {
					f, err := parser.ParseFile(token.NewFileSet(), name, nil, 0)
					if err != nil {
						t.Fatal(err)
					}
					ast.Inspect(f, func(n ast.Node) bool {
						if s, ok := n.(*ast.SelectorExpr); ok {
							if x, ok := s.X.(*ast.Ident); ok && slices.Contains(socketOpeners[x.Name], s.Sel.Name) {
								bad = append(bad, name+": "+x.Name+"."+s.Sel.Name)
							}
						}
						return true
					})
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
