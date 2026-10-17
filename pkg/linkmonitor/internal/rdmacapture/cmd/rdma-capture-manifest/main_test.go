package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stage(t *testing.T, meta bool) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"sysfs/mlx5_0/node_guid":   "9803:9b03:0012:3456\n",
		"nldev/ibv_devinfo.strace": "socket(AF_NETLINK, SOCK_RAW, NETLINK_RDMA) = 3\n",
		"nldev/ibv_devinfo.out":    "node_guid: 9803:9b03:0012:3456\n",
		"index.tsv":                "nldev/ibv_devinfo\tibv_devinfo -d mlx5_0\t\n",
	}
	if meta {
		files["meta/uname"] = "Linux l 7.1.8 x86_64\n"
		files["meta/driver"] = "mlx5_core\n"
		files["meta/firmware"] = "14.32.1010\n"
		files["meta/rdma_core"] = "65.0\n"
	}
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

func valuesFile(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "values")
	if err := os.WriteFile(p, []byte("guid 9803:9b03:0012:3456\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestRun(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		args        func(t *testing.T) []string
		expected    int
		check       func(t *testing.T, args []string)
	}{
		{"write", "positive", "write redacts, consumes index.tsv and meta/, writes the manifest",
			func(t *testing.T) []string {
				return []string{"write", "-dir", stage(t, true), "-source", "l-mlx5", "-values", valuesFile(t)}
			}, 0,
			func(t *testing.T, args []string) {
				dir := args[2]
				for _, gone := range []string{"index.tsv", "meta"} {
					if _, err := os.Stat(filepath.Join(dir, gone)); !os.IsNotExist(err) {
						t.Fatalf("%s still present", gone)
					}
				}
				data, err := os.ReadFile(filepath.Join(dir, "nldev", "ibv_devinfo.out"))
				if err != nil || strings.Contains(string(data), "9803:9b03") {
					t.Fatalf("not redacted: %q, %v", data, err)
				}
				if code := run([]string{"verify", dir}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
					t.Fatalf("verify after write exited %d", code)
				}
			}},
		{"write_no_values", "boundary", "write without -values records an empty redaction map",
			func(t *testing.T) []string { return []string{"write", "-dir", stage(t, true), "-source", "vm-rxe"} }, 0, nil},
		{"write_no_meta", "negative", "a stage without meta/ is refused",
			func(t *testing.T) []string { return []string{"write", "-dir", stage(t, false), "-source", "vm-rxe"} }, 1, nil},
		{"write_no_source", "negative", "write needs -source",
			func(t *testing.T) []string { return []string{"write", "-dir", stage(t, true)} }, 1, nil},
		{"verify_tampered", "negative", "a byte changed after write fails verify",
			func(t *testing.T) []string {
				dir := stage(t, true)
				if code := run([]string{"write", "-dir", dir, "-source", "vm-rxe"}, &bytes.Buffer{}, &bytes.Buffer{}); code != 0 {
					t.Fatalf("write exited %d", code)
				}
				if err := os.WriteFile(filepath.Join(dir, "nldev", "ibv_devinfo.out"), []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				return []string{"verify", dir}
			}, 1, nil},
		{"verify_none", "boundary", "verify with no directories", func(*testing.T) []string { return []string{"verify"} }, 1, nil},
		{"no_args", "negative", "no subcommand prints usage", func(*testing.T) []string { return nil }, 2, nil},
		{"unknown", "corner", "an unknown subcommand", func(*testing.T) []string { return []string{"redact"} }, 1, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: exit %d", tt.category, tt.description, tt.expected)
			args := tt.args(t)
			var stdout, stderr bytes.Buffer
			if code := run(args, &stdout, &stderr); code != tt.expected {
				t.Fatalf("exit %d, want %d; stdout %q stderr %q", code, tt.expected, stdout.String(), stderr.String())
			}
			if tt.check != nil {
				tt.check(t, args)
			}
		})
	}
}
