//go:build gotc_write

package gotc

import (
	"math"
	"strconv"
	"strings"

	"github.com/randomizedcoder/xtcp2/pkg/cint"
)

// The units parsers a write uses: lib/utils_math.c (get_rate, get_rate64,
// get_size64, get_size), lib/utils.c (get_time, get_time64, read_prop),
// tc/tc_util.c (parse_percent, the percent rates, get_size_and_cell,
// get_linklayer), tc/tc_core.c (time2tick, tc_calc_xmittime, the rate
// and size tables) and tc/tc_estimator.c, each as v7.2.0 has it.
//
// Every double goes to an integer as gcc's x86-64 code converts it
// (cDoubleToU32, cDoubleToU64), since tc checks few of them: get_time,
// get_rate64 and get_size's caller let inf and nan through (W12).

// cStrtod is glibc's strtod: leading white space, a sign, then "inf" or
// "infinity", "nan" with an optional "(chars)", a hex float, or a decimal
// one; rest is what it did not consume, and n == 0 is no number (rest is
// then s). erange is ERANGE on overflow, where the result is ±HUGE_VAL;
// an underflow's ERANGE is not modelled, since tc tests errno only beside
// HUGE_VAL.
func cStrtod(s string) (v float64, rest string, erange bool) {
	i := 0
	for i < len(s) && cIsSpace(s[i]) {
		i++
	}
	neg := false
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		neg = s[i] == '-'
		i++
	}
	body := s[i:]
	lower := strings.ToLower(body)
	sign := 1.0
	if neg {
		sign = -1
	}
	switch {
	case strings.HasPrefix(lower, "infinity"):
		return sign * math.Inf(1), body[len("infinity"):], false
	case strings.HasPrefix(lower, "inf"):
		return sign * math.Inf(1), body[len("inf"):], false
	case strings.HasPrefix(lower, "nan"):
		n := len("nan")
		if j := strings.IndexByte(body[n:], ')'); n < len(body) && body[n] == '(' && j > 0 &&
			isNanChars(body[n+1:n+j]) {
			n += j + 1
		}
		return math.NaN(), body[n:], false
	case strings.HasPrefix(lower, "0x"):
		if f, n, ok := hexFloat(body[2:]); ok {
			return sign * f, body[2+n:], false
		}
		// "0x" with no hex digit is the decimal 0, x left over.
		return sign * 0, body[1:], false
	}
	n := decimalPrefix(body)
	if n == 0 {
		return 0, s, false
	}
	f, err := strconv.ParseFloat(body[:n], 64)
	if err != nil && math.IsInf(f, 0) {
		erange = true
	}
	return sign * f, body[n:], erange
}

// isNanChars is the n-char-sequence nan's parentheses may hold.
func isNanChars(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z') {
			return false
		}
	}
	return true
}

// decimalPrefix is how much of s strtod reads as a decimal float: digits
// with an optional '.', at least one digit, and an exponent only if digits
// follow its 'e'.
func decimalPrefix(s string) int {
	i, digits := 0, 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
		digits++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && s[i] >= '0' && s[i] <= '9' {
			i++
			digits++
		}
	}
	if digits == 0 {
		return 0
	}
	return i + exponent(s[i:], 'e')
}

// exponent is the length of an exponent marked by mark (e or p) at the
// head of s, or 0 if no digit follows it.
func exponent(s string, mark byte) int {
	if s == "" || s[0]|0x20 != mark {
		return 0
	}
	j := 1
	if j < len(s) && (s[j] == '+' || s[j] == '-') {
		j++
	}
	k := j
	for k < len(s) && s[k] >= '0' && s[k] <= '9' {
		k++
	}
	if k == j {
		return 0
	}
	return k
}

// hexFloat reads the hex digits, optional '.', and optional p exponent
// after "0x"; n is the bytes read.
func hexFloat(s string) (v float64, n int, ok bool) {
	i, digits := 0, 0
	isHex := func(c byte) bool { return c >= '0' && c <= '9' || c|0x20 >= 'a' && c|0x20 <= 'f' }
	for i < len(s) && isHex(s[i]) {
		i++
		digits++
	}
	if i < len(s) && s[i] == '.' {
		i++
		for i < len(s) && isHex(s[i]) {
			i++
			digits++
		}
	}
	if digits == 0 {
		return 0, 0, false
	}
	mant := s[:i]
	e := exponent(s[i:], 'p')
	exp := "p0"
	if e > 0 {
		exp = s[i : i+e]
	}
	f, err := strconv.ParseFloat("0x"+mant+exp, 64)
	if err != nil && !math.IsInf(f, 0) {
		return 0, 0, false
	}
	return f, i + e, true
}

