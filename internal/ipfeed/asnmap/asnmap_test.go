package asnmap

import (
	"testing"

	"github.com/randomizedcoder/xtcp2/internal/ipfeed/model"
)

// TestLookup covers owner/provider resolution, precedence, and misses.
func TestLookup(t *testing.T) {
	tests := []struct {
		name, desc, class string
		owner, provider   string
		wantASN           uint32
		wantOK            bool
	}{
		{"positive_owner", "positive: a known network_owner resolves", "positive",
			"cloudflare", "", 13335, true},
		{"positive_provider_fallback", "positive: falls back to provider when owner is empty", "positive",
			"", "gcp", 15169, true},
		{"boundary_owner_precedence", "boundary: owner is tried before provider", "boundary",
			"fastly", "aws", 54113, true},
		{"corner_case_insensitive", "corner: matching is case- and space-insensitive", "corner",
			"  Cloudflare ", "", 13335, true},
		{"negative_unknown", "negative: an unknown name yields (0,false)", "negative",
			"acme-corp", "acme-corp", 0, false},
		{"boundary_both_empty", "boundary: empty owner and provider is a miss", "boundary",
			"", "", 0, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotASN, gotOK := Lookup(tc.owner, tc.provider)
			if gotASN != tc.wantASN || gotOK != tc.wantOK {
				t.Errorf("%s: Lookup(%q,%q) = (%d,%v), want (%d,%v)",
					tc.desc, tc.owner, tc.provider, gotASN, gotOK, tc.wantASN, tc.wantOK)
			}
		})
	}
}

// TestAnnotate verifies in-place ASN annotation across known and unknown
// owners. expectedASNs is index-aligned with records.
func TestAnnotate(t *testing.T) {
	tests := []struct {
		description  string
		records      []model.Record
		expectedASNs []uint32
	}{
		// positive
		{
			description:  "positive: a known network_owner is annotated",
			records:      []model.Record{{Prefix: "1.1.1.0/24", NetworkOwner: "cloudflare"}},
			expectedASNs: []uint32{13335},
		},
		{
			description:  "positive: falls back to provider when owner is empty",
			records:      []model.Record{{Prefix: "8.8.8.0/24", Provider: "gcp"}},
			expectedASNs: []uint32{15169},
		},
		{
			description: "positive: a mixed slice is annotated element-wise in place",
			records: []model.Record{
				{Prefix: "1.1.1.0/24", NetworkOwner: "cloudflare"},
				{Prefix: "8.8.8.0/24", Provider: "gcp"},
				{Prefix: "10.0.0.0/24", NetworkOwner: "acme"},
				{Prefix: "192.0.2.0/24", NetworkOwner: "", Provider: ""},
			},
			expectedASNs: []uint32{13335, 15169, 0, 0},
		},
		// negative
		{
			description:  "negative: an unknown owner and provider stays at 0",
			records:      []model.Record{{Prefix: "10.0.0.0/24", NetworkOwner: "acme", Provider: "acme"}},
			expectedASNs: []uint32{0},
		},
		// boundary
		{
			description:  "boundary: empty owner and provider stays at 0",
			records:      []model.Record{{Prefix: "192.0.2.0/24", NetworkOwner: "", Provider: ""}},
			expectedASNs: []uint32{0},
		},
		{
			description:  "boundary: an empty slice is a no-op",
			records:      []model.Record{},
			expectedASNs: []uint32{},
		},
		{
			description:  "boundary: a nil slice is a no-op",
			records:      nil,
			expectedASNs: nil,
		},
		// corner
		{
			description:  "corner: owner wins over a conflicting provider",
			records:      []model.Record{{Prefix: "151.101.0.0/16", NetworkOwner: "fastly", Provider: "aws"}},
			expectedASNs: []uint32{54113},
		},
		{
			description:  "corner: an unknown record's pre-existing ASN is left untouched, not reset",
			records:      []model.Record{{Prefix: "10.0.0.0/24", NetworkOwner: "acme", ASN: 64512}},
			expectedASNs: []uint32{64512},
		},
		{
			description:  "corner: a known record's pre-existing ASN is overwritten",
			records:      []model.Record{{Prefix: "1.1.1.0/24", NetworkOwner: "cloudflare", ASN: 64512}},
			expectedASNs: []uint32{13335},
		},
		{
			description:  "corner: matching is case- and whitespace-insensitive",
			records:      []model.Record{{Prefix: "1.1.1.0/24", NetworkOwner: "  CloudFlare "}},
			expectedASNs: []uint32{13335},
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			Annotate(tc.records)
			if len(tc.records) != len(tc.expectedASNs) {
				t.Fatalf("%s: test row malformed: %d records vs %d expected ASNs", tc.description, len(tc.records), len(tc.expectedASNs))
			}
			for i, want := range tc.expectedASNs {
				if got := tc.records[i].ASN; got != want {
					t.Errorf("%s: record %d (%s): ASN = %d, want %d", tc.description, i, tc.records[i].Prefix, got, want)
				}
			}
		})
	}
}
