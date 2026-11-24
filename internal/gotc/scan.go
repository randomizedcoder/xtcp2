package gotc

import (
	"math"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
)

// glibc scanf's conversions, as tc_core_init and lib/names.c's
// read_id_name use them: each skips leading whitespace, then reads what
// strtoul or strtol would, and stores it truncated to the int it points
// at. ok is false when the conversion matches nothing.

// cIsSpace is isspace in the C locale.
func cIsSpace(c byte) bool { return c == ' ' || (c >= '\t' && c <= '\r') }

// skipSpace is a scanf whitespace directive: any run, none included.
func skipSpace(s string) string {
	i := 0
	for i < len(s) && cIsSpace(s[i]) {
		i++
	}
	return s[i:]
}

// hexVal is c's value as a hex digit, or -1.
func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return -1
}

// scanNum is the body of %x (base 16) and %d (base 10) after the
// whitespace: a sign, for %x a "0x" prefix, then digits, at most width
// characters in all (0: no limit). The magnitude saturates at
// math.MaxUint64, as strtoull's does. "0x" with no hex digit after it is
// the 0 alone, the x consumed, as glibc reads it.
func scanNum(s string, base, width int) (mag uint64, neg bool, rest string, ok bool) {
	limit := len(s)
	if width > 0 && width < limit {
		limit = width
	}
	i := 0
	if i < limit && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	if base == 16 && i+1 < limit && s[i] == '0' && (s[i+1] == 'x' || s[i+1] == 'X') {
		i += 2
		ok = true // the 0 is a digit whatever follows
	}
	for ; i < limit; i++ {
		d := hexVal(s[i])
		if d < 0 || d >= base {
			break
		}
		ok = true
		if mag > (math.MaxUint64-uint64(d))/uint64(base) {
			mag = math.MaxUint64
			continue
		}
		mag = mag*uint64(base) + uint64(d)
	}
	return mag, neg, s[i:], ok
}

// scanHex is %x (with width, 0 for none) into an unsigned int: strtoul's
// value, a '-' negating it, truncated to 32 bits.
func scanHex(s string, width int) (uint32, string, bool) {
	mag, neg, rest, ok := scanNum(skipSpace(s), 16, width)
	if neg {
		mag = -mag
	}
	return cint.Low32(mag), rest, ok
}

// scanDec is %d into an int: strtol's value, clamped to a long, truncated
// to 32 bits.
func scanDec(s string) (int32, string, bool) {
	mag, neg, rest, ok := scanNum(skipSpace(s), 10, 0)
	var v uint64
	switch {
	case neg && mag > 1<<63:
		v = 1 << 63 // LONG_MIN
	case neg:
		v = -mag
	case mag > math.MaxInt64:
		v = math.MaxInt64 // LONG_MAX
	default:
		v = mag
	}
	return cint.Int(cint.Low32(v)), rest, ok
}

// scanStr is %s: a run of non-space characters.
func scanStr(s string) (string, string, bool) {
	s = skipSpace(s)
	i := strings.IndexFunc(s, func(r rune) bool { return r < 0x80 && cIsSpace(byte(r)) })
	if i < 0 {
		i = len(s)
	}
	return s[:i], s[i:], i > 0
}
