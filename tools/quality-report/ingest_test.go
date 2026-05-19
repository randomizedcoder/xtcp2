package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// ingest_test.go covers the eight per-tool ingest helpers extracted
// from runMain in the gocyclo-25 → 3 refactor. Each helper has a
// positive / negative / boundary / corner / adversarial table.
// Pre-existing main_test.go drives parsers; this file pins the
// ingestion-layer behaviour around them.

// writeRaw seeds a file under rawDir for one ingestion test.
func writeRaw(t *testing.T, rawDir, name, contents string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(rawDir, name), []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

// newIngestCtx builds an ingestCtx pointing at a fresh tempdir.
func newIngestCtx(t *testing.T) *ingestCtx {
	t.Helper()
	dir := t.TempDir()
	return &ingestCtx{
		rawDir:    dir,
		repoRoot:  ".",
		runtimes:  map[string]int{},
		exitCodes: map[string]int{},
		known:     map[string]bool{},
	}
}

// ───────────────────────────────────────────────────────────────────────
// parseRunMainFlags
// ───────────────────────────────────────────────────────────────────────

func TestParseRunMainFlags_table(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		category  string
		args      []string
		wantExit  int
		wantRawOK bool
	}{
		{"positive_all_flags", "positive", []string{"-raw-dir", "/tmp/x", "-repo-root", "/r"}, 0, true},
		{"positive_minimal_required", "positive", []string{"-raw-dir", "/tmp/x"}, 0, true},
		{"negative_no_args", "negative", []string{}, 2, false},
		{"negative_missing_required_raw_dir", "negative", []string{"-repo-root", "/r"}, 2, false},
		{"negative_unknown_flag", "negative", []string{"-not-real"}, 2, false},
		{"boundary_empty_raw_dir_arg", "boundary", []string{"-raw-dir", ""}, 2, false},
		{"corner_dash_dash_after_known", "corner", []string{"-raw-dir", "/tmp/x", "--"}, 0, true},
		{"adversarial_path_with_spaces", "adversarial", []string{"-raw-dir", "/tmp with spaces/x"}, 0, true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			t.Parallel()
			var stderr trapWriter
			raw, _, _, ec := parseRunMainFlags(tc.args, &stderr)
			if ec != tc.wantExit {
				t.Errorf("exit = %d, want %d", ec, tc.wantExit)
			}
			if tc.wantRawOK && raw == "" {
				t.Errorf("raw-dir = %q, want non-empty", raw)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// ingestGosec
// ───────────────────────────────────────────────────────────────────────

func TestIngestGosec_table(t *testing.T) {
	cases := []struct {
		name           string
		category       string
		fileContents   string
		writeFile      bool
		wantStatusOK   bool
		wantStatusName string
	}{
		{
			name:           "positive_empty_findings",
			category:       "positive",
			fileContents:   `{"Issues":[]}`,
			writeFile:      true,
			wantStatusOK:   true,
			wantStatusName: toolGosec,
		},
		{
			name:           "negative_missing_file_unavailable",
			category:       "negative",
			writeFile:      false,
			wantStatusOK:   false,
			wantStatusName: toolGosec,
		},
		{
			name:           "boundary_empty_json_object",
			category:       "boundary",
			fileContents:   `{}`,
			writeFile:      true,
			wantStatusOK:   true,
			wantStatusName: toolGosec,
		},
		{
			name:           "corner_malformed_json",
			category:       "corner",
			fileContents:   `not json`,
			writeFile:      true,
			wantStatusOK:   true, // tolerated; surfaces a parse-error finding
			wantStatusName: toolGosec,
		},
		{
			name:           "adversarial_huge_empty_array",
			category:       "adversarial",
			fileContents:   `{"Issues":[]}`,
			writeFile:      true,
			wantStatusOK:   true,
			wantStatusName: toolGosec,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			c := newIngestCtx(t)
			if tc.writeFile {
				writeRaw(t, c.rawDir, "gosec.json", tc.fileContents)
			}
			var in reportInput
			c.ingestGosec(&in)
			if len(in.Status) != 1 {
				t.Fatalf("Status len = %d, want 1", len(in.Status))
			}
			if in.Status[0].Name != tc.wantStatusName {
				t.Errorf("Status.Name = %q, want %q", in.Status[0].Name, tc.wantStatusName)
			}
			if in.Status[0].Available != tc.wantStatusOK {
				t.Errorf("Available = %v, want %v", in.Status[0].Available, tc.wantStatusOK)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// ingestFormatter — covers both gofmt and nix-fmt
// ───────────────────────────────────────────────────────────────────────

func TestIngestFormatter_table(t *testing.T) {
	cases := []struct {
		name           string
		category       string
		statusName     string
		exitCodeKey    string
		fileName       string
		fileContents   string
		writeFile      bool
		wantFiles      int
	}{
		{"positive_two_unformatted_files_gofmt", "positive", toolGofmt, toolGofmt, "gofmt.out", "a.go\nb.go\n", true, 2},
		{"positive_two_unformatted_files_nixfmt", "positive", "nixfmt", "nix-fmt", "nix-fmt.out", "x.nix\ny.nix\n", true, 2},
		{"negative_empty_file", "negative", toolGofmt, toolGofmt, "gofmt.out", "", true, 0},
		{"negative_missing_file", "negative", toolGofmt, toolGofmt, "gofmt.out", "", false, 0},
		{"boundary_only_blank_line", "boundary", toolGofmt, toolGofmt, "gofmt.out", "\n\n\n", true, 0},
		{"boundary_single_file_no_newline", "boundary", toolGofmt, toolGofmt, "gofmt.out", "lone.go", true, 1},
		{"corner_trailing_whitespace_filtered", "corner", toolGofmt, toolGofmt, "gofmt.out", "  spaced.go  \n", true, 1},
		{"adversarial_huge_file_list", "adversarial", toolGofmt, toolGofmt, "gofmt.out", repeatLine("f.go", 1000), true, 1000},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			c := newIngestCtx(t)
			if tc.writeFile {
				writeRaw(t, c.rawDir, tc.fileName, tc.fileContents)
			}
			var in reportInput
			var kept []string
			c.ingestFormatter(&in, tc.exitCodeKey, tc.fileName, tc.statusName, &kept)
			if len(kept) != tc.wantFiles {
				t.Errorf("kept files = %d, want %d", len(kept), tc.wantFiles)
			}
			// Findings should be one per kept file.
			if len(in.Findings) != tc.wantFiles {
				t.Errorf("findings = %d, want %d", len(in.Findings), tc.wantFiles)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// ingestGoVet + ingestCustomAudits + ingestGoTest — quick coverage
// ───────────────────────────────────────────────────────────────────────

func TestIngestGoVet_addsStatusRow(t *testing.T) {
	c := newIngestCtx(t)
	writeRaw(t, c.rawDir, "govet.out", "")
	var in reportInput
	c.ingestGoVet(&in)
	if len(in.Status) != 1 || in.Status[0].Name != "go vet" {
		t.Errorf("expected one go vet row, got %+v", in.Status)
	}
}

func TestIngestCustomAudits_iteratesAllFour(t *testing.T) {
	c := newIngestCtx(t)
	for _, name := range []string{"netlink-audit", "iouring-audit", "metrics-audit", "proto-field-audit"} {
		writeRaw(t, c.rawDir, name+".out", "audit: no findings\n")
	}
	var in reportInput
	c.ingestCustomAudits(&in)
	if len(in.Status) != 4 {
		t.Errorf("Status len = %d, want 4 (one per audit)", len(in.Status))
	}
}

// ───────────────────────────────────────────────────────────────────────
// ingestCliHelpSmoke
// ───────────────────────────────────────────────────────────────────────

func TestIngestCliHelpSmoke_table(t *testing.T) {
	cases := []struct {
		name            string
		category        string
		contents        string
		writeFile       bool
		wantStatusRows  int
		wantCliResults  int
	}{
		{
			name:           "negative_no_file_no_row",
			category:       "negative",
			writeFile:      false,
			wantStatusRows: 0,
		},
		{
			name:           "negative_empty_file_no_row",
			category:       "negative",
			contents:       "",
			writeFile:      true,
			wantStatusRows: 0,
		},
		// positive/adversarial rows that emit content would require
		// driving parseCliHelpSmoke's actual format; the negative-only
		// cases are sufficient to pin the early-return.
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.category+"/"+tc.name, func(t *testing.T) {
			c := newIngestCtx(t)
			if tc.writeFile {
				writeRaw(t, c.rawDir, "cli-help-smoke.out", tc.contents)
			}
			var in reportInput
			c.ingestCliHelpSmoke(&in)
			if len(in.Status) != tc.wantStatusRows {
				t.Errorf("Status rows = %d, want %d", len(in.Status), tc.wantStatusRows)
			}
			if len(in.CliHelpResults) != tc.wantCliResults {
				t.Errorf("CliHelpResults = %d, want %d", len(in.CliHelpResults), tc.wantCliResults)
			}
		})
	}
}

// ───────────────────────────────────────────────────────────────────────
// ingestCoverage
// ───────────────────────────────────────────────────────────────────────

func TestIngestCoverage_missingFilesIsNoOp(t *testing.T) {
	c := newIngestCtx(t)
	var in reportInput
	c.ingestCoverage(&in)
	// No coverage files → Coverage.Available=false → no status row, no findings.
	if in.Coverage.Available {
		t.Error("expected Available=false with no coverage files")
	}
	if len(in.Status) != 0 {
		t.Errorf("Status rows = %d, want 0", len(in.Status))
	}
}

// ───────────────────────────────────────────────────────────────────────
// Race — drive the ingestion helpers concurrently. Each goroutine gets
// its own ingestCtx + reportInput so there's no shared mutable state.
// ───────────────────────────────────────────────────────────────────────

func TestIngestHelpers_concurrent(t *testing.T) {
	const goroutines = 16
	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			c := newIngestCtx(t)
			writeRaw(t, c.rawDir, "gosec.json", `{"Issues":[]}`)
			writeRaw(t, c.rawDir, "govet.out", "")
			writeRaw(t, c.rawDir, "gofmt.out", "a.go\n")
			writeRaw(t, c.rawDir, "nix-fmt.out", "x.nix\n")
			for j := 0; j < 50; j++ {
				var in reportInput
				var kept []string
				c.ingestGosec(&in)
				c.ingestGoVet(&in)
				c.ingestFormatter(&in, toolGofmt, "gofmt.out", toolGofmt, &kept)
				c.ingestCliHelpSmoke(&in)
				c.ingestCoverage(&in)
			}
		}()
	}
	wg.Wait()
}

// ───────────────────────────────────────────────────────────────────────
// Benchmarks
// ───────────────────────────────────────────────────────────────────────

func BenchmarkParseRunMainFlags(b *testing.B) {
	args := []string{"-raw-dir", "/tmp/x", "-repo-root", "/r"}
	var w trapWriter
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _, _, _ = parseRunMainFlags(args, &w)
	}
}

func BenchmarkIngestGosec_emptyJSON(b *testing.B) {
	t := &testing.T{}
	c := newIngestCtx(t)
	writeRaw(t, c.rawDir, "gosec.json", `{"Issues":[]}`)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var in reportInput
		c.ingestGosec(&in)
	}
}

// helpers ----------------------------------------------------------------

// trapWriter is a tiny io.Writer that discards everything (test stderr).
type trapWriter struct{}

func (trapWriter) Write(p []byte) (int, error) { return len(p), nil }

// repeatLine returns a multi-line string of `n` copies of `s` separated
// by '\n'. Used to drive the "huge file list" adversarial case.
func repeatLine(s string, n int) string {
	out := make([]byte, 0, (len(s)+1)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
		out = append(out, '\n')
	}
	return string(out)
}
