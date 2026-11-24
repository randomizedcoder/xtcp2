//go:build dest_s3parquet

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/goldenrec"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp"
)

// shapeInput is one xtcp2-golden invocation; setup, when set, writes files
// into dir first, and "DIR" in args is replaced by dir.
type shapeInput struct {
	args  []string
	setup func(t *testing.T, dir string)
}

// shapeExpect is the exit code and a substring of stdout ("" = empty
// stdout).
type shapeExpect struct {
	rc     int
	stdout string
}

// writeProfile profiles rows into dir/name, with edit applied first.
func writeProfile(t *testing.T, dir, name string, rows []xtcp.ParquetRow, edit func(*goldenrec.Profile)) {
	t.Helper()
	p, err := goldenrec.ProfileOf(rows, goldenrec.Columns)
	if err != nil {
		t.Fatal(err)
	}
	if edit != nil {
		edit(&p)
	}
	b, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// go test -tags dest_s3parquet ./cmd/xtcp2-golden/ -run TestRunProfileShapeDiff
func TestRunProfileShapeDiff(t *testing.T) {
	rows := testRows(3)
	rows[2].InetDiagMsgFamily = 10
	parquetFile := func(t *testing.T, dir string) { writeRows(t, filepath.Join(dir, "x.parquet"), rows) }
	tests := []struct {
		name        string
		category    string
		description string
		input       shapeInput
		expected    shapeExpect
	}{
		{"profile_ok", "positive", "profile a capture",
			shapeInput{[]string{"profile", "DIR/x.parquet"}, parquetFile},
			shapeExpect{exitSame, `"families":{"10":1,"2":2}`}},
		{"shape_same", "positive", "a capture against itself",
			shapeInput{[]string{"shape-diff", "DIR/x.parquet", "DIR/x.parquet"}, parquetFile},
			shapeExpect{exitSame, ""}},
		{"bad_args", "negative", "the wrong argument count",
			shapeInput{[]string{"shape-diff", "DIR/x.parquet"}, parquetFile},
			shapeExpect{exitError, ""}},
		{"missing_file", "negative", "a profile that is not there",
			shapeInput{[]string{"shape-diff", "DIR/none.json", "DIR/x.parquet"}, parquetFile},
			shapeExpect{exitError, ""}},
		{"profile_json_input", "boundary", "a profile.json is accepted as input",
			shapeInput{[]string{"shape-diff", "DIR/p.json", "DIR/x.parquet"}, func(t *testing.T, dir string) {
				parquetFile(t, dir)
				writeProfile(t, dir, "p.json", rows, nil)
			}},
			shapeExpect{exitSame, ""}},
		{"shape_differs", "corner", "a profile with family 10 removed",
			shapeInput{[]string{"shape-diff", "DIR/p.json", "DIR/x.parquet"}, func(t *testing.T, dir string) {
				parquetFile(t, dir)
				writeProfile(t, dir, "p.json", rows, func(p *goldenrec.Profile) { delete(p.Families, "10") })
			}},
			shapeExpect{exitDiff, "family-missing A: 10"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.input.setup != nil {
				tc.input.setup(t, dir)
			}
			args := make([]string, len(tc.input.args))
			for i, a := range tc.input.args {
				args[i] = strings.ReplaceAll(a, "DIR", dir)
			}
			var stdout, stderr bytes.Buffer
			rc := run(args, &stdout, &stderr)
			out := stdout.String()
			if rc != tc.expected.rc || (tc.expected.stdout == "") != (out == "") || !strings.Contains(out, tc.expected.stdout) {
				t.Errorf("%s (%s): %s\ninput %q\nexpected rc %d, stdout containing %q\nactual   rc %d, stdout %q, stderr %q",
					tc.name, tc.category, tc.description, tc.input.args, tc.expected.rc, tc.expected.stdout, rc, out, stderr.String())
			}
		})
	}
}
