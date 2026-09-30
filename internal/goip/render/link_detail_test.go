package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
	"golang.org/x/sys/unix"
)

// u32a is a present U32Attr, so a table row reads as the value it is about
// rather than as a struct literal repeated forty times.
func u32a(v uint32) xtcpnl.U32Attr { return xtcpnl.U32Attr{Value: v, Present: true} }

// guestDetail is the LinkDetail every link in the 7_1_4 clean topology
// reports, give or take the three fields the rows below vary.
//
// It exists so that a row can say what it is ABOUT — "this one has no
// addrgenmode", "this one has a slave kind" — instead of restating fourteen
// counters to get to the one that differs. Transcribed from goip0's line of
// ip_link_n.
func guestDetail() xtcpnl.LinkDetail {
	return xtcpnl.LinkDetail{
		Promiscuity: u32a(0), AllMulti: u32a(0),
		MinMTU: u32a(0), MaxMTU: u32a(0),
		AddrGenMode: xtcpnl.In6AddrGenModeEUI64, HasAddrGenMode: true,
		NumTxQueues: u32a(1), NumRxQueues: u32a(1),
		GSOMaxSize: u32a(65536), GSOMaxSegs: u32a(65535),
		TSOMaxSize: u32a(65536), TSOMaxSegs: u32a(65535),
		GROMaxSize:     u32a(65536),
		GSOIPv4MaxSize: u32a(65536), GROIPv4MaxSize: u32a(65536),
	}
}

