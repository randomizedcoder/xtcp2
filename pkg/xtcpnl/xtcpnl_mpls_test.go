package xtcpnl

import (
	"encoding/binary"
	"errors"
	"math"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

// mplsEnt is one label stack entry, network order: label, TC, BoS and TTL.
func mplsEnt(label uint32, tc uint8, bos bool, ttl uint8) []byte {
	e := label<<12 | uint32(tc&0x7)<<9 | uint32(ttl)
	if bos {
		e |= 1 << 8
	}
	return binary.BigEndian.AppendUint32(nil, e)
}

// mplsStack is the labels as the kernel sends them: TC and TTL 0, BoS on
// the last.
func mplsStack(labels ...uint32) []byte {
	out := make([]byte, 0, MPLSLabelEntrySizeCst*len(labels))
	for i, l := range labels {
		out = append(out, mplsEnt(l, 0, i == len(labels)-1, 0)...)
	}
	return out
}

// rtnh is one struct rtnexthop entry with its nested attributes. rtnh_len is
// a u16, so an entry that can't be encoded fails the test rather than wrap.
func rtnh(t *testing.T, ifindex uint32, attrs ...[]byte) []byte {
	t.Helper()
	body := concat(attrs...)
	n := RtNextHopSizeCst + len(body)
	if n > math.MaxUint16 {
		t.Fatalf("rtnh: entry of %d bytes overflows rtnh_len", n)
	}
	h := make([]byte, RtNextHopSizeCst, n)
	binary.LittleEndian.PutUint16(h[0:2], uint16(n))
	binary.LittleEndian.PutUint32(h[4:8], ifindex)
	return append(h, body...)
}

// mplsEncapAttrs is RTA_ENCAP holding <inner> followed by RTA_ENCAP_TYPE
// MPLS, the kernel's order.
func mplsEncapAttrs(inner ...[]byte) []byte {
	return concat(
		rtattr(unix.RTA_ENCAP|unix.NLA_F_NESTED, concat(inner...)),
		rtattr(unix.RTA_ENCAP_TYPE, le16(unix.LWTUNNEL_ENCAP_MPLS)),
	)
}

func TestParseMPLSLabels(t *testing.T) {
	tests := []struct {
		description string
		val         []byte
		expected    []MPLSLabel
		wantErr     error
	}{
		{
			description: "positive: one label, bottom of stack",
			val:         mplsStack(100),
			expected:    []MPLSLabel{{Label: 100, BoS: true}},
		},
		{
			description: "positive: a two-label stack, BoS on the second only",
			val:         mplsStack(200, 300),
			expected:    []MPLSLabel{{Label: 200}, {Label: 300, BoS: true}},
		},
		{
			description: "positive: TC and TTL are decoded",
			val:         mplsEnt(16, 5, true, 64),
			expected:    []MPLSLabel{{Label: 16, TC: 5, BoS: true, TTL: 64}},
		},
		{
			description: "boundary: the largest label, 2^20-1",
			val:         mplsStack(1<<20 - 1),
			expected:    []MPLSLabel{{Label: 1<<20 - 1, BoS: true}},
		},
		{
			description: "boundary: an empty stack is no labels",
			val:         nil,
			expected:    nil,
		},
		{
			description: "negative: 3 bytes is not a whole entry",
			val:         []byte{0, 0x64, 0x01},
			wantErr:     ErrMPLSLabelsLen,
		},
		{
			description: "boundary: 5 bytes is one entry and a stray byte",
			val:         append(mplsStack(100), 0),
			wantErr:     ErrMPLSLabelsLen,
		},
		{
			description: "corner: a last entry without BoS",
			val:         mplsEnt(100, 0, false, 0),
			wantErr:     ErrMPLSLabelsBoS,
		},
		{
			description: "corner: BoS before the last entry",
			val:         concat(mplsEnt(100, 0, true, 0), mplsEnt(200, 0, true, 0)),
			wantErr:     ErrMPLSLabelsBoS,
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := ParseMPLSLabels(tt.val)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err %v, expected %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got %+v, expected %+v", got, tt.expected)
			}
		})
	}
}

