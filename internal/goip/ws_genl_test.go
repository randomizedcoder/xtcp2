package goip

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
)

// wsGenlSrc is a ws row's replay, with the route dumps goip asked for,
// seq and pid zeroed, kept to hold against ip's. It answers no single
// get: ip names a dev from its link map, never by RTM_GETLINK get.
type wsGenlSrc struct {
	*ReplaySource
	routeReqs [][]byte
}

func (s *wsGenlSrc) Dump(req []byte, typ uint16) ([][]byte, error) {
	s.routeReqs = append(s.routeReqs, zeroSeqPidCopy(req))
	return s.ReplaySource.Dump(req, typ)
}

// wsGenlResult is one ws row replayed through goip.
type wsGenlResult struct {
	rc             int
	stdout, stderr string
	writes         []nlparity.Msg
	src            *wsGenlSrc
}

// wsGenlRun replays the row in dir: `goip [-j] OBJ WORDS` through run,
// over the row's own pcap, every protocol of it. stderr is what goip
// prints, its refusal included; writes are the family's, from
// GOIP_WRITE_OUT.
func wsGenlRun(t *testing.T, dir, cmd, obj, family string, run func(*runCtx, []string) error) wsGenlResult {
	t.Helper()
	f := strings.Fields(cmd)
	if len(f) < 2 || f[0] != "ip" {
		t.Fatalf("not an ip command: %q", cmd)
	}
	f = f[1:]
	jsonOut := f[0] == "-j"
	if jsonOut {
		f = f[1:]
	}
	if len(f) == 0 || f[0] != obj {
		t.Fatalf("not an ip %s command: %q", obj, cmd)
	}
	rs, err := OpenReplay(filepath.Join(dir, "ip.pcap"))
	if err != nil {
		t.Fatal(err)
	}
	src := &wsGenlSrc{ReplaySource: rs}
	out := filepath.Join(t.TempDir(), "write.bin")
	t.Setenv(writeOutEnvCst, out)
	var stdout, stderr bytes.Buffer
	c := newRunCtx(nil, &stdout, &stderr)
	c.src, c.json = src, jsonOut
	rerr := run(c, f[1:])
	if rerr != nil {
		stderr.WriteString("goip: " + rerr.Error() + "\n")
	}
	r := wsGenlResult{rc: ioamExit(rerr), stdout: stdout.String(), stderr: stderr.String(), src: src}
	if _, serr := os.Stat(out); serr == nil {
		r.writes = genlWrites(t, out, family)
	}
	return r
}

// wsGenlCase is one row of a ws space's goip table.
type wsGenlCase struct {
	description string
	row         string
	expected    string // "" is ip's stdout, empty stderr; else a fragment of goip's stderr
}

// wsGenlCheck replays every case of the ws space in spaceDir and holds
// goip to ip: the table names every row on disk; each exits as ip did,
// prints ip's stdout, and ip's empty stderr or a refusal holding the
// case's fragment; makes ip's genl exchanges, every one and no write; and
// ip's route requests, byte for byte with seq and pid zeroed.
func wsGenlCheck(t *testing.T, spaceDir, obj, family string, run func(*runCtx, []string) error, tests []wsGenlCase) {
	t.Helper()
	ents, err := os.ReadDir(spaceDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != len(tests) {
		t.Errorf("%d rows on disk, %d in the table", len(ents), len(tests))
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := filepath.Join(spaceDir, tc.row)
			var row wsRowFile
			if err := json.Unmarshal([]byte(mustReadFile(t, filepath.Join(dir, "cmd.json"))), &row); err != nil {
				t.Fatal(err)
			}
			r := wsGenlRun(t, dir, row.Cmd, obj, family, run)
			if r.rc != row.Result.RC {
				t.Fatalf("%s: exit %d, ip's %d\nstderr %q", row.Cmd, r.rc, row.Result.RC, r.stderr)
			}
			if stdout := mustReadFile(t, filepath.Join(dir, "ip.out")); r.stdout != stdout {
				t.Errorf("%s: stdout\n got %q\nwant %q", row.Cmd, r.stdout, stdout)
			}
			if tc.expected == "" {
				if r.stderr != "" || mustReadFile(t, filepath.Join(dir, "ip.err")) != "" {
					t.Errorf("%s: stderr %q, ip.err must be empty and goip's too", row.Cmd, r.stderr)
				}
			} else if !strings.Contains(r.stderr, tc.expected) {
				t.Errorf("%s: stderr %q lacks %q", row.Cmd, r.stderr, tc.expected)
			}
			ioamCheckExchanges(t, r.src.ReplaySource, r.writes)
			ipReqs := mplsIPRequests(t, filepath.Join(dir, "ip.pcap"))
			got := slices.Clone(r.src.routeReqs)
			slices.SortFunc(got, bytes.Compare)
			if !slices.EqualFunc(got, ipReqs, bytes.Equal) {
				t.Errorf("%s: route requests\n got %x\nwant %x", row.Cmd, got, ipReqs)
			}
		})
	}
}
