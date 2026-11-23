//go:build dest_s3parquet

package main

import (
	"bytes"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/parquet-go/parquet-go"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp"
)

// testRows is n established v4 sockets on well-known source ports, so each
// row has its own key.
func testRows(n int) []xtcp.ParquetRow {
	rows := make([]xtcp.ParquetRow, n)
	for i := range rows {
		src := make([]byte, 16)
		a := netip.AddrFrom4([4]byte{10, 0, 0, byte(i + 1)}).As4()
		copy(src, a[:])
		rows[i] = xtcp.ParquetRow{
			Hostname: "node-17", InetDiagMsgFamily: 2, InetDiagMsgState: 1,
			InetDiagMsgSocketSourcePort: uint32(1000 + i), InetDiagMsgSocketSource: src,
			TcpInfoSndMss: 1448, TcpInfoRtt: uint32(100 + i),
		}
	}
	return rows
}

func writeRows(t *testing.T, path string, rows []xtcp.ParquetRow) string {
	t.Helper()
	if err := parquet.WriteFile(path, rows); err != nil {
		t.Fatal(err)
	}
	return path
}

// fixture writes a fixture directory the way the capture runner does:
// scrub, normalize, manifest. edit, when set, changes it afterwards.
func fixture(t *testing.T, rows []xtcp.ParquetRow, edit func(dir string)) string {
	t.Helper()
	dir := t.TempDir()
	raw := writeRows(t, filepath.Join(t.TempDir(), "raw.parquet"), rows)
	out := filepath.Join(dir, "records.parquet")
	var stdout, stderr bytes.Buffer
	if rc := run([]string{"scrub", raw, out}, &stdout, &stderr); rc != exitSame {
		t.Fatalf("scrub: %d %s", rc, stderr.String())
	}
	if rc := run([]string{"normalize", out}, &stdout, &stderr); rc != exitSame {
		t.Fatalf("normalize: %d %s", rc, stderr.String())
	}
	files := map[string]string{
		"records.jsonl": stdout.String(),
		"manifest.json": `{"rows": ` + strconv.Itoa(len(rows)) + "}\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if edit != nil {
		edit(dir)
	}
	return dir
}

func TestRun(t *testing.T) {
	tmp := t.TempDir()
	golden := writeRows(t, filepath.Join(tmp, "golden.parquet"), testRows(3))
	noisy := testRows(3)
	for i := range noisy {
		noisy[i].TcpInfoRtt += 5000
		noisy[i].Hostname = "node-18"
	}
	noise := writeRows(t, filepath.Join(tmp, "noise.parquet"), noisy)
	zeroed := testRows(3)
	zeroed[1].TcpInfoSndMss = 0
	missing := writeRows(t, filepath.Join(tmp, "missing.parquet"), zeroed)
	many := writeRows(t, filepath.Join(tmp, "many.parquet"), testRows(60))
	empty := writeRows(t, filepath.Join(tmp, "empty.parquet"), []xtcp.ParquetRow{})
	type other struct {
		X uint32 `parquet:"x"`
	}
	drifted := filepath.Join(tmp, "drift.parquet")
	if err := parquet.WriteFile(drifted, []other{{1}}); err != nil {
		t.Fatal(err)
	}
	rewrite := func(name, old, new string) func(string) {
		return func(dir string) {
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer root.Close()
			b, err := root.ReadFile(name)
			if err != nil {
				t.Fatal(err)
			}
			if err := root.WriteFile(name, bytes.Replace(b, []byte(old), []byte(new), 1), 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}

	type expected struct {
		code   int
		output string // substring of stdout+stderr; "" = anything
	}
	cases := []struct {
		name        string
		category    string
		description string
		args        func() []string
		expected    expected
	}{
		{"identical", "positive", "golden against itself",
			func() []string { return []string{"diff", golden, golden} }, expected{exitSame, ""}},
		{"noise_only", "positive", "RTTs and hostname changed",
			func() []string { return []string{"diff", golden, noise} }, expected{exitSame, ""}},
		{"check_ok", "positive", "a fixture as the runner writes it",
			func() []string { return []string{"check", fixture(t, testRows(3), nil)} }, expected{exitSame, ""}},
		{"missing_attribute", "negative", "tcp_info_snd_mss zeroed in one row",
			func() []string { return []string{"diff", golden, missing} },
			expected{exitDiff, `tcp_info_snd_mss: golden "1448", candidate "0"`}},
		{"bad_filter", "negative", "a -columns regex that does not compile",
			func() []string { return []string{"diff", "-columns", "(", golden, golden} },
			expected{exitError, "columns filter"}},
		{"missing_file", "negative", "a path that does not exist",
			func() []string { return []string{"normalize", filepath.Join(tmp, "nope.parquet")} },
			expected{exitError, "no such file"}},
		{"jsonl_edited", "negative", "records.jsonl changed by hand",
			func() []string {
				return []string{"check", fixture(t, testRows(3), rewrite("records.jsonl", `"1448"`, `"1449"`))}
			}, expected{exitDiff, "records.jsonl is not records.parquet normalized: line 1:\n  tcp_info_snd_mss: committed \"1449\", normalized \"1448\""}},
		{"real_address", "negative", "a fixture whose records.parquet was not scrubbed",
			func() []string {
				return []string{"check", fixture(t, testRows(3), func(dir string) {
					writeRows(t, filepath.Join(dir, "records.parquet"), testRows(3))
				})}
			}, expected{exitDiff, "unscrubbed address 10.0.0.1"}},
		{"no_args", "boundary", "no command", func() []string { return nil }, expected{exitError, "usage"}},
		{"output_cap", "boundary", "60 rows against none print 50 lines and a count",
			func() []string { return []string{"diff", many, empty} }, expected{exitDiff, "and 10 more"}},
		{"manifest_rows", "boundary", "manifest.json counts a different number of rows",
			func() []string {
				return []string{"check", fixture(t, testRows(3), rewrite("manifest.json", "3", "4"))}
			}, expected{exitDiff, "manifest.json says 4 rows, records.parquet has 3"}},
		{"schema_drift", "corner", "a file whose columns are not ParquetRow's",
			func() []string { return []string{"diff", drifted, golden} },
			expected{exitError, "recapture with nix run .#capture-golden-records"}},
		{"unknown_command", "corner", "a command that does not exist",
			func() []string { return []string{"compare", golden, golden} }, expected{exitError, `unknown command "compare"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			code := run(tc.args(), &out, &out)
			if code != tc.expected.code {
				t.Fatalf("[%s] %s: exit %d, expected %d:\n%s", tc.category, tc.description, code, tc.expected.code, out.String())
			}
			if !strings.Contains(out.String(), tc.expected.output) {
				t.Fatalf("[%s] %s: output lacks %q:\n%s", tc.category, tc.description, tc.expected.output, out.String())
			}
		})
	}
}
