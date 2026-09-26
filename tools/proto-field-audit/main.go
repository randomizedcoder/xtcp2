// proto-field-audit
//
// Cross-checks proto field declarations against the Go code that fills them.
//
// For each *.proto under proto/, parses field names declared in messages.
// Then AST-walks Go source under the module (pkg/, gen/, cmd/, …) looking for
// `.Set<Field>(` or `.<Field> =` references. Reports any proto field never
// written in Go. (Generated bindings live in gen/go/, so the default scan root
// is the module root rather than pkg/.)
//
// It also enforces the kernel-source annotation convention of
// xtcp_flat_record.proto: every field whose tag is in the kernel-payload
// range (>= 1000) must carry a trailing comment naming the kernel struct
// member / INET_DIAG_* attribute / SK_MEMINFO_* slot it copies (or say it is
// derived by xtcp). See docs/protobuf-formats.md "Field layout policy".
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
	"strconv"
	"strings"
)

// Match `<type> <name> = <number>;` inside `message { ... }`.
// Group 1 = field name, group 2 = field number.
var fieldRE = regexp.MustCompile(`^\s*(?:repeated\s+|optional\s+|required\s+)?[\w.<>,]+\s+(\w+)\s*=\s*(\d+)`)

// kernelAnnotationMinTag is the first field number of the kernel-payload
// range in xtcp_flat_record.proto (metadata 1-299, enrichment 300-399, spare
// 400-999, payload 1000+). Every field at or above it copies a kernel value
// and must say which one in a trailing comment.
const kernelAnnotationMinTag = 1000

// kernelAnnotationRE is the shape of the trailing comment every payload field
// must carry, naming the kernel source it copies. Accepted forms:
//
//	// struct tcp_info.tcpi_rttvar (__u32)
//	// SK_MEMINFO_RCVBUF (__u32, sock_diag.h)
//	// INET_DIAG_TOS (5): inet->tos (__u8, inet_diag.c)
//	// derived by xtcp from inet_diag_cong (not a kernel field)
var kernelAnnotationRE = regexp.MustCompile(`struct \w+\.\w+|INET_DIAG_\w+ \(\d+\)|SK_MEMINFO_\w+|derived by xtcp`)

func main() {
	os.Exit(runMain(os.Args[1:], os.Stdout, os.Stderr))
}

// runMain wires flag parsing + runAudit. Extracted so tests can drive it
// with synthetic args + capture buffers without subprocessing.
func runMain(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("proto-field-audit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	protoRoot := fs.String("proto-root", "proto", "directory containing *.proto")
	goRoot := fs.String("go-root", ".", "directory containing Go source (module root; generated bindings live in gen/go/)")
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
	unannotated := reportUnannotatedPayloadFields(fields, stdout)
	if unset > 0 || unannotated > 0 {
		fmt.Fprintf(stderr, "proto-field-audit: %d unset proto field(s), %d payload field(s) without kernel-source comment\n",
			unset, unannotated)
		return 1
	}
	fmt.Fprintln(stdout, "proto-field-audit: no findings")
	return 0
}

// reportUnannotatedPayloadFields prints one line per field that needs a
// kernel-source annotation (tag >= kernelAnnotationMinTag) but lacks one,
// and returns the count. Fields below the payload range are ignored.
func reportUnannotatedPayloadFields(fields []field, stdout io.Writer) int {
	n := 0
	for _, f := range fields {
		if f.needsKernelAnnotation() && !f.hasKernelAnnotation() {
			fmt.Fprintf(stdout, "%s: proto field %q (tag %d) lacks a kernel-source trailing comment "+
				"(want `// struct <s>.<member> (<type>)`, `// INET_DIAG_<X> (<n>): ...`, `// SK_MEMINFO_<X> ...` or `// derived by xtcp ...`)\n",
				f.where, f.name, f.number)
			n++
		}
	}
	return n
}

type field struct {
	name    string
	number  int
	comment string // trailing `//` comment on the declaring line, trimmed; "" if none
	where   string
}

// needsKernelAnnotation reports whether the field sits in the kernel-payload
// tag range and therefore must name its kernel source.
func (f field) needsKernelAnnotation() bool { return f.number >= kernelAnnotationMinTag }

// hasKernelAnnotation reports whether the field's trailing comment matches
// one of the accepted kernel-source forms (kernelAnnotationRE).
func (f field) hasKernelAnnotation() bool { return kernelAnnotationRE.MatchString(f.comment) }

// trailingComment returns the text after the first `//` on a trimmed
// field line, with surrounding whitespace removed; "" when there is none.
func trailingComment(trimmed string) string {
	if i := strings.Index(trimmed, "//"); i >= 0 {
		return strings.TrimSpace(trimmed[i+2:])
	}
	return ""
}

// updateProtoMessageDepth steps the message-depth state machine for one
// line. Returns (newDepth, skipLine). When skipLine is true the caller
// must continue to the next line — either the line declared a new
// `message` block, or it contained a closing `}` that consumed the
// trailing-brace bookkeeping.
//
// Preserves the original semantics: a line containing only `{` (without
// `message ` prefix) increments depth when already inside one message,
// then falls through; a line containing only `}` decrements + returns
// skip=true. Lines outside any message block always return skip=false.
func updateProtoMessageDepth(trimmed string, inMessage int) (int, bool) {
	if strings.HasPrefix(trimmed, "message ") {
		return inMessage + 1, true
	}
	if inMessage > 0 && strings.Contains(trimmed, "{") {
		inMessage++
	}
	if inMessage > 0 && strings.Contains(trimmed, "}") {
		return inMessage - 1, true
	}
	return inMessage, false
}

// extractFieldsFromProto parses one proto file's contents into a slice
// of field declarations. Pure function — no I/O — so it's directly
// table-testable. The previous body was inlined inside collectProtoFields
// alongside WalkDir + ReadFile, contributing most of its gocyclo 15.
func extractFieldsFromProto(path string, contents []byte) []field {
	var fields []field
	inMessage := 0
	for i, line := range strings.Split(string(contents), "\n") {
		trimmed := strings.TrimSpace(line)
		var skip bool
		inMessage, skip = updateProtoMessageDepth(trimmed, inMessage)
		if skip {
			continue
		}
		if inMessage == 0 {
			continue
		}
		if strings.HasPrefix(trimmed, "//") || trimmed == "" {
			continue
		}
		if m := fieldRE.FindStringSubmatch(trimmed); m != nil {
			number, err := strconv.Atoi(m[2])
			if err != nil {
				// \d+ guarantees digits, so this is only reachable on overflow —
				// a tag no valid proto carries. Keep the field so its name is
				// still audited; 0 is outside every allocated range and is
				// flagged by the number checks.
				number = 0
			}
			fields = append(fields, field{
				name:    m[1],
				number:  number,
				comment: trailingComment(trimmed),
				where:   fmt.Sprintf("%s:%d", path, i+1),
			})
		}
	}
	return fields
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
		fields = append(fields, extractFieldsFromProto(path, b)...)
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
