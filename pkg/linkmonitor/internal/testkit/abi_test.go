package testkit

import (
	"slices"
	"strings"
	"testing"
)

func TestCompareABILayout(t *testing.T) {
	consts := map[string]uint64{"hdrLen": 64, "regAgent2": 0xc0281b04}
	src := GoSource{Consts: []string{"hdrLen", "regAgent2"}, Literals: []uint64{64, 0xc0281b04}}
	good := "pkg hdrLen 64\npkg regAgent2 0xc0281b04\npkg !regAgent 0xc01c1b01\nother x 1\n"
	tests := []struct {
		name, category, description string
		layout                      string
		consts                      map[string]uint64
		src                         GoSource
		expected                    []string
	}{
		{"match", "positive", "Every line matches a declared constant; other packages' lines are ignored", good, consts, src, nil},
		{"wrong_value", "negative", "A constant differs from the kernel", "pkg hdrLen 63\npkg regAgent2 0xc0281b04\n", consts, src, []string{"hdrLen: Go 0x40, kernel 0x3f"}},
		{"extra_line", "negative", "The kernel layout names a constant Go lacks", good + "pkg newField 8\n", consts, src, []string{"newField: in the kernel layout but not declared in Go"}},
		{"missing_line", "negative", "Go declares a constant the layout lacks", "pkg hdrLen 64\n", consts, src, []string{"regAgent2: declared in Go but missing from the kernel layout"}},
		{"unmapped_const", "negative", "A constant is declared but not in the comparison map", good, consts, GoSource{Consts: append(slices.Clone(src.Consts), "stray")}, []string{"stray: declared but not in the comparison map"}},
		{"value_limits", "boundary", "Zero and the largest uint64, in decimal and hex", "pkg zero 0\npkg max 0xffffffffffffffff\n", map[string]uint64{"zero": 0, "max": ^uint64(0)}, GoSource{Consts: []string{"zero", "max"}}, nil},
		{"malformed_line", "boundary", "A line with two fields", "pkg hdrLen\n", map[string]uint64{}, GoSource{}, []string{"line 1: want \"<package> <name> <value>\""}},
		{"bad_value", "boundary", "A value that is not a number", "pkg hdrLen sixty\n", map[string]uint64{}, GoSource{}, []string{"hdrLen: bad value \"sixty\""}},
		{"forbidden_literal", "corner", "The forbidden ioctl number is written as a literal", good, consts, GoSource{Consts: src.Consts, Literals: []uint64{0xc01c1b01}}, []string{"literal 0xc01c1b01 is forbidden regAgent"}},
		{"forbidden_name", "corner", "The forbidden name is declared", good, map[string]uint64{"hdrLen": 64, "regAgent2": 0xc0281b04, "regAgent": 1}, GoSource{Consts: []string{"hdrLen", "regAgent2", "regAgent"}}, []string{"regAgent: declared in Go but missing from the kernel layout", "regAgent: forbidden name is declared"}},
		{"duplicate_line", "corner", "One name listed twice", good + "pkg hdrLen 64\n", consts, src, []string{"hdrLen: listed twice"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := CompareABILayout([]byte(tt.layout), "pkg", tt.consts, tt.src)
			if !slices.Equal(got, tt.expected) {
				t.Fatalf("%s (%s): got\n%s\nwant\n%s", tt.description, tt.category, strings.Join(got, "\n"), strings.Join(tt.expected, "\n"))
			}
		})
	}
}

func TestABIRowMatches(t *testing.T) {
	tests := []struct {
		name, category, description string
		row                         ABIRow
		problem                     string
		expected                    bool
	}{
		{"prefix", "positive", "A value mismatch under the row's prefix", ABIRow{Prefixes: []string{"umadHdr"}}, "umadHdrLen: Go 0x40, kernel 0x3f", true},
		{"text", "positive", "A forbidden-literal problem selected by text", ABIRow{Text: "forbidden"}, "literal 0xc01c1b01 is forbidden umadRegisterAgent", true},
		{"other_prefix", "negative", "A problem under another prefix", ABIRow{Prefixes: []string{"umadHdr"}}, "umadRegReq2Len: Go 0x28, kernel 0x29", false},
		{"empty_row", "boundary", "A row with no prefixes or text", ABIRow{}, "umadHdrLen: listed twice", false},
		{"forbidden_under_prefix", "corner", "A forbidden-name problem whose name shares the prefix", ABIRow{Prefixes: []string{"umadRegisterAgent"}}, "umadRegisterAgent: forbidden name is declared", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.row.Matches(tt.problem); got != tt.expected {
				t.Fatalf("%s (%s): got %v, want %v", tt.description, tt.category, got, tt.expected)
			}
		})
	}
}
