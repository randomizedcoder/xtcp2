package xtcpnl

import (
	"encoding/binary"
	"errors"
	"testing"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
)

// Tests for the Accurate ECN trailer DeserializeTCPInfo learned in
// TCPInfo7_0_3 — kernel bytes [248:280], see xtcpnl_inet_diag_tcpinfo.go.
//
// The trailer is optional: the kernel only emits it when it is new enough to
// have the fields, so a 4.19 capture is 224 bytes and a 7.0 capture is 280,
// and both must decode without error. That is the property most of the rows
// below exist to pin — a hard `len(data) < TCPInfo7_0_3_SizeCst` guard would
// pass the 7.0 rows and break every other fixture in the corpus.
//
// go test ./pkg/xtcpnl/... -run TestDeserializeTCPInfoAccECN -v

// accECNFields is the trailer on its own. Comparing just these keeps the
// expectations readable — the 60-odd fields before byte 248 are already
// covered by TestDeserializeTCPInfo.
type accECNFields struct {
	ReceivedCe       uint32
	DeliveredE1Bytes uint32
	DeliveredE0Bytes uint32
	DeliveredCeBytes uint32
	ReceivedE1Bytes  uint32
	ReceivedE0Bytes  uint32
	ReceivedCeBytes  uint32
	EcnMode          uint8
	AccecnOptSeen    uint8
	AccecnFailMode   uint8
	Options2         uint32
}

func accECNOf(t TCPInfo) accECNFields {
	return accECNFields{
		ReceivedCe:       t.ReceivedCe,
		DeliveredE1Bytes: t.DeliveredE1Bytes,
		DeliveredE0Bytes: t.DeliveredE0Bytes,
		DeliveredCeBytes: t.DeliveredCeBytes,
		ReceivedE1Bytes:  t.ReceivedE1Bytes,
		ReceivedE0Bytes:  t.ReceivedE0Bytes,
		ReceivedCeBytes:  t.ReceivedCeBytes,
		EcnMode:          t.EcnMode,
		AccecnOptSeen:    t.AccecnOptSeen,
		AccecnFailMode:   t.AccecnFailMode,
		Options2:         t.Options2,
	}
}

// accECNTest carries the bytes to parse in exactly one of two fields, so the
// provenance rule is enforced by the type rather than by review:
//
//   - filename: a real nlmon capture under testdata/. Every positive row uses
//     this. Synthesizing a positive fixture lets the decoder and its test be
//     wrong together and still pass, which is the whole failure mode a layout
//     test exists to catch.
//   - input: constructed bytes. Legitimate only for the truncation, boundary
//     and saturation rows, because a short or malformed datagram cannot be
//     captured off a live socket in the first place.
//
// wantBytesRecv is checked only when non-zero, and only exists for the two
// fixtures where the kernel's tcpi_received_e0_bytes happens to equal
// tcpi_bytes_received. That coincidence is what pins the trailer's byte offset
// independently of the struct definition: the two values are read 140 bytes
// apart from a header the test never consults, so agreement cannot come from a
// mis-set offset.
type accECNTest struct {
	description   string
	filename      string
	input         []byte
	stripNlaHdr   bool // fixture is a full nla attribute; skip the 4-byte header
	wantN         int
	wantErr       error
	want          accECNFields
	wantBytesRecv uint64
}

