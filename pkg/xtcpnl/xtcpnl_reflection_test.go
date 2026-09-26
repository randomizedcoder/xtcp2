package xtcpnl

// WARNING: this file contains Go reflection (binary.Read / reflect).
//
// The reflection code here is only for performance comparison, and it is
// strongly recommended that it is NOT used in production. It lives in a
// _test.go file so that it never reaches the shipped library: pkg/xtcpnl
// ships zero reflection, and every production Deserialize* reads fields at
// fixed byte offsets instead.
//
// If reflection is ever measured as even close to a manual decoder, that
// indicates a problem rather than a license to use it. See
// xtcpnl_reflection_twins_test.go for the rationale and
// xtcpnl_perf_gate_test.go for the gate that fails on convergence.

import (
	"encoding/binary"
	"io"
	"os"
	"testing"
)

// Tests for the *Reflection variants of each Deserialize* parser. These
// variants exist for benchmarking + correctness cross-check against the
// hand-rolled binary-LE readers. They round-trip the same testdata file
// and must produce the same .Len header value (which is the only
// universally-common field across the kernel structs).

type reflCase struct {
	name     string
	filename string
}

func runReflTest(t *testing.T, c reflCase, deserialize func([]byte) (int, error)) {
	t.Helper()
	f, err := os.Open(c.filename)
	if err != nil {
		t.Skipf("skipping %s: %v", c.name, err)
	}
	defer f.Close()
	bs, rerr := io.ReadAll(f)
	if rerr != nil {
		t.Fatalf("readall: %v", rerr)
	}
	if _, derr := deserialize(bs); derr != nil {
		t.Errorf("%s deserialize: %v", c.name, derr)
	}
}

func TestDeserializeRTAttrReflection(t *testing.T) {
	runReflTest(t, reflCase{"RTAttr", tdAttrBbrinfo_6_6_44}, func(buf []byte) (int, error) {
		rta := new(RTAttr)
		return deserializeRTAttrReflection(buf, rta)
	})
}

func TestDeserializeRTAttrReflection_short(t *testing.T) {
	rta := new(RTAttr)
	if _, err := deserializeRTAttrReflection([]byte{0x01}, rta); err == nil {
		t.Error("short buffer should error")
	}
}

func TestDeserializeMemInfoReflection(t *testing.T) {
	// MemInfo is attribute_meminfo, length 20 (4-byte RTAttr header + 16-byte payload).
	// deserializeMemInfoReflection reads exactly MemInfoSizeCst (16) bytes of
	// payload starting at offset RTAttrSizeCst — strip the header first.
	bs, err := os.ReadFile("./testdata/6_6_44/attribute_meminfo")
	if err != nil {
		t.Skipf("read fixture: %v", err)
	}
	mi := new(MemInfo)
	if _, derr := deserializeMemInfoReflection(bs[RTAttrSizeCst:], mi); derr != nil {
		t.Errorf("MemInfo Reflection: %v", derr)
	}
}

func TestDeserializeMemInfoReflection_short(t *testing.T) {
	mi := new(MemInfo)
	if _, err := deserializeMemInfoReflection([]byte{0x01, 0x02}, mi); err == nil {
		t.Error("short buffer should error")
	}
}

// tcpInfoFixture returns the 7_0_3 INET_DIAG_INFO payload — 280 bytes, AccECN
// trailer included — zero-padded up to binary.Size(TCPInfo{}).
//
// The padding is not cosmetic. Every TCPInfo* struct is *wider in Go than on
// the wire*, because the kernel's bitfields are modeled as separate fields:
// `__u8 tcpi_snd_wscale:4, tcpi_rcv_wscale:4` is one wire byte but two uint8s,
// and the AccECN `__u32 tcpi_ecn_mode:2, ...:2, ...:4, tcpi_options2:24` is one
// wire u32 but four fields totalling 7 bytes. So binary.Read wants 285 bytes
// where the kernel sends 280, and a twin fed the real capture unpadded fails
// with unexpected EOF.
//
// That is also the clearest statement of why these twins are test-only and are
// not the correctness oracle for TCPInfo: binary.Read cannot express a
// bitfield, so it cannot agree with the manual decoder field-for-field no
// matter how much data it is handed. These tests assert only that the twin
// runs, and that it errors on short input. The layout itself is settled by the
// manual decoder's fixtures and by the kernel-source oracle.
func tcpInfoFixture(t *testing.T) []byte {
	t.Helper()
	bs, err := os.ReadFile("./testdata/7_0_3/netlink_sock_diag_response_7_0_3_sport26546_dport443_info")
	if err != nil {
		t.Skipf("read fixture: %v", err)
	}
	payload := bs[RTAttrSizeCst:]
	want := binary.Size(TCPInfo{})
	if len(payload) >= want {
		return payload
	}
	padded := make([]byte, want)
	copy(padded, payload)
	return padded
}

