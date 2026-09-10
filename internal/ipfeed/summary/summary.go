// Package summary accumulates per-source outcomes and prints the end-of-run
// report: files processed and records processed, with explicit positive
// (valid) and negative (rejected) boundaries.
package summary

import (
	"fmt"
	"io"
	"sort"
	"text/tabwriter"
	"time"
)

// SourceResult is the outcome for one source.
type SourceResult struct {
	Name         string
	OK           bool
	HTTPStatus   int
	FetchedBytes int64
	Parsed       int // rows the parser produced
	Valid        int // positive (+) boundary
	Rejected     int // negative (-) boundary
	Duration     time.Duration
	Note         string // failure reason or extra context
}

// Summary collects SourceResults and computes totals.
type Summary struct {
	Sources []SourceResult
}

// Add records one source's result.
func (s *Summary) Add(r SourceResult) { s.Sources = append(s.Sources, r) }

// OKCount returns the number of successful sources.
func (s *Summary) OKCount() int {
	n := 0
	for _, r := range s.Sources {
		if r.OK {
			n++
		}
	}
	return n
}

// FailCount returns the number of failed sources.
func (s *Summary) FailCount() int { return len(s.Sources) - s.OKCount() }

// TotalValid returns the summed positive boundary across sources.
func (s *Summary) TotalValid() int {
	n := 0
	for _, r := range s.Sources {
		n += r.Valid
	}
	return n
}

// TotalRejected returns the summed negative boundary across sources.
func (s *Summary) TotalRejected() int {
	n := 0
	for _, r := range s.Sources {
		n += r.Rejected
	}
	return n
}

// Print renders the report to w. uploadURL/uploadBytes describe the uploaded
// object (empty uploadURL means no upload happened). It returns any error from
// flushing the tabular section to w.
func (s *Summary) Print(w io.Writer, uploadURL string, uploadBytes int64) error {
	rows := append([]SourceResult(nil), s.Sources...)
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })

	tw := tabwriter.NewWriter(w, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "source\tstatus\thttp\tfetched\tparsed\t+valid\t-rejected\tdur\tnote")
	for _, r := range rows {
		status := "ok"
		if !r.OK {
			status = "FAIL"
		}
		http := "-"
		if r.HTTPStatus > 0 {
			http = fmt.Sprintf("%d", r.HTTPStatus)
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%d\t%d\t%d\t%s\t%s\n",
			r.Name, status, http, humanBytes(r.FetchedBytes),
			r.Parsed, r.Valid, r.Rejected, r.Duration.Round(time.Millisecond), r.Note)
	}
	if err := tw.Flush(); err != nil {
		return fmt.Errorf("summary: flush table: %w", err)
	}

	fmt.Fprintf(w, "\nTOTALS  %d ok / %d fail    records: %d valid (+) / %d rejected (-)\n",
		s.OKCount(), s.FailCount(), s.TotalValid(), s.TotalRejected())
	if uploadURL != "" {
		fmt.Fprintf(w, "uploaded: %s (%s)\n", uploadURL, humanBytes(uploadBytes))
	}
	return nil
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}
