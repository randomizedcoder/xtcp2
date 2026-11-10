package goip

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

// NX3b (goip-parity-next.md): `route show` and `route get` in AF_MPLS, held
// to what ip 7.2.0 sent and printed in every mpls row of the 7.3.1 corpus.

// wsMplsDir is the mpls rows' corpus.
var wsMplsDir = filepath.Join(wsCorpusDir731, "mpls")

// mplsReplayRun runs `route ARGS` in family over pcap's replies, and
// returns the exit code, stdout, and every request goip sent, seq and pid
// zeroed, sorted.
func mplsReplayRun(t *testing.T, pcap string, family uint8, jsonOut bool, args []string) (int, string, [][]byte) {
	t.Helper()
	src := &restoreReplay{linkReplay: newLinkReplay(t, pcap)}
	rc, stdout, _ := runRouteCtx(&runCtx{src: src, family: family, json: jsonOut}, args)
	reqs := slices.Concat(src.dumpReqs, src.gets)
	slices.SortFunc(reqs, bytes.Compare)
	return rc, stdout, reqs
}

// mplsIPRequests is every request ip sent in pcap, seq and pid zeroed,
// sorted.
func mplsIPRequests(t *testing.T, pcap string) [][]byte {
	t.Helper()
	ip := captureRequests(t, pcap)
	reqs := make([][]byte, 0, len(ip))
	for _, r := range ip {
		reqs = append(reqs, zeroSeqPidCopy(r))
	}
	slices.SortFunc(reqs, bytes.Compare)
	return reqs
}

// TestWsMplsShowReadbacks replays every mpls row's two readbacks, `ip -f
// mpls route show` and `ip -j -f mpls route show`, through goip: the
// requests are ip's, and stdout is what ip printed.
//
// go test ./internal/goip/ -run TestWsMplsShowReadbacks
func TestWsMplsShowReadbacks(t *testing.T) {
	useLLMap720(t) // the rows ran ip 7.2.0, whose lookups ask for names alone
	tests := []struct {
		description string
		name        string // the readback's name: its pcap and its .out
		json        bool
	}{
		{description: "positive: the text show", name: "show"},
		{description: "positive: the -j show", name: "showj", json: true},
	}
	ents, err := os.ReadDir(wsMplsDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var rows, lines int
			for _, e := range ents {
				dir := filepath.Join(wsMplsDir, e.Name())
				want, err := os.ReadFile(filepath.Join(dir, tc.name+".out"))
				if err != nil {
					t.Fatal(err)
				}
				pcap := filepath.Join(dir, "netlink_route_"+tc.name+".pcap")
				rc, got, reqs := mplsReplayRun(t, pcap, unix.AF_MPLS, tc.json, []string{"show"})
				if rc != ExitOK {
					t.Errorf("%s: exit %d", e.Name(), rc)
					continue
				}
				if got != string(want) {
					t.Errorf("%s: stdout\n got  %q\n want %q", e.Name(), got, want)
				}
				if ip := mplsIPRequests(t, pcap); !slices.EqualFunc(reqs, ip, bytes.Equal) {
					t.Errorf("%s: requests\n goip %x\n ip   %x", e.Name(), reqs, ip)
				}
				rows++
				lines += strings.Count(string(want), "\n")
			}
			t.Logf("%d rows' readbacks, %d lines", rows, lines)
			if rows == 0 || lines == 0 {
				t.Error("compared nothing")
			}
		})
	}
}

// TestWsMplsGet replays the get rows through goip: the request is ip's,
// and the reply prints as ip printed it, or the kernel's refusal exits 2
// with nothing on stdout.
//
// go test ./internal/goip/ -run TestWsMplsGet
func TestWsMplsGet(t *testing.T) {
	useLLMap720(t)
	tests := []struct {
		description string
		row         string
		family      uint8
		args        []string
		expected    int // goip's exit, and ip's rc
	}{
		{description: "positive: a label's route", row: "get", family: unix.AF_MPLS,
			args: []string{"get", "100"}, expected: ExitOK},
		{description: "positive: as adds RTA_NEWDST, which changes nothing the kernel answers", row: "get-as",
			family: unix.AF_MPLS, args: []string{"get", "100", "as", "200"}, expected: ExitOK},
		{description: "negative: a label with no route is ENETUNREACH", row: "get-absent", family: unix.AF_MPLS,
			args: []string{"get", "101"}, expected: ExitFailure},
		{description: "negative: as on an IPv4 get is the kernel's EINVAL", row: "inet-get-as",
			args: []string{"get", "10.0.0.2", "as", "200"}, expected: ExitFailure},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dir := filepath.Join(wsMplsDir, tc.row)
			b, err := os.ReadFile(filepath.Join(dir, "cmd.json"))
			if err != nil {
				t.Fatal(err)
			}
			var row wsRowFile
			if err := json.Unmarshal(b, &row); err != nil {
				t.Fatal(err)
			}
			if row.Result.RC != tc.expected {
				t.Fatalf("%s: ip's rc is %d, not %d", tc.row, row.Result.RC, tc.expected)
			}
			want, err := os.ReadFile(filepath.Join(dir, "ip.out"))
			if err != nil {
				t.Fatal(err)
			}
			pcap := filepath.Join(dir, "ip.pcap")
			rc, got, reqs := mplsReplayRun(t, pcap, tc.family, false, tc.args)
			if rc != tc.expected {
				t.Errorf("exit %d, want %d", rc, tc.expected)
			}
			if got != string(want) {
				t.Errorf("stdout\n got  %q\n want %q", got, want)
			}
			if ip := mplsIPRequests(t, pcap); !slices.EqualFunc(reqs, ip, bytes.Equal) {
				t.Errorf("requests\n goip %x\n ip   %x", reqs, ip)
			}
		})
	}
}
