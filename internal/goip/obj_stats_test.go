package goip

import (
	"bytes"
	"encoding/binary"
	"os"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The RTM_GETSTATS captures. Each dump pcap holds the stats dump, then one
// RTM_GETLINK get per ifindex, ll_index_to_name's (ip/ipstats.c:761), so a
// single GOIP_REPLAY serves both: the stats path reads the RTM_NEWSTATS replies,
// then each record's name resolves from the RTM_NEWLINK replies. The
// _dev pcap holds the point-get reply; under replay (no Talk) IfStatsByIndex
// dumps it and filters to the ifindex, the netconf/nexthop precedent. The -s and
// -j sidecars replay the plain dump pcap — the flag is render-only, identical
// reply bytes. Every command carries `group link`, so the reply is link-only.
const (
	statsPcapBase      = guestDumpsDir + "netlink_route_getstats.pcap"
	statsPcapBaseDev   = guestDumpsDir + "netlink_route_getstats_dev.pcap"
	statsPcapMesh      = guestDumpsDir + "mesh/netlink_route_getstats.pcap"
	statsPcapMeshDev   = guestDumpsDir + "mesh/netlink_route_getstats_dev.pcap"
	statsPcapTunnel    = guestDumpsDir + "tunnel/netlink_route_getstats.pcap"
	statsPcapTunnelDev = guestDumpsDir + "tunnel/netlink_route_getstats_dev.pcap"

	// The whole-group xstats dumps (filter_mask 0x2). Every device prints four
	// leaf stanzas; only mesh's br0 carries vlan/mcast bodies.
	xstatsPcapBase   = guestDumpsDir + "netlink_route_getstats_xstats.pcap"
	xstatsPcapMesh   = guestDumpsDir + "mesh/netlink_route_getstats_xstats.pcap"
	xstatsPcapTunnel = guestDumpsDir + "tunnel/netlink_route_getstats_xstats.pcap"
)

// statsCompareMode selects how a row's output is matched against its sidecar.
type statsCompareMode int

const (
	// statsByteExact: the pcap and sidecar came from one `ip` run, so the bytes
	// match exactly (the plain dump and the point get).
	statsByteExact statsCompareMode = iota
	// statsExtended: the `-s` sidecar is a separate `ip -s` run whose main RX/TX
	// counters ticked past the pcap's; normalizeLinkStatsText collapses those
	// value lines and everything else (headers, the all-zero error lines) matches.
	statsExtended
	// statsJSON: the `-j` sidecar is a separate run too, so the stats64 numbers
	// are zeroed on both sides and the entries compared structurally.
	statsJSON
)

// TestStatsShowMatchesCapturedSidecars replays the committed RTM_GETSTATS
// captures and compares goip's output with the `ip stats show group link`
// sidecars, across all three topologies and the four render forms.
//
// go test ./internal/goip/ -run TestStatsShowMatchesCapturedSidecars
func TestStatsShowMatchesCapturedSidecars(t *testing.T) {
	tests := []struct {
		description string
		args        []string
		pcap        string
		sidecar     string
		mode        statsCompareMode
	}{
		{
			description: "positive: base plain dump renders byte-for-byte",
			args:        []string{"stats", "show", "group", "link"},
			pcap:        statsPcapBase, sidecar: "ip_stats", mode: statsByteExact,
		},
		{
			description: "positive: base extended (-s) matches on layout, counters normalized",
			args:        []string{"-s", "stats", "show", "group", "link"},
			pcap:        statsPcapBase, sidecar: "ip_stats_s", mode: statsExtended,
		},
		{
			description: "positive: base JSON equals the -j -p sidecar structurally",
			args:        []string{"-j", "stats", "show", "group", "link"},
			pcap:        statsPcapBase, sidecar: "ip_stats_json", mode: statsJSON,
		},
		{
			description: "positive: base `dev goip0` point get renders byte-for-byte",
			args:        []string{"stats", "show", "group", "link", "dev", "goip0"},
			pcap:        statsPcapBaseDev, sidecar: "ip_stats_dev", mode: statsByteExact,
		},
		{
			description: "corner: mesh plain dump",
			args:        []string{"stats", "show", "group", "link"},
			pcap:        statsPcapMesh, sidecar: "mesh/ip_stats", mode: statsByteExact,
		},
		{
			description: "corner: mesh extended (-s)",
			args:        []string{"-s", "stats", "show", "group", "link"},
			pcap:        statsPcapMesh, sidecar: "mesh/ip_stats_s", mode: statsExtended,
		},
		{
			description: "corner: mesh JSON",
			args:        []string{"-j", "stats", "show", "group", "link"},
			pcap:        statsPcapMesh, sidecar: "mesh/ip_stats_json", mode: statsJSON,
		},
		{
			description: "corner: mesh `dev veth0` point get",
			args:        []string{"stats", "show", "group", "link", "dev", "veth0"},
			pcap:        statsPcapMeshDev, sidecar: "mesh/ip_stats_dev", mode: statsByteExact,
		},
		{
			description: "corner: tunnel plain dump",
			args:        []string{"stats", "show", "group", "link"},
			pcap:        statsPcapTunnel, sidecar: "tunnel/ip_stats", mode: statsByteExact,
		},
		{
			description: "corner: tunnel extended (-s)",
			args:        []string{"-s", "stats", "show", "group", "link"},
			pcap:        statsPcapTunnel, sidecar: "tunnel/ip_stats_s", mode: statsExtended,
		},
		{
			description: "corner: tunnel JSON",
			args:        []string{"-j", "stats", "show", "group", "link"},
			pcap:        statsPcapTunnel, sidecar: "tunnel/ip_stats_json", mode: statsJSON,
		},
		{
			description: "corner: tunnel `dev gre1` point get",
			args:        []string{"stats", "show", "group", "link", "dev", "gre1"},
			pcap:        statsPcapTunnelDev, sidecar: "tunnel/ip_stats_dev", mode: statsByteExact,
		},
		{
			description: "positive: mesh xstats plain — br0 vlan+mcast bodies, empty stanzas elsewhere",
			args:        []string{"stats", "show", "group", "xstats"},
			pcap:        xstatsPcapMesh, sidecar: "mesh/ip_stats_xstats", mode: statsByteExact,
		},
		{
			description: "positive: mesh xstats -s equals plain (no extended branch)",
			args:        []string{"-s", "stats", "show", "group", "xstats"},
			pcap:        xstatsPcapMesh, sidecar: "mesh/ip_stats_xstats_s", mode: statsByteExact,
		},
		{
			description: "positive: mesh xstats JSON flat array, one object per leaf per device",
			args:        []string{"-j", "stats", "show", "group", "xstats"},
			pcap:        xstatsPcapMesh, sidecar: "mesh/ip_stats_xstats_json", mode: statsJSON,
		},
		{
			description: "corner: base xstats plain — no bridge, four empty stanzas per device",
			args:        []string{"stats", "show", "group", "xstats"},
			pcap:        xstatsPcapBase, sidecar: "ip_stats_xstats", mode: statsByteExact,
		},
		{
			description: "corner: base xstats -s equals plain",
			args:        []string{"-s", "stats", "show", "group", "xstats"},
			pcap:        xstatsPcapBase, sidecar: "ip_stats_xstats_s", mode: statsByteExact,
		},
		{
			description: "corner: base xstats JSON",
			args:        []string{"-j", "stats", "show", "group", "xstats"},
			pcap:        xstatsPcapBase, sidecar: "ip_stats_xstats_json", mode: statsJSON,
		},
		{
			description: "corner: tunnel xstats plain — same empty-stanza shape",
			args:        []string{"stats", "show", "group", "xstats"},
			pcap:        xstatsPcapTunnel, sidecar: "tunnel/ip_stats_xstats", mode: statsByteExact,
		},
		{
			description: "corner: tunnel xstats -s equals plain",
			args:        []string{"-s", "stats", "show", "group", "xstats"},
			pcap:        xstatsPcapTunnel, sidecar: "tunnel/ip_stats_xstats_s", mode: statsByteExact,
		},
		{
			description: "corner: tunnel xstats JSON",
			args:        []string{"-j", "stats", "show", "group", "xstats"},
			pcap:        xstatsPcapTunnel, sidecar: "tunnel/ip_stats_xstats_json", mode: statsJSON,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			want, err := os.ReadFile(guestDumpsDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			t.Setenv("GOIP_REPLAY", tc.pcap)
			var stdout, stderr bytes.Buffer
			if code := Run(tc.args, &stdout, &stderr); code != ExitOK {
				t.Fatalf("Run(%q) = %d, stderr=%s", tc.args, code, stderr.String())
			}
			switch tc.mode {
			case statsByteExact:
				if !bytes.Equal(stdout.Bytes(), want) {
					t.Fatalf("output mismatch\n got: %q\nwant: %q", stdout.Bytes(), want)
				}
			case statsExtended:
				assertLinesEqual(t,
					normalizeLinkStatsText(t, stdout.String()),
					normalizeLinkStatsText(t, string(want)))
			case statsJSON:
				assertJSONEntriesEqual(t,
					zeroLinkStats(t, stdout.Bytes()),
					zeroLinkStats(t, want),
					false)
			}
		})
	}
}

// typedStatsSource is a Source that answers each message type from its own body
// set, so one fake can serve the link dump (RTM_NEWLINK) and the stats dump
// (RTM_NEWSTATS) that statsShow issues in sequence.
type typedStatsSource map[uint16][][]byte

func (s typedStatsSource) Dump(_ []byte, typ uint16) ([][]byte, error) { return s[typ], nil }

// ifsmUnsupportedBody builds a synthetic RTM_NEWSTATS body: the 12-byte
// if_stats_msg header plus a single IFLA_STATS_AF_SPEC attribute — a stat group
// goip does not ground. No `group link` capture ever produces this (the kernel
// returns link-only for filter_mask 0x1); it is the bare-show-on-a-bridge shape
// the refusal exists to catch.
func ifsmUnsupportedBody(ifindex uint32) []byte {
	b := make([]byte, 12, 12+8) // the ifstatsmsg, then the 8-byte attr below
	binary.LittleEndian.PutUint32(b[4:8], ifindex)
	binary.LittleEndian.PutUint32(b[8:12], 0x1f) // all groups requested
	attr := make([]byte, 8)
	binary.LittleEndian.PutUint16(attr[0:2], 8) // rta_len: header + 4-byte value
	binary.LittleEndian.PutUint16(attr[2:4], uint16(unix.IFLA_STATS_AF_SPEC))
	return append(b, attr...)
}

// goipRtattr builds a 4-byte-padded rtattr, for the synthetic xstats bodies
// below.
func goipRtattr(atype uint16, val []byte) []byte {
	l := 4 + len(val)
	b := make([]byte, l)
	binary.LittleEndian.PutUint16(b[0:2], uint16(l))
	binary.LittleEndian.PutUint16(b[2:4], atype)
	copy(b[4:], val)
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

// xstatsBody wraps a LINK_XSTATS group attr around inner bytes, after the 12-byte
// if_stats_msg header (ifindex, filter_mask 0x2).
func xstatsBody(ifindex uint32, inner []byte) []byte {
	return statsGroupBody(ifindex, xtcpnl.StatsFilterXstats, goipRtattr(uint16(unix.IFLA_STATS_LINK_XSTATS), inner))
}

// xstatsSlaveBody is xstatsBody for LINK_XSTATS_SLAVE (filter_mask 0x4).
func xstatsSlaveBody(ifindex uint32, inner []byte) []byte {
	return statsGroupBody(ifindex, xtcpnl.StatsFilterXstatsSlave, goipRtattr(uint16(unix.IFLA_STATS_LINK_XSTATS_SLAVE), inner))
}

func statsGroupBody(ifindex, mask uint32, attrs ...[]byte) []byte {
	b := make([]byte, 12)
	binary.LittleEndian.PutUint32(b[4:8], ifindex)
	binary.LittleEndian.PutUint32(b[8:12], mask)
	for _, a := range attrs {
		b = append(b, a...)
	}
	return b
}

// TestStatsSidecarsAreDistinct keeps the comparison helpers honest: the short,
// extended and JSON forms differ, and the three topologies' goldens differ.
//
// go test ./internal/goip/ -run TestStatsSidecarsAreDistinct
func TestStatsSidecarsAreDistinct(t *testing.T) {
	tests := []struct {
		description string
		a, b        string
		wantEqual   bool
	}{
		{
			description: "negative: the plain and extended (-s) forms differ (the error lines)",
			a:           "ip_stats", b: "ip_stats_s", wantEqual: false,
		},
		{
			description: "negative: the text and JSON forms differ",
			a:           "ip_stats", b: "ip_stats_json", wantEqual: false,
		},
		{
			description: "corner: the full dump and the dev-filtered record differ",
			a:           "ip_stats", b: "ip_stats_dev", wantEqual: false,
		},
		{
			description: "corner: the base and tunnel dumps differ",
			a:           "ip_stats", b: "tunnel/ip_stats", wantEqual: false,
		},
		{
			description: "negative: xstats text differs from the link-group text",
			a:           "mesh/ip_stats_xstats", b: "mesh/ip_stats", wantEqual: false,
		},
		{
			description: "boundary: mesh xstats (br0 bodies) differs from the all-empty base xstats",
			a:           "mesh/ip_stats_xstats", b: "ip_stats_xstats", wantEqual: false,
		},
		{
			description: "positive: xstats -s equals plain (the bridge bodies have no extended branch)",
			a:           "mesh/ip_stats_xstats", b: "mesh/ip_stats_xstats_s", wantEqual: true,
		},
		{
			description: "negative: xstats text and JSON forms differ",
			a:           "mesh/ip_stats_xstats", b: "mesh/ip_stats_xstats_json", wantEqual: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			a, err := os.ReadFile(guestDumpsDir + tt.a)
			if err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(guestDumpsDir + tt.b)
			if err != nil {
				t.Fatal(err)
			}
			if got := bytes.Equal(a, b); got != tt.wantEqual {
				t.Errorf("bytes.Equal(%s, %s) = %v, want %v", tt.a, tt.b, got, tt.wantEqual)
			}
		})
	}
}
