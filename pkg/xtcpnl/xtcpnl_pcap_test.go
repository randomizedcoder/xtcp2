package xtcpnl

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"reflect"
	"testing"

	"golang.org/x/sys/unix"
)

type DeserializePcapHeaderTest struct {
	description string
	filename    string
	ph          PcapHeader
	Func        func(data []byte, ph *PcapHeader) (n int, err error)
}

// TestDeserializePcapHeader
// go test --run TestDeserializePcapHeader
// https://github.com/the-tcpdump-group/libpcap/blob/master/pcap/pcap.h#L146
// #define PCAP_VERSION_MAJOR 2
// #define PCAP_VERSION_MINOR 4
func TestDeserializePcapHeader(t *testing.T) {
	var tests = []DeserializePcapHeaderTest{
		{
			description: tnDeserializePcap,
			filename:    tdRespDumpDone_6_10_3,
			ph: PcapHeader{
				Magic:        2712847316, // a1b2c3d4 = seconds and microseconds
				VersionMajor: 2,
				VersionMinor: 4,
				Reserved1:    0,
				Reserved2:    0,
				SnapLen:      262144,
				LinkType:     PcapLinkTypeNetlinkCst,
			},
			Func: DeserializePcapHeader,
		},
		{
			description: tnDeserializePcap,
			filename:    tdRespDumpDone_6_10_3,
			ph: PcapHeader{
				Magic:        2712847316, // a1b2c3d4 = seconds and microseconds
				VersionMajor: 2,
				VersionMinor: 4,
				Reserved1:    0,
				Reserved2:    0,
				SnapLen:      262144,
				LinkType:     PcapLinkTypeNetlinkCst,
			},
			Func: DeserializePcapHeaderReflection,
		},
		{
			description: tnSport26546V4,
			filename:    tdResp26546_7_0_3,
			ph: PcapHeader{
				Magic:        2712847316,
				VersionMajor: 2,
				VersionMinor: 4,
				Reserved1:    0,
				Reserved2:    0,
				SnapLen:      262144,
				LinkType:     PcapLinkTypeNetlinkCst,
			},
			Func: DeserializePcapHeader,
		},
	}

	for i, test := range tests {

		t.Logf("#-------------------------------------")
		t.Logf("i:%d, description:%s, filename:%s", i, test.description, test.filename)

		f, err := os.Open(test.filename)
		if err != nil {
			t.Error("Test Failed Open error:", err)
		}
		defer f.Close()

		bs, err := io.ReadAll(f)
		if err != nil {
			t.Error("Test Failed ReadAll error:", err)
		}

		// t.Logf("i:%d, binary.Size(bs):%d", i, binary.Size(bs))
		// t.Logf("i:%d, file hex:%s", i, hex.EncodeToString(bs))

		buf := bs[:PcapHeaderSizeCst]

		// t.Logf("i:%d, binary.Size(buf):%d", i, binary.Size(buf))
		// t.Logf("i:%d,  buf hex:%s", i, hex.EncodeToString(buf))

		ph := new(PcapHeader)

		_, errD := test.Func(buf, ph)
		if errD != nil {
			t.Fatal("Test Failed DeserializeSockOpt errD", errD)
		}
		// t.Logf("i:%d, n:%d", i, n)

		// if ci.Cong != test.ci.Cong {
		if !reflect.DeepEqual(*ph, test.ph) {
			t.Errorf("Test %d %s !reflect.DeepEqual(*s:%x:%d, *test.s:%x:%d)", i, test.description, *ph, *ph, test.ph, test.ph)
		}

	}
}

type DeserializePcapRecordHeaderTest struct {
	description string
	filename    string
	prh         PcapRecordHeader
	Func        func(data []byte, prh *PcapRecordHeader) (n int, err error)
}

