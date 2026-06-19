package main

import (
	"fmt"
	"io"
	"log"
	"sync"

	"github.com/randomizedcoder/xtcp2/pkg/recordfmt"
	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
)

// recordPrinter writes streamed records to an io.Writer in the chosen format,
// reusing the shared pkg/recordfmt library so the client and daemon format
// identically. It serializes writes (the listen path fans out across worker
// goroutines) and emits the csv/tsv header exactly once.
type recordPrinter struct {
	mu         sync.Mutex
	w          io.Writer
	format     string
	cols       []recordfmt.Column
	comma      rune
	headerDone bool
}

// newRecordPrinter validates the format (and -columns for csv/tsv) and returns
// a printer. An unknown format or bad column spec is an error.
func newRecordPrinter(w io.Writer, format, columns string) (*recordPrinter, error) {
	p := &recordPrinter{w: w, format: format}
	switch format {
	case recordfmt.FormatJSON, recordfmt.FormatHumanize, recordfmt.FormatNull:
		// no columns
	case recordfmt.FormatCSV, recordfmt.FormatTSV:
		cols, err := recordfmt.SelectColumns(columns)
		if err != nil {
			return nil, err
		}
		p.cols = cols
		p.comma = ','
		if format == recordfmt.FormatTSV {
			p.comma = '\t'
		}
	default:
		return nil, fmt.Errorf("unknown -format %q (want json, csv, tsv, humanize, or null)", format)
	}
	return p, nil
}

// record formats and writes one record. Safe for concurrent callers.
func (p *recordPrinter) record(r *xtcp_flat_record.XtcpFlatRecord) {
	if p == nil || r == nil || p.format == recordfmt.FormatNull {
		return
	}

	switch p.format {
	case recordfmt.FormatCSV, recordfmt.FormatTSV:
		// One-row envelope through the shared table encoder; header once.
		env := &xtcp_flat_record.Envelope{Row: []*xtcp_flat_record.XtcpFlatRecord{r}}
		p.mu.Lock()
		defer p.mu.Unlock()
		first := !p.headerDone
		p.headerDone = true
		b, err := recordfmt.MarshalEnvelopeTable(env, p.cols, p.comma, first)
		if err != nil {
			log.Printf("xtcp2client: format %s: %v", p.format, err)
			return
		}
		p.write(b)

	default: // json, humanize — one object per line
		var b []byte
		var err error
		if p.format == recordfmt.FormatHumanize {
			b, err = recordfmt.MarshalHumanizedJSON(r)
		} else {
			b, err = recordfmt.MarshalJSON(r)
		}
		if err != nil {
			log.Printf("xtcp2client: format %s: %v", p.format, err)
			return
		}
		p.mu.Lock()
		defer p.mu.Unlock()
		p.write(append(b, '\n'))
	}
}

// write emits bytes to the sink, logging (not failing) on a write error —
// the caller holds the mutex (csv path) or has just taken it (json path).
func (p *recordPrinter) write(b []byte) {
	if _, err := p.w.Write(b); err != nil {
		log.Printf("xtcp2client: write: %v", err)
	}
}
