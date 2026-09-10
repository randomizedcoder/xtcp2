package s3

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigKey(t *testing.T) {
	tests := []struct {
		name     string
		desc     string
		class    string
		prefix   string
		filename string
		want     string
	}{
		{"positive_prefix", "positive: prefix and filename are joined with a slash", "positive",
			"ipfeeds", "2026-09-09-14-30.parquet", "ipfeeds/2026-09-09-14-30.parquet"},
		{"boundary_empty_prefix", "boundary: an empty prefix returns just the filename", "boundary",
			"", "2026-09-09-14-30.parquet", "2026-09-09-14-30.parquet"},
		{"corner_slashy_prefix", "corner: surrounding slashes on the prefix are trimmed", "corner",
			"/a/b/", "f.parquet", "a/b/f.parquet"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := Config{Prefix: tc.prefix}
			if got := cfg.Key(tc.filename); got != tc.want {
				t.Errorf("%s: Key = %q, want %q", tc.desc, got, tc.want)
			}
		})
	}
}

func TestSecretFromFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "secret")
	if err := os.WriteFile(good, []byte("  s3cr3t\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		desc    string
		class   string
		path    string
		want    string
		wantErr bool
	}{
		{"positive_trimmed", "positive: file contents are read and whitespace-trimmed", "positive",
			good, "s3cr3t", false},
		{"boundary_empty_path", "boundary: an empty path returns empty with no error", "boundary",
			"", "", false},
		{"negative_missing", "negative: a missing file errors", "negative",
			filepath.Join(dir, "nope"), "", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SecretFromFile(tc.path)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("%s: expected error, got nil", tc.desc)
				}
				return
			}
			if err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.desc, err)
			}
			if got != tc.want {
				t.Errorf("%s: got %q, want %q", tc.desc, got, tc.want)
			}
		})
	}
}