// rateSuffix is utils_math.c's suffixes, in its order (strcasecmp, so
// "Kibit" and "kibit" are one).
var rateSuffix = []struct {
	name  string
	scale float64
}{
	{"bit", 1}, {"Kibit", 1024}, {"kbit", 1000}, {"mibit", 1024 * 1024}, {"mbit", 1000000},
	{"gibit", 1024 * 1024 * 1024}, {"gbit", 1000000000}, {"tibit", 1024 * 1024 * 1024 * 1024},
	{"tbit", 1000000000000}, {"Bps", 8}, {"KiBps", 8 * 1024}, {"KBps", 8000},
	{"MiBps", 8 * 1024 * 1024}, {"MBps", 8000000}, {"GiBps", 8 * 1024 * 1024 * 1024}, {"GBps", 8000000000},
	{"TiBps", 8 * 1024 * 1024 * 1024 * 1024}, {"TBps", 8000000000000},
}

// rateBytes is get_rate's and get_rate64's shared head: the number, its
// suffix, and the division to bytes per second.
func rateBytes(s string) (float64, bool) {
	bps, p, erange := cStrtod(s)
	if p == s || bps < 0 || (math.IsInf(bps, 1) && erange) {
		return 0, false
	}
	for _, sf := range rateSuffix {
		if strings.EqualFold(sf.name, p) {
			bps *= sf.scale
			p = ""
			break
		}
	}
	if p != "" {
		return 0, false
	}
	return bps / 8, true
}

// getRate is get_rate: bytes per second in 32 bits, refused when the
// conversion does not equal floor(bps).
func getRate(s string) (uint32, bool) {
	bps, ok := rateBytes(s)
	if !ok {
		return 0, false
	}
	r := cDoubleToU32(bps)
	if float64(r) != math.Floor(bps) {
		return 0, false
	}
	return r, true
}

// getRate64 is get_rate64: no overflow check, so inf is 0 and nan is
// 0x8000000000000000 (W12).
func getRate64(s string) (uint64, bool) {
	bps, ok := rateBytes(s)
	if !ok {
		return 0, false
	}
	return cDoubleToU64(bps), true
}

// getSize64 is get_size64: the k, m, g and their bit forms are powers of
// 1024; the result must equal floor(sz).
func getSize64(s string) (uint64, bool) {
	sz, p, erange := cStrtod(s)
	if p == s || sz < 0 || (math.IsInf(sz, 1) && erange) {
		return 0, false
	}
	if p != "" {
		switch strings.ToLower(p) {
		case "kb", "k":
			sz *= 1024
		case "gb", "g":
			sz *= 1024 * 1024 * 1024
		case "gbit":
			sz *= 1024 * 1024 * 1024 / 8
		case "mb", "m":
			sz *= 1024 * 1024
		case "mbit":
			sz *= 1024 * 1024 / 8
		case "kbit":
			sz *= 1024 / 8
		case "b":
		default:
			return 0, false
		}
	}
	v := cDoubleToU64(sz)
	if float64(v) != math.Floor(sz) {
		return 0, false
	}
	return v, true
}

// getSize is get_size: get_size64 within 32 bits.
func getSize(s string) (uint32, bool) {
	v, ok := getSize64(s)
	if !ok || v > math.MaxUint32 {
		return 0, false
	}
	return cint.Low32(v), true
}

// getTime is get_time: microseconds in 32 bits, with no overflow check,
// so 5000s wraps and inf or nan is 0 (W12).
func getTime(s string) (uint32, bool) {
	t, p, erange := cStrtod(s)
	if p == s || t < 0 || (math.IsInf(t, 1) && erange) {
		return 0, false
	}
	if p != "" {
		switch strings.ToLower(p) {
		case "s", "sec", "secs":
			t *= timeUnitsPerSec
		case "ms", "msec", "msecs":
			t *= timeUnitsPerSec / 1000
		case "us", "usec", "usecs":
			t *= timeUnitsPerSec / 1000000
		default:
			return 0, false
		}
	}
	return cDoubleToU32(t), true
}

// getTime64 is get_time64: nanoseconds, as an __s64.
func getTime64(s string) (int64, bool) {
	ns, p, erange := cStrtod(s)
	if p == s || ns < 0 || (math.IsInf(ns, 1) && erange) {
		return 0, false
	}
	if p != "" {
		switch strings.ToLower(p) {
		case "s", "sec", "secs":
			ns *= nsecPerSec
		case "ms", "msec", "msecs":
			ns *= nsecPerMsec
		case "us", "usec", "usecs":
			ns *= nsecPerUsec
		case "ns", "nsec", "nsecs":
		default:
			return 0, false
		}
	}
	return cDoubleToI64(ns), true
}

