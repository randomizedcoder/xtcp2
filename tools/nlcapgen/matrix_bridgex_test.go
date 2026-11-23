package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestBridgexSpace pins the bridge extras rows (gobridge 6X-C): batch mode,
// -family/-4/-6/-force/-netns, and router ports. Rows are keyed by command,
// fixture and batch file.
//
// go test ./tools/nlcapgen/ -run TestBridgexSpace
func TestBridgexSpace(t *testing.T) {
	rows, err := matrixRows(families["bridgex"](), matrixOpts{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := checkRows(rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 63 {
		t.Errorf("%d rows, want 63 (23 commands plain and -j, 17 negatives)", len(rows))
	}
	key := func(r row) string { return r.Cmd + "|" + r.Axes["fix"] + "|" + r.Axes["batch"] }
	byKey := map[string]row{}
	for _, r := range rows {
		if !strings.HasPrefix(r.ID, "bridgex/") {
			t.Errorf("%s: not in bridgex/", r.ID)
		}
		if r.InitNS || r.Serial || r.Proto != "route" || strings.Join(r.Modules, ",") != "bridge,vxlan" {
			t.Errorf("%s: initns %t serial %t proto %q modules %v", r.ID, r.InitNS, r.Serial, r.Proto, r.Modules)
		}
		if _, dup := byKey[key(r)]; dup {
			t.Errorf("%s: %q twice", r.ID, key(r))
		}
		byKey[key(r)] = r
		// A batch row's file is the last pre step's.
		if strings.Contains(r.Cmd, "-batch b.txt") || strings.Contains(r.Cmd, "-batch - <") {
			if len(r.Pre) == 0 || !strings.HasSuffix(r.Pre[len(r.Pre)-1].Cmd, "> b.txt") {
				t.Errorf("%s: batch row whose last pre step doesn't write b.txt", r.ID)
			}
		}
	}

	tests := []struct {
		description string
		key         string // cmd|fix|batch
		expected    string // kind rc refused-by errno extack | pre count
	}{
		// Positive.
		{"positive: a two-line batch", "bridge -batch b.txt|br|two", "positive 0 none   | 5"},
		{"positive: a batch on stdin, -j", "bridge -j -batch - < b.txt|br|stdin", "positive 0 none   | 5"},
		{"positive: -force with nothing failing", "bridge -force -batch b.txt|br|force", "positive 0 none   | 5"},
		{"positive: -family inet is a no-op", "bridge -family inet link show|br|", "positive 0 none   | 4"},
		{"positive: -s router ports", "bridge -s vlan global show|brrtr|", "positive 0 none   | 10"},
		// Negative.
		{"negative: a failed line stops the batch", "bridge -batch b.txt|br|fail-stops", "negative 1 iproute2   | 5"},
		{"negative: -force goes on, rc 1", "bridge -force -batch b.txt|br|fail-force", "negative 1 ack-then-fail   | 5"},
		{"negative: nodev, then the next line", "bridge -force -batch b.txt|br|nodev-force", "negative 1 kernel ENODEV  | 5"},
		{"negative: B29 second vid is a duparg", "bridge -force -batch b.txt|br|sticky-vid-duparg", "negative 255 ack-then-fail   | 5"},
		{"negative: B30 exit(1) through -force", "bridge -force -batch b.txt|br|dump-exit-force",
			"negative 1 kernel EINVAL The device is not a vxlan device | 5"},
		{"negative: no batch file", "bridge -batch nosuch|bare|", "negative 1 iproute2   | 0"},
		{"negative: -batch with no file", "bridge -batch|bare|", "negative 255 iproute2   | 0"},
		{"negative: no such netns", "bridge -netns nosuch link show|bare|", "negative 255 iproute2   | 0"},
		{"negative: B33 -netns with no name", "bridge -netns|bare|", "negative 255 iproute2   | 0"},
		{"negative: unknown family", "bridge -family ipx link show|bare|", "negative 255 iproute2   | 0"},
		{"negative: unterminated quote", "bridge -batch b.txt|br|unterminated", "negative 1 iproute2   | 5"},
		// Boundary.
		{"boundary: 511 words fit", "bridge -batch b.txt|br|511-words", "boundary 0 none   | 5"},
		{"boundary: the 512th word exits", "bridge -batch b.txt|br|512-words", "negative 1 iproute2   | 5"},
		// Corner.
		{"corner: B28 vlan dev filter carried over", "bridge -batch b.txt|br|sticky-vlan-dev", "corner 0 none   | 5"},
		{"corner: B28 into vlan global show", "bridge -j -batch b.txt|br|sticky-global-dev", "corner 0 none   | 5"},
		{"corner: B28 mst dev filter", "bridge -batch b.txt|brmst|sticky-mst-dev", "corner 0 none   | 6"},
		{"corner: B31 missing continuation", "bridge -batch b.txt|br|no-continuation", "corner 0 none   | 5"},
		{"corner: B32 words after the file", "bridge -batch b.txt link show|br|extra-words", "corner 0 none   | 5"},
		{"corner: -4 parses, no effect", "bridge -4 vlan show|br|", "corner 0 none   | 4"},
		{"corner: -o router ports", "bridge -s -o vlan global show|brrtr|", "corner 0 none   | 10"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r, ok := byKey[tc.key]
			if !ok {
				t.Fatalf("no row %q", tc.key)
			}
			kind, _, _ := strings.Cut(r.Desc, ":")
			got := fmt.Sprintf("%s %d %s %s %s | %d", kind, *r.Expect.RC, r.Expect.RefusedBy, r.Expect.Errno, r.Expect.Extack, len(r.Pre))
			if got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}

	// Every positive command runs plain and -j.
	for k, r := range byKey {
		if strings.HasPrefix(r.Desc, "negative:") || strings.Contains(r.Cmd, " -j ") {
			continue
		}
		cmd, rest, _ := strings.Cut(k, "|")
		if _, ok := byKey[strings.Replace(cmd, "bridge ", "bridge -j ", 1)+"|"+rest]; !ok {
			t.Errorf("%s: %q has no -j row", r.ID, cmd)
		}
	}

	// Determinism: the same seed writes the same bytes.
	var a, b bytes.Buffer
	if err := writeRows(&a, nil, rows); err != nil {
		t.Fatal(err)
	}
	again, err := matrixRows(families["bridgex"](), matrixOpts{Seed: 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRows(&b, nil, again); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		t.Error("two runs with seed 1 differ")
	}
}

// TestBatchFile runs batchFile's and batchWords' pre steps in sh and checks
// the b.txt they write: the line bridge's getcmdline will read.
//
// go test ./tools/nlcapgen/ -run TestBatchFile
func TestBatchFile(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	tests := []struct {
		description string
		step        string
		expected    string
	}{
		{"positive: two lines", batchFile("link show", "vlan show"), "link show\nvlan show\n"},
		{"positive: one line", batchFile("vlan show dev d1"), "vlan show dev d1\n"},
		{"boundary: an empty line", batchFile("", "link"), "\nlink\n"},
		{"boundary: no extra words", batchWords("link show", 0), "link show\n"},
		{"boundary: three extra words", batchWords("link show", 3), "link show x x x\n"},
		{"corner: a single quote", batchFile(`vlan show dev 'd2'`), "vlan show dev 'd2'\n"},
		{"corner: a double quote and a #", batchFile(`link show dev "d1" # c`), "link show dev \"d1\" # c\n"},
		{"corner: a trailing backslash stays", batchFile(`link \`), "link \\\n"},
		{"corner: printf escapes in a line stay literal", batchFile(`a\nb %s`), "a\\nb %s\n"},
		{"negative: shell metacharacters stay literal", batchFile("$(touch pwned); `x`"), "$(touch pwned); `x`\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := t.TempDir()
			cmd := exec.Command("sh", "-c", tc.step)
			cmd.Dir = dir
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("%v: %s", err, out)
			}
			got, err := os.ReadFile(filepath.Join(dir, "b.txt"))
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
			if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
				t.Error("a line was run by the shell")
			}
		})
	}
}
