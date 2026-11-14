package govdpa

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// corpusGlob finds every kernel's vdpa ws corpus from this package.
const corpusGlob = "../../pkg/xtcpnl/testdata/*/ws/vdpa"

type cmdJSON struct {
	Cmd    string `json:"cmd"`
	Desc   string `json:"desc"`
	Axes   map[string]string
	Result struct {
		Status string `json:"status"`
		RC     *int   `json:"rc"`
	} `json:"result"`
}

func readCmdJSON(t *testing.T, dir string) cmdJSON {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, "cmd.json"))
	if err != nil {
		t.Fatal(err)
	}
	var c cmdJSON
	if err := json.Unmarshal(b, &c); err != nil {
		t.Fatalf("%s/cmd.json: %v", dir, err)
	}
	return c
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

type result struct {
	stdout, stderr string
	rc, pending    int
}

func replay(t *testing.T, dir string, args ...string) result {
	t.Helper()
	src, err := NewReplaySource(filepath.Join(dir, "ip.pcap"))
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	rc := run(config{src: src}, args, &out, &errb)
	return result{stdout: out.String(), stderr: errb.String(), rc: rc, pending: src.Pending()}
}

// TestGovdpaCorpus replays every vdpa ws row through govdpa with the
// arguments vdpa was run with (cmd.json), and holds its stdout, stderr and
// exit code to vdpa's .out, .err and .rc. Replay itself checks that each
// request is vdpa's (family, flags and attributes, byte for byte), and that
// every recorded request is asked for.
func TestGovdpaCorpus(t *testing.T) {
	bases, err := filepath.Glob(corpusGlob)
	if err != nil {
		t.Fatal(err)
	}
	if len(bases) == 0 {
		t.Fatal("no vdpa ws corpus under testdata/*/ws/vdpa")
	}
	rows := 0
	for _, base := range bases {
		kernel := filepath.Base(filepath.Dir(filepath.Dir(base)))
		entries, err := os.ReadDir(base)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			dir := filepath.Join(base, e.Name())
			c := readCmdJSON(t, dir)
			rows++
			t.Run(kernel+"/"+e.Name(), func(t *testing.T) {
				if c.Result.Status != "OK" {
					t.Skipf("row status %s", c.Result.Status)
				}
				args := strings.Fields(c.Cmd)
				if len(args) == 0 || args[0] != "vdpa" {
					t.Fatalf("cmd %q is not a vdpa command", c.Cmd)
				}
				args = args[1:]
				got := replay(t, dir, args...)

				wantRC, err := strconv.Atoi(strings.TrimSpace(readFile(t, filepath.Join(dir, "ip.rc"))))
				if err != nil {
					t.Fatal(err)
				}
				want := result{
					stdout:  readFile(t, filepath.Join(dir, "ip.out")),
					stderr:  readFile(t, filepath.Join(dir, "ip.err")),
					rc:      wantRC,
					pending: 0,
				}
				if got != want {
					t.Fatalf("vdpa %s\n got  %#v\n want %#v", strings.Join(args, " "), got, want)
				}
				if c.Axes["json"] == "1" && !json.Valid([]byte(got.stdout)) {
					t.Fatalf("not JSON: %q", got.stdout)
				}
			})
		}
	}
	t.Logf("replayed %d vdpa rows", rows)
}

// TestGovdpaReplayGuards pins that a replay fails, rc 2, when govdpa asks for
// something vdpa did not, and that a clean row leaves nothing pending.
func TestGovdpaReplayGuards(t *testing.T) {
	dir := firstRow(t, "ec-001") // vdpa mgmtdev show
	tests := []struct {
		description string
		args        []string
		wantRC      int
		wantPending bool
		stderrHas   string
	}{
		{"positive: the capture's own command replays clean", []string{"mgmtdev", "show"}, 0, false, ""},
		// Unlike godevlink's, the command byte is matched (vdpa's enum is
		// upstream's), so a same-shape dump of another object is refused.
		{"negative: a dev dump differs from the capture's mgmtdev dump", []string{"dev", "show"}, 2, true, "replay"},
		{"negative: a targeted GET differs from the capture's dump", []string{"dev", "config", "show", "vdpa0"}, 2, true, "replay"},
		// An unknown object is refused after vdpa_init's one family resolve,
		// so the capture's dump is left unasked for.
		{"boundary: an unknown object sends only the family resolve", []string{"nosuch"}, 1, true, "not found"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := replay(t, dir, tc.args...)
			if got.rc != tc.wantRC {
				t.Errorf("rc %d, want %d (stderr %q)", got.rc, tc.wantRC, got.stderr)
			}
			if (got.pending > 0) != tc.wantPending {
				t.Errorf("pending %d, want >0 = %v", got.pending, tc.wantPending)
			}
			if tc.stderrHas != "" && !strings.Contains(got.stderr, tc.stderrHas) {
				t.Errorf("stderr %q, want it to contain %q", got.stderr, tc.stderrHas)
			}
		})
	}

	t.Run("negative: a capture that does not exist", func(t *testing.T) {
		if _, err := NewReplaySource(filepath.Join(dir, "nope.pcap")); err == nil {
			t.Fatal("want an error for a missing capture")
		}
	})
	t.Run("negative: no source at all", func(t *testing.T) {
		var out, errb bytes.Buffer
		if rc := run(config{}, []string{"dev", "show"}, &out, &errb); rc != 2 {
			t.Fatalf("rc %d, want 2 (stderr %q)", rc, errb.String())
		}
	})
}

// firstRow is the named row of the first kernel's vdpa corpus.
func firstRow(t *testing.T, row string) string {
	t.Helper()
	bases, err := filepath.Glob(corpusGlob)
	if err != nil || len(bases) == 0 {
		t.Fatalf("no corpus: %v", err)
	}
	dir := filepath.Join(bases[0], row)
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("no %s row: %v", row, err)
	}
	return dir
}
