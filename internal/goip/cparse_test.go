package goip

import (
	"testing"
)

// TestCGetUnsigned is get_u8/u16/u32 with base 0: strtoul's quirks, which
// strconv does not have.
//
// go test ./internal/goip/ -run TestCGetUnsigned
func TestCGetUnsigned(t *testing.T) {
	type result struct {
		val uint32
		ok  bool
	}
	tests := []struct {
		description string
		arg         string
		width       int // 8, 16 or 32
		expected    result
	}{
		{description: "positive: decimal", arg: "100", width: 32, expected: result{100, true}},
		{description: "positive: base 0 reads 0x as hex", arg: "0x1f", width: 32, expected: result{31, true}},
		{description: "corner: base 0 reads a leading 0 as octal", arg: "010", width: 32, expected: result{8, true}},
		{description: "corner: strtoul skips leading whitespace", arg: " 7", width: 8, expected: result{7, true}},
		{description: "corner: a plus sign is accepted", arg: "+5", width: 16, expected: result{5, true}},
		{description: "corner: -0 is 0, strtoul negates rather than refuses", arg: "-0", width: 32, expected: result{0, true}},
		{description: "negative: -1 wraps to ULONG_MAX and fails the range test", arg: "-1", width: 32},
		{description: "boundary: 255 is the u8 maximum", arg: "255", width: 8, expected: result{255, true}},
		{description: "boundary: 256 is past it", arg: "256", width: 8},
		{description: "boundary: 65535 is the u16 maximum", arg: "65535", width: 16, expected: result{65535, true}},
		{description: "boundary: 65536 is past it", arg: "65536", width: 16},
		{description: "boundary: 4294967295 is the u32 maximum", arg: "4294967295", width: 32, expected: result{4294967295, true}},
		{description: "boundary: 4294967296 is past it", arg: "4294967296", width: 32},
		{description: "negative: ULONG_MAX overflow is ERANGE", arg: "99999999999999999999999", width: 32},
		{description: "negative: trailing garbage", arg: "10ms", width: 32},
		{description: "negative: trailing whitespace is garbage too", arg: "10 ", width: 32},
		{description: "negative: empty", arg: "", width: 32},
		{description: "negative: no digits", arg: "fast", width: 32},
		{description: "corner: 0x with no hex digit reads 0 and leaves x, which is garbage", arg: "0x", width: 32},
		{description: "negative: 08 is not octal", arg: "08", width: 32},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got result
			switch tc.width {
			case 8:
				v, ok := getU8(tc.arg, 0)
				got = result{uint32(v), ok}
			case 16:
				v, ok := getU16(tc.arg, 0)
				got = result{uint32(v), ok}
			default:
				v, ok := getU32(tc.arg, 0)
				got = result{v, ok}
			}
			if got != tc.expected {
				t.Errorf("get_u%d(%q) = %+v, want %+v", tc.width, tc.arg, got, tc.expected)
			}
		})
	}
}

// TestGetU64 is get_u64 with base 16, as `ioam namespace add ... wide`
// reads it: strtoull, whose ULLONG_MAX is accepted unless it overflowed.
//
// go test ./internal/goip/ -run TestGetU64
func TestGetU64(t *testing.T) {
	type result struct {
		val uint64
		ok  bool
	}
	tests := []struct {
		description string
		arg         string
		expected    result
	}{
		{description: "positive: hex without a prefix", arg: "2a", expected: result{0x2a, true}},
		{description: "positive: base 16 takes a 0x prefix", arg: "0x2a", expected: result{0x2a, true}},
		{description: "boundary: sixteen f's, the maximum, is accepted", arg: "ffffffffffffffff", expected: result{1<<64 - 1, true}},
		{description: "boundary: 2^64 overflows, ERANGE", arg: "10000000000000000"},
		{description: "corner: -1 wraps to all ones and has no maximum to fail", arg: "-1", expected: result{1<<64 - 1, true}},
		{description: "negative: a digit past f", arg: "fg"},
		{description: "negative: empty", arg: ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v, ok := getU64(tc.arg, 16)
			if got := (result{v, ok}); got != tc.expected {
				t.Errorf("get_u64(%q, 16) = %+v, want %+v", tc.arg, got, tc.expected)
			}
		})
	}
}

// TestCAtoi is glibc's atoi, which reads `-loops`' count.
//
// go test ./internal/goip/ -run TestCAtoi
func TestCAtoi(t *testing.T) {
	tests := []struct {
		description string
		arg         string
		expected    int64
	}{
		{description: "positive: a count", arg: "3", expected: 3},
		{description: "positive: a negative count", arg: "-1", expected: -1},
		{description: "positive: a plus sign", arg: "+7", expected: 7},
		{description: "corner: leading space is skipped", arg: " \t5", expected: 5},
		{description: "corner: the digits are a prefix, so 3x is 3", arg: "3x", expected: 3},
		{description: "corner: no digits is 0", arg: "abc", expected: 0},
		{description: "corner: a sign alone is 0", arg: "-", expected: 0},
		{description: "negative: empty is 0", arg: "", expected: 0},
		{description: "corner: base 10 only, so 0x10 is 0", arg: "0x10", expected: 0},
		{description: "corner: 010 is ten, not octal", arg: "010", expected: 10},
		{description: "boundary: INT_MAX", arg: "2147483647", expected: 2147483647},
		{description: "boundary: INT_MAX+1 keeps the low 32 bits", arg: "2147483648", expected: -2147483648},
		{description: "boundary: 2^32 is 0", arg: "4294967296", expected: 0},
		{description: "corner: past LONG_MAX clamps to it, which is -1 as an int", arg: "99999999999999999999", expected: -1},
		{description: "corner: past LONG_MIN clamps to it, which is 0 as an int", arg: "-99999999999999999999", expected: 0},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := cAtoi(tc.arg); got != tc.expected {
				t.Errorf("atoi(%q) = %d, want %d", tc.arg, got, tc.expected)
			}
		})
	}
}

// TestCStrtoulBase: strtoul's bases. One goip doesn't use converts nothing
// rather than reading decimal.
//
// go test ./internal/goip/ -run TestCStrtoulBase
func TestCStrtoulBase(t *testing.T) {
	type result struct {
		val uint64
		end int
	}
	tests := []struct {
		description string
		arg         string
		base        int
		expected    result
	}{
		{description: "positive: base 10", arg: "17", base: 10, expected: result{17, 2}},
		{description: "positive: base 16 takes a 0x prefix", arg: "0x11", base: 16, expected: result{17, 4}},
		{description: "positive: base 8", arg: "21", base: 8, expected: result{17, 2}},
		{description: "positive: base 0 reads a leading 0 as octal", arg: "021", base: 0, expected: result{17, 3}},
		{description: "negative: base 2 converts nothing", arg: "101", base: 2},
		{description: "negative: base 36 converts nothing", arg: "z", base: 36},
		{description: "negative: a negative base converts nothing", arg: "17", base: -1},
		{description: "boundary: base 1 converts nothing", arg: "17", base: 1},
		{description: "corner: base 37 converts nothing", arg: "17", base: 37},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			r := cStrtoul(tc.arg, tc.base)
			if got := (result{r.val, r.end}); got != tc.expected {
				t.Errorf("cStrtoul(%q, %d) = %+v, want %+v", tc.arg, tc.base, got, tc.expected)
			}
		})
	}
}