func TestDeserializeTCPInfoAccECN(t *testing.T) {
	// A full-length trailer with every bitfield saturated: 0xFF across
	// [276:280] means ecn_mode=3, accecn_opt_seen=3, accecn_fail_mode=0xF and
	// options2=0xFFFFFF. No real kernel emits this — it is here to prove the
	// masks and shifts do not bleed into each other, which a capture of a
	// normal connection (ecn_mode=1, everything else zero) cannot show.
	saturated := make([]byte, TCPInfo7_0_3_SizeCst)
	for i := 248; i < TCPInfo7_0_3_SizeCst; i++ {
		saturated[i] = 0xFF
	}

	// Distinct per-field values, so a transposed pair of offsets in the
	// trailer fails rather than comparing equal.
	distinct := make([]byte, TCPInfo7_0_3_SizeCst)
	for i, v := range []uint32{1, 2, 3, 4, 5, 6, 7} {
		binary.LittleEndian.PutUint32(distinct[248+i*4:252+i*4], v)
	}
	// ecn_mode=2 (AccECN), opt_seen=1, fail_mode=0b1010, options2=0x0000AB.
	binary.LittleEndian.PutUint32(distinct[276:280], 0x0000AB<<8|0b1010<<4|0x1<<2|0x2)

	var tests = []accECNTest{
		{
			// received_e0_bytes == bytes_received == 11648, with ecn_mode
			// RFC3168: classic ECN was negotiated, so the kernel counts every
			// received byte as E0, but the peer feeds back no AccECN
			// delivered-byte counters. Coherent, and the equality pins the
			// offsets.
			description:   "positive: 7.0.3 v6 capture, RFC3168 ECN, e0 bytes track bytes_received",
			filename:      tdAttrInfo19000V6_7_0_3,
			stripNlaHdr:   true,
			wantN:         TCPInfo7_0_3_SizeCst,
			want:          accECNFields{ReceivedE0Bytes: 11648, EcnMode: TCPIEcnModeRFC3168Cst},
			wantBytesRecv: 11648,
		},
		{
			description:   "positive: 7.0.3 rcvrtt capture, RFC3168 ECN, e0 bytes track bytes_received",
			filename:      tdAttrInfoRcvRtt_7_0_3,
			stripNlaHdr:   true,
			wantN:         TCPInfo7_0_3_SizeCst,
			want:          accECNFields{ReceivedE0Bytes: 98264, EcnMode: TCPIEcnModeRFC3168Cst},
			wantBytesRecv: 98264,
		},
		{
			// The same kernel and the same 280-byte length, but every byte
			// counter zero. Worth keeping distinct from the rows above: it is
			// the one real capture that would still pass if the decoder read
			// the counters from the wrong place and found zeros there, so it
			// is here for the length and the ecn_mode, not the counters.
			description: "positive: 7.0.3 v4 capture, RFC3168 ECN, byte counters all zero",
			filename:    tdAttrInfo26546_7_0_3,
			stripNlaHdr: true,
			wantN:       TCPInfo7_0_3_SizeCst,
			want:        accECNFields{EcnMode: TCPIEcnModeRFC3168Cst},
		},
		{
			description: "boundary: 6.10.3 capture predates AccECN, trailer absent, still decodes",
			filename:    tdAttrInfo_6_10_3,
			stripNlaHdr: true,
			wantN:       TCPInfo6_10_3_SizeCst,
			want:        accECNFields{},
		},
		{
			description: "boundary: 6.6.44 capture, two tails short of AccECN, still decodes",
			filename:    tdAttrInfo_6_6_44,
			stripNlaHdr: true,
			wantN:       TCPInfo6_6_44_SizeCst,
			want:        accECNFields{},
		},
		{
			description: "boundary: 4.19.319 capture, the oldest in the corpus, still decodes",
			filename:    tdAttrInfo_4_19_319,
			stripNlaHdr: true,
			wantN:       TCPInfo4_19_219_SizeCst,
			want:        accECNFields{},
		},
		{
			description: "corner: exactly TCPInfo7_0_3_SizeCst zero bytes decodes the whole trailer as zero",
			input:       make([]byte, TCPInfo7_0_3_SizeCst),
			wantN:       TCPInfo7_0_3_SizeCst,
			want:        accECNFields{},
		},
		{
			description: "corner: one byte short of the trailer falls back to the 6.10 size, no error",
			input:       make([]byte, TCPInfo7_0_3_SizeCst-1),
			wantN:       TCPInfo6_10_3_SizeCst,
			want:        accECNFields{},
		},
		{
			description: "corner: all bitfield bits set -> masks do not bleed between the four fields",
			input:       saturated,
			wantN:       TCPInfo7_0_3_SizeCst,
			want: accECNFields{
				ReceivedCe:       0xFFFFFFFF,
				DeliveredE1Bytes: 0xFFFFFFFF,
				DeliveredE0Bytes: 0xFFFFFFFF,
				DeliveredCeBytes: 0xFFFFFFFF,
				ReceivedE1Bytes:  0xFFFFFFFF,
				ReceivedE0Bytes:  0xFFFFFFFF,
				ReceivedCeBytes:  0xFFFFFFFF,
				EcnMode:          TCPIEcnModePendingCst,
				AccecnOptSeen:    TCPAccECNOptFailSeenCst,
				AccecnFailMode: TCPAccECNAceFailSendCst | TCPAccECNAceFailRecvCst |
					TCPAccECNOptFailSendCst | TCPAccECNOptFailRecvCst,
				Options2: 0x00FFFFFF,
			},
		},
		{
			description: "corner: distinct per-field values -> no two trailer offsets are transposed",
			input:       distinct,
			wantN:       TCPInfo7_0_3_SizeCst,
			want: accECNFields{
				ReceivedCe:       1,
				DeliveredE1Bytes: 2,
				DeliveredE0Bytes: 3,
				DeliveredCeBytes: 4,
				ReceivedE1Bytes:  5,
				ReceivedE0Bytes:  6,
				ReceivedCeBytes:  7,
				EcnMode:          TCPIEcnModeAccECNCst,
				AccecnOptSeen:    TCPAccECNOptEmptySeenCst,
				AccecnFailMode:   TCPAccECNAceFailRecvCst | TCPAccECNOptFailRecvCst,
				Options2:         0x0000AB,
			},
		},
		{
			description: "boundary: exactly TCPInfoMinSizeCst decodes the 4.15 base and nothing more",
			input:       make([]byte, TCPInfoMinSizeCst),
			wantN:       TCPInfoMinSizeCst,
			want:        accECNFields{},
		},
		{
			description: "negative: one byte below TCPInfoMinSizeCst -> ErrTCPInfoSmall",
			input:       make([]byte, TCPInfoMinSizeCst-1),
			wantErr:     ErrTCPInfoSmall,
		},
		{
			description: "negative: empty input -> ErrTCPInfoSmall",
			input:       []byte{},
			wantErr:     ErrTCPInfoSmall,
		},
		{
			description: "negative: nil input -> ErrTCPInfoSmall",
			input:       nil,
			wantErr:     ErrTCPInfoSmall,
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			if test.filename != "" && test.input != nil {
				t.Fatalf("%s: sets both filename and input; they are mutually exclusive",
					test.description)
			}

			data := test.input
			if test.filename != "" {
				bs, err := Readfile(test.filename)
				if err != nil {
					t.Fatalf("reading fixture %s: %v", test.filename, err)
				}
				data = bs
				if test.stripNlaHdr {
					data = bs[RTAttrSizeCst:]
				}
			}

			var info TCPInfo
			n, err := DeserializeTCPInfo(data, &info)

			if test.wantErr != nil {
				if !errors.Is(err, test.wantErr) {
					t.Fatalf("got error %v, want %v", err, test.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if n != test.wantN {
				t.Errorf("consumed %d bytes, want %d", n, test.wantN)
			}
			if got := accECNOf(info); got != test.want {
				t.Errorf("AccECN trailer mismatch\n got: %+v\nwant: %+v", got, test.want)
			}
			if test.wantBytesRecv != 0 {
				if info.BytesReceived != test.wantBytesRecv {
					t.Errorf("BytesReceived = %d, want %d", info.BytesReceived, test.wantBytesRecv)
				}
				if info.ReceivedE0Bytes != uint32(test.wantBytesRecv) {
					t.Errorf("ReceivedE0Bytes = %d, want it to equal BytesReceived %d — "+
						"the equality is what pins the trailer's byte offset",
						info.ReceivedE0Bytes, test.wantBytesRecv)
				}
			}
		})
	}
}

// TestDeserializeTCPInfoXTCPAccECN is the same assertion against the protobuf
// path, which decodes the wire bytes straight into XtcpFlatRecord rather than
// via TCPInfo. The two decoders are separate code (deserializeTCPInfoTail7_0
// vs deserializeTCPInfoXTCPTail7_0), so one can be extended and the other
// forgotten — this test is what makes that fail.
//
// go test ./pkg/xtcpnl/... -run TestDeserializeTCPInfoXTCPAccECN -v
func TestDeserializeTCPInfoXTCPAccECN(t *testing.T) {
	type xtcpAccECNTest struct {
		description string
		filename    string
		wantE0      uint32
		wantEcnMode uint32
	}

	var tests = []xtcpAccECNTest{
		{
			description: "positive: 7.0.3 v6 capture reaches the protobuf as e0=11648, RFC3168",
			filename:    tdAttrInfo19000V6_7_0_3,
			wantE0:      11648,
			wantEcnMode: TCPIEcnModeRFC3168Cst,
		},
		{
			description: "positive: 7.0.3 rcvrtt capture reaches the protobuf as e0=98264, RFC3168",
			filename:    tdAttrInfoRcvRtt_7_0_3,
			wantE0:      98264,
			wantEcnMode: TCPIEcnModeRFC3168Cst,
		},
		{
			description: "boundary: 6.10.3 capture predates AccECN, protobuf fields stay zero",
			filename:    tdAttrInfo_6_10_3,
			wantE0:      0,
			wantEcnMode: TCPIEcnModeDisabledCst,
		},
	}

	for _, test := range tests {
		t.Run(test.description, func(t *testing.T) {
			bs, err := Readfile(test.filename)
			if err != nil {
				t.Fatalf("reading fixture %s: %v", test.filename, err)
			}

			x := &xtcp_flat_record.XtcpFlatRecord{}
			if derr := DeserializeTCPInfoXTCP(bs[RTAttrSizeCst:], x); derr != nil {
				t.Fatalf("DeserializeTCPInfoXTCP: %v", derr)
			}

			if x.TcpInfoReceivedE0Bytes != test.wantE0 {
				t.Errorf("TcpInfoReceivedE0Bytes = %d, want %d", x.TcpInfoReceivedE0Bytes, test.wantE0)
			}
			if x.TcpInfoEcnMode != test.wantEcnMode {
				t.Errorf("TcpInfoEcnMode = %d, want %d", x.TcpInfoEcnMode, test.wantEcnMode)
			}
		})
	}
}
