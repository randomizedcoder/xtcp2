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

	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// linkReadsDir is the C1-T03 reads corpus, captured in a namespace holding
// br0 (ifindex 14) and its port v2 (16).
const linkReadsDir = guestDumpsDir + "reads/"

// TestParseLinkXstatsArgs is iplink_ifla_xstats's and the kinds'
// parse_ifla_xstats argument handling: the body and device chosen, the help
// text for stdout, and the 255 of every refusal.
//
// go test ./internal/goip/ -run TestParseLinkXstatsArgs
func TestParseLinkXstatsArgs(t *testing.T) {
	type result struct {
		id      string
		kind    *linkXstatsKind
		attr    uint16
		ifindex int32
		help    string
		err     error
		msg     string // in the error's text, when set
	}
	tests := []struct {
		description string
		args        string
		expected    result
	}{
		{"positive: `type bridge`, every body", "type bridge", result{id: "bridge", kind: linkXstatsBridge}},
		{"positive: `type bond_slave` is bond's printer", "type bond_slave", result{id: "bond_slave", kind: linkXstatsBond}},
		{"positive: `t` is type, by matches()", "t bridge", result{id: "bridge", kind: linkXstatsBridge}},
		{"positive: `igmp` selects the mcast body", "type bridge igmp",
			result{id: "bridge", kind: linkXstatsBridge, attr: bridgeXstatsMcastCst}},
		{"positive: `lacp` selects 802.3ad", "type bond lacp", result{id: "bond", kind: linkXstatsBond, attr: xtcpnl.BondXstats3ad}},
		{"positive: `dev` filters by its index", "type bridge_slave dev if4",
			result{id: "bridge_slave", kind: linkXstatsBridge, ifindex: 4}},
		{"corner: a later selector overrides an earlier one", "type bridge_slave stp mcast",
			result{id: "bridge_slave", kind: linkXstatsBridge, attr: bridgeXstatsMcastCst}},
		{"corner: a later `dev` overrides an earlier one", "type bond dev if3 dev if4",
			result{id: "bond", kind: linkXstatsBond, ifindex: 4}},
		{"corner: a selector is the kind's, so bond's `stp` is unknown", "type bond stp",
			result{id: "bond", kind: linkXstatsBond, err: ErrLinkUsage}},
		{"negative: no argument at all", "", result{err: ErrLinkUsage}},
		{"negative: `type` with no kind is incomplete", "type", result{err: ErrLinkUsage}},
		{"negative: a first word other than type or help", "bridge", result{err: ErrLinkUsage}},
		{"negative: selectors are strcmp, so `st` is an unknown attribute", "type bridge st",
			result{id: "bridge", kind: linkXstatsBridge, err: ErrLinkUsage}},
		{"negative: a kind ip has but with no xstats", "type veth", result{id: "veth", err: ErrLinkUsage,
			msg: "xstats: link type veth doesn't support xstats"}},
		{"negative: a kind ip doesn't have", "type nosuch", result{id: "nosuch", err: ErrLinkUsage, msg: "invalid type"}},
		{"negative: `dev` with no name is incomplete", "type bridge dev",
			result{id: "bridge", kind: linkXstatsBridge, err: ErrLinkUsage}},
		{"negative: `dev` naming no device is nodev", "type bridge dev nosuch",
			result{id: "bridge", kind: linkXstatsBridge, err: ErrLinkUsage}},
		{"boundary: `help` first is print_explain", "help", result{help: "Usage: ... xstats type TYPE [ ARGS ]\n"}},
		{"boundary: `help` after the kind is the kind's, with its id", "type bond_slave help",
			result{id: "bond_slave", kind: linkXstatsBond, help: "Usage: ... bond_slave [ 802.3ad ] [ dev DEVICE ]\n"}},
		{"corner: the words before `help` still count", "type bridge dev nosuch help",
			result{id: "bridge", kind: linkXstatsBridge, err: ErrLinkUsage}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			c := &runCtx{lltab: NewLLTab(), src: &enodevSource{}}
			c.lltab.Fill([]xtcpnl.LinkInfo{{Index: 3, Name: "if3"}, {Index: 4, Name: "if4"}})
			a, help, err := parseLinkXstatsArgs(c, strings.Fields(tc.args))
			got := result{id: a.id, kind: a.kind, attr: a.attr, ifindex: a.ifindex, help: help}
			if !errors.Is(err, tc.expected.err) {
				t.Fatalf("err = %v, want %v", err, tc.expected.err)
			}
			if tc.expected.msg != "" && !strings.Contains(err.Error(), tc.expected.msg) {
				t.Errorf("err = %v, want it to say %q", err, tc.expected.msg)
			}
			got.err, got.msg = tc.expected.err, tc.expected.msg
			if got != tc.expected {
				t.Errorf("got %+v, want %+v", got, tc.expected)
			}
			if err != nil && exitCodeOf(err) != ExitMinusOne {
				t.Errorf("rc %d, want 255", exitCodeOf(err))
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

// TestLinkXstatsPrint is bridge_print_xstats and bond_print_xstats over
// hand-built replies: the nests each prints, bond's break after the first
// member, the `dev` and body filters, and a failed dump.
//
// go test ./internal/goip/ -run TestLinkXstatsPrint
func TestLinkXstatsPrint(t *testing.T) {
	hdr := func(ifindex uint32) []byte {
		h := make([]byte, xtcpnl.IfStatsMsgSizeCst)
		binary.NativeEndian.PutUint32(h[4:8], ifindex)
		return h
	}
	u64 := func(v uint64) []byte { return binary.NativeEndian.AppendUint64(nil, v) }
	lacp := func(tx uint64) []byte {
		return rtattrBytes(xtcpnl.BondXstats3ad, rtattrBytes(xtcpnl.Bond3adStatLacpduTx, u64(tx)))
	}
	bond := func(at uint16, members ...[]byte) []byte {
		return rtattrBytes(at, rtattrBytes(linkXstatsTypeBond, slices.Concat(members...)))
	}
	stp := rtattrBytes(bridgeXstatsStpCst, make([]byte, xtcpnl.BrStpXstatsSizeCst))
	bridge := func(at uint16, members ...[]byte) []byte {
		return rtattrBytes(at, rtattrBytes(linkXstatsTypeBridge, slices.Concat(members...)))
	}
	const (
		pad    = "                    "
		lacp5  = "if3             \n" + pad + "LACPDU Tx 5\n"
		stpTxt = pad + "STP BPDU:  RX: 0 TX: 0\n" + pad + "STP TCN:   RX: 0 TX: 0\n" +
			pad + "STP Transitions: Blocked: 0 Forwarding: 0\n"
	)
	master, port := uint16(unix.IFLA_STATS_LINK_XSTATS), uint16(unix.IFLA_STATS_LINK_XSTATS_SLAVE)
	tests := []struct {
		description string
		args        string
		json        bool
		src         Source
		expected    string
		rc          int
	}{
		{description: "positive: a bond's 802.3ad counters", args: "type bond",
			src: typedStatsSource{unix.RTM_NEWSTATS: {slices.Concat(hdr(3), bond(master, lacp(5)))}}, expected: lacp5},
		{description: "positive: json, one object per device", args: "type bond", json: true,
			src:      typedStatsSource{unix.RTM_NEWSTATS: {slices.Concat(hdr(3), bond(master, lacp(5)))}},
			expected: `[{"ifname":"if3","802.3ad":{"lacpdu_tx":5}}]` + "\n"},
		{description: "corner: bond looks at its first member alone", args: "type bond",
			src:      typedStatsSource{unix.RTM_NEWSTATS: {slices.Concat(hdr(3), bond(master, lacp(5), lacp(6)))}},
			expected: lacp5},
		{description: "corner: an unknown first member leaves bond with the name alone", args: "type bond",
			src:      typedStatsSource{unix.RTM_NEWSTATS: {slices.Concat(hdr(3), bond(master, rtattrBytes(9, nil), lacp(5)))}},
			expected: "if3             \n"},
		{description: "corner: both nests in one reply print twice", args: "type bond",
			src:      typedStatsSource{unix.RTM_NEWSTATS: {slices.Concat(hdr(3), bond(master, lacp(5)), bond(port, lacp(5)))}},
			expected: lacp5 + lacp5},
		{description: "negative: a nest of the other kind prints nothing", args: "type bridge",
			src: typedStatsSource{unix.RTM_NEWSTATS: {slices.Concat(hdr(3), bond(master, lacp(5)))}}},
		{description: "positive: `stp` drops the other bodies", args: "type bridge_slave stp",
			src: typedStatsSource{unix.RTM_NEWSTATS: {slices.Concat(hdr(3), bridge(port,
				rtattrBytes(bridgeXstatsMcastCst, make([]byte, xtcpnl.BrMcastStatsSizeCst)), stp))}},
			expected: "if3             \n" + stpTxt},
		{description: "positive: `dev` keeps that device's reply alone", args: "type bond dev if4",
			src: typedStatsSource{unix.RTM_NEWSTATS: {slices.Concat(hdr(3), bond(master, lacp(5))),
				slices.Concat(hdr(4), bond(master, lacp(6)))}},
			expected: "if4             \n" + pad + "LACPDU Tx 6\n"},
		{description: "boundary: no replies, and -j an empty array", args: "type bridge", json: true,
			src: typedStatsSource{}, expected: "[]\n"},
		{description: "negative: a short reply ends the dump, which closes the array and exits 255", args: "type bond", json: true,
			src:      typedStatsSource{unix.RTM_NEWSTATS: {make([]byte, xtcpnl.IfStatsMsgSizeCst-1)}},
			expected: "[]\n", rc: ExitMinusOne},
		{description: "negative: a failed dump is the same", args: "type bridge", json: true,
			src: &failSource{}, expected: "[]\n", rc: ExitMinusOne},
		{description: "boundary: help prints on stdout, exit 0, and sends nothing", args: "type bridge help",
			src: &failSource{}, expected: "Usage: ... bridge [ igmp ] [ dev DEVICE ]\n"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var out, errOut bytes.Buffer
			c := &runCtx{out: &out, errOut: &errOut, lltab: NewLLTab(), json: tc.json, src: tc.src}
			c.lltab.Fill([]xtcpnl.LinkInfo{{Index: 3, Name: "if3"}, {Index: 4, Name: "if4"}})
			rc := ExitOK
			if err := linkXstats(c, strings.Fields(tc.args)); err != nil {
				rc = exitCodeOf(err)
			}
			if out.String() != tc.expected || rc != tc.rc {
				t.Errorf("stdout %q\nrc %d\nwant %q, %d", out.String(), rc, tc.expected, tc.rc)
			}
		})
	}
}
