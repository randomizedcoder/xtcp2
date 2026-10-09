package linkmonitor

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strings"
	"testing"

	"github.com/randomizedcoder/xtcp2/pkg/linkmonitor/internal/model"
)

func TestHostPairs(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		data, filter                                 string
		count                                        int
		invalid                                      bool
	}{
		{"protocols", "positive", "all host protocol groups and future field", "every field selected", "Ip: Forwarding\nIp: 1\nIcmp: InMsgs\nIcmp: 2\nIcmpMsg: InType3 OutType8\nIcmpMsg: 3 4\nTcp: MaxConn\nTcp: -1\nUdp: NoPorts\nUdp: 5\nTcpExt: Future\nTcpExt: 6\nIpExt: InOctets\nIpExt: 7\nMPTcpExt: Join\nMPTcpExt: 8\nFuture: X\nFuture: 9", ".*", 10, false},
		{"whitespace", "corner", "tabs blanks CRLF and no final newline", "one field", "\n Tcp:\tA \r\n\nTcp:\t1", ".*", 1, false},
		{"empty", "boundary", "empty file", "empty success", "", ".*", 0, false},
		{"empty group", "corner", "paired empty MPTCP group", "empty success", "MPTcpExt:\nMPTcpExt:\n", ".*", 0, false},
		{"empty regexp", "positive", "empty regexp", "all fields", "Tcp: A B\nTcp: 1 2", "", 2, false},
		{"none", "positive", "no matches", "empty success", "Tcp: A\nTcp: 1", "^$", 0, false},
		{"case", "positive", "case sensitive filter", "no matches", "Tcp: A\nTcp: 1", "tcp", 0, false},
		{"unanchored", "positive", "substring filter", "one field", "Tcp: A B\nTcp: 1 2", "_A", 1, false},
		{"anchored", "positive", "exact key", "one field", "Tcp: A AA\nTcp: 1 2", "^Tcp_A$", 1, false},
		{"missing row", "negative", "header only", "malformed", "Tcp: A", ".*", 0, true},
		{"missing empty row", "negative", "empty header only", "malformed", "Tcp:", ".*", 0, true},
		{"protocol", "negative", "different value protocol", "malformed", "Tcp: A\nUdp: 1", ".*", 0, true},
		{"colon", "negative", "missing protocol colon", "malformed", "Tcp A\nTcp 1", ".*", 0, true},
		{"few values", "negative", "short value list", "malformed", "Tcp: A B\nTcp: 1", ".*", 0, true},
		{"extra value", "negative", "long value list", "malformed", "Tcp: A\nTcp: 1 2", ".*", 0, true},
		{"duplicate group", "negative", "same group repeated", "malformed", "Tcp:\nTcp:\nTcp:\nTcp:", ".*", 0, true},
		{"duplicate field", "negative", "hidden duplicate field", "malformed before filter", "Tcp: A A\nTcp: 1 2", "^$", 0, true},
		{"collision", "negative", "underscores collide in final key", "malformed", "A_B: C\nA_B: 1\nA: B_C\nA: 2", ".*", 0, true},
		{"invalid protocol", "negative", "digit starts protocol", "malformed", "1Tcp:\n1Tcp:", ".*", 0, true},
		{"invalid field", "negative", "punctuation in field", "malformed", "Tcp: A-B\nTcp: 1", ".*", 0, true},
		{"UTF8", "negative", "non ASCII field", "malformed", "Tcp: \xff\nTcp: 1", ".*", 0, true},
		{"hidden bad value", "negative", "bad number excluded", "malformed", "Tcp: A\nTcp: broken", "^$", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			p := hostParser{filter: regexp.MustCompile(tc.filter)}
			p.reset()
			err := p.paired(t.Context(), []byte(tc.data))
			if errors.Is(err, errHostMalformed) != tc.invalid || (err != nil && !tc.invalid) {
				t.Fatalf("%s: %v", tc.expectedOutcome, err)
			}
			if err == nil && len(p.samples()) != tc.count {
				t.Fatal(tc.expectedOutcome)
			}
		})
	}
}

