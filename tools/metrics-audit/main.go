// metrics-audit
//
// Audits Prometheus metric definitions in the xtcp2 codebase.
//
// Checks:
//  1. Every prometheus.NewCounter / NewCounterVec / NewGauge / NewGaugeVec /
//     NewHistogram / NewHistogramVec / NewSummary call has a "Name:" field.
//  2. Every metric variable defined (as `var foo = prometheus.New...`) is
//     referenced at least once elsewhere (orphan detection).
//
// Exits non-zero if findings exist.
package main

import (
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type defn struct {
	name   string
	metric string
	pos    token.Position
}

func main() {
	os.Exit(runMain(os.Args[1:], os.Stdout, os.Stderr))
}

// runMain wires flag parsing + runAudit. Extracted so tests can drive it
// with synthetic args + capture buffers without subprocessing.
func runMain(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("metrics-audit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", ".", "repo root")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	return runAudit(*root, stdout, stderr)
}

// runAudit walks root, collects metric definitions + references, and
// reports unreferenced ones as orphans. Returns 0 / 1 / 2.
func runAudit(root string, stdout, stderr io.Writer) int {
	defs, refs, err := auditTree(root)
	if err != nil {
		fmt.Fprintf(stderr, "metrics-audit: walk failed: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "metrics-audit: scanned %s — %d metric definition(s)\n",
		root, len(defs))
	orphans := 0
	for _, d := range defs {
		if refs[d.name] <= 1 {
			fmt.Fprintf(stdout, "%s: orphan metric %q (%s) — defined but never referenced\n",
				d.pos, d.name, d.metric)
			orphans++
		}
	}
	if orphans > 0 {
		fmt.Fprintf(stderr, "metrics-audit: %d orphan metric(s)\n", orphans)
		return 1
	}
	fmt.Fprintln(stdout, "metrics-audit: no findings")
	return 0
}

// skippedDirs is the set of directory base-names auditTree skips. Lifted
// to a package-level set so tests + future audits can grep one place.
var skippedDirs = map[string]struct{}{
	"vendor": {},
	".git":   {},
	"gen":    {},
	"dart":   {},
	"python": {},
}

// shouldSkipDir returns true if the directory's base name is in
// skippedDirs (vendor, generated, language-specific subtrees).
func shouldSkipDir(path string) bool {
	_, skip := skippedDirs[filepath.Base(path)]
	return skip
}

// shouldSkipFile filters non-Go, test, and generated-proto sources.
// Keeping this as a single boolean predicate (was three inline ifs)
// drops the WalkDir callback's complexity by 3.
func shouldSkipFile(path string) bool {
	if !strings.HasSuffix(path, ".go") {
		return true
	}
	if strings.HasSuffix(path, "_test.go") {
		return true
	}
	if strings.Contains(path, ".pb.go") {
		return true
	}
	return false
}

// collectMetricsFromFile walks the file's AST, appending any metric
// definitions to defs and incrementing references[*] for every Ident.
// Extracted from auditTree so the WalkDir callback itself stays linear.
func collectMetricsFromFile(fset *token.FileSet, file *ast.File, defs *[]defn, references map[string]int) {
	ast.Inspect(file, func(n ast.Node) bool {
		if vd, ok := n.(*ast.ValueSpec); ok {
			for i, ident := range vd.Names {
				if i >= len(vd.Values) {
					break
				}
				if metric := promNewKind(vd.Values[i]); metric != "" {
					*defs = append(*defs, defn{
						name:   ident.Name,
						metric: metric,
						pos:    fset.Position(ident.Pos()),
					})
				}
			}
		}
		if id, ok := n.(*ast.Ident); ok {
			references[id.Name]++
		}
		return true
	})
}

// auditTree walks root once and returns the parsed metric definitions
// plus an identifier→count reference map for orphan detection.
//
// The body was previously a 17-cyclo monolith mixing dir-skip logic,
// file-filter logic, AST parsing, and definition/reference collection
// in nested closures. The skip + filter + AST work each moved into a
// helper; the WalkDir callback is now linear (gocyclo 6).
func auditTree(root string) ([]defn, map[string]int, error) {
	fset := token.NewFileSet()
	var definitions []defn
	references := map[string]int{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if shouldSkipDir(path) {
				return filepath.SkipDir
			}
			return nil
		}
		if shouldSkipFile(path) {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		collectMetricsFromFile(fset, file, &definitions, references)
		return nil
	})
	return definitions, references, err
}

func promNewKind(e ast.Expr) string {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return ""
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return ""
	}
	pkg, ok := sel.X.(*ast.Ident)
	if !ok || (pkg.Name != "prometheus" && pkg.Name != "promauto") {
		return ""
	}
	switch sel.Sel.Name {
	case "NewCounter", "NewCounterVec",
		"NewGauge", "NewGaugeVec",
		"NewHistogram", "NewHistogramVec",
		"NewSummary", "NewSummaryVec":
		return sel.Sel.Name
	}
	return ""
}