// getSize64AndCell is get_size64_and_cell: SIZE[/CELL], CELL a power of
// two below 2^32 whose log is cellLog; cellLog is untouched without one.
func getSize64AndCell(s string, cellLog *int) (uint64, bool) {
	sz, cell, slash := strings.Cut(s, "/")
	v, ok := getSize64(sz)
	if !ok {
		return 0, false
	}
	if slash {
		c, ok := getInteger(cell, 0)
		if !ok {
			return 0, false
		}
		for i := range 32 {
			if cint.Int(uint32(1)<<i) == c { // (1<<i) == cell, 1<<31 being INT_MIN
				*cellLog = i
				return v, true
			}
		}
		return 0, false
	}
	return v, true
}

// getSizeAndCell is get_size_and_cell: the same within 32 bits.
func getSizeAndCell(s string, cellLog *int) (uint32, bool) {
	v, ok := getSize64AndCell(s, cellLog)
	if !ok || v > math.MaxUint32 {
		return 0, false
	}
	return cint.Low32(v), true
}

// Link layers: enum link_layer (tc_core.h).
const (
	linklayerUnspec   = 0
	linklayerEthernet = 1
	linklayerATM      = 2
)

// getLinklayer is get_linklayer, by matches(): adsl is atm.
func getLinklayer(arg string) (uint32, bool) {
	switch {
	case matches(arg, "ethernet"):
		return linklayerEthernet, true
	case matches(arg, "atm"), matches(arg, "adsl"):
		return linklayerATM, true
	}
	return 0, false
}

// parsePercent is parse_percent: 0 ok, -1 error, 1 out of range; the
// range is tested before the suffix.
func parsePercent(s string) (float64, int) {
	v, p, _ := cStrtod(s)
	v /= 100
	if v > 1.0 || v < 0.0 {
		return v, 1
	}
	if p != "" && p != "%" {
		return v, -1
	}
	return v, 0
}

// time2tick is tc_core_time2tick.
func (c tcCore) time2tick(t float64) float64 { return t * c.tickInUsec }

// time2ktime is tc_core_time2ktime: the product truncated to 32 bits.
func (c tcCore) time2ktime(t uint32) uint32 { return cDoubleToU32(float64(t) * c.clockFactor) }

// calcXmittime is tc_calc_xmittime: ceil of the ticks size bytes take at
// rate, capped at UINT_MAX.
func (c tcCore) calcXmittime(rate, size uint64) uint32 {
	v := math.Ceil(c.time2tick(timeUnitsPerSec * (float64(size) / float64(rate))))
	if v > math.MaxUint32 {
		return math.MaxUint32
	}
	return cDoubleToU32(v)
}

// ATM cells: linux/atm.h.
const (
	atmCellPayload = 48
	atmCellSize    = 53
)

// adjustSize is tc_adjust_size: at least mpu, then whole ATM cells.
func adjustSize(sz, mpu uint32, linklayer uint32) uint32 {
	if sz < mpu {
		sz = mpu
	}
	if linklayer == linklayerATM {
		cells := sz / atmCellPayload
		if sz%atmCellPayload > 0 {
			cells++
		}
		return cint.Bits(cint.Int(cells) * atmCellSize) // int linksize; the product wraps
	}
	return sz
}

// calcRtable is tc_calc_rtable_64 (tc_calc_rtable is it with the spec's
// own rate): fills rtab, and sets the spec's cell_align, cell_log and
// linklayer. A cellLog below 0 is the smallest that fits mtu (2047 when
// 0) in 256 cells.
func (c tcCore) calcRtable(r *rateSpec, rtab *[256]uint32, cellLog int, mtu uint32, linklayer uint32, rate uint64) int {
	if mtu == 0 {
		mtu = 2047
	}
	if cellLog < 0 {
		cellLog = 0
		for (mtu >> cellLog) > 255 {
			cellLog++
		}
	}
	for i := range 256 {
		sz := adjustSize(shl32(cint.Bits(int32(i+1)), cellLog), uint32(r.mpu), linklayer)
		rtab[i] = c.calcXmittime(rate, uint64(sz))
	}
	r.cellAlign = -1
	r.cellLog = uint8(cint.Bits(int32(cellLog)) & 0xff)
	r.linklayer = uint8(linklayer & tcLinklayerMask)
	return cellLog
}

