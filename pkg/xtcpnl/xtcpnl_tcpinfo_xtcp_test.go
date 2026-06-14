package xtcpnl

import (
	"os"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
)

// DeserializeTCPInfoXTCP dispatches on payload length to add successive
// kernel-version tails (4_15 base → 4_19 → 5_4 → 6_6 → 6_10). Each size
// breakpoint exercises a different deserializeTCPInfoXTCPTail* function.

func TestDeserializeTCPInfoXTCP_short(t *testing.T) {
	x := &xtcp_flat_record.XtcpFlatRecord{}
	if err := DeserializeTCPInfoXTCP([]byte{0x00, 0x01}, x); err != ErrTCPInfoSmall {
		t.Errorf("short buffer err = %v, want ErrTCPInfoSmall", err)
	}
}

func TestDeserializeTCPInfoXTCP_4_15_base(t *testing.T) {
	// TCPInfo4_15 is the base (~192 bytes). Build a payload of exactly
	// that size and verify the dispatch exits after the base parse.
	data := make([]byte, TCPInfo4_15_SizeCst)
	data[0] = 0x01 // TcpInfoState
	x := &xtcp_flat_record.XtcpFlatRecord{}
	if err := DeserializeTCPInfoXTCP(data, x); err != nil {
		t.Fatalf("err = %v", err)
	}
	if x.TcpInfoState != 1 {
		t.Errorf("State = %d, want 1", x.TcpInfoState)
	}
}

func TestDeserializeTCPInfoXTCP_4_19(t *testing.T) {
	data := make([]byte, TCPInfo4_19_219_SizeCst)
	x := &xtcp_flat_record.XtcpFlatRecord{}
	if err := DeserializeTCPInfoXTCP(data, x); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestDeserializeTCPInfoXTCP_5_4(t *testing.T) {
	data := make([]byte, TCPInfo5_4_281_SizeCst)
	x := &xtcp_flat_record.XtcpFlatRecord{}
	if err := DeserializeTCPInfoXTCP(data, x); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestDeserializeTCPInfoXTCP_6_6(t *testing.T) {
	data := make([]byte, TCPInfo6_6_44_SizeCst)
	x := &xtcp_flat_record.XtcpFlatRecord{}
	if err := DeserializeTCPInfoXTCP(data, x); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestDeserializeTCPInfoXTCP_6_10(t *testing.T) {
	data := make([]byte, TCPInfo6_10_3_SizeCst)
	x := &xtcp_flat_record.XtcpFlatRecord{}
	if err := DeserializeTCPInfoXTCP(data, x); err != nil {
		t.Fatalf("err = %v", err)
	}
}

// Real fixture: the 7_0_3 capture decoded end-to-end.
func TestDeserializeTCPInfoXTCP_realFixture_7_0_3(t *testing.T) {
	bs, err := os.ReadFile("./testdata/7_0_3/netlink_sock_diag_response_7_0_3_sport26546_dport443_info")
	if err != nil {
		t.Skipf("fixture: %v", err)
	}
	x := &xtcp_flat_record.XtcpFlatRecord{}
	if err := DeserializeTCPInfoXTCP(bs[RTAttrSizeCst:], x); err != nil {
		t.Errorf("XTCP deserialize: %v", err)
	}
}

func TestDeserializeTCPInfoXTCP_realFixture_4_19(t *testing.T) {
	bs, err := os.ReadFile("./testdata/4_19_319/attribute_info")
	if err != nil {
		t.Skipf("fixture: %v", err)
	}
	x := &xtcp_flat_record.XtcpFlatRecord{}
	if err := DeserializeTCPInfoXTCP(bs[RTAttrSizeCst:], x); err != nil {
		t.Errorf("XTCP deserialize: %v", err)
	}
}

// Tail* funcs are not externally callable when payload is too short — the
// internal guard returns early. Exercise that branch directly.
func TestDeserializeTCPInfoXTCPTail_shortReturnsEarly(t *testing.T) {
	x := &xtcp_flat_record.XtcpFlatRecord{}
	deserializeTCPInfoXTCPBase(make([]byte, 10), x)     // < TCPInfoMinSizeCst → no-op
	deserializeTCPInfoXTCPTail4_19(make([]byte, 10), x) // < tail size → no-op
	deserializeTCPInfoXTCPTail5_4(make([]byte, 10), x)  // < tail size → no-op
	deserializeTCPInfoXTCPTail6_6(make([]byte, 10), x)  // < tail size → no-op
	deserializeTCPInfoXTCPTail6_10(make([]byte, 10), x) // < tail size → no-op
}