func TestParseMPLSEncap(t *testing.T) {
	tests := []struct {
		description string
		val         []byte
		expected    MPLSEncap
		wantErr     error
	}{
		{
			description: "positive: a two-label push",
			val:         rtattr(MPLSIPTunnelDst, mplsStack(100, 200)),
			expected:    MPLSEncap{Labels: []MPLSLabel{{Label: 100}, {Label: 200, BoS: true}}},
		},
		{
			description: "positive: a push with a TTL",
			val:         concat(rtattr(MPLSIPTunnelDst, mplsStack(100)), rtattr(MPLSIPTunnelTTL, []byte{64})),
			expected:    MPLSEncap{Labels: []MPLSLabel{{Label: 100, BoS: true}}, TTL: 64, HasTTL: true},
		},
		{
			description: "boundary: an empty payload",
			val:         nil,
			expected:    MPLSEncap{},
		},
		{
			description: "boundary: an empty MPLS_IPTUNNEL_TTL leaves HasTTL false",
			val:         concat(rtattr(MPLSIPTunnelDst, mplsStack(100)), rtattr(MPLSIPTunnelTTL, nil)),
			expected:    MPLSEncap{Labels: []MPLSLabel{{Label: 100, BoS: true}}},
		},
		{
			description: "negative: a malformed label stack is an error",
			val:         rtattr(MPLSIPTunnelDst, []byte{1, 2, 3}),
			wantErr:     ErrMPLSLabelsLen,
		},
		{
			description: "corner: a repeated MPLS_IPTUNNEL_DST, the first wins",
			val:         concat(rtattr(MPLSIPTunnelDst, mplsStack(100)), rtattr(MPLSIPTunnelDst, mplsStack(999))),
			expected:    MPLSEncap{Labels: []MPLSLabel{{Label: 100, BoS: true}}},
		},
		{
			description: "corner: an unknown attribute is skipped",
			val:         concat(rtattr(9, []byte{1, 2, 3, 4}), rtattr(MPLSIPTunnelDst, mplsStack(100))),
			expected:    MPLSEncap{Labels: []MPLSLabel{{Label: 100, BoS: true}}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			got, err := ParseMPLSEncap(tt.val)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err %v, expected %v", err, tt.wantErr)
			}
			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got %+v, expected %+v", got, tt.expected)
			}
		})
	}
}

