package xtcpnl

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

// cat concatenates byte slices, so a wire body reads as its parts.
func cat(parts ...[]byte) []byte {
	var b []byte
	for _, p := range parts {
		b = append(b, p...)
	}
	return b
}

// ifalHdr builds a 12-byte ifaddrlblmsg: family, reserved, prefixlen, flags, then
// the int32 index and u32 seq, little-endian.
func ifalHdr(family, prefixlen, flags uint8, index int32, seq uint32) []byte {
	return cat(
		[]byte{family, 0, prefixlen, flags},
		le32(uint32(index)),
		le32(seq),
	)
}

// addr6b builds a 16-byte IPv6 address from its leading bytes, zero-padded right.
func addr6b(lead ...byte) []byte {
	b := make([]byte, 16)
	copy(b, lead)
	return b
}

// TestParseNewAddrLabel decodes RTM_NEWADDRLABEL bodies. The positive rows are
// message bodies transcribed byte-for-byte from netlink_route_getaddrlabel.pcap
// (loopback, default route and the IPv4-mapped entry). The boundary, corner and
// negative rows are hand-built wire edges — a header with no attrs, a short body,
// an IFAL_LABEL whose payload is not four bytes, an address without a label, an
// unknown attribute, and a truncated attribute — none of which the default table
// exercises.
//
// go test ./pkg/xtcpnl/ -run TestParseNewAddrLabel
func TestParseNewAddrLabel(t *testing.T) {
	tests := []struct {
		description  string
		body         []byte
		wantErr      error
		wantFamily   uint8
		wantPrefix   uint8
		wantIndex    int32
		wantAddress  []byte
		wantLabel    uint32
		wantHasLabel bool
	}{
		{
			description:  "positive: the captured loopback entry ::1/128 label 0 (capture)",
			body:         mustHex(t, "0a008000000000000a00000014000100000000000000000000000000000000010800020000000000"),
			wantFamily:   unix.AF_INET6,
			wantPrefix:   128,
			wantAddress:  mustHex(t, "00000000000000000000000000000001"),
			wantLabel:    0,
			wantHasLabel: true,
		},
		{
			description:  "positive: the captured default-route entry ::/0 label 1, prefixlen 0 (capture)",
			body:         mustHex(t, "0a000000000000000a00000014000100000000000000000000000000000000000800020001000000"),
			wantFamily:   unix.AF_INET6,
			wantPrefix:   0,
			wantAddress:  mustHex(t, "00000000000000000000000000000000"),
			wantLabel:    1,
			wantHasLabel: true,
		},
		{
			description:  "corner: the captured IPv4-mapped entry ::ffff:0.0.0.0/96 label 4 (capture)",
			body:         mustHex(t, "0a006000000000000a0000001400010000000000000000000000ffff000000000800020004000000"),
			wantFamily:   unix.AF_INET6,
			wantPrefix:   96,
			wantAddress:  mustHex(t, "00000000000000000000ffff00000000"),
			wantLabel:    4,
			wantHasLabel: true,
		},
		{
			description: "boundary: a header-only reply decodes the ifaddrlblmsg and leaves the attributes absent",
			body:        ifalHdr(unix.AF_INET6, 64, 0, 0, 0),
			wantFamily:  unix.AF_INET6,
			wantPrefix:  64,
			wantAddress: nil,
		},
		{
			description: "boundary: a body one byte short of the header is an error, not a zero entry",
			body:        make([]byte, IfAddrlblmsgSizeCst-1),
			wantErr:     ErrIfAddrlblmsgSmall,
		},
		{
			description:  "corner: an IFAL_LABEL payload that is not four bytes leaves HasLabel false (ip/ipaddrlabel.c:87 guard)",
			body:         cat(ifalHdr(unix.AF_INET6, 128, 0, 0, 0), rtattr(unix.IFAL_ADDRESS, addr6b(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1)), rtattr(unix.IFAL_LABEL, le16(7))),
			wantFamily:   unix.AF_INET6,
			wantPrefix:   128,
			wantAddress:  addr6b(0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1),
			wantHasLabel: false,
		},
		{
			description: "corner: IFAL_ADDRESS present with no IFAL_LABEL sets the address and leaves HasLabel false",
			body:        cat(ifalHdr(unix.AF_INET6, 16, 0, 0, 0), rtattr(unix.IFAL_ADDRESS, addr6b(0x3f, 0xfe))),
			wantFamily:  unix.AF_INET6,
			wantPrefix:  16,
			wantAddress: addr6b(0x3f, 0xfe),
		},
		{
			description:  "corner: an unknown attribute type in the nest is ignored, the known fields still decode",
			body:         cat(ifalHdr(unix.AF_INET6, 7, 0, 0, 0), rtattr(99, le32(0xdeadbeef)), rtattr(unix.IFAL_LABEL, le32(5))),
			wantFamily:   unix.AF_INET6,
			wantPrefix:   7,
			wantLabel:    5,
			wantHasLabel: true,
		},
		{
			description: "negative: an attribute whose declared length exceeds the remaining body is a parse error",
			body:        cat(ifalHdr(unix.AF_INET6, 10, 0, 0, 0), rtattrOverrunning(unix.IFAL_ADDRESS, 20)),
			wantErr:     ErrRTAttrSmall,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ai, err := ParseNewAddrLabel(tc.body)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if ai.Family != tc.wantFamily {
				t.Errorf("Family = %d, want %d", ai.Family, tc.wantFamily)
			}
			if ai.Prefixlen != tc.wantPrefix {
				t.Errorf("Prefixlen = %d, want %d", ai.Prefixlen, tc.wantPrefix)
			}
			if ai.Index != tc.wantIndex {
				t.Errorf("Index = %d, want %d", ai.Index, tc.wantIndex)
			}
			if !bytes.Equal(ai.Address, tc.wantAddress) {
				t.Errorf("Address = %v, want %v", ai.Address, tc.wantAddress)
			}
			if ai.Label != tc.wantLabel {
				t.Errorf("Label = %d, want %d", ai.Label, tc.wantLabel)
			}
			if ai.HasLabel != tc.wantHasLabel {
				t.Errorf("HasLabel = %v, want %v", ai.HasLabel, tc.wantHasLabel)
			}
		})
	}
}

