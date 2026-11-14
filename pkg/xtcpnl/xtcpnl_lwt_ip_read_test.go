package xtcpnl

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"reflect"
	"testing"
)

// NX6c: print_encap_ip and print_encap_ip6's read of an RTA_ENCAP payload,
// for route and nexthop show. The captured rows are lwtroute4's and
// lwtroute6's dumps; the rest are hand-built.

// ipEncapCaptured are lwtroute4's and lwtroute6's encaps as the kernel
// dumped them, for TestParseIPEncap, the fuzz seeds and the benchmark.
var ipEncapCaptured = map[string]string{
	// encap ip id 1 dst 203.0.113.5
	"lwtroute4/ec-004": "0c000100000000000000000108000200cb0071050800030000000000050005000000000005000400000000000600060000000000",
	// … src 192.0.2.10 tos 0x10 ttl 64 key csum seq
	"lwtroute4/ec-005": "0c000100000000000000000108000200cb00710508000300c000020a0500050010000000050004004000000006000600000d0000",
	// … geneve_opts 0x0102:0x80:00112233
	"lwtroute4/ec-006": "0c000100000000000000000108000200cb0071050800030000000000050005000000000005000400000000000600060008000000200008001c000100060001000102000005000200800000000800030000112233",
	// … geneve_opts 0x0102:0x80:00112233,0x0102:0x81:44556677
	"lwtroute4/ec-008": "0c000100000000000000000108000200cb00710508000300000000000500050000000000050004000000000006000600080000003800080034000100060001000102000005000200800000000800030000112233060001000102000005000200810000000800030044556677",
	// … vxlan_opts 0x800
	"lwtroute4/ec-009": "0c000100000000000000000108000200cb0071050800030000000000050005000000000005000400000000000600060010000000100008000c0002000800010000080000",
	// … erspan_opts 1:0x1234::
	"lwtroute4/ec-010": "0c000100000000000000000108000200cb0071050800030000000000050005000000000005000400000000000600060040000000180008001400030005000100010000000800020000001234",
	// encap ip6 id 1 dst 2001:db8:9::1 src 2001:db8:1::10 tc 0x10 hoplimit 64 key csum seq
	"lwtroute6/ec-002": "0c00010000000000000000011400020020010db80009000000000000000000011400030020010db80001000000000000000000100500050010000000050004004000000006000600000d0000",
}

func be16b(v uint16) []byte { return binary.BigEndian.AppendUint16(nil, v) }
func be32b(v uint32) []byte { return binary.BigEndian.AppendUint32(nil, v) }
func le32b(v uint32) []byte { return binary.LittleEndian.AppendUint32(nil, v) }

// geneveOptAttrs is one geneve option's class, type and data.
func geneveOptAttrs(class uint16, typ uint8, data []byte) []byte {
	return concat(rtattr(LwtunnelIPOptGeneveClass, be16b(class)),
		rtattr(LwtunnelIPOptGeneveType, []byte{typ}),
		rtattr(LwtunnelIPOptGeneveData, data))
}

// ipEncapOpts is an LWTUNNEL_IP_OPTS nest holding kinds.
func ipEncapOpts(kinds ...[]byte) []byte { return rtattr(LwtunnelIPOpts, concat(kinds...)) }