// TestParseNewRouteMPLS covers RTA_NEWDST, RTA_ENCAP and RTA_ENCAP_TYPE on
// a route and in its next hops, through ParseNewRoute.
func TestParseNewRouteMPLS(t *testing.T) {
	v4Hdr := rtmsgHdr(unix.AF_INET, 24, 0, unix.RT_TABLE_MAIN, unix.RTPROT_BOOT, unix.RT_SCOPE_UNIVERSE, unix.RTN_UNICAST, 0)
	mplsHdr := rtmsgHdr(unix.AF_MPLS, 20, 0, unix.RT_TABLE_MAIN, unix.RTPROT_BOOT, unix.RT_SCOPE_UNIVERSE, unix.RTN_UNICAST, 0)
	push := func(labels ...uint32) *RouteEncap {
		e := &RouteEncap{Type: unix.LWTUNNEL_ENCAP_MPLS, HasType: true, Raw: rtattr(MPLSIPTunnelDst, mplsStack(labels...))}
		m, _ := ParseMPLSEncap(e.Raw)
		e.MPLS = &m
		return e
	}
	tests := []struct {
		description string
		body        []byte
		check       func(ri RouteInfo) any
		expected    any
		wantErr     error
	}{
		{
			description: "positive: an AF_MPLS swap's RTA_NEWDST",
			body:        concat(mplsHdr, rtattr(unix.RTA_DST, mplsStack(100)), rtattr(unix.RTA_NEWDST, mplsStack(200, 300))),
			check:       func(ri RouteInfo) any { return ri.NewDst },
			expected:    []MPLSLabel{{Label: 200}, {Label: 300, BoS: true}},
		},
		{
			description: "positive: an AF_MPLS route's RTA_DST is one label",
			body:        concat(mplsHdr, rtattr(unix.RTA_DST, mplsStack(100))),
			check: func(ri RouteInfo) any {
				l, err := ParseMPLSLabels(ri.Dst)
				if err != nil {
					return err
				}
				return l
			},
			expected: []MPLSLabel{{Label: 100, BoS: true}},
		},
		{
			description: "boundary: a pop has no RTA_NEWDST",
			body:        concat(mplsHdr, rtattr(unix.RTA_DST, mplsStack(101))),
			check:       func(ri RouteInfo) any { return ri.NewDst == nil && ri.Encap == nil },
			expected:    true,
		},
		{
			description: "positive: an IPv4 route's encap mpls 100/200, RTA_ENCAP before its type",
			body:        concat(v4Hdr, mplsEncapAttrs(rtattr(MPLSIPTunnelDst, mplsStack(100, 200)))),
			check:       func(ri RouteInfo) any { return ri.Encap },
			expected:    push(100, 200),
		},
		{
			description: "corner: the type before RTA_ENCAP decodes the same",
			body: concat(v4Hdr,
				rtattr(unix.RTA_ENCAP_TYPE, le16(unix.LWTUNNEL_ENCAP_MPLS)),
				rtattr(unix.RTA_ENCAP, rtattr(MPLSIPTunnelDst, mplsStack(100, 200)))),
			check:    func(ri RouteInfo) any { return ri.Encap },
			expected: push(100, 200),
		},
		{
			description: "corner: another encap type is kept raw, not an error",
			body: concat(v4Hdr,
				rtattr(unix.RTA_ENCAP, []byte{4, 0, 1, 0}),
				rtattr(unix.RTA_ENCAP_TYPE, le16(unix.LWTUNNEL_ENCAP_ILA))),
			check:    func(ri RouteInfo) any { return ri.Encap },
			expected: &RouteEncap{Type: unix.LWTUNNEL_ENCAP_ILA, HasType: true, Raw: []byte{4, 0, 1, 0}},
		},
		{
			description: "corner: RTA_ENCAP with no type is kept raw",
			body:        concat(v4Hdr, rtattr(unix.RTA_ENCAP, rtattr(MPLSIPTunnelDst, mplsStack(100)))),
			check:       func(ri RouteInfo) any { return ri.Encap.HasType || ri.Encap.MPLS != nil },
			expected:    false,
		},
		{
			description: "negative: a malformed RTA_NEWDST fails the route",
			body:        concat(mplsHdr, rtattr(unix.RTA_NEWDST, mplsEnt(200, 0, false, 0))),
			wantErr:     ErrMPLSLabelsBoS,
		},
		{
			description: "negative: a malformed MPLS encap fails the route",
			body:        concat(v4Hdr, mplsEncapAttrs(rtattr(MPLSIPTunnelDst, []byte{1, 2, 3}))),
			wantErr:     ErrMPLSLabelsLen,
		},
		{
			description: "positive: a per-nexthop encap inside RTA_MULTIPATH",
			body: concat(v4Hdr, rtattr(unix.RTA_MULTIPATH, concat(
				rtnh(t, 2, mplsEncapAttrs(rtattr(MPLSIPTunnelDst, mplsStack(101))), rtattr(unix.RTA_GATEWAY, v4b(192, 0, 2, 10))),
				rtnh(t, 2, mplsEncapAttrs(rtattr(MPLSIPTunnelDst, mplsStack(102))), rtattr(unix.RTA_GATEWAY, v4b(192, 0, 2, 11))),
			))),
			check: func(ri RouteInfo) any {
				got := make([]uint32, 0, len(ri.Multipath))
				for _, nh := range ri.Multipath {
					got = append(got, nh.Encap.MPLS.Labels[0].Label)
				}
				return got
			},
			expected: []uint32{101, 102},
		},
		{
			description: "positive: a per-nexthop RTA_NEWDST of an AF_MPLS multipath route",
			body: concat(mplsHdr, rtattr(unix.RTA_MULTIPATH, concat(
				rtnh(t, 2, rtattr(unix.RTA_NEWDST, mplsStack(201))),
				rtnh(t, 2, rtattr(unix.RTA_NEWDST, mplsStack(202))),
			))),
			check: func(ri RouteInfo) any {
				return [][]MPLSLabel{ri.Multipath[0].NewDst, ri.Multipath[1].NewDst}
			},
			expected: [][]MPLSLabel{{{Label: 201, BoS: true}}, {{Label: 202, BoS: true}}},
		},
		{
			description: "negative: a malformed per-nexthop encap fails the route",
			body: concat(v4Hdr, rtattr(unix.RTA_MULTIPATH,
				rtnh(t, 2, mplsEncapAttrs(rtattr(MPLSIPTunnelDst, mplsEnt(101, 0, false, 0)))))),
			wantErr: ErrMPLSLabelsBoS,
		},
		{
			description: "negative: a malformed per-nexthop RTA_NEWDST fails the route",
			body:        concat(mplsHdr, rtattr(unix.RTA_MULTIPATH, rtnh(t, 2, rtattr(unix.RTA_NEWDST, []byte{1})))),
			wantErr:     ErrMPLSLabelsLen,
		},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			ri, err := ParseNewRoute(tt.body)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err %v, expected %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				return
			}
			if got := tt.check(ri); !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("got %+v, expected %+v", got, tt.expected)
			}
		})
	}
}
