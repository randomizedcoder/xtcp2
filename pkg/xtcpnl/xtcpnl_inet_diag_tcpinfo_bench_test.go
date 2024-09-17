package xtcpnl

import (
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
			description: "attribute_info",
			filename:    "./testdata/6_10_3/attribute_info",
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
			b.Error("Test Failed DeserializeTCPInfoReflection errD", errD)
		}

	}
	resultAny = *tcpinfo
}

func BenchmarkDeserializeTCPInfoReflection(b *testing.B) {
	var tests = []BenchmarkTCPInfoTest{
		{
			description: "attribute_info",
			filename:    "./testdata/6_10_3/attribute_info",
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
		_, errD = DeserializeTCPInfoReflection(bs, tcpinfo)
		if errD != nil {
			b.Error("Test Failed DeserializeTCPInfoReflection errD", errD)
		}

	}
	resultAny = *tcpinfo
}

func BenchmarkDeserializeTCPInfo6_10_3Reflection(b *testing.B) {
	var tests = []BenchmarkTCPInfoTest{
		{
			description: "attribute_info",
			filename:    "./testdata/6_10_3/attribute_info",
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
		_, errD = DeserializeTCPInfoTCPInfoTCPInfo6_10_3Reflection(bs, tcpinfo)
		if errD != nil {
			b.Error("Test Failed DeserializeTCPInfoReflection errD", errD)
		}

	}
	resultAny = *tcpinfo
}

func BenchmarkDeserializeTCPInfoTCPInfo6_6_44Reflection(b *testing.B) {
	var tests = []BenchmarkTCPInfoTest{
		{
			description: "attribute_info",
			filename:    "./testdata/6_6_44/attribute_info",
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
		_, errD = DeserializeTCPInfoTCPInfo6_6_44Reflection(bs, tcpinfo)
		if errD != nil {
			b.Error("Test Failed DeserializeTCPInfoReflection errD", errD)
		}

	}
	resultAny = *tcpinfo
}

func BenchmarkDeserializeTCPInfo5_4_281Reflection(b *testing.B) {
	var tests = []BenchmarkTCPInfoTest{
		{
			description: "attribute_info",
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
		_, errD = DeserializeTCPInfo5_4_281Reflection(bs, tcpinfo)
		if errD != nil {
			b.Error("Test Failed DeserializeTCPInfoReflection errD", errD)
		}

	}
	resultAny = *tcpinfo
}

func BenchmarkDeserializeTCPInfo4_19_219Reflection(b *testing.B) {
	var tests = []BenchmarkTCPInfoTest{
		{
			description: "attribute_info",
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
		_, errD = DeserializeTCPInfo4_19_219Reflection(bs, tcpinfo)
		if errD != nil {
			b.Error("Test Failed DeserializeTCPInfoReflection errD", errD)
		}

	}
	resultAny = *tcpinfo
}

// Haven't got test data for 4.15 unfortunately
//func BenchmarkDeserializeTCPInfoTCPInfo4_15Reflection(b *testing.B) {