// TestDeserializeIfAddrLblMsg covers the fixed-header read in isolation: every
// byte is distinct so a mis-sliced field shows up, the exact-size body is the
// boundary that must succeed, and one byte less is the boundary that must fail.
//
// go test ./pkg/xtcpnl/ -run TestDeserializeIfAddrLblMsg
func TestDeserializeIfAddrLblMsg(t *testing.T) {
	tests := []struct {
		description string
		data        []byte
		wantErr     error
		wantN       int
		want        IfAddrLblMsg
	}{
		{
			description: "positive: distinct bytes land in their own fields",
			data:        cat([]byte{unix.AF_INET6, 0xaa, 0x40, 0x01}, le32(7), le32(0x11223344)),
			wantN:       IfAddrlblmsgSizeCst,
			want:        IfAddrLblMsg{Family: unix.AF_INET6, Reserved: 0xaa, Prefixlen: 0x40, Flags: 0x01, Index: 7, Seq: 0x11223344},
		},
		{
			description: "boundary: trailing bytes past the header are ignored and n is the header size",
			data:        cat(ifalHdr(unix.AF_INET6, 128, 0, 3, 9), []byte{0xde, 0xad}),
			wantN:       IfAddrlblmsgSizeCst,
			want:        IfAddrLblMsg{Family: unix.AF_INET6, Prefixlen: 128, Index: 3, Seq: 9},
		},
		{
			description: "boundary: a body one byte short of the header is an error",
			data:        make([]byte, IfAddrlblmsgSizeCst-1),
			wantErr:     ErrIfAddrlblmsgSmall,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var h IfAddrLblMsg
			n, err := DeserializeIfAddrLblMsg(tc.data, &h)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if n != tc.wantN {
				t.Errorf("n = %d, want %d", n, tc.wantN)
			}
			if h != tc.want {
				t.Errorf("header = %+v, want %+v", h, tc.want)
			}
		})
	}
}

// TestBuildDumpAddrLabelRequest pins the RTM_GETADDRLABEL dump byte-for-byte. The
// request is a 16-byte nlmsghdr (type 74, flags NLM_F_REQUEST|NLM_F_DUMP) plus a
// 12-byte ifaddrlblmsg carrying only ifal_family — the bare rtnl_addrlbldump_req
// (lib/libnetlink.c:365-379). The AF_UNSPEC and AF_INET6 forms differ in exactly
// one byte, which is the whole of what `-6` would change at this layer (the
// substitution itself lives in obj_addrlabel.go).
//
// go test ./pkg/xtcpnl/ -run TestBuildDumpAddrLabelRequest
func TestBuildDumpAddrLabelRequest(t *testing.T) {
	tests := []struct {
		description string
		family      uint8
		seq         uint32
		want        string
	}{
		{
			description: "positive: AF_INET6 — the family a substituted `ip addrlabel show` asks for",
			family:      unix.AF_INET6, seq: 5,
			want: "1c0000004a0001030500000000000000" + "0a00000000000000" + "00000000",
		},
		{
			description: "boundary: AF_UNSPEC differs from AF_INET6 in the family byte alone",
			family:      unix.AF_UNSPEC, seq: 5,
			want: "1c0000004a0001030500000000000000" + "0000000000000000" + "00000000",
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := BuildDumpAddrLabelRequest(tc.family, tc.seq)
			if want := mustHex(t, tc.want); !bytes.Equal(got, want) {
				t.Fatalf("request mismatch\ngot  %s\nwant %s", hex.EncodeToString(got), tc.want)
			}
			// negative: a dump carries NLM_F_DUMP; a show that lost it would be a
			// point-GET the kernel answers with one entry, not the table.
			flags := binary.LittleEndian.Uint16(got[6:8])
			if flags&unix.NLM_F_DUMP == 0 {
				t.Errorf("flags %#x lack NLM_F_DUMP", flags)
			}
			// boundary: the family byte is the first byte of the body, nothing else
			// is set, and the body is exactly one ifaddrlblmsg.
			if got[NlMsgHdrSizeCst] != tc.family {
				t.Errorf("ifal_family = %d, want %d", got[NlMsgHdrSizeCst], tc.family)
			}
			if body := got[NlMsgHdrSizeCst:]; len(body) != IfAddrlblmsgSizeCst {
				t.Errorf("body length = %d, want %d", len(body), IfAddrlblmsgSizeCst)
			}
		})
	}
}