func TestParseIPEncap(t *testing.T) {
	v4 := func(a, b, c, d byte) []byte { return []byte{a, b, c, d} }
	// The captures' common head: id 1, dst 203.0.113.5, src 0.0.0.0, ttl 0
	// and tos 0, and the flags.
	head := func(flags uint16) IPEncapInfo {
		return IPEncapInfo{ID: 1, Dst: v4(203, 0, 113, 5), Src: v4(0, 0, 0, 0), Flags: flags,
			HasID: true, HasTTL: true, HasTos: true, HasFlags: true}
	}
	withOpts := func(e IPEncapInfo, o IPEncapOpts) IPEncapInfo { e.Opts = &o; return e }
	geneve := func(class uint16, typ uint8, data string) GeneveOpt {
		d, _ := hex.DecodeString(data)
		return GeneveOpt{Class: class, Type: typ, Data: d, HasClass: true, HasType: true, HasData: true}
	}
	tests := []struct {
		description string
		captured    string // a key of ipEncapCaptured, or ""
		encap       []byte // when captured is ""
		wantErr     error  // errors.Is, or nil
		expected    IPEncapInfo
	}{
		{
			description: "positive: captured id and dst, the kernel's zero src, ttl and tos",
			captured:    "lwtroute4/ec-004",
			expected:    head(0),
		},
		{
			description: "positive: captured src, tos, ttl and key csum seq",
			captured:    "lwtroute4/ec-005",
			expected: IPEncapInfo{ID: 1, Dst: v4(203, 0, 113, 5), Src: v4(192, 0, 2, 10), TTL: 64, Tos: 0x10, Flags: 0x0d,
				HasID: true, HasTTL: true, HasTos: true, HasFlags: true},
		},
		{
			description: "positive: captured geneve_opts, one option; the kernel's GENEVE_OPT flag",
			captured:    "lwtroute4/ec-006",
			expected:    withOpts(head(0x0800), IPEncapOpts{Kind: LwtunnelIPOptsGeneve, Geneve: []GeneveOpt{geneve(0x0102, 0x80, "00112233")}}),
		},
		{
			description: "positive: captured geneve_opts, two options",
			captured:    "lwtroute4/ec-008",
			expected: withOpts(head(0x0800), IPEncapOpts{Kind: LwtunnelIPOptsGeneve, Geneve: []GeneveOpt{
				geneve(0x0102, 0x80, "00112233"), geneve(0x0102, 0x81, "44556677")}}),
		},
		{
			description: "positive: captured vxlan_opts gbp 0x800",
			captured:    "lwtroute4/ec-009",
			expected:    withOpts(head(0x1000), IPEncapOpts{Kind: LwtunnelIPOptsVxlan, VxlanGBP: 0x800}),
		},
		{
			description: "positive: captured erspan_opts version 1, index 0x1234",
			captured:    "lwtroute4/ec-010",
			expected: withOpts(head(0x4000), IPEncapOpts{Kind: LwtunnelIPOptsErspan,
				Erspan: ErspanOpt{Ver: 1, Index: 0x1234, HasVer: true, HasIndex: true}}),
		},
		{
			description: "positive: captured ip6 with hoplimit, tc and flags",
			captured:    "lwtroute6/ec-002",
			expected: IPEncapInfo{ID: 1,
				Dst: []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0x09, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
				Src: []byte{0x20, 0x01, 0x0d, 0xb8, 0, 0x01, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x10},
				TTL: 64, Tos: 0x10, Flags: 0x0d, HasID: true, HasTTL: true, HasTos: true, HasFlags: true},
		},
		{
			description: "positive: erspan version 2, direction and hardware id",
			encap: ipEncapOpts(rtattr(LwtunnelIPOptsErspan, concat(rtattr(LwtunnelIPOptErspanVer, []byte{2}),
				rtattr(LwtunnelIPOptErspanDir, []byte{1}), rtattr(LwtunnelIPOptErspanHwid, []byte{7})))),
			expected: IPEncapInfo{Opts: &IPEncapOpts{Kind: LwtunnelIPOptsErspan,
				Erspan: ErspanOpt{Ver: 2, Dir: 1, Hwid: 7, HasVer: true, HasDir: true, HasHwid: true}}},
		},
		{
			description: "boundary: an empty payload is an empty encap",
			encap:       []byte{},
			expected:    IPEncapInfo{},
		},
		{
			description: "boundary: an id one byte short is dropped",
			encap:       rtattr(LwtunnelIPID, make([]byte, 7)),
			expected:    IPEncapInfo{},
		},
		{
			description: "boundary: an empty ttl and a one-byte flags are dropped",
			encap:       concat(rtattr(LwtunnelIPTTL, nil), rtattr(LwtunnelIPFlags, []byte{4})),
			expected:    IPEncapInfo{},
		},
		{
			description: "corner: the first of a repeated id wins, as in parse_rtattr",
			encap:       concat(rtattr(LwtunnelIPID, binary.BigEndian.AppendUint64(nil, 7)), rtattr(LwtunnelIPID, binary.BigEndian.AppendUint64(nil, 9))),
			expected:    IPEncapInfo{ID: 7, HasID: true},
		},
		{
			description: "corner: geneve wins over a vxlan sent before it",
			encap: ipEncapOpts(rtattr(LwtunnelIPOptsVxlan, rtattr(LwtunnelIPOptVxlanGBP, le32b(5))),
				rtattr(LwtunnelIPOptsGeneve, geneveOptAttrs(1, 2, []byte{0xaa, 0xbb, 0xcc, 0xdd}))),
			expected: IPEncapInfo{Opts: &IPEncapOpts{Kind: LwtunnelIPOptsGeneve, Geneve: []GeneveOpt{geneve(1, 2, "aabbccdd")}}},
		},
		{
			description: "corner: an opts nest of no known kind prints nothing",
			encap:       ipEncapOpts(rtattr(9, []byte{1, 2, 3, 4})),
			expected:    IPEncapInfo{Opts: &IPEncapOpts{}},
		},
		{
			description: "corner: a geneve option with empty data, a 20-byte stride",
			encap:       ipEncapOpts(rtattr(LwtunnelIPOptsGeneve, concat(geneveOptAttrs(1, 2, nil), geneveOptAttrs(3, 4, nil)))),
			expected: IPEncapInfo{Opts: &IPEncapOpts{Kind: LwtunnelIPOptsGeneve, Geneve: []GeneveOpt{
				{Class: 1, Type: 2, HasClass: true, HasType: true, HasData: true},
				{Class: 3, Type: 4, HasClass: true, HasType: true, HasData: true}}}},
		},
		{
			description: "corner: an empty geneve nest, which ip prints as the keyword alone",
			encap:       ipEncapOpts(rtattr(LwtunnelIPOptsGeneve, nil)),
			expected:    IPEncapInfo{Opts: &IPEncapOpts{Kind: LwtunnelIPOptsGeneve}},
		},
		{
			description: "negative: a geneve option without its data",
			encap: ipEncapOpts(rtattr(LwtunnelIPOptsGeneve, concat(rtattr(LwtunnelIPOptGeneveClass, be16b(1)),
				rtattr(LwtunnelIPOptGeneveType, []byte{2})))),
			wantErr: ErrIPEncapOpts,
		},
		{
			description: "negative: the class last and unpadded, so the stride runs past the end",
			encap: ipEncapOpts(rtattr(LwtunnelIPOptsGeneve, concat(rtattr(LwtunnelIPOptGeneveData, []byte{1, 2, 3, 4}),
				rtattr(LwtunnelIPOptGeneveType, []byte{2}), rtattr(LwtunnelIPOptGeneveClass, be16b(1))[:6]))),
			wantErr: ErrIPEncapOpts,
		},
		{
			description: "negative: vxlan_opts without its gbp",
			encap:       ipEncapOpts(rtattr(LwtunnelIPOptsVxlan, rtattr(9, le32b(5)))),
			wantErr:     ErrIPEncapOpts,
		},
		{
			description: "negative: vxlan_opts with a short gbp",
			encap:       ipEncapOpts(rtattr(LwtunnelIPOptsVxlan, rtattr(LwtunnelIPOptVxlanGBP, []byte{5, 0}))),
			wantErr:     ErrIPEncapOpts,
		},
		{
			description: "negative: erspan version 1 without its index",
			encap:       ipEncapOpts(rtattr(LwtunnelIPOptsErspan, rtattr(LwtunnelIPOptErspanVer, []byte{1}))),
			wantErr:     ErrIPEncapOpts,
		},
		{
			description: "negative: erspan version 2 without its hardware id",
			encap: ipEncapOpts(rtattr(LwtunnelIPOptsErspan, concat(rtattr(LwtunnelIPOptErspanVer, []byte{2}),
				rtattr(LwtunnelIPOptErspanDir, []byte{1})))),
			wantErr: ErrIPEncapOpts,
		},
		{
			description: "negative: erspan_opts without a version",
			encap:       ipEncapOpts(rtattr(LwtunnelIPOptsErspan, rtattr(LwtunnelIPOptErspanIndex, be32b(1)))),
			wantErr:     ErrIPEncapOpts,
		},
		{
			description: "negative: an attribute overrunning the payload",
			encap:       concat(rtattr(LwtunnelIPTTL, []byte{1}), []byte{0x40, 0, 2, 0}),
			wantErr:     ErrRTAttrSmall,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			encap := tc.encap
			if tc.captured != "" {
				var err error
				if encap, err = hex.DecodeString(ipEncapCaptured[tc.captured]); err != nil {
					t.Fatal(err)
				}
				if got := seg6RouteEncap(t, "7_3_0/ws/"+tc.captured+"/netlink_route_dump.pcap"); !reflect.DeepEqual(got, encap) {
					t.Fatalf("the capture's encap is %x", got)
				}
			}
			got, err := ParseIPEncap(encap)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err %v, expected %v", err, tc.wantErr)
			}
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got %+v (opts %+v), expected %+v (opts %+v)", got, got.Opts, tc.expected, tc.expected.Opts)
			}
		})
	}
}