// TestDeserializePcapRecordHeader
// go test --run TestDeserializePcapRecordHeader
func TestDeserializePcapRecordHeader(t *testing.T) {
	var tests = []DeserializePcapRecordHeaderTest{
		{
			description: tnDeserializePcap,
			filename:    tdRespDumpDone_6_10_3,
			prh: PcapRecordHeader{
				TsSec:  1723171594,
				TsXsec: 213187,
				CapLen: 36,
				Len:    36,
			},
			Func: DeserializePcapRecordHeader,
		},
		{
			description: tnDeserializePcap,
			filename:    tdRespDumpDone_6_10_3,
			prh: PcapRecordHeader{
				TsSec:  1723171594,
				TsXsec: 213187,
				CapLen: 36,
				Len:    36,
			},
			Func: DeserializePcapRecordHeaderReflection,
		},
		{
			description: tnSport26546V4,
			filename:    tdResp26546_7_0_3,
			prh: PcapRecordHeader{
				TsSec:  1778603922,
				TsXsec: 514716,
				CapLen: 3724,
				Len:    3724,
			},
			Func: DeserializePcapRecordHeader,
		},
	}

	for i, test := range tests {

		t.Logf("#-------------------------------------")
		t.Logf("i:%d, description:%s, filename:%s", i, test.description, test.filename)

		f, err := os.Open(test.filename)
		if err != nil {
			t.Error("Test Failed Open error:", err)
		}
		defer f.Close()

		bs, err := io.ReadAll(f)
		if err != nil {
			t.Error("Test Failed ReadAll error:", err)
		}

		// t.Logf("i:%d, binary.Size(bs):%d", i, binary.Size(bs))
		// t.Logf("i:%d, file hex:%s", i, hex.EncodeToString(bs))

		buf := bs[PcapHeaderSizeCst : PcapHeaderSizeCst+PcapRecordHeaderSizeCst]

		// t.Logf("i:%d, binary.Size(buf):%d", i, binary.Size(buf))
		// t.Logf("i:%d,  buf hex:%s", i, hex.EncodeToString(buf))

		prh := new(PcapRecordHeader)

		_, errD := test.Func(buf, prh)
		if errD != nil {
			t.Fatal("Test Failed DeserializeSockOpt errD", errD)
		}
		// t.Logf("i:%d, n:%d", i, n)

		// if ci.Cong != test.ci.Cong {
		if !reflect.DeepEqual(*prh, test.prh) {
			t.Errorf("Test %d %s !reflect.DeepEqual(*s:%x:%d, *test.s:%x:%d)", i, test.description, *prh, *prh, test.prh, test.prh)
		}

	}
}

// ---- multi-record reader -----------------------------------------------------
//
// ParsePcap exists because an event capture is a sequence: the fixed
// PcapNetlinkOffsetCst slice only ever reaches the first record. These builders
// synthesize whole files so the boundary and corner rows can be exact; the
// agreement with the real fixtures is asserted separately in
// TestParseNetlinkPcapRealFixtures below.

const pcapTestSnapLenCst = 262144

// pcapFileHdr builds a 24-byte pcap global header.
func pcapFileHdr(magic, linkType uint32) []byte {
	return concat(
		le32(magic),
		[]byte{2, 0}, // version_major 2
		[]byte{4, 0}, // version_minor 4
		le32(0),      // thiszone
		le32(0),      // sigfigs
		le32(pcapTestSnapLenCst),
		le32(linkType),
	)
}

// pcapRecRaw builds a record header with caller-chosen lengths, so a row can
// declare a caplen that disagrees with the bytes that follow.
func pcapRecRaw(tsSec, tsXsec, capLen, origLen uint32, data []byte) []byte {
	return concat(le32(tsSec), le32(tsXsec), le32(capLen), le32(origLen), data)
}

