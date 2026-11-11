package render

import (
	"encoding/hex"
	"encoding/json"
	"testing"

	"golang.org/x/sys/unix"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// srhOf is an SRH as parse_srh builds it, from hex segment addresses.
func srhOf(t *testing.T, hmac uint32, segs ...string) []byte {
	t.Helper()
	a := make([][16]byte, len(segs))
	for i, s := range segs {
		b, err := hex.DecodeString(s)
		if err != nil || len(b) != 16 {
			t.Fatalf("segment %q", s)
		}
		a[i] = [16]byte(b)
	}
	b, err := xtcpnl.SRH(len(segs), a, hmac)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// go test ./internal/goip/render/ -run TestEncapViewOf
func TestEncapViewOf(t *testing.T) {
	const s1, s2 = "20010db8000900000000000000000001", "20010db8000900000000000000000002"
	raw := []byte{0} // RTA_ENCAP present; the decoded fields are what render
	mpls := func(m xtcpnl.MPLSEncap) *xtcpnl.RouteEncap {
		return &xtcpnl.RouteEncap{Type: unix.LWTUNNEL_ENCAP_MPLS, HasType: true, Raw: raw, MPLS: &m}
	}
	seg6 := func(s xtcpnl.Seg6EncapInfo) *xtcpnl.RouteEncap {
		return &xtcpnl.RouteEncap{Type: unix.LWTUNNEL_ENCAP_SEG6, HasType: true, Raw: raw, Seg6: &s}
	}
	tunsrc, _ := hex.DecodeString(s2)
	tests := []struct {
		description string
		e           *xtcpnl.RouteEncap
		text, json  string // expected; both "" for no view
	}{
		{"positive: mpls, one label", mpls(xtcpnl.MPLSEncap{Labels: []xtcpnl.MPLSLabel{{Label: 100, BoS: true}}}),
			" encap mpls  100 ", `{"encap_type":"mpls","dst":"100"}`},
		{"positive: mpls, a stack and a ttl",
			mpls(xtcpnl.MPLSEncap{Labels: []xtcpnl.MPLSLabel{{Label: 100}, {Label: 200, BoS: true}}, TTL: 10, HasTTL: true}),
			" encap mpls  100/200 ttl 10 ", `{"encap_type":"mpls","dst":"100/200","ttl":10}`},
		{"boundary: mpls ttl 0 still prints", mpls(xtcpnl.MPLSEncap{Labels: []xtcpnl.MPLSLabel{{Label: 100, BoS: true}}, HasTTL: true}),
			" encap mpls  100 ttl 0 ", `{"encap_type":"mpls","dst":"100","ttl":0}`},
		{"positive: seg6 encap, one segment", seg6(xtcpnl.Seg6EncapInfo{Mode: xtcpnl.Seg6ModeEncap, HasSRH: true, SRH: srhOf(t, 0, s1)}),
			" encap seg6 mode encap segs 1 [ 2001:db8:9::1 ] ", `{"encap_type":"seg6","mode":"encap","segs":["2001:db8:9::1"]}`},
		{"positive: seg6 with tunsrc, two segments, hmac and lookup main",
			seg6(xtcpnl.Seg6EncapInfo{Mode: xtcpnl.Seg6ModeInline, HasSRH: true, SRH: srhOf(t, 0x1a, s1, s2),
				TunSrc: tunsrc, Lookup: unix.RT_TABLE_MAIN, HasLookup: true}),
			" encap seg6 mode inline tunsrc 2001:db8:9::2 segs 2 [ 2001:db8:9::1 2001:db8:9::2 ] hmac 1A lookup main ",
			`{"encap_type":"seg6","mode":"inline","tunsrc":"2001:db8:9::2","segs":["2001:db8:9::1","2001:db8:9::2"],"hmac":"0x1a","lookup":"main"}`},
		{"boundary: seg6 lookup 100 prints the number", seg6(xtcpnl.Seg6EncapInfo{Mode: xtcpnl.Seg6ModeL2EncapRed, HasSRH: true, SRH: srhOf(t, 0, s1), Lookup: 100, HasLookup: true}),
			" encap seg6 mode l2encap.red segs 1 [ 2001:db8:9::1 ] lookup 100 ",
			`{"encap_type":"seg6","mode":"l2encap.red","segs":["2001:db8:9::1"],"lookup":"100"}`},
		{"corner: seg6 without its SRH prints the type alone", seg6(xtcpnl.Seg6EncapInfo{}),
			" encap seg6 ", `{"encap_type":"seg6"}`},
		{"corner: seg6 mode past the table is <unknown>", seg6(xtcpnl.Seg6EncapInfo{Mode: 5, HasSRH: true, SRH: srhOf(t, 0, s1)}),
			" encap seg6 mode <unknown> segs 1 [ 2001:db8:9::1 ] ", `{"encap_type":"seg6","mode":"<unknown>","segs":["2001:db8:9::1"]}`},
		{"corner: an SRH too short prints no segments", seg6(xtcpnl.Seg6EncapInfo{Mode: xtcpnl.Seg6ModeEncap, HasSRH: true, SRH: []byte{0, 2, 4, 0, 0}}),
			" encap seg6 mode encap ", `{"encap_type":"seg6","mode":"encap"}`},
		{"negative: an undecoded type prints nothing", &xtcpnl.RouteEncap{Type: unix.LWTUNNEL_ENCAP_IP, HasType: true, Raw: raw}, "", ""},
		{"negative: RTA_ENCAP without RTA_ENCAP_TYPE", &xtcpnl.RouteEncap{Raw: raw}, "", ""},
		{"negative: RTA_ENCAP_TYPE without RTA_ENCAP", &xtcpnl.RouteEncap{Type: unix.LWTUNNEL_ENCAP_MPLS, HasType: true,
			MPLS: &xtcpnl.MPLSEncap{}}, "", ""},
		{"negative: no encap", nil, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v := EncapViewOf(tc.e)
			if v == nil {
				if tc.text != "" {
					t.Fatalf("no view, expected %q", tc.text)
				}
				return
			}
			if got := v.Text(); got != tc.text {
				t.Errorf("text %q, expected %q", got, tc.text)
			}
			js, err := v.MarshalJSON()
			if err != nil || string(js) != tc.json {
				t.Errorf("json %s (%v), expected %s", js, err, tc.json)
			}
		})
	}
}

// TestRouteViewEncapPlacement puts the encap where print_route and
// print_rta_multipath do: after `as to`, and first in a hop.
//
// go test ./internal/goip/render/ -run TestRouteViewEncapPlacement
func TestRouteViewEncapPlacement(t *testing.T) {
	e := &xtcpnl.RouteEncap{Type: unix.LWTUNNEL_ENCAP_MPLS, HasType: true, Raw: []byte{0},
		MPLS: &xtcpnl.MPLSEncap{Labels: []xtcpnl.MPLSLabel{{Label: 100, BoS: true}}}}
	tab := fakeNames{2: {name: "d0"}}
	tests := []struct {
		description string
		ri          xtcpnl.RouteInfo
		text, json  string
	}{
		{"positive: a route's encap before its gateway",
			xtcpnl.RouteInfo{Family: unix.AF_INET, Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT, Dst: []byte{198, 51, 100, 0}, DstLen: 24,
				Gateway: []byte{192, 0, 2, 1}, Oif: 2, Encap: e},
			"198.51.100.0/24  encap mpls  100 via 192.0.2.1 dev d0 \n",
			`{"dst":"198.51.100.0/24","encap":{"encap_type":"mpls","dst":"100"},"gateway":"192.0.2.1","dev":"d0","flags":[]}`},
		{"positive: a hop's encap first",
			xtcpnl.RouteInfo{Family: unix.AF_INET, Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT, Dst: []byte{198, 51, 100, 0}, DstLen: 24,
				Multipath: []xtcpnl.RouteNextHop{{Ifindex: 2, Gateway: []byte{192, 0, 2, 1}, Encap: e}}},
			"198.51.100.0/24 \n\tnexthop  encap mpls  100 via 192.0.2.1 dev d0 weight 1 \n",
			`{"dst":"198.51.100.0/24","flags":[],"nexthops":[{"encap":{"encap_type":"mpls","dst":"100"},"gateway":"192.0.2.1","dev":"d0","weight":1,"flags":[]}]}`},
		{"negative: no encap, no token",
			xtcpnl.RouteInfo{Family: unix.AF_INET, Type: unix.RTN_UNICAST, Protocol: unix.RTPROT_BOOT, Dst: []byte{198, 51, 100, 0}, DstLen: 24, Oif: 2},
			"198.51.100.0/24 dev d0 \n", `{"dst":"198.51.100.0/24","dev":"d0","flags":[]}`},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v := RouteViewOf(tc.ri, tab, RouteShowFilter{})
			if got := v.Text(); got != tc.text {
				t.Errorf("text %q, expected %q", got, tc.text)
			}
			js, err := json.Marshal(v)
			if err != nil || string(js) != tc.json {
				t.Errorf("json %s (%v), expected %s", js, err, tc.json)
			}
		})
	}
}
