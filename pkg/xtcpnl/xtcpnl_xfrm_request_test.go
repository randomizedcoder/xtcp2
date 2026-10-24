package xtcpnl

import (
	"bytes"
	"encoding/hex"
	"errors"
	"testing"

	"golang.org/x/sys/unix"
)

// TestXfrmFamilyHdrLen is each XFRM type's attribute offset: its struct
// aligned to four, a dump's none, and -1 for what is not modeled.
//
// go test ./pkg/xtcpnl/ -run TestXfrmFamilyHdrLen
func TestXfrmFamilyHdrLen(t *testing.T) {
	const dump = uint16(unix.NLM_F_DUMP)
	tests := []struct {
		description string
		msgType     uint16
		flags       uint16
		expected    int
	}{
		{"positive: NEWSA, xfrm_usersa_info", XfrmMsgNewSA, unix.NLM_F_MULTI, 224},
		{"positive: UPDSA, the same struct", XfrmMsgUpdSA, 0, 224},
		{"positive: GETSA get, xfrm_usersa_id", XfrmMsgGetSA, 0, 24},
		{"boundary: GETSA dump, no header", XfrmMsgGetSA, dump, 0},
		{"positive: GETPOLICY get, xfrm_userpolicy_id", XfrmMsgGetPolicy, 0, 64},
		{"boundary: GETPOLICY dump, no header", XfrmMsgGetPolicy, dump, 0},
		{"corner: a dump flag on DELSA is no dump", XfrmMsgDelSA, dump, 24},
		{"corner: NLM_F_ROOT alone is no dump", XfrmMsgGetSA, unix.NLM_F_ROOT, 24},
		{"positive: NEWPOLICY, xfrm_userpolicy_info", XfrmMsgNewPolicy, 0, 168},
		{"positive: ALLOCSPI, xfrm_userspi_info", XfrmMsgAllocSPI, 0, 232},
		{"positive: ACQUIRE", XfrmMsgAcquire, 0, 280},
		{"positive: EXPIRE, its u8 padded", XfrmMsgExpire, 0, 232},
		{"positive: POLEXPIRE", XfrmMsgPolExpire, 0, 176},
		{"boundary: FLUSHSA's one byte aligned to four", XfrmMsgFlushSA, 0, 4},
		{"boundary: FLUSHPOLICY, none", XfrmMsgFlushPolicy, 0, 0},
		{"positive: GETAE, xfrm_aevent_id", XfrmMsgGetAE, 0, 48},
		{"positive: GETSADINFO, a u32", XfrmMsgGetSADInfo, 0, 4},
		{"positive: NEWSADINFO, the answer's u32", XfrmMsgNewSADInfo, 0, 4},
		{"positive: GETSPDINFO", XfrmMsgGetSPDInfo, 0, 4},
		{"positive: MAPPING", XfrmMsgMapping, 0, 64},
		{"boundary: GETDEFAULT's three bytes aligned to four", XfrmMsgGetDefault, 0, 4},
		{"negative: REPORT is not modeled", XfrmMsgReport, 0, -1},
		{"negative: MIGRATE_STATE is not modeled", XfrmMsgMigrateState, 0, -1},
		{"negative: below the base", 15, 0, -1},
		{"negative: past the enum", 42, 0, -1},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := XfrmFamilyHdrLen(tc.msgType, tc.flags); got != tc.expected {
				t.Errorf("got %d, want %d", got, tc.expected)
			}
		})
	}
}

// TestIsXfrmReadType is the XFRM read gate.
//
// go test ./pkg/xtcpnl/ -run TestIsXfrmReadType
func TestIsXfrmReadType(t *testing.T) {
	tests := []struct {
		description string
		msgType     uint16
		expected    bool
	}{
		{"positive: GETSA", XfrmMsgGetSA, true},
		{"positive: GETPOLICY", XfrmMsgGetPolicy, true},
		{"positive: GETAE", XfrmMsgGetAE, true},
		{"positive: GETSADINFO", XfrmMsgGetSADInfo, true},
		{"positive: GETSPDINFO", XfrmMsgGetSPDInfo, true},
		{"positive: GETDEFAULT", XfrmMsgGetDefault, true},
		{"negative: NEWSA", XfrmMsgNewSA, false},
		{"negative: DELSA", XfrmMsgDelSA, false},
		{"negative: FLUSHSA", XfrmMsgFlushSA, false},
		{"negative: SETDEFAULT", XfrmMsgSetDefault, false},
		{"corner: ALLOCSPI writes, a GET though it answers", XfrmMsgAllocSPI, false},
		{"boundary: 0", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := IsXfrmReadType(tc.msgType); got != tc.expected {
				t.Errorf("got %v, want %v", got, tc.expected)
			}
		})
	}
}