// pcapRec builds a well-formed record whose caplen matches its payload. The
// explicit bound keeps the length conversion provably in range; a test payload
// that large would be a mistake in the row, not a case worth encoding.
func pcapRec(tsSec, tsXsec uint32, data []byte) []byte {
	n := len(data)
	if n < 0 || n > pcapTestSnapLenCst {
		panic("pcapRec: test payload outside the snaplen")
	}
	return pcapRecRaw(tsSec, tsXsec, uint32(n), uint32(n), data)
}

// sllNetlink prefixes payload with the 16-byte Linux SLL cooked header a
// DLT_NETLINK record carries. The three fields that matter are big-endian.
func sllNetlink(family uint16, payload []byte) []byte {
	h := make([]byte, NetlinkCookedHeaderSizeCst, NetlinkCookedHeaderSizeCst+len(payload))
	binary.BigEndian.PutUint16(h[0:2], 4)   // sll_pkttype: LINUX_SLL_OUTGOING
	binary.BigEndian.PutUint16(h[2:4], 824) // sll_hatype: ARPHRD_NETLINK
	binary.BigEndian.PutUint16(h[SllProtocolOffsetCst:SllProtocolOffsetCst+2], family)
	return append(h, payload...)
}

// wantHdr is the global header every well-formed row below shares.
func wantHdr(magic, linkType uint32) PcapHeader {
	return PcapHeader{
		Magic:        magic,
		VersionMajor: 2,
		VersionMinor: 4,
		SnapLen:      pcapTestSnapLenCst,
		LinkType:     linkType,
	}
}

