package summary

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestSummaryCounts covers Add plus the four aggregate accessors across empty,
// single, multi-source, and zero-record inputs.
func TestSummaryCounts(t *testing.T) {
	tests := []struct {
		description      string
		sources          []SourceResult
		expectedLen      int
		expectedOK       int
		expectedFail     int
		expectedValid    int
		expectedRejected int
	}{
		// positive
		{
			description:   "positive: a single successful source is counted once with its record boundaries",
			sources:       []SourceResult{{Name: "a", OK: true, Parsed: 10, Valid: 8, Rejected: 2}},
			expectedLen:   1,
			expectedOK:    1,
			expectedValid: 8, expectedRejected: 2,
		},
		{
			description: "positive: multiple sources sum valid/rejected and split ok/fail",
			sources: []SourceResult{
				{Name: "a", OK: true, Valid: 100, Rejected: 1},
				{Name: "b", OK: false, Note: "timeout"},
				{Name: "c", OK: true, Valid: 50, Rejected: 5},
			},
			expectedLen: 3, expectedOK: 2, expectedFail: 1,
			expectedValid: 150, expectedRejected: 6,
		},
		// negative
		{
			description:  "negative: a single failed source counts as fail with zero records",
			sources:      []SourceResult{{Name: "a", OK: false, HTTPStatus: 503, Note: "boom"}},
			expectedLen:  1,
			expectedFail: 1,
		},
		// boundary
		{
			description: "boundary: no sources yields all-zero totals",
			sources:     nil,
		},
		{
			description: "boundary: an ok source with zero records contributes to ok but not to record totals",
			sources:     []SourceResult{{Name: "empty", OK: true}},
			expectedLen: 1, expectedOK: 1,
		},
		// corner
		{
			description: "corner: a failed source that still reports records is summed into totals",
			sources:     []SourceResult{{Name: "partial", OK: false, Parsed: 3, Valid: 2, Rejected: 1}},
			expectedLen: 1, expectedFail: 1,
			expectedValid: 2, expectedRejected: 1,
		},
		{
			description: "corner: duplicate source names are not merged",
			sources: []SourceResult{
				{Name: "dup", OK: true, Valid: 1},
				{Name: "dup", OK: true, Valid: 1},
			},
			expectedLen: 2, expectedOK: 2, expectedValid: 2,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var s Summary
			for _, r := range tc.sources {
				s.Add(r)
			}
			if got := len(s.Sources); got != tc.expectedLen {
				t.Errorf("%s: len(Sources) = %d, want %d", tc.description, got, tc.expectedLen)
			}
			if got := s.OKCount(); got != tc.expectedOK {
				t.Errorf("%s: OKCount = %d, want %d", tc.description, got, tc.expectedOK)
			}
			if got := s.FailCount(); got != tc.expectedFail {
				t.Errorf("%s: FailCount = %d, want %d", tc.description, got, tc.expectedFail)
			}
			if got := s.TotalValid(); got != tc.expectedValid {
				t.Errorf("%s: TotalValid = %d, want %d", tc.description, got, tc.expectedValid)
			}
			if got := s.TotalRejected(); got != tc.expectedRejected {
				t.Errorf("%s: TotalRejected = %d, want %d", tc.description, got, tc.expectedRejected)
			}
		})
	}
}

// TestHumanBytes covers the byte formatter at each unit boundary.
func TestHumanBytes(t *testing.T) {
	tests := []struct {
		description string
		in          int64
		expected    string
	}{
		// positive
		{"positive: small byte count is printed as-is", 512, "512 B"},
		{"positive: 1.5 KiB rounds to one decimal", 1536, "1.5 KB"},
		{"positive: whole MiB", 1 << 20, "1.0 MB"},
		{"positive: whole GiB", 1 << 30, "1.0 GB"},
		{"positive: whole TiB", 1 << 40, "1.0 TB"},
		// boundary
		{"boundary: zero bytes", 0, "0 B"},
		{"boundary: one below the KiB threshold stays in bytes", 1023, "1023 B"},
		{"boundary: exactly one KiB switches unit", 1024, "1.0 KB"},
		{"boundary: one below the MiB threshold stays in KB", (1 << 20) - 1, "1024.0 KB"},
		// corner
		{"corner: non-integral MiB keeps one decimal", 5*(1<<20) + 3*(1<<18), "5.8 MB"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := humanBytes(tc.in); got != tc.expected {
				t.Errorf("%s: humanBytes(%d) = %q, want %q", tc.description, tc.in, got, tc.expected)
			}
		})
	}
}

// failingWriter always errors, to exercise the tabwriter flush error path.
type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("sink closed") }

// normalizeLines splits output into lines and collapses runs of whitespace so
// tabwriter column padding does not make expectations alignment-dependent.
func normalizeLines(s string) []string {
	var out []string
	for _, ln := range strings.Split(strings.TrimRight(s, "\n"), "\n") {
		out = append(out, strings.Join(strings.Fields(ln), " "))
	}
	return out
}

