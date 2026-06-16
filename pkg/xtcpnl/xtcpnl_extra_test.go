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

// Readfile previously used a bufio.Reader and called .Read(buf) ONCE,
// which returns at most bufio's internal buffer (4096 bytes). Any file
// larger than that produced n=4096, the n!=size check tripped, and
// the function returned an error. The contract — "read the whole file"
// — was silently broken for inputs over 4 KB.
func TestReadfile_largeFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "big.bin")
	// 32 KB — well over the bufio default of 4 KB.
	want := make([]byte, 32*1024)
	for i := range want {
		want[i] = byte(i & 0xff)
	}
	if err := os.WriteFile(p, want, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Readfile(p)
	if err != nil {
		t.Fatalf("err = %v (the bufio.Read short-read bug fires here)", err)
	}
	if len(got) != len(want) {
		t.Fatalf("got %d bytes, want %d (bufio.Read returned a single 4 KB chunk pre-fix)", len(got), len(want))
	}
	for i, b := range got {
		if b != want[i] {
			t.Fatalf("byte %d: got %#x want %#x", i, b, want[i])
			break
		}
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
	_, _ = DeserializeInetDiagMsgWG(wg, make([]byte, InetDiagMsgSizeCst), idm, sock)
	wg.Wait()
}

func TestDeserializeInetDiagMsgXTCPWG(t *testing.T) {
	wg := new(sync.WaitGroup)
	wg.Add(1)
	x := &xtcp_flat_record.XtcpFlatRecord{}
	_ = DeserializeInetDiagMsgXTCPWG(wg, make([]byte, InetDiagMsgSizeCst), x)
	wg.Wait()
}

// TestDeserializeCongInfoXTCP_short exercises the length-guard branch
// separately — every other case has the 4-byte minimum.
func TestDeserializeCongInfoXTCP_short(t *testing.T) {
	x := &xtcp_flat_record.XtcpFlatRecord{}
	if err := DeserializeCongInfoXTCP([]byte{0x01}, x); err != ErrCongInfoSmall {
		t.Errorf("err = %v, want ErrCongInfoSmall", err)
	}
}

// TestDeserializeCongInfoXTCP_dispatch is the table-driven combination of
// the previous 8 one-off tests. Each row exercises one prefix → enum
// mapping; the BBR row covers the data[3] sub-discriminator added in
// bug 50. An empty wantAlg means "enum should stay at zero" (unknown
// prefix branch).
func TestDeserializeCongInfoXTCP_dispatch(t *testing.T) {
	cases := []struct {
		name    string
		data    []byte
		wantAlg xtcp_flat_record.XtcpFlatRecord_CongestionAlgorithm
	}{
		{"cubic", []byte("cub\x00"), xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_CUBIC},
		{"bbr1_explicit_nul", []byte{'b', 'b', 'r', 0}, xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_BBR1},
		{"bbr1_prefix", []byte("bbr\x00"), xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_BBR1},
		{"bbr2", []byte("bbr2"), xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_BBR2},
		{"bbr3", []byte("bbr3"), xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_BBR3},
		{"dctcp", []byte("dct\x00"), xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_DCTCP},
		{"vegas", []byte("veg\x00"), xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_VEGAS},
		{"unknown_prefix_stays_zero", []byte("xxxx"), 0},
		// "bbr" with garbage 4th byte (anything but '2' or '3') falls
		// back to BBR1 — covers the default-case of the inner switch.
		{"bbr_garbage_byte_falls_back_to_bbr1", []byte("bbrX"), xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_BBR1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			x := &xtcp_flat_record.XtcpFlatRecord{}
			if err := DeserializeCongInfoXTCP(tc.data, x); err != nil {
				t.Fatalf("err = %v", err)
			}
			if x.CongestionAlgorithmEnum != tc.wantAlg {
				t.Errorf("alg = %v, want %v", x.CongestionAlgorithmEnum, tc.wantAlg)
			}
		})
	}
}
