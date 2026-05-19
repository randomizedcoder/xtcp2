package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ───────────────────────────────────────────────────────────────────────
// Helpers — atoiOr0, fileExists, bytesIndex, relpath, readKV*, readLines
// ───────────────────────────────────────────────────────────────────────

func TestAtoiOr0(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"42", 42},
		{"", 0},
		{"not-a-number", 0},
		{"0", 0},
		{"-123", -123},
		{"   ", 0},      // whitespace-only also unparseable
		{"1.5", 0},      // floats aren't integers
		{"42a", 0},      // trailing garbage
		{"2147483647", 2147483647}, // int32 max round-trips
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			if got := atoiOr0(tc.in); got != tc.want {
				t.Errorf("atoiOr0(%q) = %d, want %d", tc.in, got, tc.want)
			}
		})
	}
}

func TestFileExists(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "exists")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !fileExists(path) {
		t.Error("fileExists should return true for existing file")
	}
	if fileExists(filepath.Join(dir, "missing")) {
		t.Error("fileExists should return false for missing path")
	}
}

func TestBytesIndex(t *testing.T) {
	cases := []struct {
		name     string
		haystack []byte
		needle   []byte
		want     int
	}{
		{"middle", []byte("hello world"), []byte("world"), 6},
		{"missing", []byte("abc"), []byte("d"), -1},
		{"empty_needle", []byte("abc"), []byte(""), 0},
		{"empty_haystack", []byte(""), []byte("x"), -1},
		{"empty_both", []byte(""), []byte(""), 0},
		{"exact_match", []byte("abc"), []byte("abc"), 0},
		{"prefix_match", []byte("abcdef"), []byte("abc"), 0},
		{"suffix_match", []byte("abcdef"), []byte("def"), 3},
		{"needle_longer_than_haystack", []byte("ab"), []byte("abcd"), -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := bytesIndex(tc.haystack, tc.needle); got != tc.want {
				t.Errorf("bytesIndex(%q, %q) = %d, want %d", tc.haystack, tc.needle, got, tc.want)
			}
		})
	}
}