// TestLinkDetailText drives the `-d` token run on its own, which is possible
// because detailText continues a line rather than owning one: its output is
// exactly the tokens, so a row's want is readable as the diff `-d` makes.
//
// go test ./internal/goip/render/ -run TestLinkDetailText
func TestLinkDetailText(t *testing.T) {
	tests := []struct {
		description string
		li          xtcpnl.LinkInfo
		linkObject  bool
		want        string
	}{
		{
			// ip_link_n:8, goip0's whole run, with the leading space that
			// separates it from `brd ff:ff:ff:ff:ff:ff` and the trailing one
			// that ends the line.
			description: "positive: a dummy in the guest topology renders the full run on two lines",
			li:          xtcpnl.LinkInfo{Kind: "dummy", Detail: guestDetail()},
			linkObject:  true,
			want: " promiscuity 0 allmulti 0 minmtu 0 maxmtu 0 " +
				"\n    dummy addrgenmode eui64 numtxqueues 1 numrxqueues 1 " +
				"gso_max_size 65536 gso_max_segs 65535 tso_max_size 65536 tso_max_segs 65535 " +
				"gro_max_size 65536 gso_ipv4_max_size 65536 gro_ipv4_max_size 65536 ",
		},
		{
			// THE row the whole nil-versus-zero design exists for, and it is
			// not hypothetical: `ip -d -6 addr show` prints the link/ line and
			// stops, because an AF_INET6 link dump carries none of these
			// attributes. ip_addr_v6_n is this output. A renderer that emitted
			// the run from the FLAG rather than from the attributes would put
			// fourteen zeros here.
			description: "negative: a reply carrying no detail attribute renders nothing at all",
			li:          xtcpnl.LinkInfo{},
			linkObject:  true,
			want:        "",
		},
		{
			// lo: the only link in the corpus with netns-immutable set, and
			// the only one with no IFLA_LINKINFO — so the token that would
			// open a new line does not, and addrgenmode lands on the same line
			// as netns-immutable. ip_link_n:2.
			description: "corner: with no link kind the run stays on one line and netns-immutable precedes addrgenmode",
			li: xtcpnl.LinkInfo{Detail: func() xtcpnl.LinkDetail {
				d := guestDetail()
				d.NetnsImmutable = true
				d.TSOMaxSize = u32a(524280)
				return d
			}()},
			linkObject: true,
			want: " promiscuity 0 allmulti 0 minmtu 0 maxmtu 0 netns-immutable " +
				"addrgenmode eui64 numtxqueues 1 numrxqueues 1 " +
				"gso_max_size 65536 gso_max_segs 65535 tso_max_size 524280 tso_max_segs 65535 " +
				"gro_max_size 65536 gso_ipv4_max_size 65536 gro_ipv4_max_size 65536 ",
		},
		{
			// The `do_link` gate, and the ONE token it controls. `ip -d addr
			// show` prints every other token in this run and not this one,
			// because print_af_spec is guarded on do_link
			// (ip/ipaddress.c:1185-1186). ip_addr_n:9 against ip_link_n:8 is
			// the pair.
			description: "negative: the addr object prints the whole run except addrgenmode",
			li:          xtcpnl.LinkInfo{Kind: "dummy", Detail: guestDetail()},
			linkObject:  false,
			want: " promiscuity 0 allmulti 0 minmtu 0 maxmtu 0 " +
				"\n    dummy numtxqueues 1 numrxqueues 1 " +
				"gso_max_size 65536 gso_max_segs 65535 tso_max_size 65536 tso_max_segs 65535 " +
				"gro_max_size 65536 gso_ipv4_max_size 65536 gro_ipv4_max_size 65536 ",
		},
		{
			// Two kinds, two lines, and the `_slave` suffix that exists only
			// in the text form: the wire attribute holds "bridge".
			// ip_link_n:29-30.
			description: "corner: a bridge port opens two lines and the slave kind gains a _slave the wire never had",
			li: xtcpnl.LinkInfo{
				Kind: "veth", SlaveKind: "bridge",
				Detail: xtcpnl.LinkDetail{Promiscuity: u32a(1), AllMulti: u32a(1)},
			},
			linkObject: true,
			want:       " promiscuity 1 allmulti 1 \n    veth \n    bridge_slave ",
		},
		{
			// The leading space belongs to promiscuity alone, so a reply that
			// omits it and carries allmulti runs the token straight into
			// whatever preceded it. iproute2 does exactly this, and the row
			// exists so that tidying the asymmetry away fails a test rather
			// than passing quietly on a corpus where promiscuity is always
			// present.
			description: "boundary: without promiscuity the run opens with no leading space",
			li:          xtcpnl.LinkInfo{Detail: xtcpnl.LinkDetail{AllMulti: u32a(0)}},
			linkObject:  true,
			want:        "allmulti 0 ",
		},
		{
			// The physical tail, in print order, with the two hex tokens.
			// ip_link_n:6 — enp35s0f0np0, the only link in the corpus with a
			// switch id.
			description: "positive: the physical tail renders portname, hex ids and the parent device in print order",
			li: xtcpnl.LinkInfo{Detail: xtcpnl.LinkDetail{
				PhysPortName:     "p0",
				PhysPortID:       []byte{0x01, 0x0f},
				PhysSwitchID:     []byte{0xd0, 0xd8, 0xcf, 0xff, 0xff, 0x73, 0x09, 0x04},
				ParentDevBusName: "pci",
				ParentDevName:    "0000:23:00.0",
			}},
			linkObject: true,
			want:       "portname p0 portid 010f switchid d0d8cfffff730904 parentbus pci parentdev 0000:23:00.0 ",
		},
		{
			// hexstring_n2a is "%02x" per byte with no separator
			// (lib/utils.c:1174-1187), so a leading zero byte must survive as
			// "00" rather than collapsing. Go's hex.EncodeToString does the
			// same thing, and this row is what says so.
			description: "boundary: a hex id with a leading zero byte keeps both nibbles",
			li:          xtcpnl.LinkInfo{Detail: xtcpnl.LinkDetail{PhysPortID: []byte{0x00, 0x00, 0x0a}}},
			linkObject:  true,
			want:        "portid 00000a ",
		},
		{
			// A u32 at its maximum. The renderer formats with %d and the
			// decoded type is uint32, so a signed conversion anywhere on the
			// path prints -1 instead.
			description: "corner: a counter at 0xFFFFFFFF renders unsigned",
			li:          xtcpnl.LinkInfo{Detail: xtcpnl.LinkDetail{GSOMaxSize: u32a(4294967295)}},
			linkObject:  true,
			want:        "gso_max_size 4294967295 ",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v := LinkView{}.WithDetail(tc.li, tc.linkObject)
			if got := v.detailText(); got != tc.want {
				t.Errorf("detailText() =\n  %q\nwant\n  %q", got, tc.want)
			}
		})
	}
}

