package goip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// The RTM_GETSTATS captures. Each dump pcap holds the ll_init_map link dump
// ahead of the stats transaction (do_ipstats resolves names via a link dump), so
// a single GOIP_REPLAY serves both: StatsLinks reads the RTM_NEWLINK replies to
// fill the index cache, then the stats path reads the RTM_NEWSTATS replies. The
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
	b := make([]byte, 12)
	binary.LittleEndian.PutUint32(b[4:8], ifindex)
	binary.LittleEndian.PutUint32(b[8:12], 0x1f) // all groups requested
	attr := make([]byte, 8)
	binary.LittleEndian.PutUint16(attr[0:2], 8) // rta_len: header + 4-byte value
	binary.LittleEndian.PutUint16(attr[2:4], uint16(unix.IFLA_STATS_AF_SPEC))
	return append(b, attr...)
}

// TestStatsUnsupportedGroupRefused is the "decode only what renders, refuse the
// rest" safety: a reply carrying a stat group beyond link makes statsShow return
// ErrNotImplemented rather than silently drop the group. It needs a synthetic
// body because no grounded `group link` capture can produce one.
//
// go test ./internal/goip/ -run TestStatsUnsupportedGroupRefused
func TestStatsUnsupportedGroupRefused(t *testing.T) {
	src := typedStatsSource{
		uint16(unix.RTM_NEWLINK):  nil,
		uint16(unix.RTM_NEWSTATS): {ifsmUnsupportedBody(7)},
	}
	var out bytes.Buffer
	c := &runCtx{src: src, lltab: NewLLTab(), out: &out, errOut: &out}

	err := statsShow(c, "link", "")
	if !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("statsShow with an unsupported group = %v, want ErrNotImplemented", err)
	}
	if out.Len() != 0 {
		t.Errorf("a refused record still produced output: %q", out.String())
	}
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
	b := make([]byte, 12)
	binary.LittleEndian.PutUint32(b[4:8], ifindex)
	binary.LittleEndian.PutUint32(b[8:12], xtcpnl.StatsFilterXstats)
	return append(b, goipRtattr(uint16(unix.IFLA_STATS_LINK_XSTATS), inner)...)
}

// TestStatsXstatsUngroundedBodyRefused is the sub-attribute safety: an xstats
// reply carrying a bridge stp or bond body makes statsShow refuse rather than
// drop it, while the grounded vlan body renders. The stp/bond shapes need
// synthetic bodies — the grounded topology has STP disabled and no bond.
//
// go test ./internal/goip/ -run TestStatsXstatsUngroundedBodyRefused
func TestStatsXstatsUngroundedBodyRefused(t *testing.T) {
	bridgeVlan := goipRtattr(xtcpnl.LinkXstatsTypeBridge, goipRtattr(xtcpnl.BridgeXstatsVlan, make([]byte, xtcpnl.BrVlanXstatsSizeCst)))
	bond := goipRtattr(xtcpnl.LinkXstatsTypeBond, nil)
	bridgeStp := goipRtattr(xtcpnl.LinkXstatsTypeBridge, goipRtattr(xtcpnl.BridgeXstatsStp, make([]byte, 48)))

	tests := []struct {
		description string
		body        []byte
		wantRefuse  bool
	}{
		{
			description: "positive: a bridge vlan body renders, no refusal",
			body:        xstatsBody(5, bridgeVlan),
			wantRefuse:  false,
		},
		{
			description: "negative: a bond xstats type is refused",
			body:        xstatsBody(5, bond),
			wantRefuse:  true,
		},
		{
			description: "negative: a bridge stp body is refused",
			body:        xstatsBody(5, bridgeStp),
			wantRefuse:  true,
		},
		{
			description: "corner: an empty bridge nest renders the empty stanzas, no refusal",
			body:        xstatsBody(5, goipRtattr(xtcpnl.LinkXstatsTypeBridge, nil)),
			wantRefuse:  false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := typedStatsSource{
				uint16(unix.RTM_NEWLINK):  nil,
				uint16(unix.RTM_NEWSTATS): {tc.body},
			}
			var out bytes.Buffer
			c := &runCtx{src: src, lltab: NewLLTab(), out: &out, errOut: &out}
			err := statsShow(c, "xstats", "")
			switch {
			case tc.wantRefuse:
				if !errors.Is(err, ErrNotImplemented) {
					t.Fatalf("err = %v, want ErrNotImplemented", err)
				}
				if out.Len() != 0 {
					t.Errorf("a refused record still produced output: %q", out.String())
				}
			default:
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				if out.Len() == 0 {
					t.Error("expected rendered stanzas, got no output")
				}
			}
		})
	}
}

// TestRunStatsArgs covers the CLI surface: the one verb runStats accepts, the
// required `group link` selector, the `dev` filter, and everything it refuses.
// show/group/dev are all strcmp'd (ip/ipstats.c), so NONE of them abbreviate —
// `stats sh` is not `stats show`, unlike netconf's `netconf sh`.
//
// go test ./internal/goip/ -run TestRunStatsArgs
func TestRunStatsArgs(t *testing.T) {
	tests := []struct {
		description      string
		args             []string
		wantCode         int
		wantStdoutPrefix string
		wantStderrSubstr string
	}{
		{
			description:      "positive: `stats show group link` lists",
			args:             []string{"stats", "show", "group", "link"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "1: lo: group link\n",
		},
		{
			description:      "positive: `stats show group link dev goip0` filters to the device",
			args:             []string{"stats", "show", "group", "link", "dev", "goip0"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "3: goip0: group link\n",
		},
		{
			description:      "negative: a bare `stats` is the multi-group default, which goip does not ground",
			args:             []string{"stats"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: `stats show` with no group is refused",
			args:             []string{"stats", "show"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: `stats sh` does not abbreviate show (strcmp)",
			args:             []string{"stats", "sh", "group", "link"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: a non-link group is refused",
			args:             []string{"stats", "show", "group", "bridge"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: subgroup is refused",
			args:             []string{"stats", "show", "group", "link", "subgroup", "bridge"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: the write verb `set` is refused",
			args:             []string{"stats", "set"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: `stats help` is refused, goip being read-only",
			args:             []string{"stats", "help"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: `stats show group link dev` with no name is refused",
			args:             []string{"stats", "show", "group", "link", "dev"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: `-j -s` is refused, the extended JSON keys are not grounded",
			args:             []string{"-j", "-s", "stats", "show", "group", "link"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			// Against the link replay pcap lo carries no bridge xstats, so the first
			// leaf renders header-only — a deterministic prefix for the xstats path.
			description:      "positive: `stats show group xstats` runs the xstats path (empty-header leaves on a non-bridge)",
			args:             []string{"stats", "show", "group", "xstats"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "1: lo: group xstats subgroup bond suite 802.3ad\n",
		},
		{
			description:      "negative: `group xstats subgroup bridge` partial selection is refused",
			args:             []string{"stats", "show", "group", "xstats", "subgroup", "bridge"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: `group xstats ... suite stp` partial selection is refused",
			args:             []string{"stats", "show", "group", "xstats", "subgroup", "bridge", "suite", "stp"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: `group xstats dev br0` has no grounded point get",
			args:             []string{"stats", "show", "group", "xstats", "dev", "br0"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
		{
			description:      "negative: `-j -s group xstats` is refused by the extended-JSON guard",
			args:             []string{"-j", "-s", "stats", "show", "group", "xstats"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "not implemented",
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			t.Setenv("GOIP_REPLAY", statsPcapBase)
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