// TestParsePcap covers the whole-file record walker: magic validation, record
// iteration, and every way a file can be truncated.
//
// go test ./pkg/xtcpnl/ -run TestParsePcap
func TestParsePcap(t *testing.T) {
	tests := []struct {
		description string
		data        []byte
		wantPH      PcapHeader
		wantRecords []PcapRecord
		wantErr     error
	}{
		{
			description: "positive: one DLT_NETLINK record",
			data: concat(
				pcapFileHdr(PcapMagicMicrosCst, PcapLinkTypeNetlinkCst),
				pcapRec(1700000000, 123456, sllNetlink(unix.NETLINK_ROUTE, []byte{0xaa, 0xbb})),
			),
			wantPH: wantHdr(PcapMagicMicrosCst, PcapLinkTypeNetlinkCst),
			wantRecords: []PcapRecord{
				{
					Header: PcapRecordHeader{TsSec: 1700000000, TsXsec: 123456, CapLen: 18, Len: 18},
					Data:   sllNetlink(unix.NETLINK_ROUTE, []byte{0xaa, 0xbb}),
				},
			},
		},
		{
			description: "positive: three records are returned in capture order",
			data: concat(
				pcapFileHdr(PcapMagicMicrosCst, PcapLinkTypeNetlinkCst),
				pcapRec(1, 100, sllNetlink(unix.NETLINK_ROUTE, []byte{0x01})),
				pcapRec(2, 200, sllNetlink(unix.NETLINK_ROUTE, []byte{0x02})),
				pcapRec(3, 300, sllNetlink(unix.NETLINK_ROUTE, []byte{0x03})),
			),
			wantPH: wantHdr(PcapMagicMicrosCst, PcapLinkTypeNetlinkCst),
			wantRecords: []PcapRecord{
				{
					Header: PcapRecordHeader{TsSec: 1, TsXsec: 100, CapLen: 17, Len: 17},
					Data:   sllNetlink(unix.NETLINK_ROUTE, []byte{0x01}),
				},
				{
					Header: PcapRecordHeader{TsSec: 2, TsXsec: 200, CapLen: 17, Len: 17},
					Data:   sllNetlink(unix.NETLINK_ROUTE, []byte{0x02}),
				},
				{
					Header: PcapRecordHeader{TsSec: 3, TsXsec: 300, CapLen: 17, Len: 17},
					Data:   sllNetlink(unix.NETLINK_ROUTE, []byte{0x03}),
				},
			},
		},
		{
			description: "positive: the nanosecond magic is accepted as well as the microsecond one",
			data: concat(
				pcapFileHdr(PcapMagicNanosCst, PcapLinkTypeNetlinkCst),
				pcapRec(1, 999999999, sllNetlink(unix.NETLINK_ROUTE, nil)),
			),
			wantPH: wantHdr(PcapMagicNanosCst, PcapLinkTypeNetlinkCst),
			wantRecords: []PcapRecord{
				{
					Header: PcapRecordHeader{TsSec: 1, TsXsec: 999999999, CapLen: 16, Len: 16},
					Data:   sllNetlink(unix.NETLINK_ROUTE, nil),
				},
			},
		},
		{
			description: "positive: a non-netlink link type is not ParsePcap's business",
			data: concat(
				pcapFileHdr(PcapMagicMicrosCst, 1), // DLT_EN10MB
				pcapRec(1, 0, []byte{0xde, 0xad}),
			),
			wantPH: wantHdr(PcapMagicMicrosCst, 1),
			wantRecords: []PcapRecord{
				{
					Header: PcapRecordHeader{TsSec: 1, CapLen: 2, Len: 2},
					Data:   []byte{0xde, 0xad},
				},
			},
		},
		{
			description: "boundary: a header-only file yields no records and no error",
			data:        pcapFileHdr(PcapMagicMicrosCst, PcapLinkTypeNetlinkCst),
			wantPH:      wantHdr(PcapMagicMicrosCst, PcapLinkTypeNetlinkCst),
			wantRecords: nil,
		},
		{
			description: "boundary: a zero-caplen record is a record, not the end of the file",
			data: concat(
				pcapFileHdr(PcapMagicMicrosCst, PcapLinkTypeNetlinkCst),
				pcapRec(7, 0, nil),
				pcapRec(8, 0, []byte{0x01}),
			),
			wantPH: wantHdr(PcapMagicMicrosCst, PcapLinkTypeNetlinkCst),
			wantRecords: []PcapRecord{
				{Header: PcapRecordHeader{TsSec: 7}, Data: []byte{}},
				{Header: PcapRecordHeader{TsSec: 8, CapLen: 1, Len: 1}, Data: []byte{0x01}},
			},
		},
		{
			description: "boundary: exactly PcapHeaderSizeCst+PcapRecordHeaderSizeCst bytes (empty final record)",
			data: concat(
				pcapFileHdr(PcapMagicMicrosCst, PcapLinkTypeNetlinkCst),
				pcapRecRaw(1, 2, 0, 0, nil),
			),
			wantPH:      wantHdr(PcapMagicMicrosCst, PcapLinkTypeNetlinkCst),
			wantRecords: []PcapRecord{{Header: PcapRecordHeader{TsSec: 1, TsXsec: 2}, Data: []byte{}}},
		},
		{
			description: "boundary: caplen below the original length (a snaplen-truncated packet) is honored",
			data: concat(
				pcapFileHdr(PcapMagicMicrosCst, PcapLinkTypeNetlinkCst),
				pcapRecRaw(1, 0, 2, 9000, []byte{0xaa, 0xbb}),
			),
			wantPH: wantHdr(PcapMagicMicrosCst, PcapLinkTypeNetlinkCst),
			wantRecords: []PcapRecord{
				{Header: PcapRecordHeader{TsSec: 1, CapLen: 2, Len: 9000}, Data: []byte{0xaa, 0xbb}},
			},
		},
		{
			description: "negative: a byte-swapped microsecond magic is rejected, not misparsed",
			data:        pcapFileHdr(PcapMagicMicrosSwappedCst, PcapLinkTypeNetlinkCst),
			wantErr:     ErrPcapByteSwapped,
		},
		{
			description: "negative: a byte-swapped nanosecond magic is rejected too",
			data:        pcapFileHdr(PcapMagicNanosSwappedCst, PcapLinkTypeNetlinkCst),
			wantErr:     ErrPcapByteSwapped,
		},
		{
			description: "negative: an arbitrary magic -> ErrPcapBadMagic",
			data:        pcapFileHdr(0xdeadbeef, PcapLinkTypeNetlinkCst),
			wantErr:     ErrPcapBadMagic,
		},
		{
			description: "negative: a zero magic -> ErrPcapBadMagic",
			data:        pcapFileHdr(0, PcapLinkTypeNetlinkCst),
			wantErr:     ErrPcapBadMagic,
		},
		{
			description: "corner: one byte short of the global header -> ErrPcapHeaderSmall",
			data:        pcapFileHdr(PcapMagicMicrosCst, PcapLinkTypeNetlinkCst)[:PcapHeaderSizeCst-1],
			wantErr:     ErrPcapHeaderSmall,
		},
		{
			description: "corner: empty input -> ErrPcapHeaderSmall",
			data:        nil,
			wantErr:     ErrPcapHeaderSmall,
		},
		{
			description: "corner: a trailing remainder too short for a record header -> ErrPcapTruncatedRecord",
			data: concat(
				pcapFileHdr(PcapMagicMicrosCst, PcapLinkTypeNetlinkCst),
				pcapRec(1, 0, []byte{0xaa}),
				make([]byte, PcapRecordHeaderSizeCst-1),
			),
			wantErr: ErrPcapTruncatedRecord,
		},
		{
			description: "corner: caplen one byte beyond the file -> ErrPcapTruncatedRecord",
			data: concat(
				pcapFileHdr(PcapMagicMicrosCst, PcapLinkTypeNetlinkCst),
				pcapRecRaw(1, 0, 3, 3, []byte{0xaa, 0xbb}),
			),
			wantErr: ErrPcapTruncatedRecord,
		},
		{
			description: "corner: a max-uint32 caplen cannot panic the slice -> ErrPcapTruncatedRecord",
			data: concat(
				pcapFileHdr(PcapMagicMicrosCst, PcapLinkTypeNetlinkCst),
				pcapRecRaw(1, 0, 0xffffffff, 0xffffffff, []byte{0xaa, 0xbb}),
			),
			wantErr: ErrPcapTruncatedRecord,
		},
		{
			description: "corner: a good record followed by a truncated one fails the whole file",
			data: concat(
				pcapFileHdr(PcapMagicMicrosCst, PcapLinkTypeNetlinkCst),
				pcapRec(1, 0, sllNetlink(unix.NETLINK_ROUTE, []byte{0x01})),
				pcapRecRaw(2, 0, 64, 64, []byte{0x02}),
			),
			wantErr: ErrPcapTruncatedRecord,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			ph, records, err := ParsePcap(tc.data)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				if records != nil {
					t.Errorf("records = %v on error, want nil", records)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(ph, tc.wantPH) {
				t.Errorf("PcapHeader = %+v, want %+v", ph, tc.wantPH)
			}
			if !reflect.DeepEqual(records, tc.wantRecords) {
				t.Errorf("records = %+v, want %+v", records, tc.wantRecords)
			}
		})
	}
}