func TestRelpath(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "sub", "file.go")
	r := relpath(p, dir)
	if r != filepath.Join("sub", "file.go") {
		t.Errorf("relpath = %q, want sub/file.go", r)
	}
	// Empty root → unchanged.
	if got := relpath(p, ""); got != p {
		t.Errorf("relpath with empty root should return input")
	}
	// Path outside root → unchanged (contains ..).
	outside := relpath("/var/log/foo", dir)
	if !strings.HasPrefix(outside, "/var") {
		t.Errorf("relpath outside root should preserve absolute path; got %q", outside)
	}
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	if err := os.WriteFile(p, []byte("hello"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readFile(p); got != "hello" {
		t.Errorf("readFile = %q", got)
	}
	if got := readFile(filepath.Join(dir, "missing")); got != "" {
		t.Errorf("readFile(missing) should be empty; got %q", got)
	}
}

func TestReadLines(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "x")
	if err := os.WriteFile(p, []byte("a\nb\nc"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := readLines(p)
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("readLines len = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// Missing file → nil.
	if r := readLines(filepath.Join(dir, "missing")); r != nil {
		t.Errorf("readLines(missing) should be nil; got %v", r)
	}
}

func TestReadKVFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "kv")
	if err := os.WriteFile(p, []byte("a=1\nb=hello\nbad-line\nc = trimmed \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := readKVFile(p)
	if got["a"] != "1" {
		t.Errorf("a = %q, want 1", got["a"])
	}
	if got["b"] != "hello" {
		t.Errorf("b = %q, want hello", got["b"])
	}
	if got["c"] != "trimmed" {
		t.Errorf("c = %q, want trimmed (whitespace stripped)", got["c"])
	}
	if _, ok := got["bad-line"]; ok {
		t.Error("bad-line should not produce a key (no =)")
	}
}

func TestReadRuntimes(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "rt")
	if err := os.WriteFile(p, []byte("golangci=12\ngosec=3\nbad=notnum\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := readRuntimes(p)
	if got["golangci"] != 12 || got["gosec"] != 3 {
		t.Errorf("readRuntimes = %v", got)
	}
	if got["bad"] != 0 {
		t.Errorf("unparseable value should map to 0; got %d", got["bad"])
	}
}

func TestReadExitCodes(t *testing.T) {
	// readExitCodes is an alias for readRuntimes; verify it exists + works.
	dir := t.TempDir()
	p := filepath.Join(dir, "ec")
	if err := os.WriteFile(p, []byte("x=2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := readExitCodes(p); got["x"] != 2 {
		t.Errorf("readExitCodes returned %v", got)
	}
}

// ───────────────────────────────────────────────────────────────────────
// Coverage parser — reads coverage-func.out + coverage-per-package.tsv
// ───────────────────────────────────────────────────────────────────────

func TestParseCoverage(t *testing.T) {
	dir := t.TempDir()
	// Minimal coverage-func.out — anything that ends with the total: line.
	funcOut := "github.com/x/y/foo.go:10:\tFoo\t100.0%\n" +
		"total:\t(statements)\t42.3%\n"
	if err := os.WriteFile(filepath.Join(dir, "coverage-func.out"), []byte(funcOut), 0o600); err != nil {
		t.Fatal(err)
	}
	tsv := "pkg/io_uring\t78.6\npkg/xtcp\t17.0\ncmd/xtcp2\t73.2\n"
	if err := os.WriteFile(filepath.Join(dir, "coverage-per-package.tsv"), []byte(tsv), 0o600); err != nil {
		t.Fatal(err)
	}
	cov := parseCoverage(dir)
	if !cov.Available {
		t.Fatal("coverage should be Available with both files present")
	}
	if cov.Total != 42.3 {
		t.Errorf("Total = %.1f, want 42.3", cov.Total)
	}
	if cov.PerPackage["pkg/io_uring"] != 78.6 {
		t.Errorf("pkg/io_uring = %.1f, want 78.6", cov.PerPackage["pkg/io_uring"])
	}
	if len(cov.PerPackage) != 3 {
		t.Errorf("PerPackage len = %d, want 3", len(cov.PerPackage))
	}
}

func TestParseCoverage_missing(t *testing.T) {
	dir := t.TempDir()
	cov := parseCoverage(dir)
	if cov.Available {
		t.Error("coverage should not be Available without input files")
	}
	if cov.Total != 0 {
		t.Errorf("Total = %.1f, want 0 when missing", cov.Total)
	}
}

func TestCountBelowThreshold(t *testing.T) {
	cov := Coverage{
		PerPackage: map[string]float64{
			"a": 100.0,
			"b": 89.9,
			"c": 50.0,
			"d": 90.0, // exactly at threshold ⇒ NOT below
		},
	}
	if got := countBelowThreshold(cov); got != 2 {
		t.Errorf("countBelowThreshold = %d, want 2", got)
	}
}

// ───────────────────────────────────────────────────────────────────────
// loadKnownFailures — line-per-test failure allowlist
// ───────────────────────────────────────────────────────────────────────

func TestLoadKnownFailures(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "kf")
	body := `# leading comment
TestReconcileMaps
# blank line below

TestReconcileMaps/Sub
pkg/misc.TestCheckFilePermissions
`
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	got := loadKnownFailures(p)
	if !got["TestReconcileMaps"] {
		t.Error("TestReconcileMaps should be in allowlist")
	}
	if !got["TestReconcileMaps/Sub"] {
		t.Error("subtest entry missing")
	}
	if !got["pkg/misc.TestCheckFilePermissions"] {
		t.Error("Package.Test form missing")
	}
}

func TestLoadKnownFailures_emptyPath(t *testing.T) {
	got := loadKnownFailures("")
	if got == nil {
		t.Fatal("loadKnownFailures(\"\") should return non-nil empty map")
	}
	if len(got) != 0 {
		t.Errorf("empty path should produce empty map; got %v", got)
	}
}

func TestLoadKnownFailures_missingFile(t *testing.T) {
	got := loadKnownFailures(filepath.Join(t.TempDir(), "missing"))
	if got == nil || len(got) != 0 {
		t.Errorf("missing file should produce empty (not nil) map; got %v", got)
	}
}

// ───────────────────────────────────────────────────────────────────────
// statusLabel / isTieredTool / isQuickFixableRule / severityOrder
// ───────────────────────────────────────────────────────────────────────

func TestStatusLabel(t *testing.T) {
	cases := []struct {
		s    ToolStatus
		want string
	}{
		{s: ToolStatus{Available: false}, want: "not run"},
		{s: ToolStatus{Available: true, ExitCode: 0, Findings: 0}, want: "clean"},
		{s: ToolStatus{Available: true, ExitCode: 0, Findings: 5}, want: "findings"},
		{s: ToolStatus{Available: true, ExitCode: 2, Findings: 0}, want: "exit 2"},
	}
	for _, tc := range cases {
		if got := statusLabel(tc.s); got != tc.want {
			t.Errorf("statusLabel(%+v) = %q, want %q", tc.s, got, tc.want)
		}
	}
}

func TestIsTieredTool(t *testing.T) {
	if !isTieredTool("golangci-lint") {
		t.Error("golangci-lint should be a tiered tool")
	}
	if isTieredTool("gosec") {
		t.Error("gosec should not be a tiered tool")
	}
}

func TestIsQuickFixableRule(t *testing.T) {
	// At least one canonical fixable rule should report true.
	if !isQuickFixableRule("gofmt") && !isQuickFixableRule("misspell") &&
		!isQuickFixableRule("unconvert") {
		t.Error("expected at least one of gofmt/misspell/unconvert to be quick-fixable")
	}
	if isQuickFixableRule("not-a-real-rule") {
		t.Error("unknown rule should not be quick-fixable")
	}
}

func TestSeverityOrder(t *testing.T) {
	// Should order: error < warning < info < other.
	if severityOrder(severityError) >= severityOrder(severityWarning) {
		t.Errorf("error (%d) should sort before warning (%d)",
			severityOrder(severityError), severityOrder(severityWarning))
	}
	if severityOrder(severityWarning) >= severityOrder(severityInfo) {
		t.Errorf("warning should sort before info")
	}
}

// ───────────────────────────────────────────────────────────────────────
// parseGolangci — JSON with Issues
// ───────────────────────────────────────────────────────────────────────

func TestParseGolangci_happy(t *testing.T) {
	dir := t.TempDir()
	body := `{
  "Issues": [
    {"FromLinter":"govet","Text":"shadow: declaration of err shadows declaration","Severity":"warning",
     "Pos":{"Filename":"pkg/x/a.go","Line":10,"Column":5}},
    {"FromLinter":"errcheck","Text":"Error return value is not checked","Severity":"error",
     "Pos":{"Filename":"pkg/x/b.go","Line":20,"Column":1}}
  ]
}`
	p := filepath.Join(dir, "gci.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	fs, ok := parseGolangci(p, "golangci-lint", 0, "")
	if !ok {
		t.Fatal("parseGolangci returned ok=false")
	}
	if len(fs) != 2 {
		t.Fatalf("len(findings) = %d, want 2", len(fs))
	}
	if fs[0].Rule != "govet" || fs[1].Rule != "errcheck" {
		t.Errorf("rules: %q,%q", fs[0].Rule, fs[1].Rule)
	}
}

func TestParseGolangci_missing(t *testing.T) {
	if _, ok := parseGolangci(filepath.Join(t.TempDir(), "missing"), "x", 0, ""); ok {
		t.Error("missing file should produce ok=false")
	}
}

func TestParseGolangci_garbageBeforeJSON(t *testing.T) {
	dir := t.TempDir()
	body := "warning: ignoring foo\n{\"Issues\":[{\"FromLinter\":\"x\",\"Pos\":{\"Filename\":\"f\",\"Line\":1}}]}"
	p := filepath.Join(dir, "g.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	fs, ok := parseGolangci(p, "golangci-lint", 0, "")
	if !ok {
		t.Fatal("ok=false on garbage-prefix; defensive bytesIndex should have rescued the parse")
	}
	if len(fs) != 1 {
		t.Errorf("len(findings) = %d, want 1", len(fs))
	}
}

func TestParseGolangci_emptyFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "empty.json")
	if err := os.WriteFile(p, []byte(""), 0o600); err != nil {
		t.Fatal(err)
	}
	fs, ok := parseGolangci(p, "x", 0, "")
	if !ok {
		t.Error("empty file should parse as ok=true (no issues)")
	}
	if len(fs) != 0 {
		t.Errorf("empty file should produce 0 findings; got %d", len(fs))
	}
}

// ───────────────────────────────────────────────────────────────────────
// parseGosec — JSON with Issues array (different shape than golangci)
// ───────────────────────────────────────────────────────────────────────

func TestParseGosec(t *testing.T) {
	dir := t.TempDir()
	body := `{
  "Issues": [
    {"severity":"HIGH","rule_id":"G104","details":"Errors unhandled","cwe":{"ID":"703"},
     "file":"pkg/x/y.go","line":"42","column":"3"}
  ]
}`
	p := filepath.Join(dir, "gosec.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	fs, ok := parseGosec(p, "")
	if !ok {
		t.Fatal("parseGosec ok=false")
	}
	if len(fs) != 1 {
		t.Fatalf("len(findings) = %d, want 1", len(fs))
	}
	if fs[0].Rule != "G104" {
		t.Errorf("rule = %q, want G104", fs[0].Rule)
	}
	if fs[0].Line != 42 {
		t.Errorf("line = %d, want 42", fs[0].Line)
	}
	if !strings.Contains(fs[0].Message, "Errors unhandled") {
		t.Errorf("message missing details: %q", fs[0].Message)
	}
}

func TestParseGosec_missing(t *testing.T) {
	if _, ok := parseGosec(filepath.Join(t.TempDir(), "missing"), ""); ok {
		t.Error("missing file should produce ok=false")
	}
}

// ───────────────────────────────────────────────────────────────────────
// parseLineFindings — "file:line[:col] message"
// ───────────────────────────────────────────────────────────────────────

func TestParseLineFindings(t *testing.T) {
	dir := t.TempDir()
	body := `pkg/x/a.go:5: vet: useless conversion
pkg/x/b.go:10:3: index out of range
not-a-finding-line
`
	p := filepath.Join(dir, "vet.out")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	fs, ok := parseLineFindings(p, "go-vet", 0, "default-rule")
	if !ok {
		t.Fatal("parseLineFindings ok=false")
	}
	if len(fs) < 2 {
		t.Fatalf("expected at least 2 findings; got %d", len(fs))
	}
	if fs[0].Tool != "go-vet" {
		t.Errorf("Tool = %q", fs[0].Tool)
	}
}

// ───────────────────────────────────────────────────────────────────────
// parseCliHelpSmoke — `<binary> <rc> <bytes>` per line
// ───────────────────────────────────────────────────────────────────────

func TestParseCliHelpSmoke(t *testing.T) {
	dir := t.TempDir()
	body := "xtcp2 0 1234\nns 2 56\n# comment-or-bad-line\nbroken 1\n"
	p := filepath.Join(dir, "smoke.out")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	results, ok := parseCliHelpSmoke(p)
	if !ok {
		t.Fatal("parseCliHelpSmoke ok=false")
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 valid results (broken line dropped); got %d", len(results))
	}
	if results[0].Binary != "xtcp2" || results[0].ExitCode != 0 || results[0].Bytes != 1234 {
		t.Errorf("row 0: %+v", results[0])
	}
	if !results[0].OK {
		t.Errorf("xtcp2 rc=0 bytes>0 should be OK")
	}
}

// ───────────────────────────────────────────────────────────────────────
// parseGoTest — `go test -json` event stream
// ───────────────────────────────────────────────────────────────────────

func TestParseGoTest(t *testing.T) {
	dir := t.TempDir()
	// Each line is a JSON event from `go test -json`.
	body := `{"Time":"2024-01-01T00:00:00Z","Action":"pass","Package":"pkg/x","Test":"TestA","Elapsed":0.01}
{"Time":"2024-01-01T00:00:00Z","Action":"fail","Package":"pkg/x","Test":"TestB","Elapsed":0.02}
{"Time":"2024-01-01T00:00:00Z","Action":"skip","Package":"pkg/x","Test":"TestC","Elapsed":0.00}
`
	p := filepath.Join(dir, "tests.json")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{"TestB": true}
	results, ok := parseGoTest(p, known)
	if !ok {
		t.Fatal("parseGoTest ok=false")
	}
	var pass, fail, skip int
	var preexist int
	for _, r := range results {
		switch r.Action {
		case testActionPass:
			pass++
		case testActionFail:
			fail++
			if r.Preexist {
				preexist++
			}
		case testActionSkip:
			skip++
		}
	}
	if pass != 1 || fail != 1 || skip != 1 {
		t.Errorf("counts: pass=%d fail=%d skip=%d (want 1/1/1)", pass, fail, skip)
	}
	if preexist != 1 {
		t.Errorf("TestB should be marked preexist; got preexist=%d", preexist)
	}
}

func TestParseGoTest_missing(t *testing.T) {
	if _, ok := parseGoTest(filepath.Join(t.TempDir(), "missing"), nil); ok {
		t.Error("missing file should produce ok=false")
	}
}

// ───────────────────────────────────────────────────────────────────────
// aggregateByLinter + aggregateByFile
// ───────────────────────────────────────────────────────────────────────

func TestAggregateByLinter(t *testing.T) {
	findings := []Finding{
		{Tool: "golangci-lint", Rule: "govet", File: "a.go"},
		{Tool: "golangci-lint", Rule: "govet", File: "b.go"},
		{Tool: "golangci-lint", Rule: "errcheck", File: "a.go"},
		{Tool: "gosec", Rule: "G104", File: "c.go"},
	}
	got := aggregateByLinter(findings)
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	// Sorted by Count descending, then linter name ascending.
	if got[0].Linter != "govet" {
		t.Errorf("top linter = %q, want govet", got[0].Linter)
	}
	if got[0].Count != 2 {
		t.Errorf("govet count = %d, want 2", got[0].Count)
	}
}

func TestAggregateByFile(t *testing.T) {
	findings := []Finding{
		{File: "a.go", Rule: "r1"},
		{File: "a.go", Rule: "r1"},
		{File: "a.go", Rule: "r2"},
		{File: "b.go", Rule: "r1"},
		{File: "", Rule: "noop"}, // empty File → skipped
	}
	got := aggregateByFile(findings)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].File != "a.go" || got[0].Count != 3 {
		t.Errorf("top file = %+v, want a.go count=3", got[0])
	}
	if !strings.Contains(got[0].Top[0], "r1") {
		t.Errorf("top rule should be r1: %v", got[0].Top)
	}
}

// ───────────────────────────────────────────────────────────────────────
// parseAuditOutput — `file:line[:col]: msg` lines, plus free-form lines
// ───────────────────────────────────────────────────────────────────────

func TestParseAuditOutput(t *testing.T) {
	dir := t.TempDir()
	body := `netlink-audit: scanned pkg/xtcpnl
pkg/x/a.go:10:3: index access without prior len() guard
pkg/x/b.go:5: missing alignment
free-form summary line
`
	p := filepath.Join(dir, "audit.out")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	fs, ok := parseAuditOutput(p, "netlink-audit")
	if !ok {
		t.Fatal("parseAuditOutput ok=false")
	}
	// 1 summary-prefixed line is dropped → 3 findings (col, no-col, free-form).
	if len(fs) != 3 {
		t.Fatalf("len = %d, want 3 (summary should be dropped)", len(fs))
	}
	if fs[0].Tool != "netlink-audit" {
		t.Errorf("Tool = %q", fs[0].Tool)
	}
	if fs[0].Line != 10 || fs[0].Column != 3 {
		t.Errorf("first finding: line=%d col=%d", fs[0].Line, fs[0].Column)
	}
}

func TestParseAuditOutput_missing(t *testing.T) {
	if _, ok := parseAuditOutput(filepath.Join(t.TempDir(), "missing"), "x"); ok {
		t.Error("missing file should produce ok=false")
	}
}

// ───────────────────────────────────────────────────────────────────────
// synthRecommendations — produces a list of follow-up suggestions
// based on the rendered input.
// ───────────────────────────────────────────────────────────────────────

func TestSynthRecommendations_nonEmpty(t *testing.T) {
	// A renderInput with active linter findings + a below-threshold pkg
	// should produce at least one recommendation. (We don't pin specific
	// strings — the function may evolve — just check non-emptiness.)
	r := renderInput{
		reportInput: reportInput{
			Findings: []Finding{
				{Tool: "golangci-lint", Rule: "govet", File: "a.go", Tier: 0},
			},
			Coverage: Coverage{
				Total:      40,
				PerPackage: map[string]float64{"pkg/xtcp": 17.0},
				Available:  true,
			},
		},
		TierCounts: tierCounts{T0: 1},
	}
	got := synthRecommendations(r)
	if len(got) == 0 {
		t.Error("synthRecommendations should produce at least one item for a non-clean report")
	}
}

// ───────────────────────────────────────────────────────────────────────
// emit — template execution. Just verify it runs against a populated
// reportInput and produces non-empty markdown.
// ───────────────────────────────────────────────────────────────────────

func TestEmit(t *testing.T) {
	in := reportInput{
		Versions:   map[string]string{"go": "go1.25"},
		Findings:   []Finding{{Tool: "golangci-lint", Rule: "govet", File: "a.go", Line: 1, Severity: severityWarning, Tier: 0, Message: "shadow"}},
		Status:     []ToolStatus{{Name: "go vet", Available: true}},
		Exclusions: []ConfigExclusion{{Source: "x.yml", Rule: "x", Scope: "y", Justified: true, Note: "n"}},
		Coverage: Coverage{
			Total:      88.0,
			PerPackage: map[string]float64{"pkg/xtcp": 17.0, "pkg/xtcpnl": 92.0},
			Available:  true,
		},
	}
	var sb strings.Builder
	if err := emit(&sb, in); err != nil {
		t.Fatalf("emit returned err: %v", err)
	}
	out := sb.String()
	if !strings.Contains(out, "## 1. Executive summary") {
		t.Errorf("missing executive summary header")
	}
	if !strings.Contains(out, "## 13. Test coverage") {
		t.Errorf("missing coverage section")
	}
	if !strings.Contains(out, "pkg/xtcp") {
		t.Errorf("coverage row missing pkg/xtcp")
	}
}

// ───────────────────────────────────────────────────────────────────────
// parseExclusions — reads .golangci*.yml files in repo root
// ───────────────────────────────────────────────────────────────────────

func TestParseExclusions(t *testing.T) {
	dir := t.TempDir()
	yaml := `linters:
  enable:
    - govet
  exclude-rules:
    # this is the justification
    - path: 'pkg/foo/.*\.go'
    - text: 'shadow:'
`
	if err := os.WriteFile(filepath.Join(dir, ".golangci.yml"), []byte(yaml), 0o600); err != nil {
		t.Fatal(err)
	}
	out := parseExclusions(dir)
	// Should include both YAML-extracted exclusions + the hardcoded gosec ones.
	var foundYAML, foundGosec bool
	for _, e := range out {
		if strings.Contains(e.Rule, "pkg/foo") {
			foundYAML = true
			if !e.Justified {
				t.Errorf("YAML rule should pick up adjacent comment as justification: %+v", e)
			}
		}
		if e.Rule == "G103" {
			foundGosec = true
		}
	}
	if !foundYAML {
		t.Error("YAML exclusion path/foo not extracted")
	}
	if !foundGosec {
		t.Error("hardcoded G103 gosec exclusion missing from output")
	}
}
