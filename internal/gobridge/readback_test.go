package gobridge

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readbackGlob finds the fdb and mdb write corpora whose rows each read
// back with `bridge fdb|mdb show dev X`: the readback's nlmon capture is
// netlink_route_dump.pcap and its stdout dump.out (no stderr or rc kept).
const readbackGlob = "../../pkg/xtcpnl/testdata/*/ws/[fm]db*"

// readbackRows is how many readbacks each corpus holds (V105).
var readbackRows = map[string]int{"fdbbr": 99, "fdbvx": 100, "mdbbr": 90, "mdbvx": 87}

// readbacksLive is false until 7R implements fdb and mdb show; until then
// the rows are counted and their commands checked, not replayed.
var readbacksLive = true

type readbackJSON struct {
	Readback []struct {
		Name string `json:"name"`
		Cmd  string `json:"cmd"`
	} `json:"readback"`
}

// TestGobridgeReadbacks replays each fdb/mdb write row's readback through
// gobridge and holds its stdout to dump.out, byte for byte; the replay
// checks every request is bridge's and the pending count that all were
// asked for.
//
// go test ./internal/gobridge/ -run TestGobridgeReadbacks
func TestGobridgeReadbacks(t *testing.T) {
	bases, err := filepath.Glob(readbackGlob)
	if err != nil {
		t.Fatal(err)
	}
	perBase := map[string]int{}
	for _, base := range bases {
		kernel := filepath.Base(filepath.Dir(filepath.Dir(base)))
		entries, err := os.ReadDir(base)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			dir := filepath.Join(base, e.Name())
			if _, err := os.Stat(filepath.Join(dir, "netlink_route_dump.pcap")); err != nil {
				continue
			}
			var rb readbackJSON
			if err := json.Unmarshal([]byte(readFile(t, filepath.Join(dir, "cmd.json"))), &rb); err != nil {
				t.Fatal(err)
			}
			if len(rb.Readback) != 1 || rb.Readback[0].Name != "dump" {
				t.Fatalf("%s: readback %+v, want one named dump", dir, rb.Readback)
			}
			args := strings.Fields(rb.Readback[0].Cmd)
			if len(args) < 3 || args[0] != "bridge" || (args[1] != "fdb" && args[1] != "mdb") || args[2] != "show" {
				t.Fatalf("%s: readback %q is not a bridge fdb/mdb show", dir, rb.Readback[0].Cmd)
			}
			perBase[filepath.Base(base)]++
			if !readbacksLive {
				continue
			}
			t.Run(kernel+"/"+filepath.Base(base)+"/"+e.Name(), func(t *testing.T) {
				src, err := NewReplaySource(filepath.Join(dir, "netlink_route_dump.pcap"))
				if err != nil {
					t.Fatal(err)
				}
				var out, errb bytes.Buffer
				rc := run(config{src: src}, args[1:], &out, &errb)
				want := readFile(t, filepath.Join(dir, "dump.out"))
				if rc != 0 || out.String() != want || errb.Len() != 0 || src.Pending() != 0 {
					t.Fatalf("%s: rc %d pending %d stderr %q\n got  %q\n want %q",
						rb.Readback[0].Cmd, rc, src.Pending(), errb.String(), out.String(), want)
				}
			})
		}
	}
	for name, want := range readbackRows {
		if perBase[name] != want {
			t.Errorf("%s: %d readbacks, want %d", name, perBase[name], want)
		}
	}
	if !readbacksLive {
		t.Skip("counted; replayed once 7R implements fdb and mdb show")
	}
}
