package goip

import (
	"bytes"
	"os"
	"testing"
)

// The RTM_GETNETCONF captures. Each pcap holds the ll_init_map link dump ahead of
// the netconf transaction (do_ipnetconf runs ll_init_map first), so a single
// GOIP_REPLAY serves both: NetconfLinks reads the RTM_NEWLINK replies to fill the
// index cache, then the netconf path reads the RTM_NEWNETCONF replies. The _dev4
// pcap holds the point-get reply; under replay (no Talk) NetconfByIndex dumps it
// and filters to the ifindex, the NexthopByID precedent.
const (
	netconfDir = "../../pkg/xtcpnl/testdata/7_1_4/dumps/"

	netconfPcapBase       = netconfDir + "netlink_route_getnetconf.pcap"
	netconfPcapBaseDev    = netconfDir + "netlink_route_getnetconf_dev.pcap"
	netconfPcapBaseDev4   = netconfDir + "netlink_route_getnetconf_dev4.pcap"
	netconfPcapMesh       = netconfDir + "mesh/netlink_route_getnetconf.pcap"
	netconfPcapMeshDev    = netconfDir + "mesh/netlink_route_getnetconf_dev.pcap"
	netconfPcapMeshDev4   = netconfDir + "mesh/netlink_route_getnetconf_dev4.pcap"
	netconfPcapTunnel     = netconfDir + "tunnel/netlink_route_getnetconf.pcap"
	netconfPcapTunnelDev  = netconfDir + "tunnel/netlink_route_getnetconf_dev.pcap"
	netconfPcapTunnelDev4 = netconfDir + "tunnel/netlink_route_getnetconf_dev4.pcap"
)

