package goip

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"golang.org/x/sys/unix"
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

// TestNetconfDevRequestsMatchIpBytes is do_show's request sequence for a dev
// (ip/ipnetconf.c:173-203): ll_name_to_index's RTM_GETLINK by name on the
// still-empty cache, ll_init_map's link dump, then the netconf dump or, under
// an explicit family, the point get. Every request is byte for byte what ip
// sent in the capture. G0-T03 added the get by name, which goip had answered
// from its own link dump, so `netconf show dev` sends ip's transaction count.
//
// go test ./internal/goip/ -run TestNetconfDevRequestsMatchIpBytes
func TestNetconfDevRequestsMatchIpBytes(t *testing.T) {
	const (
		getlink    = 18 // RTM_GETLINK
		getnetconf = 82 // RTM_GETNETCONF
	)
	tests := []struct {
		description string
		pcap        string
		family      uint8
		dev         string
		expected    []uint16 // request types, in order
	}{
		{"positive: base `show dev`, the get by name, the link dump, the netconf dump",
			netconfPcapBaseDev, unix.AF_UNSPEC, "goip0", []uint16{getlink, getlink, getnetconf}},
		{"positive: base `-4 show dev`, the point get last",
			netconfPcapBaseDev4, unix.AF_INET, "goip0", []uint16{getlink, getlink, getnetconf}},
		{"positive: mesh `show dev veth0`",
			netconfPcapMeshDev, unix.AF_UNSPEC, "veth0", []uint16{getlink, getlink, getnetconf}},
		{"positive: mesh `-4 show dev veth0`",
			netconfPcapMeshDev4, unix.AF_INET, "veth0", []uint16{getlink, getlink, getnetconf}},
		{"boundary: tunnel `show dev gre1`, the largest link dump",
			netconfPcapTunnelDev, unix.AF_UNSPEC, "gre1", []uint16{getlink, getlink, getnetconf}},
		{"boundary: tunnel `-4 show dev gre1`",
			netconfPcapTunnelDev4, unix.AF_INET, "gre1", []uint16{getlink, getlink, getnetconf}},
		{"negative: a bare show sends no get by name",
			netconfPcapBase, unix.AF_UNSPEC, "", []uint16{getlink, getnetconf}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := newStatsReplay(t, tc.pcap)
			c := &runCtx{src: src, lltab: NewLLTab(), out: io.Discard, errOut: io.Discard, family: tc.family}
			if err := netconfShow(c, tc.dev); err != nil {
				t.Fatalf("netconfShow(%q): %v", tc.dev, err)
			}
			capt, err := nlparity.ParseRouteCaptureFile(tc.pcap)
			if err != nil {
				t.Fatal(err)
			}
			var ip [][]byte
			for _, m := range capt.Requests() {
				ip = append(ip, capturedRequest(m))
			}
			if len(src.sent) != len(tc.expected) || len(ip) != len(tc.expected) {
				t.Fatalf("goip sent %d requests, ip %d; want %d each", len(src.sent), len(ip), len(tc.expected))
			}
			for i := range ip {
				if typ := requestTypeOf(src.sent[i]); typ != tc.expected[i] {
					t.Errorf("request %d type %d, want %d", i+1, typ, tc.expected[i])
				}
				if !bytes.Equal(src.sent[i], ip[i]) {
					t.Errorf("request %d differs\ngoip %x\nip   %x", i+1, src.sent[i], ip[i])
				}
			}
		})
	}

	// corner: a name the kernel does not know stops at the get by name, as
	// ll_name_to_index's 0 does, with no link dump after it.
	t.Run("corner: a missing device sends only the get by name", func(t *testing.T) {
		src := newStatsReplay(t, netconfPcapBaseDev)
		c := &runCtx{src: src, lltab: NewLLTab(), out: io.Discard, errOut: io.Discard}
		err := netconfShow(c, "nosuch0")
		if !errors.Is(err, ErrNoSuchDevice) {
			t.Fatalf("netconfShow(nosuch0) = %v, want ErrNoSuchDevice", err)
		}
		if len(src.sent) != 1 || src.dumps != 0 {
			t.Errorf("sent %d requests, %d dumps; want 1 get and no dump", len(src.sent), src.dumps)
		}
	})
}
