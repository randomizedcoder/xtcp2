//go:build goip_write

package goip

import (
	"bytes"
	"math"
	"net/netip"
	"strings"
	"testing"
)

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

// TestCStrtol is strtol on a 64-bit long: the value and endptr, clamped to
// the long range on ERANGE.
//
// go test ./internal/goip/ -run TestCStrtol
func TestCStrtol(t *testing.T) {
	type result struct {
		val int64
		end int
	}
	tests := []struct {
		description string
		arg         string
		base        int
		expected    result
	}{
		{description: "positive: decimal", arg: "42", expected: result{42, 2}},
		{description: "positive: hex in base 0", arg: "0x10", expected: result{16, 4}},
		{description: "positive: octal in base 0", arg: "010", expected: result{8, 3}},
		{description: "positive: negative", arg: "-5", expected: result{-5, 2}},
		{description: "negative: no digits", arg: "x", expected: result{0, 0}},
		{description: "negative: empty", arg: "", expected: result{0, 0}},
		{description: "boundary: LONG_MAX", arg: "9223372036854775807", expected: result{math.MaxInt64, 19}},
		{description: "boundary: LONG_MAX+1 clamps", arg: "9223372036854775808", expected: result{math.MaxInt64, 19}},
		{description: "boundary: LONG_MIN", arg: "-9223372036854775808", expected: result{math.MinInt64, 20}},
		{description: "boundary: LONG_MIN-1 clamps", arg: "-9223372036854775809", expected: result{math.MinInt64, 20}},
		{description: "corner: past ULONG_MAX clamps", arg: "99999999999999999999", expected: result{math.MaxInt64, 20}},
		{description: "corner: past ULONG_MAX negative clamps", arg: "-99999999999999999999", expected: result{math.MinInt64, 21}},
		{description: "corner: trailing garbage stops end", arg: "5x", expected: result{5, 1}},
		{description: "corner: base 10 reads 0x10 as 0", arg: "0x10", base: 10, expected: result{0, 1}},
	}
	for i := range tests {
		tc := &tests[i]
		t.Run(tc.description, func(t *testing.T) {
			val, end := cStrtol(tc.arg, tc.base)
			if got := (result{val, end}); got != tc.expected {
				t.Errorf("strtol(%q, %d) = %+v, want %+v", tc.arg, tc.base, got, tc.expected)
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

// TestCIntBits is cIntBits: get_integer's int as its 32 bits, clamped.
func TestCIntBits(t *testing.T) {
	tests := []struct {
		description string
		got         uint64
		expected    uint64
	}{
		{"positive: cIntBits of -2", uint64(cIntBits(-2)), 0xfffffffe},
		{"corner: cIntBits clamps past INT32_MAX", uint64(cIntBits(math.MaxInt32 + 1)), 0x7fffffff},
		{"corner: cIntBits clamps past INT32_MIN", uint64(cIntBits(math.MinInt32 - 1)), 0x80000000},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			if tc.got != tc.expected {
				t.Errorf("%#x, want %#x", tc.got, tc.expected)
			}
		})
	}
}

// TestCStrcaseEq is strcasecmp(a, b) == 0 in the C locale.
//
// go test ./internal/goip/ -run TestCStrcaseEq
func TestCStrcaseEq(t *testing.T) {
	tests := []struct {
		description string
		a, b        string
		expected    bool
	}{
		{description: "positive: equal", a: "none", b: "none", expected: true},
		{description: "positive: upper against lower", a: "NONE", b: "none", expected: true},
		{description: "positive: mixed, with an underscore and digits", a: "Eui64_X", b: "eUI64_x", expected: true},
		{description: "negative: a different letter", a: "nope", b: "none", expected: false},
		{description: "negative: a prefix", a: "non", b: "none", expected: false},
		{description: "boundary: both empty", a: "", b: "", expected: true},
		{description: "boundary: A and Z fold", a: "AZ", b: "az", expected: true},
		{description: "boundary: the bytes either side of A and Z don't fold", a: "@[", b: "`{", expected: false},
		{description: "corner: U+017F is not s", a: "ſ", b: "s", expected: false},
		{description: "corner: the Kelvin sign is not k", a: "K", b: "k", expected: false},
		{description: "corner: non-ASCII bytes compare exactly", a: "é", b: "é", expected: true},
	}
	for i := range tests {
		tt := &tests[i]
		t.Run(tt.description, func(t *testing.T) {
			if got := cStrcaseEq(tt.a, tt.b); got != tt.expected {
				t.Errorf("cStrcaseEq(%q, %q) = %v, want %v", tt.a, tt.b, got, tt.expected)
			}
		})
	}
}

// TestLLProtoA2N is ll_proto_a2n: names fold only ASCII case, as
// strcasecmp does, and anything else is get_be16's base-0 number.
//
// go test ./internal/goip/ -run TestLLProtoA2N
func TestLLProtoA2N(t *testing.T) {
	type result struct {
		id uint16
		ok bool
	}
	tests := []struct {
		description string
		arg         string
		expected    result
	}{
		{description: "positive: a name", arg: "ipv6", expected: result{0x86dd, true}},
		{description: "positive: upper case folds", arg: "SNAP", expected: result{0x0005, true}},
		{description: "positive: a number", arg: "0x88cc", expected: result{0x88cc, true}},
		{description: "negative: an unknown name", arg: "nosuch"},
		{description: "boundary: 0xffff is the largest number", arg: "0xffff", expected: result{0xffff, true}},
		{description: "boundary: 0x10000 is past it", arg: "0x10000"},
		{description: "corner: U+017F does not fold to s", arg: "ſnap"},
		{description: "corner: the Kelvin sign does not fold to k", arg: "atalK"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			id, ok := llProtoA2N(tc.arg)
			if got := (result{id, ok}); got != tc.expected {
				t.Errorf("llProtoA2N(%q) = %+v, want %+v", tc.arg, got, tc.expected)
			}
		})
	}
}
