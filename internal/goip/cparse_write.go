package goip

import (
	"math"
	"net/netip"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
)

// cStrtol is glibc's strtol on a 64-bit long: strtoul's reading, clamped to
// [LONG_MIN, LONG_MAX] as strtol clamps on ERANGE. end is cStrtoul's: 0 when
// no digits were read.
func cStrtol(s string, base int) (val int64, end int) {
	r := cStrtoul(s, base)
	if r.end == 0 {
		return 0, 0
	}
	if r.overflow {
		if r.neg {
			return math.MinInt64, r.end
		}
		return math.MaxInt64, r.end
	}
	if r.neg {
		v, ok := cint.NegInt64(-r.val)
		if !ok {
			return math.MinInt64, r.end
		}
		return v, r.end
	}
	v, ok := cint.Int64(r.val)
	if !ok {
		return math.MaxInt64, r.end
	}
	return v, r.end
}

// getS32 is get_s32: get_long's reading, then the int32 range.
func getS32(arg string, base int) (int32, bool) {
	v, ok := getLong(arg, base)
	if !ok || v < math.MinInt32 || v > math.MaxInt32 {
		return 0, false
	}
	return int32(v), true
}

// getIndex is iplink_bond.c's get_index: an integer that indexes the table
// is that index, so `mode 4` is `mode 802.3ad`; otherwise the name, by
// strcmp. An integer past the end falls through to the name test and fails
// it. -1 is no match.
func getIndex(tbl []string, name string) int {
	if i, ok := getInteger(name, 10); ok && i >= 0 && i < len(tbl) {
		return i
	}
	for i, t := range tbl {
		if t == name {
			return i
		}
	}
	return -1
}

// getAddrIPv4 is get_addr_1(…, AF_INET) as get_addr32 reads it: "default",
// "all" and "any" are 0.0.0.0; an argument with a ':' is an IPv6 address and
// refused for AF_INET; anything else is get_addr_ipv4.
func getAddrIPv4(name string) (netip.Addr, bool) {
	switch name {
	case addrNameDefaultCst, addrNameAllCst, addrNameAnyCst:
		return netip.IPv4Unspecified(), true
	}
	if strings.IndexByte(name, ':') >= 0 {
		return netip.Addr{}, false
	}
	return getAddrIPv4Parts(name)
}

// llAddrA2N is ll_addr_a2n into a buffer of maxLen bytes. An argument with a
// '.' is an IPv4 address, four bytes. Otherwise each ':'-separated part is
// read by sscanf("%x") into an int and must be 0-255.
//
// sscanf's %x is laxer than it looks, and the laxness is kept: it skips
// whitespace, takes a sign and an optional 0x, and stops at the first
// non-hex character without complaint, so "0a:zz" fails but "0aq:01" is
// 0a:01. The unsigned long it reads is truncated to the int, so a value
// that wraps into 0-255 is accepted.
//
// A part count above maxLen makes ll_addr_a2n return maxLen+1 and the caller
// copy past its buffer; that is refused here rather than reproduced.
func llAddrA2N(arg string, maxLen int) ([]byte, bool) {
	b, n, _ := llAddrA2NPart(arg, maxLen)
	return b, n > 0 && n <= maxLen
}

// llAddrA2NPart is ll_addr_a2n with what it returns and prints: the bytes,
// its return (the byte count; maxLen+1 when there are more parts than
// that, the rest unread; -1 for a refusal), and the word its `"%s" is
// invalid lladdr.` names, which is the failing part, or the whole argument
// for an IPv4 address. It is "" when nothing was printed: a success, or
// an IPv4 address that doesn't fit maxLen.
func llAddrA2NPart(arg string, maxLen int) ([]byte, int, string) {
	if strings.IndexByte(arg, '.') >= 0 {
		a, ok := getAddrIPv4(arg)
		if !ok {
			return nil, -1, arg
		}
		if maxLen < 4 {
			return nil, -1, ""
		}
		b := a.As4()
		return b[:], 4, ""
	}
	if LLAddrStrict {
		return llAddrA2NStrict(arg, maxLen)
	}
	out := make([]byte, 0, max(maxLen, 0))
	for rest := arg; len(out) < maxLen; {
		part, next, more := strings.Cut(rest, ":")
		v, ok := scanHexInt(part)
		if !ok || v < 0 || v > 255 {
			return nil, -1, part
		}
		out = append(out, byte(v))
		if !more {
			return out, len(out), ""
		}
		rest = next
	}
	return nil, maxLen + 1, ""
}

// llAddrA2NStrict is 7.3.0's hex loop: strtoul base 16 from a hex digit, a
// value of at most 255, then a ':' to go on or the end to stop. Anything else,
// or a part past maxLen, is `"ARG" is invalid lladdr.`.
func llAddrA2NStrict(arg string, maxLen int) ([]byte, int, string) {
	out := make([]byte, 0, max(maxLen, 0))
	for cp := arg; len(out) < maxLen; {
		if cp == "" || cDigitVal(cp[0]) >= 16 {
			break
		}
		r := cStrtoul(cp, 16)
		if r.val > 255 {
			break
		}
		out = append(out, byte(r.val))
		switch rest := cp[r.end:]; {
		case rest == "":
			return out, len(out), ""
		case rest[0] != ':':
			return nil, -1, arg
		default:
			cp = rest[1:]
		}
	}
	return nil, -1, arg
}

// llAddrCut is the word ll_addr_a2n leaves in argv: before 7.3.0 it writes a
// NUL over the first ':' before reading the first part, so a caller that
// PREV_ARGs re-reads the word cut there. An IPv4 address is left whole, and
// so is every argument under LLAddrStrict.
func llAddrCut(arg string) string {
	if LLAddrStrict || strings.IndexByte(arg, '.') >= 0 {
		return arg
	}
	w, _, _ := strings.Cut(arg, ":")
	return w
}

// scanHexInt is sscanf(s, "%x", &int) == 1: strtoul base 16 over a prefix,
// truncated to a C int.
func scanHexInt(s string) (int32, bool) {
	r := cStrtoul(s, 16)
	if r.end == 0 {
		return 0, false
	}
	return cint.Int(cint.Low32(r.val)), true
}

// cIntBits is a C int's 32 bits, as ip copies an int into a u32 attribute
// (addattr32). n comes from get_integer, so it is in an int32's range; a
// value past it is clamped, which no caller passes.
func cIntBits(n int) uint32 {
	return cint.Bits(int32(max(min(n, math.MaxInt32), math.MinInt32)))
}

// cStrcaseEq is strcasecmp(a, b) == 0 in the C locale `ip` runs in: only
// ASCII letters fold, byte by byte. strings.EqualFold folds Unicode too, so
// it would take "ſ" (U+017F) for "s" where strcasecmp does not.
func cStrcaseEq(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range len(a) {
		if asciiLower(a[i]) != asciiLower(b[i]) {
			return false
		}
	}
	return true
}

func asciiLower(c byte) byte {
	if 'A' <= c && c <= 'Z' {
		return c + 'a' - 'A'
	}
	return c
}
