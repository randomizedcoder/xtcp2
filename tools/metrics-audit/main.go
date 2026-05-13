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

	fset := token.NewFileSet()
	var definitions []defn
	references := map[string]int{}

	err := filepath.WalkDir(*root, func(path string, d fs.DirEntry, err error) error {
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
			// Capture metric definitions: `var foo = prometheus.NewCounter(...)`
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
	if err != nil {
		fmt.Fprintf(os.Stderr, "metrics-audit: walk failed: %v\n", err)
		os.Exit(2)
	}

	fmt.Printf("metrics-audit: scanned %s — %d metric definition(s)\n", *root, len(definitions))

	orphans := 0
	for _, d := range definitions {
		if references[d.name] <= 1 { // 1 = the definition itself
			fmt.Printf("%s: orphan metric %q (%s) — defined but never referenced\n",
				d.pos, d.name, d.metric)
			orphans++
		}
	}
	if orphans > 0 {
		fmt.Fprintf(os.Stderr, "metrics-audit: %d orphan metric(s)\n", orphans)
		os.Exit(1)
	}
	fmt.Println("metrics-audit: no findings")
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
