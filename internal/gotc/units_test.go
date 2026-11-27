//go:build gotc_write

package gotc

import (
	"fmt"
	"math"
	"testing"
)

// The expected values below are iproute2 v7.2.0's own lib/utils_math.c,
// get_time, tc/tc_core.c and tc/tc_estimator.c, built with gcc -O2 into a
// harness and run on each input.

// TestTcUnits is the units parsers against tc's results (17P).
//
// go test -tags gotc_write ./internal/gotc/ -run TestTcUnits
func TestTcUnits(t *testing.T) {
	type res struct {
		v  uint64
		ok bool
	}
	tests := []struct {
		description string
		fn          string // rate rate64 size size64 time
		in          string
		expected    res
	}{
		// Positive: each suffix.
		{"positive: bit", "rate", "8bit", res{1, true}},
		{"positive: kbit", "rate", "1kbit", res{125, true}},
		{"positive: Kibit", "rate", "1Kibit", res{128, true}},
		{"positive: suffix case", "rate", "1KIBIT", res{128, true}},
		{"positive: mbit", "rate", "1mbit", res{125000, true}},
		{"positive: Mbps", "rate", "1Mbps", res{1000000, true}},
		{"positive: gbit", "rate", "1gbit", res{125000000, true}},
		{"positive: leading space", "rate", " 8bit", res{1, true}},
		{"positive: rate64 tbit", "rate64", "1tbit", res{125000000000, true}},
		{"positive: rate64 TBps", "rate64", "100TBps", res{100000000000000, true}},
		{"positive: size bare", "size", "1", res{1, true}},
		{"positive: size k", "size", "1k", res{1024, true}},
		{"positive: size kb", "size", "1kb", res{1024, true}},
		{"positive: size m", "size", "1m", res{1048576, true}},
		{"positive: size g", "size", "1gb", res{1073741824, true}},
		{"positive: size kbit", "size", "1kbit", res{128, true}},
		{"positive: size mbit", "size", "1mbit", res{131072, true}},
		{"positive: size gbit", "size", "1gbit", res{134217728, true}},
		{"positive: size b", "size", "1b", res{1, true}},
		{"positive: size64 4g", "size64", "4g", res{4294967296, true}},
		{"positive: time bare", "time", "1", res{1, true}},
		{"positive: time ms", "time", "1ms", res{1000, true}},
		{"positive: time s", "time", "1s", res{1000000, true}},
		{"positive: time secs", "time", "1secs", res{1000000, true}},
		{"positive: time msec", "time", "1msec", res{1000, true}},
		// Negative.
		{"negative: empty rate", "rate", "", res{0, false}},
		{"negative: bad suffix", "rate", "1foo", res{0, false}},
		{"negative: negative rate", "rate", "-1", res{0, false}},
		{"negative: rate inf", "rate", "inf", res{0, false}},
		{"negative: rate nan", "rate", "nan", res{0, false}},
		{"negative: rate overflow", "rate", "1e400bit", res{0, false}},
		{"negative: hex float eats the b", "rate", "0x10bit", res{0, false}},
		{"negative: size x", "size", "1x", res{0, false}},
		{"negative: size -1", "size", "-1", res{0, false}},
		{"negative: size inf", "size", "inf", res{0, false}},
		{"negative: size nan", "size", "nan", res{0, false}},
		{"negative: time -1", "time", "-1", res{0, false}},
		{"negative: time min", "time", "1min", res{0, false}},
		// Boundary.
		{"boundary: rate at 2^32-1", "rate", "34359738360bit", res{4294967295, true}},
		{"boundary: rate at 2^32", "rate", "34359738368bit", res{0, false}},
		{"boundary: rate tbit past 32 bits", "rate", "1tbit", res{0, false}},
		{"boundary: size at 2^32-1", "size", "4294967295", res{4294967295, true}},
		{"boundary: size 4g", "size", "4g", res{0, false}},
		{"boundary: size64 1e19", "size64", "1e19", res{10000000000000000000, true}},
		{"boundary: size64 2e19", "size64", "2e19", res{0, false}},
		{"boundary: time 2^32-1", "time", "4294.967295s", res{4294967295, true}},
		{"boundary: time 2^32 wraps", "time", "4294.967296s", res{0, true}},
		// Corner: rounding and the unchecked conversions (W12).
		{"corner: 1.5kbit truncates", "rate", "1.5kbit", res{187, true}},
		{"corner: size 1.5k", "size", "1.5k", res{1536, true}},
		{"corner: size 0.5 truncates", "size", "0.5", res{0, true}},
		{"corner: time 0.1ms", "time", "0.1ms", res{100, true}},
		{"corner: time 1.5us", "time", "1.5us", res{1, true}},
		{"corner: time 5000s wraps", "time", "5000s", res{705032704, true}},
		{"corner: time inf is 0", "time", "inf", res{0, true}},
		{"corner: time nan is 0", "time", "nan", res{0, true}},
		{"corner: rate64 inf is 0", "rate64", "inf", res{0, true}},
		{"corner: rate64 nan", "rate64", "nan", res{0x8000000000000000, true}},
		{"corner: rate64 past 2^64 is 0", "rate64", "1e30bit", res{0, true}},
		{"corner: size64 inf", "size64", "inf", res{0, false}},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			var got res
			switch tc.fn {
			case "rate":
				v, ok := getRate(tc.in)
				got = res{uint64(v), ok}
			case "rate64":
				got.v, got.ok = getRate64(tc.in)
			case "size":
				v, ok := getSize(tc.in)
				got = res{uint64(v), ok}
			case "size64":
				got.v, got.ok = getSize64(tc.in)
			case "time":
				v, ok := getTime(tc.in)
				got = res{uint64(v), ok}
			}
			if !got.ok {
				got.v = 0
			}
			if got != tc.expected {
				t.Errorf("%s(%q) = %+v, want %+v", tc.fn, tc.in, got, tc.expected)
			}
		})
	}
}

