package gotc

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

// corpusRows is how many rows each space holds (V002).
var corpusRows = map[string]int{"tcqdisc": 142, "tcclass": 82, "tcfilter": 159, "tcflower": 97, "tcaction": 132}

// corpusKnown are the rows gotc cannot match byte for byte, each with the
// reason, so the rest are held exactly.
var corpusKnown = map[string]string{
	// Q34: tc's second graph walks freed nodes; its stderr's first line is
	// heap garbage ("!!!Deficit N, rta_len=<garbage>"). gotc gives the
	// abort's line and status.
	"tcclass/ng-012": "Q34 heap garbage on stderr",
}

// corpusWrites are the write rows 9C recorded so none could pass as a
// read: gotc refuses each, exit 255, sending nothing.
// corpusRefused are the rows gotc refuses, exit 255, naming what: a
// flower get with match keys.
var corpusRefused = map[string]string{
	// FL21: tc parses get's match keys, sends them, then fails on the
	// words left; gotc refuses the key.
	"tcflower/ng-004": "not implemented: flower ip_proto in a get",
}

var corpusWrites = map[string]bool{
	"tcqdisc/ng-018": true, "tcclass/ng-011": true, "tcfilter/ng-024": true, "tcflower/ng-009": true,
	"tcaction/ng-006": true,
}

// pschedRow is the one row that reads the workbench's /proc/net/psched
// beside its capture; pschedContent is what it read, loaded before any
// row's Chdir.
const pschedRow = "../../pkg/xtcpnl/testdata/7_3_1/ws/tcqdisc/pw-001/psched.out"

var pschedContent string

// shareDir holds the workbench tc's CONF_USR_DIR files (iproute2 7.2.0's
// share/iproute2), as an absolute path taken before any row's Chdir.
var shareDir = func() string {
	d, err := filepath.Abs(filepath.Join("testdata", "share-iproute2"))
	if err != nil {
		panic(err)
	}
	return d
}()

