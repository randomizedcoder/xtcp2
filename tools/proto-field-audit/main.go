// proto-field-audit
//
// Cross-checks proto field declarations against the Go code that fills them.
//
// For each *.proto under proto/, parses field names declared in messages.
// Then AST-walks Go source under pkg/ looking for `.Set<Field>(` or
// `.<Field> =` references. Reports any proto field never written in Go.
//
// This is the inverse of the existing Rust proto-audit tool in the sibling
// xdp2 repo (which audits which kernel structs map to which proto fields).
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
	"regexp"
	"strings"
)

// Match `<type> <name> = <number>;` inside `message { ... }`.
var fieldRE = regexp.MustCompile(`^\s*(?:repeated\s+|optional\s+|required\s+)?[\w.<>,]+\s+(\w+)\s*=\s*\d+`)

func main() {
	os.Exit(runMain(os.Args[1:], os.Stdout, os.Stderr))
}

// runMain wires flag parsing + runAudit. Extracted so tests can drive it
// with synthetic args + capture buffers without subprocessing.
func runMain(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("proto-field-audit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	protoRoot := fs.String("proto-root", "proto", "directory containing *.proto")
	goRoot := fs.String("go-root", "pkg", "directory containing Go source")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	return runAudit(*protoRoot, *goRoot, stdout, stderr)
}

// runAudit collects fields from `protoRoot` and references from `goRoot`
// then reports each proto field that has no matching Set<Camel>() call
// or `.Camel = ...` assignment in the Go source. Returns 0 / 1 / 2.
func runAudit(protoRoot, goRoot string, stdout, stderr io.Writer) int {
	fields, err := collectProtoFields(protoRoot)
	if err != nil {
		fmt.Fprintf(stderr, "proto-field-audit: collect protos: %v\n", err)
		return 2
	}
	references, err := collectGoReferences(goRoot)
	if err != nil {
		fmt.Fprintf(stderr, "proto-field-audit: collect go: %v\n", err)
		return 2
	}
	fmt.Fprintf(stdout, "proto-field-audit: %d proto field(s), %d Go reference(s) scanned\n",
		len(fields), len(references))

	unset := 0
	for _, f := range fields {
		camel := snakeToCamel(f.name)
		setterCalled := references["Set"+camel]
		directAssign := references[camel]
		if !setterCalled && !directAssign {
			fmt.Fprintf(stdout, "%s: proto field %q (camel: %s) never written in Go (no Set%s, no .%s assignment)\n",
				f.where, f.name, camel, camel, camel)
			unset++
		}
	}
	if unset > 0 {
		fmt.Fprintf(stderr, "proto-field-audit: %d unset proto field(s)\n", unset)
		return 1
	}
	fmt.Fprintln(stdout, "proto-field-audit: no findings")
	return 0
}

type field struct {
	name  string
	where string
}

func collectProtoFields(root string) ([]field, error) {
	var fields []field
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".proto") {
			return nil
		}
		// #nosec G122 -- this tool runs in CI against trusted local repo source; TOCTOU on .proto files is not a real threat vector
		b, readErr := os.ReadFile(path) //nolint:gosec // mirrored by the #nosec annotation above for the standalone gosec run
		if readErr != nil {
			return readErr
		}
		inMessage := 0
		for i, line := range strings.Split(string(b), "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "message ") {
				inMessage++
				continue
			}
			if inMessage > 0 && strings.Contains(trimmed, "{") {
				inMessage++
			}
			if inMessage > 0 && strings.Contains(trimmed, "}") {
				inMessage--
				continue
			}
			if inMessage == 0 {
				continue
			}
			if strings.HasPrefix(trimmed, "//") || trimmed == "" {
				continue
			}
			m := fieldRE.FindStringSubmatch(trimmed)
			if m == nil {
				continue
			}
			fields = append(fields, field{
				name:  m[1],
				where: fmt.Sprintf("%s:%d", path, i+1),
			})
		}
		return nil
	})
	return fields, err
}

func collectGoReferences(root string) (map[string]bool, error) {
	refs := map[string]bool{}
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			base := filepath.Base(path)
			if base == "vendor" || base == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if parseErr != nil {
			return parseErr
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if sel, ok := n.(*ast.SelectorExpr); ok {
				refs[sel.Sel.Name] = true
			}
			return true
		})
		return nil
	})
	return refs, err
}

func snakeToCamel(s string) string {
	parts := strings.Split(s, "_")
	for i, p := range parts {
		if p == "" {
			continue
		}
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, "")
}