// TestCStrtod is glibc's strtod prefixes.
func TestCStrtod(t *testing.T) {
	tests := []struct {
		description string
		in          string
		expected    string // value|rest|erange
	}{
		{"positive: decimal", "1.5k", "1.5|k|false"},
		{"positive: exponent", "1e3x", "1000|x|false"},
		{"positive: hex", "0x10", "16|false"},
		{"positive: hex float", "0x1p4", "16||false"},
		{"positive: leading space and sign", " -2", "-2||false"},
		{"negative: no number", "abc", "0|abc|false"},
		{"negative: bare sign", "-", "0|-|false"},
		{"boundary: overflow", "1e400", "+Inf||true"},
		{"boundary: e with no digits", "1e", "1|e|false"},
		{"corner: inf", "inf", "+Inf||false"},
		{"corner: infinity", "Infinity", "+Inf||false"},
		{"corner: nan with chars", "nan(x1)y", "NaN|y|false"},
		{"corner: 0x with no digit", "0xg", "0|xg|false"},
		{"corner: hex eats b", "0x10bit", "267|it|false"},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			v, rest, er := cStrtod(tc.in)
			got := fmt.Sprintf("%v|%s|%t", v, rest, er)
			if tc.expected == "16|false" {
				tc.expected = "16||false"
			}
			if got != tc.expected {
				t.Errorf("cStrtod(%q) = %s, want %s", tc.in, got, tc.expected)
			}
		})
	}
}

