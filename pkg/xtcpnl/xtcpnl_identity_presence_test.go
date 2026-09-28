package xtcpnl

import (
	"bytes"
	"testing"

	"golang.org/x/sys/unix"
)

// This file guards two properties that are invisible in a dump-driven test,
// because a live kernel does not produce the inputs that distinguish them:
//
//   - a route's identity is (family, dst/dst_len, src/src_len, tos, table),
//     not just the destination, so a parser that drops src_len or tos collapses
//     distinct routes into one;
//   - an optional attribute carrying 0 is not the same fact as that attribute
//     being absent, so every such field needs a presence flag beside it.
//
// Both are asserted on constructed bytes on purpose. The corpus in
// xtcpnl_rtnetlink_realfixtures_test.go has no source-routed entry at all, and
// its link replies always carry IFLA_OPERSTATE / IFLA_CARRIER / IFLA_MTU /
// IFLA_LINKMODE (see that file's table header for why), so the absent half of
// each presence pair is unreachable from real captures.

// TestParseNewRouteSourceIdentity asserts that rtm_src_len, rtm_tos and RTA_SRC
// survive the parse, since a route keyed on the destination alone is a
// different route from the one the kernel described.
//
// go test ./pkg/xtcpnl/ -run TestParseNewRouteSourceIdentity
func TestParseNewRouteSourceIdentity(t *testing.T) {
	tests := []struct {
		description string
		body        []byte
		wantSrcLen  uint8
		wantTos     uint8
		wantSrc     []byte
		wantErr     bool
	}{
		{
			// The shape `ip route add 198.51.100.0/24 from 192.0.2.0/16 tos 7`
			// installs: a source-routed entry, which no dump in the corpus has.
			description: "positive: src_len, tos and RTA_SRC all survive the parse",
			body: concat(
				rtmsgHdrSrc(unix.AF_INET, 24, 16, 7, unix.RT_TABLE_MAIN, unix.RTPROT_STATIC, unix.RT_SCOPE_UNIVERSE, unix.RTN_UNICAST, 0),
				rtattr(uint16(unix.RTA_SRC), v4b(192, 0, 2, 0)),
			),
			wantSrcLen: 16,
			wantTos:    7,
			wantSrc:    v4b(192, 0, 2, 0),
		},
		{
			// The ordinary case, and the reason the positive row above is not
			// enough on its own: if the parser hardcoded src_len it would still
			// pass here. Every route in the committed dump looks like this.
			description: "negative: a route with no source selector reports src_len 0, tos 0 and a nil Src",
			body: concat(
				rtmsgHdr(unix.AF_INET, 24, 0, unix.RT_TABLE_MAIN, unix.RTPROT_KERNEL, unix.RT_SCOPE_LINK, unix.RTN_UNICAST, 0),
				rtattr(uint16(unix.RTA_DST), v4b(10, 0, 0, 0)),
			),
			wantSrcLen: 0,
			wantTos:    0,
			wantSrc:    nil,
		},
		{
			// src_len 32 on IPv4 is a host source, the widest an v4 selector
			// gets; tos 0xfc is the whole DSCP field with the two ECN bits
			// clear, the widest value `ip route ... tos` accepts.
			description: "boundary: the widest v4 src_len and tos values both round-trip",
			body: concat(
				rtmsgHdrSrc(unix.AF_INET, 32, 32, 0xfc, unix.RT_TABLE_MAIN, unix.RTPROT_STATIC, unix.RT_SCOPE_UNIVERSE, unix.RTN_UNICAST, 0),
				rtattr(uint16(unix.RTA_SRC), v4b(192, 0, 2, 1)),
			),
			wantSrcLen: 32,
			wantTos:    0xfc,
			wantSrc:    v4b(192, 0, 2, 1),
		},
		{
			// src_len says 16 but no RTA_SRC follows. The header and the
			// attributes disagree, and the parser must report both halves as
			// they arrived rather than inventing a Src to match the header or
			// zeroing src_len to match the attributes — either repair would
			// hide a malformed sender.
			description: "corner: src_len set with no RTA_SRC keeps both the length and the nil Src",
			body:        rtmsgHdrSrc(unix.AF_INET, 24, 16, 0, unix.RT_TABLE_MAIN, unix.RTPROT_STATIC, unix.RT_SCOPE_UNIVERSE, unix.RTN_UNICAST, 0),
			wantSrcLen:  16,
			wantTos:     0,
			wantSrc:     nil,
		},
		{
			description: "negative: a body shorter than the rtmsg header is an error, not a zero route",
			body:        make([]byte, RtMsgSizeCst-1),
			wantErr:     true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ri, err := ParseNewRoute(tc.body)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseNewRoute = %+v, want error", ri)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseNewRoute: %v", err)
			}
			if ri.SrcLen != tc.wantSrcLen {
				t.Errorf("SrcLen = %d, want %d", ri.SrcLen, tc.wantSrcLen)
			}
			if ri.Tos != tc.wantTos {
				t.Errorf("Tos = %d, want %d", ri.Tos, tc.wantTos)
			}
			if !bytes.Equal(ri.Src, tc.wantSrc) {
				t.Errorf("Src = %v, want %v", ri.Src, tc.wantSrc)
			}
		})
	}
}

