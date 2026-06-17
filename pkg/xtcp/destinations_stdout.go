package xtcp

import (
	"context"
	"fmt"
	"io"
	"os"
)

// writerDest sends each marshaled payload to an arbitrary io.Writer,
// verbatim. It is the reusable core behind any stream sink: the "stdout"
// scheme wires it to os.Stdout, "stderr" to os.Stderr, and "file" to an
// *os.File — each a one-line factory over this type rather than a copy of
// the Send/Close boilerplate.
//
// Framing is the marshaller's responsibility, not the destination's: the
// text/line marshallers (protoJson envelope, jsonl, csv, tsv) terminate their
// output with a trailing newline, while the binary marshallers (protobufList,
// msgpack) do not. Keeping writerDest a verbatim byte writer means every
// format frames correctly on every sink — including the raw tcp/http sinks
// that must not have stray bytes injected into a length-delimited stream.
//
// The io.Writer seam is also the test seam — unit tests inject a
// *bytes.Buffer and assert on the bytes without touching the real os.Stdout.
//
// Send is invoked serially (see the Destination contract), so the writer is
// used without an internal mutex.
type writerDest struct {
	x      *XTCP
	w      io.Writer
	label  string    // metric label, e.g. "destStdout"
	closer io.Closer // optional; closed by Close. nil for os.Stdout/os.Stderr.
}

func (d *writerDest) Send(_ context.Context, b *[]byte) (int, error) {
	d.x.pC.WithLabelValues(d.label, "start", "count").Inc()
	n, err := d.w.Write(*b)
	if err != nil {
		return n, fmt.Errorf("%s write: %w", d.label, err)
	}
	return n, nil
}

func (d *writerDest) Close() error {
	if d.closer != nil {
		return d.closer.Close()
	}
	return nil
}

// newStdoutDest wires writerDest to os.Stdout. Pair with a line-oriented
// marshaller (`-marshal jsonl|csv|tsv|protoJson`) for human/jq-able output;
// the daemon's logs go to stderr, so stdout carries only records.
func newStdoutDest(_ context.Context, x *XTCP) (Destination, error) {
	return &writerDest{x: x, w: os.Stdout, label: "destStdout"}, nil
}

func init() {
	RegisterDestination(schemeStdout, newStdoutDest)
}
