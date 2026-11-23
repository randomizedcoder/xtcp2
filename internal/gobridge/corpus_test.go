package gobridge

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// corpusGlob finds every kernel's bridge ws corpus from this package.
const corpusGlob = "../../pkg/xtcpnl/testdata/*/ws/bridge"

type cmdJSON struct {
	Cmd    string            `json:"cmd"`
	Axes   map[string]string `json:"axes"`
	Result struct {
		Status string `json:"status"`
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

// TestGobridgeCorpus replays every bridge ws row through gobridge with the
// arguments bridge was run with (cmd.json), and holds its stdout, stderr
// and exit code to bridge's .out, .err and .rc. Replay itself checks that
// each request is bridge's, byte for byte (seq aside), and the pending
// count that every recorded request was asked for: the link lookups
// bridge made for names its cache lacked included.
//
// go test ./internal/gobridge/ -run TestGobridgeCorpus
func TestGobridgeCorpus(t *testing.T) {
	bases, err := filepath.Glob(corpusGlob)
	if err != nil {
		t.Fatal(err)
	}
	if len(bases) == 0 {
		t.Fatal("no bridge ws corpus under testdata/*/ws/bridge")
	}
	rows, jsonRows := 0, 0
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
					t.Fatalf("row status %s", c.Result.Status)
				}
				args := strings.Fields(c.Cmd)
				if len(args) == 0 || args[0] != "bridge" {
					t.Fatalf("cmd %q is not a bridge command", c.Cmd)
				}
				args = args[1:]
				got := replay(t, dir, args...)
				wantRC, err := strconv.Atoi(strings.TrimSpace(readFile(t, filepath.Join(dir, "ip.rc"))))
				if err != nil {
					t.Fatal(err)
				}
				want := result{
					stdout: readFile(t, filepath.Join(dir, "ip.out")),
					stderr: readFile(t, filepath.Join(dir, "ip.err")),
					rc:     wantRC,
				}
				if got != want {
					t.Fatalf("bridge %s\n got  %#v\n want %#v", strings.Join(args, " "), got, want)
				}
				if strings.Contains(" "+c.Cmd+" ", " -j ") && strings.HasPrefix(got.stdout, "[") {
					if !json.Valid([]byte(got.stdout)) {
						t.Fatalf("-j stdout is not JSON: %q", got.stdout)
					}
					jsonRows++
				}
			})
		}
	}
	t.Logf("replayed %d bridge rows, %d printing JSON", rows, jsonRows)
	if rows != 113 {
		t.Errorf("%d rows, want the 113 of 6C", rows)
	}
}