// TestLinkViewWithoutDetailPrintsNone is the negative control for the whole
// file: the flag, not the data, is what turns the run on.
//
// Every link in the corpus carries these attributes, because -d changes no
// request byte — so a renderer keyed on "did the reply have them" would print
// the detail run for plain `ip link show` too, and every plain golden in the
// corpus would fail at once. This asserts the opposite directly, on a link
// whose Detail is fully populated.
//
// go test ./internal/goip/render/ -run TestLinkViewWithoutDetailPrintsNone
func TestLinkViewWithoutDetailPrintsNone(t *testing.T) {
	li := xtcpnl.LinkInfo{
		Index: 3, Name: "goip0", Type: unix.ARPHRD_ETHER,
		Kind: "dummy", Detail: guestDetail(),
	}
	names := fakeNames{}

	tests := []struct {
		description string
		view        LinkView
		wantContain bool
	}{
		{
			description: "negative: a view built without WithDetail prints no detail token, though the data is there",
			view:        LinkViewOf(li, names),
			wantContain: false,
		},
		{
			description: "positive: the same link through WithDetail prints them",
			view:        LinkViewOf(li, names).WithDetail(li, true),
			wantContain: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := strings.Contains(tc.view.Text(), "promiscuity")
			if got != tc.wantContain {
				t.Errorf("Text() contains %q = %v, want %v\ntext: %q",
					"promiscuity", got, tc.wantContain, tc.view.Text())
			}
		})
	}
}

// TestAddrGenModeName covers print_af_spec's switch including the arm a kernel
// newer than the pinned iproute2 will take (ip/ipaddress.c:171-202).
//
// go test ./internal/goip/render/ -run TestAddrGenModeName
func TestAddrGenModeName(t *testing.T) {
	tests := []struct {
		description string
		mode        uint8
		want        string
	}{
		{
			description: "positive: eui64, which is zero and is what nearly every link in the corpus reports",
			mode:        xtcpnl.In6AddrGenModeEUI64,
			want:        "eui64",
		},
		{
			// enp1s0 in the 7_1_8 dump, the only non-eui64 link in the corpus.
			description: "positive: none, the one other mode the corpus contains",
			mode:        xtcpnl.In6AddrGenModeNone,
			want:        "none",
		},
		{
			// The token and the enum disagree: IN6_ADDR_GEN_MODE_STABLE_PRIVACY
			// prints "stable_secret". Transcribing the enum name here would be
			// a plausible-looking divergence on a machine using it.
			description: "corner: stable_privacy prints as stable_secret, not as its enum name",
			mode:        xtcpnl.In6AddrGenModeStablePrivacy,
			want:        "stable_secret",
		},
		{
			description: "positive: random",
			mode:        xtcpnl.In6AddrGenModeRandom,
			want:        "random",
		},
		{
			// The default arm: snprintf("%#.2hhx"). Four is one past the last
			// named mode, so this is the boundary as well as the fallback.
			description: "boundary: one past the last named mode prints as two-digit hex with a 0x",
			mode:        4,
			want:        "0x04",
		},
		{
			description: "corner: the widest a u8 goes still renders two digits and a prefix",
			mode:        255,
			want:        "0xff",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := addrGenModeName(tc.mode); got != tc.want {
				t.Errorf("addrGenModeName(%d) = %q, want %q", tc.mode, got, tc.want)
			}
		})
	}
}

