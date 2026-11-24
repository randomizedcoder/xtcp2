package xtcpnl

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/randomizedcoder/xtcp2/gen/go/xtcp_flat_record"
	"github.com/randomizedcoder/xtcp2/pkg/strpolicy"
)

type congNameCase struct {
	name        string
	category    string
	description string
	input       string
	expected    congNameWant
}

type congNameWant struct {
	cong string
	enum xtcp_flat_record.XtcpFlatRecord_CongestionAlgorithm
	err  error
}

const (
	congUnspec = xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_UNSPECIFIED
	congCubic  = xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_CUBIC
	congDctcp  = xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_DCTCP
	congVegas  = xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_VEGAS
	congBbr1   = xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_BBR1
	congBbr3   = xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_BBR3
)

// congNameCases are INET_DIAG_CONG payloads. The kernel writes ca_ops->name
// with nla_put_string, so the payload is the name plus one NUL; the
// netlink walker may hand over the 4-byte-aligned slice instead.
var congNameCases = []congNameCase{
	{"cubic", "positive", "the common name decodes", "cubic\x00", congNameWant{"cubic", congCubic, nil}},
	{"bbr", "positive", "a 4-byte name still decodes", "bbr\x00", congNameWant{"bbr", congBbr1, nil}},
	{"bbr3", "positive", "the version byte is kept", "bbr3\x00", congNameWant{"bbr3", congBbr3, nil}},
	{"dctcp", "positive", "dctcp has its enum", "dctcp\x00", congNameWant{"dctcp", congDctcp, nil}},
	{"vegas", "positive", "vegas has its enum", "vegas\x00", congNameWant{"vegas", congVegas, nil}},
	{"unknown_name", "negative", "a CA outside the enum keeps its name", "westwood\x00", congNameWant{"westwood", congUnspec, nil}},
	{"too_small", "negative", "below the minimum", "\x00", congNameWant{"", congUnspec, ErrCongInfoSmall}},
	{"empty", "negative", "no payload at all", "", congNameWant{"", congUnspec, ErrCongInfoSmall}},
	{"control_bytes", "negative", "a non-printable name goes through Clean", "cu\x01bic\x00", congNameWant{"cu�bic", congUnspec, nil}},
	{"two_char_nv", "boundary", "the shortest kernel name, unpadded", "nv\x00", congNameWant{"nv", congUnspec, nil}},
	{"two_char_lp", "boundary", "the other 2-char name, unpadded", "lp\x00", congNameWant{"lp", congUnspec, nil}},
	{"max_len", "boundary", "TCP_CA_NAME_MAX: 15 chars plus NUL", strings.Repeat("a", 15) + "\x00", congNameWant{strings.Repeat("a", 15), congUnspec, nil}},
	{"dctcp_reno", "corner", "the dctcp prefix must not over-match its reno fallback", "dctcp-reno\x00", congNameWant{"dctcp-reno", congUnspec, nil}},
	{"cubic_prefix", "corner", "a name that only starts like cubic", "cubical\x00", congNameWant{"cubical", congUnspec, nil}},
	{"padded", "corner", "NLA padding after the NUL is ignored", "nv\x00\x00", congNameWant{"nv", congUnspec, nil}},
	{"no_nul", "corner", "a missing terminator", "cubic", congNameWant{"cubic", congCubic, nil}},
}

func TestDeserializeCongInfoXTCPNames(t *testing.T) {
	for _, tc := range congNameCases {
		t.Run(tc.name, func(t *testing.T) {
			// A fresh record per row, primed with a stale value.
			x := &xtcp_flat_record.XtcpFlatRecord{InetDiagCong: "stale", InetDiagCongEnum: congBbr3}
			data := []byte(tc.input)
			err := DeserializeCongInfoXTCP(data, x)
			if !errors.Is(err, tc.expected.err) {
				t.Fatalf("%s [%s] %s: input %q: err = %v, expected %v", tc.name, tc.category, tc.description, tc.input, err, tc.expected.err)
			}
			if err != nil {
				return
			}
			for i := range data {
				data[i] = 'x' // The netlink receive buffer is reused after decoding.
			}
			if x.InetDiagCong != tc.expected.cong || x.InetDiagCongEnum != tc.expected.enum {
				t.Errorf("%s [%s] %s: input %q: got %q/%v, expected %q/%v",
					tc.name, tc.category, tc.description, tc.input, x.InetDiagCong, x.InetDiagCongEnum, tc.expected.cong, tc.expected.enum)
			}
		})
	}
}

// FuzzDeserializeCongInfoXTCP seeds from the table: no panic, the name
// stays valid UTF-8 within TCP_CA_NAME_MAX runes, and an enum is only set
// for the exact names it stands for.
func FuzzDeserializeCongInfoXTCP(f *testing.F) {
	for _, tc := range congNameCases {
		f.Add([]byte(tc.input))
	}
	enumNames := map[xtcp_flat_record.XtcpFlatRecord_CongestionAlgorithm][]string{
		congCubic: {"cubic"},
		congDctcp: {"dctcp"},
		congVegas: {"vegas"},
		congBbr1:  {"bbr", "bbr1"},
		xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_BBR2: {"bbr2"},
		congBbr3: {"bbr3"},
		xtcp_flat_record.XtcpFlatRecord_CONGESTION_ALGORITHM_PRAGUE: {"prague"},
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		x := new(xtcp_flat_record.XtcpFlatRecord)
		if err := DeserializeCongInfoXTCP(data, x); err != nil {
			return
		}
		if !utf8.ValidString(x.InetDiagCong) || utf8.RuneCountInString(x.InetDiagCong) > strpolicy.MaxCong {
			t.Fatalf("input %q: name %q not valid within %d runes", data, x.InetDiagCong, strpolicy.MaxCong)
		}
		if x.InetDiagCongEnum == congUnspec {
			return
		}
		for _, n := range enumNames[x.InetDiagCongEnum] {
			if x.InetDiagCong == n {
				return
			}
		}
		t.Fatalf("input %q: enum %v set for name %q", data, x.InetDiagCongEnum, x.InetDiagCong)
	})
}

// BenchmarkDeserializeCongInfoXTCPNames covers the decode paths: an enum
// name, a kernel name outside the enum, the shortest and the longest.
func BenchmarkDeserializeCongInfoXTCPNames(b *testing.B) {
	for _, bc := range []struct{ name, input string }{
		{"bench_cubic", "cubic\x00\x00\x00"},
		{"bench_unknown", "westwood\x00"},
		{"bench_short", "nv\x00"},
		{"bench_long", strings.Repeat("a", 15) + "\x00"},
	} {
		b.Run(bc.name, func(b *testing.B) {
			data := []byte(bc.input)
			x := new(xtcp_flat_record.XtcpFlatRecord)
			b.ReportAllocs()
			for b.Loop() {
				if err := DeserializeCongInfoXTCP(data, x); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