// shl32 is a 32-bit C shift on x86: the count is taken mod 32.
func shl32(v uint32, n int) uint32 { return v << (uint(n) & 31) }

// shr32 is the same for a right shift.
func shr32(v uint32, n int) uint32 { return v >> (uint(n) & 31) }

// rateSpec is struct tc_ratespec as tc fills it.
type rateSpec struct {
	cellLog   uint8
	linklayer uint8
	overhead  uint16
	cellAlign int16
	mpu       uint16
	rate      uint32
}

// sizeSpec is struct tc_sizespec.
type sizeSpec struct {
	cellLog   uint8
	sizeLog   uint8
	cellAlign int16
	overhead  int32
	linklayer uint32
	mpu       uint32
	mtu       uint32
	tsize     uint32
}

// stabHangLimit is where tc_calc_size_table's cell_log loop would wrap:
// with tsize 1 it never ends (W13), which gotc refuses rather than hang.
const stabHangLimit = 32

// calcSizeTable is tc_calc_size_table; ok false is the loop that never
// ends (tsize 1: mtu >> cell_log is never 0 once the count wraps).
func calcSizeTable(s *sizeSpec) (stab []uint16, ok bool) {
	if s.linklayer <= linklayerEthernet && s.mpu == 0 {
		s.mtu, s.tsize, s.cellLog, s.cellAlign = 0, 0, 0, 0
		return nil, true
	}
	if s.mtu == 0 {
		s.mtu = 2047
	}
	if s.tsize == 0 {
		s.tsize = 512
	}
	cl := 0
	for shr32(s.mtu, cl) > s.tsize-1 {
		cl++
		if cl >= stabHangLimit {
			return nil, false
		}
	}
	s.cellLog = uint8(cint.Bits(int32(cl)) & 0xff)
	stab = make([]uint16, s.tsize)
again:
	for i := len(stab) - 1; i >= 0; i-- {
		sz := adjustSize(shl32(cint.Bits(int32(i+1)), int(s.cellLog)), s.mpu, s.linklayer)
		if shr32(sz, int(s.sizeLog)) > math.MaxUint16 {
			s.sizeLog++
			goto again
		}
		stab[i] = uint16(shr32(sz, int(s.sizeLog)) & math.MaxUint16)
	}
	s.cellAlign = -1
	return stab, true
}

// estimator is struct tc_estimator.
type estimator struct {
	interval int8
	ewmaLog  uint8
}

// setupEstimator is tc_setup_estimator: the interval as a power of two of
// a quarter second (-2 to 3), then the EWMA log for time_const.
func setupEstimator(a, timeConst uint32, est *estimator) bool {
	var iv int
	for iv = 0; iv <= 5; iv++ {
		if uint64(a) <= uint64(1)<<iv*(timeUnitsPerSec/4) {
			break
		}
	}
	if iv > 5 {
		return false
	}
	est.interval = int8(iv - 2) // -2..3
	var el int
	for el = 1; el < 32; el++ {
		w := 1.0 - 1.0/float64(cint.Int(uint32(1)<<el)) // 1<<31 is INT_MIN
		if float64(a)/(-math.Log(w)) > float64(timeConst) {
			break
		}
	}
	el--
	est.ewmaLog = uint8(el & 0xff)
	return el != 0 && el < 31
}

// getInteger is lib/utils.c get_integer: strtol in base, nothing left
// over, no ERANGE, within an int.
func getInteger(s string, base uint64) (int32, bool) {
	t := strings.TrimLeft(s, " \t\n\v\f\r")
	neg := false
	if t != "" && (t[0] == '+' || t[0] == '-') {
		neg, t = t[0] == '-', t[1:]
	}
	if t == "" || t[0] == '+' || t[0] == '-' || cIsSpace(t[0]) {
		return 0, false
	}
	mag, rest, ok, over := strtoulOver(t, base)
	switch {
	case !ok || rest != "" || over:
		return 0, false
	case neg && mag <= 1<<31:
		return cint.Int(cint.Low32(-mag)), true
	case !neg && mag <= math.MaxInt32:
		return cint.Int(cint.Low32(mag)), true
	}
	return 0, false
}

// cDoubleToI64 is C's (__s64) of a double on x86-64: cvttsd2si, whose
// out-of-range and NaN result is INT64_MIN.
func cDoubleToI64(v float64) int64 {
	if math.IsNaN(v) || v >= 1<<63 || v < -(1<<63) {
		return math.MinInt64
	}
	return int64(v)
}
