package goip

import (
	"bytes"
	"net/netip"
	"strings"
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

// TestCGetSigned is get_s32.
//
// go test ./internal/goip/ -run TestCGetSigned
func TestCGetSigned(t *testing.T) {
	type result struct {
		val int32
		ok  bool
	}
	tests := []struct {
		description string
		arg         string
		expected    result
	}{
		{description: "positive: -1", arg: "-1", expected: result{-1, true}},
		{description: "positive: base 0 hex", arg: "0x10", expected: result{16, true}},
		{description: "corner: leading whitespace and a sign", arg: " -7", expected: result{-7, true}},
		{description: "boundary: INT32_MIN", arg: "-2147483648", expected: result{-2147483648, true}},
		{description: "boundary: INT32_MAX", arg: "2147483647", expected: result{2147483647, true}},
		{description: "boundary: one past INT32_MAX", arg: "2147483648"},
		{description: "boundary: one below INT32_MIN", arg: "-2147483649"},
		{description: "negative: past LONG_MAX", arg: "9223372036854775808"},
		{description: "negative: garbage", arg: "1x"},
		{description: "negative: empty", arg: ""},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v, ok := getS32(tc.arg, 0)
			if got := (result{v, ok}); got != tc.expected {
				t.Errorf("get_s32(%q) = %+v, want %+v", tc.arg, got, tc.expected)
			}
		})
	}
}

// TestGetIndex is iplink_bond.c's get_index.
//
// go test ./internal/goip/ -run TestGetIndex
func TestGetIndex(t *testing.T) {
	tbl := []string{"slow", "fast"}
	tests := []struct {
		description string
		name        string
		expected    int
	}{
		{description: "positive: a name", name: "fast", expected: 1},
		{description: "positive: a numeric index", name: "1", expected: 1},
		{description: "boundary: index 0", name: "0", expected: 0},
		{description: "negative: an index past the end falls through to the names and fails", name: "2", expected: -1},
		{description: "negative: a negative index", name: "-1", expected: -1},
		{description: "corner: names are strcmp, not prefix", name: "fas", expected: -1},
		{description: "corner: get_integer is base 10, so 0x1 is a name, and not one", name: "0x1", expected: -1},
		{description: "corner: get_integer skips whitespace, so \" 1\" is index 1", name: " 1", expected: 1},
		{description: "negative: empty", name: "", expected: -1},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if got := getIndex(tbl, tc.name); got != tc.expected {
				t.Errorf("get_index(%q) = %d, want %d", tc.name, got, tc.expected)
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

// TestGetAddr is get_addr_1 for AF_INET (as get_addr32 reads it) and AF_INET6.
//
// go test ./internal/goip/ -run TestGetAddr
func TestGetAddr(t *testing.T) {
	tests := []struct {
		description string
		arg         string
		v6          bool
		expected    string // "" when refused
	}{
		{description: "positive: dotted quad", arg: "10.0.0.1", expected: "10.0.0.1"},
		{description: "corner: each part is strtoul base 0, so 010 is octal 8", arg: "10.010.0.1", expected: "10.8.0.1"},
		{description: "corner: a hex part", arg: "0x0a.0.0.1", expected: "10.0.0.1"},
		{description: "corner: missing trailing parts are zero", arg: "10.1", expected: "10.1.0.0"},
		{description: "corner: one part is the first octet", arg: "10", expected: "10.0.0.0"},
		{description: "corner: default is 0.0.0.0", arg: "default", expected: "0.0.0.0"},
		{description: "corner: any is 0.0.0.0", arg: "any", expected: "0.0.0.0"},
		{description: "boundary: 255 in every part", arg: "255.255.255.255", expected: "255.255.255.255"},
		{description: "boundary: 256 in a part", arg: "10.0.0.256"},
		{description: "negative: five parts", arg: "1.2.3.4.5"},
		{description: "negative: an empty part", arg: "10..0.1"},
		{description: "negative: a trailing dot", arg: "10.0.0.1."},
		{description: "negative: an IPv6 address for AF_INET", arg: "2001:db8::1"},
		{description: "negative: a name", arg: "gateway"},
		{description: "positive: IPv6", arg: "2001:db8::1", v6: true, expected: "2001:db8::1"},
		{description: "corner: v4-mapped IPv6 stays an IPv6 address", arg: "::ffff:10.0.0.1", v6: true, expected: "::ffff:10.0.0.1"},
		{description: "corner: default is :: for AF_INET6", arg: "default", v6: true, expected: "::"},
		{description: "negative: an IPv4 address for AF_INET6", arg: "10.0.0.1", v6: true},
		{description: "negative: inet_pton takes no zone", arg: "fe80::1%eth0", v6: true},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got netip.Addr
			var ok bool
			if tc.v6 {
				got, ok = getAddrIPv6(tc.arg)
			} else {
				got, ok = getAddrIPv4(tc.arg)
			}
			if tc.expected == "" {
				if ok {
					t.Errorf("got %v, want a refusal", got)
				}
				return
			}
			if !ok || got.String() != tc.expected {
				t.Errorf("got %v, %v; want %s", got, ok, tc.expected)
			}
		})
	}
}

// TestLLAddrA2N is ll_addr_a2n into bond_parse_opt's 32-byte buffer.
//
// go test ./internal/goip/ -run TestLLAddrA2N
func TestLLAddrA2N(t *testing.T) {
	tests := []struct {
		description string
		arg         string
		expected    []byte // nil when refused
	}{
		{description: "positive: a MAC", arg: "02:00:00:00:00:0a", expected: []byte{2, 0, 0, 0, 0, 0x0a}},
		{description: "corner: two parts is a two-byte address; the kernel refuses it, not ip", arg: "02:00", expected: []byte{2, 0}},
		{description: "corner: one part", arg: "ff", expected: []byte{0xff}},
		{description: "corner: a dot makes it an IPv4 address, four bytes", arg: "10.0.0.1", expected: []byte{10, 0, 0, 1}},
		{description: "corner: sscanf %x stops at a non-hex character without complaint", arg: "0aq:01", expected: []byte{0x0a, 1}},
		{description: "corner: sscanf %x takes a 0x prefix", arg: "0x0a:01", expected: []byte{0x0a, 1}},
		{description: "corner: a value that truncates into 0-255 as a C int is kept", arg: "100000002", expected: []byte{2}},
		{description: "boundary: 0x100 is past 255", arg: "100"},
		{description: "negative: a part with no hex digit", arg: "zz:01"},
		{description: "negative: an empty part", arg: "02::03"},
		{description: "negative: -1 is negative", arg: "-1"},
		{description: "boundary: thirty-two parts fill the buffer", arg: strings.Repeat("01:", 31) + "01", expected: bytes.Repeat([]byte{1}, 32)},
		{description: "boundary: thirty-three overrun it, which ip copies past its buffer; refused", arg: strings.Repeat("01:", 32) + "01"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got, ok := llAddrA2N(tc.arg, bondLLAddrBufCst)
			if tc.expected == nil {
				if ok {
					t.Errorf("got % x, want a refusal", got)
				}
				return
			}
			if !ok || !bytes.Equal(got, tc.expected) {
				t.Errorf("got % x, %v; want % x", got, ok, tc.expected)
			}
		})
	}
}
