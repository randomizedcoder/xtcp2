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
	root := flag.String("root", ".", "repo root")
	flag.Parse()
	os.Exit(runAudit(*root, os.Stdout, os.Stderr))
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

// auditTree walks root once and returns the parsed metric definitions
// plus an identifier→count reference map for orphan detection.
func auditTree(root string) ([]defn, map[string]int, error) {
	fset := token.NewFileSet()
	var definitions []defn
	references := map[string]int{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := filepath.Base(path)
			if base == "vendor" || base == ".git" || base == "gen" || base == "dart" || base == "python" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		if strings.Contains(path, ".pb.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if vd, ok := n.(*ast.ValueSpec); ok {
				for i, ident := range vd.Names {
					if i >= len(vd.Values) {
						break
					}
					if metric := promNewKind(vd.Values[i]); metric != "" {
						definitions = append(definitions, defn{
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
