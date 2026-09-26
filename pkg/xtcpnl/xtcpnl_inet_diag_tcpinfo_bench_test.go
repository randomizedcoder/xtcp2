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

var (
	resultAny any
)

type BenchmarkTCPInfoTest struct {
	description string
	filename    string
}

func BenchmarkDeserializeTCPInfo(b *testing.B) {
	var tests = []BenchmarkTCPInfoTest{
		{
			description: tnAttrInfo,
			filename:    tdAttrInfo_6_10_3,
		},
	}

	test := tests[0]

	f, err := os.Open(test.filename)
	if err != nil {
		b.Error("Test Failed Open error:", err)
	}
	defer f.Close()

	bs, err := io.ReadAll(f)
	if err != nil {
		b.Error("Test Failed ReadAll error:", err)
	}

	tcpinfo := new(TCPInfo)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, errD = DeserializeTCPInfo(bs, tcpinfo)
		if errD != nil {
			b.Error("Test Failed deserializeTCPInfoReflection errD", errD)
		}

	}
	resultAny = *tcpinfo
}

// BenchmarkDeserializeTCPInfoReflection uses the 7.0.3 fixture, not the 6.10.3
// one its manual counterpart above uses, and pads it.
//
// TCPInfo aliases TCPInfo7_0_3, whose Go width is binary.Size = 285 bytes
// against a 280-byte wire struct, because the kernel's bitfields are modeled
// as separate Go fields. binary.Read wants all 285, so the 252-byte 6.10.3
// attribute this used to read fails with unexpected EOF. Padding the longest
// real capture is the closest a binary.Read twin can get to the manual
// decoder's input — and the gap is itself part of what the pair measures.
func BenchmarkDeserializeTCPInfoReflection(b *testing.B) {
	var tests = []BenchmarkTCPInfoTest{
		{
			description: tnAttrInfo,
			filename:    tdAttrInfo26546_7_0_3,
		},
	}

	test := tests[0]

	f, err := os.Open(test.filename)
	if err != nil {
		b.Error("Test Failed Open error:", err)
	}
	defer f.Close()

	bs, err := io.ReadAll(f)
	if err != nil {
		b.Error("Test Failed ReadAll error:", err)
	}

	data := padTo(bs[RTAttrSizeCst:], binary.Size(TCPInfo{}))

	tcpinfo := new(TCPInfo)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, errD = deserializeTCPInfoReflection(data, tcpinfo)
		if errD != nil {
			b.Error("Test Failed deserializeTCPInfoReflection errD", errD)
		}

	}
	resultAny = *tcpinfo
}

// BenchmarkDeserializeTCPInfo7_0_3 is the manual counterpart to the above: the
// same 7.0.3 capture through the real decoder, decoding the full 280-byte
// AccECN trailer. This is the pair to read side by side.
func BenchmarkDeserializeTCPInfo7_0_3(b *testing.B) {
	var tests = []BenchmarkTCPInfoTest{
		{
			description: tnAttrInfo,
			filename:    tdAttrInfo26546_7_0_3,
		},
	}

	test := tests[0]

	bs, err := Readfile(test.filename)
	if err != nil {
		b.Error("Test Failed Readfile error:", err)
	}

	data := bs[RTAttrSizeCst:]

	tcpinfo := new(TCPInfo)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, errD = DeserializeTCPInfo(data, tcpinfo)
		if errD != nil {
			b.Error("Test Failed DeserializeTCPInfo errD", errD)
		}
	}
	resultAny = *tcpinfo
}

func BenchmarkDeserializeTCPInfo7_0_3Reflection(b *testing.B) {
	var tests = []BenchmarkTCPInfoTest{
		{
			description: tnAttrInfo,
			filename:    tdAttrInfo26546_7_0_3,
		},
	}

	test := tests[0]

	bs, err := Readfile(test.filename)
	if err != nil {
		b.Error("Test Failed Readfile error:", err)
	}

	data := padTo(bs[RTAttrSizeCst:], binary.Size(TCPInfo7_0_3{}))

	tcpinfo := new(TCPInfo7_0_3)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, errD = deserializeTCPInfo7_0_3Reflection(data, tcpinfo)
		if errD != nil {
			b.Error("Test Failed deserializeTCPInfo7_0_3Reflection errD", errD)
		}
	}
	resultAny = *tcpinfo
}

