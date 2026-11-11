//go:build dest_file

package xtcp

import (
	"context"
	"fmt"
	"os"
	"strings"
)

// file destination: a thin factory over the reusable writerDest. It writes
// records to a path chosen by configuration, so it is compiled in only with
// `-tags dest_file` (the full/dev flavor); production flavors omit it. Pair
// with a line/tabular marshaller (jsonl/csv/tsv/protoJson) — the marshaller
// frames each flush.

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
	RegisterDestination(schemeFile, newFileDest)
}
