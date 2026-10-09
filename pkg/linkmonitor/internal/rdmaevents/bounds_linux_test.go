package rdmaevents

import (
	"fmt"
	"strings"
	"testing"
)

func TestIdentityBounds(t *testing.T) {
	for _, tc := range []struct {
		name, category, description, expectedOutcome string
		count, nameLength, hardwareLength            int
		valid                                        bool
	}{
		{"empty", "boundary", "zero HCAs", "valid empty set", 0, 0, 0, true},
		{"one", "positive", "one HCA", "valid identity", 1, 1, 1, true},
		{"name-max", "boundary", "63-byte name", "accepted", 1, 63, 1, true},
		{"name-over", "negative", "64-byte name", "rejected", 1, 64, 1, false},
		{"hardware-max", "boundary", "1024-byte hardware path", "accepted", 1, 1, 1024, true},
		{"hardware-over", "negative", "1025-byte hardware path", "rejected", 1, 1, 1025, false},
		{"set-max", "boundary", "65536 distinct HCAs", "bounded identity set accepted without opening descriptors", Limit, 0, 1, true},
		{"set-over", "negative", "65537 HCAs", "rejected before acquisition", Limit + 1, 0, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s: %s; expected: %s", tc.category, tc.description, tc.expectedOutcome)
			ids := make([]Identity, tc.count)
			for i := range ids {
				name := strings.Repeat("a", tc.nameLength)
				if tc.nameLength == 0 {
					name = fmt.Sprintf("hca%d", i)
				}
				ids[i] = Identity{Name: name, Hardware: strings.Repeat("h", tc.hardwareLength)}
			}
			if err := validateIdentities(ids); (err == nil) != tc.valid {
				t.Fatal(tc.expectedOutcome, err)
			}
		})
	}
}