// TestParseIPEncapCopies checks the decode doesn't alias the payload.
func TestParseIPEncapCopies(t *testing.T) {
	raw, err := hex.DecodeString(ipEncapCaptured["lwtroute4/ec-006"])
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseIPEncap(raw)
	if err != nil {
		t.Fatal(err)
	}
	clear(raw)
	tests := []struct {
		description string
		got         []byte
		expected    []byte
	}{
		{description: "positive: dst survives the payload's reuse", got: got.Dst, expected: []byte{203, 0, 113, 5}},
		{description: "positive: src survives", got: got.Src, expected: []byte{0, 0, 0, 0}},
		{description: "positive: geneve data survives", got: got.Opts.Geneve[0].Data, expected: []byte{0, 0x11, 0x22, 0x33}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if !reflect.DeepEqual(tc.got, tc.expected) {
				t.Errorf("got %x, expected %x", tc.got, tc.expected)
			}
		})
	}
}

func FuzzParseIPEncap(f *testing.F) {
	for _, s := range ipEncapCaptured {
		b, err := hex.DecodeString(s)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(b)
	}
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, val []byte) {
		a, err := ParseIPEncap(val)
		if err != nil {
			return
		}
		b, err := ParseIPEncap(val)
		if err != nil || !reflect.DeepEqual(a, b) {
			t.Fatalf("two decodes differ: %+v / %+v (%v)", a, b, err)
		}
	})
}

// go test ./pkg/xtcpnl/ -run '^$' -bench BenchmarkParseIPEncap
func BenchmarkParseIPEncap(b *testing.B) {
	raw, err := hex.DecodeString(ipEncapCaptured["lwtroute4/ec-008"])
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseIPEncap(raw); err != nil {
			b.Fatal(err)
		}
	}
}
