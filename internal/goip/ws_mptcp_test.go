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
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// wsMptcpDir is the workbench's mptcp space (genl-clones 4C): `ip mptcp`'s
// endpoint and limits shows, captured on generic+route, so each pcap holds
// the mptcp_pm exchanges and the RTM_GETLINK dump ip's ll_init_map made.
const wsMptcpDir = "../../pkg/xtcpnl/testdata/7_3_1/ws/mptcp"

// wsMptcpSrc is a row's replay, with the route dumps goip asked for, seq
// and pid zeroed, kept to hold against ip's. It answers no single get:
// ip names an endpoint's dev from its link map, never by RTM_GETLINK get.
type wsMptcpSrc struct {
	*ReplaySource
	routeReqs [][]byte
}

func (s *wsMptcpSrc) Dump(req []byte, typ uint16) ([][]byte, error) {
	s.routeReqs = append(s.routeReqs, zeroSeqPidCopy(req))
	return s.ReplaySource.Dump(req, typ)
}

// wsMptcpResult is one row replayed through goip.
type wsMptcpResult struct {
	rc             int
	stdout, stderr string
	writes         []nlparity.Msg
	src            *wsMptcpSrc
}

// wsMptcpRun replays the row in dir: `goip [-j] mptcp WORDS` over the
// row's own pcap, both protocols of it. stderr is what goip prints, its
// refusal included.
func wsMptcpRun(t *testing.T, dir, cmd string) wsMptcpResult {
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
	if len(f) == 0 || f[0] != "mptcp" {
		t.Fatalf("not an ip mptcp command: %q", cmd)
	}
	rs, err := OpenReplay(filepath.Join(dir, "ip.pcap"))
	if err != nil {
		t.Fatal(err)
	}
	src := &wsMptcpSrc{ReplaySource: rs}
	out := filepath.Join(t.TempDir(), "write.bin")
	t.Setenv(writeOutEnvCst, out)
	var stdout, stderr bytes.Buffer
	c := newRunCtx(nil, &stdout, &stderr)
	c.src, c.json = src, jsonOut
	rerr := runMptcp(c, f[1:])
	if rerr != nil {
		stderr.WriteString("goip: " + rerr.Error() + "\n")
	}
	r := wsMptcpResult{rc: ioamExit(rerr), stdout: stdout.String(), stderr: stderr.String(), src: src}
	if _, serr := os.Stat(out); serr == nil {
		r.writes = genlWrites(t, out, xtcpnl.MPTCPPMGenlName)
	}
	return r
}

// TestWsMptcpCorpus replays every row of the mptcp ws space through goip
// (genl-clones 4R): a show prints ip's stdout byte for byte, the -j
// limits' text included, and nothing on stderr; every row exits as ip
// did; goip makes ip's genl exchanges, every one of them and no write, and
// ip's route requests, the ll_init_map link dump, byte for byte with seq
// and pid zeroed. A refusal is goip's own text, ip's message inside it
// (without its final full stop), but for the kernel's extack, which goip
// does not print: it names the errno.
//
// go test ./internal/goip/ -run TestWsMptcpCorpus
func TestWsMptcpCorpus(t *testing.T) {
	useLLMap720(t) // the rows ran ip 7.2.0, whose link dump asks for names alone
	tests := []struct {
		description string
		row         string
		expected    string // "" is ip's stdout, empty stderr; else a fragment of goip's stderr
	}{
		{description: "boundary: no endpoints, NLMSG_DONE alone and no link dump", row: "ec-001"},
		{description: "boundary: -j no endpoints, an empty array", row: "pw-002"},
		{description: "positive: one endpoint, the link dump on its print", row: "pw-001"},
		{description: "positive: -j one endpoint", row: "ec-002"},
		{description: "positive: four endpoints, dev, port and flags in table order", row: "ec-003"},
		{description: "positive: -j four endpoints", row: "pw-003"},
		{description: "positive: one endpoint by id, the link dump first", row: "ec-004"},
		{description: "positive: -j one endpoint by id, an array of one", row: "pw-004"},
		{description: "corner: bare endpoint is the dump", row: "ec-005"},
		{description: "corner: -j bare endpoint", row: "pw-005"},
		{description: "boundary: the namespace's default limits", row: "ec-006"},
		{description: "corner: -j default limits print text", row: "pw-006"},
		{description: "positive: limits set in pre, read back", row: "ec-007"},
		{description: "corner: -j limits set in pre, text", row: "pw-007"},
		{description: "corner: bare limits is show", row: "ec-008"},
		{description: "corner: -j bare limits, text", row: "pw-008"},
		{description: "corner: limits show with a set's attribute, the same limits", row: "ec-009"},
		{description: "corner: -j limits show with a set's attribute, text", row: "pw-009"},
		{description: "negative: help, usage before the lookup", row: "ng-001", expected: "Usage:\tip mptcp endpoint add ADDRESS"},
		{description: "negative: an unknown object after the lookup", row: "ng-002",
			expected: `Command "badobj" is unknown, try "ip mptcp help"`},
		{description: "negative: an unknown endpoint sub-command", row: "ng-003",
			expected: `Command "junk" is unknown, try "ip mptcp help"`},
		{description: "negative: show's word is no address, get_addr's exit 1", row: "ng-004",
			expected: `Error: any valid address is expected rather than "junk"`},
		{description: "boundary: id 0 is only for delete", row: "ng-005", expected: `Error: argument "ID" is wrong: cannot be 0`},
		{description: "negative: the kernel has no endpoint 9, EINVAL (ip prints its extack)", row: "ng-006",
			expected: "invalid argument"},
		{description: "negative: limits show's word is no limit", row: "ng-007",
			expected: `Error: argument "junk" is wrong: unknown limit`},
		{description: "corner: limits has no unknown branch of its own", row: "ng-008",
			expected: `Command "junk" is unknown, try "ip mptcp help"`},
	}
	ents, err := os.ReadDir(wsMptcpDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ents) != len(tests) {
		t.Errorf("%d rows on disk, %d in the table", len(ents), len(tests))
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := filepath.Join(wsMptcpDir, tc.row)
			var row wsRowFile
			if err := json.Unmarshal([]byte(mustReadFile(t, filepath.Join(dir, "cmd.json"))), &row); err != nil {
				t.Fatal(err)
			}
			r := wsMptcpRun(t, dir, row.Cmd)
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
