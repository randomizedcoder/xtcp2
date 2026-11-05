package goip

import (
	"encoding/binary"
	"errors"
	"math"
	"net/netip"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
	"github.com/randomizedcoder/xtcp2/pkg/xtcpnl"
)

// This file transcribes the libc and lib/utils.c number and address readers
// the write path's argv parsing goes through: strtoul/strtol, get_u8/u16/u32,
// get_s32, get_integer, atoi, get_index (ip/iplink_bond.c), get_addr_1 for AF_INET
// and AF_INET6, and ll_addr_a2n (lib/ll_addr.c).
//
// strconv is not a substitute, and the differences are all inputs a user can
// type. strtoul skips leading whitespace, takes a sign, and negates an
// unsigned result instead of refusing it: `get_u32("-0")` is 0 and
// `get_u8(" 7")` is 7, where strconv.ParseUint rejects both. Base 0 reads
// "010" as octal 8. get_addr_ipv4 reads each part with strtoul base 0 too,
// so `10.010.0.1` is 10.8.0.1 and `10.1` is 10.1.0.0. A write request built
// from a different reading of the same argv is a different request, and the
// byte comparison would show it.

// cIsSpace is isspace(3) in the C locale.
func cIsSpace(c byte) bool {
	return c == ' ' || (c >= '\t' && c <= '\r')
}

func cDigitVal(c byte) uint64 {
	switch {
	case c >= '0' && c <= '9':
		return uint64(c - '0')
	case c >= 'a' && c <= 'z':
		return uint64(c-'a') + 10
	case c >= 'A' && c <= 'Z':
		return uint64(c-'A') + 10
	}
	return math.MaxUint64
}

// cStrtoulResult is what strtoul hands back: the value, how much of the input
// it consumed (end, the endptr offset; 0 when no digits were read, as strtoul
// sets endptr to nptr), and whether it overflowed (ERANGE, value ULONG_MAX).
type cStrtoulResult struct {
	val      uint64
	end      int
	overflow bool
	neg      bool
}

// cStrtoul is glibc's strtoul on a 64-bit long. base is 0, 8, 10 or 16;
// any other base converts nothing (end 0), as strtoul does for a base it
// refuses with EINVAL, so a caller's bad base fails rather than reads
// decimal.
func cStrtoul(s string, base int) cStrtoulResult {
	switch base {
	case 0, 8, 10, 16:
	default:
		return cStrtoulResult{}
	}
	i := 0
	for i < len(s) && cIsSpace(s[i]) {
		i++
	}
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	hasHexPrefix := i+1 < len(s) && s[i] == '0' && (s[i+1] == 'x' || s[i+1] == 'X') &&
		i+2 < len(s) && cDigitVal(s[i+2]) < 16
	switch {
	case (base == 0 || base == 16) && hasHexPrefix:
		base = 16
		i += 2
	case base == 0 && i < len(s) && s[i] == '0':
		base = 8
	case base == 0:
		base = 10
	}
	// base is 8, 10 or 16 here.
	ubase := uint64(10)
	switch base {
	case 8:
		ubase = 8
	case 16:
		ubase = 16
	}

	start := i
	var r cStrtoulResult
	for i < len(s) {
		d := cDigitVal(s[i])
		if d >= ubase {
			break
		}
		if !r.overflow {
			if r.val > (math.MaxUint64-d)/ubase {
				r.overflow = true
			} else {
				r.val = r.val*ubase + d
			}
		}
		i++
	}
	if i == start {
		return cStrtoulResult{}
	}
	r.end = i
	r.neg = neg
	if r.overflow {
		r.val = math.MaxUint64
	} else if neg {
		r.val = -r.val
	}
	return r
}

// cGetUnsigned is the shared body of get_u8, get_u16 and get_u32: the whole
// argument must be consumed, ULONG_MAX with ERANGE is refused, and so is a
// value above max. A negative argument is strtoul's wrapped value, so "-1"
// fails the max test and "-0" is 0.
func cGetUnsigned(arg string, base int, maxVal uint64) (uint64, bool) {
	if arg == "" {
		return 0, false
	}
	r := cStrtoul(arg, base)
	if r.end == 0 || r.end != len(arg) || r.overflow || r.val > maxVal {
		return 0, false
	}
	return r.val, true
}

func getU8(arg string, base int) (uint8, bool) {
	v, ok := cGetUnsigned(arg, base, math.MaxUint8)
	if !ok {
		return 0, false
	}
	return cint.U8From64(v)
}

func getU16(arg string, base int) (uint16, bool) {
	v, ok := cGetUnsigned(arg, base, math.MaxUint16)
	if !ok {
		return 0, false
	}
	return cint.U16From64(v)
}

func getU32(arg string, base int) (uint32, bool) {
	v, ok := cGetUnsigned(arg, base, math.MaxUint32)
	if !ok {
		return 0, false
	}
	return cint.U32From64(v)
}

// getU64 is get_u64: strtoull, the whole argument, ULLONG_MAX with ERANGE
// refused. With no maximum below it, "-1" is all ones, as strtoull wraps it.
func getU64(arg string, base int) (uint64, bool) {
	return cGetUnsigned(arg, base, math.MaxUint64)
}

