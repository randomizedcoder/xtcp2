package goip

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunOutputModes covers -br and -o once an object is named (A3-T01,
// A3-T02).
//
// `addr show` renders -br, -br -j and -o, and the rendered rows compare
// stdout with the mixed_links sidecar of the same form. Every form with no
// committed capture is refused as not rendered, every other object refuses
// them by name, and an address write ignores them as ip does. Each refusal
// must come before any request, so a row that reaches the replay and prints a
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
		sidecar   string // the mixed_links sidecar stdout must equal; "" for no output
	}
	const (
		notRendered = "render not implemented"
		addrOnly    = "is only implemented for address"
		unknown     = "is unknown"
	)
	mixed := filepath.Join(linkWriteDir, "addr-show", "mixed_links")
	showPcap := filepath.Join(mixed, "netlink_route_getaddr.pcap")
	briefPcap := filepath.Join(mixed, "netlink_route_getaddr_br.pcap")
	briefJSONPcap := filepath.Join(mixed, "netlink_route_getaddr_br_json.pcap")
	onelinePcap := filepath.Join(mixed, "netlink_route_getaddr_o.pcap")
	addPcap := filepath.Join(addrWriteCorpusDir, "add_v4", "ip.pcap")
	tests := []struct {
		description string
		pcap        string
		args        string
		expected    result
	}{
		{description: "positive: -br addr show renders the brief listing",
			pcap: briefPcap, args: "-br addr show",
			expected: result{code: ExitOK, sidecar: "ip_addr_br"}},
		{description: "positive: -o addr show renders one line per address",
			pcap: onelinePcap, args: "-o addr show",
			expected: result{code: ExitOK, sidecar: "ip_addr_o"}},
		{description: "corner: -br and -j together render the brief JSON",
			pcap: briefJSONPcap, args: "-br -j addr show",
			expected: result{code: ExitOK, sidecar: "ip_addr_br_json"}},
		{description: "corner: the abbreviation a with no verb is address show",
			pcap: briefPcap, args: "-br a",
			expected: result{code: ExitOK, sidecar: "ip_addr_br"}},
		{description: "negative: -o with -j has no capture, so it is refused",
			pcap: showPcap, args: "-o -j addr show",
			expected: result{code: ExitUsage, stderr: "-oneline with -json " + notRendered}},
		{description: "negative: -br and -o together are refused",
			pcap: showPcap, args: "-br -o addr show",
			expected: result{code: ExitUsage, stderr: "-brief with -oneline " + notRendered}},
		{description: "negative: -br with a selector is refused",
			pcap: showPcap, args: "-br addr show up",
			expected: result{code: ExitUsage, stderr: "-brief with a selector " + notRendered}},
		{description: "negative: -o with dev is refused",
			pcap: showPcap, args: "-o addr show dev d0",
			expected: result{code: ExitUsage, stderr: "-oneline with a selector " + notRendered}},
		{description: "negative: -br with -s is refused",
			pcap: showPcap, args: "-br -s addr show",
			expected: result{code: ExitUsage, stderr: "-brief with -stats " + notRendered}},
		{description: "negative: -o with -d is refused",
			pcap: showPcap, args: "-o -d addr show",
			expected: result{code: ExitUsage, stderr: "-oneline with -details " + notRendered}},
		{description: "negative: -br with -0 is refused",
			pcap: showPcap, args: "-br -0 addr show",
			expected: result{code: ExitUsage, stderr: "-brief with -0 " + notRendered}},
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
			var want []byte
			if tc.expected.sidecar != "" {
				var err error
				if want, err = os.ReadFile(filepath.Join(mixed, tc.expected.sidecar)); err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Equal(stdout.Bytes(), want) {
				t.Errorf("stdout = %q, want %q", stdout.String(), want)
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