func BenchmarkDeserializeTCPInfo6_10_3Reflection(b *testing.B) {
	var tests = []BenchmarkTCPInfoTest{
		{
			description: tnAttrInfo,
			filename:    tdAttrInfo_6_10_3,
		},
	}

	test := tests[0]

	f, err := os.Open(test.filename)
	if err != nil {
		b.Error("Test Failed Open error:", err)
	}
	defer f.Close()

	bs, err := io.ReadAll(f)
	if err != nil {
		b.Error("Test Failed ReadAll error:", err)
	}

	tcpinfo := new(TCPInfo6_10_3)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, errD = deserializeTCPInfo6_10_3Reflection(bs, tcpinfo)
		if errD != nil {
			b.Error("Test Failed deserializeTCPInfoReflection errD", errD)
		}

	}
	resultAny = *tcpinfo
}

func BenchmarkDeserializeTCPInfoTCPInfo6_6_44Reflection(b *testing.B) {
	var tests = []BenchmarkTCPInfoTest{
		{
			description: tnAttrInfo,
			filename:    tdAttrInfo_6_6_44,
		},
	}

	test := tests[0]

	f, err := os.Open(test.filename)
	if err != nil {
		b.Error("Test Failed Open error:", err)
	}
	defer f.Close()

	bs, err := io.ReadAll(f)
	if err != nil {
		b.Error("Test Failed ReadAll error:", err)
	}

	tcpinfo := new(TCPInfo6_6_44)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, errD = deserializeTCPInfo6_6_44Reflection(bs, tcpinfo)
		if errD != nil {
			b.Error("Test Failed deserializeTCPInfoReflection errD", errD)
		}

	}
	resultAny = *tcpinfo
}

func BenchmarkDeserializeTCPInfo5_4_281Reflection(b *testing.B) {
	var tests = []BenchmarkTCPInfoTest{
		{
			description: tnAttrInfo,
			filename:    "./testdata/5_4_281/attribute_info",
		},
	}

	test := tests[0]

	f, err := os.Open(test.filename)
	if err != nil {
		b.Error("Test Failed Open error:", err)
	}
	defer f.Close()

	bs, err := io.ReadAll(f)
	if err != nil {
		b.Error("Test Failed ReadAll error:", err)
	}

	tcpinfo := new(TCPInfo5_4_281)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, errD = deserializeTCPInfo5_4_281Reflection(bs, tcpinfo)
		if errD != nil {
			b.Error("Test Failed deserializeTCPInfoReflection errD", errD)
		}

	}
	resultAny = *tcpinfo
}

func BenchmarkDeserializeTCPInfo4_19_219Reflection(b *testing.B) {
	var tests = []BenchmarkTCPInfoTest{
		{
			description: tnAttrInfo,
			filename:    "./testdata/4_19_319/attribute_info",
		},
	}

	test := tests[0]

	f, err := os.Open(test.filename)
	if err != nil {
		b.Error("Test Failed Open error:", err)
	}
	defer f.Close()

	bs, err := io.ReadAll(f)
	if err != nil {
		b.Error("Test Failed ReadAll error:", err)
	}

	tcpinfo := new(TCPInfo4_19_219)

	var errD error
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, errD = deserializeTCPInfo4_19_219Reflection(bs, tcpinfo)
		if errD != nil {
			b.Error("Test Failed deserializeTCPInfoReflection errD", errD)
		}

	}
	resultAny = *tcpinfo
}

// Haven't got test data for 4.15 unfortunately
// func BenchmarkDeserializeTCPInfoTCPInfo4_15Reflection(b *testing.B) {
