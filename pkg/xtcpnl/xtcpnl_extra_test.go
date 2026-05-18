package xtcpnl

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/xtcp_flat_record"
)

// ───────────────────────────────────────────────────────────────────────
// Readfile — happy path + missing
// ───────────────────────────────────────────────────────────────────────

func TestReadfile_happy(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "f.bin")
	want := []byte("hello")
	if err := os.WriteFile(p, want, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Readfile(p)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("got %q, want hello", got)
	}
}

func TestReadfile_missing(t *testing.T) {
	if _, err := Readfile("/no/such/path"); err == nil {
		t.Error("missing path should error")
	}
}

// ───────────────────────────────────────────────────────────────────────
// DeserializeNlMsgHdrRelection / DeserializeInetDiagReqV2Relection
// + DeserializeInetDiagSockIDReflection — happy paths via fixtures
// ───────────────────────────────────────────────────────────────────────

func TestDeserializeNlMsgHdrRelection(t *testing.T) {
	// 16-byte NlMsgHdr requires exactly 16 bytes.
	data := make([]byte, NlMsgHdrSizeCst)
	hdr := new(NlMsgHdr)
	if _, err := DeserializeNlMsgHdrRelection(data, hdr); err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestDeserializeNlMsgHdrRelection_short(t *testing.T) {
	hdr := new(NlMsgHdr)
	if _, err := DeserializeNlMsgHdrRelection([]byte{0x01}, hdr); err == nil {
		t.Error("short buffer should error")
	}
}

func TestDeserializeInetDiagSockIDReflection(t *testing.T) {
	data := make([]byte, InetDiagSockIDSizeCst)
	sock := new(InetDiagSockID)
	if _, err := DeserializeInetDiagSockIDReflection(data, sock); err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestDeserializeInetDiagSockIDReflection_short(t *testing.T) {
	sock := new(InetDiagSockID)
	if _, err := DeserializeInetDiagSockIDReflection([]byte{0x01}, sock); err == nil {
		t.Error("short buffer should error")
	}
}

func TestDeserializeInetDiagReqV2Relection(t *testing.T) {
	// InetDiagReqV2 (with embedded SockID) is 56 bytes total — binary.Read
	// needs the full struct.
	data := make([]byte, InetDiagReqV2SizeCst)
	req := new(InetDiagReqV2)
	sock := new(InetDiagSockID)
	if _, err := DeserializeInetDiagReqV2Relection(data, req, sock); err != nil {
		t.Errorf("err = %v", err)
	}
}

// ───────────────────────────────────────────────────────────────────────
// DeserializeInetDiagMsgXTCP / DeserializeInetDiagSockIDXTCP /
// DeserializeInetDiagMsgWG / DeserializeInetDiagMsgXTCPWG
// ───────────────────────────────────────────────────────────────────────

func TestDeserializeInetDiagMsgXTCP_short(t *testing.T) {
	x := &xtcp_flat_record.XtcpFlatRecord{}
	if err := DeserializeInetDiagMsgXTCP(make([]byte, 8), x); err != ErrInetDiagMsgSmall {
		t.Errorf("short err = %v, want ErrInetDiagMsgSmall", err)
	}
}

func TestDeserializeInetDiagMsgXTCP_full(t *testing.T) {
	x := &xtcp_flat_record.XtcpFlatRecord{}
	data := make([]byte, InetDiagMsgSizeCst)
	if err := DeserializeInetDiagMsgXTCP(data, x); err != nil {
		t.Errorf("full err = %v", err)
	}
}

func TestDeserializeInetDiagSockIDXTCP_short(t *testing.T) {
	x := &xtcp_flat_record.XtcpFlatRecord{}
	if err := DeserializeInetDiagSockIDXTCP(make([]byte, 8), x); err != ErrInetDiagSockIDSmall {
		t.Errorf("short err = %v, want ErrInetDiagSockIDSmall", err)
	}
}

func TestDeserializeInetDiagSockIDXTCP_full(t *testing.T) {
	x := &xtcp_flat_record.XtcpFlatRecord{}
	data := make([]byte, InetDiagSockIDSizeCst)
	if err := DeserializeInetDiagSockIDXTCP(data, x); err != nil {
		t.Errorf("full err = %v", err)
	}
}

// WG variants: wrap base in defer wg.Done().
func TestDeserializeInetDiagMsgWG(t *testing.T) {
	wg := new(sync.WaitGroup)
	wg.Add(1)
	idm := new(InetDiagMsg)
	sock := new(InetDiagSockID)
	_, _ = DeserializeInetDiagMsgWG(wg, make([]byte, InetDiagMsgSizeCst), idm, sock) //nolint:errcheck // test plumbing
	wg.Wait()
}

func TestDeserializeInetDiagMsgXTCPWG(t *testing.T) {
	wg := new(sync.WaitGroup)
	wg.Add(1)
	x := &xtcp_flat_record.XtcpFlatRecord{}
	_ = DeserializeInetDiagMsgXTCPWG(wg, make([]byte, InetDiagMsgSizeCst), x) //nolint:errcheck // test plumbing
	wg.Wait()
}

// DeserializeCongInfoXTCP: 4-byte prefix dispatches to one of 5 congestion
// algorithm enums.
func TestDeserializeCongInfoXTCP_short(t *testing.T) {
	x := &xtcp_flat_record.XtcpFlatRecord{}
	if err := DeserializeCongInfoXTCP([]byte{0x01}, x); err != ErrCongInfoSmall {
		t.Errorf("err = %v, want ErrCongInfoSmall", err)
	}
}

func TestDeserializeCongInfoXTCP_cubic(t *testing.T) {
	x := &xtcp_flat_record.XtcpFlatRecord{}
	data := []byte("cub\x00")
	if err := DeserializeCongInfoXTCP(data, x); err != nil {
		t.Fatalf("err = %v", err)
	}
	if x.CongestionAlgorithmEnum != xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_CUBIC {
		t.Errorf("alg = %v, want CUBIC", x.CongestionAlgorithmEnum)
	}
}

func TestDeserializeCongInfoXTCP_bbr(t *testing.T) {
	x := &xtcp_flat_record.XtcpFlatRecord{}
	data := []byte("bbr\x00")
	if err := DeserializeCongInfoXTCP(data, x); err != nil {
		t.Fatalf("err = %v", err)
	}
	if x.CongestionAlgorithmEnum != xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_BBR1 {
		t.Errorf("alg = %v, want BBR1", x.CongestionAlgorithmEnum)
	}
}

func TestDeserializeCongInfoXTCP_dctcp(t *testing.T) {
	x := &xtcp_flat_record.XtcpFlatRecord{}
	data := []byte("dct\x00")
	if err := DeserializeCongInfoXTCP(data, x); err != nil {
		t.Fatalf("err = %v", err)
	}
	if x.CongestionAlgorithmEnum != xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_DCTCP {
		t.Errorf("alg = %v, want DCTCP", x.CongestionAlgorithmEnum)
	}
}

func TestDeserializeCongInfoXTCP_vegas(t *testing.T) {
	x := &xtcp_flat_record.XtcpFlatRecord{}
	data := []byte("veg\x00")
	if err := DeserializeCongInfoXTCP(data, x); err != nil {
		t.Fatalf("err = %v", err)
	}
	if x.CongestionAlgorithmEnum != xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_VEGAS {
		t.Errorf("alg = %v, want VEGAS", x.CongestionAlgorithmEnum)
	}
}

func TestDeserializeCongInfoXTCP_bbr2(t *testing.T) {
	x := &xtcp_flat_record.XtcpFlatRecord{}
	// "bbr2" — 3-byte prefix "bbr" matches the bbr branch
	data := []byte("bbr2")
	if err := DeserializeCongInfoXTCP(data, x); err != nil {
		t.Fatalf("err = %v", err)
	}
	if x.CongestionAlgorithmEnum != xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_BBR1 {
		t.Errorf("alg = %v, want BBR1", x.CongestionAlgorithmEnum)
	}
}

func TestDeserializeCongInfoXTCP_unknown(t *testing.T) {
	// Unknown prefix: switch falls through to no-op (enum stays zero).
	x := &xtcp_flat_record.XtcpFlatRecord{}
	data := []byte("xxxx")
	if err := DeserializeCongInfoXTCP(data, x); err != nil {
		t.Fatalf("err = %v", err)
	}
	if x.CongestionAlgorithmEnum != 0 {
		t.Errorf("unknown prefix should leave enum at 0; got %v", x.CongestionAlgorithmEnum)
	}
}
