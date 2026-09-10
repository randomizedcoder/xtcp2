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

// TestAnnotate verifies in-place ASN annotation across known and unknown owners.
func TestAnnotate(t *testing.T) {
	recs := []model.Record{
		{Prefix: "1.1.1.0/24", NetworkOwner: "cloudflare"},       // known
		{Prefix: "8.8.8.0/24", Provider: "gcp"},                  // known via provider
		{Prefix: "10.0.0.0/24", NetworkOwner: "acme"},            // unknown -> 0
		{Prefix: "192.0.2.0/24", NetworkOwner: "", Provider: ""}, // empty -> 0
	}
	Annotate(recs)

	want := []uint32{13335, 15169, 0, 0}
	for i, w := range want {
		if recs[i].ASN != w {
			t.Errorf("record %d (%s): ASN = %d, want %d", i, recs[i].Prefix, recs[i].ASN, w)
		}
	}
}
