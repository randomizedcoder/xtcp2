package xtcpnl

import (
	"io"
	"os"
	"reflect"
	"testing"
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
				FCS:          253,
				LinkType:     0,
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
				FCS:          253,
				LinkType:     0,
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
				FCS:          253,
				LinkType:     0,
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

func DeserializePcapHeaderBoth(b *testing.B, Func func(data []byte, ph *PcapHeader) (n int, err error)) {
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
		_, errD = Func(buf, ph)
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

func DeserializePcapRecordHeaderBoth(b *testing.B, Func func(data []byte, prh *PcapRecordHeader) (n int, err error)) {
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
		_, errD = Func(buf, prh)
		if errD != nil {
			b.Error("Test Failed DeserializePcapRecordHeaderBoth errD", errD)
		}
	}
	resultPcapRecordHeader = *prh
}