type cmdJSON struct {
	ID     string `json:"id"`
	Cmd    string `json:"cmd"`
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

// testSys is a sysFS root holding the workbench's psched and, when the row
// has one, its cls.txt (the -cf file, read relative to the root).
func testSys(t *testing.T, rowDir string) sysFS {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "proc", "net"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "proc", "net", "psched"), []byte(pschedContent), 0o600); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(filepath.Join(rowDir, "cls.txt")); err == nil {
		rt, err := os.OpenRoot(root)
		if err != nil {
			t.Fatal(err)
		}
		defer func() {
			if err := rt.Close(); err != nil {
				t.Error(err)
			}
		}()
		if err := rt.WriteFile("cls.txt", b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// tc's CONF_USR_DIR files: the workbench tc's ematch_map and rt_realms.
	if err := os.MkdirAll(filepath.Join(root, confUsrDirCst), 0o755); err != nil {
		t.Fatal(err)
	}
	rt, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := rt.Close(); err != nil {
			t.Error(err)
		}
	}()
	for _, name := range []string{ematchMapName, "rt_realms"} {
		b, err := os.ReadFile(filepath.Join(shareDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := rt.WriteFile(strings.TrimPrefix(confUsrDirCst, "/")+"/"+name, b, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return sysFS{root: root, usrDir: confUsrDirCst}
}

// replayIn replays dir's ip.pcap through gotc, with stdin for `-batch -`.
func replayIn(t *testing.T, dir string, stdin io.Reader, args ...string) result {
	t.Helper()
	src, err := NewReplaySource(filepath.Join(dir, "ip.pcap"))
	if err != nil {
		t.Fatal(err)
	}
	var out, errb bytes.Buffer
	rc := run(config{src: src, stdin: stdin, sys: testSys(t, dir)}, args, &out, &errb)
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

// TestGotcCorpus replays every tcqdisc and tcclass row through gotc with
// the arguments tc was run with, and holds its stdout, stderr and exit
// code to tc's. Replay checks each request is tc's, byte for byte (seq
// aside), and the pending count that every recorded request was asked for.
//
// go test ./internal/gotc/ -run TestGotcCorpus
func TestGotcCorpus(t *testing.T) {
	bases, err := filepath.Glob("../../pkg/xtcpnl/testdata/*/ws/tcqdisc")
	if err != nil {
		t.Fatal(err)
	}
	for _, sp := range []string{"tcclass", "tcfilter", "tcflower", "tcaction"} {
		more, err := filepath.Glob("../../pkg/xtcpnl/testdata/*/ws/" + sp)
		if err != nil {
			t.Fatal(err)
		}
		bases = append(bases, more...)
	}
	pschedContent = readFile(t, pschedRow)
	if len(bases) == 0 {
		t.Fatal("no tc ws corpus")
	}
	rows, jsonRows, invalidJSON := 0, 0, 0
	perBase := map[string]int{}
	for _, base := range bases {
		base, err := filepath.Abs(base)
		if err != nil {
			t.Fatal(err)
		}
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
			t.Run(c.ID, func(t *testing.T) {
				got, want := corpusRow(t, dir, c)
				if corpusWrites[c.ID] {
					if got.rc != 255 || !strings.Contains(got.stderr, "not implemented") || got.stdout != "" {
						t.Fatalf("write row not refused: %#v", got)
					}
					return
				}
				if what, ok := corpusRefused[c.ID]; ok {
					if got.rc != 255 || !strings.Contains(got.stderr, what) {
						t.Fatalf("row not refused for %s: %#v", what, got)
					}
					return
				}
				if why, ok := corpusKnown[c.ID]; ok {
					if got.rc != want.rc || got.stdout != want.stdout {
						t.Fatalf("known row (%s) changed: got %#v\nwant %#v", why, got, want)
					}
					return
				}
				if got != want {
					t.Fatalf("%s\n got  %#v\n want %#v", c.Cmd, got, want)
				}
				if strings.Contains(" "+c.Cmd+" ", " -j ") && strings.HasPrefix(got.stdout, "[") {
					jsonRows++
					if err := jsonStream(got.stdout); err != nil {
						invalidJSON++ // tc's own: Q12, Q22, Q28
					}
				}
			})
		}
	}
	t.Logf("replayed %d tc rows, %d printing JSON, %d of them not a clean JSON stream", rows, jsonRows, invalidJSON)
	for name, want := range corpusRows {
		if perBase[name] != want {
			t.Errorf("%s: %d rows, want %d", name, perBase[name], want)
		}
	}
}

// corpusRow runs one row: a batch row's file is read from the row's
// directory, or from stdin after `<`.
func corpusRow(t *testing.T, dir string, c cmdJSON) (got, want result) {
	t.Helper()
	if c.Result.Status != "OK" {
		t.Fatalf("row status %s", c.Result.Status)
	}
	args := strings.Fields(c.Cmd)
	if len(args) == 0 || args[0] != "tc" {
		t.Fatalf("cmd %q is not a tc command", c.Cmd)
	}
	args = args[1:]
	var stdin io.Reader
	if n := len(args); n >= 2 && args[n-2] == "<" {
		f, err := os.Open(filepath.Join(dir, args[n-1]))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := f.Close(); err != nil {
				t.Error(err)
			}
		})
		stdin, args = f, args[:n-2]
	}
	t.Chdir(dir)
	got = replayIn(t, dir, stdin, args...)
	wantRC, err := strconv.Atoi(strings.TrimSpace(readFile(t, filepath.Join(dir, "ip.rc"))))
	if err != nil {
		t.Fatal(err)
	}
	want = result{
		stdout: readFile(t, filepath.Join(dir, "ip.out")),
		stderr: readFile(t, filepath.Join(dir, "ip.err")),
		rc:     wantRC,
	}
	return got, want
}
