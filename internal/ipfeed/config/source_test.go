package config

import (
	"os"
	"path/filepath"
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

func TestLoadDir(t *testing.T) {
	t.Run("positive_enabled_only_sorted", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "b.yaml", "name: bbb\nurl: https://x\nparser: text_cidr\n")
		writeFile(t, dir, "a.yaml", "name: aaa\nurl: https://x\nparser: text_cidr\n")
		writeFile(t, dir, "off.yaml", "name: ccc\nurl: https://x\nparser: text_cidr\nenabled: false\n")
		got, err := LoadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 {
			t.Fatalf("got %d sources, want 2 (disabled excluded)", len(got))
		}
		if got[0].Name != "aaa" || got[1].Name != "bbb" {
			t.Errorf("not sorted by name: %q, %q", got[0].Name, got[1].Name)
		}
	})

	t.Run("negative_duplicate_name", func(t *testing.T) {
		dir := t.TempDir()
		writeFile(t, dir, "one.yaml", "name: dup\nurl: https://x\nparser: csv\n")
		writeFile(t, dir, "two.yaml", "name: dup\nurl: https://y\nparser: csv\n")
		if _, err := LoadDir(dir); err == nil {
			t.Fatal("expected duplicate-name error, got nil")
		}
	})

	t.Run("boundary_empty_dir", func(t *testing.T) {
		got, err := LoadDir(t.TempDir())
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 0 {
			t.Fatalf("got %d, want 0", len(got))
		}
	})
}