// TestLinkDetailJSONKeys asserts the `-j -d` key names and, more importantly,
// where they sit.
//
// iproute2 emits every one of these at the TOP level of the link object rather
// than under a wrapper, which is why LinkDetailView is embedded anonymously —
// a named field would nest them one level down and diverge on every key at
// once, silently, because each key would still be spelled correctly.
//
// go test ./internal/goip/render/ -run TestLinkDetailJSONKeys
func TestLinkDetailJSONKeys(t *testing.T) {
	tests := []struct {
		description string
		view        LinkView
		wantKeys    []string
		absentKeys  []string
	}{
		{
			description: "positive: the counters are top-level keys of the link object, not nested under a wrapper",
			view: LinkView{IfIndex: 3, IfName: "goip0"}.
				WithDetail(xtcpnl.LinkInfo{Kind: "dummy", Detail: guestDetail()}, true),
			wantKeys: []string{
				`"promiscuity":0`, `"allmulti":0`, `"min_mtu":0`, `"max_mtu":0`,
				`"num_tx_queues":1`, `"gso_max_size":65536`, `"gro_ipv4_max_size":65536`,
				`"inet6_addr_gen_mode":"eui64"`,
			},
			absentKeys: []string{`"detail"`, `"LinkDetailView"`},
		},
		{
			// print_linktype opens a JSON object where the text form opens a
			// line (ip/ipaddress.c:219), so this is the one part of the run
			// that IS nested — and the slave kind inside it is the unsuffixed
			// wire value, because print_string's JSON arm writes the argument
			// and not the format.
			description: "corner: linkinfo is a nested object and its slave kind carries no _slave suffix",
			view: LinkView{IfIndex: 60, IfName: "veth179a698"}.
				WithDetail(xtcpnl.LinkInfo{Kind: "veth", SlaveKind: "bridge"}, true),
			wantKeys:   []string{`"linkinfo":{"info_kind":"veth","info_slave_kind":"bridge"}`},
			absentKeys: []string{`"bridge_slave"`},
		},
		{
			// omitempty on a NON-NIL pointer to zero keeps the key, which is
			// the only reason these are pointers. lo really does report
			// promiscuity 0, and `"promiscuity":0` is what `ip -j -d` emits
			// for it.
			description: "boundary: a zero counter keeps its key, because the pointer is not nil",
			view: LinkView{IfIndex: 1, IfName: "lo"}.
				WithDetail(xtcpnl.LinkInfo{Detail: xtcpnl.LinkDetail{Promiscuity: u32a(0)}}, true),
			wantKeys: []string{`"promiscuity":0`},
		},
		{
			// The same field absent. Together with the row above this is the
			// whole nil-versus-zero contract, stated in the output rather
			// than in the struct.
			description: "negative: an absent counter has no key at all",
			view: LinkView{IfIndex: 1, IfName: "lo"}.
				WithDetail(xtcpnl.LinkInfo{Detail: xtcpnl.LinkDetail{AllMulti: u32a(0)}}, true),
			wantKeys:   []string{`"allmulti":0`},
			absentKeys: []string{`"promiscuity"`},
		},
		{
			// netns-immutable is the one key here with a dash in it, and it is
			// a bool rather than a pointer because iproute2 tests the value
			// (:1176-1179) — so false and absent really are one state and
			// omitempty is right for it where it would be wrong above.
			description: "corner: netns-immutable keeps its dash and disappears when false",
			view: LinkView{IfIndex: 1, IfName: "lo"}.
				WithDetail(xtcpnl.LinkInfo{Detail: xtcpnl.LinkDetail{NetnsImmutable: true}}, true),
			wantKeys:   []string{`"netns-immutable":true`},
			absentKeys: []string{`"netns_immutable"`},
		},
		{
			description: "negative: a view with no detail emits none of the keys and stays a valid link object",
			view:        LinkView{IfIndex: 1, IfName: "lo"},
			wantKeys:    []string{`"ifindex":1`, `"ifname":"lo"`},
			absentKeys: []string{
				`"promiscuity"`, `"linkinfo"`, `"inet6_addr_gen_mode"`,
				`"netns-immutable"`, `"parentbus"`,
			},
		},
		{
			description: "positive: the physical tail uses parentbus and parentdev, which are not the attribute names",
			view: LinkView{IfIndex: 3, IfName: "enp35s0f0np0"}.
				WithDetail(xtcpnl.LinkInfo{Detail: xtcpnl.LinkDetail{
					PhysPortName:     "p0",
					PhysSwitchID:     []byte{0xd0, 0xd8},
					ParentDevBusName: "pci",
					ParentDevName:    "0000:23:00.0",
				}}, true),
			wantKeys: []string{
				`"phys_port_name":"p0"`, `"phys_switch_id":"d0d8"`,
				`"parentbus":"pci"`, `"parentdev":"0000:23:00.0"`,
			},
			absentKeys: []string{`"phys_port_id"`, `"parent_dev_name"`},
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			b, err := json.Marshal(tc.view)
			if err != nil {
				t.Fatalf("Marshal: %v", err)
			}
			got := string(b)
			for _, k := range tc.wantKeys {
				if !strings.Contains(got, k) {
					t.Errorf("missing %s in\n  %s", k, got)
				}
			}
			for _, k := range tc.absentKeys {
				if strings.Contains(got, k) {
					t.Errorf("unexpected %s in\n  %s", k, got)
				}
			}
		})
	}
}
