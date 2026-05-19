// netlink-audit
//
// Audits pkg/xtcpnl for unsafe byte-slice access patterns that could panic on
// malformed netlink messages.
//
// Reports a finding for every []byte index/slice expression in a function that
// does not have a `len(...)` guard somewhere above it in the same function
// body. Conservative: a function with any len() check anywhere is treated as
// "guarded", even if the guard does not cover this specific access.
//
// Exit codes: 0 = no findings, 1 = findings, 2 = internal error.
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

type finding struct {
	pos token.Position
	fn  string
	msg string
}

func main() {
	os.Exit(runMain(os.Args[1:], os.Stdout, os.Stderr))
}

// runMain wires flag parsing + runAudit. Extracted so tests can drive it
// with synthetic args + capture buffers without subprocessing.
func runMain(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("netlink-audit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	root := fs.String("root", "pkg/xtcpnl", "directory to audit")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	return runAudit(*root, stdout, stderr)
}

// runAudit walks `root` and reports per-function indexing without a
// preceding len() guard. Returns 0 (clean), 1 (findings), or 2 (walk
// or parse error). Output is informative; not parsed by the report
// aggregator beyond grepping for "no findings".
func runAudit(root string, stdout, stderr io.Writer) int {
	findings, err := auditTree(root)
	if err != nil {
		fmt.Fprintf(stderr, "netlink-audit: walk failed: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "netlink-audit: scanned %s\n", root)
	if len(findings) == 0 {
		fmt.Fprintln(stdout, "netlink-audit: no findings")
		return 0
	}
	for _, f := range findings {
		fmt.Fprintf(stdout, "%s: %s (in func %s): %s\n", f.pos, f.msg, f.fn,
			"consider adding `if len(b) < N { return ... }`")
	}
	fmt.Fprintf(stderr, "netlink-audit: %d finding(s)\n", len(findings))
	return 1
}

// shouldSkipFile filters non-Go, test, and generated-proto sources.
// Same predicate as metrics-audit; kept inline here so this binary
// stays single-file (no shared internal/ dependency).
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

// hasLenGuard returns true if body contains at least one call to the
// built-in len(). Conservative heuristic: any `len(x)` call anywhere in
// the function body silences the audit for the whole function, even if
// the call does not guard this specific access. This intentionally
// trades false negatives for low review noise in xtcpnl, where every
// safe parser already has at least one len() check.
func hasLenGuard(body *ast.BlockStmt) bool {
	found := false
	ast.Inspect(body, func(m ast.Node) bool {
		call, ok := m.(*ast.CallExpr)
		if !ok {
			return true
		}
		if id, okIdent := call.Fun.(*ast.Ident); okIdent && id.Name == "len" {
			found = true
			return false // short-circuit further descent
		}
		return true
	})
	return found
}

// findUnguardedAccesses appends one finding per IndexExpr / SliceExpr
// whose operand is a known byte-slice identifier (b, buf, data, …).
// Caller must have already confirmed fn has no len() guard.
func findUnguardedAccesses(fset *token.FileSet, fn *ast.FuncDecl, findings *[]finding) {
	ast.Inspect(fn.Body, func(m ast.Node) bool {
		switch e := m.(type) {
		case *ast.IndexExpr:
			if isByteSliceExpr(e.X) {
				*findings = append(*findings, finding{
					pos: fset.Position(e.Pos()),
					fn:  fn.Name.Name,
					msg: "index access without prior len() guard in function",
				})
			}
		case *ast.SliceExpr:
			if isByteSliceExpr(e.X) {
				*findings = append(*findings, finding{
					pos: fset.Position(e.Pos()),
					fn:  fn.Name.Name,
					msg: "slice expression without prior len() guard in function",
				})
			}
		}
		return true
	})
}

// auditFuncDecls walks every top-level FuncDecl in the file, skips those
// without a body or with at least one len() call, and appends findings
// for the rest.
func auditFuncDecls(fset *token.FileSet, file *ast.File, findings *[]finding) {
	ast.Inspect(file, func(n ast.Node) bool {
		fn, ok := n.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			return true
		}
		if hasLenGuard(fn.Body) {
			return true
		}
		findUnguardedAccesses(fset, fn, findings)
		return true
	})
}

// auditTree walks `root` once and produces the full list of unguarded
// byte-slice access findings.
//
// The body was previously a 17-cyclo monolith of three nested ast.Inspect
// closures + WalkDir filtering. The skip filter is shouldSkipFile; the
// len()-guard probe is hasLenGuard; the per-function finding pass is
// auditFuncDecls. Resulting auditTree complexity: 5.
func auditTree(root string) ([]finding, error) {
	fset := token.NewFileSet()
	var findings []finding
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if shouldSkipFile(path) {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		auditFuncDecls(fset, file, &findings)
		return nil
	})
	return findings, err
}

func isByteSliceExpr(e ast.Expr) bool {
	id, ok := e.(*ast.Ident)
	if !ok {
		return false
	}
	// Heuristic: identifier name commonly used for byte slices.
	switch id.Name {
	case "b", "buf", "buffer", "data", "msg", "raw", "p", "payload":
		return true
	}
	return false
}
