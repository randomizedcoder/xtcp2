package goldenrec

import (
	"slices"
	"testing"
)

// coverageInput is a profile, the coverage it must meet, and the manifest
// lists "@manifest.<key>" entries resolve against.
type coverageInput struct {
	profile  Profile
	coverage Coverage
	refs     map[string][]string
}

// coverageProfile is a small capture's shape: state 1 and 10, cubic,
// families 2 and 10, host and container namespaces, bbr_info all zero.
func coverageProfile() Profile {
	return Profile{
		Rows: 4,
		Columns: map[string]ColumnProfile{
			"inet_diag_msg_state": {Class: "strict", Nonzero: 4, Distinct: "2", Values: []string{"1", "10"}},
			"inet_diag_cong":      {Class: "strict", Nonzero: 4, Distinct: "1", Values: []string{"cubic"}},
			"tcp_info_rtt":        {Class: "presence", Nonzero: 3, Distinct: "3"},
			"bbr_info_bw_hi":      {Class: "presence", Nonzero: 0, Distinct: "1"},
			"inet_diag_msg_uid":   {Class: "strict", Nonzero: 2, Distinct: "2"},
		},
		Families:   map[string]int{"2": 2, "10": 2},
		NetnsKinds: map[string]int{"host": 2, "container": 2},
	}
}

// go test ./pkg/goldenrec/ -run TestCheckCoverage
func TestCheckCoverage(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		input       coverageInput
		expected    []string
	}{
		{"met", "positive", "a profile that meets its coverage",
			coverageInput{coverageProfile(), Coverage{
				Populated: []string{"tcp_info_rtt", "inet_diag_cong"},
				Families:  []string{"2", "10"},
				Kinds:     []string{"host", "container"},
			}, nil},
			nil},
		{"values_met", "positive", "required values are present",
			coverageInput{coverageProfile(), Coverage{Values: map[string][]string{"inet_diag_msg_state": {"1", "10"}}}, nil},
			nil},
		{"missing_populated", "negative", "a required column is all-zero",
			coverageInput{coverageProfile(), Coverage{Populated: []string{"bbr_info_bw_hi"}}, nil},
			[]string{"coverage: bbr_info_bw_hi never populated"}},
		{"missing_value", "negative", "a required value is absent",
			coverageInput{coverageProfile(), Coverage{Values: map[string][]string{"inet_diag_msg_state": {"1", "6"}}}, nil},
			[]string{"coverage: inet_diag_msg_state=6 missing"}},
		{"unknown_column", "negative", "the coverage names a non-column",
			coverageInput{coverageProfile(), Coverage{Populated: []string{"nope"}}, nil},
			[]string{"coverage: nope is not a column"}},
		{"missing_family_kind", "negative", "a required family and kind are absent",
			coverageInput{coverageProfile(), Coverage{Families: []string{"2", "44"}, Kinds: []string{"named"}}, nil},
			[]string{"coverage: family 44 missing", "coverage: kind named missing"}},
		{"manifest_ref", "boundary", "the CA list comes from the manifest",
			coverageInput{coverageProfile(), Coverage{Values: map[string][]string{"inet_diag_cong": {"@manifest.cong_available"}}},
				map[string][]string{"cong_available": {"cubic", "nv"}}},
			[]string{"coverage: inet_diag_cong=nv missing"}},
		{"empty_coverage", "boundary", "an empty coverage requires nothing",
			coverageInput{coverageProfile(), Coverage{}, nil},
			nil},
		{"manifest_ref_empty", "boundary", "an empty manifest list is an error",
			coverageInput{coverageProfile(), Coverage{Values: map[string][]string{"inet_diag_cong": {"@manifest.cong_available"}}},
				map[string][]string{"cong_available": {}}},
			[]string{"coverage: manifest cong_available is empty"}},
		{"manifest_ref_absent", "corner", "a reference the manifest doesn't have",
			coverageInput{coverageProfile(), Coverage{Values: map[string][]string{"inet_diag_cong": {"@manifest.cong_available"}}}, nil},
			[]string{"coverage: manifest has no cong_available"}},
		{"counted_column", "corner", "values required of a column that is only counted",
			coverageInput{coverageProfile(), Coverage{Values: map[string][]string{"inet_diag_msg_uid": {"0"}}}, nil},
			[]string{"coverage: inet_diag_msg_uid values are not listed"}},
		{"sorted_findings", "corner", "findings come out sorted whatever the input order",
			coverageInput{coverageProfile(), Coverage{Populated: []string{"zz", "aa"}}, nil},
			[]string{"coverage: aa is not a column", "coverage: zz is not a column"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := CheckCoverage(tc.input.profile, tc.input.coverage, tc.input.refs)
			if !slices.Equal(got, tc.expected) {
				t.Errorf("%s [%s] %s: coverage %+v: got %q, expected %q",
					tc.name, tc.category, tc.description, tc.input.coverage, got, tc.expected)
			}
		})
	}
}