// TestParseNetlinkPcap covers the DLT_NETLINK assertion ParsePcap deliberately
// omits. Nothing validated the link type before this existed — every caller
// sliced at a fixed offset and assumed.
//
// go test ./pkg/xtcpnl/ -run TestParseNetlinkPcap$
func TestParseNetlinkPcap(t *testing.T) {
	tests := []struct {
		description string
		data        []byte
		wantRecords int
		wantErr     error
	}{
		{
			description: "positive: a DLT_NETLINK capture is accepted",
			data: concat(
				pcapFileHdr(PcapMagicMicrosCst, PcapLinkTypeNetlinkCst),
				pcapRec(1, 0, sllNetlink(unix.NETLINK_ROUTE, []byte{0x01})),
			),
			wantRecords: 1,
		},
		{
			description: "boundary: a DLT_NETLINK capture with no records is still accepted",
			data:        pcapFileHdr(PcapMagicMicrosCst, PcapLinkTypeNetlinkCst),
			wantRecords: 0,
		},
		{
			description: "negative: DLT_EN10MB (1) -> ErrPcapNotNetlink",
			data:        pcapFileHdr(PcapMagicMicrosCst, 1),
			wantErr:     ErrPcapNotNetlink,
		},
		{
			description: "negative: DLT_NULL (0) -> ErrPcapNotNetlink",
			data:        pcapFileHdr(PcapMagicMicrosCst, 0),
			wantErr:     ErrPcapNotNetlink,
		},
		{
			description: "boundary: link type 252, one below DLT_NETLINK -> ErrPcapNotNetlink",
			data:        pcapFileHdr(PcapMagicMicrosCst, PcapLinkTypeNetlinkCst-1),
			wantErr:     ErrPcapNotNetlink,
		},
		{
			description: "corner: a bad magic is reported as such, not as a link-type problem",
			data:        pcapFileHdr(0xdeadbeef, PcapLinkTypeNetlinkCst),
			wantErr:     ErrPcapBadMagic,
		},
		{
			description: "corner: a truncated record is reported before the link type is judged",
			data: concat(
				pcapFileHdr(PcapMagicMicrosCst, PcapLinkTypeNetlinkCst),
				pcapRecRaw(1, 0, 99, 99, []byte{0x01}),
			),
			wantErr: ErrPcapTruncatedRecord,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			_, records, err := ParseNetlinkPcap(tc.data)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if len(records) != tc.wantRecords {
				t.Errorf("len(records) = %d, want %d", len(records), tc.wantRecords)
			}
		})
	}
}