func TestHostNumbers(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		want                                         model.Number
		invalid                                      bool
	}{
		{"0", "boundary", "zero", "exact unsigned", model.Unsigned(0), false},
		{"+1", "positive", "explicit plus", "exact unsigned", model.Unsigned(1), false},
		{"-0", "corner", "negative zero", "signed zero", model.Signed(0), false},
		{"-1", "positive", "Tcp MaxConn sentinel", "exact signed", model.Signed(-1), false},
		{"4294967295", "boundary", "32 bit maximum", "exact unsigned", model.Unsigned(math.MaxUint32), false},
		{"9007199254740993", "boundary", "above float exact range", "exact unsigned", model.Unsigned(1<<53 + 1), false},
		{"18446744073709551615", "boundary", "64 bit unsigned maximum", "exact unsigned", model.Unsigned(math.MaxUint64), false},
		{"-9223372036854775808", "boundary", "64 bit signed minimum", "exact signed", model.Signed(math.MinInt64), false},
		{"18446744073709551616", "negative", "unsigned overflow", "reject", model.Number{}, true},
		{"-9223372036854775809", "negative", "signed overflow", "reject", model.Number{}, true},
		{"+", "negative", "incomplete sign", "reject", model.Number{}, true},
		{"-", "negative", "incomplete negative", "reject", model.Number{}, true},
		{"1.1", "negative", "fraction", "reject", model.Number{}, true},
		{"1e2", "negative", "exponent", "reject", model.Number{}, true},
		{"0xff", "negative", "hexadecimal", "reject", model.Number{}, true},
		{"+-1", "negative", "two signs", "reject", model.Number{}, true},
		{"++1", "negative", "repeated plus sign", "reject", model.Number{}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			value, err := hostNumber([]byte(tc.name))
			if (err != nil) != tc.invalid || (err == nil && value != tc.want) {
				t.Fatalf("%s: %v %v", tc.expectedOutcome, value, err)
			}
		})
	}
}

func TestHostIPv6(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome, data string
		count                                              int
		invalid                                            bool
	}{
		{"valid", "positive", "IPv6 and ICMP types", "three fields", "Ip6InReceives 1\nIcmp6InType128 2\nUdp6NoPorts 3", 3, false},
		{"empty", "boundary", "empty IPv6", "success", "\n \t", 0, false},
		{"no six", "negative", "no protocol delimiter", "reject", "IpInReceives 1", 0, true},
		{"first", "negative", "no protocol prefix", "reject", "6A 1", 0, true},
		{"last", "negative", "no field", "reject", "Ip6 1", 0, true},
		{"short", "negative", "no value", "reject", "Ip6InReceives", 0, true},
		{"long", "negative", "extra token", "reject", "Ip6InReceives 1 2", 0, true},
		{"duplicate", "negative", "repeated IPv6 field", "reject", "Ip6A 1\nIp6A 2", 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			p := hostParser{filter: regexp.MustCompile(".*")}
			p.reset()
			err := p.ipv6(t.Context(), []byte(tc.data))
			if (err != nil) != tc.invalid || (err == nil && len(p.samples()) != tc.count) {
				t.Fatalf("%s: %v", tc.expectedOutcome, err)
			}
		})
	}
}

func hostTestData(count int) []byte {
	var header, values strings.Builder
	header.WriteString("Future:")
	values.WriteString("Future:")
	for i := range count {
		fmt.Fprintf(&header, " Field%d", i)
		values.WriteString(" 9007199254740993")
	}
	return []byte(header.String() + "\n" + values.String() + "\n")
}

func TestHostFieldBound(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		count                                        int
		invalid                                      bool
	}{
		{"limit", "boundary", "65536 fields with long header", "accepted", maximumSamples, false},
		{"excess", "negative", "65537 fields before filtering", "oversize", maximumSamples + 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			p := hostParser{filter: regexp.MustCompile("^$")}
			p.reset()
			err := p.paired(t.Context(), hostTestData(tc.count))
			if errors.Is(err, errHostLimit) != tc.invalid || (err != nil && !tc.invalid) {
				t.Fatal(err)
			}
			if err == nil && len(p.samples()) != 0 {
				t.Fatal("filter ignored")
			}
		})
	}
}

func TestHostCombinedBound(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		second                                       string
		invalid                                      bool
	}{
		{"limit", "boundary", "65535 fields plus one in another file", "combined limit accepted", "Tcp: A\nTcp: 1", false},
		{"excess", "negative", "65535 fields plus two in another file", "combined bound rejected", "Tcp: A B\nTcp: 1 2", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			p := hostParser{filter: regexp.MustCompile("^$")}
			p.reset()
			if err := p.paired(t.Context(), hostTestData(maximumSamples-1)); err != nil {
				t.Fatal(err)
			}
			err := p.paired(t.Context(), []byte(tc.second))
			if errors.Is(err, errHostLimit) != tc.invalid || (err != nil && !tc.invalid) {
				t.Fatal(tc.expectedOutcome, err)
			}
		})
	}
}
