package goip

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/goip/model"
	"github.com/randomizedcoder/xtcp2/internal/goip/render"
	"github.com/randomizedcoder/xtcp2/pkg/nlparity"
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
		wantErr     error
	}{
		{"positive: no argument lists every device", "", nil},
		{"negative: `dev` resolves a name no capture grounds", "dev br0", ErrNotImplemented},
		{"negative: `dev` is strcmp, so `de` is unknown", "de br0", ErrInvalidArgument},
		{"negative: an unknown word", "bogus", ErrInvalidArgument},
		{"boundary: `help` is usage", "help", ErrNotImplemented},
		{"corner: `h` is help by matches()", "h", ErrNotImplemented},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if err := parseLinkAfstatsArgs(strings.Fields(tc.args)); !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
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

// TestAfstatsUngrounded is the refusal of a record ip would print for.
//
// go test ./internal/goip/ -run TestAfstatsUngrounded
func TestAfstatsUngrounded(t *testing.T) {
	const afMPLS = 28
	empty := model.IfStats{Ifindex: 2, HasAfSpec: true}
	mpls := model.IfStats{Ifindex: 3, HasAfSpec: true, AfSpec: []uint16{afMPLS}}
	tests := []struct {
		description string
		records     []model.IfStats
		family      uint8
		wantErr     error
	}{
		{"positive: empty nests print nothing and pass", []model.IfStats{empty, empty}, 0, nil},
		{"negative: an MPLS member with no family asked is refused", []model.IfStats{empty, mpls}, 0, ErrNotImplemented},
		{"negative: -M asks for that family and is refused", []model.IfStats{mpls}, afMPLS, ErrNotImplemented},
		{"corner: -4 filters the MPLS member out, so nothing prints", []model.IfStats{mpls}, 2, nil},
		{"boundary: no records at all", nil, 0, nil},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if err := afstatsUngrounded(tc.records, tc.family); !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
