package goip

import (
	"bytes"
	"os"
	"testing"
)

// The RTM_GETNEIGHTBL dumps. Each pcap holds the ll_init_map link dump ahead of
// the table dump (do_ipntable runs ll_init_map first), so a single GOIP_REPLAY
// serves both: NeighTableLinks reads the RTM_NEWLINK replies to fill the index
// cache, NeighTables reads the RTM_NEWNEIGHTBL replies. The `_s` pcaps back the
// `-s` goldens; their reply bytes match the plain form (the kernel always sends
// config/stats), but they are paired with their own sidecars captured at the
// same instant - the config timestamps are wall-clock.
const (
	ntableDumpPcap        = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getneightbl.pcap"
	ntableDumpPcapMesh    = "../../pkg/xtcpnl/testdata/7_1_4/dumps/mesh/netlink_route_getneightbl.pcap"
	ntableDumpPcapTunnel  = "../../pkg/xtcpnl/testdata/7_1_4/dumps/tunnel/netlink_route_getneightbl.pcap"
	ntableDumpPcapS       = "../../pkg/xtcpnl/testdata/7_1_4/dumps/netlink_route_getneightbl_s.pcap"
	ntableDumpPcapSMesh   = "../../pkg/xtcpnl/testdata/7_1_4/dumps/mesh/netlink_route_getneightbl_s.pcap"
	ntableDumpPcapSTunnel = "../../pkg/xtcpnl/testdata/7_1_4/dumps/tunnel/netlink_route_getneightbl_s.pcap"

	ntableSidecarDir = "../../pkg/xtcpnl/testdata/7_1_4/dumps/"
)

// The capture instants, recovered once from each committed `_s` pcap and its
// golden: now = golden_last_flush + ndtc_last_flush/1000, in UTC (the capture
// VM's zone). Injected via GOIP_NOW so `-s` replays reproduce the two wall-clock
// config dates byte-for-byte. The three topologies were captured seconds apart,
// so each carries its own instant.
const (
	ntableNowBase   = "2026-10-09T16:01:41Z"
	ntableNowMesh   = "2026-10-09T16:02:32Z"
	ntableNowTunnel = "2026-10-09T16:03:26Z"
)

// TestNeighTblShowMatchesCapturedSidecars replays the committed RTM_GETNEIGHTBL
// dumps and compares goip's output with the `ip ntable show` sidecars.
//
// Text rows are BYTE exact: the kernel dumps the table list in a stable order
// and ipntable does no sorting. The `-s` rows set GOIP_NOW to the capture
// instant so the config last_flush/last_rand dates reproduce exactly; every
// other `-s` field is a pure function of the bytes. The JSON row compares
// structurally against the `-j -p` sidecar.
//
// go test ./internal/goip/ -run TestNeighTblShowMatchesCapturedSidecars
func TestNeighTblShowMatchesCapturedSidecars(t *testing.T) {
	tests := []struct {
		description    string
		args           []string
		pcap           string
		sidecar        string
		now            string
		jsonEquivalent bool
	}{
		{
			description: "positive: the full table list renders byte-for-byte",
			args:        []string{"ntable", "show"},
			pcap:        ntableDumpPcap,
			sidecar:     "ip_ntable",
		},
		{
			description:    "positive: the JSON form equals the -j -p sidecar structurally",
			args:           []string{"-j", "ntable", "show"},
			pcap:           ntableDumpPcap,
			sidecar:        "ip_ntable_json",
			jsonEquivalent: true,
		},
		{
			description: "positive: `-s` adds the config and stats blocks, timestamps reproduced via GOIP_NOW",
			args:        []string{"-s", "ntable", "show"},
			pcap:        ntableDumpPcapS,
			sidecar:     "ip_ntable_s",
			now:         ntableNowBase,
		},
		{
			description: "corner: the verb abbreviation `lst` resolves to the listing",
			args:        []string{"ntable", "lst"},
			pcap:        ntableDumpPcap,
			sidecar:     "ip_ntable",
		},
		{
			description: "corner: a bare `ntable` is a listing, taking the no-verb path",
			args:        []string{"ntable"},
			pcap:        ntableDumpPcap,
			sidecar:     "ip_ntable",
		},
		{
			description: "corner: the `ntbl` object alias resolves identically",
			args:        []string{"ntbl", "show"},
			pcap:        ntableDumpPcap,
			sidecar:     "ip_ntable",
		},
		{
			description: "corner: the mesh namespace, an independently captured topology",
			args:        []string{"ntable", "show"},
			pcap:        ntableDumpPcapMesh,
			sidecar:     "mesh/ip_ntable",
		},
		{
			description: "corner: the mesh namespace under `-s`",
			args:        []string{"-s", "ntable", "show"},
			pcap:        ntableDumpPcapSMesh,
			sidecar:     "mesh/ip_ntable_s",
			now:         ntableNowMesh,
		},
		{
			description: "corner: the tunnel namespace, more devices than the base",
			args:        []string{"ntable", "show"},
			pcap:        ntableDumpPcapTunnel,
			sidecar:     "tunnel/ip_ntable",
		},
		{
			description: "corner: the tunnel namespace under `-s`",
			args:        []string{"-s", "ntable", "show"},
			pcap:        ntableDumpPcapSTunnel,
			sidecar:     "tunnel/ip_ntable_s",
			now:         ntableNowTunnel,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want, err := os.ReadFile(ntableSidecarDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOIP_REPLAY", tc.pcap)
			if tc.now != "" {
				t.Setenv("GOIP_NOW", tc.now)
			}
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			if tc.jsonEquivalent {
				assertJSONEntriesEqual(t, stdout.Bytes(), want, false)
				return
			}
			if !bytes.Equal(stdout.Bytes(), want) {
				t.Fatalf("output mismatch\n got: %q\nwant: %q", stdout.Bytes(), want)
			}
		})
	}
}

