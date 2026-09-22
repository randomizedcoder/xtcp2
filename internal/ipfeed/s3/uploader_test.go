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

// TestParseEndpoint covers scheme stripping, TLS selection, and trailing-slash
// trimming. Path-style vs virtual-host addressing is not a concept here (minio
// decides that from the host), so it is not tested.
func TestParseEndpoint(t *testing.T) {
	tests := []struct {
		description    string
		in             string
		expectedHost   string
		expectedSecure bool
	}{
		// positive
		{"positive: https URL is stripped to host and selects TLS", "https://s3.amazonaws.com", "s3.amazonaws.com", true},
		{"positive: http URL is stripped to host and disables TLS", "http://minio.local:9000", "minio.local:9000", false},
		{"positive: bare host:port without scheme is passed through and defaults to TLS", "minio.local:9000", "minio.local:9000", true},
		{"positive: bare hostname without port or scheme defaults to TLS", "s3.us-east-1.amazonaws.com", "s3.us-east-1.amazonaws.com", true},
		// negative
		{"negative: an unrecognised scheme is not stripped and defaults to TLS (minio rejects it later)", "ftp://host:21", "ftp://host:21", true},
		{"negative: a malformed URL is passed through verbatim", "ht!tp://bad host", "ht!tp://bad host", true},
		// boundary
		{"boundary: empty string yields empty host with TLS default", "", "", true},
		{"boundary: a scheme with no host yields empty host", "https://", "", true},
		{"boundary: a lone slash is trimmed to empty", "/", "", true},
		// corner
		{"corner: trailing slash on https URL is trimmed", "https://s3.amazonaws.com/", "s3.amazonaws.com", true},
		{"corner: trailing slash on bare host:port is trimmed", "minio.local:9000/", "minio.local:9000", true},
		{"corner: only one trailing slash is trimmed", "http://host//", "host/", false},
		{"corner: a path component is kept after the host", "https://host:9000/bucket/", "host:9000/bucket", true},
		{"corner: uppercase scheme is not recognised and is kept verbatim", "HTTPS://host", "HTTPS://host", true},
		{"corner: an IPv4 literal with port and http scheme", "http://127.0.0.1:9000/", "127.0.0.1:9000", false},
		{"corner: a bracketed IPv6 literal with port and https scheme", "https://[::1]:9000", "[::1]:9000", true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			host, secure := parseEndpoint(tc.in)
			if host != tc.expectedHost || secure != tc.expectedSecure {
				t.Errorf("%s: parseEndpoint(%q) = (%q, %v), want (%q, %v)",
					tc.description, tc.in, host, secure, tc.expectedHost, tc.expectedSecure)
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
