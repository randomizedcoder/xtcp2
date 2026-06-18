package xtcp

import (
	"context"
	"fmt"
	"io"
	"os"
)

// writerDest sends each marshaled record to an arbitrary io.Writer,
// newline-terminated (one frame per Send). It is the reusable core behind
// any stream sink: the "stdout" scheme wires it to os.Stdout, and a future
// stderr/file sink is a one-line factory over the same type rather than a
// copy of the Send/Close boilerplate.
//
// The io.Writer seam is also the test seam — unit tests inject a
// *bytes.Buffer and assert on the framing without touching the real
// os.Stdout (see destinations_stdout_test.go).
//
// Pair with `-marshal protoJson` (or protoText) to stream records as NDJSON
// for local development, debugging, or piping to jq. The daemon's logs go to
// stderr, so stdout carries only records.
//
// Send is invoked serially (see the Destination contract), so the writer is
// used without an internal mutex.
type writerDest struct {
	x     *XTCP
	w     io.Writer
	label string // metric label, e.g. "destStdout"
}

// streamFrameSep terminates each record written by writerDest. Kept as a
// package-level slice so Send never appends to (and thus never reallocates
// or corrupts) the caller's pooled payload buffer.
var streamFrameSep = []byte{'\n'}

func (d *writerDest) Send(_ context.Context, b *[]byte) (int, error) {
	d.x.pC.WithLabelValues(d.label, "start", "count").Inc()
	n, err := d.w.Write(*b)
	if err != nil {
		return n, fmt.Errorf("%s write: %w", d.label, err)
	}
	if _, err := d.w.Write(streamFrameSep); err != nil {
		return n, fmt.Errorf("%s newline: %w", d.label, err)
	}
	return n, nil
}

func (d *writerDest) Close() error { return nil }

// newStdoutDest wires writerDest to os.Stdout.
func newStdoutDest(_ context.Context, x *XTCP) (Destination, error) {
	return &writerDest{x: x, w: os.Stdout, label: "destStdout"}, nil
}

func init() {
	RegisterDestination(schemeStdout, newStdoutDest)
}
