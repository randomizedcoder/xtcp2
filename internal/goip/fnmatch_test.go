package goip

import (
	"errors"
	"testing"
)

// fnmatchTests are TestFnmatch0's rows; FuzzFnmatch seeds from them.
var fnmatchTests = []struct {
	description string
	pattern     string
	s           string
	expected    bool
	wantErr     error
}{
	{description: "positive: literal", pattern: "d0", s: "d0", expected: true},
	{description: "positive: star", pattern: "d0:*", s: "d0:1", expected: true},
	{description: "positive: star matches empty", pattern: "d0*", s: "d0", expected: true},
	{description: "positive: star in the middle backtracks", pattern: "d*:1", s: "d0:0:1", expected: true},
	{description: "positive: question mark", pattern: "d?", s: "d7", expected: true},
	{description: "negative: question mark needs a character", pattern: "d?", s: "d", expected: false},
	{description: "positive: range", pattern: "d[0-3]", s: "d2", expected: true},
	{description: "negative: outside the range", pattern: "d[0-3]", s: "d4", expected: false},
	{description: "positive: negated bracket with !", pattern: "d[!0]", s: "d1", expected: true},
	{description: "positive: negated bracket with ^", pattern: "d[^0]", s: "d1", expected: true},
	{description: "negative: negated bracket", pattern: "d[!0]", s: "d0", expected: false},
	{description: "corner: ] first in a bracket is a member", pattern: "[]a]", s: "]", expected: true},
	{description: "corner: ] first after ! is a member", pattern: "[!]]", s: "]", expected: false},
	{description: "corner: - last in a bracket is a member", pattern: "d[0-]", s: "d-", expected: true},
	{description: "corner: star crosses / (glibc, unlike path.Match)", pattern: "a*", s: "a/b", expected: true},
	{description: "corner: ? matches / with no FNM_PATHNAME", pattern: "a?b", s: "a/b", expected: true},
	{description: "corner: unterminated [ is a literal", pattern: "d[0", s: "d[0", expected: true},
	{description: "corner: unterminated [ does not match what a bracket would", pattern: "d[0", s: "d0", expected: false},
	{description: "corner: escaped star is a literal", pattern: `d\*`, s: "d*", expected: true},
	{description: "negative: escaped star is not a wildcard", pattern: `d\*`, s: "d0", expected: false},
	{description: "corner: escape inside a bracket", pattern: `d[\]]`, s: "d]", expected: true},
	{description: "corner: a trailing backslash matches nothing", pattern: `d\`, s: `d\`, expected: false},
	{description: "boundary: empty pattern, empty string", pattern: "", s: "", expected: true},
	{description: "boundary: empty pattern, non-empty string", pattern: "", s: "d0", expected: false},
	{description: "boundary: star alone matches empty", pattern: "*", s: "", expected: true},
	{description: "negative: string too short for the pattern", pattern: "d0:*", s: "d0", expected: false},
	{description: "negative: case matters", pattern: "D0", s: "d0", expected: false},
	{description: "negative: POSIX class is refused", pattern: "[[:digit:]]", s: "1", wantErr: ErrNotImplemented},
	{description: "negative: equivalence class is refused", pattern: "[[=a=]]", s: "a", wantErr: ErrNotImplemented},
	{description: "corner: a refused class fails whatever the string", pattern: "x[[.a.]]", s: "", wantErr: ErrNotImplemented},
}

// TestFnmatch0 is fnmatch0 against glibc's fnmatch(pattern, s, 0). The
// expectations are glibc's (posix/fnmatch_loop.c), and the corner rows are
// the ones where path.Match would answer differently.
//
// go test ./internal/goip/ -run TestFnmatch0
func TestFnmatch0(t *testing.T) {
	for _, tc := range fnmatchTests {
		t.Run(tc.description, func(t *testing.T) {
			got, err := fnmatch0(tc.pattern, tc.s)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("fnmatch0(%q, %q) err = %v, want %v", tc.pattern, tc.s, err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("fnmatch0(%q, %q): %v", tc.pattern, tc.s, err)
			}
			if got != tc.expected {
				t.Fatalf("fnmatch0(%q, %q) = %v, want %v", tc.pattern, tc.s, got, tc.expected)
			}
		})
	}
}