// TestPcapRecordNetlinkPayload covers the SLL cooked-header strip, including the
// one big-endian field in a package that is otherwise little-endian throughout.
//
// go test ./pkg/xtcpnl/ -run TestPcapRecordNetlinkPayload
func TestPcapRecordNetlinkPayload(t *testing.T) {
	tests := []struct {
		description string
		data        []byte
		wantFamily  uint16
		wantBody    []byte
		wantErr     error
	}{
		{
			description: "positive: NETLINK_ROUTE (0) with a body",
			data:        sllNetlink(unix.NETLINK_ROUTE, []byte{0x10, 0x20, 0x30}),
			wantFamily:  unix.NETLINK_ROUTE,
			wantBody:    []byte{0x10, 0x20, 0x30},
		},
		{
			description: "positive: NETLINK_SOCK_DIAG (4) is read big-endian, not byte-swapped to 1024",
			data:        sllNetlink(unix.NETLINK_SOCK_DIAG, []byte{0xff}),
			wantFamily:  unix.NETLINK_SOCK_DIAG,
			wantBody:    []byte{0xff},
		},
		{
			description: "positive: NETLINK_NETFILTER (12), a family this package does not parse",
			data:        sllNetlink(unix.NETLINK_NETFILTER, nil),
			wantFamily:  unix.NETLINK_NETFILTER,
			wantBody:    []byte{},
		},
		{
			description: "boundary: exactly NetlinkCookedHeaderSizeCst bytes yields an empty body, not an error",
			data:        sllNetlink(unix.NETLINK_ROUTE, nil),
			wantFamily:  unix.NETLINK_ROUTE,
			wantBody:    []byte{},
		},
		{
			description: "boundary: the largest family value round-trips",
			data:        sllNetlink(0xffff, []byte{0x01}),
			wantFamily:  0xffff,
			wantBody:    []byte{0x01},
		},
		{
			description: "corner: one byte short of the cooked header -> ErrPcapCookedHeaderSmall",
			data:        sllNetlink(unix.NETLINK_ROUTE, nil)[:NetlinkCookedHeaderSizeCst-1],
			wantErr:     ErrPcapCookedHeaderSmall,
		},
		{
			description: "corner: an empty record -> ErrPcapCookedHeaderSmall",
			data:        nil,
			wantErr:     ErrPcapCookedHeaderSmall,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r := PcapRecord{Data: tc.data}
			family, body, err := r.NetlinkPayload()
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if family != tc.wantFamily {
				t.Errorf("family = %d, want %d", family, tc.wantFamily)
			}
			if !reflect.DeepEqual(body, tc.wantBody) {
				t.Errorf("body = %x, want %x", body, tc.wantBody)
			}
		})
	}
}

