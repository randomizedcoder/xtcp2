package godevlink

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// corpusGlob finds every kernel's devlink ws corpus from this package.
const corpusGlob = "../../pkg/xtcpnl/testdata/*/ws/devlink"

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

// TestGodevlinkCorpus replays every devlink ws row through godevlink with the
// arguments devlink was run with (cmd.json), and holds its stdout, stderr and
// exit code to devlink's .out, .err and .rc. Replay itself checks that each
// request is devlink's (family, flags and attributes, byte for byte), and that
// every recorded request is asked for.
func TestGodevlinkCorpus(t *testing.T) {
	bases, err := filepath.Glob(corpusGlob)
	if err != nil {
		t.Fatal(err)
	}
	if len(bases) == 0 {
		t.Fatal("no devlink ws corpus under testdata/*/ws/devlink")
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
				if len(args) == 0 || args[0] != "devlink" {
					t.Fatalf("cmd %q is not a devlink command", c.Cmd)
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
					t.Fatalf("devlink %s\n got  %#v\n want %#v", strings.Join(args, " "), got, want)
				}
				if c.Axes["json"] == "1" && !json.Valid([]byte(got.stdout)) {
					t.Fatalf("not JSON: %q", got.stdout)
				}
			})
		}
	}
	t.Logf("replayed %d devlink rows", rows)
}

// TestGodevlinkReplayGuards pins that a replay fails, rc 2, when godevlink asks
// for something devlink did not, and that a clean row leaves nothing pending.
func TestGodevlinkReplayGuards(t *testing.T) {
	bases, err := filepath.Glob(corpusGlob)
	if err != nil || len(bases) == 0 {
		t.Fatalf("no corpus: %v", err)
	}
	// Use the dev-show plain row (ec-001) of the first kernel.
	base := bases[0]
	dir := filepath.Join(base, "ec-001")
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("no ec-001 row: %v", err)
	}
	tests := []struct {
		description string
		args        []string
		wantRC      int
		wantPending bool
		stderrHas   string
	}{
		{"positive: the capture's own command replays clean", []string{"dev", "show"}, 0, false, ""},
		// A targeted GET of a handle differs from the capture's bare dump (its
		// flags carry no NLM_F_DUMP and it adds BUS_NAME/DEV_NAME), so the
		// replay rejects it — unlike a same-shape dump of another object, which
		// is indistinguishable once the kernel-specific command byte is masked.
		{"negative: a targeted GET differs from the capture's dump", []string{"dev", "show", "netdevsim/netdevsim0"}, 2, true, "replay"},
		// An unknown object is refused after dl_init's one family resolve, so
		// the capture's dump is left unasked for.
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
}
