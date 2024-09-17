package xtcpnl

import (
	"testing"
)

type calculatePadTest struct {
	description string
	RTAttrLen   int
	F           func(size int) int
	Pad         int
}

// go test -run=TestCalculatePadding
func TestCalculatePadding(t *testing.T) {
	var tests = []calculatePadTest{
		{
			description: "pad slow",
			RTAttrLen:   0,
			F: func(size int) int {
				return CalculatePadding(size)
			},
			Pad: 0,
		},
		{
			description: "pad slow",
			RTAttrLen:   5,
			F: func(size int) int {
				return CalculatePadding(size)
			},
			Pad: 3,
		},
		{
			description: "pad slow",
			RTAttrLen:   6,
			F: func(size int) int {
				return CalculatePadding(size)
			},
			Pad: 2,
		},
		{
			description: "pad slow",
			RTAttrLen:   7,
			F: func(size int) int {
				return CalculatePadding(size)
			},
			Pad: 1,
		},
		{
			description: "pad slow",
			RTAttrLen:   8,
			F: func(size int) int {
				return CalculatePadding(size)
			},
			Pad: 0,
		},
		// Fast! Branchless
		{
			description: "pad fast/brachless",
			RTAttrLen:   0,
			F: func(size int) int {
				return FourByteAlignPadding(size)
			},
			Pad: 0,
		},
		{
			description: "pad fast/brachless",
			RTAttrLen:   5,
			F: func(size int) int {
				return FourByteAlignPadding(size)
			},
			Pad: 3,
		},
		{
			description: "pad fast/brachless",
			RTAttrLen:   6,
			F: func(size int) int {
				return FourByteAlignPadding(size)
			},
			Pad: 2,
		},
		{
			description: "pad fast/brachless",
			RTAttrLen:   7,
			F: func(size int) int {
				return FourByteAlignPadding(size)
			},
			Pad: 1,
		},
		{
			description: "pad fast/brachless",
			RTAttrLen:   8,
			F: func(size int) int {
				return FourByteAlignPadding(size)
			},
			Pad: 0,
		},
	}
	for i, test := range tests {

		t.Logf("#-------------------------------------")
		t.Logf("i:%d, description:%s, RTAttrLen:%d", i, test.description, test.RTAttrLen)

		pad := test.F(test.RTAttrLen)

		if pad != test.Pad {
			t.Errorf("Test %d %s Failed pad:%d != test.Pad:%d", i, test.description, pad, test.Pad)
		}
	}

}

var (
	resultsPad int
)

// go test -bench=BenchmarkPad
func BenchmarkPadCalculatePadding(b *testing.B) {
	f := func(size int) int {
		return CalculatePadding(size)
	}
	CalculatePaddingBoth(b, f)
}

func BenchmarkPadFourByteAlignPadding(b *testing.B) {
	f := func(size int) int {
		return FourByteAlignPadding(size)
	}
	CalculatePaddingBoth(b, f)
}

func CalculatePaddingBoth(b *testing.B, f func(size int) int) {
	var tests = []calculatePadTest{
		{
			description: "verify_request",
			RTAttrLen:   6,
			Pad:         2,
		},
	}
	test := tests[0]

	var pad int
	for i := 0; i < b.N; i++ {
		pad = f(test.RTAttrLen)
	}
	resultsPad = pad
}
