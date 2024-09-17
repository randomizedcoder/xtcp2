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
			description: "DeserializePcapTest",
			filename:    "./testdata/6_10_3/netlink_sock_diag_response_dump_done.pcap",
			ph: PcapHeader{
				Magic:        2712847316, //a1b2c3d4 = seconds and microseconds
				VersionMajor: 2,
				VersionMinor: 4,
				Reserved1:    0,
				Reserved2:    0,
				SnapLen:      262144,
				FCS:          253,
				LinkType:     0,
			},
			Func: func(data []byte, ph *PcapHeader) (n int, err error) {
				return DeserializePcapHeader(data, ph)
			},
		},
		{
			description: "DeserializePcapTest",
			filename:    "./testdata/6_10_3/netlink_sock_diag_response_dump_done.pcap",
			ph: PcapHeader{
				Magic:        2712847316, //a1b2c3d4 = seconds and microseconds
				VersionMajor: 2,
				VersionMinor: 4,
				Reserved1:    0,
				Reserved2:    0,
				SnapLen:      262144,
				FCS:          253,
				LinkType:     0,
			},
			Func: func(data []byte, ph *PcapHeader) (n int, err error) {
				return DeserializePcapHeaderReflection(data, ph)
			},
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
		//t.Logf("i:%d, n:%d", i, n)

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
			description: "DeserializePcapTest",
			filename:    "./testdata/6_10_3/netlink_sock_diag_response_dump_done.pcap",
			prh: PcapRecordHeader{
				TsSec:  1723171594,
				TsXsec: 213187,
				CapLen: 36,
				Len:    36,
			},
			Func: func(data []byte, prh *PcapRecordHeader) (n int, err error) {
				return DeserializePcapRecordHeader(data, prh)
			},
		},
		{
			description: "DeserializePcapTest",
			filename:    "./testdata/6_10_3/netlink_sock_diag_response_dump_done.pcap",
			prh: PcapRecordHeader{
				TsSec:  1723171594,
				TsXsec: 213187,
				CapLen: 36,
				Len:    36,
			},
			Func: func(data []byte, prh *PcapRecordHeader) (n int, err error) {
				return DeserializePcapRecordHeaderReflection(data, prh)
			},
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
		//t.Logf("i:%d, n:%d", i, n)

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
	f := func(data []byte, ph *PcapHeader) (n int, err error) {
		return DeserializePcapHeader(data, ph)
	}
	DeserializePcapHeaderBoth(b, f)
}

func BenchmarkDeserializePcapHeaderReflection(b *testing.B) {
	f := func(data []byte, ph *PcapHeader) (n int, err error) {
		return DeserializePcapHeaderReflection(data, ph)
	}
	DeserializePcapHeaderBoth(b, f)
}

func DeserializePcapHeaderBoth(b *testing.B, Func func(data []byte, ph *PcapHeader) (n int, err error)) {
	var tests = []DeserializePcapHeaderTest{
		{
			description: "DeserializePcapTest",
			filename:    "./testdata/6_10_3/netlink_sock_diag_response_dump_done.pcap",
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
	f := func(data []byte, prh *PcapRecordHeader) (n int, err error) {
		return DeserializePcapRecordHeader(data, prh)
	}
	DeserializePcapRecordHeaderBoth(b, f)
}

func BenchmarkDeserializePcapRecordHeaderReflection(b *testing.B) {
	f := func(data []byte, prh *PcapRecordHeader) (n int, err error) {
		return DeserializePcapRecordHeaderReflection(data, prh)
	}
	DeserializePcapRecordHeaderBoth(b, f)
}

func DeserializePcapRecordHeaderBoth(b *testing.B, Func func(data []byte, prh *PcapRecordHeader) (n int, err error)) {
	var tests = []DeserializePcapRecordHeaderTest{
		{
			description: "DeserializePcapTest",
			filename:    "./testdata/6_10_3/netlink_sock_diag_response_dump_done.pcap",
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
