package rdmacapture

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTree writes files (slash paths) under a new temporary directory.
func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for p, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func captureTree() map[string]string {
	return map[string]string{
		"nldev/ibv_devinfo-mlx5_0.strace": "socket(AF_NETLINK, SOCK_RAW, NETLINK_RDMA) = 3\n",
		"nldev/ibv_devinfo-mlx5_0.out":    "node_guid: 9803:9b03:0012:3456\n",
		"nldev/ibv_devinfo-mlx5_0.rc":     "0\n",
		"sysfs/mlx5_0/node_guid":          "9803:9b03:0012:3456\n",
		"nlmon/gid_add_del.pcap":          string(pcap(realLL)),
	}
}

func TestBuildVerify(t *testing.T) {
	index := map[string]IndexEntry{
		"nldev/ibv_devinfo-mlx5_0": {Stem: "nldev/ibv_devinfo-mlx5_0", Command: "ibv_devinfo -d mlx5_0"},
		"nlmon/gid_add_del":        {Stem: "nlmon/gid_add_del", Command: "tcpdump -i nlmon0", Trigger: "ip -6 addr add/del"},
	}
	tests := []struct {
		name        string
		category    string
		description string
		files       map[string]string
		index       map[string]IndexEntry
		mutate      func(t *testing.T, dir string)
		buildErr    string // non-empty: Build fails with this text
		expected    string // "" means Verify passes; otherwise a substring of its error
	}{
		{"valid", "positive", "a redacted capture verifies", captureTree(), index, nil, "", ""},
		{"tampered_byte", "negative", "one byte changed after the manifest was written", captureTree(), index,
			func(t *testing.T, dir string) { appendFile(t, dir, "sysfs/mlx5_0/node_guid", "x") }, "", "sha256"},
		{"missing_file", "negative", "a listed file deleted", captureTree(), index,
			func(t *testing.T, dir string) { removeFile(t, dir, "nldev/ibv_devinfo-mlx5_0.rc") }, "", "no such file"},
		{"extra_file", "negative", "a file added after the manifest was written", captureTree(), index,
			func(t *testing.T, dir string) { addFile(t, dir, "async/late.strace") }, "", "not in the manifest"},
		{"golden_ignored", "corner", "golden files sit beside the captures unlisted", captureTree(), index,
			func(t *testing.T, dir string) { addFile(t, dir, "async/x.golden.json") }, "", ""},
		{"outside_stream", "negative", "a file outside every stream directory", map[string]string{"stray.txt": "x"}, nil, nil,
			"not under a stream directory", ""},
		{"unknown_stream", "negative", "a top-level directory that is not a stream", map[string]string{"logs/a.txt": "x"}, nil, nil,
			"not under a stream directory", ""},
		{"empty", "boundary", "a capture with no files", map[string]string{}, nil, nil, "no captured files", ""},
		{"index_unused", "negative", "the index names a row that wrote nothing", captureTree(),
			map[string]IndexEntry{"umad/smpquery": {Stem: "umad/smpquery"}}, nil, "wrote no file", ""},
		{"traversal", "negative", "a manifest path that leaves the directory", captureTree(), index,
			func(t *testing.T, dir string) { rewriteManifest(t, dir, "sysfs/mlx5_0/node_guid", "../node_guid") }, "", "clean relative path"},
		{"unknown_field", "negative", "a manifest field the reader does not know", captureTree(), index,
			func(t *testing.T, dir string) { rewriteManifest(t, dir, `"source"`, `"extra": 1, "source"`) }, "", "unknown field"},
		{"stream_mismatch", "negative", "a file's stream does not match its directory", captureTree(), index,
			func(t *testing.T, dir string) { rewriteManifest(t, dir, `"stream": "sysfs"`, `"stream": "umad"`) }, "", "does not match its directory"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: build %q, verify %q", tt.category, tt.description, tt.buildErr, tt.expected)
			dir := writeTree(t, tt.files)
			m, err := Build(dir, Manifest{Source: "l-mlx5"}, tt.index, testRedactor(t))
			if tt.buildErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.buildErr) {
					t.Fatalf("Build: got %v, want %q", err, tt.buildErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := Write(dir, m); err != nil {
				t.Fatal(err)
			}
			if tt.mutate != nil {
				tt.mutate(t, dir)
			}
			_, err = Verify(dir)
			if tt.expected == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if !errors.Is(err, ErrManifest) || !strings.Contains(err.Error(), tt.expected) {
				t.Fatalf("Verify: got %v, want %q", err, tt.expected)
			}
		})
	}
}

func TestBuildRedactsAndIndexes(t *testing.T) {
	t.Log("positive: Build rewrites real values in place and attaches the index to every file of a row; expected: no real value left, commands recorded")
	dir := writeTree(t, captureTree())
	index := map[string]IndexEntry{"nldev/ibv_devinfo-mlx5_0": {Stem: "nldev/ibv_devinfo-mlx5_0", Command: "ibv_devinfo -d mlx5_0"}}
	m, err := Build(dir, Manifest{Source: "l-mlx5"}, index, testRedactor(t))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "sysfs", "mlx5_0", "node_guid"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "0200:5e10:0000:0001\n" {
		t.Fatalf("node_guid = %q", data)
	}
	withCommand := 0
	for _, f := range m.Files {
		if f.Command == "ibv_devinfo -d mlx5_0" {
			withCommand++
		}
	}
	if withCommand != 3 {
		t.Fatalf("%d files carry the row's command, want 3 (.strace, .out, .rc)", withCommand)
	}
	if len(m.Redaction) == 0 {
		t.Fatal("no redaction map")
	}
}

func TestReadIndex(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		input       string
		expected    int // entries; -1: error
	}{
		{"rows", "positive", "two rows", "nldev/a\tcmd a\t\nnlmon/b\ttcpdump\tip link set x down\n", 2},
		{"empty", "boundary", "no rows", "", 0},
		{"two_fields", "negative", "a row without the trigger column", "nldev/a\tcmd\n", -1},
		{"duplicate", "negative", "a stem listed twice", "nldev/a\tx\t\nnldev/a\ty\t\n", -1},
		{"blank_lines", "corner", "blank lines are skipped", "\nnldev/a\tx\t\n\n", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %d", tt.category, tt.description, tt.expected)
			got, err := ReadIndex([]byte(tt.input))
			if tt.expected < 0 {
				if !errors.Is(err, ErrManifest) {
					t.Fatalf("got %v, %v; want ErrManifest", got, err)
				}
				return
			}
			if err != nil || len(got) != tt.expected {
				t.Fatalf("got %d, %v; want %d", len(got), err, tt.expected)
			}
		})
	}
}

func appendFile(t *testing.T, dir, rel, s string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(dir, filepath.FromSlash(rel)), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func removeFile(t *testing.T, dir, rel string) {
	t.Helper()
	if err := os.Remove(filepath.Join(dir, filepath.FromSlash(rel))); err != nil {
		t.Fatal(err)
	}
}

func addFile(t *testing.T, dir, rel string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("late\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func rewriteManifest(t *testing.T, dir, from, to string) {
	t.Helper()
	p := filepath.Join(dir, ManifestName)
	data, err := os.ReadFile(p) // #nosec G304 -- test temp dir
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), from) {
		t.Fatalf("manifest has no %q", from)
	}
	if err := os.WriteFile(p, []byte(strings.Replace(string(data), from, to, 1)), 0o600); err != nil { // #nosec G703 -- test temp dir
		t.Fatal(err)
	}
}
