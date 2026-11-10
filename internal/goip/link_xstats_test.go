package goip

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// linkReadsDir is the C1-T03 reads corpus, captured in a namespace holding
// br0 (ifindex 14) and its port v2 (16).
const linkReadsDir = guestDumpsDir + "reads/"

// TestParseLinkXstatsArgs is iplink_ifla_xstats's and bridge_parse_xstats's
// argument handling.
//
// go test ./internal/goip/ -run TestParseLinkXstatsArgs
func TestParseLinkXstatsArgs(t *testing.T) {
	tests := []struct {
		description string
		args        string
		expected    linkXstatsArgs
		wantErr     error
	}{
		{"positive: `type bridge`, both bodies", "type bridge", linkXstatsArgs{kind: kindBridgeCst}, nil},
		{"positive: `type bridge_slave`", "type bridge_slave", linkXstatsArgs{kind: kindBridgeSlaveCst}, nil},
		{"positive: `t` is type, by matches()", "t bridge", linkXstatsArgs{kind: kindBridgeCst}, nil},
		{"positive: `igmp` selects the mcast body", "type bridge igmp", linkXstatsArgs{kind: kindBridgeCst, sel: render.LinkXstatsMcastCst}, nil},
		{"positive: `stp` selects the stp body", "type bridge_slave stp", linkXstatsArgs{kind: kindBridgeSlaveCst, sel: render.LinkXstatsStpCst}, nil},
		{"corner: a later selector overrides an earlier one", "type bridge_slave stp mcast",
			linkXstatsArgs{kind: kindBridgeSlaveCst, sel: render.LinkXstatsMcastCst}, nil},
		{"negative: no argument at all", "", linkXstatsArgs{}, ErrInvalidArgument},
		{"negative: `type` with no kind is incomplete", "type", linkXstatsArgs{}, ErrInvalidArgument},
		{"negative: a first word other than type or help", "bridge", linkXstatsArgs{}, ErrInvalidArgument},
		{"negative: selectors are strcmp, so `st` is an unknown attribute", "type bridge st",
			linkXstatsArgs{kind: kindBridgeCst}, ErrInvalidArgument},
		{"negative: the bond kinds are not grounded", "type bond", linkXstatsArgs{kind: kindBondCst}, ErrNotImplemented},
		{"negative: a kind with no xstats is not grounded", "type veth", linkXstatsArgs{kind: kindVethCst}, ErrNotImplemented},
		{"negative: `dev` resolves a name no capture grounds", "type bridge dev br0",
			linkXstatsArgs{kind: kindBridgeCst}, ErrNotImplemented},
		{"boundary: `help` first is usage", "help", linkXstatsArgs{}, ErrNotImplemented},
		{"boundary: `help` after the kind is the kind's usage", "type bridge help",
			linkXstatsArgs{kind: kindBridgeCst}, ErrNotImplemented},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := parseLinkXstatsArgs(strings.Fields(tc.args))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if got != tc.expected {
				t.Errorf("args = %+v, want %+v", got, tc.expected)
			}
		})
	}
}

// TestParseLinkAfstatsArgs is iplink_afstats's argument loop.
//
// go test ./internal/goip/ -run TestParseLinkAfstatsArgs
func TestParseLinkAfstatsArgs(t *testing.T) {
	tests := []struct {
		description string
		args        string
		expected    string // the dev
		wantErr     error
		wantRC      int
	}{
		{"positive: no argument lists every device", "", "", nil, ExitOK},
		{"positive: `dev` names one", "dev br0", "br0", nil, ExitOK},
		{"negative: `dev` with no name is incomplete", "dev", "", ErrLinkUsage, ExitMinusOne},
		{"negative: a second `dev` is duparg2", "dev d0 dev d1", "", ErrLinkUsage, ExitMinusOne},
		{"negative: `dev` is strcmp, so `de` is unknown", "de br0", "", ErrLinkUsage, ExitMinusOne},
		{"negative: an unknown word", "bogus", "", ErrLinkUsage, ExitMinusOne},
		{"boundary: `help` is usage", "help", "", ErrNotImplemented, exitCodeOf(ErrNotImplemented)},
		{"corner: `h` is help by matches()", "h", "", ErrNotImplemented, exitCodeOf(ErrNotImplemented)},
		{"corner: the word after `dev` is the name, even `help`", "dev help", "help", nil, ExitOK},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			dev, err := parseLinkAfstatsArgs(strings.Fields(tc.args))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
			if dev != tc.expected {
				t.Errorf("dev = %q, want %q", dev, tc.expected)
			}
			if rc := exitCodeOf(err); err != nil && rc != tc.wantRC {
				t.Errorf("rc = %d, want %d", rc, tc.wantRC)
			}
		})
	}
}