// TestSummaryPrint covers the rendered report: header, name-sorted rows, ok/FAIL
// status, "-" for a missing HTTP status, humanised bytes, millisecond-rounded
// durations, the TOTALS line, and the optional uploaded line.
func TestSummaryPrint(t *testing.T) {
	tests := []struct {
		description   string
		sources       []SourceResult
		uploadURL     string
		uploadBytes   int64
		expectedLines []string
		expectErr     bool
	}{
		// positive
		{
			description: "positive: two ok sources are rendered sorted by name with totals and an uploaded line",
			sources: []SourceResult{
				{Name: "zeta", OK: true, HTTPStatus: 200, FetchedBytes: 2048, Parsed: 3, Valid: 3, Duration: 1500 * time.Millisecond},
				{Name: "alpha", OK: true, HTTPStatus: 200, FetchedBytes: 100, Parsed: 2, Valid: 1, Rejected: 1, Duration: 20 * time.Millisecond, Note: "1 bad cidr"},
			},
			uploadURL:   "s3://bucket/ipfeeds/x.parquet",
			uploadBytes: 3 * 1024 * 1024,
			expectedLines: []string{
				"source status http fetched parsed +valid -rejected dur note",
				"alpha ok 200 100 B 2 1 1 20ms 1 bad cidr",
				"zeta ok 200 2.0 KB 3 3 0 1.5s",
				"",
				"TOTALS 2 ok / 0 fail records: 4 valid (+) / 1 rejected (-)",
				"uploaded: s3://bucket/ipfeeds/x.parquet (3.0 MB)",
			},
		},
		// negative
		{
			description: "negative: a failed source shows FAIL, '-' for no http status, and its note",
			sources: []SourceResult{
				{Name: "broken", OK: false, Duration: 5 * time.Second, Note: "dial tcp: connection refused"},
			},
			expectedLines: []string{
				"source status http fetched parsed +valid -rejected dur note",
				"broken FAIL - 0 B 0 0 0 5s dial tcp: connection refused",
				"",
				"TOTALS 0 ok / 1 fail records: 0 valid (+) / 0 rejected (-)",
			},
		},
		{
			description: "negative: a writer that fails surfaces a flush error",
			sources:     []SourceResult{{Name: "a", OK: true}},
			expectErr:   true,
		},
		// boundary
		{
			description: "boundary: no sources prints just the header and a zero TOTALS line",
			sources:     nil,
			expectedLines: []string{
				"source status http fetched parsed +valid -rejected dur note",
				"",
				"TOTALS 0 ok / 0 fail records: 0 valid (+) / 0 rejected (-)",
			},
		},
		{
			description: "boundary: an ok source with zero records renders zeros and no uploaded line when URL is empty",
			sources:     []SourceResult{{Name: "empty", OK: true, HTTPStatus: 204}},
			uploadBytes: 999, // ignored without a URL
			expectedLines: []string{
				"source status http fetched parsed +valid -rejected dur note",
				"empty ok 204 0 B 0 0 0 0s",
				"",
				"TOTALS 1 ok / 0 fail records: 0 valid (+) / 0 rejected (-)",
			},
		},
		// corner
		{
			description: "corner: a failed source with a non-2xx status still prints the numeric status and sub-ms durations round to 0s",
			sources: []SourceResult{
				{Name: "b", OK: false, HTTPStatus: 503, FetchedBytes: 12, Duration: 400 * time.Microsecond, Note: "status 503"},
				{Name: "a", OK: true, HTTPStatus: 200, FetchedBytes: 1, Valid: 1, Duration: 1499 * time.Microsecond},
			},
			expectedLines: []string{
				"source status http fetched parsed +valid -rejected dur note",
				"a ok 200 1 B 0 1 0 1ms",
				"b FAIL 503 12 B 0 0 0 0s status 503",
				"",
				"TOTALS 1 ok / 1 fail records: 1 valid (+) / 0 rejected (-)",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var s Summary
			for _, r := range tc.sources {
				s.Add(r)
			}
			if tc.expectErr {
				err := s.Print(failingWriter{}, tc.uploadURL, tc.uploadBytes)
				if err == nil {
					t.Fatalf("%s: expected error, got nil", tc.description)
				}
				if !strings.Contains(err.Error(), "summary: flush table") {
					t.Errorf("%s: error %q lacks flush-table context", tc.description, err)
				}
				return
			}
			var buf bytes.Buffer
			if err := s.Print(&buf, tc.uploadURL, tc.uploadBytes); err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.description, err)
			}
			got := normalizeLines(buf.String())
			if len(got) != len(tc.expectedLines) {
				t.Fatalf("%s: got %d lines, want %d\n%s", tc.description, len(got), len(tc.expectedLines), buf.String())
			}
			for i := range tc.expectedLines {
				if got[i] != tc.expectedLines[i] {
					t.Errorf("%s: line %d = %q, want %q", tc.description, i, got[i], tc.expectedLines[i])
				}
			}
		})
	}
}
