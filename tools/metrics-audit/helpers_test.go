package main

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// helpers_test.go covers the three helpers extracted from auditTree in
// the gocyclo-17 → 6 refactor (shouldSkipDir / shouldSkipFile /
// collectMetricsFromFile) with positive / negative / boundary / corner /
// adversarial categories, plus race + benchmarks.
// Pre-existing coverage in main_test.go drives the orchestrator
// end-to-end; these tests pin the units in isolation.

func TestShouldSkipDir_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		input    string
		want     bool
	}{
		{"positive_vendor_skipped", "positive", "/repo/vendor", true},
		{"positive_dotgit_skipped", "positive", "/repo/.git", true},
		{"positive_gen_skipped", "positive", "/repo/gen", true},
		{"positive_dart_skipped", "positive", "/repo/dart", true},
		{"positive_python_skipped", "positive", "/repo/python", true},
		{"negative_pkg_not_skipped", "negative", "/repo/pkg", false},
		{"negative_internal_not_skipped", "negative", "/repo/internal", false},
		{"boundary_root_dot", "boundary", ".", false},
		{"boundary_empty_string", "boundary", "", false},
		{"corner_vendor_substring_in_name", "corner", "/repo/myvendor", false}, // exact base only
		{"corner_uppercase_VENDOR", "corner", "/repo/VENDOR", false},           // case-sensitive
		{"corner_trailing_slash", "corner", "/repo/vendor/", true},
		{"adversarial_nested_vendor_dir", "adversarial", "/repo/a/b/c/vendor", true},
		{"adversarial_unicode_directory", "adversarial", "/repo/véndor", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldSkipDir(tc.input); got != tc.want {
				t.Errorf("shouldSkipDir(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

func TestShouldSkipFile_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name     string
		category string
		input    string
		want     bool
	}{
		{"positive_plain_go_file", "positive", "/repo/pkg/foo.go", false},
		{"positive_pkg_main_go", "positive", "/repo/cmd/x/main.go", false},
		{"negative_test_go", "negative", "/repo/pkg/foo_test.go", true},
		{"negative_pb_go", "negative", "/repo/pkg/foo.pb.go", true},
		{"negative_non_go_file", "negative", "/repo/pkg/foo.proto", true},
		{"negative_yaml_file", "negative", "/repo/pkg/foo.yaml", true},
		{"boundary_empty_path", "boundary", "", true},
		{"boundary_dot_go_filename", "boundary", "/repo/.go", false}, // ends with .go
		{"corner_test_go_in_path_segment", "corner", "/repo/_test.go/x.go", false},
		{"corner_pb_in_middle_not_end", "corner", "/repo/x.pb.go.bak", true}, // .pb.go is a substring
		{"corner_double_test_suffix", "corner", "/repo/foo_test_test.go", true},
		{"adversarial_extremely_long_name", "adversarial", strings.Repeat("a", 1<<16) + ".go", false},
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

// collectMetricsFromFile is exercised indirectly via auditTree end-to-end
// tests in main_test.go; here we drive it directly to pin its contract
// when given hand-crafted ASTs.
func TestCollectMetricsFromFile_table(t *testing.T) {
	cases := []struct {
		name        string
		category    string
		src         string
		wantDefs    []string // metric kinds expected, in order
		wantRefs    map[string]int
		wantNoDefs  bool
	}{
		{
			name:     "positive_single_counter_definition",
			category: "positive",
			src: `package x
import "github.com/prometheus/client_golang/prometheus"
var foo = prometheus.NewCounter(prometheus.CounterOpts{Name: "foo"})
`,
			wantDefs: []string{"NewCounter"},
		},
		{
			name:     "positive_multiple_definitions_block",
			category: "positive",
			src: `package x
import "github.com/prometheus/client_golang/prometheus"
var (
  a = prometheus.NewCounter(prometheus.CounterOpts{Name: "a"})
  b = prometheus.NewGauge(prometheus.GaugeOpts{Name: "b"})
  c = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "c"})
)
`,
			wantDefs: []string{"NewCounter", "NewGauge", "NewHistogram"},
		},
		{
			name:     "negative_no_prometheus_definitions",
			category: "negative",
			src: `package x
var foo = "not a metric"
`,
			wantNoDefs: true,
		},
		{
			name:     "negative_unrelated_NewCounter_call",
			category: "negative",
			src: `package x
type customPkg struct{}
var p customPkg
var foo = p.NewCounter() // method on a non-prometheus pkg
func (customPkg) NewCounter() int { return 0 }
`,
			wantNoDefs: true,
		},
		{
			name:     "boundary_value_spec_without_value",
			category: "boundary",
			src: `package x
var foo int // no Values slice
`,
			wantNoDefs: true,
		},
		{
			name:     "boundary_value_spec_more_names_than_values",
			category: "boundary",
			src: `package x
import "github.com/prometheus/client_golang/prometheus"
var a, b = prometheus.NewCounter(prometheus.CounterOpts{Name: "a"}), prometheus.NewGauge(prometheus.GaugeOpts{Name: "b"})
`,
			wantDefs: []string{"NewCounter", "NewGauge"},
		},
		{
			name:     "corner_promauto_dispatch",
			category: "corner",
			src: `package x
import "github.com/prometheus/client_golang/prometheus/promauto"
import "github.com/prometheus/client_golang/prometheus"
var s = promauto.NewSummary(prometheus.SummaryOpts{Name: "s"})
`,
			wantDefs: []string{"NewSummary"},
		},
		{
			name:     "corner_unknown_prom_constructor",
			category: "corner",
			src: `package x
import "github.com/prometheus/client_golang/prometheus"
var x = prometheus.NotARealConstructor()
`,
			wantNoDefs: true,
		},
		{
			name:     "adversarial_definition_inside_function_body",
			category: "adversarial",
			src: `package x
import "github.com/prometheus/client_golang/prometheus"
func wrap() {
  // ValueSpec inside a function — auditTree still finds it because
  // ast.Inspect walks every node, not just top-level decls.
  var inner = prometheus.NewCounter(prometheus.CounterOpts{Name: "inner"})
  _ = inner
}
`,
			wantDefs: []string{"NewCounter"},
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
			var defs []defn
			refs := map[string]int{}
			collectMetricsFromFile(fset, file, &defs, refs)

			if tc.wantNoDefs && len(defs) != 0 {
				t.Errorf("expected no defs, got %d: %+v", len(defs), defs)
				return
			}
			if !tc.wantNoDefs {
				if len(defs) != len(tc.wantDefs) {
					t.Fatalf("defs count = %d (%v), want %d (%v)", len(defs), defs, len(tc.wantDefs), tc.wantDefs)
				}
				for i, want := range tc.wantDefs {
					if defs[i].metric != want {
						t.Errorf("defs[%d].metric = %q, want %q", i, defs[i].metric, want)
					}
				}
			}
		})
	}
}

// TestAuditTree_excludedAllSkipDirs covers all five skip-set entries in
// one walk to catch any future entry that's added without a matching
// case in shouldSkipDir.
func TestAuditTree_excludedAllSkipDirs(t *testing.T) {
	dir := t.TempDir()
	// Seed one .go file with a metric in each skip directory.
	src := `package x
import "github.com/prometheus/client_golang/prometheus"
var v = prometheus.NewCounter(prometheus.CounterOpts{Name: "v"})
`
	for sub := range skippedDirs {
		path := filepath.Join(dir, sub)
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", path, err)
		}
		if err := os.WriteFile(filepath.Join(path, "m.go"), []byte(src), 0o600); err != nil {
			t.Fatalf("write %s/m.go: %v", path, err)
		}
	}
	defs, _, err := auditTree(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(defs) != 0 {
		t.Errorf("all skip dirs should be excluded; got defs %+v", defs)
	}
}

// ───────────────────────────────────────────────────────────────────────
// Race — drive shouldSkipDir + shouldSkipFile concurrently. Both read
// the package-level skippedDirs map; the race detector verifies that's
// safe (Go maps are safe for concurrent read-only access).
// ───────────────────────────────────────────────────────────────────────

func TestShouldSkipDirAndFile_concurrent(t *testing.T) {
	const goroutines = 32
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		i := i
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				p := "/repo/some_dir"
				if (i+j)%5 == 0 {
					p = "/repo/vendor"
				}
				_ = shouldSkipDir(p)
				_ = shouldSkipFile("/repo/file.go")
				_ = shouldSkipFile("/repo/file_test.go")
				_ = shouldSkipFile("/repo/file.pb.go")
			}
		}()
	}
	wg.Wait()
}

// ───────────────────────────────────────────────────────────────────────
// Benchmarks
// ───────────────────────────────────────────────────────────────────────

func BenchmarkShouldSkipDir_skip(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = shouldSkipDir("/repo/vendor")
	}
}

func BenchmarkShouldSkipDir_keep(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = shouldSkipDir("/repo/pkg/foo")
	}
}

func BenchmarkShouldSkipFile_pbgo(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = shouldSkipFile("/repo/pkg/x.pb.go")
	}
}

func BenchmarkCollectMetricsFromFile_smallFile(b *testing.B) {
	b.ReportAllocs()
	src := `package x
import "github.com/prometheus/client_golang/prometheus"
var (
  a = prometheus.NewCounter(prometheus.CounterOpts{Name: "a"})
  b = prometheus.NewGauge(prometheus.GaugeOpts{Name: "b"})
)
`
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "bench.go", src, parser.SkipObjectResolution)
	if err != nil {
		b.Fatalf("parse: %v", err)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var defs []defn
		refs := map[string]int{}
		collectMetricsFromFile(fset, file, &defs, refs)
	}
}
