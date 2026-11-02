package cint

import (
	"math"
	"testing"
)

// TestBitsInt is the C int and u32 round trip: Bits is (__u32)int and Int
// is (int)__u32.
//
// go test ./pkg/cint/ -run TestBitsInt
func TestBitsInt(t *testing.T) {
	tests := []struct {
		description string
		n           int32
		expected    uint32
	}{
		{"positive: a small int", 7, 7},
		{"positive: an ifindex", 4096, 4096},
		{"negative: -1 is all ones", -1, math.MaxUint32},
		{"negative: -2", -2, math.MaxUint32 - 1},
		{"boundary: zero", 0, 0},
		{"boundary: MaxInt32", math.MaxInt32, math.MaxInt32},
		{"boundary: MinInt32 is the sign bit", math.MinInt32, 1 << 31},
		{"corner: MinInt32+1", math.MinInt32 + 1, 1<<31 + 1},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := Bits(tt.n); got != tt.expected {
				t.Fatalf("Bits(%d) = %#x, expected %#x", tt.n, got, tt.expected)
			}
			if got := Int(tt.expected); got != tt.n {
				t.Fatalf("Int(%#x) = %d, expected %d", tt.expected, got, tt.n)
			}
		})
	}
}

// TestLow32 is (__u32) of a u64.
//
// go test ./pkg/cint/ -run TestLow32
func TestLow32(t *testing.T) {
	tests := []struct {
		description string
		v           uint64
		expected    uint32
	}{
		{"positive: fits", 1234, 1234},
		{"negative: high bits dropped", 1<<32 + 5, 5},
		{"boundary: MaxUint32", math.MaxUint32, math.MaxUint32},
		{"boundary: 1<<32 is zero", 1 << 32, 0},
		{"corner: MaxUint64", math.MaxUint64, math.MaxUint32},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := Low32(tt.v); got != tt.expected {
				t.Fatalf("Low32(%#x) = %#x, expected %#x", tt.v, got, tt.expected)
			}
		})
	}
}

// TestLongToInt is (int)long and (__u32)long: the low 32 bits, as a C int
// and as their bits.
//
// go test ./pkg/cint/ -run TestLongToInt
func TestLongToInt(t *testing.T) {
	tests := []struct {
		description string
		l           int64
		expected    int32
	}{
		{"positive: fits", 42, 42},
		{"negative: fits", -42, -42},
		{"boundary: MaxInt32", math.MaxInt32, math.MaxInt32},
		{"boundary: MaxInt32+1 wraps to MinInt32", math.MaxInt32 + 1, math.MinInt32},
		{"corner: MaxInt64 is -1", math.MaxInt64, -1},
		{"corner: MinInt64 is 0", math.MinInt64, 0},
		{"corner: 1<<32 is 0", 1 << 32, 0},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := LongToInt(tt.l); got != tt.expected {
				t.Fatalf("LongToInt(%d) = %d, expected %d", tt.l, got, tt.expected)
			}
			if got := LongBits(tt.l); got != Bits(tt.expected) {
				t.Fatalf("LongBits(%d) = %#x, expected %#x", tt.l, got, Bits(tt.expected))
			}
		})
	}
}

// TestInt64 is the checked u64-to-s64 conversions, positive and negated.
//
// go test ./pkg/cint/ -run TestInt64
func TestInt64(t *testing.T) {
	type result struct {
		n  int64
		ok bool
	}
	tests := []struct {
		description string
		neg         bool
		v           uint64
		expected    result
	}{
		{"positive: fits", false, 9, result{9, true}},
		{"positive: negated", true, 9, result{-9, true}},
		{"boundary: MaxInt64", false, math.MaxInt64, result{math.MaxInt64, true}},
		{"boundary: MaxInt64+1 refused", false, 1 << 63, result{0, false}},
		{"boundary: -(1<<63) is MinInt64", true, 1 << 63, result{math.MinInt64, true}},
		{"negative: -(1<<63 + 1) refused", true, 1<<63 + 1, result{0, false}},
		{"corner: -0", true, 0, result{0, true}},
		{"corner: MaxUint64 refused", false, math.MaxUint64, result{0, false}},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			f := Int64
			if tt.neg {
				f = NegInt64
			}
			n, ok := f(tt.v)
			if got := (result{n, ok}); got != tt.expected {
				t.Fatalf("got %+v, expected %+v", got, tt.expected)
			}
		})
	}
}

