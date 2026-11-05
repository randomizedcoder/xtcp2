// Package cint holds the integer conversions the netlink code transcribes
// from C: a C int's bits as the u32 the wire carries and back, a long cast
// to an int, and the narrowings of a length or a parsed value into a header
// field.
//
// Each conversion either cannot overflow (a mask, or a guard on both
// bounds that gosec's G115 range check can see) or reports that it would,
// so callers never need to silence the linter.
package cint

import "math"

// Bits is (__u32)n: an int32's two's-complement bits, so -1 is 0xffffffff.
func Bits(n int32) uint32 {
	return uint32(int64(n) & math.MaxUint32)
}

// Int is (int)v: a u32's bits read as a C int, so 0xffffffff is -1. It is
// Bits' inverse.
func Int(v uint32) int32 {
	if v <= math.MaxInt32 {
		return int32(v)
	}
	return -int32(math.MaxUint32-v) - 1
}

// Low32 is (__u32)v: the low 32 bits of a wider unsigned value.
func Low32(v uint64) uint32 {
	return uint32(v & math.MaxUint32)
}

// LongBits is (__u32)l: a long's low 32 bits.
func LongBits(l int64) uint32 {
	return uint32(l & math.MaxUint32)
}

// LongToInt is (int)l: a long's low 32 bits read as a C int, as gcc
// converts it.
func LongToInt(l int64) int32 {
	return Int(LongBits(l))
}

// Int64 is v as an int64; false past MaxInt64.
func Int64(v uint64) (int64, bool) {
	if v > math.MaxInt64 {
		return 0, false
	}
	return int64(v), true
}

// NegInt64 is -mag as an int64; false past 1<<63, MinInt64's magnitude.
func NegInt64(mag uint64) (int64, bool) {
	if mag == 1<<63 {
		return math.MinInt64, true
	}
	n, ok := Int64(mag)
	return -n, ok
}

// U8 is n as a uint8; false outside [0, MaxUint8].
func U8(n int) (uint8, bool) {
	if n < 0 || n > math.MaxUint8 {
		return 0, false
	}
	return uint8(n), true
}

// U16 is n as a uint16; false outside [0, MaxUint16].
func U16(n int) (uint16, bool) {
	if n < 0 || n > math.MaxUint16 {
		return 0, false
	}
	return uint16(n), true
}

// U32 is n as a uint32; false outside [0, MaxUint32].
func U32(n int) (uint32, bool) {
	if n < 0 || n > math.MaxUint32 {
		return 0, false
	}
	return uint32(n), true
}

// U8From64 is v as a uint8; false past MaxUint8.
func U8From64(v uint64) (uint8, bool) {
	if v > math.MaxUint8 {
		return 0, false
	}
	return uint8(v), true
}

// U16From64 is v as a uint16; false past MaxUint16.
func U16From64(v uint64) (uint16, bool) {
	if v > math.MaxUint16 {
		return 0, false
	}
	return uint16(v), true
}

// U32From64 is v as a uint32; false past MaxUint32.
func U32From64(v uint64) (uint32, bool) {
	if v > math.MaxUint32 {
		return 0, false
	}
	return uint32(v), true
}