// TestNeighTblSidecarsAreDistinct keeps the comparison helper honest. Unlike the
// addrlabel table, the neighbor tables are NOT identical across topologies: the
// device-specific parameter sets differ by interface set (the tunnel namespace
// has a gre device the base does not), and the reachable_time values are
// per-table randomized. So the measured assertion here is non-identity, plus the
// one text/JSON negative every object carries.
//
// go test ./internal/goip/ -run TestNeighTblSidecarsAreDistinct
func TestNeighTblSidecarsAreDistinct(t *testing.T) {
	tests := []struct {
		description string
		a, b        string
		wantEqual   bool
	}{
		{
			description: "corner: the base and tunnel tables differ, the tunnel having an extra device",
			a:           "ip_ntable", b: "tunnel/ip_ntable", wantEqual: false,
		},
		{
			description: "negative: the text and JSON forms of the same table differ",
			a:           "ip_ntable", b: "ip_ntable_json", wantEqual: false,
		},
		{
			description: "negative: the plain and `-s` forms differ, `-s` adding config and stats",
			a:           "ip_ntable", b: "ip_ntable_s", wantEqual: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			a, err := os.ReadFile(ntableSidecarDir + tt.a)
			if err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(ntableSidecarDir + tt.b)
			if err != nil {
				t.Fatal(err)
			}
			if got := bytes.Equal(a, b); got != tt.wantEqual {
				t.Errorf("bytes.Equal(%s, %s) = %v, want %v", tt.a, tt.b, got, tt.wantEqual)
			}
		})
	}
}

// TestRunNTableArgs covers the CLI surface: the verbs runNTable accepts and the
// selectors it refuses. Refusing dev/name matters for the ntable reason -
// ipntable applies them client-side over an unfiltered dump, so a selector goip
// accepted would narrow nothing while claiming to.
//
// go test ./internal/goip/ -run TestRunNTableArgs
func TestRunNTableArgs(t *testing.T) {
	const firstLine = "inet arp_cache \n"

	tests := []struct {
		description      string
		args             []string
		wantCode         int
		wantStdoutPrefix string
		wantStderrSubstr string
	}{
		{
			description:      "positive: `ntable show` renders the first table",
			args:             []string{"ntable", "show"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			description:      "positive: `ntable list` is a synonym",
			args:             []string{"ntable", "list"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			description:      "positive: `ntable lst` is the third synonym",
			args:             []string{"ntable", "lst"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			description:      "positive: a bare `ntable` lists, taking the no-verb path",
			args:             []string{"ntable"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			description:      "positive: the `ntbl` object alias lists too",
			args:             []string{"ntbl"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			// "s" is a prefix of show alone (list/lst need "l"), so it resolves.
			description:      "boundary: `ntable s` abbreviates show",
			args:             []string{"ntable", "s"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			description:      "boundary: `ntable ls` abbreviates lst and not list",
			args:             []string{"ntable", "ls"},
			wantCode:         ExitOK,
			wantStdoutPrefix: firstLine,
		},
		{
			description:      "negative: the write verb `change` is refused, goip being read-only",
			args:             []string{"ntable", "change"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: `chg` is refused, the abbreviation of change",
			args:             []string{"ntable", "chg"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: `help` is refused",
			args:             []string{"ntable", "help"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// `ip ntable show dev DEV` is a client-side print filter; goip refuses
			// it rather than answer a filtered query with the whole table.
			description:      "negative: a `dev` selector is refused",
			args:             []string{"ntable", "show", "dev", "lo"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: a `name` selector is refused",
			args:             []string{"ntable", "show", "name", "arp_cache"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: an unknown verb is refused",
			args:             []string{"ntable", "frobnicate"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", ntableDumpPcap)
			var stdout, stderr bytes.Buffer
			if code := Run(tt.args, &stdout, &stderr); code != tt.wantCode {
				t.Fatalf("Run(%q) = %d, want %d; stderr=%s",
					tt.args, code, tt.wantCode, stderr.String())
			}
			if tt.wantStdoutPrefix != "" && !bytes.HasPrefix(stdout.Bytes(), []byte(tt.wantStdoutPrefix)) {
				t.Errorf("stdout = %q, want it to start with %q", stdout.String(), tt.wantStdoutPrefix)
			}
			if tt.wantStderrSubstr != "" && !bytes.Contains(stderr.Bytes(), []byte(tt.wantStderrSubstr)) {
				t.Errorf("stderr = %q, want it to contain %q", stderr.String(), tt.wantStderrSubstr)
			}
		})
	}
}
