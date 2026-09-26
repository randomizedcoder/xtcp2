package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	// Import parse so its parsers register via init(), making parser keys like
	// "text_cidr" valid during config validation.
	_ "github.com/randomizedcoder/xtcp2/internal/ipfeed/parse"
)

func writeFile(t *testing.T, dir, name, body string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadFile(t *testing.T) {
	tests := []struct {
		name    string
		desc    string
		class   string
		body    string
		wantErr bool
	}{
		{"positive_valid", "positive: a complete valid source loads", "positive",
			"name: cf\nprovider: cloudflare\nurl: https://x/y\nparser: text_cidr\n", false},
		{"negative_missing_url", "negative: a missing required field (url) errors", "negative",
			"name: cf\nprovider: cloudflare\nparser: text_cidr\n", true},
		{"negative_unknown_parser", "negative: an unregistered parser key errors", "negative",
			"name: cf\nurl: https://x/y\nparser: nope_parser\n", true},
		{"negative_unknown_field", "negative: an unknown YAML key is rejected (KnownFields)", "negative",
			"name: cf\nurl: https://x/y\nparser: text_cidr\ntypo_field: 1\n", true},
		{"boundary_minimal", "boundary: only the three required fields is enough", "boundary",
			"name: m\nurl: https://x\nparser: csv\n", false},
		{"corner_bad_discover", "corner: an unknown discover mode errors", "corner",
			"name: m\nurl: https://x\nparser: csv\ndiscover: teleport\n", true},
	}
	dir := t.TempDir()
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			p := writeFile(t, dir, tc.name+".yaml", tc.body)
			_, err := LoadFile(p)
			if tc.wantErr && err == nil {
				t.Fatalf("%s: expected error, got nil", tc.desc)
			}
			if !tc.wantErr && err != nil {
				t.Fatalf("%s: unexpected error: %v", tc.desc, err)
			}
		})
	}
}

// TestLoadDir covers directory-level loading: the up-front stat of the
// sources dir, file globbing, enabled filtering, ordering, and cross-file
// duplicate detection. Each row's setup builds the directory (or non-directory)
// to load and returns the path to pass to LoadDir.
func TestLoadDir(t *testing.T) {
	const valid = "name: %s\nurl: https://x\nparser: text_cidr\n"

	tests := []struct {
		description   string
		setup         func(t *testing.T) string // returns the path handed to LoadDir
		expectErr     bool
		expectErrText string   // substring the error must contain (when expectErr)
		expectNames   []string // enabled source names, in returned order (when !expectErr)
	}{
		// positive
		{
			description: "positive: a dir with one valid yaml loads that source",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				writeFile(t, dir, "cf.yaml", fmt.Sprintf(valid, "cloudflare"))
				return dir
			},
			expectNames: []string{"cloudflare"},
		},
		{
			description: "positive: multiple files load sorted by source name, disabled ones excluded",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				writeFile(t, dir, "b.yaml", fmt.Sprintf(valid, "bbb"))
				writeFile(t, dir, "a.yaml", fmt.Sprintf(valid, "aaa"))
				writeFile(t, dir, "off.yaml", fmt.Sprintf(valid, "ccc")+"enabled: false\n")
				return dir
			},
			expectNames: []string{"aaa", "bbb"},
		},
		// negative
		{
			description: "negative: a missing dir errors with a clear 'not found'",
			setup: func(t *testing.T) string {
				return filepath.Join(t.TempDir(), "does-not-exist")
			},
			expectErr:     true,
			expectErrText: "not found",
		},
		{
			description: "negative: a path that is a file, not a dir, errors with 'not a directory'",
			setup: func(t *testing.T) string {
				return writeFile(t, t.TempDir(), "sources.yaml", fmt.Sprintf(valid, "x"))
			},
			expectErr:     true,
			expectErrText: "not a directory",
		},
		{
			description: "negative: a dir containing an invalid yaml fails the whole load (fail fast)",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				writeFile(t, dir, "good.yaml", fmt.Sprintf(valid, "good"))
				writeFile(t, dir, "bad.yaml", "name: bad\nurl: https://x\nparser: nope_parser\n")
				return dir
			},
			expectErr:     true,
			expectErrText: "unknown parser",
		},
		{
			description: "negative: malformed yaml syntax fails the load",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				writeFile(t, dir, "broken.yaml", "name: [unterminated\n")
				return dir
			},
			expectErr:     true,
			expectErrText: "decode",
		},
		{
			description: "negative: two files sharing a source name are rejected as duplicates",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				writeFile(t, dir, "one.yaml", "name: dup\nurl: https://x\nparser: csv\n")
				writeFile(t, dir, "two.yaml", "name: dup\nurl: https://y\nparser: csv\n")
				return dir
			},
			expectErr:     true,
			expectErrText: "duplicate source name",
		},
		// boundary
		{
			description: "boundary: an empty dir loads zero sources without error",
			setup:       func(t *testing.T) string { return t.TempDir() },
			expectNames: nil,
		},
		{
			description: "boundary: a dir whose only source is disabled loads zero sources",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				writeFile(t, dir, "off.yaml", fmt.Sprintf(valid, "off")+"enabled: false\n")
				return dir
			},
			expectNames: nil,
		},
		// corner
		{
			description: "corner: .yml files are picked up alongside .yaml",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				writeFile(t, dir, "a.yml", fmt.Sprintf(valid, "yml-source"))
				writeFile(t, dir, "b.yaml", fmt.Sprintf(valid, "yaml-source"))
				return dir
			},
			expectNames: []string{"yaml-source", "yml-source"},
		},
		{
			description: "corner: non-yaml files in the dir are ignored, not parsed",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				writeFile(t, dir, "README.md", "# not yaml at all\n")
				writeFile(t, dir, "notes.txt", "name: [broken\n")
				writeFile(t, dir, "ok.yaml", fmt.Sprintf(valid, "ok"))
				return dir
			},
			expectNames: []string{"ok"},
		},
		{
			description: "corner: a disabled duplicate still counts as a duplicate name",
			setup: func(t *testing.T) string {
				dir := t.TempDir()
				writeFile(t, dir, "one.yaml", fmt.Sprintf(valid, "dup"))
				writeFile(t, dir, "two.yaml", fmt.Sprintf(valid, "dup")+"enabled: false\n")
				return dir
			},
			expectErr:     true,
			expectErrText: "duplicate source name",
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			path := tc.setup(t)
			got, err := LoadDir(path)
			if tc.expectErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (loaded %d sources)", tc.expectErrText, len(got))
				}
				if !strings.Contains(err.Error(), tc.expectErrText) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.expectErrText)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			var names []string
			for _, s := range got {
				names = append(names, s.Name)
			}
			if !slices.Equal(names, tc.expectNames) {
				t.Errorf("source names = %q, want %q", names, tc.expectNames)
			}
			for _, s := range got {
				if s.Path == "" {
					t.Errorf("source %q: Path not recorded for diagnostics", s.Name)
				}
			}
		})
	}
}
