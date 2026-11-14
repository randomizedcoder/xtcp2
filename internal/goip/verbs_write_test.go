package goip

import "testing"

// tableSegment is the chain segment a write table implies: every entry a
// verbWrite, in table order.
func tableSegment[T any](table []T, name func(*T) (string, bool)) []verb {
	out := make([]verb, len(table))
	for i := range table {
		n, exact := name(&table[i])
		out[i] = verb{name: n, kind: verbWrite, exact: exact}
	}
	return out
}

// TestWriteSegmentsMatchTables pins the chains' copies of the write verb
// names to the tables the write paths run from, so a verb added to a table
// cannot be missing from the chain, where /ip would read it as a device
// name instead of refusing it.
func TestWriteSegmentsMatchTables(t *testing.T) {
	tests := []struct {
		name        string
		category    string
		description string
		segment     []verb
		expected    []verb
	}{
		{
			name:        "link",
			category:    "B",
			description: "linkWriteSegment equals linkWriteVerbs, names and order",
			segment:     linkWriteSegment,
			expected:    tableSegment(linkWriteVerbs, func(v *linkWriteVerb) (string, bool) { return v.name, false }),
		},
		{
			name:        "address",
			category:    "B",
			description: "addrWriteSegment equals addrWriteVerbs, exact chg included",
			segment:     addrWriteSegment,
			expected:    tableSegment(addrWriteVerbs, func(v *addrWriteVerb) (string, bool) { return v.name, v.exact }),
		},
		{
			name:        "route",
			category:    "B",
			description: "routeWriteSegment equals routeWriteVerbs, prepend/append/test included",
			segment:     routeWriteSegment,
			expected:    tableSegment(routeWriteVerbs, func(v *routeWriteVerb) (string, bool) { return v.name, v.exact }),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if len(tt.segment) != len(tt.expected) {
				t.Fatalf("%s: %d verbs, table has %d", tt.description, len(tt.segment), len(tt.expected))
			}
			for i := range tt.expected {
				if tt.segment[i] != tt.expected[i] {
					t.Errorf("%s: verb %d = %+v, table says %+v", tt.description, i, tt.segment[i], tt.expected[i])
				}
			}
		})
	}
}
