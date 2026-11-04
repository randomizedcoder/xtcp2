package xtcpnl

import (
	"encoding/hex"
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

// The goldens are hand-built from include/uapi/linux/lwtunnel.h: the encap
// nest is NHA_ENCAP 8|NLA_F_NESTED ("0880"), its type NHA_ENCAP_TYPE 7.
func TestPutIPEncap(t *testing.T) {
	dst4 := IPEncapItem{Kind: LwtunnelIPDst, Addr: []byte{203, 0, 113, 5}}
	id1 := IPEncapItem{Kind: LwtunnelIPID, U64: 1}
	geneve := func(g ...GeneveOpt) IPEncapItem {
		return IPEncapItem{Kind: LwtunnelIPOpts, Opts: IPEncapOpts{Kind: LwtunnelIPOptsGeneve, Geneve: g}}
	}
	g1 := GeneveOpt{Class: 0x0102, Type: 0x80, Data: []byte{0, 0x11, 0x22, 0x33}, HasClass: true, HasType: true, HasData: true}
	g2 := GeneveOpt{Class: 0x0102, Type: 0x81, Data: []byte{0x44, 0x55, 0x66, 0x77}, HasClass: true, HasType: true, HasData: true}
	const ipType, ip6Type = "0600070002000000", "0600070004000000"
	tests := []struct {
		description string
		e           IPEncap
		expected    string
		err         error
	}{
		{
			description: "positive: id and dst",
			e:           IPEncap{Family: unix.AF_INET, Items: []IPEncapItem{id1, dst4}},
			expected:    "18000880" + "0c0001000000000000000001" + "08000200cb007105" + ipType,
		},
		{
			description: "positive: every scalar in argv order, then the flags big-endian",
			e: IPEncap{Family: unix.AF_INET, Items: []IPEncapItem{
				id1, dst4, {Kind: LwtunnelIPSrc, Addr: []byte{192, 0, 2, 10}},
				{Kind: LwtunnelIPTos, U8: 0x10}, {Kind: LwtunnelIPTTL, U8: 64},
			}, Flags: LwtTunnelKey | LwtTunnelCsum | LwtTunnelSeq},
			expected: "38000880" + "0c0001000000000000000001" + "08000200cb007105" + "08000300c000020a" +
				"0500050010000000" + "0500040040000000" + "06000600000d0000" + ipType,
		},
		{
			description: "positive: IPv6's tc and hoplimit, and its type",
			e: IPEncap{Family: unix.AF_INET6, Items: []IPEncapItem{
				{Kind: LwtunnelIPDst, Addr: []byte{0x20, 0x01, 0x0d, 0xb8, 0, 9, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}},
				{Kind: LwtunnelIPTos, U8: 0x10}, {Kind: LwtunnelIPTTL, U8: 64},
			}},
			expected: "28000880" + "1400020020010db8000900000000000000000001" + "0500050010000000" + "0500040040000000" + ip6Type,
		},
		{
			description: "positive: two geneve options, a nest each",
			e:           IPEncap{Family: unix.AF_INET, Items: []IPEncapItem{geneve(g1, g2)}},
			expected: "40000880" + "3c000880" +
				"1c000180" + "0600010001020000" + "0500020080000000" + "0800030000112233" +
				"1c000180" + "0600010001020000" + "0500020081000000" + "0800030044556677" + ipType,
		},
		{
			description: "positive: vxlan's gbp",
			e: IPEncap{Family: unix.AF_INET, Items: []IPEncapItem{
				{Kind: LwtunnelIPOpts, Opts: IPEncapOpts{Kind: LwtunnelIPOptsVxlan, VxlanGBP: 0x800}},
			}},
			expected: "14000880" + "10000880" + "0c000280" + "0800010000080000" + ipType,
		},
		{
			description: "positive: erspan's version and index, the index big-endian",
			e: IPEncap{Family: unix.AF_INET, Items: []IPEncapItem{
				{Kind: LwtunnelIPOpts, Opts: IPEncapOpts{Kind: LwtunnelIPOptsErspan,
					Erspan: ErspanOpt{Ver: 1, Index: 0x1234, HasVer: true, HasIndex: true}}},
			}},
			expected: "1c000880" + "18000880" + "14000380" + "0500010001000000" + "0800020000001234" + ipType,
		},
		{
			description: "positive: erspan's every field",
			e: IPEncap{Family: unix.AF_INET, Items: []IPEncapItem{
				{Kind: LwtunnelIPOpts, Opts: IPEncapOpts{Kind: LwtunnelIPOptsErspan,
					Erspan: ErspanOpt{Ver: 2, Index: 1, Dir: 1, Hwid: 7, HasVer: true, HasIndex: true, HasDir: true, HasHwid: true}}},
			}},
			expected: "2c000880" + "28000880" + "24000380" + "0500010002000000" + "0800020000000001" +
				"0500030001000000" + "0500040007000000" + ipType,
		},
		{
			description: "negative: no flags, no LWTUNNEL_IP_FLAGS",
			e:           IPEncap{Family: unix.AF_INET, Items: []IPEncapItem{{Kind: LwtunnelIPTTL, U8: 64}}},
			expected:    "0c000880" + "0500040040000000" + ipType,
		},
		{description: "negative: an unspecified family", e: IPEncap{Items: []IPEncapItem{id1}}, err: ErrIPEncap},
		{
			description: "negative: an IPv4 address in an ip6 encap",
			e:           IPEncap{Family: unix.AF_INET6, Items: []IPEncapItem{dst4}},
			err:         ErrIPEncap,
		},
		{
			description: "negative: an item of no known kind",
			e:           IPEncap{Family: unix.AF_INET, Items: []IPEncapItem{{Kind: 7}}},
			err:         ErrIPEncap,
		},
		{
			description: "negative: options of no known kind",
			e:           IPEncap{Family: unix.AF_INET, Items: []IPEncapItem{{Kind: LwtunnelIPOpts, Opts: IPEncapOpts{Kind: 4}}}},
			err:         ErrIPEncap,
		},
		{
			description: "boundary: geneve data of no bytes is not sent",
			e:           IPEncap{Family: unix.AF_INET, Items: []IPEncapItem{geneve(GeneveOpt{Class: 0x0102, Type: 0x80, HasClass: true, HasType: true})}},
			expected:    "1c000880" + "18000880" + "14000180" + "0600010001020000" + "0500020080000000" + ipType,
		},
		{
			description: "corner: an empty geneve class field",
			e:           IPEncap{Family: unix.AF_INET, Items: []IPEncapItem{geneve(GeneveOpt{Type: 0x80, Data: g1.Data, HasType: true, HasData: true})}},
			expected:    "1c000880" + "18000880" + "14000180" + "0500020080000000" + "0800030000112233" + ipType,
		},
		{
			description: "corner: dst any is an empty attribute",
			e:           IPEncap{Family: unix.AF_INET6, Items: []IPEncapItem{{Kind: LwtunnelIPDst}}},
			expected:    "08000880" + "04000200" + ip6Type,
		},
		{
			description: "corner: one hex digit of data is an empty DATA",
			e:           IPEncap{Family: unix.AF_INET, Items: []IPEncapItem{geneve(GeneveOpt{HasData: true})}},
			expected:    "10000880" + "0c000880" + "08000180" + "04000300" + ipType,
		},
	}
	for i := range tests {
		tt := &tests[i]
		t.Run(tt.description, func(t *testing.T) {
			ab := NewAttrBuilder(make([]byte, 512))
			err := PutIPEncap(&ab, NhaEncap, NhaEncapType, tt.e)
			if tt.err != nil {
				if !errors.Is(err, tt.err) || ab.Len() != 0 {
					t.Fatalf("err = %v with %d bytes written, want %v and none", err, ab.Len(), tt.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := hex.EncodeToString(ab.Bytes()); got != tt.expected {
				t.Errorf("got  %s\nwant %s", got, tt.expected)
			}
		})
	}
}

// TestPutLwtEncap holds PutLwtEncap's choice and its bound.
func TestPutLwtEncap(t *testing.T) {
	ip := &IPEncap{Family: unix.AF_INET, Items: []IPEncapItem{{Kind: LwtunnelIPID, U64: 1}}}
	mpls := &MplsEncap{Dst: []byte{0, 6, 0x51, 0}}
	const ipLen = 24 // "10000880" "0c0001000000000000000001" "0600070002000000"
	tests := []struct {
		description string
		e           LwtEncap
		buf         int
		expected    int // bytes written
		err         error
	}{
		{description: "positive: ip", e: LwtEncap{IP: ip}, buf: 512, expected: ipLen},
		{description: "positive: mpls", e: LwtEncap{MPLS: mpls}, buf: 512, expected: 20},
		{description: "negative: neither", e: LwtEncap{}, buf: 512, err: ErrLwtEncap},
		{description: "negative: both", e: LwtEncap{IP: ip, MPLS: mpls}, buf: 512, err: ErrLwtEncap},
		{description: "boundary: the buffer exactly the encoding", e: LwtEncap{IP: ip}, buf: ipLen, expected: ipLen},
		{description: "boundary: one byte short, nothing written", e: LwtEncap{IP: ip}, buf: ipLen - 1, err: ErrAttrNoSpace},
		{description: "corner: room for the nest but not the type", e: LwtEncap{IP: ip}, buf: ipLen - 8, err: ErrAttrNoSpace},
	}
	for i := range tests {
		tt := &tests[i]
		t.Run(tt.description, func(t *testing.T) {
			ab := NewAttrBuilder(make([]byte, tt.buf))
			err := PutLwtEncap(&ab, NhaEncap, NhaEncapType, tt.e)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if tt.err != nil {
				if ab.Len() != 0 {
					t.Errorf("%d bytes written on an error", ab.Len())
				}
				return
			}
			if ab.Len() != tt.expected {
				t.Errorf("%d bytes, want %d", ab.Len(), tt.expected)
			}
		})
	}
}

func TestEncodeLwtEncap(t *testing.T) {
	encap, encapType := uint16(unix.RTA_ENCAP)|unix.NLA_F_NESTED, uint16(unix.RTA_ENCAP_TYPE)
	tests := []struct {
		description string
		e           LwtEncap
		bound       int
		expected    []RouteAttr
		err         error
	}{
		{
			description: "positive: mpls as PutMplsEncap writes it, split in two",
			e:           LwtEncap{MPLS: &MplsEncap{Dst: []byte{0, 6, 0x51, 0}}},
			bound:       1020,
			expected:    []RouteAttr{{encap, []byte{8, 0, 1, 0, 0, 6, 0x51, 0}}, {encapType, []byte{1, 0}}},
		},
		{
			description: "positive: ip, the type unpadded",
			e:           LwtEncap{IP: &IPEncap{Family: unix.AF_INET, Items: []IPEncapItem{{Kind: LwtunnelIPTTL, U8: 1}}}},
			bound:       1020,
			expected:    []RouteAttr{{encap, []byte{5, 0, 4, 0, 1, 0, 0, 0}}, {encapType, []byte{2, 0}}},
		},
		{
			description: "negative: no encap",
			e:           LwtEncap{},
			bound:       1020,
			err:         ErrLwtEncap,
		},
		{
			description: "boundary: a bound one byte short",
			e:           LwtEncap{MPLS: &MplsEncap{Dst: []byte{0, 6, 0x51, 0}}},
			bound:       19,
			err:         ErrAttrNoSpace,
		},
		{
			description: "corner: a negative bound",
			e:           LwtEncap{MPLS: &MplsEncap{Dst: []byte{0, 6, 0x51, 0}}},
			bound:       -1,
			err:         ErrAttrNoSpace,
		},
	}
	for i := range tests {
		tt := &tests[i]
		t.Run(tt.description, func(t *testing.T) {
			got, err := EncodeLwtEncap(tt.e, tt.bound)
			if !errors.Is(err, tt.err) {
				t.Fatalf("err = %v, want %v", err, tt.err)
			}
			if len(got) != len(tt.expected) {
				t.Fatalf("%d attributes, want %d", len(got), len(tt.expected))
			}
			for j := range got {
				if got[j].Type != tt.expected[j].Type || hex.EncodeToString(got[j].Payload) != hex.EncodeToString(tt.expected[j].Payload) {
					t.Errorf("attr %d: %d %x, want %d %x", j, got[j].Type, got[j].Payload, tt.expected[j].Type, tt.expected[j].Payload)
				}
			}
		})
	}
}
