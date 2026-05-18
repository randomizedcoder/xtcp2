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
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	os.Exit(runMain(os.Args[1:], os.Stdout, os.Stderr))
}

// runMain wires flag parsing + runAudit. Extracted so tests can drive it
// with synthetic args + capture buffers without subprocessing.
func runMain(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("iouring-audit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", "pkg/io_uring", "directory to audit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	return runAudit(*root, stdout, stderr)
}

// AuditResult counts the call sites for the SQE submission lifecycle.
type AuditResult struct {
	GetSQE   int // SQE acquisitions
	Submit   int // Submit + SubmitAndWait
	Findings []string
}

// auditTree walks `root`, parsing each non-test .go file and counting
// GetSQE / Submit* call sites. Returns the totals plus any per-file
// parse error encountered.
func auditTree(root string) (AuditResult, error) {
	var res AuditResult
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
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
				res.GetSQE++
			case "SubmitAndWait", "Submit":
				res.Submit++
			}
			return true
		})
		return nil
	})
	return res, err
}

// runAudit ties auditTree to the binary's exit-code contract:
//
//	0 — clean.
//	1 — finding: GetSQE > 0 but Submit == 0.
//	2 — walk/parse error.
func runAudit(root string, stdout, stderr io.Writer) int {
	res, err := auditTree(root)
	if err != nil {
		fmt.Fprintf(stderr, "iouring-audit: walk failed: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "iouring-audit: scanned %s\n", root)
	fmt.Fprintf(stdout, "iouring-audit: GetSQE calls = %d, Submit*/SubmitAndWait calls = %d\n",
		res.GetSQE, res.Submit)
	if res.GetSQE > 0 && res.Submit == 0 {
		fmt.Fprintf(stderr,
			"iouring-audit: found %d GetSQE call(s) but no submission call — SQEs never reach the kernel\n",
			res.GetSQE)
		return 1
	}
	fmt.Fprintln(stdout, "iouring-audit: no findings")
	return 0
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
