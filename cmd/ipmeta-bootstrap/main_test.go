package main

import (
	"path/filepath"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
	"github.com/randomizedcoder/xtcp2/internal/ipfeed/output"
	"github.com/randomizedcoder/xtcp2/pkg/ipasn"
)

func TestRun(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "full.parquet")
	out := filepath.Join(dir, "bootstrap.lookup.parquet.zst")
	if _, err := output.WriteParquet(in, []model.Record{
		{Prefix: "8.8.8.0/24", ASN: 15169, NetworkOwner: "google"},
	}); err != nil {
		t.Fatalf("write input parquet: %v", err)
	}

	tests := []struct {
		name        string
		description string
		args        []string
		wantErr     bool
	}{
		{
			name:        "positive_build",
			description: "positive: full collector parquet is projected into a compact zstd lookup artifact",
			args:        []string{"-in", in, "-out", out},
		},
		{
			name:        "negative_missing_args",
			description: "negative: input and output paths are required",
			args:        nil,
			wantErr:     true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := run(tc.args)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("%s: expected error", tc.description)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: %v", tc.description, err)
			}
			ix, err := ipasn.New(out)
			if err != nil {
				t.Fatalf("%s: reload output: %v", tc.description, err)
			}
			if got := ix.Len(); got != 1 {
				t.Fatalf("%s: Len = %d, want 1", tc.description, got)
			}
		})
	}
}