// TestParseNewLinkPresenceOfZeroAttributes asserts that an IFLA_* attribute
// carrying 0 sets its presence flag, and that omitting the attribute leaves the
// flag clear. Without the flags the two decode identically, and the four
// attributes here all have 0 as a legitimate value.
//
// go test ./pkg/xtcpnl/ -run TestParseNewLinkPresenceOfZeroAttributes
func TestParseNewLinkPresenceOfZeroAttributes(t *testing.T) {
	const noSuchIFLA = 0x3f // below attrSeen's 64-bit ceiling, above every IFLA_* xtcpnl decodes

	tests := []struct {
		description  string
		body         []byte
		wantOperSt   bool
		wantCarrier  bool
		wantMTU      bool
		wantLinkMode bool
	}{
		{
			// All four present and all four zero: the row the flags exist for.
			// IF_OPER_UNKNOWN, carrier down, MTU 0 and mode DEFAULT are each a
			// real statement by the kernel, not a gap.
			description: "positive: all four zero-valued attributes set their presence flags",
			body: concat(
				ifinfomsgHdr(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 5, 0),
				rtattr(uint16(unix.IFLA_OPERSTATE), []byte{0}),
				rtattr(uint16(unix.IFLA_CARRIER), []byte{0}),
				rtattr(uint16(unix.IFLA_MTU), le32(0)),
				rtattr(uint16(unix.IFLA_LINKMODE), []byte{0}),
			),
			wantOperSt: true, wantCarrier: true, wantMTU: true, wantLinkMode: true,
		},
		{
			// The other half of the pair, and unreachable from the dump corpus:
			// rtnl_fill_ifinfo emits all four unconditionally, so only
			// constructed bytes can assert the absent case.
			description: "negative: a header with no attributes at all leaves every presence flag clear",
			body:        ifinfomsgHdr(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 5, 0),
		},
		{
			description: "boundary: exactly one of the four present sets only its own flag",
			body: concat(
				ifinfomsgHdr(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 5, 0),
				rtattr(uint16(unix.IFLA_CARRIER), []byte{0}),
			),
			wantCarrier: true,
		},
		{
			// An attribute this package does not decode must not disturb the
			// flags of the ones it does.
			description: "corner: an unknown attribute alongside IFLA_MTU sets only HasMTU",
			body: concat(
				ifinfomsgHdr(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 5, 0),
				rtattr(noSuchIFLA, []byte{1, 2, 3, 4}),
				rtattr(uint16(unix.IFLA_MTU), le32(0)),
			),
			wantMTU: true,
		},
		{
			// A truncated IFLA_MTU cannot yield a value, so it must not claim
			// presence either: HasMTU true with MTU 0 would assert the kernel
			// said "MTU 0", which it did not.
			description: "corner: a truncated IFLA_MTU claims no presence",
			body: concat(
				ifinfomsgHdr(unix.AF_UNSPEC, unix.ARPHRD_ETHER, 5, 0),
				rtattr(uint16(unix.IFLA_MTU), []byte{0, 0}),
			),
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			li, err := ParseNewLink(tc.body)
			if err != nil {
				t.Fatalf("ParseNewLink: %v", err)
			}
			if li.HasOperState != tc.wantOperSt {
				t.Errorf("HasOperState = %v, want %v", li.HasOperState, tc.wantOperSt)
			}
			if li.HasCarrier != tc.wantCarrier {
				t.Errorf("HasCarrier = %v, want %v", li.HasCarrier, tc.wantCarrier)
			}
			if li.HasMTU != tc.wantMTU {
				t.Errorf("HasMTU = %v, want %v", li.HasMTU, tc.wantMTU)
			}
			if li.HasLinkMode != tc.wantLinkMode {
				t.Errorf("HasLinkMode = %v, want %v", li.HasLinkMode, tc.wantLinkMode)
			}
		})
	}
}

