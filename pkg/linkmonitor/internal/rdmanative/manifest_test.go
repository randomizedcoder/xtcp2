package rdmanative

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/rdmacapture"
)

// captureDirs returns every committed capture directory, plus any listed in
// LINKMONITOR_CAPTURE_DIRS (colon-separated), which is how the capture leaf
// checks a dry run's output.
func captureDirs(t *testing.T) []string {
	t.Helper()
	manifests, err := filepath.Glob(filepath.Join("testdata", "*", "*", rdmacapture.ManifestName))
	if err != nil {
		t.Fatal(err)
	}
	dirs := make([]string, 0, len(manifests))
	for _, m := range manifests {
		dirs = append(dirs, filepath.Dir(m))
	}
	if extra := os.Getenv("LINKMONITOR_CAPTURE_DIRS"); extra != "" {
		dirs = append(dirs, strings.Split(extra, ":")...)
	}
	return dirs
}

// TestCaptureManifests verifies every capture directory's sha256 list, then
// shows on a copy that a tampered file fails.
func TestCaptureManifests(t *testing.T) {
	for _, dir := range captureDirs(t) {
		t.Run(filepath.ToSlash(dir), func(t *testing.T) {
			t.Logf("positive: %s; expected: every file matches the manifest and nothing is unlisted", dir)
			if _, err := rdmacapture.Verify(dir); err != nil {
				t.Fatal(err)
			}
		})
	}

	tests := []struct {
		name        string
		category    string
		description string
		mutate      func(dir string) error
		expected    bool // Verify passes
	}{
		{"copy", "positive", "an unmodified copy", func(string) error { return nil }, true},
		{"tampered", "negative", "one byte appended to a captured file",
			func(dir string) error { return appendByte(filepath.Join(dir, "sysfs", "dev", "node_guid")) }, false},
		{"removed", "negative", "a captured file deleted",
			func(dir string) error { return os.Remove(filepath.Join(dir, "sysfs", "dev", "node_guid")) }, false},
		{"golden_added", "corner", "a golden file added beside the captures",
			func(dir string) error {
				return os.WriteFile(filepath.Join(dir, "sysfs", "dev.golden.json"), []byte("{}\n"), 0o600)
			}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: verify passes = %v", tt.category, tt.description, tt.expected)
			dir := syntheticCapture(t)
			if err := tt.mutate(dir); err != nil {
				t.Fatal(err)
			}
			_, err := rdmacapture.Verify(dir)
			if tt.expected != (err == nil) {
				t.Fatalf("Verify: %v", err)
			}
			if err != nil && !errors.Is(err, rdmacapture.ErrManifest) {
				t.Fatalf("Verify: %v does not wrap ErrManifest", err)
			}
		})
	}
}

// syntheticCapture writes a small capture and its manifest through the same
// code the capture tool uses.
func syntheticCapture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for p, content := range map[string]string{
		"sysfs/dev/node_guid":  "0200:5e10:0000:0001\n",
		"async/dev-up.strace":  "read(3, \"\\x09\\x00\\x00\\x00\", 16) = 16\n",
		"nldev/devinfo.strace": "socket(AF_NETLINK, SOCK_RAW, NETLINK_RDMA) = 3\n",
	} {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	r, err := rdmacapture.NewRedactor(nil)
	if err != nil {
		t.Fatal(err)
	}
	m, err := rdmacapture.Build(dir, rdmacapture.Manifest{Source: "synthetic"}, nil, r)
	if err != nil {
		t.Fatal(err)
	}
	if err := rdmacapture.Write(dir, m); err != nil {
		t.Fatal(err)
	}
	return dir
}

func appendByte(p string) error {
	f, err := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0) // #nosec G304 -- test temp dir
	if err != nil {
		return err
	}
	_, err = f.Write([]byte{'x'})
	return errors.Join(err, f.Close())
}
