package gobridge

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// corpusGlob finds every kernel's bridge ws corpora from this package: 6C's
// bridge, 6X-C's bridgex and 7C's bridgefdb and bridgemdb.
const corpusGlob = "../../pkg/xtcpnl/testdata/*/ws/bridge*"

// corpusRows is how many rows each corpus holds (V097, V101, V108).
var corpusRows = map[string]int{"bridge": 113, "bridgex": 63, "bridgefdb": 94, "bridgemdb": 74}

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
	return replayIn(t, dir, nil, args...)
}

// replayIn is replay with stdin, for `-batch -`.
func replayIn(t *testing.T, dir string, stdin io.Reader, args ...string) result {
	t.Helper()
	src, err := NewReplaySource(filepath.Join(dir, "ip.pcap"))
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	rc := run(config{src: src, stdin: stdin}, args, &out, &errb)
	return result{stdout: out.String(), stderr: errb.String(), rc: rc, pending: src.Pending()}
}

// jsonStream checks s is one or more JSON documents and nothing else.
func jsonStream(s string) error {
	dec := json.NewDecoder(strings.NewReader(s))
	n := 0
	for {
		var v any
		err := dec.Decode(&v)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return err
		}
		n++
	}
	if n == 0 {
		return errors.New("no document")
	}
	return nil
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
		t.Fatal("no bridge ws corpus under testdata/*/ws/bridge*")
	}
	rows, jsonRows := 0, 0
	perBase := map[string]int{}
	for _, base := range bases {
		base, err := filepath.Abs(base) // a batch row runs in its own directory
		if err != nil {
			t.Fatal(err)
		}
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
			perBase[filepath.Base(base)]++
			t.Run(kernel+"/"+filepath.Base(base)+"/"+e.Name(), func(t *testing.T) {
				if c.Result.Status != "OK" {
					t.Fatalf("row status %s", c.Result.Status)
				}
				args := strings.Fields(c.Cmd)
				if len(args) == 0 || args[0] != "bridge" {
					t.Fatalf("cmd %q is not a bridge command", c.Cmd)
				}
				args = args[1:]
				// A batch row reads b.txt, written beside its capture: from
				// the row's directory, or with `< b.txt` on stdin.
				var stdin io.Reader
				if n := len(args); n >= 2 && args[n-2] == "<" {
					f, err := os.Open(filepath.Join(dir, args[n-1]))
					if err != nil {
						t.Fatal(err)
					}
					defer func() {
						if err := f.Close(); err != nil {
							t.Error(err)
						}
					}()
					stdin, args = f, args[:n-2]
				}
				t.Chdir(dir)
				got := replayIn(t, dir, stdin, args...)
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
					// A batch prints one array per command that got as far
					// as new_json_obj: a stream of documents (B36).
					if err := jsonStream(got.stdout); err != nil {
						t.Fatalf("-j stdout is not a JSON stream (%v): %q", err, got.stdout)
					}
					jsonRows++
				}
			})
		}
	}
	t.Logf("replayed %d bridge rows, %d printing JSON", rows, jsonRows)
	for name, want := range corpusRows {
		if perBase[name] != want {
			t.Errorf("%s: %d rows, want %d", name, perBase[name], want)
		}
	}
}
