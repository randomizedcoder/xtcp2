//go:build dest_s3parquet

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/goldenrec"
)

// coverageFixture is a fixture directory whose parent holds coverage (no
// file when coverage is ""), with manifest.json replaced when manifest is
// set.
type coverageFixture struct {
	coverage string
	manifest string
}

// go test -tags dest_s3parquet ./cmd/xtcp2-golden/ -run TestRunCheckCoverage
func TestRunCheckCoverage(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		input       coverageFixture
		expected    shapeExpect
	}{
		{"coverage_ok", "positive", "a fixture meeting its coverage passes",
			coverageFixture{`{"populated":["tcp_info_rtt"],"values":{"inet_diag_msg_state":["1"]},"families":["2"]}`, ""},
			shapeExpect{exitSame, ""}},
		{"coverage_fail", "negative", "a stricter coverage fails",
			coverageFixture{`{"populated":["bbr_info_bw_hi"]}`, ""},
			shapeExpect{exitDiff, "coverage: bbr_info_bw_hi never populated"}},
		{"no_file", "negative", "no coverage.json checks as before",
			coverageFixture{"", ""},
			shapeExpect{exitSame, ""}},
		{"bad_json", "boundary", "a malformed coverage file",
			coverageFixture{`{`, ""},
			shapeExpect{exitError, ""}},
		{"manifest_missing_key", "corner", "a manifest reference the manifest lacks",
			coverageFixture{`{"values":{"inet_diag_cong":["@manifest.cong_available"]}}`, ""},
			shapeExpect{exitDiff, "coverage: manifest has no cong_available"}},
		{"manifest_list_met", "corner", "the manifest's list is met",
			coverageFixture{`{"values":{"inet_diag_msg_state":["@manifest.states"]}}`, `{"rows": 3, "states": ["1"]}`},
			shapeExpect{exitSame, ""}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := fixture(t, testRows(3), func(dir string) {
				if tc.input.manifest != "" {
					if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(tc.input.manifest), 0o600); err != nil {
						t.Fatal(err)
					}
				}
			})
			cov := filepath.Join(filepath.Dir(dir), "coverage.json")
			_ = os.Remove(cov)
			if tc.input.coverage != "" {
				if err := os.WriteFile(cov, []byte(tc.input.coverage), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			var stdout, stderr bytes.Buffer
			rc := run([]string{"check", dir}, &stdout, &stderr)
			out := stdout.String()
			ok := rc == tc.expected.rc
			if tc.expected.stdout == "" {
				ok = ok && (rc == exitError || out == "")
			} else {
				ok = ok && strings.Contains(out, tc.expected.stdout)
			}
			if !ok {
				t.Errorf("%s [%s] %s: coverage %q: rc %d stdout %q stderr %q, expected rc %d stdout containing %q",
					tc.name, tc.category, tc.description, tc.input.coverage, rc, out, stderr.String(), tc.expected.rc, tc.expected.stdout)
			}
		})
	}
}

// TestRunCheckCommittedCoverage checks the committed fixtures against the
// committed coverage.json, as checks.golden-records does.
func TestRunCheckCommittedCoverage(t *testing.T) {
	entries, err := os.ReadDir("../../pkg/xtcp/testdata/golden")
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, filepath.Join("../../pkg/xtcp/testdata/golden", e.Name()))
		}
	}
	if len(dirs) == 0 {
		t.Fatal("no golden fixtures")
	}
	if _, err := os.Stat("../../pkg/xtcp/testdata/golden/coverage.json"); err != nil {
		t.Fatalf("coverage.json: %v", err)
	}
	for _, dir := range dirs {
		var stdout, stderr bytes.Buffer
		if rc := run([]string{"check", dir}, &stdout, &stderr); rc != exitSame {
			t.Errorf("check %s: rc %d\n%s%s", dir, rc, stdout.String(), stderr.String())
		}
	}
}

// go test -tags dest_s3parquet ./cmd/xtcp2-golden/ -run TestManifestRefs
func TestManifestRefs(t *testing.T) {
	cov := goldenrec.Coverage{Values: map[string][]string{"inet_diag_cong": {"@manifest.cong_available"}}}
	prof := goldenrec.Profile{Rows: 1, Columns: map[string]goldenrec.ColumnProfile{
		"inet_diag_cong": {Class: "strict", Nonzero: 1, Distinct: "2", Values: []string{"bbr", "cubic"}},
	}}
	tests := []struct {
		name        string
		category    string
		description string
		input       string
		expected    []string
	}{
		{"read_list", "positive", "the capture's list is read and met",
			`{"rows": 1, "cong_available": ["cubic", "bbr"]}`, nil},
		{"absent", "negative", "an old manifest without the list",
			`{"rows": 1}`, []string{"coverage: manifest has no cong_available"}},
		{"empty_list", "boundary", "an empty list is an error",
			`{"rows": 1, "cong_available": []}`, []string{"coverage: manifest cong_available is empty"}},
		{"unsorted", "corner", "order doesn't matter",
			`{"rows": 1, "cong_available": ["bbr", "cubic"]}`, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			refs, err := manifestRefs([]byte(tc.input))
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			got := goldenrec.CheckCoverage(prof, cov, refs)
			if !slices.Equal(got, tc.expected) {
				t.Errorf("%s [%s] %s: manifest %s: got %q, expected %q", tc.name, tc.category, tc.description, tc.input, got, tc.expected)
			}
		})
	}
}
