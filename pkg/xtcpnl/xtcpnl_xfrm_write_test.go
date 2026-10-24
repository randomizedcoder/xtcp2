package xtcpnl

import (
	"encoding/hex"
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

// TestIsAdmittedXfrmWrite checks the XFRM write gate admits the SA writes
// `ip xfrm state` sends, and nothing else.
//
// go test ./pkg/xtcpnl/ -run TestIsAdmittedXfrmWrite
func TestIsAdmittedXfrmWrite(t *testing.T) {
	tests := []struct {
		description string
		msgType     uint16
		expected    bool
	}{
		{"positive: NEWSA, add", XfrmMsgNewSA, true},
		{"positive: UPDSA, update", XfrmMsgUpdSA, true},
		{"positive: ALLOCSPI, allocspi", XfrmMsgAllocSPI, true},
		{"positive: DELSA, delete and deleteall", XfrmMsgDelSA, true},
		{"positive: FLUSHSA, flush", XfrmMsgFlushSA, true},
		{"negative: GETSA, a read", XfrmMsgGetSA, false},
		{"negative: GETSADINFO, a read", XfrmMsgGetSADInfo, false},
		{"negative: NEWPOLICY, not yet admitted", XfrmMsgNewPolicy, false},
		{"negative: FLUSHPOLICY, not yet admitted", XfrmMsgFlushPolicy, false},
		{"negative: SETDEFAULT, not yet admitted", XfrmMsgSetDefault, false},
		{"corner: EXPIRE, a kernel event", XfrmMsgExpire, false},
		{"boundary: 0", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := IsAdmittedXfrmWrite(tc.msgType); got != tc.expected {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}

// TestBuildXfrmWriteRequest checks an admitted write's layout: the type,
// flags plus NLM_F_REQUEST, the header, its padding before attributes, and
// the refusals: a type the gate does not admit, and a header not its
// struct's size.
//
// go test ./pkg/xtcpnl/ -run TestBuildXfrmWriteRequest
func TestBuildXfrmWriteRequest(t *testing.T) {
	attr := []byte{8, 0, 21, 0, 1, 0, 0, 0}
	tests := []struct {
		description string
		msgType     uint16
		flags       uint16
		hdr, attrs  []byte
		expected    string // the request's hex
		err         error
	}{
		{description: "positive: FLUSHSA, its one byte unpadded, with an ACK", msgType: XfrmMsgFlushSA,
			flags: unix.NLM_F_ACK, hdr: []byte{50}, expected: "11000000" + "1c00" + "0500" + "07000000" + "00000000" + "32"},
		{description: "positive: DELSA with an attribute", msgType: XfrmMsgDelSA, hdr: make([]byte, 24), attrs: attr,
			expected: "30000000" + "1100" + "0100" + "07000000" + "00000000" + hex.EncodeToString(make([]byte, 24)) +
				"0800150001000000"},
		{description: "boundary: FLUSHSA with an attribute, its byte padded to four", msgType: XfrmMsgFlushSA,
			hdr: []byte{50}, attrs: attr, expected: "1c000000" + "1c00" + "0100" + "07000000" + "00000000" + "32000000" +
				"0800150001000000"},
		{description: "negative: GETSA, a read", msgType: XfrmMsgGetSA, hdr: make([]byte, 24), err: ErrXfrmWrite},
		{description: "negative: NEWPOLICY, not yet admitted", msgType: XfrmMsgNewPolicy, hdr: make([]byte, 168),
			err: ErrXfrmWrite},
		{description: "boundary: NEWSA with a header a byte short", msgType: XfrmMsgNewSA, hdr: make([]byte, 223),
			err: ErrBadFamilyHdr},
		{description: "corner: RTM_NEWLINK's number is NEWSA, its 224 bytes", msgType: unix.RTM_NEWLINK,
			hdr: make([]byte, 16), err: ErrBadFamilyHdr},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			b, err := BuildXfrmWriteRequest(tc.msgType, tc.flags, 7, tc.hdr, tc.attrs)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err %v, want %v", err, tc.err)
			}
			if got := hex.EncodeToString(b); got != tc.expected {
				t.Errorf("request\n got %s\nwant %s", got, tc.expected)
			}
		})
	}
}
