package xtcp

import (
	"context"
	"os"
)

// stderr destination: a thin factory over the reusable writerDest. Stdlib
// (no build tag). Pair with a line/tabular marshaller (jsonl/csv/tsv/
// protoJson) — the marshaller frames each flush.

// newStderrDest writes records to the process's stderr. Handy when stdout is
// reserved for something else, or for `2>records.log`.
func newStderrDest(_ context.Context, x *XTCP) (Destination, error) {
	return &writerDest{x: x, w: os.Stderr, label: "destStderr"}, nil
}

func init() {
	RegisterDestination(schemeStderr, newStderrDest)
}
