package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strings"
	"sync"
	"testing"
)

// helpers_test.go covers the four helpers extracted from auditTree in
// the gocyclo-17 → 5 refactor (shouldSkipFile / hasLenGuard /
// findUnguardedAccesses / auditFuncDecls) plus race + benchmarks.
// Existing main_test.go drives the end-to-end audit; these tests pin
// the unit-level contracts.

// parseBody is a small helper that yields the *ast.BlockStmt for the
// body of the first function in src — used by hasLenGuard / accessor
// tests below.
func parseBody(t *testing.T, src string) (*token.FileSet, *ast.FuncDecl) {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, d := range file.Decls {
		if fn, ok := d.(*ast.FuncDecl); ok && fn.Body != nil {
			return fset, fn
		}
	}
	t.Fatalf("no function found in src")
	return nil, nil
}

func TestShouldSkipFile_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		input    string
		want     bool
	}{
		{"positive_plain_go_file", "positive", "pkg/xtcpnl/parser.go", false},
		{"positive_subdir_go_file", "positive", "/repo/pkg/xtcpnl/deep/file.go", false},
		{"negative_test_go", "negative", "pkg/xtcpnl/parser_test.go", true},
		{"negative_pb_go", "negative", "pkg/xtcpnl/types.pb.go", true},
		{"negative_yaml_file", "negative", "pkg/xtcpnl/config.yaml", true},
		{"negative_no_extension", "negative", "pkg/xtcpnl/Makefile", true},
		{"boundary_empty_path", "boundary", "", true},
		{"boundary_only_dot_go", "boundary", ".go", false},
		{"corner_test_in_directory_name", "corner", "/repo/_test/x.go", false},
		{"corner_pb_in_middle_not_suffix", "corner", "pkg/x.pb.go.bak", true},
		{"adversarial_long_path", "adversarial", strings.Repeat("a/", 1<<10) + "x.go", false},
		{"adversarial_path_with_null_byte", "adversarial", "/repo/x\x00.go", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldSkipFile(tc.input); got != tc.want {
				t.Errorf("shouldSkipFile(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestHasLenGuard_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		src      string
		want     bool
	}{
		{
			name:     "positive_len_guard_present",
			category: "positive",
			src: `package x
func f(b []byte) byte {
  if len(b) < 4 { return 0 }
  return b[0]
}`,
			want: true,
		},
		{
			name:     "positive_len_in_assertion_form",
			category: "positive",
			src: `package x
func f(b []byte) int {
  n := len(b)
  return n
}`,
			want: true,
		},
		{
			name:     "negative_no_len_call",
			category: "negative",
			src: `package x
func f(b []byte) byte {
  return b[0]
}`,
			want: false,
		},
		{
			name:     "negative_len_in_comment_only",
			category: "negative",
			src: `package x
func f(b []byte) byte {
  // len(b) is not actually called
  return b[0]
}`,
			want: false,
		},
		{
			name:     "boundary_empty_body",
			category: "boundary",
			src: `package x
func f(b []byte) {}`,
			want: false,
		},
		{
			name:     "corner_len_inside_nested_func_lit",
			category: "corner",
			src: `package x
func f(b []byte) byte {
  cb := func(s []byte) int { return len(s) }
  _ = cb
  return b[0]
}`,
			want: true, // ast.Inspect descends into nested function literals
		},
		{
			name:     "corner_len_in_unreachable_branch",
			category: "corner",
			src: `package x
func f(b []byte) byte {
  if false {
    if len(b) > 0 { return 1 }
  }
  return b[0]
}`,
			want: true, // any len() anywhere silences the audit (documented heuristic)
		},
		{
			name:     "adversarial_method_named_len_on_other_type",
			category: "adversarial",
			src: `package x
type T struct{}
func (T) len() int { return 0 }
func f(b []byte) byte {
  var t T
  _ = t.len() // method call, not built-in len()
  return b[0]
}`,
			want: false, // hasLenGuard checks for Fun=Ident("len"), not SelectorExpr
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			_, fn := parseBody(t, tc.src)
			if got := hasLenGuard(fn.Body); got != tc.want {
				t.Errorf("hasLenGuard = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestFindUnguardedAccesses_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		category     string
		src          string
		wantFindings int
		wantMsgKind  string // "index" or "slice" — checked when wantFindings > 0
	}{
		{
			name:         "positive_index_b",
			category:     "positive",
			src:          `package x
func f(b []byte) byte { return b[0] }`,
			wantFindings: 1,
			wantMsgKind:  "index access",
		},
		{
			name:         "positive_slice_data",
			category:     "positive",
			src:          `package x
func f(data []byte) []byte { return data[0:4] }`,
			wantFindings: 1,
			wantMsgKind:  "slice expression",
		},
		{
			name:         "negative_unknown_identifier",
			category:     "negative",
			src:          `package x
func f(x []byte) byte { return x[0] }`,
			wantFindings: 0,
		},
		{
			name:         "negative_index_into_map",
			category:     "negative",
			src:          `package x
func f(m map[string]int) int { return m["k"] }`,
			wantFindings: 0,
		},
		{
			name:         "boundary_multiple_indices",
			category:     "boundary",
			src:          `package x
func f(buf []byte) byte { return buf[buf[0]] }`,
			wantFindings: 2, // outer buf[…] + inner buf[0] both flagged
		},
		{
			name:         "corner_slice_three_index",
			category:     "corner",
			src:          `package x
func f(buf []byte) []byte { return buf[1:2:4] }`,
			wantFindings: 1,
			wantMsgKind:  "slice expression",
		},
		{
			name:         "corner_chained_byte_slice_names",
			category:     "corner",
			src:          `package x
func f(b []byte) byte {
  payload := b
  _ = payload
  return b[0]
}`,
			wantFindings: 1,
			wantMsgKind:  "index access",
		},
		{
			name:         "adversarial_deeply_nested_index",
			category:     "adversarial",
			src:          `package x
func f(b []byte) byte {
  for i := 0; i < 1; i++ {
    if true {
      for j := 0; j < 1; j++ {
        return b[i+j]
      }
    }
  }
  return 0
}`,
			wantFindings: 1,
			wantMsgKind:  "index access",
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			fset, fn := parseBody(t, tc.src)
			var findings []finding
			findUnguardedAccesses(fset, fn, &findings)
			if len(findings) != tc.wantFindings {
				t.Fatalf("findings = %d (%v), want %d", len(findings), findings, tc.wantFindings)
			}
			if tc.wantMsgKind != "" {
				if !strings.Contains(findings[0].msg, tc.wantMsgKind) {
					t.Errorf("first finding msg = %q, want substring %q", findings[0].msg, tc.wantMsgKind)
				}
			}
		})
	}
}

// auditFuncDecls is the orchestrator that ties hasLenGuard +
// findUnguardedAccesses together over every FuncDecl in a file.
func TestAuditFuncDecls_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		category     string
		src          string
		wantFindings int
	}{
		{
			name:     "positive_one_function_with_findings",
			category: "positive",
			src: `package x
func f(b []byte) byte { return b[0] }`,
			wantFindings: 1,
		},
		{
			name:     "positive_guarded_fn_zero_findings",
			category: "positive",
			src: `package x
func f(b []byte) byte {
  if len(b) < 4 { return 0 }
  return b[0]
}`,
			wantFindings: 0,
		},
		{
			name:     "negative_no_funcs",
			category: "negative",
			src: `package x
const C = 1`,
			wantFindings: 0,
		},
		{
			name:     "boundary_fn_without_body",
			category: "boundary",
			src: `package x
type I interface { Run() } // interface methods have nil Body`,
			wantFindings: 0,
		},
		{
			name:     "corner_two_funcs_one_guarded",
			category: "corner",
			src: `package x
func ok(b []byte) byte {
  if len(b) < 1 { return 0 }
  return b[0]
}
func bad(buf []byte) byte { return buf[0] }`,
			wantFindings: 1,
		},
		{
			name:     "adversarial_many_funcs",
			category: "adversarial",
			src: `package x
func a(b []byte) byte { return b[0] }
func b1(b []byte) byte { return b[0] }
func c(b []byte) byte { return b[0] }
func d(b []byte) byte { return b[0] }`,
			wantFindings: 4,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "test.go", tc.src, parser.SkipObjectResolution)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			var findings []finding
			auditFuncDecls(fset, file, &findings)
			if len(findings) != tc.wantFindings {
				t.Errorf("findings = %d (%v), want %d", len(findings), findings, tc.wantFindings)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// Race — many goroutines calling the read-only helpers concurrently.
// ───────────────────────────────────────────────────────────────────────

func TestHelpers_concurrent(t *testing.T) {
	src := `package x
func f(b []byte) byte {
  if len(b) < 4 { return 0 }
  return b[0]
}
func g(buf []byte) byte { return buf[0] }`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "test.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	const goroutines = 16
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = shouldSkipFile("/repo/pkg/x.go")
				var local []finding
				auditFuncDecls(fset, file, &local)
			}
		}()
	}
	wg.Wait()
}

// ───────────────────────────────────────────────────────────────────────
// Benchmarks
// ───────────────────────────────────────────────────────────────────────

func BenchmarkShouldSkipFile_pb(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = shouldSkipFile("/repo/pkg/x.pb.go")
	}
}

func BenchmarkHasLenGuard_present(b *testing.B) {
	src := `package x
func f(b []byte) byte {
  if len(b) < 4 { return 0 }
  return b[0]
}`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "bench.go", src, parser.SkipObjectResolution)
	if err != nil {
		b.Fatalf("parse: %v", err)
	}
	var fn *ast.FuncDecl
	for _, d := range file.Decls {
		if f, ok := d.(*ast.FuncDecl); ok && f.Body != nil {
			fn = f
			break
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = hasLenGuard(fn.Body)
	}
}

func BenchmarkAuditFuncDecls_unguarded(b *testing.B) {
	src := `package x
func f(buf []byte) byte { return buf[0] }
func g(data []byte) []byte { return data[0:4] }`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "bench.go", src, parser.SkipObjectResolution)
	if err != nil {
		b.Fatalf("parse: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var findings []finding
		auditFuncDecls(fset, file, &findings)
	}
}
