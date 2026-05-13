// iouring-audit
//
// Audits pkg/io_uring for SQE/CQE lifecycle issues.
//
// Current checks (package-wide, not per-function):
//  1. At least one SQE submission entrypoint exists. If the package calls
//     GetSQE but never Submit/SubmitAndWait, that's a smell.
//
// xtcp2's netlink workload is asymmetric: many GetSQE calls (one per
// pre-posted recv buffer) are batched and submitted later from a different
// function. Per-function pairing checks are therefore unsound here.
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

func main() {
	root := flag.String("root", "pkg/io_uring", "directory to audit")
	flag.Parse()

	fset := token.NewFileSet()
	getSQECount, submitCount := 0, 0

	err := filepath.WalkDir(*root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			switch selectorName(call.Fun) {
			case "GetSQE":
				getSQECount++
			case "SubmitAndWait", "Submit":
				submitCount++
			}
			return true
		})
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "iouring-audit: walk failed: %v\n", err)
		os.Exit(2)
	}

	fmt.Printf("iouring-audit: scanned %s\n", *root)
	fmt.Printf("iouring-audit: GetSQE calls = %d, Submit*/SubmitAndWait calls = %d\n",
		getSQECount, submitCount)

	if getSQECount > 0 && submitCount == 0 {
		fmt.Fprintf(os.Stderr,
			"iouring-audit: found %d GetSQE call(s) but no submission call — SQEs never reach the kernel\n",
			getSQECount)
		os.Exit(1)
	}

	fmt.Println("iouring-audit: no findings")
}

func selectorName(e ast.Expr) string {
	switch v := e.(type) {
	case *ast.SelectorExpr:
		return v.Sel.Name
	case *ast.Ident:
		return v.Name
	}
	return ""
}