// getLong is get_long: strtol, the whole argument, no ERANGE.
func getLong(arg string, base int) (int64, bool) {
	if arg == "" {
		return 0, false
	}
	r := cStrtoul(arg, base)
	if r.end == 0 || r.end != len(arg) {
		return 0, false
	}
	// strtol's range: the magnitude before the sign is applied.
	mag := r.val
	if r.neg && !r.overflow {
		mag = -r.val
	}
	switch {
	case r.overflow:
		return 0, false
	case r.neg:
		return cint.NegInt64(mag)
	}
	return cint.Int64(mag)
}

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

// cAtoi is glibc's atoi: (int)strtol(s, NULL, 10). It reads the longest
// prefix of space, a sign and decimal digits, so "3x" is 3 and "abc" is 0.
// strtol clamps to the long range, and the cast to int keeps the low 32 bits,
// so LONG_MAX reads as -1 and LONG_MIN as 0. The result is int64 so that the
// wrap is arithmetic rather than a narrowing conversion.
func cAtoi(s string) int64 {
	i := 0
	for i < len(s) && cIsSpace(s[i]) {
		i++
	}
	start := i
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	digits := i
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	if i == digits {
		return 0
	}
	// ParseInt clamps an out-of-range value to the int64 bounds, as strtol
	// clamps to LONG_MIN and LONG_MAX, and reports ErrRange, which atoi
	// ignores.
	v, err := strconv.ParseInt(s[start:i], 10, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return 0
	}
	v &= math.MaxUint32
	if v > math.MaxInt32 {
		v -= 1 << 32
	}
	return v
}

// getS32 is get_s32: get_long's reading, then the int32 range.
func getS32(arg string, base int) (int32, bool) {
	v, ok := getLong(arg, base)
	if !ok || v < math.MinInt32 || v > math.MaxInt32 {
		return 0, false
	}
	return int32(v), true
}

// getInteger is get_integer: get_long, then the int range.
func getInteger(arg string, base int) (int, bool) {
	v, ok := getLong(arg, base)
	if !ok || v < math.MinInt32 || v > math.MaxInt32 {
		return 0, false
	}
	return int(v), true
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

// The names get_addr_1 reads as the unspecified address.
const (
	addrNameDefaultCst = "default"
	addrNameAllCst     = "all"
	addrNameAnyCst     = "any"
)

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

// getAddrIPv4Parts is get_addr_ipv4 (lib/utils.c): up to four parts split on
// '.', each read by strtoul base 0 and at most 255. Missing trailing parts
// are zero, so "10" is 10.0.0.0.
func getAddrIPv4Parts(cp string) (netip.Addr, bool) {
	var b [4]byte
	for i := 0; i < 4; i++ {
		r := cStrtoul(cp, 0)
		if r.end == 0 || r.val > 255 {
			return netip.Addr{}, false
		}
		b[i] = byte(r.val)
		rest := cp[r.end:]
		if rest == "" {
			return netip.AddrFrom4(b), true
		}
		if i == 3 || rest[0] != '.' {
			return netip.Addr{}, false
		}
		cp = rest[1:]
	}
	return netip.Addr{}, false
}

// getAddrIPv6 is get_addr_1(…, AF_INET6): "default", "all" and "any" are ::;
// an argument with a ':' is inet_pton(AF_INET6), which takes no zone; one
// without is an IPv4 address and refused for AF_INET6.
func getAddrIPv6(name string) (netip.Addr, bool) {
	switch name {
	case addrNameDefaultCst, addrNameAllCst, addrNameAnyCst:
		return netip.IPv6Unspecified(), true
	}
	if strings.IndexByte(name, ':') < 0 {
		return netip.Addr{}, false
	}
	a, err := netip.ParseAddr(name)
	if err != nil || a.Zone() != "" || !a.Is6() {
		return netip.Addr{}, false
	}
	return a, true
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

// llAddrCut is the word ll_addr_a2n leaves in argv: it writes a NUL over
// the first ':' before reading the first part, so a caller that PREV_ARGs
// re-reads the word cut there. An IPv4 address is left whole.
func llAddrCut(arg string) string {
	if strings.IndexByte(arg, '.') >= 0 {
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

// mplsLabelMaxCst is 1 << 20: an MPLS label is 20 bits.
const mplsLabelMaxCst = 1 << 20

// mplsPton is mpls_pton1 (lib/mpls_pton.c): up to xtcpnl.MplsLabelStackMax
// labels split on '/', each strtoul in base 0 below 2^20, stored big-endian
// as label << 12 with the bottom-of-stack bit (0x100) on the last. tooMany
// is the stack running past the limit, where mpls_pton1 prints "too many
// labels" before failing.
func mplsPton(s string) (data []byte, tooMany, ok bool) {
	for range xtcpnl.MplsLabelStackMax {
		r := cStrtoul(s, 0)
		label, fits := cint.U32From64(r.val)
		if r.overflow || !fits || label >= mplsLabelMaxCst || r.end == 0 {
			return nil, false, false
		}
		entry := label << 12
		rest := s[r.end:]
		if rest == "" {
			return binary.BigEndian.AppendUint32(data, entry|1<<8), false, true
		}
		if rest[0] != '/' {
			return nil, false, false
		}
		data = binary.BigEndian.AppendUint32(data, entry)
		s = rest[1:]
	}
	return nil, true, false
}
