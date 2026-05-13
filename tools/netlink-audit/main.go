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
	root := flag.String("root", "pkg/xtcpnl", "directory to audit")
	flag.Parse()

	fset := token.NewFileSet()
	var findings []finding

	err := filepath.WalkDir(*root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
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
			fn, ok := n.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				return true
			}
			hasLenGuard := false
			ast.Inspect(fn.Body, func(m ast.Node) bool {
				call, ok := m.(*ast.CallExpr)
				if !ok {
					return true
				}
				if id, ok := call.Fun.(*ast.Ident); ok && id.Name == "len" {
					hasLenGuard = true
				}
				return true
			})
			if hasLenGuard {
				return true
			}
			ast.Inspect(fn.Body, func(m ast.Node) bool {
				switch e := m.(type) {
				case *ast.IndexExpr:
					if isByteSliceExpr(e.X) {
						findings = append(findings, finding{
							pos: fset.Position(e.Pos()),
							fn:  fn.Name.Name,
							msg: "index access without prior len() guard in function",
						})
					}
				case *ast.SliceExpr:
					if isByteSliceExpr(e.X) {
						findings = append(findings, finding{
							pos: fset.Position(e.Pos()),
							fn:  fn.Name.Name,
							msg: "slice expression without prior len() guard in function",
						})
					}
				}
				return true
			})
			return true
		})
		return nil
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "netlink-audit: walk failed: %v\n", err)
		os.Exit(2)
	}

	fmt.Printf("netlink-audit: scanned %s\n", *root)
	if len(findings) == 0 {
		fmt.Println("netlink-audit: no findings")
		return
	}
	for _, f := range findings {
		fmt.Printf("%s: %s (in func %s): %s\n", f.pos, f.msg, f.fn, "consider adding `if len(b) < N { return ... }`")
	}
	fmt.Fprintf(os.Stderr, "netlink-audit: %d finding(s)\n", len(findings))
	os.Exit(1)
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
