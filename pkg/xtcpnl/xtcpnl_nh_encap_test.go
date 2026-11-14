package xtcpnl

import (
	"encoding/hex"
	"fmt"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

// NX6b (goip-parity-next.md): NHA_ENCAP_TYPE and NHA_ENCAP on reads,
// decoded as a route's RTA_ENCAP is. The captured row is kwnh/ec-001's
// `nexthop add id 51 encap mpls 101 via 192.0.2.1 dev d0`.

// nhEncapSummary is what TestParseNewNexthopEncap compares.
func nhEncapSummary(e *RouteEncap) string {
	switch {
	case e == nil:
		return "none"
	case !e.HasType:
		return fmt.Sprintf("no type, %d bytes", len(e.Raw))
	case e.MPLS != nil:
		s := fmt.Sprintf("type %d, labels %d", e.Type, len(e.MPLS.Labels))
		for _, l := range e.MPLS.Labels {
			s += fmt.Sprintf(" %d", l.Label)
		}
		if e.MPLS.HasTTL {
			s += fmt.Sprintf(", ttl %d", e.MPLS.TTL)
		}
		return s
	case e.Seg6 != nil:
		return fmt.Sprintf("type %d, seg6 mode %d", e.Type, e.Seg6.Mode)
	}
	return fmt.Sprintf("type %d, raw %d bytes", e.Type, len(e.Raw))
}

// go test ./pkg/xtcpnl/ -run TestParseNewNexthopEncap
func TestParseNewNexthopEncap(t *testing.T) {
	const (
		head    = "02fd000000000000" + "0800010033000000" // AF_INET, scope link, id 51
		mplsTyp = "0600070001000000"                      // NHA_ENCAP_TYPE mpls
		ipTyp   = "0600070002000000"                      // NHA_ENCAP_TYPE ip
		lab101  = "0800010000065100"                      // MPLS_IPTUNNEL_DST 101, BoS
		ttl10   = "050002000a000000"                      // MPLS_IPTUNNEL_TTL 10
	)
	tests := []struct {
		description string
		body        string
		expected    string
		wantErr     bool
	}{
		{"positive: mpls 101", head + mplsTyp + "0c000800" + lab101, "type 1, labels 1 101", false},
		{"positive: mpls 101 ttl 10", head + mplsTyp + "14000800" + lab101 + ttl10, "type 1, labels 1 101, ttl 10", false},
		{"positive: the encap before its type", head + "0c000800" + lab101 + mplsTyp, "type 1, labels 1 101", false},
		{"corner: an undecoded type is kept raw", head + ipTyp + "0c000800" + "0800020000000001", "type 2, raw 8 bytes", false},
		{"corner: NHA_ENCAP without NHA_ENCAP_TYPE", head + "0c000800" + lab101, "no type, 8 bytes", false},
		{"boundary: no encap", head, "none", false},
		{"negative: a label stack cut short", head + mplsTyp + "0b000800" + "07000100000651" + "00", "", true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			b, err := hex.DecodeString(tc.body)
			if err != nil {
				t.Fatal(err)
			}
			nh, err := ParseNewNexthop(b)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err %v, wantErr %v", err, tc.wantErr)
			}
			if err != nil {
				return
			}
			if got := nhEncapSummary(nh.Encap); got != tc.expected {
				t.Errorf("got %q, expected %q", got, tc.expected)
			}
		})
	}
}

// TestWsNexthopEncapCorpus reads kwnh/ec-001's captured nexthop.
//
// go test ./pkg/xtcpnl/ -run TestWsNexthopEncapCorpus
func TestWsNexthopEncapCorpus(t *testing.T) {
	p := filepath.Join(tdBase, "7_3_0", "ws", "kwnh", "ec-001", "netlink_route_dump.pcap")
	n := 0
	for _, m := range pcapNlMsgs(t, p) {
		if m.h.Type != unix.RTM_NEWNEXTHOP {
			continue
		}
		nh, err := ParseNewNexthop(m.body)
		if err != nil {
			t.Fatal(err)
		}
		n++
		if got, want := nhEncapSummary(nh.Encap), "type 1, labels 1 101"; got != want {
			t.Errorf("id %d: %q, ip printed %q", nh.ID, got, want)
		}
	}
	if n != 1 {
		t.Errorf("%d nexthops, want 1", n)
	}
}
