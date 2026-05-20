package main

import (
	"os"
	"path/filepath"
	"testing"
)

// ratchet_test.go covers the coverage-ratchet helpers added to
// tools/quality-report/main.go:
//   - readCoverageBaseline (parses the baseline file)
//   - evaluateCoverageRatchet (decides pass / breach)
// Wired into runMain via the -coverage-baseline + -coverage-max-drop
// flags; the orchestrator in nix/quality-report/default.nix passes the
// flag pointing at ./docs/coverage-baseline.txt inside the Nix
// sandbox.

// ───────────────────────────────────────────────────────────────────────
// readCoverageBaseline
// ───────────────────────────────────────────────────────────────────────

func TestReadCoverageBaseline_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		category  string
		writeBody string
		writeFile bool
		path      string // override (e.g. "" for empty-path corner)
		wantVal   float64
		wantOK    bool
	}{
		{"positive_plain_float", "positive", "73.5", true, "", 73.5, true},
		{"positive_with_percent_suffix", "positive", "82.4%", true, "", 82.4, true},
		{"positive_whitespace", "positive", "  90.0  \n", true, "", 90.0, true},
		{"positive_zero", "positive", "0", true, "", 0.0, true},
		{"positive_three_decimals", "positive", "73.500", true, "", 73.5, true},
		{"negative_missing_file", "negative", "", false, "", 0, false},
		{"negative_empty_string_path", "negative", "ignored", false, "", 0, false},
		{"negative_unparseable", "negative", "not a number", true, "", 0, false},
		{"boundary_high_value", "boundary", "100.0", true, "", 100.0, true},
		{"boundary_negative_value", "boundary", "-5.0", true, "", -5.0, true},
		{"corner_only_percent_sign", "corner", "%", true, "", 0, false},
		{"adversarial_giant_value", "adversarial", "9999999.99", true, "", 9999999.99, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			path := tc.path
			if tc.writeFile {
				dir := t.TempDir()
				path = filepath.Join(dir, "baseline.txt")
				if err := os.WriteFile(path, []byte(tc.writeBody), 0o600); err != nil {
					t.Fatalf("write: %v", err)
				}
			}
			gotVal, gotOK := readCoverageBaseline(path)
			if gotOK != tc.wantOK {
				t.Errorf("ok = %v, want %v", gotOK, tc.wantOK)
			}
			if gotOK && gotVal != tc.wantVal {
				t.Errorf("val = %v, want %v", gotVal, tc.wantVal)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// evaluateCoverageRatchet
// ───────────────────────────────────────────────────────────────────────

func TestEvaluateCoverageRatchet_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		category     string
		baselineBody string
		writeFile    bool
		baselinePath string
		current      float64
		maxDropAbs   float64
		wantBreached bool
	}{
		{"positive_current_above_baseline_passes", "positive", "70.0", true, "", 75.0, 0.5, false},
		{"positive_current_equals_baseline_passes", "positive", "73.5", true, "", 73.5, 0.5, false},
		{"positive_within_grace_passes", "positive", "73.5", true, "", 73.2, 0.5, false},
		{"positive_exactly_at_grace_passes", "positive", "74.0", true, "", 73.5, 0.5, false},
		{"negative_drop_just_over_grace_breaches", "negative", "74.0", true, "", 73.0, 0.5, true},
		{"negative_large_drop_breaches", "negative", "90.0", true, "", 50.0, 0.5, true},
		{"boundary_zero_baseline_passes", "boundary", "0", true, "", 5.0, 0.5, false},
		{"boundary_zero_max_drop_strict", "boundary", "70.0", true, "", 69.99, 0.0, true},
		{"corner_no_baseline_file_passes", "corner", "", false, "", 50.0, 0.5, false},
		{"corner_unparseable_baseline_passes", "corner", "garbage", true, "", 50.0, 0.5, false},
		{"adversarial_huge_drop_breaches", "adversarial", "99.9", true, "", 0.1, 0.5, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			path := tc.baselinePath
			if tc.writeFile {
				dir := t.TempDir()
				path = filepath.Join(dir, "baseline.txt")
				if err := os.WriteFile(path, []byte(tc.baselineBody), 0o600); err != nil {
					t.Fatalf("write: %v", err)
				}
			}
			msg, breached := evaluateCoverageRatchet(path, tc.current, tc.maxDropAbs)
			if breached != tc.wantBreached {
				t.Errorf("breached = %v (msg=%q), want %v", breached, msg, tc.wantBreached)
			}
			if breached && msg == "" {
				t.Error("breached=true but msg is empty")
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// End-to-end: runMain exits with code 3 when a ratchet breach occurs.
// ───────────────────────────────────────────────────────────────────────

func TestRunMain_coverageRatchetBreach(t *testing.T) {
	// Seed a high baseline that no minimal raw-dir can plausibly hit.
	// runMain's ingestCoverage step looks for $RAW/coverage.out and
	// $RAW/coverage-func.out; if both are missing, in.Coverage.Available
	// is false and the ratchet is skipped. Provide a tiny synthetic
	// coverage profile so Available=true and Total is computed.
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "coverage-per-package.tsv"),
		[]byte("pkg/x\t50.0\n"), 0o600); err != nil {
		t.Fatalf("write tsv: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "coverage-func.out"),
		[]byte("total:                                                  (statements)            50.0%\n"), 0o600); err != nil {
		t.Fatalf("write func: %v", err)
	}
	baseline := filepath.Join(dir, "baseline.txt")
	if err := os.WriteFile(baseline, []byte("99.0"), 0o600); err != nil {
		t.Fatalf("write baseline: %v", err)
	}

	var stdout, stderr trapWriter
	args := []string{
		"-raw-dir", dir,
		"-coverage-baseline", baseline,
		"-coverage-max-drop", "0.5",
	}
	rc := runMain(args, &stdout, &stderr)
	if rc != 3 {
		t.Errorf("rc = %d, want 3 (ratchet breach)", rc)
	}
}

// TestRunMain_coverageRatchetPasses confirms the happy path: a
// baseline close to the synthetic 50.0% coverage does NOT trigger
// the ratchet.
func TestRunMain_coverageRatchetPasses(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "coverage-per-package.tsv"),
		[]byte("pkg/x\t50.0\n"), 0o600); err != nil {
		t.Fatalf("write tsv: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "coverage-func.out"),
		[]byte("total:                                                  (statements)            50.0%\n"), 0o600); err != nil {
		t.Fatalf("write func: %v", err)
	}
	baseline := filepath.Join(dir, "baseline.txt")
	if err := os.WriteFile(baseline, []byte("49.8"), 0o600); err != nil {
		t.Fatalf("write baseline: %v", err)
	}

	var stdout, stderr trapWriter
	args := []string{
		"-raw-dir", dir,
		"-coverage-baseline", baseline,
		"-coverage-max-drop", "0.5",
	}
	rc := runMain(args, &stdout, &stderr)
	if rc != 0 {
		t.Errorf("rc = %d, want 0 (ratchet passes)", rc)
	}
}

// ───────────────────────────────────────────────────────────────────────
// Benchmark
// ───────────────────────────────────────────────────────────────────────

func BenchmarkEvaluateCoverageRatchet_baselineMissing(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = evaluateCoverageRatchet("/no/such/path", 75.0, 0.5)
	}
}

func BenchmarkReadCoverageBaseline_hit(b *testing.B) {
	dir := b.TempDir()
	path := filepath.Join(dir, "baseline.txt")
	if err := os.WriteFile(path, []byte("73.5"), 0o600); err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = readCoverageBaseline(path)
	}
}