// TestLinkXstatsMatchesIp replays the three C1-T03 captures through the
// command with nothing cached: goip sends ip's requests byte for byte, the
// stats dump and, for each device it names, ll_index_to_name's RTM_GETLINK
// get, and prints ip's sidecar.
//
// go test ./internal/goip/ -run TestLinkXstatsMatchesIp
func TestLinkXstatsMatchesIp(t *testing.T) {
	const (
		getlink  = 18 // RTM_GETLINK
		getstats = 94 // RTM_GETSTATS
	)
	tests := []struct {
		description string
		pcap        string
		sidecar     string
		args        string
		expected    []uint16 // request types, in order
	}{
		{"positive: `xstats type bridge` names br0 alone, by one get", "netlink_route_getstats_xstats_bridge.pcap",
			"ip_link_xstats_bridge", "xstats type bridge", []uint16{getstats, getlink}},
		{"positive: `xstats type bridge_slave` names v2 and prints its mcast and stp", "netlink_route_getstats_xstats_bridge_slave.pcap",
			"ip_link_xstats_bridge_slave", "xstats type bridge_slave", []uint16{getstats, getlink}},
		{"boundary: `afstats` with no MPLS names nothing and prints nothing", "netlink_route_getstats_afstats.pcap",
			"ip_link_afstats", "afstats", []uint16{getstats}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			pcap := linkReadsDir + tc.pcap
			src := routeGetReplay{newStatsReplay(t, pcap)}
			var out bytes.Buffer
			c := &runCtx{src: src, lltab: NewLLTab(), out: &out, errOut: io.Discard}
			if err := runLink(c, strings.Fields(tc.args)); err != nil {
				t.Fatalf("runLink: %v", err)
			}
			want, err := os.ReadFile(linkReadsDir + tc.sidecar)
			if err != nil {
				t.Fatal(err)
			}
			if out.String() != string(want) {
				t.Errorf("stdout differs from %s\n got %q\nwant %q", tc.sidecar, out.String(), want)
			}
			capt, err := nlparity.ParseRouteCaptureFile(pcap)
			if err != nil {
				t.Fatal(err)
			}
			ip := capt.Requests()
			if len(src.sent) != len(tc.expected) || len(ip) != len(tc.expected) {
				t.Fatalf("goip sent %d requests, ip %d; want %d each", len(src.sent), len(ip), len(tc.expected))
			}
			for i := range ip {
				if typ := requestTypeOf(src.sent[i]); typ != tc.expected[i] {
					t.Errorf("request %d type %d, want %d", i+1, typ, tc.expected[i])
				}
				if !bytes.Equal(src.sent[i], capturedRequest(ip[i])) {
					t.Errorf("request %d differs\ngoip %x\nip   %x", i+1, src.sent[i], capturedRequest(ip[i]))
				}
			}
		})
	}
}

// TestLinkXstatsJSON is the -j array: one object per bridge stanza, the name
// then the bodies, and `[]` for afstats with nothing to print.
//
// go test ./internal/goip/ -run TestLinkXstatsJSON
func TestLinkXstatsJSON(t *testing.T) {
	tests := []struct {
		description string
		pcap        string
		args        []string
		expected    []string // top-level keys of each element, in order
	}{
		{"positive: the master stanza is ifname and multicast", "netlink_route_getstats_xstats_bridge.pcap",
			[]string{"xstats", "type", "bridge"}, []string{"ifname,multicast"}},
		{"positive: the port stanza adds stp", "netlink_route_getstats_xstats_bridge_slave.pcap",
			[]string{"xstats", "type", "bridge_slave"}, []string{"ifname,multicast,stp"}},
		{"corner: `stp` on the master leaves the name alone", "netlink_route_getstats_xstats_bridge.pcap",
			[]string{"xstats", "type", "bridge", "stp"}, []string{"ifname"}},
		{"boundary: afstats is the empty array", "netlink_route_getstats_afstats.pcap",
			[]string{"afstats"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			src := routeGetReplay{newStatsReplay(t, linkReadsDir+tc.pcap)}
			var out bytes.Buffer
			c := &runCtx{src: src, lltab: NewLLTab(), out: &out, errOut: io.Discard, json: true}
			if err := runLink(c, tc.args); err != nil {
				t.Fatalf("runLink: %v", err)
			}
			var elems []json.RawMessage
			if err := json.Unmarshal(out.Bytes(), &elems); err != nil {
				t.Fatalf("not a JSON array: %v: %q", err, out.String())
			}
			if len(elems) != len(tc.expected) {
				t.Fatalf("%d elements, want %d: %s", len(elems), len(tc.expected), out.String())
			}
			for i, e := range elems {
				if got := strings.Join(jsonKeys(t, e), ","); got != tc.expected[i] {
					t.Errorf("element %d keys %s, want %s", i, got, tc.expected[i])
				}
			}
		})
	}
}

// jsonKeys returns an object's top-level keys in document order.
func jsonKeys(t *testing.T, obj json.RawMessage) []string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(obj))
	var keys []string
	if _, err := dec.Token(); err != nil {
		t.Fatal(err)
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			t.Fatal(err)
		}
		keys = append(keys, tok.(string))
		var skip json.RawMessage
		if err := dec.Decode(&skip); err != nil {
			t.Fatal(err)
		}
	}
	return keys
}

