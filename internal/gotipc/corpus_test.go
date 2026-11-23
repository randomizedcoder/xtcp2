package gotipc

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// corpusGlob finds every kernel's tipc ws corpus from this package.
const corpusGlob = "../../pkg/xtcpnl/testdata/*/ws/tipc"

type cmdJSON struct {
	Cmd    string            `json:"cmd"`
	Pre    []json.RawMessage `json:"pre"`
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

// preCmds is the row's setup steps' commands; a step is a string, or
// [cmd, [rcs]].
func (c cmdJSON) preCmds(t *testing.T) []string {
	t.Helper()
	out := make([]string, 0, len(c.Pre))
	for _, raw := range c.Pre {
		var s string
		if json.Unmarshal(raw, &s) == nil {
			out = append(out, s)
			continue
		}
		var pair []json.RawMessage
		if err := json.Unmarshal(raw, &pair); err != nil || len(pair) == 0 || json.Unmarshal(pair[0], &s) != nil {
			t.Fatalf("pre step %s is neither a string nor [cmd, rcs]", raw)
		}
		out = append(out, s)
	}
	return out
}

// hash128to32 is the kernel's node hash (net/tipc/addr.h): the identity's
// four big-endian words XORed, or ORed when that is zero.
func hash128to32(id [16]byte) uint32 {
	var x, o uint32
	for i := 0; i < 16; i += 4 {
		w := binary.BigEndian.Uint32(id[i:])
		x ^= w
		o |= w
	}
	if x != 0 {
		return x
	}
	return o
}

// nodeState is the node a row's capture ran on, from its setup: the
// identity `tipc node set identity` gave it, and its address, the
// identity's hash once a bearer was enabled (0 before, as the bare and
// ident rows print).
func nodeState(t *testing.T, c cmdJSON) (own string, addr uint32) {
	t.Helper()
	bearer := false
	for _, p := range c.preCmds(t) {
		if rest, ok := strings.CutPrefix(p, "tipc node set identity "); ok {
			own = strings.TrimSpace(rest)
		}
		if strings.Contains(p, "tipc bearer enable ") {
			bearer = true
		}
	}
	if bearer && own != "" {
		addr = hash128to32(nodeIDBytes(own))
	}
	return own, addr
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

func replay(t *testing.T, dir, own string, addr uint32, args ...string) result {
	t.Helper()
	src, err := NewReplaySource(filepath.Join(dir, "ip.pcap"))
	if err != nil {
		t.Fatal(err)
	}
	src.Own, src.Addr = own, addr
	var out, errb bytes.Buffer
	rc := run(config{src: src}, args, &out, &errb)
	return result{stdout: out.String(), stderr: errb.String(), rc: rc, pending: src.Pending()}
}

// TestGotipcCorpus replays every tipc ws row through gotipc with the
// arguments tipc was run with (cmd.json), and holds its stdout, stderr and
// exit code to tipc's .out, .err and .rc. Replay itself checks that each
// request is tipc's (family, flags and attributes, byte for byte), that
// every recorded request is asked for, and so that the follow-up queries
// socket list and monitor list make are the ones tipc made. The node's
// identity and address, which tipc reads from a TIPC socket, come from the
// row's setup (nodeState).
//
// go test ./internal/gotipc/ -run TestGotipcCorpus
func TestGotipcCorpus(t *testing.T) {
	bases, err := filepath.Glob(corpusGlob)
	if err != nil {
		t.Fatal(err)
	}
	if len(bases) == 0 {
		t.Fatal("no tipc ws corpus under testdata/*/ws/tipc")
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
				if len(args) == 0 || args[0] != "tipc" {
					t.Fatalf("cmd %q is not a tipc command", c.Cmd)
				}
				args = args[1:]
				own, addr := nodeState(t, c)
				got := replay(t, dir, own, addr, args...)
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
					t.Fatalf("tipc %s\n got  %#v\n want %#v", strings.Join(args, " "), got, want)
				}
				if c.Axes["json"] == "1" && strings.HasPrefix(got.stdout, "[") {
					jsonRows++
				}
			})
		}
	}
	t.Logf("replayed %d tipc rows, %d printing JSON", rows, jsonRows)
	if rows != 93 {
		t.Errorf("%d rows, want the 93 of 5C", rows)
	}
}

// firstRow is the named row of the first kernel's tipc corpus.
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
