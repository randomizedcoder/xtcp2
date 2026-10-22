package goip

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
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

	err := statsShow(c, statsSel{group: "link"})
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

// TestStatsXstatsUngroundedBodyRefused is the sub-attribute safety: an xstats
// or xstats_slave reply carrying a bridge stp body, a repeated 802.3ad nest, or
// a group other than the one requested makes statsShow refuse rather than drop
// it, while the grounded bodies render. These shapes need synthetic bodies: the
// grounded topologies have STP disabled, and the kernel puts one 802.3ad nest
// and only the requested group.
//
// go test ./internal/goip/ -run TestStatsXstatsUngroundedBodyRefused
func TestStatsXstatsUngroundedBodyRefused(t *testing.T) {
	bridgeVlan := goipRtattr(xtcpnl.LinkXstatsTypeBridge, goipRtattr(xtcpnl.BridgeXstatsVlan, make([]byte, xtcpnl.BrVlanXstatsSizeCst)))
	counter := goipRtattr(xtcpnl.Bond3adStatLacpduRx, make([]byte, 8))
	bond3ad := goipRtattr(xtcpnl.LinkXstatsTypeBond, goipRtattr(xtcpnl.BondXstats3ad, counter))
	bondTwice := goipRtattr(xtcpnl.LinkXstatsTypeBond, append(goipRtattr(xtcpnl.BondXstats3ad, counter), goipRtattr(xtcpnl.BondXstats3ad, counter)...))
	bridgeStp := goipRtattr(xtcpnl.LinkXstatsTypeBridge, goipRtattr(xtcpnl.BridgeXstatsStp, make([]byte, 48)))

	tests := []struct {
		description string
		group       string
		body        []byte
		expected    bool // refused
	}{
		{
			description: "positive: a bridge vlan body renders, no refusal",
			group:       "xstats",
			body:        xstatsBody(5, bridgeVlan),
			expected:    false,
		},
		{
			description: "positive: a bond 802.3ad body renders, no refusal",
			group:       "xstats",
			body:        xstatsBody(5, bond3ad),
			expected:    false,
		},
		{
			description: "positive: a port's 802.3ad body renders under xstats_slave",
			group:       "xstats_slave",
			body:        xstatsSlaveBody(3, bond3ad),
			expected:    false,
		},
		{
			description: "negative: a repeated 802.3ad nest is refused",
			group:       "xstats",
			body:        xstatsBody(5, bondTwice),
			expected:    true,
		},
		{
			description: "negative: a bridge stp body is refused",
			group:       "xstats",
			body:        xstatsBody(5, bridgeStp),
			expected:    true,
		},
		{
			description: "negative: a bridge stp body under xstats_slave is refused",
			group:       "xstats_slave",
			body:        xstatsSlaveBody(3, bridgeStp),
			expected:    true,
		},
		{
			description: "negative: an xstats_slave group in an xstats reply is refused, not dropped",
			group:       "xstats",
			body:        statsGroupBody(5, xtcpnl.StatsFilterXstats, goipRtattr(uint16(unix.IFLA_STATS_LINK_XSTATS_SLAVE), bond3ad)),
			expected:    true,
		},
		{
			description: "negative: an xstats group in an xstats_slave reply is refused, not dropped",
			group:       "xstats_slave",
			body:        statsGroupBody(5, xtcpnl.StatsFilterXstatsSlave, goipRtattr(uint16(unix.IFLA_STATS_LINK_XSTATS), bond3ad)),
			expected:    true,
		},
		{
			description: "negative: a link group in an xstats reply is refused",
			group:       "xstats",
			body:        statsGroupBody(5, xtcpnl.StatsFilterXstats, goipRtattr(uint16(unix.IFLA_STATS_LINK_64), nil)),
			expected:    true,
		},
		{
			description: "corner: an empty bridge nest renders the empty stanzas, no refusal",
			group:       "xstats",
			body:        xstatsBody(5, goipRtattr(xtcpnl.LinkXstatsTypeBridge, nil)),
			expected:    false,
		},
		{
			description: "corner: an empty bond type nest (a bond not in 802.3ad mode) renders, no refusal",
			group:       "xstats",
			body:        xstatsBody(5, goipRtattr(xtcpnl.LinkXstatsTypeBond, nil)),
			expected:    false,
		},
		{
			description: "boundary: an empty xstats_slave nest renders the empty stanzas",
			group:       "xstats_slave",
			body:        xstatsSlaveBody(3, nil),
			expected:    false,
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
			err := statsShow(c, statsSel{group: tc.group})
			switch {
			case tc.expected:
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
// required group selector, the `subgroup`/`suite` narrowing, the `dev` point
// get, and everything it refuses.
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
		replay           string // defaults to statsPcapBase
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
			description:      "negative: a subgroup inside the link leaf is refused",
			args:             []string{"stats", "show", "group", "link", "subgroup", "bridge"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "inside leaf group link",
		},
		{
			description:      "negative: `set` with no arguments is ip's missing-dev refusal",
			args:             []string{"stats", "set"},
			wantCode:         ExitUsage,
			wantStderrSubstr: `"dev" argument is required`,
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
			// lo carries no bridge xstats, so the first leaf renders header-only
			// — a deterministic prefix for the xstats path.
			description:      "positive: `stats show group xstats` runs the xstats path (empty-header leaves on a non-bridge)",
			args:             []string{"stats", "show", "group", "xstats"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "1: lo: group xstats subgroup bond suite 802.3ad\n",
			replay:           xstatsPcapBase,
		},
		{
			// The replay serves RTM_NEWSTATS by type, so the link group's
			// replies arrive under an xstats request. The kernel never does
			// that; goip refuses rather than render headers over a link body.
			description:      "negative: link-group replies under an xstats request are refused",
			args:             []string{"stats", "show", "group", "xstats"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "beyond link",
		},
		{
			description:      "positive: `group xstats subgroup bridge` skips the bond leaf",
			args:             []string{"stats", "show", "group", "xstats", "subgroup", "bridge"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "1: lo: group xstats subgroup bridge suite vlan\n1: lo: group xstats subgroup bridge suite mcast\n",
			replay:           xstatsPcapBase,
		},
		{
			description:      "boundary: `subgroup bridge suite stp`, the last leaf alone",
			args:             []string{"stats", "show", "group", "xstats", "subgroup", "bridge", "suite", "stp"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "1: lo: group xstats subgroup bridge suite stp\n\n2: ",
			replay:           xstatsPcapBase,
		},
		{
			description:      "negative: a suite from the other subgroup is refused",
			args:             []string{"stats", "show", "group", "xstats", "subgroup", "bond", "suite", "vlan"},
			wantCode:         ExitUsage,
			wantStderrSubstr: "no such suite",
			replay:           xstatsPcapBase,
		},
		{
			description:      "positive: `group xstats dev lo` is the point get, one interface and no dump blank line",
			args:             []string{"stats", "show", "group", "xstats", "dev", "lo"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "1: lo: group xstats subgroup bond suite 802.3ad\n1: lo: group xstats subgroup bridge suite vlan\n",
			replay:           xstatsPcapBase,
		},
		{
			description:      "positive: `stats show group xstats_slave` runs the slave path",
			args:             []string{"stats", "show", "group", "xstats_slave"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "1: lo: group xstats_slave subgroup bond suite 802.3ad\n",
			replay:           linkWriteDir + "lacp_converged/netlink_route_getstats_xstats_slave.pcap",
		},
		{
			description:      "positive: `group xstats_slave dev la1` is the port's point get",
			args:             []string{"stats", "show", "group", "xstats_slave", "dev", "la1"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "3: la1: group xstats_slave subgroup bond suite 802.3ad\n                    LACPDU Rx ",
			replay:           linkWriteDir + "lacp_converged/netlink_route_getstats_xstats_slave.pcap",
		},
		{
			description:      "positive: `group xstats_slave subgroup bond` prints the bond leaf alone per interface",
			args:             []string{"stats", "show", "group", "xstats_slave", "subgroup", "bond"},
			wantCode:         ExitOK,
			wantStdoutPrefix: "1: lo: group xstats_slave subgroup bond suite 802.3ad\n\n2: nlmon0: group xstats_slave subgroup bond suite 802.3ad\n\n3: la1: ",
			replay:           linkWriteDir + "lacp_converged/netlink_route_getstats_xstats_slave.pcap",
		},
		{
			description:      "negative: a dev the capture does not hold is refused",
			args:             []string{"stats", "show", "group", "xstats_slave", "dev", "nosuch"},
			wantCode:         ExitFailure,
			wantStderrSubstr: "cannot find device",
			replay:           linkWriteDir + "lacp_converged/netlink_route_getstats_xstats_slave.pcap",
		},
		{
			description:      "negative: `group xstats_s` does not abbreviate (strcmp)",
			args:             []string{"stats", "show", "group", "xstats_s"},
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
			replay := statsPcapBase
			if tt.replay != "" {
				replay = tt.replay
			}
			t.Setenv("GOIP_REPLAY", replay)
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

// TestParseStatsShowArgs pins the selection parseStatsShowArgs builds from the
// words after `stats show`, and which forms it refuses. subgroup and suite are
// render-only (ip/ipstats.c:578-585 sends no filter nest for xstats), so a
// wrong pairing is refused here, before anything is sent, as
// ipstats_enable_find_stat_desc (:999-1029) refuses it in iproute2.
func TestParseStatsShowArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		description string
		args        []string
		expected    statsSel
		wantErr     string // "" means no error; otherwise a substring
	}{
		{
			description: "positive: xstats with a dev",
			args:        []string{"group", "xstats", "dev", "bond0"},
			expected:    statsSel{group: "xstats", dev: "bond0"},
		},
		{
			description: "positive: dev ahead of the group is the same selection",
			args:        []string{"dev", "la1", "group", "xstats_slave"},
			expected:    statsSel{group: "xstats_slave", dev: "la1"},
		},
		{
			description: "positive: subgroup and suite",
			args:        []string{"group", "xstats", "subgroup", "bond", "suite", "802.3ad"},
			expected:    statsSel{group: "xstats", xsel: render.XstatsSelect{Subgroup: "bond", Suite: "802.3ad"}},
		},
		{
			description: "positive: the link group with a dev is unchanged",
			args:        []string{"group", "link", "dev", "goip0"},
			expected:    statsSel{group: "link", dev: "goip0"},
		},
		{
			description: "boundary: a subgroup without a suite",
			args:        []string{"group", "xstats_slave", "subgroup", "bond"},
			expected:    statsSel{group: "xstats_slave", xsel: render.XstatsSelect{Subgroup: "bond"}},
		},
		{
			description: "boundary: the last bridge suite",
			args:        []string{"group", "xstats", "subgroup", "bridge", "suite", "stp"},
			expected:    statsSel{group: "xstats", xsel: render.XstatsSelect{Subgroup: "bridge", Suite: "stp"}},
		},
		{
			description: "negative: a subgroup on the leaf link group",
			args:        []string{"group", "link", "subgroup", "bond"},
			wantErr:     "requested inside leaf group link",
		},
		{
			description: "negative: a suite without a subgroup",
			args:        []string{"group", "xstats", "suite", "802.3ad"},
			wantErr:     "without selecting a subgroup first",
		},
		{
			description: "negative: a subgroup before any group",
			args:        []string{"subgroup", "bond", "group", "xstats"},
			wantErr:     "without selecting a group first",
		},
		{
			description: "negative: an unknown subgroup",
			args:        []string{"group", "xstats", "subgroup", "foo"},
			wantErr:     "no subgroup of that name inside group xstats",
		},
		{
			description: "negative: a suite of the other subgroup",
			args:        []string{"group", "xstats", "subgroup", "bond", "suite", "vlan"},
			wantErr:     "no such suite inside that subgroup",
		},
		{
			description: "negative: a second group",
			args:        []string{"group", "xstats", "group", "link"},
			wantErr:     "one group per command",
		},
		{
			description: "negative: no group at all",
			args:        []string{"dev", "bond0"},
			wantErr:     "a grounded group",
		},
		{
			description: "negative: a second dev",
			args:        []string{"group", "xstats", "dev", "bond0", "dev", "la1"},
			wantErr:     "a second dev",
		},
		{
			description: "corner: a trailing dev keyword",
			args:        []string{"group", "xstats", "dev"},
			wantErr:     "missing device name",
		},
		{
			description: "corner: a trailing subgroup keyword",
			args:        []string{"group", "xstats", "subgroup"},
			wantErr:     "missing subgroup name",
		},
		{
			description: "corner: an unknown keyword",
			args:        []string{"group", "xstats", "nosuch"},
			wantErr:     `"nosuch"`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			got, err := parseStatsShowArgs(tc.args)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("parseStatsShowArgs(%q): %v", tc.args, err)
				}
				if got != tc.expected {
					t.Fatalf("parseStatsShowArgs(%q) = %+v, want %+v", tc.args, got, tc.expected)
				}
				return
			}
			if err == nil {
				t.Fatalf("parseStatsShowArgs(%q) = %+v, want an error containing %q", tc.args, got, tc.wantErr)
			}
			if !errors.Is(err, ErrNotImplemented) {
				t.Fatalf("parseStatsShowArgs(%q): %v, want it to wrap ErrNotImplemented", tc.args, err)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("parseStatsShowArgs(%q): %v, want it to contain %q", tc.args, err, tc.wantErr)
			}
		})
	}
}