// TestLinkAfstats is iplink_afstats and print_af_stats over hand-built
// replies: the family filter, the stderr-only families, the record-level
// filter for `dev`, and the exits that are all 0 once the arguments parse.
//
// go test ./internal/goip/ -run TestLinkAfstats
func TestLinkAfstats(t *testing.T) {
	hdr := func(ifindex uint32) []byte {
		h := make([]byte, xtcpnl.IfStatsMsgSizeCst)
		binary.NativeEndian.PutUint32(h[4:8], ifindex)
		return h
	}
	link := make([]byte, xtcpnl.MplsLinkStatsSizeCst)
	binary.NativeEndian.PutUint64(link[16:], 7) // rx_bytes
	spec := func(members ...[]byte) []byte {
		return rtattrBytes(unix.IFLA_STATS_AF_SPEC, slices.Concat(members...))
	}
	mpls := rtattrBytes(unix.AF_MPLS, rtattrBytes(mplsStatsLinkCst, link))
	const (
		head3 = "3:if3\n"
		body  = "    mpls:\n" +
			"        RX: bytes packets errors dropped noroute\n" +
			"                7       0      0       0       0 \n" +
			"        TX: bytes packets errors dropped\n" +
			"                0       0      0       0 \n"
	)
	tests := []struct {
		description string
		args        string
		family      uint8
		json        bool
		src         Source
		expected    string
		stderr      string
	}{
		{description: "positive: an mpls member under its device's name",
			src: typedStatsSource{unix.RTM_NEWSTATS: {slices.Concat(hdr(3), spec(mpls))}}, expected: head3 + body},
		{description: "positive: json is the members with no object around them",
			json: true, src: typedStatsSource{unix.RTM_NEWSTATS: {slices.Concat(hdr(3), spec(mpls)), slices.Concat(hdr(4), spec(mpls))}},
			expected: `["ifindex":3,"ifname":"if3","rx":{"bytes":7,"packets":0,"errors":0,"dropped":0,"noroute":0},` +
				`"tx":{"bytes":0,"packets":0,"errors":0,"dropped":0},"ifindex":4,"ifname":"if4","rx":{"bytes":7,` +
				`"packets":0,"errors":0,"dropped":0,"noroute":0},"tx":{"bytes":0,"packets":0,"errors":0,"dropped":0}]` + "\n"},
		{description: "positive: `dev` keeps that device's record alone", args: "dev if4",
			src:      typedStatsSource{unix.RTM_NEWSTATS: {slices.Concat(hdr(3), spec(mpls)), slices.Concat(hdr(4), spec(mpls))}},
			expected: "4:if4\n" + body},
		{description: "negative: -4 filters the mpls member out, so nothing prints", family: unix.AF_INET,
			src: typedStatsSource{unix.RTM_NEWSTATS: {slices.Concat(hdr(3), spec(mpls))}}},
		{description: "positive: -M keeps it", family: unix.AF_MPLS,
			src: typedStatsSource{unix.RTM_NEWSTATS: {slices.Concat(hdr(3), spec(mpls))}}, expected: head3 + body},
		{description: "corner: another family prints the name, and the complaint on stderr",
			src:      typedStatsSource{unix.RTM_NEWSTATS: {slices.Concat(hdr(3), spec(rtattrBytes(unix.AF_INET6, nil), mpls))}},
			expected: head3 + body, stderr: "    unknown af(10)\n"},
		{description: "corner: an mpls member with no MPLS_STATS_LINK is the name alone",
			src:      typedStatsSource{unix.RTM_NEWSTATS: {slices.Concat(hdr(3), spec(rtattrBytes(unix.AF_MPLS, nil)))}},
			expected: head3},
		{description: "boundary: an empty nest prints nothing, and -j an empty array",
			json: true, src: typedStatsSource{unix.RTM_NEWSTATS: {slices.Concat(hdr(3), spec())}}, expected: "[]\n"},
		{description: "negative: a short reply ends the dump, which still closes the array",
			json: true, src: typedStatsSource{unix.RTM_NEWSTATS: {make([]byte, xtcpnl.IfStatsMsgSizeCst-1)}},
			expected: "[]\n", stderr: "BUG: wrong nlmsg len -1\nDump terminated\n"},
		{description: "negative: a failed dump is the same",
			json: true, src: &failSource{}, expected: "[]\n", stderr: "source refused\nDump terminated\n"},
		{description: "negative: a `dev` the kernel has no device for is stderr alone", args: "dev nosuch",
			src: &enodevSource{}, stderr: "Device \"nosuch\" does not exist.\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var out, errOut bytes.Buffer
			c := &runCtx{out: &out, errOut: &errOut, lltab: NewLLTab(), json: tc.json, family: tc.family, src: tc.src}
			c.lltab.Fill([]xtcpnl.LinkInfo{{Index: 3, Name: "if3"}, {Index: 4, Name: "if4"}})
			err := linkAfstats(c, strings.Fields(tc.args))
			if err != nil || out.String() != tc.expected || errOut.String() != tc.stderr {
				t.Errorf("stdout %q\nstderr %q\nerr %v\nwant %q, %q", out.String(), errOut.String(), err, tc.expected, tc.stderr)
			}
		})
	}
}
