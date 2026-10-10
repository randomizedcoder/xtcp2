package render

import (
	"encoding/json"
	"testing"
)

// The header and separator are fixed strings ipvrf_show prints before any row
// (ip/ipvrf.c:608-611); every text expectation below carries them verbatim so a
// drift in either shows up as a row failure rather than silently.
const (
	vrfHdr = "Name              Table\n"
	vrfSep = "-----------------------\n"
)

// TestRenderVrfText is `ip vrf show`'s text body: the header and 23-dash
// separator always, then one "%-16s %5u\n" row per VRF, or the
// "No VRF has been configured" form when none matched.
func TestRenderVrfText(t *testing.T) {
	tests := []struct {
		description string
		views       []VrfView
		expected    string
	}{
		{
			description: "positive: one VRF renders header, separator and a single padded row",
			views:       []VrfView{{Name: "goipvrf", Table: 100}},
			expected:    vrfHdr + vrfSep + "goipvrf            100\n",
		},
		{
			description: "positive: multiple VRFs render in slice order, not sorted here",
			views:       []VrfView{{Name: "vrfred", Table: 10}, {Name: "vrfblue", Table: 9}},
			expected:    vrfHdr + vrfSep + "vrfred              10\n" + "vrfblue              9\n",
		},
		{
			description: "corner: no VRF renders the configured-none form after the header",
			views:       nil,
			expected:    vrfHdr + vrfSep + "No VRF has been configured\n",
		},
		{
			description: "corner: an empty (non-nil) slice renders the same empty form",
			views:       []VrfView{},
			expected:    vrfHdr + vrfSep + "No VRF has been configured\n",
		},
		{
			description: "boundary: a 16-char name gets exactly one space before the table field",
			views:       []VrfView{{Name: "sixteencharname0", Table: 5}},
			expected:    vrfHdr + vrfSep + "sixteencharname0     5\n",
		},
		{
			description: "boundary: a name past 16 cols is not truncated, still one space then %5u",
			views:       []VrfView{{Name: "seventeencharname7", Table: 5}},
			expected:    vrfHdr + vrfSep + "seventeencharname7     5\n",
		},
		{
			description: "boundary: a table id wider than five digits grows the field, no truncation",
			views:       []VrfView{{Name: "vrfbig", Table: 999999}},
			expected:    vrfHdr + vrfSep + "vrfbig           999999\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			got := RenderVrfText(tc.views)
			if got != tc.expected {
				t.Errorf("RenderVrfText\n got %q\nwant %q", got, tc.expected)
			}
		})
	}
}

// TestVrfViewJSON pins the `-j` object shape: ipvrf_print's keys in order, with
// table a JSON number (print_uint), not a string. The object encodes a
// []VrfView directly (obj_vrf.go), so the struct tags are the whole contract.
func TestVrfViewJSON(t *testing.T) {
	tests := []struct {
		description string
		view        VrfView
		expected    string
	}{
		{
			description: "positive: key order name then table, table numeric",
			view:        VrfView{Name: "goipvrf", Table: 100},
			expected:    `{"name":"goipvrf","table":100}`,
		},
		{
			description: "boundary: a name with a quote is JSON-escaped",
			view:        VrfView{Name: `a"b`, Table: 1},
			expected:    `{"name":"a\"b","table":1}`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			b, err := json.Marshal(tc.view)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			if string(b) != tc.expected {
				t.Errorf("json.Marshal\n got %s\nwant %s", b, tc.expected)
			}
		})
	}
}

// TestVrfViewSliceJSON is the array the object actually emits: a flat list, and
// the empty array (not null) when no VRF matched. obj_vrf.go builds a non-nil
// slice precisely so the empty case marshals to [] rather than null.
func TestVrfViewSliceJSON(t *testing.T) {
	tests := []struct {
		description string
		views       []VrfView
		expected    string
	}{
		{
			description: "positive: one object in an array",
			views:       []VrfView{{Name: "goipvrf", Table: 100}},
			expected:    `[{"name":"goipvrf","table":100}]`,
		},
		{
			description: "corner: an empty non-nil slice is [], not null",
			views:       []VrfView{},
			expected:    `[]`,
		},
	}
	for _, tc := range tests {
		t.Run(tc.description, func(t *testing.T) {
			b, err := json.Marshal(tc.views)
			if err != nil {
				t.Fatalf("json.Marshal: %v", err)
			}
			if string(b) != tc.expected {
				t.Errorf("json.Marshal\n got %s\nwant %s", b, tc.expected)
			}
		})
	}
}
