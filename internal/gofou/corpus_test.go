package gofou

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// corpusGlob finds every kernel's fou ws corpus from this package.
const corpusGlob = "../../pkg/xtcpnl/testdata/*/ws/fou"

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

// TestGofouCorpus replays every fou ws row through gofou with the arguments
// `ip` was run with (cmd.json, minus the leading "ip"), and holds its stdout,
// stderr and exit code to ip's .out, .err and .rc. Replay itself checks that
// each request is ip's (the GETFAMILY and the FOU_CMD_GET dump, byte for
// byte), and that every recorded request is asked for.
func TestGofouCorpus(t *testing.T) {
	bases, err := filepath.Glob(corpusGlob)
	if err != nil {
		t.Fatal(err)
	}
	if len(bases) == 0 {
		t.Fatal("no fou ws corpus under testdata/*/ws/fou")
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
				if len(args) == 0 || args[0] != "ip" {
					t.Fatalf("cmd %q is not an ip command", c.Cmd)
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
					t.Fatalf("ip %s\n got  %#v\n want %#v", strings.Join(args, " "), got, want)
				}
				if c.Axes["json"] == "1" && !json.Valid([]byte(got.stdout)) {
					t.Fatalf("not JSON: %q", got.stdout)
				}
			})
		}
	}
	t.Logf("replayed %d fou rows", rows)
}

// TestGofouReplayGuards pins that a replay fails, rc 2, when gofou asks for
// something ip did not, and that a clean row leaves nothing pending.
func TestGofouReplayGuards(t *testing.T) {
	show := firstRow(t, "ec-004") // ip fou show over the multi fixture
	tests := []struct {
		description string
		args        []string
		wantRC      int
		wantPending bool
		stderrHas   string
	}{
		{"positive: the capture's own command replays clean", []string{"fou", "show"}, 0, false, ""},
		// help is refused before the family resolve, so the capture's two
		// requests (GETFAMILY + the dump) are both left unasked for.
		{"boundary: help sends nothing and leaves the capture unasked", []string{"fou", "help"}, 255, true, "Usage"},
		// A stray arg is refused after the family resolve, so only the dump
		// is left pending.
		{"negative: a stray arg sends only the family resolve", []string{"fou", "show", "junk"}, 1, true, "does not take any arguments"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := replay(t, show, tc.args...)
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
		if _, err := NewReplaySource(filepath.Join(show, "nope.pcap")); err == nil {
			t.Fatal("want an error for a missing capture")
		}
	})
	t.Run("negative: no source at all", func(t *testing.T) {
		var out, errb bytes.Buffer
		if rc := run(config{}, []string{"fou", "show"}, &out, &errb); rc != 2 {
			t.Fatalf("rc %d, want 2 (stderr %q)", rc, errb.String())
		}
	})
}

// firstRow is the named row of the first kernel's fou corpus.
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