// TestParseNewAddrPresenceOfZeroProto is the IFA_PROTO half of the same
// property. IFA_PROTO_UNSPEC is 0, so an address whose proto the kernel did not
// classify and an address on a kernel too old to report proto at all both give
// Proto 0 — and `ip` prints `proto unspec` for the first and nothing for the
// second.
//
// go test ./pkg/xtcpnl/ -run TestParseNewAddrPresenceOfZeroProto
func TestParseNewAddrPresenceOfZeroProto(t *testing.T) {
	tests := []struct {
		description string
		body        []byte
		wantProto   uint8
		wantHas     bool
	}{
		{
			description: "positive: IFA_PROTO carrying IFA_PROTO_UNSPEC (0) sets HasProto",
			body: concat(
				ifaddrmsgHdr(unix.AF_INET, 24, 0, unix.RT_SCOPE_UNIVERSE, 2),
				rtattr(IfaProto, []byte{0}),
			),
			wantProto: 0, wantHas: true,
		},
		{
			description: "negative: no IFA_PROTO leaves HasProto false",
			body:        ifaddrmsgHdr(unix.AF_INET, 24, 0, unix.RT_SCOPE_UNIVERSE, 2),
			wantProto:   0, wantHas: false,
		},
		{
			description: "boundary: IFA_PROTO 0xff, the widest 8-bit proto id, round-trips",
			body: concat(
				ifaddrmsgHdr(unix.AF_INET, 24, 0, unix.RT_SCOPE_UNIVERSE, 2),
				rtattr(IfaProto, []byte{0xff}),
			),
			wantProto: 0xff, wantHas: true,
		},
		{
			// parse_rtattr keeps the first occurrence, so a duplicate must not
			// overwrite the value `ip` would have rendered.
			description: "corner: a duplicate IFA_PROTO keeps the first value",
			body: concat(
				ifaddrmsgHdr(unix.AF_INET, 24, 0, unix.RT_SCOPE_UNIVERSE, 2),
				rtattr(IfaProto, []byte{IfaProtoKernelLL}),
				rtattr(IfaProto, []byte{0}),
			),
			wantProto: IfaProtoKernelLL, wantHas: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ai, err := ParseNewAddr(tc.body)
			if err != nil {
				t.Fatalf("ParseNewAddr: %v", err)
			}
			if ai.Proto != tc.wantProto {
				t.Errorf("Proto = %d, want %d", ai.Proto, tc.wantProto)
			}
			if ai.HasProto != tc.wantHas {
				t.Errorf("HasProto = %v, want %v", ai.HasProto, tc.wantHas)
			}
		})
	}
}