// TestParseNetlinkPcapRealFixtures asserts the new multi-record reader against
// every committed nlmon capture, and — the point of the test — that its first
// record agrees byte for byte with the fixed PcapNetlinkOffsetCst slice the rest
// of this package has always used. If the two ever disagree, one of them is
// wrong, and the fixed offset is the one with years of passing tests behind it.
//
// go test ./pkg/xtcpnl/ -run TestParseNetlinkPcapRealFixtures
func TestParseNetlinkPcapRealFixtures(t *testing.T) {
	// wantRecords is pinned per fixture. The single-record *_dump.pcap files are
	// what the generator sliced out and what the fixed-offset path could already
	// read; the bulk captures are the ones only the iterator can reach past
	// record 0, so their counts are the real regression guard here.
	tests := []struct {
		description string
		filename    string
		wantFamily  uint16
		wantRecords int
	}{
		{
			description: "positive: 6_10_3 sock_diag dump-done capture, single record",
			filename:    tdRespDumpDone_6_10_3,
			wantFamily:  unix.NETLINK_SOCK_DIAG,
			wantRecords: 1,
		},
		{
			description: "positive: 7_0_3 sock_diag v4 response, single record",
			filename:    tdResp26546_7_0_3,
			wantFamily:  unix.NETLINK_SOCK_DIAG,
			wantRecords: 1,
		},
		{
			description: "positive: 7_0_3 sock_diag v6 response, single record",
			filename:    tdResp19000V6_7_0_3,
			wantFamily:  unix.NETLINK_SOCK_DIAG,
			wantRecords: 1,
		},
		{
			description: "positive: 7_1_8 rtnetlink getlink dump, single record",
			filename:    tdRouteGetLinkDump_7_1_8,
			wantFamily:  unix.NETLINK_ROUTE,
			wantRecords: 1,
		},
		{
			description: "positive: 7_1_8 rtnetlink getaddr v4 dump, single record",
			filename:    tdRouteGetAddrV4Dump_7_1_8,
			wantFamily:  unix.NETLINK_ROUTE,
			wantRecords: 1,
		},
		{
			description: "positive: 7_1_8 rtnetlink getaddr v6 dump, single record",
			filename:    tdRouteGetAddrV6Dump_7_1_8,
			wantFamily:  unix.NETLINK_ROUTE,
			wantRecords: 1,
		},
		{
			description: "positive: 7_1_8 rtnetlink getroute dump, single record",
			filename:    tdRouteGetRouteDump_7_1_8,
			wantFamily:  unix.NETLINK_ROUTE,
			wantRecords: 1,
		},
		{
			description: "positive: 7_1_8 raw getlink bulk capture, 5 records",
			filename:    tdRouteBulkGetLink_7_1_8,
			wantFamily:  unix.NETLINK_ROUTE,
			wantRecords: 5,
		},
		{
			description: "positive: 7_1_8 raw getaddr bulk capture, 76 records",
			filename:    tdRouteBulkGetAddr_7_1_8,
			wantFamily:  unix.NETLINK_ROUTE,
			wantRecords: 76,
		},
		{
			description: "positive: 7_1_8 raw getroute bulk capture, 24 records",
			filename:    tdRouteBulkGetRoute_7_1_8,
			wantFamily:  unix.NETLINK_ROUTE,
			wantRecords: 24,
		},
	}

	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			bs, err := Readfile(tc.filename)
			if err != nil {
				t.Fatalf("Readfile(%s): %v", tc.filename, err)
			}

			ph, records, err := ParseNetlinkPcap(bs)
			if err != nil {
				t.Fatalf("ParseNetlinkPcap: %v", err)
			}
			if ph.LinkType != PcapLinkTypeNetlinkCst {
				t.Fatalf("LinkType = %d, want %d", ph.LinkType, PcapLinkTypeNetlinkCst)
			}
			if len(records) != tc.wantRecords {
				t.Fatalf("len(records) = %d, want %d", len(records), tc.wantRecords)
			}

			family, body, err := records[0].NetlinkPayload()
			if err != nil {
				t.Fatalf("NetlinkPayload: %v", err)
			}
			if family != tc.wantFamily {
				t.Errorf("family = %d, want %d", family, tc.wantFamily)
			}

			// The fixed-offset slice the rest of the package uses must be the
			// same bytes the iterator hands back for record 0.
			wantBody := bs[PcapNetlinkOffsetCst:]
			if len(body) > len(wantBody) {
				t.Fatalf("record 0 body is %d bytes, longer than the rest of the file (%d)", len(body), len(wantBody))
			}
			if !reflect.DeepEqual(body, wantBody[:len(body)]) {
				t.Error("record 0 body disagrees with the PcapNetlinkOffsetCst slice")
			}

			// Every record must carry a complete cooked header.
			for i, r := range records {
				if _, _, perr := r.NetlinkPayload(); perr != nil {
					t.Errorf("record %d: NetlinkPayload: %v", i, perr)
				}
			}
		})
	}
}

