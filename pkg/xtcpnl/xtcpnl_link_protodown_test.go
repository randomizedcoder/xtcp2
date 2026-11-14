package xtcpnl

import (
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

// TestParseNewLinkProtoDown checks IFLA_PROTO_DOWN and the
// IFLA_PROTO_DOWN_REASON nest, which print_proto_down reads.
func TestParseNewLinkProtoDown(t *testing.T) {
	hdr := ifinfomsgHdr(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 4, 0)
	reason := func(attrs ...[]byte) []byte {
		return rtattr(unix.IFLA_PROTO_DOWN_REASON|unix.NLA_F_NESTED, concat(attrs...))
	}
	tests := []struct {
		description string
		body        []byte
		expected    LinkInfo
	}{
		{description: "positive: protodown on",
			body:     concat(hdr, rtattr(unix.IFLA_PROTO_DOWN, []byte{1})),
			expected: LinkInfo{Index: 4, Type: 1, ProtoDown: 1, HasProtoDown: true}},
		{description: "positive: a reason value, as the kernel sends it",
			body: concat(hdr, rtattr(unix.IFLA_PROTO_DOWN, []byte{1}),
				reason(rtattr(unix.IFLA_PROTO_DOWN_REASON_VALUE, le32(0x80000009)))),
			expected: LinkInfo{Index: 4, Type: 1, ProtoDown: 1, HasProtoDown: true,
				ProtoDownReason: 0x80000009, HasProtoDownReason: true, HasProtoDownReasonValue: true}},
		{description: "negative: neither attribute",
			body: hdr, expected: LinkInfo{Index: 4, Type: 1}},
		{description: "boundary: protodown off is present",
			body:     concat(hdr, rtattr(unix.IFLA_PROTO_DOWN, []byte{0})),
			expected: LinkInfo{Index: 4, Type: 1, HasProtoDown: true}},
		{description: "boundary: a nest with only a mask has no value",
			body:     concat(hdr, reason(rtattr(unix.IFLA_PROTO_DOWN_REASON_MASK, le32(1)))),
			expected: LinkInfo{Index: 4, Type: 1, HasProtoDownReason: true}},
		{description: "corner: the first of a repeated value wins",
			body: concat(hdr, reason(rtattr(unix.IFLA_PROTO_DOWN_REASON_VALUE, le32(1)),
				rtattr(unix.IFLA_PROTO_DOWN_REASON_VALUE, le32(2)))),
			expected: LinkInfo{Index: 4, Type: 1, ProtoDownReason: 1, HasProtoDownReason: true, HasProtoDownReasonValue: true}},
		{description: "corner: a value shorter than a u32 is dropped",
			body:     concat(hdr, reason(rtattr(unix.IFLA_PROTO_DOWN_REASON_VALUE, []byte{1, 0}))),
			expected: LinkInfo{Index: 4, Type: 1, HasProtoDownReason: true}},
		{description: "corner: an empty IFLA_PROTO_DOWN is dropped",
			body:     concat(hdr, rtattr(unix.IFLA_PROTO_DOWN, nil)),
			expected: LinkInfo{Index: 4, Type: 1}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := ParseNewLink(tc.body)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.expected) {
				t.Errorf("got %+v\nexpected %+v", got, tc.expected)
			}
		})
	}
}
