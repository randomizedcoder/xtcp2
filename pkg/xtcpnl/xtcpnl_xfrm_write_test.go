package xtcpnl

import (
	"encoding/hex"
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

// TestIsAdmittedXfrmWrite checks the XFRM write gate admits the SA and
// policy writes `ip xfrm state` and `ip xfrm policy` send, and nothing else.
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
		{"positive: NEWPOLICY, policy add", XfrmMsgNewPolicy, true},
		{"positive: UPDPOLICY, policy update", XfrmMsgUpdPolicy, true},
		{"positive: DELPOLICY, policy delete and deleteall", XfrmMsgDelPolicy, true},
		{"positive: FLUSHPOLICY, policy flush", XfrmMsgFlushPolicy, true},
		{"positive: NEWSPDINFO, policy set", XfrmMsgNewSPDInfo, true},
		{"positive: SETDEFAULT, policy setdefault", XfrmMsgSetDefault, true},
		{"negative: GETPOLICY, a read", XfrmMsgGetPolicy, false},
		{"negative: GETSPDINFO, a read", XfrmMsgGetSPDInfo, false},
		{"negative: GETDEFAULT, a read", XfrmMsgGetDefault, false},
		{"corner: POLEXPIRE, a kernel event", XfrmMsgPolExpire, false},
		{"corner: NEWSADINFO, the kernel's answer, never sent", XfrmMsgNewSADInfo, false},
		{"corner: MIGRATE, ip's but not goip's", XfrmMsgMigrate, false},
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
		{description: "positive: FLUSHPOLICY, no header, with an ACK", msgType: XfrmMsgFlushPolicy,
			flags: unix.NLM_F_ACK, expected: "10000000" + "1d00" + "0500" + "07000000" + "00000000"},
		{description: "positive: SETDEFAULT, its three bytes unpadded", msgType: XfrmMsgSetDefault,
			flags: unix.NLM_F_ACK, hdr: []byte{1, 1, 2}, expected: "13000000" + "2700" + "0500" + "07000000" +
				"00000000" + "010102"},
		{description: "positive: NEWSPDINFO, its u32 and an attribute", msgType: XfrmMsgNewSPDInfo,
			hdr: []byte{0xff, 0xff, 0xff, 0xff}, attrs: attr, expected: "1c000000" + "2400" + "0100" + "07000000" +
				"00000000" + "ffffffff" + "0800150001000000"},
		{description: "negative: GETPOLICY, a read", msgType: XfrmMsgGetPolicy, hdr: make([]byte, 64),
			err: ErrXfrmWrite},
		{description: "boundary: NEWPOLICY with a header a byte short", msgType: XfrmMsgNewPolicy,
			hdr: make([]byte, 167), err: ErrBadFamilyHdr},
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
