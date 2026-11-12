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
	ip := func(typ uint16, e xtcpnl.IPEncapInfo) *xtcpnl.RouteEncap {
		return &xtcpnl.RouteEncap{Type: typ, HasType: true, Raw: raw, IP: &e}
	}
	dst4 := xtcpnl.IPEncapInfo{ID: 1, Dst: []byte{203, 0, 113, 5}, HasID: true}
	opts := func(o xtcpnl.IPEncapOpts) xtcpnl.IPEncapInfo { e := dst4; e.Opts = &o; return e }
	tunsrc, _ := hex.DecodeString(s2)
	ila := func(e xtcpnl.IlaEncapInfo) *xtcpnl.RouteEncap {
		return &xtcpnl.RouteEncap{Type: unix.LWTUNNEL_ENCAP_ILA, HasType: true, Raw: raw, Ila: &e}
	}
	xfrm := func(e xtcpnl.XfrmEncapInfo) *xtcpnl.RouteEncap {
		return &xtcpnl.RouteEncap{Type: unix.LWTUNNEL_ENCAP_XFRM, HasType: true, Raw: raw, Xfrm: &e}
	}
	const loc = 0x2001_0000_0000_0002
	tab := fakeNames{4: {name: "d0"}}
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
		{"positive: ip with every scalar and key csum seq",
			ip(unix.LWTUNNEL_ENCAP_IP, xtcpnl.IPEncapInfo{ID: 1, Src: []byte{192, 0, 2, 10}, Dst: []byte{203, 0, 113, 5}, TTL: 64, Tos: 16,
				Flags: 0x0d, HasID: true, HasTTL: true, HasTos: true, HasFlags: true}),
			" encap ip id 1 src 192.0.2.10 dst 203.0.113.5 ttl 64 tos 16 key csum seq ",
			`{"encap_type":"ip","id":1,"src":"192.0.2.10","dst":"203.0.113.5","ttl":64,"tos":16,"key":true,"csum":true,"seq":true}`},
		{"positive: ip6 names them hoplimit and tc",
			ip(unix.LWTUNNEL_ENCAP_IP6, xtcpnl.IPEncapInfo{ID: 1, Src: make([]byte, 16), Dst: tunsrc, TTL: 64, Tos: 16, Flags: 0x04,
				HasID: true, HasTTL: true, HasTos: true, HasFlags: true}),
			" encap ip6 id 1 src :: dst 2001:db8:9::2 hoplimit 64 tc 16 key ",
			`{"encap_type":"ip6","id":1,"src":"::","dst":"2001:db8:9::2","hoplimit":64,"tc":16,"key":true}`},
		{"boundary: id 2^64-1, and the kernel's GENEVE_OPT flag alone prints no flag",
			ip(unix.LWTUNNEL_ENCAP_IP, xtcpnl.IPEncapInfo{ID: 1<<64 - 1, Flags: 0x0800, HasID: true, HasFlags: true}),
			" encap ip id 18446744073709551615 ", `{"encap_type":"ip","id":18446744073709551615}`},
		{"positive: two geneve options, a comma between, a space after the last",
			ip(unix.LWTUNNEL_ENCAP_IP, opts(xtcpnl.IPEncapOpts{Kind: xtcpnl.LwtunnelIPOptsGeneve, Geneve: []xtcpnl.GeneveOpt{
				{Class: 0x0102, Type: 0x80, Data: []byte{0, 0x11, 0x22, 0x33}}, {Class: 0x0102, Type: 0x81, Data: []byte{0x44, 0x55, 0x66, 0x77}}}})),
			" encap ip id 1 dst 203.0.113.5 \n\tgeneve_opts 258:128:00112233,258:129:44556677 ",
			`{"encap_type":"ip","id":1,"dst":"203.0.113.5","geneve_opts":[{"class":258,"type":128,"data":"00112233"},{"class":258,"type":129,"data":"44556677"}]}`},
		{"corner: an empty geneve nest prints the keyword alone",
			ip(unix.LWTUNNEL_ENCAP_IP, opts(xtcpnl.IPEncapOpts{Kind: xtcpnl.LwtunnelIPOptsGeneve})),
			" encap ip id 1 dst 203.0.113.5 \n\tgeneve_opts ", `{"encap_type":"ip","id":1,"dst":"203.0.113.5","geneve_opts":[]}`},
		{"positive: vxlan_opts gbp",
			ip(unix.LWTUNNEL_ENCAP_IP, opts(xtcpnl.IPEncapOpts{Kind: xtcpnl.LwtunnelIPOptsVxlan, VxlanGBP: 2048})),
			" encap ip id 1 dst 203.0.113.5 \n\tvxlan_opts 2048 ", `{"encap_type":"ip","id":1,"dst":"203.0.113.5","vxlan_opts":[{"gbp":2048}]}`},
		{"positive: erspan version 1 prints its index, and 0 for direction and hardware id",
			ip(unix.LWTUNNEL_ENCAP_IP, opts(xtcpnl.IPEncapOpts{Kind: xtcpnl.LwtunnelIPOptsErspan, Erspan: xtcpnl.ErspanOpt{Ver: 1, Index: 0x1234, Dir: 1, Hwid: 7}})),
			" encap ip id 1 dst 203.0.113.5 \n\terspan_opts 1:4660:0:0 ",
			`{"encap_type":"ip","id":1,"dst":"203.0.113.5","erspan_opts":[{"ver":1,"index":4660,"dir":0,"hwid":0}]}`},
		{"positive: erspan version 2 prints 0 for its index",
			ip(unix.LWTUNNEL_ENCAP_IP, opts(xtcpnl.IPEncapOpts{Kind: xtcpnl.LwtunnelIPOptsErspan, Erspan: xtcpnl.ErspanOpt{Ver: 2, Index: 0x1234, Dir: 1, Hwid: 7}})),
			" encap ip id 1 dst 203.0.113.5 \n\terspan_opts 2:0:1:7 ",
			`{"encap_type":"ip","id":1,"dst":"203.0.113.5","erspan_opts":[{"ver":2,"index":0,"dir":1,"hwid":7}]}`},
		{"corner: an opts nest of no known kind prints nothing",
			ip(unix.LWTUNNEL_ENCAP_IP, opts(xtcpnl.IPEncapOpts{})),
			" encap ip id 1 dst 203.0.113.5 ", `{"encap_type":"ip","id":1,"dst":"203.0.113.5"}`},
		{"corner: ip with no attributes prints the type alone", ip(unix.LWTUNNEL_ENCAP_IP, xtcpnl.IPEncapInfo{}),
			" encap ip ", `{"encap_type":"ip"}`},
		{"positive: ila, every attribute, as the kernel fills them",
			ila(xtcpnl.IlaEncapInfo{Locator: loc, CsumMode: xtcpnl.ILACsumNoAction, IdentType: xtcpnl.ILAAtypeLUID,
				HookType: xtcpnl.ILAHookRouteOutput, HasLocator: true, HasCsumMode: true, HasIdentType: true, HasHookType: true}),
			" encap ila  2001:0:0:2  csum-mode no-action  ident-type luid  hook-type output ",
			`{"encap_type":"ila","locator":"2001:0:0:2","csum_mode":"no-action","ident_type":"luid","hook_type":"output"}`},
		{"positive: ila adj-transport and the input hook",
			ila(xtcpnl.IlaEncapInfo{Locator: loc, HookType: xtcpnl.ILAHookRouteInput, HasLocator: true, HasCsumMode: true, HasHookType: true}),
			" encap ila  2001:0:0:2  csum-mode adj-transport  hook-type input ",
			`{"encap_type":"ila","locator":"2001:0:0:2","csum_mode":"adj-transport","hook_type":"input"}`},
		{"boundary: ila locator all ones and all zeros groups",
			ila(xtcpnl.IlaEncapInfo{Locator: 0xffff_0000_ffff_0000, HasLocator: true}),
			" encap ila  ffff:0:ffff:0 ", `{"encap_type":"ila","locator":"ffff:0:ffff:0"}`},
		{"corner: ila values with no name print unknown",
			ila(xtcpnl.IlaEncapInfo{CsumMode: 9, IdentType: 9, HookType: 9, HasCsumMode: true, HasIdentType: true, HasHookType: true}),
			" encap ila  csum-mode unknown  ident-type unknown  hook-type unknown ",
			`{"encap_type":"ila","csum_mode":"unknown","ident_type":"unknown","hook_type":"unknown"}`},
		{"corner: ila with no attributes prints the type alone", ila(xtcpnl.IlaEncapInfo{}), " encap ila ", `{"encap_type":"ila"}`},
		{"positive: xfrm if_id alone", xfrm(xtcpnl.XfrmEncapInfo{IfID: 7, HasIfID: true}),
			" encap xfrm if_id 7 ", `{"encap_type":"xfrm","if_id":7}`},
		{"positive: xfrm if_id and a cached link_dev", xfrm(xtcpnl.XfrmEncapInfo{IfID: 1, Link: 4, HasIfID: true, HasLink: true}),
			" encap xfrm if_id 1 link_dev d0 ", `{"encap_type":"xfrm","if_id":1,"link_dev":"d0"}`},
		{"boundary: xfrm if_id 4294967295 and link 0, which ll_index_to_name names *",
			xfrm(xtcpnl.XfrmEncapInfo{IfID: 4294967295, HasIfID: true, HasLink: true}),
			" encap xfrm if_id 4294967295 link_dev * ", `{"encap_type":"xfrm","if_id":4294967295,"link_dev":"*"}`},
		{"corner: xfrm link not in the cache prints if%u", xfrm(xtcpnl.XfrmEncapInfo{Link: 99, HasLink: true}),
			" encap xfrm link_dev if99 ", `{"encap_type":"xfrm","link_dev":"if99"}`},
		{"negative: an undecoded type prints nothing", &xtcpnl.RouteEncap{Type: unix.LWTUNNEL_ENCAP_RPL, HasType: true, Raw: raw}, "", ""},
		{"negative: RTA_ENCAP without RTA_ENCAP_TYPE", &xtcpnl.RouteEncap{Raw: raw}, "", ""},
		{"negative: RTA_ENCAP_TYPE without RTA_ENCAP", &xtcpnl.RouteEncap{Type: unix.LWTUNNEL_ENCAP_MPLS, HasType: true,
			MPLS: &xtcpnl.MPLSEncap{}}, "", ""},
		{"negative: no encap", nil, "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v := EncapViewOf(tc.e, tab)
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
