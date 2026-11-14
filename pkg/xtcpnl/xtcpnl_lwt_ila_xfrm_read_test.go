package xtcpnl

import (
	"encoding/hex"
	"errors"
	"testing"
)

// NX6d: print_encap_ila and print_encap_xfrm's read of an RTA_ENCAP
// payload, for route and nexthop show. The captured rows are the ila and
// xfrm spaces' dumps; the rest are hand-built.

// ilaXfrmEncapCaptured are the ila and xfrm spaces' encaps as the kernel
// dumped them (7_3_0/ws/<row>/netlink_route_dump.pcap).
var ilaXfrmEncapCaptured = map[string]string{
	// encap ila 2001:0:0:2 ident-type luid; the kernel adds no-action and output
	"ila/ec-001": "0c0001002001000000000002050007000200000005000800010000000500090000000000",
	// … csum-mode adj-transport ident-type luid
	"ila/ec-003": "0c0001002001000000000002050007000000000005000800010000000500090000000000",
	// encap xfrm if_id 1
	"xfrm/ec-001": "0800010001000000",
	// encap xfrm if_id 1 link_dev d0 (ifindex 3)
	"xfrm/ec-002": "08000100010000000800020003000000",
	// encap xfrm if_id 7
	"xfrm/ec-003": "0800010007000000",
}

func capturedEncap(t testing.TB, row string) []byte {
	t.Helper()
	b, err := hex.DecodeString(ilaXfrmEncapCaptured[row])
	if err != nil || len(b) == 0 {
		t.Fatalf("%s: %v", row, err)
	}
	return b
}

func TestParseIlaEncap(t *testing.T) {
	loc := []byte{0x20, 0x01, 0, 0, 0, 0, 0, 2}
	tests := []struct {
		description string
		val         []byte
		expected    IlaEncapInfo
		err         error
	}{
		{description: "positive: captured, the kernel's defaults filled in", val: capturedEncap(t, "ila/ec-001"),
			expected: IlaEncapInfo{Locator: 0x2001_0000_0000_0002, CsumMode: ILACsumNoAction, IdentType: ILAAtypeLUID,
				HookType: ILAHookRouteOutput, HasLocator: true, HasCsumMode: true, HasIdentType: true, HasHookType: true}},
		{description: "positive: captured, csum-mode adj-transport is 0 and still present", val: capturedEncap(t, "ila/ec-003"),
			expected: IlaEncapInfo{Locator: 0x2001_0000_0000_0002, IdentType: ILAAtypeLUID,
				HasLocator: true, HasCsumMode: true, HasIdentType: true, HasHookType: true}},
		{description: "positive: the input hook", val: rtattr(ILAAttrHookType, []byte{ILAHookRouteInput}),
			expected: IlaEncapInfo{HookType: ILAHookRouteInput, HasHookType: true}},
		{description: "boundary: an empty payload has nothing", val: nil, expected: IlaEncapInfo{}},
		{description: "boundary: a 7-byte locator is dropped", val: rtattr(ILAAttrLocator, loc[:7]), expected: IlaEncapInfo{}},
		{description: "boundary: an empty csum-mode is dropped", val: rtattr(ILAAttrCsumMode, nil), expected: IlaEncapInfo{}},
		{description: "corner: the first of a repeated locator wins",
			val:      concat(rtattr(ILAAttrLocator, loc), rtattr(ILAAttrLocator, []byte{0xff, 0xff, 0, 0, 0, 0, 0, 0})),
			expected: IlaEncapInfo{Locator: 0x2001_0000_0000_0002, HasLocator: true}},
		{description: "corner: an unknown attribute is skipped", val: concat(rtattr(99, []byte{1}), rtattr(ILAAttrIdentType, []byte{32})),
			expected: IlaEncapInfo{IdentType: ILAAtypeUseFormat, HasIdentType: true}},
		{description: "negative: an attribute longer than the payload", val: []byte{12, 0, 1, 0, 0x20, 1}, err: ErrRTAttrSmall},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseIlaEncap(tc.val)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("err %v, expected %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.expected {
				t.Errorf("got %+v, expected %+v", got, tc.expected)
			}
		})
	}
}

func TestParseXfrmEncap(t *testing.T) {
	tests := []struct {
		description string
		val         []byte
		expected    XfrmEncapInfo
		err         error
	}{
		{description: "positive: captured, if_id alone", val: capturedEncap(t, "xfrm/ec-001"),
			expected: XfrmEncapInfo{IfID: 1, HasIfID: true}},
		{description: "positive: captured, if_id and link_dev", val: capturedEncap(t, "xfrm/ec-002"),
			expected: XfrmEncapInfo{IfID: 1, Link: 3, HasIfID: true, HasLink: true}},
		{description: "positive: captured, if_id 7", val: capturedEncap(t, "xfrm/ec-003"),
			expected: XfrmEncapInfo{IfID: 7, HasIfID: true}},
		{description: "boundary: if_id 0 is present", val: rtattr(LwtXfrmIfID, le32b(0)), expected: XfrmEncapInfo{HasIfID: true}},
		{description: "boundary: if_id 4294967295", val: rtattr(LwtXfrmIfID, le32b(4294967295)),
			expected: XfrmEncapInfo{IfID: 4294967295, HasIfID: true}},
		{description: "boundary: a 3-byte link is dropped", val: rtattr(LwtXfrmLink, []byte{3, 0, 0}), expected: XfrmEncapInfo{}},
		{description: "corner: the first of a repeated if_id wins, even when the first is short",
			val:      concat(rtattr(LwtXfrmIfID, []byte{1}), rtattr(LwtXfrmIfID, le32b(2))),
			expected: XfrmEncapInfo{}},
		{description: "corner: an unknown attribute is skipped", val: concat(rtattr(9, le32b(5)), rtattr(LwtXfrmLink, le32b(4))),
			expected: XfrmEncapInfo{Link: 4, HasLink: true}},
		{description: "negative: an attribute longer than the payload", val: []byte{8, 0, 1, 0, 1}, err: ErrRTAttrSmall},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseXfrmEncap(tc.val)
			if tc.err != nil {
				if !errors.Is(err, tc.err) {
					t.Fatalf("err %v, expected %v", err, tc.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.expected {
				t.Errorf("got %+v, expected %+v", got, tc.expected)
			}
		})
	}
}

func FuzzParseIlaXfrmEncap(f *testing.F) {
	for _, h := range ilaXfrmEncapCaptured {
		b, _ := hex.DecodeString(h)
		f.Add(b)
	}
	f.Fuzz(func(t *testing.T, b []byte) {
		ila, ierr := ParseIlaEncap(b)
		if ierr != nil && ila != (IlaEncapInfo{}) {
			t.Fatalf("ila error %v with a value %+v", ierr, ila)
		}
		x, xerr := ParseXfrmEncap(b)
		if xerr != nil && x != (XfrmEncapInfo{}) {
			t.Fatalf("xfrm error %v with a value %+v", xerr, x)
		}
		if (ierr == nil) != (xerr == nil) {
			t.Fatalf("the same walk disagrees: ila %v, xfrm %v", ierr, xerr)
		}
	})
}

func BenchmarkParseIlaEncap(b *testing.B) {
	val := capturedEncap(b, "ila/ec-001")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseIlaEncap(val); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkParseXfrmEncap(b *testing.B) {
	val := capturedEncap(b, "xfrm/ec-002")
	b.ReportAllocs()
	for b.Loop() {
		if _, err := ParseXfrmEncap(val); err != nil {
			b.Fatal(err)
		}
	}
}