var (
	resultPcapHeader PcapHeader
)

// go test -bench=BenchmarkDeserializePcapHeader
func BenchmarkDeserializePcapHeader(b *testing.B) {
	DeserializePcapHeaderBoth(b, DeserializePcapHeader)
}

func BenchmarkDeserializePcapHeaderReflection(b *testing.B) {
	DeserializePcapHeaderBoth(b, DeserializePcapHeaderReflection)
}

func DeserializePcapHeaderBoth(b *testing.B, fn func(data []byte, ph *PcapHeader) (n int, err error)) {
	var tests = []DeserializePcapHeaderTest{
		{
			description: tnDeserializePcap,
			filename:    tdRespDumpDone_6_10_3,
		},
	}

	test := tests[0]

	bs, err := Readfile(test.filename)
	if err != nil {
		b.Error("Test Failed Readfile error:", err)
	}

	buf := bs

	ph := new(PcapHeader)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, errD = fn(buf, ph)
		if errD != nil {
			b.Error("Test Failed DeserializePcapHeaderBoth errD", errD)
		}
	}
	resultPcapHeader = *ph
}

var (
	resultPcapRecordHeader PcapRecordHeader
)

// go test -bench=BenchmarkDeserializePcapRecordHeader
func BenchmarkDeserializePcapRecordHeader(b *testing.B) {
	DeserializePcapRecordHeaderBoth(b, DeserializePcapRecordHeader)
}

func BenchmarkDeserializePcapRecordHeaderReflection(b *testing.B) {
	DeserializePcapRecordHeaderBoth(b, DeserializePcapRecordHeaderReflection)
}

func DeserializePcapRecordHeaderBoth(b *testing.B, fn func(data []byte, prh *PcapRecordHeader) (n int, err error)) {
	var tests = []DeserializePcapRecordHeaderTest{
		{
			description: tnDeserializePcap,
			filename:    tdRespDumpDone_6_10_3,
		},
	}

	test := tests[0]

	bs, err := Readfile(test.filename)
	if err != nil {
		b.Error("Test Failed Readfile error:", err)
	}

	buf := bs

	prh := new(PcapRecordHeader)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, errD = fn(buf, prh)
		if errD != nil {
			b.Error("Test Failed DeserializePcapRecordHeaderBoth errD", errD)
		}
	}
	resultPcapRecordHeader = *prh
}
