package goip

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// wsStatsReplay answers a stats row's requests from its capture: dumps and
// link gets as restoreReplay does, and ipstats_show_one's stats get with
// the capture's reply for its ifindex, recorded with the other gets.
type wsStatsReplay struct{ *restoreReplay }

func (r wsStatsReplay) Talk(request []byte, msgType uint16) ([]byte, error) {
	if msgType != unix.RTM_NEWSTATS {
		return r.restoreReplay.Talk(request, msgType)
	}
	r.gets = append(r.gets, zeroSeqPidCopy(request))
	bodies, err := r.inner.Dump(request, msgType)
	if err != nil {
		return nil, err
	}
	want := binary.NativeEndian.Uint32(request[xtcpnl.NlMsgHdrSizeCst+4:])
	for _, b := range bodies {
		if binary.NativeEndian.Uint32(b[4:8]) == want {
			return b, nil
		}
	}
	return nil, ErrNoReplay
}

// wsStatsRow is one stats row as the workbench recorded it.
type wsStatsRow struct {
	id, cmd string
	dir     string
	out     []byte
	rc      int
}

func wsStatsRows(t *testing.T) []wsStatsRow {
	t.Helper()
	dir := filepath.Join(wsCorpusDir731, "stats")
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]wsStatsRow, 0, len(ents))
	for _, e := range ents {
		rd := filepath.Join(dir, e.Name())
		raw, err := os.ReadFile(filepath.Join(rd, "cmd.json"))
		if err != nil {
			t.Fatal(err)
		}
		var cj struct {
			Cmd string `json:"cmd"`
		}
		if err := json.Unmarshal(raw, &cj); err != nil {
			t.Fatal(err)
		}
		out, err := os.ReadFile(filepath.Join(rd, "ip.out"))
		if err != nil {
			t.Fatal(err)
		}
		rcRaw, err := os.ReadFile(filepath.Join(rd, "ip.rc"))
		if err != nil {
			t.Fatal(err)
		}
		rc, err := strconv.Atoi(strings.TrimSpace(string(rcRaw)))
		if err != nil {
			t.Fatal(err)
		}
		rows = append(rows, wsStatsRow{id: e.Name(), cmd: cj.Cmd, dir: rd, out: out, rc: rc})
	}
	return rows
}

// TestWsStatsCorpus replays every `ip stats` row of the NC4 corpus through
// goip: ip's requests, a `stats set`'s write among them, its stdout byte for
// byte, and its rc; and the `ip link afstats` rows, iplink.c's loop over the
// same MPLS body. The `ip link xstats` rows are counted apart.
//
// go test ./internal/goip/ -run TestWsStatsCorpus
func TestWsStatsCorpus(t *testing.T) {
	useLLMap720(t) // the rows ran ip 7.2.0, whose lookups ask for names alone
	tests := []struct {
		description string
		prefix      string // the rows whose cmd starts so
		expected    int    // how many there are
	}{
		{description: "positive: every `ip stats show` and `ip stats` row", prefix: "ip stats show", expected: 73},
		{description: "positive: `ip stats help`, which prints the usage on stderr", prefix: "ip stats help", expected: 1},
		{description: "positive: `ip stats set`, on, off and a value parse_on_off refuses", prefix: "ip stats set", expected: 3},
		{description: "positive: `ip link afstats`, -j's members with no object around them as ip prints them",
			prefix: "ip link afstats", expected: 8},
	}
	rows := wsStatsRows(t)
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			n := 0
			for _, row := range rows {
				if !strings.HasPrefix(strings.Join(slices.DeleteFunc(strings.Fields(row.cmd), func(f string) bool {
					return strings.HasPrefix(f, "-")
				}), " "), tc.prefix) {
					continue
				}
				n++
				t.Run(row.id, func(t *testing.T) { wsStatsCheck(t, row) })
			}
			if n != tc.expected {
				t.Errorf("%d rows, want %d", n, tc.expected)
			}
		})
	}
}

// wsStatsCheck runs one row through goip against its capture.
func wsStatsCheck(t *testing.T, row wsStatsRow) {
	t.Helper()
	c, object, args, err := wsReadbackArgv(row.cmd)
	if err != nil {
		t.Fatal(err)
	}
	run := map[string]func(*runCtx, []string) error{"stats": runStats, "link": runLink}[object]
	if run == nil {
		t.Fatalf("object %s in %q", object, row.cmd)
	}
	pcap := filepath.Join(row.dir, "ip.pcap")
	src := &restoreReplay{linkReplay: newLinkReplay(t, pcap)}
	c.src = wsStatsReplay{src}
	var out, errOut bytes.Buffer
	c.out, c.errOut, c.lltab = &out, &errOut, NewLLTab()
	written := filepath.Join(t.TempDir(), "write.bin")
	t.Setenv(writeOutEnvCst, written)
	rc := ExitOK
	if err := run(c, args); err != nil {
		rc = exitCodeOf(err)
	}
	if rc != row.rc {
		t.Errorf("rc %d, want %d (%s)", rc, row.rc, strings.TrimSpace(errOut.String()))
	}
	if !bytes.Equal(out.Bytes(), row.out) {
		t.Errorf("stdout\n got  %q\n want %q", out.Bytes(), row.out)
	}
	reqs := slices.Concat(src.dumpReqs, src.gets)
	if writtenFile(written) {
		for _, w := range restoreWrites(t, written) {
			reqs = append(reqs, capturedRequest(w))
		}
	}
	slices.SortFunc(reqs, bytes.Compare)
	if ip := mplsIPRequests(t, pcap); !slices.EqualFunc(reqs, ip, bytes.Equal) {
		t.Errorf("requests\n goip %x\n ip   %x", reqs, ip)
	}
}