// TestNetconfShowMatchesCapturedSidecars replays the committed RTM_GETNETCONF
// captures and compares goip's output with the `ip netconf show` sidecars.
//
// Text rows are BYTE exact: the kernel dumps the records in a stable order and
// ipnetconf does no sorting. The `dev` rows are the dump-plus-client-filter form,
// the `-4 dev` rows the point get; the JSON row compares structurally against the
// `-j -p` sidecar. netconf renders no wall-clock field, so no GOIP_NOW.
//
// go test ./internal/goip/ -run TestNetconfShowMatchesCapturedSidecars
func TestNetconfShowMatchesCapturedSidecars(t *testing.T) {
	tests := []struct {
		description    string
		args           []string
		pcap           string
		sidecar        string
		jsonEquivalent bool
	}{
		{
			description: "positive: base, the full per-family list renders byte-for-byte",
			args:        []string{"netconf", "show"},
			pcap:        netconfPcapBase,
			sidecar:     "ip_netconf",
		},
		{
			description:    "positive: base JSON equals the -j -p sidecar structurally",
			args:           []string{"-j", "netconf", "show"},
			pcap:           netconfPcapBase,
			sidecar:        "ip_netconf_json",
			jsonEquivalent: true,
		},
		{
			description: "positive: base `show dev goip0` is the dump filtered to the ifindex",
			args:        []string{"netconf", "show", "dev", "goip0"},
			pcap:        netconfPcapBaseDev,
			sidecar:     "ip_netconf_dev",
		},
		{
			description: "positive: base `-4 show dev goip0` is the point get",
			args:        []string{"-4", "netconf", "show", "dev", "goip0"},
			pcap:        netconfPcapBaseDev4,
			sidecar:     "ip_netconf_dev4",
		},
		{
			description: "corner: mesh namespace, the full list",
			args:        []string{"netconf", "show"},
			pcap:        netconfPcapMesh,
			sidecar:     "mesh/ip_netconf",
		},
		{
			description:    "corner: mesh JSON",
			args:           []string{"-j", "netconf", "show"},
			pcap:           netconfPcapMesh,
			sidecar:        "mesh/ip_netconf_json",
			jsonEquivalent: true,
		},
		{
			description: "corner: mesh `show dev veth0`",
			args:        []string{"netconf", "show", "dev", "veth0"},
			pcap:        netconfPcapMeshDev,
			sidecar:     "mesh/ip_netconf_dev",
		},
		{
			description: "corner: mesh `-4 show dev veth0` point get",
			args:        []string{"-4", "netconf", "show", "dev", "veth0"},
			pcap:        netconfPcapMeshDev4,
			sidecar:     "mesh/ip_netconf_dev4",
		},
		{
			description: "corner: tunnel namespace, the full list",
			args:        []string{"netconf", "show"},
			pcap:        netconfPcapTunnel,
			sidecar:     "tunnel/ip_netconf",
		},
		{
			description:    "corner: tunnel JSON",
			args:           []string{"-j", "netconf", "show"},
			pcap:           netconfPcapTunnel,
			sidecar:        "tunnel/ip_netconf_json",
			jsonEquivalent: true,
		},
		{
			description: "corner: tunnel `show dev gre1`",
			args:        []string{"netconf", "show", "dev", "gre1"},
			pcap:        netconfPcapTunnelDev,
			sidecar:     "tunnel/ip_netconf_dev",
		},
		{
			description: "corner: tunnel `-4 show dev gre1` point get",
			args:        []string{"-4", "netconf", "show", "dev", "gre1"},
			pcap:        netconfPcapTunnelDev4,
			sidecar:     "tunnel/ip_netconf_dev4",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want, err := os.ReadFile(netconfDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOIP_REPLAY", tc.pcap)
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

// TestNetconfSidecarsAreDistinct keeps the comparison helper honest: the three
// topologies carry different interface sets, and the text and JSON forms differ.
//
// go test ./internal/goip/ -run TestNetconfSidecarsAreDistinct
func TestNetconfSidecarsAreDistinct(t *testing.T) {
	tests := []struct {
		description string
		a, b        string
		wantEqual   bool
	}{
		{
			description: "corner: the base and tunnel lists differ, the tunnel having a gre device",
			a:           "ip_netconf", b: "tunnel/ip_netconf", wantEqual: false,
		},
		{
			description: "negative: the text and JSON forms of the same list differ",
			a:           "ip_netconf", b: "ip_netconf_json", wantEqual: false,
		},
		{
			description: "corner: the full list and the dev-filtered list differ",
			a:           "ip_netconf", b: "ip_netconf_dev", wantEqual: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			a, err := os.ReadFile(netconfDir + tt.a)
			if err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(netconfDir + tt.b)
			if err != nil {
				t.Fatal(err)
			}
			if got := bytes.Equal(a, b); got != tt.wantEqual {
				t.Errorf("bytes.Equal(%s, %s) = %v, want %v", tt.a, tt.b, got, tt.wantEqual)
			}
		})
	}
}

// TestRunNetconfArgs covers the CLI surface: the verbs runNetconf accepts, the
// `dev` selector it implements, and the write/help verbs and bad selectors it
// refuses. `dev` is strcmp'd (ip/ipnetconf.c:173), so it does not abbreviate.
//
// go test ./internal/goip/ -run TestRunNetconfArgs
func TestRunNetconfArgs(t *testing.T) {
	tests := []struct {
		description      string
		args             []string
		wantCode         int
		wantStdoutPrefix string
		wantStderrSubstr string
	}{
		{
			description:      "positive: a bare `netconf` lists, taking the no-verb path",
			args:             []string{"netconf"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "inet ",
		},
		{
			description:      "positive: `netconf show`",
			args:             []string{"netconf", "show"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "inet ",
		},
		{
			description:      "boundary: `netconf sh` abbreviates show",
			args:             []string{"netconf", "sh"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "inet ",
		},
		{
			description:      "positive: `netconf list` is a synonym",
			args:             []string{"netconf", "list"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "inet ",
		},
		{
			description:      "positive: `netconf show dev goip0` filters to the device",
			args:             []string{"netconf", "show", "dev", "goip0"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "inet goip0 ",
		},
		{
			description:      "negative: `netconf help` is refused, goip being read-only",
			args:             []string{"netconf", "help"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: an unknown verb is refused",
			args:             []string{"netconf", "frobnicate"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: `netconf show dev` with no name is refused",
			args:             []string{"netconf", "show", "dev"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: the write verb `change` is refused",
			args:             []string{"netconf", "change"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", netconfPcapBase)
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
