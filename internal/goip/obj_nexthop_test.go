package goip

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

// The nexthop capture is the gated microVM's one-command-clean pcap:
//
//	netlink_route_getnexthop.pcap   `ip nexthop show`  (ip_nexthop sidecar)
//	                                `ip -d nexthop show` (ip_nexthop_n sidecar)
//
// Like the route captures and for the same reason, these replays set
// GOIP_REPLAY and NOT GOIP_REPLAY_PORTID: the RTM_GETNEXTHOP dump runs on the
// main socket, and the lazy ll_index_to_name for nh_oif=3 opens a fresh socket
// whose RTM_NEWLINK reply carries a different portid (print_rta_ifidx ->
// ll_index_to_name -> ll_link_get, ip/iproute.c:443). Pinning the dump's portid
// would hide that link reply and the `dev goip0` token with it; the capture is
// single-command clean, so replaying the whole file is correct.
const (
	nexthopDumpPcap   = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getnexthop.pcap"
	nexthopSidecarDir = "../../pkg/xtcpnl/testdata/7_1_4/dumps/"
	nexthopSidecar    = "ip_nexthop"
	nexthopSidecarDtl = "ip_nexthop_n"
)

// TestNexthopShowMatchesCapturedSidecars replays the one committed nexthop dump
// and compares goip's stdout with the `ip nexthop show` and `ip -d nexthop
// show` sidecars captured beside it.
//
// The only object in the capture is the clean topology's nexthop id 1, a simple
// via/dev/link-scope shape. The plain and `-d` rows differ by exactly the
// show_details gate: proto unspec appears only under `-d` (ip/ipnexthop.c), and
// nothing else moves, which is the whole claim the two rows make together. The
// bare `nexthop` row proves do_ipnh's argc==0 path lists rather than erroring
// (ip/ipnexthop.c:1453), and `list`/`lst` prove the verb synonyms.
//
// go test ./internal/goip/ -run TestNexthopShowMatchesCapturedSidecars
func TestNexthopShowMatchesCapturedSidecars(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		sidecar     string
	}{
		{
			description: "positive: `nexthop show` reproduces ip_nexthop, trailing space included",
			args:        []string{"nexthop", "show"},
			sidecar:     nexthopSidecar,
		},
		{
			// do_ipnh lists when argc is 0 (ip/ipnexthop.c:1453), so a bare
			// object must not be a usage error.
			description: "positive: a bare `nexthop` is a list",
			args:        []string{"nexthop"},
			sidecar:     nexthopSidecar,
		},
		{
			description: "positive: `nexthop list` is the same listing (matches() synonym)",
			args:        []string{"nexthop", "list"},
			sidecar:     nexthopSidecar,
		},
		{
			description: "positive: `nexthop lst` is the same listing (the third synonym)",
			args:        []string{"nexthop", "lst"},
			sidecar:     nexthopSidecar,
		},
		{
			description: "positive: `-d nexthop show` reproduces ip_nexthop_n, which adds proto unspec",
			args:        []string{"-d", "nexthop", "show"},
			sidecar:     nexthopSidecarDtl,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want, err := os.ReadFile(nexthopSidecarDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOIP_REPLAY", nexthopDumpPcap)
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			if !bytes.Equal(stdout.Bytes(), want) {
				t.Fatalf("output mismatch\n got: %q\nwant: %q", stdout.Bytes(), want)
			}
		})
	}
}

// TestNexthopShowRefusals pins the shapes goip declines rather than guesses: the
// write and single-get verbs, the dump selectors no capture exercises, and
// `-json` for which there is no sidecar. Each is ErrNotImplemented, so Run exits
// ExitUsage and prints nothing to stdout.
//
// go test ./internal/goip/ -run TestNexthopShowRefusals
func TestNexthopShowRefusals(t *testing.T) {
	tests := []struct {
		description string
		args        []string
	}{
		{
			description: "negative: `nexthop add` is a write verb goip does not implement",
			args:        []string{"nexthop", "add"},
		},
		{
			description: "negative: `nexthop get` is a single-get verb with no captured shape",
			args:        []string{"nexthop", "get"},
		},
		{
			description: "negative: `nexthop flush` is a write verb",
			args:        []string{"nexthop", "flush"},
		},
		{
			description: "negative: `nexthop show dev goip0` is a filtered dump no capture grounds",
			args:        []string{"nexthop", "show", "dev", "goip0"},
		},
		{
			description: "negative: `nexthop show id 1` is a filtered dump no capture grounds",
			args:        []string{"nexthop", "show", "id", "1"},
		},
		{
			description: "negative: `-json nexthop show` has no captured sidecar to reproduce",
			args:        []string{"-json", "nexthop", "show"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", nexthopDumpPcap)
			var stdout, stderr bytes.Buffer
			code := Run(tc.args, &stdout, &stderr)
			if code != ExitUsage {
				t.Fatalf("Run(%q) = %d, want ExitUsage (%d); stderr=%s", tc.args, code, ExitUsage, stderr.String())
			}
			if stdout.Len() != 0 {
				t.Errorf("Run(%q) printed to stdout: %q", tc.args, stdout.String())
			}
			if !strings.Contains(stderr.String(), "not implemented") {
				t.Errorf("Run(%q) stderr = %q, want an ErrNotImplemented message", tc.args, stderr.String())
			}
		})
	}
}
