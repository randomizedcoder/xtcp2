package testkit

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// abiLayoutFile is nix/tests/linkmonitor-abi.c output, relative to a package
// directory under pkg/linkmonitor/internal. test-linkmonitor-rdma-abi
// regenerates it from the pinned headers and fails if the copy differs.
const abiLayoutFile = "../rdmanative/testdata/abi_layout.txt"

// PackageGoFiles lists dir's non-test Go files, for a package-wide literal scan.
func PackageGoFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go") {
			files = append(files, filepath.Join(dir, name))
		}
	}
	return files, nil
}

// GoSource is what the ABI comparison needs from a package's layout files.
type GoSource struct {
	Consts   []string
	Literals []uint64
}

// ParseGoSource collects declared constant names and integer literals.
func ParseGoSource(files ...string) (GoSource, error) {
	var src GoSource
	fset := token.NewFileSet()
	for _, name := range files {
		f, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			return GoSource{}, err
		}
		ast.Inspect(f, func(n ast.Node) bool {
			switch n := n.(type) {
			case *ast.GenDecl:
				if n.Tok != token.CONST {
					break
				}
				for _, spec := range n.Specs {
					if vs, ok := spec.(*ast.ValueSpec); ok {
						for _, id := range vs.Names {
							src.Consts = append(src.Consts, id.Name)
						}
					}
				}
			case *ast.BasicLit:
				if n.Kind == token.INT {
					if v, err := strconv.ParseUint(strings.ReplaceAll(n.Value, "_", ""), 0, 64); err == nil {
						src.Literals = append(src.Literals, v)
					}
				}
			}
			return true
		})
	}
	return src, nil
}

// CompareABILayout checks pkg's lines of the layout against consts, which maps
// each Go constant name to its value. Every declared constant must be in
// consts, every layout line must match one, and no "!name" value may be
// declared or written as a literal. It returns one problem per mismatch.
func CompareABILayout(layout []byte, pkg string, consts map[string]uint64, src GoSource) []string {
	var problems []string
	declared := map[string]bool{}
	for _, name := range src.Consts {
		declared[name] = true
		if _, ok := consts[name]; !ok {
			problems = append(problems, name+": declared but not in the comparison map")
		}
	}
	seen := map[string]bool{}
	for i, line := range bytes.Split(layout, []byte("\n")) {
		fields := strings.Fields(string(line))
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 3 {
			problems = append(problems, fmt.Sprintf("line %d: want \"<package> <name> <value>\"", i+1))
			continue
		}
		if fields[0] != pkg {
			continue
		}
		name := fields[1]
		value, err := strconv.ParseUint(fields[2], 0, 64)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s: bad value %q", name, fields[2]))
			continue
		}
		if seen[name] {
			problems = append(problems, name+": listed twice")
		}
		seen[name] = true
		if forbidden, ok := strings.CutPrefix(name, "!"); ok {
			problems = append(problems, forbiddenUses(forbidden, value, consts, src)...)
			continue
		}
		got, ok := consts[name]
		switch {
		case !ok:
			problems = append(problems, name+": in the kernel layout but not declared in Go")
		case !declared[name]:
			problems = append(problems, name+": in the comparison map but not declared in the layout files")
		case got != value:
			problems = append(problems, fmt.Sprintf("%s: Go %#x, kernel %#x", name, got, value))
		}
	}
	for name := range consts {
		if !seen[name] {
			problems = append(problems, name+": declared in Go but missing from the kernel layout")
		}
	}
	sort.Strings(problems)
	return problems
}

func forbiddenUses(name string, value uint64, consts map[string]uint64, src GoSource) []string {
	var problems []string
	if _, ok := consts[name]; ok {
		problems = append(problems, name+": forbidden name is declared")
	}
	for other, v := range consts {
		if v == value {
			problems = append(problems, fmt.Sprintf("%s: equals forbidden %s %#x", other, name, value))
		}
	}
	for _, v := range src.Literals {
		if v == value {
			problems = append(problems, fmt.Sprintf("literal %#x is forbidden %s", value, name))
		}
	}
	return problems
}

// ABIRow selects the layout problems for one guard-table row, by constant
// name prefix or by text in the problem.
type ABIRow struct {
	Name, Category, Description, Expected string
	Prefixes                              []string
	Text                                  string
}

// Matches reports whether problem belongs to the row.
func (r ABIRow) Matches(problem string) bool {
	if r.Text != "" && strings.Contains(problem, r.Text) {
		return true
	}
	for _, prefix := range r.Prefixes {
		if strings.HasPrefix(problem, prefix) && !strings.Contains(problem, "forbidden") {
			return true
		}
	}
	return false
}

// CheckABILayout runs the guard-table rows for one package: each row must have
// no problems, and no problem may fall outside every row.
func CheckABILayout(t *testing.T, layoutFiles []string, consts map[string]uint64, pkg string, rows []ABIRow) {
	t.Helper()
	layout, err := os.ReadFile(abiLayoutFile)
	if err != nil {
		t.Fatal(err)
	}
	src, err := ParseGoSource(layoutFiles...)
	if err != nil {
		t.Fatal(err)
	}
	all, err := PackageGoFiles(".")
	if err != nil {
		t.Fatal(err)
	}
	whole, err := ParseGoSource(all...)
	if err != nil {
		t.Fatal(err)
	}
	src.Literals = whole.Literals
	problems := CompareABILayout(layout, pkg, consts, src)
	for _, row := range rows {
		t.Run(row.Name, func(t *testing.T) {
			for _, p := range problems {
				if row.Matches(p) {
					t.Errorf("%s (%s), expected %s: %s", row.Description, row.Category, row.Expected, p)
				}
			}
		})
	}
	for _, p := range problems {
		if !slices.ContainsFunc(rows, func(r ABIRow) bool { return r.Matches(p) }) {
			t.Errorf("no guard row covers: %s", p)
		}
	}
}