// TestNarrowInt is U8, U16 and U32 of an int: in range, or false.
//
// go test ./pkg/cint/ -run TestNarrowInt
func TestNarrowInt(t *testing.T) {
	type result struct {
		v  uint64
		ok bool
	}
	narrow := map[string]func(int) result{
		"U8":  func(n int) result { v, ok := U8(n); return result{uint64(v), ok} },
		"U16": func(n int) result { v, ok := U16(n); return result{uint64(v), ok} },
		"U32": func(n int) result { v, ok := U32(n); return result{uint64(v), ok} },
	}
	tests := []struct {
		description string
		fn          string
		n           int
		expected    result
	}{
		{"positive: U8 fits", "U8", 128, result{128, true}},
		{"positive: U16 fits", "U16", 1500, result{1500, true}},
		{"positive: U32 fits", "U32", 70000, result{70000, true}},
		{"negative: U8 of -1", "U8", -1, result{0, false}},
		{"negative: U16 of -1", "U16", -1, result{0, false}},
		{"negative: U32 of -1", "U32", -1, result{0, false}},
		{"boundary: U8 max", "U8", math.MaxUint8, result{math.MaxUint8, true}},
		{"boundary: U8 max+1", "U8", math.MaxUint8 + 1, result{0, false}},
		{"boundary: U16 max", "U16", math.MaxUint16, result{math.MaxUint16, true}},
		{"boundary: U16 max+1", "U16", math.MaxUint16 + 1, result{0, false}},
		{"boundary: U32 max", "U32", math.MaxUint32, result{math.MaxUint32, true}},
		{"boundary: U32 max+1", "U32", math.MaxUint32 + 1, result{0, false}},
		{"corner: zero", "U8", 0, result{0, true}},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := narrow[tt.fn](tt.n); got != tt.expected {
				t.Fatalf("%s(%d) = %+v, expected %+v", tt.fn, tt.n, got, tt.expected)
			}
		})
	}
}

// TestNarrowU64 is U8From64, U16From64 and U32From64: in range, or false.
//
// go test ./pkg/cint/ -run TestNarrowU64
func TestNarrowU64(t *testing.T) {
	type result struct {
		v  uint64
		ok bool
	}
	narrow := map[string]func(uint64) result{
		"U8":  func(n uint64) result { v, ok := U8From64(n); return result{uint64(v), ok} },
		"U16": func(n uint64) result { v, ok := U16From64(n); return result{uint64(v), ok} },
		"U32": func(n uint64) result { v, ok := U32From64(n); return result{uint64(v), ok} },
	}
	tests := []struct {
		description string
		fn          string
		v           uint64
		expected    result
	}{
		{"positive: U8 fits", "U8", 3, result{3, true}},
		{"positive: U16 fits", "U16", 300, result{300, true}},
		{"positive: U32 fits", "U32", 1 << 20, result{1 << 20, true}},
		{"negative: U8 of MaxUint64", "U8", math.MaxUint64, result{0, false}},
		{"negative: U32 of MaxUint64", "U32", math.MaxUint64, result{0, false}},
		{"boundary: U8 max+1", "U8", math.MaxUint8 + 1, result{0, false}},
		{"boundary: U16 max", "U16", math.MaxUint16, result{math.MaxUint16, true}},
		{"boundary: U16 max+1", "U16", math.MaxUint16 + 1, result{0, false}},
		{"boundary: U32 max", "U32", math.MaxUint32, result{math.MaxUint32, true}},
		{"boundary: U32 max+1", "U32", math.MaxUint32 + 1, result{0, false}},
		{"corner: zero", "U16", 0, result{0, true}},
	}
	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			if got := narrow[tt.fn](tt.v); got != tt.expected {
				t.Fatalf("%sFrom64(%d) = %+v, expected %+v", tt.fn, tt.v, got, tt.expected)
			}
		})
	}
}
