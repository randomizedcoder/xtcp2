package combine

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
)

// TestValidate is table-driven and covers positive, negative, boundary, and
// corner cases for CIDR validation and the positive/negative boundary counts.
func TestValidate(t *testing.T) {
	tests := []struct {
		name       string
		desc       string // what this row exercises + case class
		class      string
		in         []model.Record
		wantValid  []string       // canonical prefixes expected in output (in order)
		wantVer    []int32        // ip_version expected, parallel to wantValid
		wantReject []RejectReason // rejection reasons expected (in order)
	}{
		{
			name:      "valid_v4",
			desc:      "positive: a normal IPv4 /24 is kept and canonical",
			class:     "positive",
			in:        []model.Record{{Prefix: "13.248.0.0/16"}},
			wantValid: []string{"13.248.0.0/16"},
			wantVer:   []int32{4},
		},
		{
			name:      "valid_v6",
			desc:      "positive: a normal IPv6 prefix is kept with version 6",
			class:     "positive",
			in:        []model.Record{{Prefix: "2600:1f00::/24"}},
			wantValid: []string{"2600:1f00::/24"},
			wantVer:   []int32{6},
		},
		{
			name:      "boundary_default_route_and_host",
			desc:      "boundary: /0 and /32 edges are valid",
			class:     "boundary",
			in:        []model.Record{{Prefix: "0.0.0.0/0"}, {Prefix: "1.2.3.4/32"}},
			wantValid: []string{"0.0.0.0/0", "1.2.3.4/32"},
			wantVer:   []int32{4, 4},
		},
		{
			name:      "boundary_v6_full_length",
			desc:      "boundary: a /128 single-address IPv6 prefix is valid",
			class:     "boundary",
			in:        []model.Record{{Prefix: "2001:db8::1/128"}},
			wantValid: []string{"2001:db8::1/128"},
			wantVer:   []int32{6},
		},
		{
			name:       "negative_empty",
			desc:       "negative: an empty prefix is rejected as Empty",
			class:      "negative",
			in:         []model.Record{{Prefix: ""}},
			wantReject: []RejectReason{ReasonEmpty},
		},
		{
			name:       "negative_garbage",
			desc:       "negative: non-CIDR text is rejected as ParseError",
			class:      "negative",
			in:         []model.Record{{Prefix: "not-a-cidr"}},
			wantReject: []RejectReason{ReasonParseError},
		},
		{
			name:       "negative_bad_mask",
			desc:       "negative: an out-of-range mask /33 is rejected",
			class:      "negative",
			in:         []model.Record{{Prefix: "10.0.0.0/33"}},
			wantReject: []RejectReason{ReasonParseError},
		},
		{
			name:       "negative_bare_ip",
			desc:       "negative: a bare IP with no mask is rejected (ParsePrefix needs a /)",
			class:      "negative",
			in:         []model.Record{{Prefix: "1.2.3.4"}},
			wantReject: []RejectReason{ReasonParseError},
		},
		{
			name:      "corner_noncanonical",
			desc:      "corner: host bits set are masked to the canonical network",
			class:     "corner",
			in:        []model.Record{{Prefix: "1.2.3.4/24"}},
			wantValid: []string{"1.2.3.0/24"},
			wantVer:   []int32{4},
		},
		{
			name:       "corner_duplicate",
			desc:       "corner: an identical record is kept once, second is Duplicate",
			class:      "corner",
			in:         []model.Record{{Prefix: "8.8.8.0/24"}, {Prefix: "8.8.8.0/24"}},
			wantValid:  []string{"8.8.8.0/24"},
			wantVer:    []int32{4},
			wantReject: []RejectReason{ReasonDuplicate},
		},
		{
			name:  "corner_same_prefix_diff_service_kept",
			desc:  "corner: same prefix under a different service is not a duplicate",
			class: "corner",
			in: []model.Record{
				{Prefix: "8.8.8.0/24", Service: "a"},
				{Prefix: "8.8.8.0/24", Service: "b"},
			},
			wantValid: []string{"8.8.8.0/24", "8.8.8.0/24"},
			wantVer:   []int32{4, 4},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			seen := map[string]struct{}{}
			got := Validate(tc.in, seen)

			if len(got.Valid) != len(tc.wantValid) {
				t.Fatalf("%s: valid count = %d, want %d", tc.desc, len(got.Valid), len(tc.wantValid))
			}
			for i, want := range tc.wantValid {
				if got.Valid[i].Prefix != want {
					t.Errorf("%s: valid[%d].Prefix = %q, want %q", tc.desc, i, got.Valid[i].Prefix, want)
				}
				if got.Valid[i].IPVersion != tc.wantVer[i] {
					t.Errorf("%s: valid[%d].IPVersion = %d, want %d", tc.desc, i, got.Valid[i].IPVersion, tc.wantVer[i])
				}
			}
			if len(got.Rejected) != len(tc.wantReject) {
				t.Fatalf("%s: rejected count = %d, want %d", tc.desc, len(got.Rejected), len(tc.wantReject))
			}
			for i, want := range tc.wantReject {
				if got.Rejected[i].Reason != want {
					t.Errorf("%s: rejected[%d].Reason = %q, want %q", tc.desc, i, got.Rejected[i].Reason, want)
				}
			}
		})
	}
}