// TestBuildXfrmRequest lays out XFRM reads as `ip` does: the dump with no
// header, the get with its struct, GETDEFAULT's odd length.
//
// go test ./pkg/xtcpnl/ -run TestBuildXfrmRequest
func TestBuildXfrmRequest(t *testing.T) {
	const dump = uint16(unix.NLM_F_DUMP)
	attr := []byte{5, 0, 25, 0, 50, 0, 0, 0} // XFRMA_PROTO esp
	tests := []struct {
		description string
		msgType     uint16
		flags       uint16
		hdr, attrs  []byte
		expected    string // hex, or the error
		err         error
	}{
		{"positive: a GETSA dump, attributes right after the nlmsghdr", XfrmMsgGetSA, dump, nil, attr,
			"18000000" + "1200" + "0103" + "07000000" + "00000000" + "0500190032000000", nil},
		{"positive: GETSADINFO, its u32", XfrmMsgGetSADInfo, 0, []byte{0xff, 0xff, 0xff, 0xff}, nil,
			"14000000" + "2300" + "0100" + "07000000" + "00000000" + "ffffffff", nil},
		{"boundary: GETDEFAULT's three bytes, nlmsg_len 19", XfrmMsgGetDefault, 0, []byte{1, 2, 3}, nil,
			"13000000" + "2800" + "0100" + "07000000" + "00000000" + "010203", nil},
		{"corner: three bytes padded ahead of an attribute", XfrmMsgGetDefault, 0, []byte{1, 2, 3}, attr,
			"1c000000" + "2800" + "0100" + "07000000" + "00000000" + "01020300" + "0500190032000000", nil},
		{"negative: a GETSA get with no header", XfrmMsgGetSA, 0, nil, nil, "", ErrBadFamilyHdr},
		{"negative: a dump with a header", XfrmMsgGetSA, dump, make([]byte, 24), nil, "", ErrBadFamilyHdr},
		{"negative: NEWSA is a write", XfrmMsgNewSA, 0, make([]byte, 224), nil, "", ErrNotAnXfrmRead},
		{"negative: FLUSHSA is a write", XfrmMsgFlushSA, 0, []byte{0}, nil, "", ErrNotAnXfrmRead},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			b, err := BuildXfrmRequest(tc.msgType, tc.flags, 7, tc.hdr, tc.attrs)
			if !errors.Is(err, tc.err) {
				t.Fatalf("err %v, want %v", err, tc.err)
			}
			if err == nil {
				if got := hex.EncodeToString(b); got != tc.expected {
					t.Errorf("got %s, want %s", got, tc.expected)
				}
			}
		})
	}
	t.Run("corner: the caller's header is not written past", func(t *testing.T) {
		hdr := []byte{1, 2, 3, 9}
		if _, err := BuildXfrmRequest(XfrmMsgGetDefault, 0, 1, hdr[:3], attr); err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(hdr, []byte{1, 2, 3, 9}) {
			t.Errorf("header buffer %x, want 01020309", hdr)
		}
	})
}

// TestXfrmMsgTypeName names the XFRM types.
//
// go test ./pkg/xtcpnl/ -run TestXfrmMsgTypeName
func TestXfrmMsgTypeName(t *testing.T) {
	tests := []struct {
		description string
		msgType     uint16
		expected    string
	}{
		{"boundary: the first, NEWSA", XfrmMsgNewSA, "XFRM_MSG_NEWSA"},
		{"positive: GETSADINFO", XfrmMsgGetSADInfo, "XFRM_MSG_GETSADINFO"},
		{"boundary: the last, MIGRATE_STATE", XfrmMsgMigrateState, "XFRM_MSG_MIGRATE_STATE"},
		{"negative: below the base", 15, "type(15)"},
		{"negative: past the end", 42, "type(42)"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := XfrmMsgTypeName(tc.msgType); got != tc.expected {
				t.Errorf("got %q, want %q", got, tc.expected)
			}
		})
	}
}