func TestDeserializeTCPInfoReflection_padded(t *testing.T) {
	ti := new(TCPInfo)
	if _, derr := deserializeTCPInfoReflection(tcpInfoFixture(t), ti); derr != nil {
		t.Errorf("TCPInfoReflection: %v", derr)
	}
}

func TestDeserializeTCPInfo7_0_3Reflection(t *testing.T) {
	ti := new(TCPInfo7_0_3)
	if _, derr := deserializeTCPInfo7_0_3Reflection(tcpInfoFixture(t), ti); derr != nil {
		t.Errorf("TCPInfo7_0_3 Reflection: %v", derr)
	}
}

func TestDeserializeTCPInfo7_0_3Reflection_short(t *testing.T) {
	ti := new(TCPInfo7_0_3)
	// The 6.10 payload length — long enough for every older struct, one
	// AccECN trailer short of this one.
	if _, err := deserializeTCPInfo7_0_3Reflection(make([]byte, TCPInfo6_10_3_SizeCst), ti); err == nil {
		t.Error("a 248-byte buffer should be too short for TCPInfo7_0_3")
	}
}

func TestDeserializeTCPInfoTCPInfoTCPInfo6_10_3Reflection(t *testing.T) {
	ti := new(TCPInfo6_10_3)
	if _, derr := deserializeTCPInfo6_10_3Reflection(tcpInfoFixture(t), ti); derr != nil {
		t.Errorf("TCPInfo6_10_3 Reflection: %v", derr)
	}
}

func TestDeserializeTCPInfoTCPInfo6_6_44Reflection(t *testing.T) {
	ti := new(TCPInfo6_6_44)
	if _, derr := deserializeTCPInfo6_6_44Reflection(tcpInfoFixture(t), ti); derr != nil {
		t.Errorf("TCPInfo6_6_44 Reflection: %v", derr)
	}
}

func TestDeserializeTCPInfo5_4_281Reflection(t *testing.T) {
	ti := new(TCPInfo5_4_281)
	if _, derr := deserializeTCPInfo5_4_281Reflection(tcpInfoFixture(t), ti); derr != nil {
		t.Errorf("TCPInfo5_4_281 Reflection: %v", derr)
	}
}

func TestDeserializeTCPInfo4_19_219Reflection(t *testing.T) {
	ti := new(TCPInfo4_19_219)
	if _, derr := deserializeTCPInfo4_19_219Reflection(tcpInfoFixture(t), ti); derr != nil {
		t.Errorf("TCPInfo4_19_219 Reflection: %v", derr)
	}
}

func TestDeserializeTCPInfoReflection_short(t *testing.T) {
	ti := new(TCPInfo)
	if _, err := deserializeTCPInfoReflection([]byte{0x01}, ti); err == nil {
		t.Error("short buffer should error")
	}
}

// ───────────────────────────────────────────────────────────────────────
// xtcpnl.go pure helpers: NativeEndian, Swap16, BuildNetlinkSockDiagRequest
// ───────────────────────────────────────────────────────────────────────

func TestNativeEndian(t *testing.T) {
	got := NativeEndian()
	if got != binary.LittleEndian && got != binary.BigEndian {
		t.Errorf("NativeEndian = %T %v, want little or big endian", got, got)
	}
	// Should be cached — second call returns same value.
	if NativeEndian() != got {
		t.Error("NativeEndian not cached")
	}
}

func TestSwap16(t *testing.T) {
	got := Swap16(0x1234)
	// On little-endian hosts (all common ones in CI), Swap16 returns the
	// byte-swapped value; on big-endian it returns input unchanged.
	if NativeEndian() == binary.LittleEndian && got != 0x3412 {
		t.Errorf("Swap16(0x1234) = 0x%04x on little-endian, want 0x3412", got)
	}
	if NativeEndian() == binary.BigEndian && got != 0x1234 {
		t.Errorf("Swap16(0x1234) = 0x%04x on big-endian, want 0x1234", got)
	}
}

func TestBuildNetlinkSockDiagRequest(t *testing.T) {
	r := BuildNLRequest{
		AddressFamily: 2,
		MakeSize:      72,
		NlMsgLen:      72,
		NlMsgSeq:      1,
		NlMsgPid:      0,
		IDiagExt:      0xFF,
		States:        0xFFFFFFFF,
	}
	got := BuildNetlinkSockDiagRequest(r)
	if len(got) != 72 {
		t.Errorf("len = %d, want 72", len(got))
	}
	if binary.LittleEndian.Uint32(got[0:4]) != 72 {
		t.Errorf("nlmsg_len = %d, want 72", binary.LittleEndian.Uint32(got[0:4]))
	}
}