// TestTcEstimatorTables is tc_setup_estimator, tc_calc_rtable and
// tc_calc_size_table against tc's results.
func TestTcEstimatorTables(t *testing.T) {
	est := []struct {
		description string
		a, tc       uint32
		expected    string // ok interval ewma
	}{
		{"positive: est 1s 8s", 1000000, 8000000, "true 0 3"},
		{"positive: est 250ms 1s", 250000, 1000000, "true -2 2"},
		{"positive: est 125ms 1s", 125000, 1000000, "true -2 3"},
		{"boundary: est 8s 64s", 8000000, 64000000, "true 3 3"},
		{"negative: interval past 8s", 9000000, 1, "false"},
		{"negative: ewma log 0", 1, 1, "false"},
	}
	for _, tc := range est {
		t.Run(tc.description, func(t *testing.T) {
			var e estimator
			ok := setupEstimator(tc.a, tc.tc, &e)
			got := fmt.Sprintf("%t %d %d", ok, e.interval, e.ewmaLog)
			if !ok {
				got = "false"
			}
			if got != tc.expected {
				t.Errorf("got %s, want %s", got, tc.expected)
			}
		})
	}

	core := tcCore{clockFactor: 1, tickInUsec: 1}
	rt := []struct {
		description string
		rate        uint32
		mpu         uint16
		cellLog     int
		mtu, ll     uint32
		expected    string // cl, rtab[0], rtab[37], rtab[255]
	}{
		{"positive: 1mbit", 125000, 0, -1, 0, 0, "3 64 2432 16384"},
		{"positive: mpu, mtu, atm", 125000, 64, -1, 1600, linklayerATM, "3 848 2968 18232"},
		{"boundary: rate 1", 1, 0, 3, 0, linklayerEthernet, "3 8000000 304000000 2048000000"},
		{"boundary: rate 0 caps at UINT_MAX", 0, 0, -1, 0, 0, "3 4294967295 4294967295 4294967295"},
		{"corner: mtu 9000", math.MaxUint32, 0, -1, 9000, 0, "6 1 1 4"},
	}
	for _, tc := range rt {
		t.Run(tc.description, func(t *testing.T) {
			r := rateSpec{rate: tc.rate, mpu: tc.mpu}
			var tab [256]uint32
			cl := core.calcRtable(&r, &tab, tc.cellLog, tc.mtu, tc.ll, uint64(r.rate))
			got := fmt.Sprintf("%d %d %d %d", cl, tab[0], tab[37], tab[255])
			if got != tc.expected || r.cellAlign != -1 || int(r.cellLog) != cl {
				t.Errorf("got %s (align %d log %d), want %s", got, r.cellAlign, r.cellLog, tc.expected)
			}
		})
	}

	st := []struct {
		description         string
		mtu, tsize, mpu, ll uint32
		expected            string // cl sl align mtu tsize d0 d1 dlast
	}{
		{"positive: overhead only, no table", 0, 0, 0, 0, "0 0 0 0 0"},
		{"positive: atm defaults", 0, 0, 0, linklayerATM, "2 0 -1 2047 512 53 53 2279"},
		{"positive: atm mtu tsize mpu", 2048, 128, 64, linklayerATM, "5 0 -1 2048 128 106 106 4558"},
		{"positive: ethernet with mpu", 0, 0, 64, linklayerEthernet, "2 0 -1 2047 512 64 64 2048"},
		{"boundary: size_log grows", 100000, 2, 0, linklayerATM, "16 2 -1 100000 2 18099 36185 36185"},
		{"boundary: a large table", 70000, 70000, 0, linklayerATM, "1 2 -1 70000 70000 13 13 38650"},
		{"boundary: tsize 1", 0, 1, 0, linklayerATM, "11 0 -1 2047 1 2279 2279"},
		{"boundary: tsize 1 mtu 2^31-1", math.MaxInt32, 1, 0, linklayerATM, "31 16 -1 2147483647 1 36181 36181"},
		{"corner: tsize 1 mtu 2^31 never ends (W13)", 1 << 31, 1, 0, linklayerATM, "hang"},
	}
	for _, tc := range st {
		t.Run(tc.description, func(t *testing.T) {
			s := sizeSpec{mtu: tc.mtu, tsize: tc.tsize, mpu: tc.mpu, linklayer: tc.ll}
			d, ok := calcSizeTable(&s)
			got := "hang"
			if ok {
				got = fmt.Sprintf("%d %d %d %d %d", s.cellLog, s.sizeLog, s.cellAlign, s.mtu, s.tsize)
				if d != nil {
					if len(d) > 1 {
						got += fmt.Sprintf(" %d %d %d", d[0], d[1], d[len(d)-1])
					} else {
						got += fmt.Sprintf(" %d %d", d[0], d[len(d)-1])
					}
				}
			}
			if got != tc.expected {
				t.Errorf("got %s, want %s", got, tc.expected)
			}
		})
	}
}
