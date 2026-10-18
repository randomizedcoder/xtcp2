package goip

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunOutputModes covers -br and -o once an object is named (A3-T01).
//
// They parse, and that is all so far: `addr show` refuses them as not yet
// rendered (A3-T02 writes the renderers), every other object refuses them by
// name, and an address write ignores them as ip does. Each refusal must come
// before any request, so a row that reaches the replay and prints the full
// listing has found a dropped flag.
//
// GOIP_REPLAY is the addr-show capture for the read rows, so a missing refusal
// renders and fails on stdout rather than on the replay. The write row replays
// add_v4, whose capture answers the `dev d0` lookup.
//
// go test ./internal/goip/ -run TestRunOutputModes
func TestRunOutputModes(t *testing.T) {
	type result struct {
		code      int
		stderr    string // a fragment that must appear
		writeSlug string // the slug whose write GOIP_WRITE_OUT must hold; "" for no file
	}
	const (
		notRendered = "render not implemented"
		addrOnly    = "is only implemented for address"
		unknown     = "is unknown"
	)
	showPcap := filepath.Join(linkWriteDir, "addr-show", "mixed_links", "netlink_route_getaddr.pcap")
	addPcap := filepath.Join(addrWriteCorpusDir, "add_v4", "ip.pcap")
	tests := []struct {
		description string
		pcap        string
		args        string
		expected    result
	}{
		{description: "positive: -br with addr show is accepted and refused as not yet rendered",
			pcap: showPcap, args: "-br addr show",
			expected: result{code: ExitUsage, stderr: "-brief " + notRendered}},
		{description: "positive: -o with addr show is accepted and refused as not yet rendered",
			pcap: showPcap, args: "-o addr show",
			expected: result{code: ExitUsage, stderr: "-oneline " + notRendered}},
		{description: "corner: -br and -j together are accepted, and brief is the one named",
			pcap: showPcap, args: "-br -j addr show",
			expected: result{code: ExitUsage, stderr: "-brief " + notRendered}},
		{description: "corner: the abbreviation a with no verb is address show",
			pcap: showPcap, args: "-br a",
			expected: result{code: ExitUsage, stderr: "-brief " + notRendered}},
		{description: "negative: -br with link is refused for the object",
			pcap: showPcap, args: "-br link show",
			expected: result{code: ExitUsage, stderr: "-brief " + addrOnly}},
		{description: "negative: -o with route is refused for the object",
			pcap: showPcap, args: "-o route show",
			expected: result{code: ExitUsage, stderr: "-oneline " + addrOnly}},
		{description: "negative: -br with neigh, which ip renders but goip does not claim",
			pcap: showPcap, args: "-br neigh show",
			expected: result{code: ExitUsage, stderr: "-brief " + addrOnly}},
		{description: "corner: -br is ignored by an address write, as in ip",
			pcap: addPcap, args: "-br addr add 192.0.2.10/24 dev d0",
			expected: result{code: ExitOK, writeSlug: "add_v4"}},
		{description: "boundary: -b with addr is unknown, not brief",
			pcap: showPcap, args: "-b addr show",
			expected: result{code: ExitUsage, stderr: unknown}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", tc.pcap)
			out := filepath.Join(t.TempDir(), "write.bin")
			t.Setenv(writeOutEnvCst, out)
			var stdout, stderr bytes.Buffer
			code := Run(strings.Fields(tc.args), &stdout, &stderr)
			if code != tc.expected.code {
				t.Errorf("code = %d, want %d; stderr %q", code, tc.expected.code, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("stdout = %q, want nothing", stdout.String())
			}
			if !strings.Contains(stderr.String(), tc.expected.stderr) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tc.expected.stderr)
			}
			if tc.expected.stderr != unknown && strings.Contains(stderr.String(), unknown) {
				t.Errorf("stderr = %q, want the option accepted", stderr.String())
			}
			got, rerr := os.ReadFile(out)
			switch {
			case tc.expected.writeSlug == "" && rerr == nil:
				t.Errorf("wrote %x, want no file", got)
			case tc.expected.writeSlug != "":
				if want := addrSlugWrite(t, tc.expected.writeSlug); !bytes.Equal(zeroSeqPidCopy(got), want) {
					t.Errorf("wrote %x (%v)\nwant  %x", zeroSeqPidCopy(got), rerr, want)
				}
			}
		})
	}
}
