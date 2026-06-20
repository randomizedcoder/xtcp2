package xtcp

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// stderr and file destinations: thin factories over the reusable writerDest.
// Both are stdlib (no build tag). Pair with a line/tabular marshaller
// (jsonl/csv/tsv/protoJson) — the marshaller frames each flush.

// newStderrDest writes records to the process's stderr. Handy when stdout is
// reserved for something else, or for `2>records.log`.
func newStderrDest(_ context.Context, x *XTCP) (Destination, error) {
	return &writerDest{x: x, w: os.Stderr, label: "destStderr"}, nil
}

// newFileDest appends records to a file: `-dest file:/var/log/xtcp.jsonl`.
// The file is created if missing and opened for append; the *os.File is
// closed via writerDest.Close.
func newFileDest(_ context.Context, x *XTCP) (Destination, error) {
	path := strings.TrimPrefix(x.config.Dest, schemeFile+":")
	if path == "" {
		return nil, fmt.Errorf("InitDestFile: empty path (use -dest file:/path/to/file)")
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("InitDestFile OpenFile(%q): %w", path, err)
	}
	return &writerDest{x: x, w: f, label: "destFile", closer: f}, nil
}

func init() {
	RegisterDestination(schemeStderr, newStderrDest)
	RegisterDestination(schemeFile, newFileDest)
}
